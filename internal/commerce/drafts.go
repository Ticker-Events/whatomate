package commerce

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDraftConflict    = errors.New("commerce draft version conflict")
	ErrDuplicateMessage = errors.New("commerce draft message already processed")
	ErrAlreadySubmitted = errors.New("commerce draft already submitted")
)

type DraftRepository struct {
	db *gorm.DB
}

func NewDraftRepository(db *gorm.DB) *DraftRepository { return &DraftRepository{db: db} }

func activeOwnerKey(draft *models.CommerceDraft) *string {
	if draft == nil {
		return nil
	}
	switch draft.Status {
	case "active", "submitting", "pending_payment":
	default:
		return nil
	}
	owner := fmt.Sprintf("%s\x00%s\x00%s\x00%s",
		draft.OrganizationID, draft.ContactID, draft.WhatsAppAccount, draft.StoreID)
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(owner)))
	return &key
}

func (r *DraftRepository) Create(draft *models.CommerceDraft) error {
	if r == nil || r.db == nil || draft == nil {
		return errors.New("commerce draft repository is not configured")
	}
	if draft.ID == uuid.Nil {
		draft.ID = uuid.New()
	}
	if draft.Version == 0 {
		draft.Version = 1
	}
	if draft.Status == "" {
		draft.Status = "active"
	}
	if draft.Cart == nil {
		draft.Cart = models.JSONB{}
	}
	if draft.Addons == nil {
		draft.Addons = models.JSONBArray{}
	}
	if draft.CapturedFields == nil {
		draft.CapturedFields = models.JSONB{}
	}
	if draft.Attachments == nil {
		draft.Attachments = models.JSONBArray{}
	}
	if draft.Notes == nil {
		draft.Notes = models.JSONB{}
	}
	if draft.AddressSnapshot == nil {
		draft.AddressSnapshot = models.JSONB{}
	}
	draft.ActiveOwnerKey = activeOwnerKey(draft)
	if err := r.db.Create(draft).Error; err != nil {
		// A concurrent creator may have won the unique active-owner key.
		// Reload that durable aggregate instead of creating another.
		if draft.ActiveOwnerKey != nil {
			existing, loadErr := r.LatestActive(
				draft.OrganizationID, draft.ContactID, draft.WhatsAppAccount, draft.StoreID,
			)
			if loadErr == nil {
				*draft = *existing
				return nil
			}
		}
		return err
	}
	return nil
}

func (r *DraftRepository) Get(id, organizationID uuid.UUID) (*models.CommerceDraft, error) {
	var draft models.CommerceDraft
	err := r.db.Where("id = ? AND organization_id = ?", id, organizationID).First(&draft).Error
	return &draft, err
}

func (r *DraftRepository) LatestActive(organizationID, contactID uuid.UUID, account, storeID string) (*models.CommerceDraft, error) {
	var draft models.CommerceDraft
	err := r.db.Where(
		"organization_id = ? AND contact_id = ? AND whats_app_account = ? AND store_id = ? AND status IN ?",
		organizationID, contactID, account, storeID, []string{"active", "submitting", "pending_payment"},
	).Order("updated_at DESC").First(&draft).Error
	return &draft, err
}

// Save performs an optimistic update. Callers must supply the version they read.
func (r *DraftRepository) Save(draft *models.CommerceDraft, expectedVersion int64) error {
	if draft == nil || expectedVersion < 1 {
		return ErrDraftConflict
	}
	draft.ActiveOwnerKey = activeOwnerKey(draft)
	draft.Version = expectedVersion + 1
	result := r.db.Model(&models.CommerceDraft{}).
		Where("id = ? AND organization_id = ? AND version = ?", draft.ID, draft.OrganizationID, expectedVersion).
		Select("*").Omit("id", "created_at", "deleted_at").
		Updates(draft)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		draft.Version = expectedVersion
		return ErrDraftConflict
	}
	return nil
}

// RecordMessage atomically protects state transitions from webhook redelivery.
func (r *DraftRepository) RecordMessage(id, organizationID uuid.UUID, expectedVersion int64, messageID string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		receipt := models.CommerceDraftMessage{
			BaseModel:         models.BaseModel{ID: uuid.New()},
			OrganizationID:    organizationID,
			DraftID:           id,
			WhatsAppMessageID: messageID,
		}
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&receipt)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return ErrDuplicateMessage
		}
		result := tx.Model(&models.CommerceDraft{}).
			Where("id = ? AND organization_id = ? AND version = ?", id, organizationID, expectedVersion).
			Updates(map[string]any{"last_message_id": messageID, "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDraftConflict
		}
		return nil
	})
}

// ClaimSubmission creates one stable key and transitions exactly once to
// submitting. Retries return the already persisted key/result instead.
func (r *DraftRepository) ClaimSubmission(id, organizationID uuid.UUID, expectedVersion int64, key string) (*models.CommerceDraft, error) {
	key = strings.TrimSpace(key)
	if len(key) < 8 {
		return nil, fmt.Errorf("submission idempotency key must contain at least 8 characters")
	}
	result := r.db.Model(&models.CommerceDraft{}).
		Where("id = ? AND organization_id = ? AND version = ? AND status = ?", id, organizationID, expectedVersion, "active").
		Updates(map[string]any{
			"submission_key": key,
			"status":         "submitting",
			"version":        gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	draft, err := r.Get(id, organizationID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected == 1 {
		return draft, nil
	}
	if draft.SubmissionKey != "" {
		return draft, ErrAlreadySubmitted
	}
	return draft, ErrDraftConflict
}

// CompleteSubmission durably stores the backend result using the stable
// submission key. It is safe to repeat after the backend idempotently returns
// an already-created order.
func (r *DraftRepository) CompleteSubmission(
	id, organizationID uuid.UUID,
	key, orderID, paymentID string,
) (*models.CommerceDraft, error) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(orderID) == "" {
		return nil, errors.New("submission key and backend order ID are required")
	}
	result := r.db.Model(&models.CommerceDraft{}).
		Where(
			"id = ? AND organization_id = ? AND submission_key = ? AND status IN ? AND (backend_order_id = ? OR backend_order_id = ?)",
			id, organizationID, key, []string{"submitting", "pending_payment"}, "", orderID,
		).
		Updates(map[string]any{
			"status":             "pending_payment",
			"backend_order_id":   orderID,
			"backend_payment_id": paymentID,
			"version":            gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	draft, err := r.Get(id, organizationID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected == 1 ||
		(draft.SubmissionKey == key && draft.BackendOrderID == orderID && draft.Status == "pending_payment") {
		return draft, nil
	}
	return draft, ErrDraftConflict
}
