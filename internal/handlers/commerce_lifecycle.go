package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	appcrypto "github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/shridarpatil/whatomate/internal/middleware"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	lifecycleContractVersion = "1"
	lifecycleTimestampSkew   = 5 * time.Minute
	lifecycleClaimTimeout    = 2 * time.Minute
)

var lifecycleEventMessages = map[string]string{
	"payment.succeeded": "Payment received for order %s. Thank you!",
	"payment.failed":    "Payment for order %s was unsuccessful. Please try again.",
	"order.confirmed":   "Order %s has been confirmed.",
	"order.assigned":    "Order %s has been assigned to our team.",
	"order.preparing":   "Order %s is now being prepared.",
	"order.ready":       "Order %s is ready.",
	"order.dispatched":  "Order %s is on its way.",
	"order.delivered":   "Order %s has been delivered.",
	"order.cancelled":   "Order %s has been cancelled.",
	"order.completed":   "Order %s is complete. Thank you!",
}

type commerceLifecycleEnvelope struct {
	Version        string                 `json:"version"`
	EventID        uuid.UUID              `json:"event_id"`
	EventType      string                 `json:"event_type"`
	OccurredAt     time.Time              `json:"occurred_at"`
	StoreID        int64                  `json:"store_id"`
	OrderID        string                 `json:"order_id"`
	OrderDisplayID string                 `json:"order_display_id,omitempty"`
	PaymentID      string                 `json:"payment_id,omitempty"`
	CorrelationID  uuid.UUID              `json:"correlation_id"`
	CausationID    *uuid.UUID             `json:"causation_id"`
	Sequence       *int64                 `json:"sequence,omitempty"`
	Capabilities   map[string]any         `json:"capabilities"`
	Conversation   *lifecycleConversation `json:"conversation,omitempty"`
	Data           map[string]any         `json:"data"`
}

type lifecycleConversation struct {
	PhoneNumber     string `json:"phone_number,omitempty"`
	WhatsAppAccount string `json:"whatsapp_account,omitempty"`
}

func canonicalLifecycleJSON(payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func lifecycleError(r *fastglue.Request, status int, code, message string, retryable bool, correlationID uuid.UUID) error {
	body := map[string]any{
		"version": lifecycleContractVersion,
		"error": map[string]any{
			"code":      code,
			"message":   message,
			"retryable": retryable,
			"details":   map[string]any{},
		},
	}
	if correlationID != uuid.Nil {
		body["correlation_id"] = correlationID.String()
	}
	encoded, _ := json.Marshal(body)
	r.RequestCtx.Response.Header.SetContentType("application/json")
	r.RequestCtx.SetStatusCode(status)
	r.RequestCtx.SetBody(encoded)
	return nil
}

// PutCommerceLifecycleConfig creates or rotates the write-only lifecycle
// signing secret for one Backend store/WhatsApp-account/API-key binding.
func (a *App) PutCommerceLifecycleConfig(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}
	if err := a.requirePermission(r, userID, models.ResourceSettingsChatbotAI, models.ActionWrite); err != nil {
		return nil
	}
	var req struct {
		StoreID         string    `json:"store_id"`
		WhatsAppAccount string    `json:"whatsapp_account"`
		APIKeyID        uuid.UUID `json:"api_key_id"`
		SigningSecret   *string   `json:"signing_secret"`
		Enabled         *bool     `json:"enabled"`
	}
	if err := json.Unmarshal(r.RequestCtx.PostBody(), &req); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid request body", nil, "")
	}
	req.StoreID = strings.TrimSpace(req.StoreID)
	req.WhatsAppAccount = strings.TrimSpace(req.WhatsAppAccount)
	if req.StoreID == "" || req.WhatsAppAccount == "" || req.APIKeyID == uuid.Nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "store_id, whatsapp_account and api_key_id are required", nil, "")
	}
	var account models.WhatsAppAccount
	if err := a.DB.Where("organization_id = ? AND name = ?", orgID, req.WhatsAppAccount).First(&account).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "WhatsApp account not found", nil, "")
	}
	var key models.APIKey
	if err := a.DB.Where("id = ? AND organization_id = ? AND is_active = ?", req.APIKeyID, orgID, true).First(&key).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "API key not found", nil, "")
	}
	var config models.CommerceLifecycleConfig
	result := a.DB.Where("organization_id = ? AND store_id = ? AND whats_app_account = ?", orgID, req.StoreID, req.WhatsAppAccount).First(&config)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		if req.SigningSecret == nil || strings.TrimSpace(*req.SigningSecret) == "" {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "signing_secret is required", nil, "")
		}
		config = models.CommerceLifecycleConfig{
			OrganizationID: orgID, StoreID: req.StoreID, WhatsAppAccount: req.WhatsAppAccount,
			APIKeyID: req.APIKeyID, Enabled: true,
		}
	} else if result.Error != nil {
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to load lifecycle configuration", nil, "")
	}
	config.APIKeyID = req.APIKeyID
	if req.Enabled != nil {
		config.Enabled = *req.Enabled
	}
	if req.SigningSecret != nil && strings.TrimSpace(*req.SigningSecret) != "" {
		encrypted, err := appcrypto.Encrypt(strings.TrimSpace(*req.SigningSecret), a.Config.App.EncryptionKey)
		if err != nil {
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to encrypt signing secret", nil, "")
		}
		config.SigningSecret = encrypted
	}
	if err := a.DB.Save(&config).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to save lifecycle configuration", nil, "")
	}
	return r.SendEnvelope(map[string]any{
		"store_id": config.StoreID, "whatsapp_account": config.WhatsAppAccount,
		"api_key_id": config.APIKeyID, "signing_secret_set": config.SigningSecret != "", "enabled": config.Enabled,
	})
}

