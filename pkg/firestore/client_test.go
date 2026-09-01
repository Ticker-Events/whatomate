package firestore_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/firestore"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
)

func TestClient_DisabledWhenNil(t *testing.T) {
	var c *firestore.Client
	assert.False(t, c.Enabled())
}

func TestInit_EmptyCredentialsReturnsNil(t *testing.T) {
	c, err := firestore.Init(t.Context(), "", testutil.NopLogger())
	assert.NoError(t, err)
	assert.Nil(t, c)
}

func TestSyncMessage_NoOpWhenDisabled(t *testing.T) {
	c, err := firestore.Init(t.Context(), "", testutil.NopLogger())
	assert.NoError(t, err)

	orgID := uuid.New()
	contactID := uuid.New()
	now := time.Now()
	msg := &models.Message{
		BaseModel:       models.BaseModel{ID: uuid.New(), CreatedAt: now, UpdatedAt: now},
		OrganizationID:  orgID,
		ContactID:       contactID,
		Direction:       models.DirectionIncoming,
		MessageType:     models.MessageTypeText,
		Content:         "hello",
		Status:          models.MessageStatusReceived,
		WhatsAppAccount: "main",
	}
	contact := &models.Contact{
		BaseModel:       models.BaseModel{ID: contactID},
		OrganizationID:  orgID,
		PhoneNumber:     "+1234567890",
		ProfileName:     "Test",
		WhatsAppAccount: "main",
	}

	err = c.SyncMessage(t.Context(), orgID, msg, contact, false)
	assert.NoError(t, err)
}

func TestUpdateMessageStatus_NoOpWhenDisabled(t *testing.T) {
	c, err := firestore.Init(t.Context(), "", testutil.NopLogger())
	assert.NoError(t, err)

	err = c.UpdateMessageStatus(t.Context(), uuid.New(), models.MessageStatusSent, "")
	assert.NoError(t, err)
}

func TestResetContactUnread_NoOpWhenDisabled(t *testing.T) {
	c, err := firestore.Init(t.Context(), "", testutil.NopLogger())
	assert.NoError(t, err)

	err = c.ResetContactUnread(t.Context(), uuid.New(), uuid.New())
	assert.NoError(t, err)
}

func TestSyncContact_NoOpWhenDisabled(t *testing.T) {
	c, err := firestore.Init(t.Context(), "", testutil.NopLogger())
	assert.NoError(t, err)

	err = c.SyncContact(t.Context(), uuid.New(), &models.Contact{
		BaseModel: models.BaseModel{ID: uuid.New()},
		IsClosed:  true,
	}, false)
	assert.NoError(t, err)
}

func TestCreateCustomToken_ErrorWhenDisabled(t *testing.T) {
	c, err := firestore.Init(t.Context(), "", testutil.NopLogger())
	assert.NoError(t, err)

	_, err = c.CreateCustomToken(t.Context(), uuid.New(), uuid.New())
	assert.Error(t, err)
}
