package tiqrecommerce

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type orderStatusTestChat struct {
	session *models.ChatbotSession
}

func (c *orderStatusTestChat) Session() *models.ChatbotSession             { return c.session }
func (c *orderStatusTestChat) Account() *models.WhatsAppAccount           { return nil }
func (c *orderStatusTestChat) Contact() *models.Contact                   { return nil }
func (c *orderStatusTestChat) UserInput() string                          { return "" }
func (c *orderStatusTestChat) SetUserInput(string)                        {}
func (c *orderStatusTestChat) InboundMedia() codedflow.InboundMedia       { return codedflow.InboundMedia{} }
func (c *orderStatusTestChat) ButtonID() string                           { return "" }
func (c *orderStatusTestChat) SetButtonID(string)                         {}
func (c *orderStatusTestChat) FlowResponseData() map[string]any           { return nil }
func (c *orderStatusTestChat) SetFlowResponseData(map[string]any)         {}
func (c *orderStatusTestChat) QuotedMessage() *codedflow.QuotedMessage    { return nil }
func (c *orderStatusTestChat) Consumed() bool                             { return false }
func (c *orderStatusTestChat) SetConsumed(bool)                           {}
func (c *orderStatusTestChat) Capturing() bool                            { return false }
func (c *orderStatusTestChat) LastTiqr() map[string]any                   { return nil }
func (c *orderStatusTestChat) SetLastTiqr(map[string]any)                 {}
func (c *orderStatusTestChat) LastTiqrErr() string                        { return "" }
func (c *orderStatusTestChat) SetLastTiqrErr(string)                      {}
func (c *orderStatusTestChat) LastTiqrStatus() int                        { return 0 }
func (c *orderStatusTestChat) SetLastTiqrStatus(int)                      {}
func (c *orderStatusTestChat) Preview() *codedflow.PreviewSink            { return nil }
func (c *orderStatusTestChat) SetPreview(*codedflow.PreviewSink)          {}
func (c *orderStatusTestChat) Archive() *codedflow.PreviewSink            { return nil }
func (c *orderStatusTestChat) SetArchive(*codedflow.PreviewSink)          {}
func (c *orderStatusTestChat) Native() any                                { return nil }

func TestReshapeOrdersForList(t *testing.T) {
	rows := reshapeOrdersForList([]any{
		map[string]any{
			"id":          11,
			"display_uid": "ORD-11",
			"created_at":  "2026-10-02T12:30:00Z",
			"status":      "CONFIRMED",
		},
		map[string]any{
			"id":         12,
			"created_at": "2026-09-01T08:00:00+05:30",
		},
		map[string]any{"display_uid": "no-id"},
	})
	require.Len(t, rows, 2)

	first, ok := rows[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "11", first["id"])
	assert.Equal(t, "ORD-11", first["display_uid"])
	assert.Equal(t, "2 Oct 2026", first["placed_on"])

	second, ok := rows[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "12", second["id"])
	assert.Equal(t, "12", second["display_uid"])
	assert.Equal(t, "1 Sep 2026", second["placed_on"])
}

func TestFormatOrderStatusSummary_Delivery(t *testing.T) {
	c := Wrap(codedflow.NewConv(nil, &orderStatusTestChat{
		session: &models.ChatbotSession{SessionData: models.JSONB{}},
	}))
	summary := formatOrderStatusSummary(c, map[string]any{
		"display_uid":   "ORD-9",
		"status":        "PENDING_PAYMENT",
		"total_amount":  15000,
		"delivery_mode": ModeDelivery,
		"items": []any{
			map[string]any{
				"quantity": 2,
				"amount":   10000,
				"product_option_snapshot": map[string]any{
					"name": "Large",
					"product": map[string]any{
						"name": "Kunafa",
					},
				},
			},
		},
		"address": map[string]any{
			"name":           "Asha",
			"address_line_1": "12 MG Road",
			"city":           "Bengaluru",
			"state":          "KA",
			"pincode":        "560001",
			"country":        "India",
		},
	}, "INR")

	assert.Contains(t, summary, "Order *{{order_id}}*")
	assert.Contains(t, summary, "Status: {{order_status}}")
	assert.Contains(t, summary, "{{order_items}}")
	assert.Contains(t, summary, "Total: {{order_total}}")
	assert.Contains(t, summary, "Fulfillment: Delivery")
	assert.Contains(t, summary, "{{order_address}}")
	assert.Equal(t, "ORD-9", c.Session().SessionData["order_id"])
	assert.Equal(t, "pending payment", c.Session().SessionData["order_status"])
	assert.Contains(t, asString(c.Session().SessionData["order_items"]), "Kunafa (Large) × 2 — ₹100.00")
	assert.Equal(t, "₹150.00", c.Session().SessionData["order_total"])
	assert.Contains(t, asString(c.Session().SessionData["order_address"]), "12 MG Road")
	assert.Contains(t, asString(c.Session().SessionData["order_address"]), "Bengaluru, KA")
}

func TestFormatOrderStatusSummary_PickupOmitsAddress(t *testing.T) {
	c := Wrap(codedflow.NewConv(nil, &orderStatusTestChat{
		session: &models.ChatbotSession{SessionData: models.JSONB{}},
	}))
	summary := formatOrderStatusSummary(c, map[string]any{
		"display_uid":   "ORD-1",
		"status":        "CONFIRMED",
		"total_amount":  5000,
		"delivery_mode": tiqrModePickup,
		"items": []any{
			map[string]any{
				"quantity": 1,
				"amount":   5000,
				"product_option_snapshot": map[string]any{
					"name": "Default",
					"product": map[string]any{
						"name": "Tea",
					},
				},
			},
		},
		"address": map[string]any{
			"address_line_1": "should not appear",
		},
	}, "INR")

	assert.Contains(t, summary, "Fulfillment: Store pickup")
	assert.NotContains(t, summary, "order_address")
	assert.NotContains(t, summary, "should not appear")
	assert.NotContains(t, summary, "Address")
}