func (a *App) GetCommerceLifecycleConfig(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}
	if err := a.requirePermission(r, userID, models.ResourceSettingsChatbotAI, models.ActionRead); err != nil {
		return nil
	}
	var configs []models.CommerceLifecycleConfig
	if err := a.DB.Where("organization_id = ?", orgID).Order("store_id, whats_app_account").Find(&configs).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to load lifecycle configuration", nil, "")
	}
	out := make([]map[string]any, 0, len(configs))
	for _, config := range configs {
		out = append(out, map[string]any{
			"id": config.ID, "store_id": config.StoreID, "whatsapp_account": config.WhatsAppAccount,
			"api_key_id": config.APIKeyID, "signing_secret_set": config.SigningSecret != "", "enabled": config.Enabled,
		})
	}
	return r.SendEnvelope(map[string]any{"configs": out})
}

// ListCommerceLifecycleEvents gives organization-scoped dead-letter/audit
// visibility without exposing payloads from another tenant.
func (a *App) ListCommerceLifecycleEvents(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}
	if err := a.requirePermission(r, userID, models.ResourceSettingsChatbotAI, models.ActionRead); err != nil {
		return nil
	}
	query := a.DB.Where("organization_id = ?", orgID)
	if status := strings.TrimSpace(string(r.RequestCtx.QueryArgs().Peek("status"))); status != "" {
		query = query.Where("status = ?", status)
	}
	var events []models.CommerceLifecycleEvent
	if err := query.Order("received_at DESC").Limit(200).Find(&events).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to load lifecycle events", nil, "")
	}
	return r.SendEnvelope(map[string]any{"events": events})
}

