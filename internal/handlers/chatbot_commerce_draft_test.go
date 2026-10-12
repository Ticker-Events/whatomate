package handlers

import (
	"testing"

	"github.com/google/uuid"
	draftrepo "github.com/shridarpatil/whatomate/internal/commerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/logf"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestJSONArrayFromSessionEmptyAddonListIsNotNil(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		SessionData: models.JSONB{"commerce_addons": []any{}},
	}
	addons := jsonArrayFromSession(session, "commerce_addons")
	assert.NotNil(t, addons)
	assert.Empty(t, addons)

	value, err := addons.Value()
	assert.NoError(t, err)
	assert.NotNil(t, value)
}

func draftTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE commerce_drafts (
		id uuid PRIMARY KEY, created_at datetime, updated_at datetime, deleted_at datetime,
		organization_id uuid NOT NULL, contact_id uuid NOT NULL, whats_app_account text NOT NULL,
		store_id text NOT NULL, status text NOT NULL, version integer NOT NULL, active_owner_key text UNIQUE,
		selected_category_id integer, cart jsonb, addons jsonb, captured_fields jsonb, attachments jsonb, notes jsonb,
		fulfillment_mode text, fulfillment_slot_token text, requested_fulfillment_at datetime,
		promised_ready_at datetime, saved_address_id integer, address_snapshot jsonb,
		latitude real, longitude real, backend_order_id text, backend_payment_id text,
		backend_order_status text, backend_payment_status text,
		submission_key text, transfer_id uuid, last_message_id text, abandoned_at datetime, reminder_at datetime,
		last_lifecycle_at datetime, last_lifecycle_event_id uuid,
		pending_payment_reminder_at datetime, abandoned_reminder_at datetime, reminder_claimed_at datetime,
		reminder_claim_token text, reminder_claim_kind text, reminder_attempts integer, reminder_last_error text
	)`).Error)
	return db
}

func TestEnsureCommerceDraftDoesNotHydratePreviousSession(t *testing.T) {
	db := draftTestDB(t)
	app := &App{DB: db, Log: logf.New(logf.Opts{Level: logf.ErrorLevel})}

	orgID := uuid.New()
	contactID := uuid.New()
	account := "shop"
	storeID := "7"

	repo := draftrepo.NewDraftRepository(db)
	old := &models.CommerceDraft{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  orgID,
		ContactID:       contactID,
		WhatsAppAccount: account,
		StoreID:         storeID,
		Cart:            models.JSONB{"10": map[string]any{"qty": float64(2)}},
		Addons:          models.JSONBArray{map[string]any{"addon": 9, "quantity": 1}},
		CapturedFields:  models.JSONB{"writing": "Happy Birthday"},
		Notes:           models.JSONB{"note": "old"},
	}
	require.NoError(t, repo.Create(old))

	contact := &models.Contact{BaseModel: models.BaseModel{ID: contactID}, OrganizationID: orgID}
	session := &models.ChatbotSession{
		OrganizationID:  orgID,
		ContactID:       contactID,
		WhatsAppAccount: account,
		SessionData:     models.JSONB{},
	}
	settings := &models.ChatbotSettings{AI: models.AIConfig{CommerceStoreID: storeID}}

	draft, err := app.ensureCommerceDraft(contact, session, settings)
	require.NoError(t, err)
	require.NotNil(t, draft)
	assert.NotEqual(t, old.ID, draft.ID)
	assert.Equal(t, draft.ID, commerceDraftID(session))
	assert.Nil(t, session.SessionData["commerce_captured_fields"])
	assert.Nil(t, session.SessionData["commerce_addons"])
	assert.Nil(t, session.SessionData[cartKey])
	assert.Empty(t, draft.CapturedFields)
	assert.Empty(t, draft.Addons)
	assert.Empty(t, draft.Cart)

	savedOld, err := repo.Get(old.ID, orgID)
	require.NoError(t, err)
	assert.Equal(t, "abandoned", savedOld.Status)
}

func TestRecoverCommerceDraftDoesNotAttachPriorDraft(t *testing.T) {
	db := draftTestDB(t)
	app := &App{DB: db, Log: logf.New(logf.Opts{Level: logf.ErrorLevel})}

	orgID := uuid.New()
	contactID := uuid.New()
	repo := draftrepo.NewDraftRepository(db)
	old := &models.CommerceDraft{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  orgID,
		ContactID:       contactID,
		WhatsAppAccount: "shop",
		StoreID:         "7",
		CapturedFields:  models.JSONB{"writing": "leak me"},
		Addons:          models.JSONBArray{map[string]any{"addon": 1, "quantity": 1}},
	}
	require.NoError(t, repo.Create(old))

	session := &models.ChatbotSession{
		OrganizationID:  orgID,
		ContactID:       contactID,
		WhatsAppAccount: "shop",
		SessionData:     models.JSONB{},
	}
	settings := &models.ChatbotSettings{AI: models.AIConfig{
		CommerceStoreID: "7", CommerceEnabled: true, CommerceMCPURL: "https://mcp.example",
	}}
	app.recoverCommerceDraft(&models.Contact{BaseModel: models.BaseModel{ID: contactID}}, session, settings)

	assert.Equal(t, uuid.Nil, commerceDraftID(session))
	assert.Nil(t, session.SessionData["commerce_captured_fields"])
	assert.Nil(t, session.SessionData["commerce_addons"])
}
