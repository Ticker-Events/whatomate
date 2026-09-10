package commerce

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testDraftRepository(t *testing.T) (*DraftRepository, *gorm.DB) {
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
	require.NoError(t, db.Exec(`CREATE TABLE commerce_draft_messages (
		id uuid PRIMARY KEY, created_at datetime, updated_at datetime, deleted_at datetime,
		organization_id uuid NOT NULL, draft_id uuid NOT NULL, whats_app_message_id text NOT NULL,
		UNIQUE (organization_id, whats_app_message_id)
	)`).Error)
	return NewDraftRepository(db), db
}

func newDraft() *models.CommerceDraft {
	return &models.CommerceDraft{
		OrganizationID:  uuid.New(),
		ContactID:       uuid.New(),
		WhatsAppAccount: "shop",
		StoreID:         "7",
		Cart: models.JSONB{
			"10": map[string]any{"qty": float64(2)},
		},
	}
}

func TestDraftPersistenceAndRecovery(t *testing.T) {
	repo, _ := testDraftRepository(t)
	draft := newDraft()
	require.NoError(t, repo.Create(draft))

	recovered, err := repo.LatestActive(draft.OrganizationID, draft.ContactID, draft.WhatsAppAccount, draft.StoreID)
	require.NoError(t, err)
	assert.Equal(t, draft.ID, recovered.ID)
	assert.EqualValues(t, 2, recovered.Cart["10"].(map[string]any)["qty"])
}

func TestDraftOptimisticVersion(t *testing.T) {
	repo, _ := testDraftRepository(t)
	draft := newDraft()
	require.NoError(t, repo.Create(draft))

	draft.Status = "pending_payment"
	require.NoError(t, repo.Save(draft, 1))
	assert.EqualValues(t, 2, draft.Version)

	draft.Status = "active"
	err := repo.Save(draft, 1)
	assert.ErrorIs(t, err, ErrDraftConflict)
}

func TestDraftDuplicateMessageAndSubmission(t *testing.T) {
	repo, _ := testDraftRepository(t)
	draft := newDraft()
	require.NoError(t, repo.Create(draft))

	require.NoError(t, repo.RecordMessage(draft.ID, draft.OrganizationID, 1, "wamid.1"))
	err := repo.RecordMessage(draft.ID, draft.OrganizationID, 2, "wamid.1")
	assert.ErrorIs(t, err, ErrDuplicateMessage)

	current, err := repo.Get(draft.ID, draft.OrganizationID)
	require.NoError(t, err)
	claimed, err := repo.ClaimSubmission(current.ID, current.OrganizationID, current.Version, "submit-123")
	require.NoError(t, err)
	assert.Equal(t, "submit-123", claimed.SubmissionKey)

	again, err := repo.ClaimSubmission(current.ID, current.OrganizationID, current.Version, "submit-456")
	assert.True(t, errors.Is(err, ErrAlreadySubmitted) || errors.Is(err, ErrDraftConflict))
	assert.Equal(t, "submit-123", again.SubmissionKey)
}

func TestDraftCreateReloadsConcurrentActiveOwner(t *testing.T) {
	repo, _ := testDraftRepository(t)
	first := newDraft()
	require.NoError(t, repo.Create(first))

	concurrent := newDraft()
	concurrent.OrganizationID = first.OrganizationID
	concurrent.ContactID = first.ContactID
	concurrent.WhatsAppAccount = first.WhatsAppAccount
	concurrent.StoreID = first.StoreID
	require.NoError(t, repo.Create(concurrent))
	assert.Equal(t, first.ID, concurrent.ID)
}

func TestCompleteSubmissionIsRecoverableAndIdempotent(t *testing.T) {
	repo, _ := testDraftRepository(t)
	draft := newDraft()
	require.NoError(t, repo.Create(draft))
	claimed, err := repo.ClaimSubmission(draft.ID, draft.OrganizationID, draft.Version, "submit-stable")
	require.NoError(t, err)

	recovered, err := repo.LatestActive(
		claimed.OrganizationID, claimed.ContactID, claimed.WhatsAppAccount, claimed.StoreID,
	)
	require.NoError(t, err)
	assert.Equal(t, claimed.ID, recovered.ID)
	assert.Equal(t, "submitting", recovered.Status)
	assert.Equal(t, "submit-stable", recovered.SubmissionKey)

	completed, err := repo.CompleteSubmission(
		claimed.ID, claimed.OrganizationID, claimed.SubmissionKey, "order-1", "payment-1",
	)
	require.NoError(t, err)
	assert.Equal(t, "pending_payment", completed.Status)
	assert.Equal(t, "order-1", completed.BackendOrderID)

	replayed, err := repo.CompleteSubmission(
		claimed.ID, claimed.OrganizationID, claimed.SubmissionKey, "order-1", "payment-1",
	)
	require.NoError(t, err)
	assert.Equal(t, completed.ID, replayed.ID)

	conflict, err := repo.CompleteSubmission(
		claimed.ID, claimed.OrganizationID, claimed.SubmissionKey, "order-2", "payment-2",
	)
	assert.ErrorIs(t, err, ErrDraftConflict)
	assert.Equal(t, "order-1", conflict.BackendOrderID)
}
