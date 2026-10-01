package handlers

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
	out := orderAddonsForAPI([]any{
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
	params := pickupOrderParams(map[string]any{
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
	text := formatHandoffAddons([]any{
		map[string]any{"addon": 9, "quantity": 2, "name": "Candles"},
	})
	assert.Equal(t, "Candles x 2", text)
}

func TestAppendCommerceAddon_KeepsName(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	appendCommerceAddon(session, 9, 2, "Candles")
	appendCommerceAddon(session, 9, 1, "Candles")
	addons := checkoutAddons(session)
	require.Len(t, addons, 1)
	assert.Equal(t, 9, anyToInt(addons[0]["addon"]))
	assert.Equal(t, 3, anyToInt(addons[0]["quantity"]))
	assert.Equal(t, "Candles", asString(addons[0]["name"]))
}

func useCodedAddonParse(t *testing.T, parse func(string) (codedAddonParseResult, error)) {
	t.Helper()
	prev := parseCodedAddons
	parseCodedAddons = func(_ *App, _ *models.ChatbotSession, prompt string) (codedAddonParseResult, error) {
		if parse == nil {
			t.Fatal("addon parse was called")
		}
		return parse(prompt)
	}
	t.Cleanup(func() { parseCodedAddons = prev })
}

func productWithAddons() []any {
	return []any{
		map[string]any{
			"id": "101", "name": "Themed Cake", "min_price": "500",
			"description": "Celebration cake",
			"images":      []any{map[string]any{"original_url": "https://example.com/cake.jpg"}},
			"options":     []any{map[string]any{"id": "9", "name": "Regular", "price": "500"}},
			"addons": []any{
				map[string]any{"id": 21, "name": "Candles", "price": 50, "is_active": true},
				map[string]any{"id": 22, "name": "Flowers", "price": 100, "is_active": true},
			},
		},
	}
}

func TestTiqrEcommerce_BuyFlowStoresCatalogAddons(t *testing.T) {
	useCodedIntent(t, nil, nil)
	qty := 2
	useCodedAddonParse(t, func(string) (codedAddonParseResult, error) {
		return codedAddonParseResult{
			Intent:     codedAddonIntentSelect,
			Confidence: 0.92,
			Items:      []codedAddonParseItem{{Index: 1, Quantity: &qty}},
		}, nil
	})
	app, account, contact, session := startEcommerce(t, productWithAddons(), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Cakes", "58", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add to cart", "101", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "quantity", session.CurrentStep)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	assert.Contains(t, outgoingBlob(t, app, session), "This product has the following add-ons")
	assert.Contains(t, outgoingBlob(t, app, session), "1. Candles")
	assert.Equal(t, "product_addons_1", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "item 1 - 2", "", nil))
	reloadSession(t, app, session)
	addons := checkoutAddons(session)
	require.Len(t, addons, 1)
	assert.Equal(t, 21, anyToInt(addons[0]["addon"]))
	assert.Equal(t, 2, anyToInt(addons[0]["quantity"]))
	assert.Equal(t, "Candles", asString(addons[0]["name"]))
	assert.Contains(t, outgoingBlob(t, app, session), "Added Candles x2")
	_, hasCart := session.SessionData["tiqr_cart"]
	assert.True(t, hasCart)
}

func TestTiqrEcommerce_AddonMissingQuantityThenSave(t *testing.T) {
	useCodedIntent(t, nil, nil)
	calls := 0
	useCodedAddonParse(t, func(string) (codedAddonParseResult, error) {
		calls++
		if calls == 1 {
			return codedAddonParseResult{
				Intent:          codedAddonIntentSelect,
				Confidence:      0.9,
				Items:           []codedAddonParseItem{{Index: 1}},
				MissingQuantity: []int{1},
			}, nil
		}
		qty := 3
		return codedAddonParseResult{
			Intent:     codedAddonIntentSelect,
			Confidence: 0.95,
			Items:      []codedAddonParseItem{{Index: 1, Quantity: &qty}},
		}, nil
	})
	app, account, contact, session := startEcommerce(t, productWithAddons(), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Cakes", "58", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add to cart", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Candles", "", nil))
	reloadSession(t, app, session)
	assert.Contains(t, outgoingBlob(t, app, session), "How many of item 1 (Candles)")
	assert.Empty(t, checkoutAddons(session))

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "3", "", nil))
	reloadSession(t, app, session)
	addons := checkoutAddons(session)
	require.Len(t, addons, 1)
	assert.Equal(t, 3, anyToInt(addons[0]["quantity"]))
}

func TestTiqrEcommerce_AddonUnclearTransfersAfterGuides(t *testing.T) {
	useCodedIntent(t, nil, nil)
	useCodedAddonParse(t, func(string) (codedAddonParseResult, error) {
		return codedAddonParseResult{
			Intent:     codedAddonIntentUnclear,
			Confidence: 0.2,
			Question:   "Which add-on did you mean?",
		}, nil
	})
	app, account, contact, session := startEcommerceWithStore(t, productWithAddons(), nil, map[string]any{
		"id": 42, "name": "Demo",
	}, &models.AIConfig{CommerceEnabled: true})
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Cakes", "58", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add to cart", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)

	for i := 0; i < codedIntentSettings.MaxGuideTurns; i++ {
		require.NoError(t, app.runCodedFlow(account, contact, session, flow, "something weird", "", nil))
		reloadSession(t, app, session)
		assert.Empty(t, checkoutAddons(session))
		assert.NotEqual(t, models.SessionStatusCancelled, session.Status)
	}
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "still unclear", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCancelled, session.Status)
	assert.Empty(t, checkoutAddons(session))
}
