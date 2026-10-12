package models

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatbotSettingsDecryptSecrets(t *testing.T) {
	const key = "this-is-a-32-character-test-key-XX"
	aiKey, err := crypto.Encrypt("ai-secret", key)
	require.NoError(t, err)
	mcpKey, err := crypto.Encrypt("mcp-secret", key)
	require.NoError(t, err)
	mapsKey, err := crypto.Encrypt("maps-secret", key)
	require.NoError(t, err)

	settings := ChatbotSettings{
		AI: AIConfig{
			APIKey:             aiKey,
			CommerceMCPAPIKey:  mcpKey,
			CommerceMapsAPIKey: mapsKey,
		},
	}
	settings.DecryptSecrets(key)

	assert.Equal(t, "ai-secret", settings.AI.APIKey)
	assert.Equal(t, "mcp-secret", settings.AI.CommerceMCPAPIKey)
	assert.Equal(t, "maps-secret", settings.AI.CommerceMapsAPIKey)
}
