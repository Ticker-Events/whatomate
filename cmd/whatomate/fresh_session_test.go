package main

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testFreshSessionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	stmts := []string{
		`CREATE TABLE contacts (
			id text PRIMARY KEY, created_at datetime, updated_at datetime, deleted_at datetime,
			organization_id text NOT NULL, phone_number text NOT NULL, profile_name text,
			whats_app_account text, assigned_user_id text, last_message_at datetime,
			last_message_preview text, is_read integer, tags text, metadata text,
			last_inbound_at datetime, is_closed integer, awaiting_reply_since datetime,
			marketing_opt_out integer, bs_uid text, chatbot_last_message_at datetime, chatbot_reminder_sent integer
		)`,
		`CREATE TABLE whatsapp_accounts (
			id text PRIMARY KEY, created_at datetime, updated_at datetime, deleted_at datetime,
			organization_id text NOT NULL, name text NOT NULL, app_id text, phone_id text,
			business_id text, access_token text, app_secret text, webhook_verify_token text,
			api_version text, is_default_incoming integer, is_default_outgoing integer,
			auto_read_receipt integer, business_calling_enabled integer, status text,
			created_by_id text, updated_by_id text, provider text, ai_sensy_email text,
			ai_sensy_password text, ai_sensy_project_id text, ai_sensy_token text
		)`,
		`CREATE TABLE chatbot_sessions (
			id text PRIMARY KEY, created_at datetime, updated_at datetime, deleted_at datetime,
			organization_id text NOT NULL, contact_id text NOT NULL, whats_app_account text NOT NULL,
			phone_number text NOT NULL, status text, current_flow_id text, current_step text,
			step_retries integer, session_data text, started_at datetime, last_activity_at datetime,
			completed_at datetime
		)`,
		`CREATE TABLE chatbot_session_messages (
			id text PRIMARY KEY, created_at datetime, updated_at datetime, deleted_at datetime,
			session_id text NOT NULL, message_id text, direction text NOT NULL, message text,
			step_name text, attachments text
		)`,
		`CREATE TABLE agent_transfers (
			id text PRIMARY KEY, created_at datetime, updated_at datetime, deleted_at datetime,
			organization_id text NOT NULL, contact_id text NOT NULL, whats_app_account text NOT NULL,
			phone_number text NOT NULL, status text, source text, agent_id text, team_id text,
			transferred_by_user_id text, notes text, metadata text, commerce_draft_id text,
			transferred_at datetime, resumed_at datetime, resumed_by text,
			sla_response_deadline datetime, sla_resolution_deadline datetime, sla_escalation_at datetime,
			expires_at datetime, picked_up_at datetime, first_response_at datetime,
			escalation_level integer, escalated_at datetime, sla_breached integer, sla_breached_at datetime
		)`,
		`CREATE TABLE commerce_drafts (
			id text PRIMARY KEY, created_at datetime, updated_at datetime, deleted_at datetime,
			organization_id text NOT NULL, contact_id text NOT NULL, whats_app_account text NOT NULL,
			store_id text NOT NULL, status text NOT NULL, version integer NOT NULL, active_owner_key text,
			selected_category_id integer, cart text, addons text, captured_fields text,
			attachments text, notes text, fulfillment_mode text, fulfillment_slot_token text,
			requested_fulfillment_at datetime, promised_ready_at datetime, saved_address_id integer,
			address_snapshot text, latitude real, longitude real, backend_order_id text,
			backend_payment_id text, backend_order_status text, backend_payment_status text,
			submission_key text, transfer_id text, last_message_id text, abandoned_at datetime,
			reminder_at datetime, last_lifecycle_at datetime, last_lifecycle_event_id text,
			pending_payment_reminder_at datetime, abandoned_reminder_at datetime,
			reminder_claimed_at datetime, reminder_claim_token text, reminder_claim_kind text,
			reminder_attempts integer, reminder_last_error text
		)`,
	}
	for _, stmt := range stmts {
		require.NoError(t, db.Exec(stmt).Error)
	}
	return db
}

