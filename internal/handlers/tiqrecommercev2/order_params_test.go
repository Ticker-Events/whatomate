package tiqrecommercev2

import (
	"encoding/json"
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV2OrderParams_UseItemsNotItemsJSON(t *testing.T) {
	data := map[string]any{
		"tiqr_cart": []any{
			map[string]any{
				"line_id":        "line_1",
				"product_option": "3335",
				"quantity":       "2",
				"option_name":    "CHOCO PRALINE",
				"product_name":   "CHOCO PRALINE",
			},
		},
		"customer_email": "aswindivakar100@gmail.com",
		"customer_name":  "Aswin",
		"customer_phone": "919846435358",
		"delivery_mode":  modePickup,
	}

	preview := tiqrecommerce.CheckoutPreviewParams(data)
	require.NotEmpty(t, preview["items"])
	assert.NotContains(t, preview, "items_json")
	var previewItems []map[string]any
	require.NoError(t, json.Unmarshal([]byte(preview["items"]), &previewItems))
	require.Len(t, previewItems, 1)
	assert.Equal(t, "3335", previewItems[0]["product_option"])
	assert.Equal(t, "2", previewItems[0]["quantity"])
	assert.Equal(t, modePickup, preview["delivery_mode"])

	order := tiqrecommerce.PickupOrderParams(data)
	require.NotEmpty(t, order["items"])
	assert.NotContains(t, order, "items_json")
	assert.Equal(t, "aswindivakar100@gmail.com", order["email"])
	assert.Equal(t, "919846435358", order["phone_number"])
	assert.Equal(t, modePickup, order["delivery_mode"])
	var orderItems []map[string]any
	require.NoError(t, json.Unmarshal([]byte(order["items"]), &orderItems))
	require.Len(t, orderItems, 1)
	assert.Equal(t, "3335", orderItems[0]["product_option"])
}
