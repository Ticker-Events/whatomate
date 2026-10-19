package handlers

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodedRoleAccountSettings_UsesRoleSystemPromptAndHistory(t *testing.T) {
	settings := &models.ChatbotSettings{}
	settings.AI.IncludeHistory = false
	settings.AI.SystemPrompt = "You are a chatty store assistant."
	settings.AI.Provider = models.AIProviderGoogle
	settings.AI.APIKey = "secret"

	got := codedRoleAccountSettings(settings)
	require.NotNil(t, got)
	assert.True(t, got.AI.IncludeHistory)
	assert.Equal(t, codedRoleSystemPrompt, got.AI.SystemPrompt)
	assert.True(t, skipCodedRoleAIMedia(got))
	assert.False(t, skipCodedRoleAIMedia(settings))
	assert.Equal(t, models.AIProviderGoogle, got.AI.Provider)
	assert.Equal(t, "secret", got.AI.APIKey)

	// Original settings must stay unchanged for normal chat AI.
	assert.False(t, settings.AI.IncludeHistory)
	assert.Equal(t, "You are a chatty store assistant.", settings.AI.SystemPrompt)
}

func TestCodedRoleAccountSettings_Nil(t *testing.T) {
	assert.Nil(t, codedRoleAccountSettings(nil))
}
