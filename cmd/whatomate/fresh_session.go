package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/internal/database"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/zerodha/logf"
	"gorm.io/gorm"
)

func runFreshSession(args []string) {
	fs := flag.NewFlagSet("fresh-session", flag.ExitOnError)
	configPath := fs.String("config", "config.toml", "Path to config file")
	phone := fs.String("phone", "", "Contact phone number (with or without +)")
	orgIDStr := fs.String("org", "", "Organization UUID (required when phone matches multiple orgs)")
	accountName := fs.String("account", "", "WhatsApp account name (optional filter / create target)")
	_ = fs.Parse(args)

	lo := logf.New(logf.Opts{
		EnableColor:     true,
		Level:           logf.InfoLevel,
		TimestampFormat: "2006-01-02 15:04:05",
		DefaultFields:   []any{"app", "whatomate-fresh-session"},
	})

	normalized := normalizeCLIPhone(*phone)
	if normalized == "" {
		fmt.Fprintln(os.Stderr, "Error: -phone is required")
		fs.Usage()
		os.Exit(1)
	}

	var orgFilter *uuid.UUID
	if strings.TrimSpace(*orgIDStr) != "" {
		id, err := uuid.Parse(strings.TrimSpace(*orgIDStr))
		if err != nil {
			lo.Fatal("Invalid -org UUID", "error", err)
		}
		orgFilter = &id
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		lo.Fatal("Failed to load config", "error", err)
	}

	db, err := database.NewPostgres(&cfg.Database, cfg.App.Debug)
	if err != nil {
		lo.Fatal("Failed to connect to database", "error", err)
	}

	result, err := startFreshSession(db, normalized, orgFilter, strings.TrimSpace(*accountName))
	if err != nil {
		lo.Fatal("Failed to start fresh session", "error", err)
	}

	lo.Info("Fresh session ready",
		"phone", result.Phone,
		"contact_id", result.ContactID,
		"organization_id", result.OrganizationID,
		"whatsapp_account", result.WhatsAppAccount,
		"cancelled_sessions", result.CancelledSessions,
		"resumed_transfers", result.ResumedTransfers,
		"abandoned_drafts", result.AbandonedDrafts,
		"new_session_id", result.NewSessionID,
	)
}

type freshSessionResult struct {
	Phone             string
	ContactID         uuid.UUID
	OrganizationID    uuid.UUID
	WhatsAppAccount   string
	CancelledSessions int64
	ResumedTransfers  int64
	AbandonedDrafts   int64
	NewSessionID      uuid.UUID
}

