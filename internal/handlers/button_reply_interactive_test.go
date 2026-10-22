package handlers

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestButtonReplyInteractiveData_CarouselCardSnapshot(t *testing.T) {
	t.Parallel()

	carousel := &models.Message{
		InteractiveData: models.JSONB{
			"type": "carousel",
			"body": "Here is what's available",
			"cards": []any{
				map[string]any{
					"media_type": "image",
					"media_url":  "https://cdn.example.com/blue.jpeg",
					"body":       "Blue Echeveria\nA rosette succulent",
					"buttons": []any{
						map[string]any{"id": "buy-blue", "title": "Add to cart"},
					},
				},
				map[string]any{
					"media_type": "image",
					"media_url":  "https://cdn.example.com/zebra.jpeg",
					"body":       "Zebra Haworthia",
					"buttons": []any{
						map[string]any{"id": "buy-zebra", "title": "Add to cart"},
					},
				},
			},
		},
	}

	got := buttonReplyInteractiveData("Add to cart", "buy-blue", carousel)
	require.NotNil(t, got)
	assert.Equal(t, "buy-blue", got["button_id"])
	assert.Equal(t, "Add to cart", got["title"])
	assert.Equal(t, "Blue Echeveria", got["card_body"])
	assert.Equal(t, "https://cdn.example.com/blue.jpeg", got["card_media_url"])
}

func TestButtonReplyInteractiveData_NoCarousel(t *testing.T) {
	t.Parallel()

	got := buttonReplyInteractiveData("Yes", "btn-yes", nil)
	require.NotNil(t, got)
	assert.Equal(t, "btn-yes", got["button_id"])
	assert.Equal(t, "Yes", got["title"])
	_, hasCard := got["card_body"]
	assert.False(t, hasCard)
}
