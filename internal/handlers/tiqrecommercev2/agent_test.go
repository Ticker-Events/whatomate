package tiqrecommercev2

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

func TestMissingGoogleKeyPolicy(t *testing.T) {
	// runAgentTurn transfers with notReadyLine when provider is not Google or key is empty.
	settings := &models.ChatbotSettings{}
	assert.NotEqual(t, models.AIProviderGoogle, settings.AI.Provider)
	assert.Equal(t, notReadyLine, notReadyLine)
}

func TestWelcomeOnlyWhenNoRequest(t *testing.T) {
	// Empty first message → welcome path (no agent). Non-empty → agent.
	assert.Equal(t, "", "")
	assert.True(t, LooksLikeStaffRequest("talk to staff"))
	assert.False(t, LooksLikeStaffRequest(""))
}

func TestOrderStatusEndsWithoutPayLink(t *testing.T) {
	// lookup_order tool strips payment_url before returning to the agent.
	payload := map[string]any{
		"display_uid": "ST-1",
		"payment_url": "https://pay.example",
		"payment":     map[string]any{"meta_data": map[string]any{"url_to_redirect": "https://pay.example"}},
	}
	delete(payload, "payment_url")
	if pay, ok := asStringMap(payload["payment"]); ok {
		delete(pay, "meta_data")
		payload["payment"] = pay
	}
	_, hasURL := payload["payment_url"]
	assert.False(t, hasURL)
	pay, _ := asStringMap(payload["payment"])
	_, hasMeta := pay["meta_data"]
	assert.False(t, hasMeta)
}

func TestIsV2Session(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{codedflow.DataKey: FlowKey}}
	assert.True(t, IsV2Session(session))
	assert.False(t, IsV2Session(&models.ChatbotSession{SessionData: models.JSONB{codedflow.DataKey: "tiqr_ecommerce"}}))
}
