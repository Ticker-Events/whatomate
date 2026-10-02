package tiqrecommerce

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	summary := formatOrderStatusSummary(map[string]any{
		"display_uid":  "ORD-9",
		"status":       "PENDING_PAYMENT",
		"total_amount": 15000,
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

	assert.Contains(t, summary, "Order *ORD-9*")
	assert.Contains(t, summary, "pending payment")
	assert.Contains(t, summary, "Kunafa (Large) × 2 — ₹100.00")
	assert.Contains(t, summary, "Total: ₹150.00")
	assert.Contains(t, summary, "Fulfillment: Delivery")
	assert.Contains(t, summary, "12 MG Road")
	assert.Contains(t, summary, "Bengaluru, KA")
}

func TestFormatOrderStatusSummary_PickupOmitsAddress(t *testing.T) {
	summary := formatOrderStatusSummary(map[string]any{
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
	assert.NotContains(t, summary, "should not appear")
	assert.NotContains(t, summary, "Address")
}