func startFreshSession(db *gorm.DB, phone string, orgFilter *uuid.UUID, accountFilter string) (*freshSessionResult, error) {
	contacts, err := findContactsByPhone(db, phone, orgFilter)
	if err != nil {
		return nil, err
	}
	if len(contacts) == 0 {
		return nil, fmt.Errorf("no contact found for phone %s", phone)
	}
	if len(contacts) > 1 {
		var parts []string
		for _, c := range contacts {
			parts = append(parts, fmt.Sprintf("org=%s contact=%s", c.OrganizationID, c.ID))
		}
		return nil, fmt.Errorf("phone matches %d contacts; pass -org to disambiguate: %s", len(contacts), strings.Join(parts, ", "))
	}
	contact := contacts[0]

	now := time.Now()
	sessionQuery := db.Model(&models.ChatbotSession{}).
		Where("organization_id = ? AND contact_id = ? AND status = ?",
			contact.OrganizationID, contact.ID, models.SessionStatusActive)
	if accountFilter != "" {
		sessionQuery = sessionQuery.Where("whats_app_account = ?", accountFilter)
	}

	var activeSessions []models.ChatbotSession
	if err := sessionQuery.Find(&activeSessions).Error; err != nil {
		return nil, fmt.Errorf("list active sessions: %w", err)
	}

	accounts := uniqueSessionAccounts(activeSessions, accountFilter, contact.WhatsAppAccount)
	if len(accounts) == 0 {
		resolved, err := resolveWhatsAppAccount(db, contact.OrganizationID, accountFilter, contact.WhatsAppAccount)
		if err != nil {
			return nil, err
		}
		accounts = []string{resolved}
	}
	if len(accounts) > 1 {
		return nil, fmt.Errorf("contact has active sessions on multiple WhatsApp accounts (%s); pass -account to choose one", strings.Join(accounts, ", "))
	}
	account := accounts[0]

	cancelQ := db.Model(&models.ChatbotSession{}).
		Where("organization_id = ? AND contact_id = ? AND status = ? AND whats_app_account = ?",
			contact.OrganizationID, contact.ID, models.SessionStatusActive, account)
	cancelRes := cancelQ.Updates(map[string]any{
		"status":       models.SessionStatusCancelled,
		"completed_at": now,
	})
	if cancelRes.Error != nil {
		return nil, fmt.Errorf("cancel active sessions: %w", cancelRes.Error)
	}

	transferRes := db.Model(&models.AgentTransfer{}).
		Where("organization_id = ? AND contact_id = ? AND status = ?",
			contact.OrganizationID, contact.ID, models.TransferStatusActive).
		Updates(map[string]any{
			"status":     models.TransferStatusResumed,
			"resumed_at": now,
		})
	if transferRes.Error != nil {
		return nil, fmt.Errorf("resume active transfers: %w", transferRes.Error)
	}

	draftRes := db.Model(&models.CommerceDraft{}).
		Where("organization_id = ? AND contact_id = ? AND whats_app_account = ? AND status IN ?",
			contact.OrganizationID, contact.ID, account,
			[]string{"active", "submitting", "pending_payment"}).
		Updates(map[string]any{
			"status":           "abandoned",
			"abandoned_at":     now,
			"active_owner_key": nil,
			"version":          gorm.Expr("version + 1"),
		})
	if draftRes.Error != nil {
		return nil, fmt.Errorf("abandon commerce drafts: %w", draftRes.Error)
	}

	if err := db.Model(&models.Contact{}).
		Where("id = ?", contact.ID).
		Updates(map[string]any{
			"chatbot_last_message_at": nil,
			"chatbot_reminder_sent":   false,
		}).Error; err != nil {
		return nil, fmt.Errorf("clear chatbot tracking: %w", err)
	}

	session := models.ChatbotSession{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  contact.OrganizationID,
		ContactID:       contact.ID,
		WhatsAppAccount: account,
		PhoneNumber:     phone,
		Status:          models.SessionStatusActive,
		SessionData:     models.JSONB{},
		StartedAt:       now,
		LastActivityAt:  now,
	}
	if err := db.Create(&session).Error; err != nil {
		return nil, fmt.Errorf("create fresh session: %w", err)
	}

	return &freshSessionResult{
		Phone:             phone,
		ContactID:         contact.ID,
		OrganizationID:    contact.OrganizationID,
		WhatsAppAccount:   account,
		CancelledSessions: cancelRes.RowsAffected,
		ResumedTransfers:  transferRes.RowsAffected,
		AbandonedDrafts:   draftRes.RowsAffected,
		NewSessionID:      session.ID,
	}, nil
}

func normalizeCLIPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	return strings.TrimPrefix(phone, "+")
}

func findContactsByPhone(db *gorm.DB, phone string, orgFilter *uuid.UUID) ([]models.Contact, error) {
	var contacts []models.Contact
	q := db.Where("phone_number IN ?", []string{phone, "+" + phone})
	if orgFilter != nil {
		q = q.Where("organization_id = ?", *orgFilter)
	}
	if err := q.Find(&contacts).Error; err != nil {
		return nil, err
	}
	return contacts, nil
}

func uniqueSessionAccounts(sessions []models.ChatbotSession, accountFilter, contactAccount string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if accountFilter != "" && name != accountFilter {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, s := range sessions {
		add(s.WhatsAppAccount)
	}
	if len(out) == 0 {
		add(contactAccount)
	}
	return out
}

func resolveWhatsAppAccount(db *gorm.DB, orgID uuid.UUID, accountFilter, contactAccount string) (string, error) {
	if accountFilter != "" {
		var count int64
		if err := db.Model(&models.WhatsAppAccount{}).
			Where("organization_id = ? AND name = ?", orgID, accountFilter).
			Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return "", fmt.Errorf("WhatsApp account %q not found in organization %s", accountFilter, orgID)
		}
		return accountFilter, nil
	}
	if strings.TrimSpace(contactAccount) != "" {
		return contactAccount, nil
	}
	var accounts []models.WhatsAppAccount
	if err := db.Where("organization_id = ?", orgID).Find(&accounts).Error; err != nil {
		return "", err
	}
	if len(accounts) == 1 {
		return accounts[0].Name, nil
	}
	if len(accounts) == 0 {
		return "", fmt.Errorf("organization %s has no WhatsApp accounts", orgID)
	}
	names := make([]string, 0, len(accounts))
	for _, a := range accounts {
		names = append(names, a.Name)
	}
	return "", fmt.Errorf("multiple WhatsApp accounts (%s); pass -account", strings.Join(names, ", "))
}
