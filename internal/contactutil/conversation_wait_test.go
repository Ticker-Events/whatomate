package contactutil

import (
	"testing"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyConversationWait_InboundStartsClockWhenEmpty(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	contact := &models.Contact{}

	updates := ApplyConversationWait(contact, true, at)

	assert.False(t, contact.IsClosed)
	require.NotNil(t, contact.AwaitingReplySince)
	assert.True(t, at.Equal(*contact.AwaitingReplySince))
	assert.Equal(t, false, updates["is_closed"])
	assert.Equal(t, at, updates["awaiting_reply_since"])
}

func TestApplyConversationWait_InboundDoesNotResetExistingClock(t *testing.T) {
	started := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	later := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	contact := &models.Contact{AwaitingReplySince: &started}

	updates := ApplyConversationWait(contact, true, later)

	assert.False(t, contact.IsClosed)
	require.NotNil(t, contact.AwaitingReplySince)
	assert.True(t, started.Equal(*contact.AwaitingReplySince))
	assert.Equal(t, map[string]any{"is_closed": false}, updates)
}

func TestApplyConversationWait_InboundOnClosedStartsFreshClock(t *testing.T) {
	oldClock := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	contact := &models.Contact{IsClosed: true, AwaitingReplySince: &oldClock}

	updates := ApplyConversationWait(contact, true, at)

	assert.False(t, contact.IsClosed)
	require.NotNil(t, contact.AwaitingReplySince)
	assert.True(t, at.Equal(*contact.AwaitingReplySince))
	assert.Equal(t, false, updates["is_closed"])
	assert.Equal(t, at, updates["awaiting_reply_since"])
}

func TestApplyConversationWait_OutboundClosesAndClearsClock(t *testing.T) {
	started := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	contact := &models.Contact{AwaitingReplySince: &started}

	updates := ApplyConversationWait(contact, false, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))

	assert.True(t, contact.IsClosed)
	assert.Nil(t, contact.AwaitingReplySince)
	assert.Equal(t, true, updates["is_closed"])
	assert.Nil(t, updates["awaiting_reply_since"])
}

func TestApplyConversationWait_NilContact(t *testing.T) {
	assert.Empty(t, ApplyConversationWait(nil, true, time.Now()))
}
