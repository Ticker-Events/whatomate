package codedflow

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type quoteTestChat struct {
	legendTestChat
	userInput string
	buttonID  string
	consumed  bool
	quoted    *QuotedMessage
}

func (c *quoteTestChat) UserInput() string             { return c.userInput }
func (c *quoteTestChat) SetUserInput(v string)         { c.userInput = v }
func (c *quoteTestChat) ButtonID() string              { return c.buttonID }
func (c *quoteTestChat) SetButtonID(v string)          { c.buttonID = v }
func (c *quoteTestChat) Consumed() bool                { return c.consumed }
func (c *quoteTestChat) SetConsumed(v bool)            { c.consumed = v }
func (c *quoteTestChat) QuotedMessage() *QuotedMessage { return c.quoted }

func TestPrepareQuotedProductSelection_RestoresShownProduct(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{
		shownProductsKey: []any{
			map[string]any{
				"id":   "101",
				"name": "Kunafa",
				"options": []any{
					map[string]any{"id": "9", "name": "Regular"},
				},
			},
		},
		"products": []any{
			map[string]any{"id": "201", "name": "Other"},
		},
	}}
	chat := &quoteTestChat{
		legendTestChat: legendTestChat{session: session},
		buttonID:       "101",
		quoted: &QuotedMessage{
			IsLatestOutgoing: false,
			InteractiveData: map[string]any{
				"type": "carousel",
				"cards": []any{
					map[string]any{
						"body": "Kunafa",
						"buttons": []any{
							map[string]any{"id": "101", "title": "Add to cart"},
						},
					},
				},
			},
		},
	}
	c := NewConv(nil, chat)
	ok := c.prepareQuotedProductSelection(map[string]any{
		"mode":      "carousel",
		"items_var": "products",
	})
	require.True(t, ok)
	assert.Equal(t, "101", asString(session.SessionData["product_id"]))
	assert.Equal(t, "Kunafa", asString(session.SessionData["product_name"]))
	items, ok := anySlice(session.SessionData["products"])
	require.True(t, ok)
	require.Len(t, items, 1)
	assert.Equal(t, "101", fieldString(asStringMapMust(items[0]), "id"))
}

func TestTryQuotedReply_SkipsLatestOutgoing(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	chat := &quoteTestChat{
		legendTestChat: legendTestChat{session: session},
		buttonID:       "101",
		userInput:      "Add to cart",
		quoted: &QuotedMessage{
			IsLatestOutgoing: true,
			InteractiveData: map[string]any{
				"type": "carousel",
				"cards": []any{
					map[string]any{
						"buttons": []any{map[string]any{"id": "101", "title": "Add to cart"}},
					},
				},
			},
		},
	}
	c := NewConv(nil, chat)
	assert.False(t, c.tryQuotedReply("product", map[string]any{"mode": "carousel", "items_var": "products"}, RouteOptions{}))
	assert.False(t, c.HasReplyRoute())
}

func TestTryQuotedReply_CollectionDivert(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"collections": []any{
			map[string]any{"id": "57", "name": "Sweets"},
		},
	}}
	chat := &quoteTestChat{
		legendTestChat: legendTestChat{session: session},
		buttonID:       "57",
		userInput:      "Sweets",
		quoted: &QuotedMessage{
			IsLatestOutgoing: false,
			InteractiveData: map[string]any{
				"type": "list",
				"rows": []any{
					map[string]any{"id": "57", "title": "Sweets"},
				},
			},
		},
	}
	c := NewConv(nil, chat)
	handled := c.tryQuotedReply("product", map[string]any{
		"mode":      "carousel",
		"items_var": "products",
	}, RouteOptions{AllowCatalog: true})
	require.True(t, handled)
	require.True(t, c.HasReplyRoute())
	route := c.TakeReplyRoute()
	assert.Equal(t, RouteCollection, route.Kind)
	assert.Equal(t, "57", route.ID)
	assert.True(t, chat.consumed)
}