func TestStartFreshSession_CancelsHistoryAndCreatesEmptySession(t *testing.T) {
	db := testFreshSessionDB(t)
	orgID := uuid.New()
	contactID := uuid.New()
	oldSessionID := uuid.New()
	now := time.Now()

	require.NoError(t, db.Create(&models.Contact{
		BaseModel:       models.BaseModel{ID: contactID},
		OrganizationID:  orgID,
		PhoneNumber:     "919876543210",
		WhatsAppAccount: "shop",
	}).Error)
	require.NoError(t, db.Create(&models.WhatsAppAccount{
		BaseModel:      models.BaseModel{ID: uuid.New()},
		OrganizationID: orgID,
		Name:           "shop",
		PhoneID:        "1",
		BusinessID:     "1",
		AccessToken:    "token",
	}).Error)

	oldSession := models.ChatbotSession{
		BaseModel:       models.BaseModel{ID: oldSessionID},
		OrganizationID:  orgID,
		ContactID:       contactID,
		WhatsAppAccount: "shop",
		PhoneNumber:     "919876543210",
		Status:          models.SessionStatusActive,
		SessionData:     models.JSONB{"commerce_cart": map[string]any{"1": 1}},
		StartedAt:       now.Add(-time.Hour),
		LastActivityAt:  now,
	}
	require.NoError(t, db.Create(&oldSession).Error)
	require.NoError(t, db.Create(&models.ChatbotSessionMessage{
		BaseModel: models.BaseModel{ID: uuid.New()},
		SessionID: oldSessionID,
		Direction: models.DirectionIncoming,
		Message:   "old polluted message",
	}).Error)
	require.NoError(t, db.Create(&models.AgentTransfer{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  orgID,
		ContactID:       contactID,
		PhoneNumber:     "919876543210",
		WhatsAppAccount: "shop",
		Status:          models.TransferStatusActive,
		TransferredAt:   now,
		Metadata:        models.JSONB{},
	}).Error)
	require.NoError(t, db.Create(&models.CommerceDraft{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  orgID,
		ContactID:       contactID,
		WhatsAppAccount: "shop",
		StoreID:         "1",
		Status:          "active",
		Version:         1,
		Cart:            models.JSONB{"10": map[string]any{"qty": 1}},
	}).Error)

	result, err := startFreshSession(db, "919876543210", nil, "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.CancelledSessions)
	assert.Equal(t, int64(1), result.ResumedTransfers)
	assert.Equal(t, int64(1), result.AbandonedDrafts)
	assert.NotEqual(t, oldSessionID, result.NewSessionID)

	var cancelled models.ChatbotSession
	require.NoError(t, db.First(&cancelled, "id = ?", oldSessionID).Error)
	assert.Equal(t, models.SessionStatusCancelled, cancelled.Status)

	var fresh models.ChatbotSession
	require.NoError(t, db.First(&fresh, "id = ?", result.NewSessionID).Error)
	assert.Equal(t, models.SessionStatusActive, fresh.Status)
	assert.Empty(t, fresh.SessionData)

	var freshMsgs int64
	require.NoError(t, db.Model(&models.ChatbotSessionMessage{}).Where("session_id = ?", fresh.ID).Count(&freshMsgs).Error)
	assert.Equal(t, int64(0), freshMsgs)

	var transfer models.AgentTransfer
	require.NoError(t, db.Where("contact_id = ?", contactID).First(&transfer).Error)
	assert.Equal(t, models.TransferStatusResumed, transfer.Status)

	var draft models.CommerceDraft
	require.NoError(t, db.Where("contact_id = ?", contactID).First(&draft).Error)
	assert.Equal(t, "abandoned", draft.Status)
}

func TestStartFreshSession_RequiresOrgWhenAmbiguous(t *testing.T) {
	db := testFreshSessionDB(t)
	phone := "919111111111"
	require.NoError(t, db.Create(&models.Contact{
		BaseModel: models.BaseModel{ID: uuid.New()}, OrganizationID: uuid.New(), PhoneNumber: phone,
	}).Error)
	require.NoError(t, db.Create(&models.Contact{
		BaseModel: models.BaseModel{ID: uuid.New()}, OrganizationID: uuid.New(), PhoneNumber: phone,
	}).Error)

	_, err := startFreshSession(db, phone, nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pass -org")
}

func TestNormalizeCLIPhone(t *testing.T) {
	assert.Equal(t, "919876543210", normalizeCLIPhone("+919876543210"))
	assert.Equal(t, "919876543210", normalizeCLIPhone(" 919876543210 "))
}