// ReceiveCommerceLifecycleEvent verifies, deduplicates, applies and dispatches
// one Backend-originated lifecycle event.
func (a *App) ReceiveCommerceLifecycleEvent(r *fastglue.Request) error {
	apiKeyID, ok := r.RequestCtx.UserValue(middleware.ContextKeyAPIKeyID).(uuid.UUID)
	if !ok || apiKeyID == uuid.Nil {
		return lifecycleError(r, fasthttp.StatusUnauthorized, "API_KEY_REQUIRED", "A valid API key is required", false, uuid.Nil)
	}
	orgID, err := a.getOrgID(r)
	if err != nil {
		return lifecycleError(r, fasthttp.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key", false, uuid.Nil)
	}
	if contentType := strings.ToLower(strings.TrimSpace(strings.Split(string(r.RequestCtx.Request.Header.ContentType()), ";")[0])); contentType != "application/json" {
		return lifecycleError(r, fasthttp.StatusBadRequest, "INVALID_CONTENT_TYPE", "Content-Type must be application/json", false, uuid.Nil)
	}
	var payload commerceLifecycleEnvelope
	decoder := json.NewDecoder(bytes.NewReader(r.RequestCtx.PostBody()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return lifecycleError(r, fasthttp.StatusBadRequest, "INVALID_BODY", "Invalid lifecycle event body", false, uuid.Nil)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return lifecycleError(r, fasthttp.StatusBadRequest, "INVALID_BODY", "Invalid lifecycle event body", false, payload.CorrelationID)
	}
	if err := validateLifecycleEnvelope(payload); err != nil {
		return lifecycleError(r, fasthttp.StatusBadRequest, "INVALID_EVENT", err.Error(), false, payload.CorrelationID)
	}
	eventHeader, eventErr := uuid.Parse(strings.TrimSpace(string(r.RequestCtx.Request.Header.Peek("X-Event-ID"))))
	correlationHeader, correlationErr := uuid.Parse(strings.TrimSpace(string(r.RequestCtx.Request.Header.Peek("X-Correlation-ID"))))
	idempotencyKey := strings.TrimSpace(string(r.RequestCtx.Request.Header.Peek("Idempotency-Key")))
	if eventErr != nil || correlationErr != nil || eventHeader != payload.EventID ||
		correlationHeader != payload.CorrelationID || idempotencyKey != payload.EventID.String() {
		return lifecycleError(r, fasthttp.StatusBadRequest, "HEADER_MISMATCH", "Event, correlation and idempotency headers must match the body", false, payload.CorrelationID)
	}
	timestamp, err := strconv.ParseInt(strings.TrimSpace(string(r.RequestCtx.Request.Header.Peek("X-TiQR-Timestamp"))), 10, 64)
	if err != nil || absDuration(time.Since(time.Unix(timestamp, 0))) > lifecycleTimestampSkew {
		return lifecycleError(r, fasthttp.StatusUnauthorized, "TIMESTAMP_OUT_OF_RANGE", "Request timestamp is outside the allowed tolerance", false, payload.CorrelationID)
	}
	accountName := ""
	if payload.Conversation != nil {
		accountName = strings.TrimSpace(payload.Conversation.WhatsAppAccount)
	}
	var config models.CommerceLifecycleConfig
	if err := a.DB.Where(
		"organization_id = ? AND store_id = ? AND whats_app_account = ? AND api_key_id = ? AND enabled = ?",
		orgID, strconv.FormatInt(payload.StoreID, 10), accountName, apiKeyID, true,
	).First(&config).Error; err != nil {
		return lifecycleError(r, fasthttp.StatusUnauthorized, "STORE_BINDING_MISMATCH", "API key is not authorized for this store and account", false, payload.CorrelationID)
	}
	secret, err := appcrypto.Decrypt(config.SigningSecret, a.Config.App.EncryptionKey)
	if err != nil || secret == "" {
		return lifecycleError(r, fasthttp.StatusUnauthorized, "SIGNATURE_CONFIG_ERROR", "Lifecycle signing secret is unavailable", false, payload.CorrelationID)
	}
	canonical, err := canonicalLifecycleJSON(payload)
	if err != nil || !validLifecycleSignature(secret, timestamp, canonical, string(r.RequestCtx.Request.Header.Peek("X-TiQR-Signature"))) {
		return lifecycleError(r, fasthttp.StatusUnauthorized, "INVALID_SIGNATURE", "Invalid lifecycle signature", false, payload.CorrelationID)
	}
	var canonicalMap models.JSONB
	if err := json.Unmarshal(canonical, &canonicalMap); err != nil {
		return lifecycleError(r, fasthttp.StatusBadRequest, "INVALID_BODY", "Invalid lifecycle event body", false, payload.CorrelationID)
	}
	event, duplicate, err := a.claimLifecycleEvent(orgID, config, payload, canonicalMap)
	if err != nil {
		return lifecycleError(r, fasthttp.StatusConflict, "EVENT_IN_PROGRESS", err.Error(), true, payload.CorrelationID)
	}
	if duplicate {
		return sendLifecycleAccepted(r, payload.EventID, true)
	}
	if err := a.processLifecycleEvent(event, payload); err != nil {
		_ = a.DB.Model(event).Updates(map[string]any{
			"status": models.CommerceLifecycleStatusFailed, "last_error": err.Error(), "processing_claimed": nil,
		}).Error
		return lifecycleError(r, fasthttp.StatusConflict, "PROCESSING_FAILED", "Event processing failed and may be retried", true, payload.CorrelationID)
	}
	return sendLifecycleAccepted(r, payload.EventID, false)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("trailing JSON value")
}

func validateLifecycleEnvelope(payload commerceLifecycleEnvelope) error {
	if payload.Version != lifecycleContractVersion {
		return errors.New("unsupported contract version")
	}
	if payload.EventID == uuid.Nil || payload.CorrelationID == uuid.Nil || payload.StoreID < 1 ||
		strings.TrimSpace(payload.OrderID) == "" || payload.OccurredAt.IsZero() {
		return errors.New("required lifecycle fields are missing")
	}
	if payload.Sequence != nil && *payload.Sequence < 1 {
		return errors.New("lifecycle sequence must be positive")
	}
	if _, ok := lifecycleEventMessages[payload.EventType]; !ok {
		return errors.New("unsupported lifecycle event type")
	}
	if payload.Capabilities == nil {
		return errors.New("capabilities is required")
	}
	if payload.Capabilities["contract_version"] != lifecycleContractVersion {
		return errors.New("capabilities contract version mismatch")
	}
	return nil
}

func validLifecycleSignature(secret string, timestamp int64, canonical []byte, supplied string) bool {
	if !strings.HasPrefix(supplied, "sha256=") {
		return false
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(supplied, "sha256="))
	if err != nil || len(raw) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	_, _ = mac.Write(canonical)
	return hmac.Equal(raw, mac.Sum(nil))
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func sendLifecycleAccepted(r *fastglue.Request, eventID uuid.UUID, duplicate bool) error {
	body, _ := json.Marshal(map[string]any{
		"version": lifecycleContractVersion, "event_id": eventID, "accepted": true, "duplicate": duplicate,
	})
	r.RequestCtx.Response.Header.SetContentType("application/json")
	r.RequestCtx.SetStatusCode(fasthttp.StatusAccepted)
	r.RequestCtx.SetBody(body)
	return nil
}

func (a *App) claimLifecycleEvent(
	orgID uuid.UUID,
	config models.CommerceLifecycleConfig,
	payload commerceLifecycleEnvelope,
	canonical models.JSONB,
) (*models.CommerceLifecycleEvent, bool, error) {
	event := &models.CommerceLifecycleEvent{}
	now := time.Now()
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("event_id = ?", payload.EventID).First(event)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			event = &models.CommerceLifecycleEvent{
				EventID: payload.EventID, OrganizationID: orgID, StoreID: config.StoreID,
				WhatsAppAccount: config.WhatsAppAccount, EventType: payload.EventType, Version: payload.Version,
				OrderID: payload.OrderID, PaymentID: payload.PaymentID, CorrelationID: payload.CorrelationID,
				Sequence: payload.Sequence, OccurredAt: payload.OccurredAt, ReceivedAt: now, CanonicalPayload: canonical,
				Status: models.CommerceLifecycleStatusProcessing, Attempts: 1, ProcessingClaimed: &now,
			}
			return tx.Create(event).Error
		}
		if result.Error != nil {
			return result.Error
		}
		if event.OrganizationID != orgID || event.StoreID != config.StoreID || event.EventType != payload.EventType ||
			event.OrderID != payload.OrderID || event.CorrelationID != payload.CorrelationID ||
			!reflect.DeepEqual(event.CanonicalPayload, canonical) {
			return errors.New("event ID was already used with different content")
		}
		if event.Status == models.CommerceLifecycleStatusProcessed ||
			event.Status == models.CommerceLifecycleStatusStale ||
			event.Status == models.CommerceLifecycleStatusDeadLetter ||
			event.Status == models.CommerceLifecycleStatusTerminal {
			return nil
		}
		if event.Status == models.CommerceLifecycleStatusProcessing && event.ProcessingClaimed != nil &&
			now.Sub(*event.ProcessingClaimed) < lifecycleClaimTimeout {
			return errors.New("event is already being processed")
		}
		return tx.Model(event).Updates(map[string]any{
			"status": models.CommerceLifecycleStatusProcessing, "attempts": gorm.Expr("attempts + 1"),
			"processing_claimed": now, "last_error": "",
		}).Error
	})
	if err != nil {
		return nil, false, err
	}
	return event, event.Status == models.CommerceLifecycleStatusProcessed ||
		event.Status == models.CommerceLifecycleStatusStale ||
		event.Status == models.CommerceLifecycleStatusDeadLetter ||
		event.Status == models.CommerceLifecycleStatusTerminal, nil
}

