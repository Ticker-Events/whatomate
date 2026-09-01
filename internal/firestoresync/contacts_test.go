package firestoresync

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContactDocument_IncludesClosedAndClock(t *testing.T) {
	id := uuid.New()
	orgID := uuid.New()
	clock := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	lastMessage := clock.Add(time.Minute)
	contact := &models.Contact{
		BaseModel:          models.BaseModel{ID: id},
		OrganizationID:     orgID,
		PhoneNumber:        "919800000001",
		ProfileName:        "Ada",
		WhatsAppAccount:    "primary",
		LastMessageAt:      &lastMessage,
		LastMessagePreview: "hello",
		IsRead:             false,
		IsClosed:           false,
		AwaitingReplySince: &clock,
	}

	data := ContactDocument(contact)

	assert.Equal(t, id.String(), data["id"])
	assert.Equal(t, "919800000001", data["phone_number"])
	assert.Equal(t, "Ada", data["name"])
	assert.Equal(t, "Ada", data["profile_name"])
	assert.Equal(t, false, data["is_closed"])
	assert.Equal(t, clock, data["awaiting_reply_since"])
	assert.Equal(t, lastMessage, data["last_message_at"])
}

func TestContactDocument_OmitsNilClock(t *testing.T) {
	contact := &models.Contact{
		BaseModel: models.BaseModel{ID: uuid.New()},
		IsClosed:  true,
	}

	data := ContactDocument(contact)

	assert.Equal(t, true, data["is_closed"])
	_, hasClock := data["awaiting_reply_since"]
	assert.False(t, hasClock)
}

func TestContactDocument_NilContact(t *testing.T) {
	require.Empty(t, ContactDocument(nil))
}

func TestNew_DisabledWhenProjectIDEmpty(t *testing.T) {
	store, err := New(context.Background(), config.FirebaseConfig{})
	require.NoError(t, err)
	assert.Nil(t, store)
}
