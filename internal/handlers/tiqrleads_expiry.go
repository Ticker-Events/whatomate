package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrleads"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
)

// flushExpiredLeadsSessions finds timed-out tiqr_leads sessions and posts a
// partial unclassified lead when answers were collected.
func (p *SLAProcessor) flushExpiredLeadsSessions() {
	if p == nil || p.app == nil || p.app.DB == nil {
		return
	}
	var sessions []models.ChatbotSession
	err := p.app.DB.Where(
		"status = ? AND session_data ->> '_coded_flow' = ?",
		models.SessionStatusActive,
		tiqrleads.FlowKey,
	).Limit(200).Find(&sessions).Error
	if err != nil {
		p.app.Log.Error("Failed to load leads sessions for expiry flush", "error", err)
		return
	}
	now := time.Now()
	for i := range sessions {
		session := &sessions[i]
		timeoutMins := p.app.sessionTimeoutMins(session.OrganizationID, session.WhatsAppAccount)
		cutoff := now.Add(-time.Duration(timeoutMins) * time.Minute)
		if session.LastActivityAt.After(cutoff) {
			continue
		}
		p.app.saveExpiredLeadsSession(session)
	}
}

func (a *App) sessionTimeoutMins(orgID uuid.UUID, accountName string) int {
	timeoutMins := 30
	settings, err := a.getChatbotSettingsCached(orgID, accountName)
	if err == nil && settings != nil && settings.SessionTimeoutMins > 0 {
		timeoutMins = settings.SessionTimeoutMins
	}
	return timeoutMins
}

// flushTimedOutLeadsSessionForContact saves a partial lead when this contact's
// previous tiqr_leads session has gone idle past the timeout (race with SLA).
func (a *App) flushTimedOutLeadsSessionForContact(orgID, contactID uuid.UUID, accountName string, timeoutMins int) {
	if a == nil || a.DB == nil {
		return
	}
	if timeoutMins <= 0 {
		timeoutMins = 30
	}
	cutoff := time.Now().Add(-time.Duration(timeoutMins) * time.Minute)
	var session models.ChatbotSession
	err := a.DB.Where(
		"organization_id = ? AND contact_id = ? AND whats_app_account = ? AND status = ? AND last_activity_at <= ? AND session_data ->> '_coded_flow' = ?",
		orgID, contactID, accountName, models.SessionStatusActive, cutoff, tiqrleads.FlowKey,
	).Order("last_activity_at DESC").First(&session).Error
	if err != nil {
		return
	}
	a.saveExpiredLeadsSession(&session)
}

// saveExpiredLeadsSession posts create_lead for a timed-out partial session and
// marks the session timeout. Returns true when the lead was saved (or already saved).
func (a *App) saveExpiredLeadsSession(session *models.ChatbotSession) bool {
	if a == nil || session == nil {
		return false
	}
	if !tiqrleads.IsLeadsFlowSession(session) {
		return false
	}
	if !tiqrleads.HasCollectableAnswers(session) {
		// Still mark timed out so it stops matching active session queries.
		a.markLeadsSessionTimedOut(session)
		return false
	}
	params, ok := tiqrleads.ExpiryCreateLeadParams(session)
	if !ok {
		a.markLeadsSessionTimedOut(session)
		return false
	}

	settings, err := a.getChatbotSettingsCached(session.OrganizationID, session.WhatsAppAccount)
	if err != nil || settings == nil || !commerceRESTConfigured(settings.AI) {
		a.Log.Warn("leads expiry save skipped; commerce REST unavailable",
			"session", session.ID, "error", err)
		return false
	}
	storeID := strings.TrimSpace(settings.AI.CommerceStoreID)
	if storeID == "" {
		a.Log.Warn("leads expiry save skipped; missing store id", "session", session.ID)
		return false
	}

	client := newTiqrStoreRESTClient(settings.AI.CommerceRESTURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := ticker.CreateLeadRequest{
		SessionID:        params["session_id"],
		PhoneNumber:      params["phone_number"],
		Name:             params["name"],
		EvaluationMode:   params["evaluation_mode"],
		CompletionAction: params["completion_action"],
		Qualified:        false,
	}
	if raw := params["answers_json"]; raw != "" {
		var answers []any
		if err := json.Unmarshal([]byte(raw), &answers); err != nil {
			a.Log.Error("leads expiry invalid answers_json", "session", session.ID, "error", err)
			return false
		}
		body.Answers = answers
	}
	if raw := params["criteria_snapshot"]; raw != "" {
		snap := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &snap); err != nil {
			a.Log.Error("leads expiry invalid criteria_snapshot", "session", session.ID, "error", err)
			return false
		}
		body.CriteriaSnapshot = snap
	}

	if _, err := client.CreateLead(ctx, storeID, body); err != nil {
		a.Log.Error("leads expiry create_lead failed", "session", session.ID, "error", err)
		return false
	}

	tiqrleads.MarkLeadSavedOnSession(session)
	a.markLeadsSessionTimedOut(session)
	a.Log.Info("Saved unclassified lead on session timeout",
		"session", session.ID,
		"phone", session.PhoneNumber,
	)
	return true
}

func (a *App) markLeadsSessionTimedOut(session *models.ChatbotSession) {
	if a == nil || a.DB == nil || session == nil {
		return
	}
	now := time.Now()
	session.Status = models.SessionStatusTimeout
	session.CompletedAt = &now
	updates := map[string]any{
		"status":       models.SessionStatusTimeout,
		"completed_at": now,
		"session_data": session.SessionData,
	}
	if err := a.DB.Model(session).Updates(updates).Error; err != nil {
		a.Log.Error("Failed to mark leads session timed out", "session", session.ID, "error", err)
	}
}