func (a *App) processLifecycleEvent(event *models.CommerceLifecycleEvent, payload commerceLifecycleEnvelope) error {
	var draft models.CommerceDraft
	if err := a.DB.Preload("Contact").Where(
		"organization_id = ? AND store_id = ? AND backend_order_id = ?",
		event.OrganizationID, event.StoreID, event.OrderID,
	).Order("created_at DESC").First(&draft).Error; err != nil {
		return fmt.Errorf("matching commerce draft not found: %w", err)
	}
	if draft.WhatsAppAccount != event.WhatsAppAccount {
		return errors.New("draft WhatsApp account does not match event binding")
	}
	if payload.Conversation != nil && strings.TrimSpace(payload.Conversation.PhoneNumber) != "" {
		eventPhone := strings.TrimPrefix(strings.TrimSpace(payload.Conversation.PhoneNumber), "+")
		if draft.Contact == nil || strings.TrimPrefix(strings.TrimSpace(draft.Contact.PhoneNumber), "+") != eventPhone {
			return errors.New("event phone number does not match draft contact")
		}
	}
	if draft.Contact == nil {
		return errors.New("draft contact not found")
	}
	stale, err := a.claimLifecyclePosition(event, &draft)
	if err != nil {
		return err
	}
	if stale {
		return nil
	}
	var account models.WhatsAppAccount
	if err := a.DB.Where("organization_id = ? AND name = ?", event.OrganizationID, event.WhatsAppAccount).First(&account).Error; err != nil {
		return fmt.Errorf("WhatsApp account not found: %w", err)
	}

	orderStatus, paymentStatus, draftStatus := lifecycleState(payload.EventType)
	displayID := payload.OrderDisplayID
	if displayID == "" {
		displayID = payload.OrderID
	}
	request := a.lifecycleMessageRequest(&account, draft.Contact, payload, fmt.Sprintf(lifecycleEventMessages[payload.EventType], displayID))
	request.LifecycleEventID = &event.EventID
	request.DurableSendKey = "lifecycle:" + event.EventID.String()
	msg, err := a.SendOutgoingMessage(context.Background(), request, SLASendOptions())
	if err != nil {
		return err
	}
	if msg != nil {
		var persisted models.Message
		if err := a.DB.First(&persisted, "id = ?", msg.ID).Error; err != nil {
			return fmt.Errorf("reload lifecycle message: %w", err)
		}
		msg = &persisted
	}
	if msg == nil || (msg.Status != models.MessageStatusSent &&
		msg.Status != models.MessageStatusDelivered && msg.Status != models.MessageStatusRead) {
		if msg != nil && msg.ErrorMessage != "" {
			return errors.New(msg.ErrorMessage)
		}
		return errors.New("lifecycle message was not durably marked sent")
	}
	now := time.Now()
	return a.DB.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{}
		if orderStatus != "" {
			updates["backend_order_status"] = orderStatus
		}
		if paymentStatus != "" {
			updates["backend_payment_status"] = paymentStatus
		}
		if draftStatus != "" {
			advanceDraftStatus := true
			if strings.HasPrefix(payload.EventType, "payment.") &&
				draft.Status != "active" && draft.Status != "submitting" && draft.Status != "pending_payment" {
				advanceDraftStatus = false
			}
			if advanceDraftStatus {
				updates["status"] = draftStatus
				if draftStatus != "active" && draftStatus != "submitting" && draftStatus != "pending_payment" {
					updates["active_owner_key"] = nil
				}
			}
		}
		if payload.PaymentID != "" {
			updates["backend_payment_id"] = payload.PaymentID
		}
		if err := tx.Model(&models.CommerceDraft{}).Where("id = ?", draft.ID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Model(event).Updates(map[string]any{
			"status": models.CommerceLifecycleStatusProcessed, "processed_at": now,
			"processing_claimed": nil, "last_error": "", "message_id": msg.ID,
		}).Error
	})
}

