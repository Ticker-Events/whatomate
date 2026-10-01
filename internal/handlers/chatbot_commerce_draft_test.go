package handlers

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
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
