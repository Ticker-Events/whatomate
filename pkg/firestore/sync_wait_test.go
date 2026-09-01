package firestore

import (
	"testing"
	"time"

	gfirestore "cloud.google.com/go/firestore"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestApplyConversationWaitFields_IncludesClosedAndClock(t *testing.T) {
	clock := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	data := map[string]any{}
	applyConversationWaitFields(data, &models.Contact{
		IsClosed:           false,
		AwaitingReplySince: &clock,
	})

	assert.Equal(t, false, data["isClosed"])
	assert.Equal(t, clock.Format(time.RFC3339), data["awaitingReplySince"])
}

func TestApplyConversationWaitFields_DeletesNilClock(t *testing.T) {
	data := map[string]any{}
	applyConversationWaitFields(data, &models.Contact{IsClosed: true})

	assert.Equal(t, true, data["isClosed"])
	assert.Equal(t, gfirestore.Delete, data["awaitingReplySince"])
}

func TestApplyConversationWaitFields_NilContact(t *testing.T) {
	data := map[string]any{"keep": true}
	applyConversationWaitFields(data, nil)
	assert.Equal(t, true, data["keep"])
	assert.NotContains(t, data, "isClosed")
}