// claimLifecyclePosition serializes known events for an order and advances a
// monotonic cursor before any customer-visible side effect. A delayed event is
// durably recorded as stale and never regresses the draft or sends a message.
func (a *App) claimLifecyclePosition(event *models.CommerceLifecycleEvent, draft *models.CommerceDraft) (bool, error) {
	stale := false
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		var locked models.CommerceDraft
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND organization_id = ?", draft.ID, draft.OrganizationID).
			First(&locked).Error; err != nil {
			return err
		}
		if lifecycleWouldRegress(locked, event.EventType) {
			stale = true
			now := time.Now()
			return tx.Model(event).Updates(map[string]any{
				"status": models.CommerceLifecycleStatusStale, "processed_at": now,
				"processing_claimed": nil, "last_error": "lifecycle state regression",
			}).Error
		}
		if locked.LastLifecycleAt != nil {
			previous := models.CommerceLifecycleEvent{OccurredAt: *locked.LastLifecycleAt}
			if locked.LastLifecycleEventID != nil {
				previous.EventID = *locked.LastLifecycleEventID
				if err := tx.Where("event_id = ?", *locked.LastLifecycleEventID).First(&previous).Error; err != nil &&
					!errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
			}
			position := compareLifecyclePosition(event, &previous)
			if position < 0 {
				stale = true
				now := time.Now()
				return tx.Model(event).Updates(map[string]any{
					"status": models.CommerceLifecycleStatusStale, "processed_at": now,
					"processing_claimed": nil, "last_error": "stale lifecycle event",
				}).Error
			}
			if position == 0 && locked.LastLifecycleEventID != nil &&
				*locked.LastLifecycleEventID == event.EventID {
				return nil
			}
		}

		var retryableEvents []models.CommerceLifecycleEvent
		if err := tx.
			Where(
				"organization_id = ? AND store_id = ? AND order_id = ? AND event_id <> ? AND status IN ?",
				event.OrganizationID, event.StoreID, event.OrderID, event.EventID,
				[]string{
					models.CommerceLifecycleStatusPending,
					models.CommerceLifecycleStatusProcessing,
					models.CommerceLifecycleStatusFailed,
				},
			).Find(&retryableEvents).Error; err != nil {
			return err
		}
		for i := range retryableEvents {
			if compareLifecyclePosition(&retryableEvents[i], event) < 0 {
				return errors.New("an earlier retryable lifecycle event has not completed")
			}
		}
		eventID := event.EventID
		if err := tx.Model(&locked).Updates(map[string]any{
			"last_lifecycle_at": event.OccurredAt, "last_lifecycle_event_id": eventID,
		}).Error; err != nil {
			return err
		}
		draft.LastLifecycleAt = &event.OccurredAt
		draft.LastLifecycleEventID = &eventID
		return nil
	})
	return stale, err
}

