package tiqrecommerce

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroundAddonParse_SelectWithQuantities(t *testing.T) {
	qty2 := 2
	qty1 := 1
	choices := []map[string]any{
		{"id": 9, "name": "Candles", "price": 50},
		{"id": 3, "name": "Flowers", "price": 100},
	}
	out := groundAddonParse(codedAddonParseResult{
		Intent:     codedAddonIntentSelect,
		Confidence: 0.9,
		Items: []codedAddonParseItem{
			{Index: 1, Quantity: &qty2},
			{Index: 2, Quantity: &qty1},
		},
	}, choices)
	require.Equal(t, codedAddonIntentSelect, out.kind)
	require.Len(t, out.lines, 2)
	assert.Equal(t, 9, out.lines[0].ID)
	assert.Equal(t, 2, out.lines[0].Quantity)
	assert.Equal(t, "Candles", out.lines[0].Name)
	assert.Equal(t, 3, out.lines[1].ID)
}

func TestGroundAddonParse_MissingQuantity(t *testing.T) {
	choices := []map[string]any{
		{"id": 9, "name": "Candles"},
	}
	out := groundAddonParse(codedAddonParseResult{
		Intent:          codedAddonIntentSelect,
		Confidence:      0.9,
		Items:           []codedAddonParseItem{{Index: 1}},
		MissingQuantity: []int{1},
	}, choices)
	assert.Equal(t, "missing_quantity", out.kind)
	assert.Equal(t, []int{1}, out.missing)
}

func TestGroundAddonParse_SkipAndLowConfidence(t *testing.T) {
	choices := []map[string]any{{"id": 9, "name": "Candles"}}
	assert.Equal(t, codedAddonIntentSkip, groundAddonParse(codedAddonParseResult{
		Intent: codedAddonIntentSkip, Confidence: 0.9,
	}, choices).kind)
	assert.Equal(t, codedAddonIntentUnclear, groundAddonParse(codedAddonParseResult{
		Intent: codedAddonIntentSkip, Confidence: 0.2,
	}, choices).kind)
	qty := 1
	assert.Equal(t, codedAddonIntentUnclear, groundAddonParse(codedAddonParseResult{
		Intent: codedAddonIntentSelect, Confidence: 0.5,
		Items: []codedAddonParseItem{{Index: 1, Quantity: &qty}},
	}, choices).kind)
}

func TestGroundAddonParse_OutOfRangeIgnoredAsUnclear(t *testing.T) {
	qty := 2
	choices := []map[string]any{{"id": 9, "name": "Candles"}}
	out := groundAddonParse(codedAddonParseResult{
		Intent:     codedAddonIntentSelect,
		Confidence: 0.95,
		Items:      []codedAddonParseItem{{Index: 9, Quantity: &qty}},
	}, choices)
	assert.Equal(t, codedAddonIntentUnclear, out.kind)
	assert.Empty(t, out.lines)
}

func TestOrderAddonsForAPI_DropsName(t *testing.T) {
	out := OrderAddonsForAPI([]any{
		map[string]any{"addon": 9, "quantity": 2, "name": "Candles"},
		map[string]any{"addon": 0, "quantity": 1},
	})
	require.Len(t, out, 1)
	assert.Equal(t, 9, out[0]["addon"])
	assert.Equal(t, 2, out[0]["quantity"])
	_, hasName := out[0]["name"]
	assert.False(t, hasName)
}

func TestPickupOrderParams_IncludesAddons(t *testing.T) {
	params := PickupOrderParams(map[string]any{
		"customer_email": "buyer@example.com",
		"tiqr_cart": []any{map[string]any{
			"product_option": "1312",
			"quantity":       "1",
		}},
		"commerce_addons": []any{
			map[string]any{"addon": 9, "quantity": 2, "name": "Candles"},
		},
	})
	assert.Contains(t, params["addons"], `"addon":9`)
	assert.Contains(t, params["addons"], `"quantity":2`)
	assert.NotContains(t, params["addons"], "Candles")
}

func TestFormatHandoffAddons(t *testing.T) {
	text := FormatHandoffAddons([]any{
		map[string]any{"addon": 9, "quantity": 2, "name": "Candles"},
	})
	assert.Equal(t, "Candles x 2", text)
}

func TestAppendCommerceAddon_KeepsName(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	appendCommerceAddon(session, 9, 2, "Candles")
	appendCommerceAddon(session, 9, 1, "Candles")
	raw, _ := session.SessionData["commerce_addons"].([]any)
	require.Len(t, raw, 1)
	addon, ok := raw[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 9, anyToInt(addon["addon"]))
	assert.Equal(t, 3, anyToInt(addon["quantity"]))
	assert.Equal(t, "Candles", asString(addon["name"]))
}