func compareLifecyclePosition(left, right *models.CommerceLifecycleEvent) int {
	if left.Sequence != nil && right.Sequence != nil && *left.Sequence != *right.Sequence {
		if *left.Sequence < *right.Sequence {
			return -1
		}
		return 1
	}
	if !left.OccurredAt.Equal(right.OccurredAt) {
		if left.OccurredAt.Before(right.OccurredAt) {
			return -1
		}
		return 1
	}
	leftPrecedence := lifecycleEventPrecedence(left.EventType)
	rightPrecedence := lifecycleEventPrecedence(right.EventType)
	if leftPrecedence != rightPrecedence {
		if leftPrecedence < rightPrecedence {
			return -1
		}
		return 1
	}
	return strings.Compare(left.EventID.String(), right.EventID.String())
}

func lifecycleEventPrecedence(eventType string) int {
	switch eventType {
	case "payment.failed":
		return 0
	case "payment.succeeded":
		return 1
	case "order.confirmed":
		return 2
	case "order.assigned":
		return 3
	case "order.preparing":
		return 4
	case "order.ready":
		return 5
	case "order.dispatched":
		return 6
	case "order.delivered":
		return 7
	case "order.completed":
		return 8
	case "order.cancelled":
		return 9
	default:
		return -1
	}
}

func lifecycleWouldRegress(draft models.CommerceDraft, eventType string) bool {
	if strings.HasPrefix(eventType, "payment.") {
		if draft.BackendPaymentStatus == "succeeded" && eventType == "payment.failed" {
			return true
		}
		return eventType == "payment.failed" &&
			draft.BackendOrderStatus != "" && draft.BackendOrderStatus != "pending"
	}
	if !strings.HasPrefix(eventType, "order.") {
		return false
	}
	current := draft.BackendOrderStatus
	next, _, _ := lifecycleState(eventType)
	if current == "cancelled" || current == "completed" {
		return next != current
	}
	rank := map[string]int{
		"": 0, "pending": 0, "confirmed": 1, "assigned": 2, "preparing": 3,
		"ready": 4, "dispatched": 5, "delivered": 6, "completed": 7,
	}
	return next != "cancelled" && rank[next] < rank[current]
}

func lifecycleState(eventType string) (orderStatus, paymentStatus, draftStatus string) {
	switch eventType {
	case "payment.succeeded":
		return "", "succeeded", "confirmed"
	case "payment.failed":
		return "", "failed", "pending_payment"
	case "order.confirmed":
		return "confirmed", "", "confirmed"
	case "order.assigned":
		return "assigned", "", "assigned"
	case "order.preparing":
		return "preparing", "", "preparing"
	case "order.ready":
		return "ready", "", "ready"
	case "order.dispatched":
		return "dispatched", "", "dispatched"
	case "order.delivered":
		return "delivered", "", "delivered"
	case "order.cancelled":
		return "cancelled", "", "cancelled"
	case "order.completed":
		return "completed", "", "completed"
	default:
		return "", "", ""
	}
}

func (a *App) lifecycleMessageRequest(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	payload commerceLifecycleEnvelope,
	fallback string,
) OutgoingMessageRequest {
	templateName := "commerce_" + strings.ReplaceAll(payload.EventType, ".", "_")
	var template models.Template
	if err := a.DB.Where(
		"organization_id = ? AND whats_app_account = ? AND name = ? AND status = ?",
		account.OrganizationID, account.Name, templateName, "APPROVED",
	).First(&template).Error; err == nil {
		return OutgoingMessageRequest{
			Account: account, Contact: contact, Type: models.MessageTypeTemplate, Template: &template,
			BodyParams: map[string]string{"order_id": firstNonEmpty(payload.OrderDisplayID, payload.OrderID)},
		}
	}
	if url := lifecyclePaymentURL(payload.Data); url != "" &&
		(payload.EventType == "payment.failed" || payload.EventType == "order.confirmed") {
		return OutgoingMessageRequest{
			Account: account, Contact: contact, Type: models.MessageTypeInteractive,
			InteractiveType: "cta_url", BodyText: fallback, ButtonText: "Retry payment", URL: url,
		}
	}
	return OutgoingMessageRequest{Account: account, Contact: contact, Type: models.MessageTypeText, Content: fallback}
}

func lifecyclePaymentURL(data map[string]any) string {
	for _, key := range []string{"payment_url", "retry_url", "url_to_redirect"} {
		if value, ok := data[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
