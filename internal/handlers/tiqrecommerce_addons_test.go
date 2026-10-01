package handlers

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useCodedAddonParse(t *testing.T, parse func(string) (tiqrecommerce.AddonParseResult, error)) {
	t.Helper()
	restore := tiqrecommerce.SetParseCodedAddonsForTest(func(_ codedflow.Host, _ *models.ChatbotSession, prompt string) (tiqrecommerce.AddonParseResult, error) {
		if parse == nil {
			t.Fatal("addon parse was called")
		}
		return parse(prompt)
	})
	t.Cleanup(restore)
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
	useCodedAddonParse(t, func(string) (tiqrecommerce.AddonParseResult, error) {
		return tiqrecommerce.AddonParseResult{
			Intent:     tiqrecommerce.AddonIntentSelect,
			Confidence: 0.92,
			Items:      []tiqrecommerce.AddonParseItem{{Index: 1, Quantity: &qty}},
		}, nil
	})
	app, account, contact, session := startEcommerce(t, productWithAddons(), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
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
	useCodedAddonParse(t, func(string) (tiqrecommerce.AddonParseResult, error) {
		calls++
		if calls == 1 {
			return tiqrecommerce.AddonParseResult{
				Intent:          tiqrecommerce.AddonIntentSelect,
				Confidence:      0.9,
				Items:           []tiqrecommerce.AddonParseItem{{Index: 1}},
				MissingQuantity: []int{1},
			}, nil
		}
		qty := 3
		return tiqrecommerce.AddonParseResult{
			Intent:     tiqrecommerce.AddonIntentSelect,
			Confidence: 0.95,
			Items:      []tiqrecommerce.AddonParseItem{{Index: 1, Quantity: &qty}},
		}, nil
	})
	app, account, contact, session := startEcommerce(t, productWithAddons(), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
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
	useCodedAddonParse(t, func(string) (tiqrecommerce.AddonParseResult, error) {
		return tiqrecommerce.AddonParseResult{
			Intent:     tiqrecommerce.AddonIntentUnclear,
			Confidence: 0.2,
			Question:   "Which add-on did you mean?",
		}, nil
	})
	app, account, contact, session := startEcommerceWithStore(t, productWithAddons(), nil, map[string]any{
		"id": 42, "name": "Demo",
	}, &models.AIConfig{CommerceEnabled: true})
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Cakes", "58", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add to cart", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)

	for i := 0; i < codedflow.MaxGuideTurns(); i++ {
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

func TestTiqrEcommerce_BuyFlowUsesOptionAddons(t *testing.T) {
	useCodedIntent(t, nil, nil)
	products := []any{
		map[string]any{
			"id": "101", "name": "Themed Cake", "min_price": "500",
			"description": "Celebration cake",
			"images":      []any{map[string]any{"original_url": "https://example.com/cake.jpg"}},
			"options": []any{
				map[string]any{
					"id": "9", "name": "Regular", "price": "500",
					"addons": []any{
						map[string]any{"id": 21, "name": "Candles", "price": 50, "is_active": true},
					},
				},
			},
		},
	}
	app, account, contact, session := startEcommerce(t, products, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Cakes", "58", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add to cart", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "product_addons_1", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "1. Candles")
}

func earlyHandoffAddonStore() map[string]any {
	return map[string]any{
		"id":   42,
		"name": "Demo",
		"test_collections": []any{
			map[string]any{
				"id":             "57",
				"name":           "Custom Cakes",
				"description":    "Made to order",
				"handoff_policy": "after_capture",
				"required_capture_fields": []any{
					map[string]any{
						"key": "writing", "label": "Cake writing", "type": "text", "required": true,
					},
				},
			},
		},
	}
}

func TestTiqrEcommerce_EarlyHandoffUsesCatalogAddons(t *testing.T) {
	useCodedIntent(t, nil, nil)
	app, account, contact, session := startEcommerceWithStore(t, productWithAddons(), nil, earlyHandoffAddonStore(), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Custom Cakes", "57", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_0_writing", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Happy Birthday", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "early_handoff_addons_1", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "1. Candles")
	assert.NotContains(t, outgoingBlob(t, app, session), "Any add-ons")
	_ = contact
}

func TestTiqrEcommerce_EarlyHandoffManyProductsUsesCatalogAddons(t *testing.T) {
	useCodedIntent(t, nil, nil)
	products := []any{
		map[string]any{
			"id": "101", "name": "Themed Cake", "min_price": "500",
			"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "500"}},
			"addons": []any{
				map[string]any{"id": 21, "name": "Candles", "price": 50, "is_active": true},
			},
		},
		map[string]any{
			"id": "102", "name": "Plain Cake", "min_price": "400",
			"options": []any{map[string]any{"id": "10", "name": "Regular", "price": "400"}},
		},
	}
	app, account, contact, session := startEcommerceWithStore(t, products, nil, earlyHandoffAddonStore(), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Custom Cakes", "57", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_0_writing", session.CurrentStep)
	assert.NotEqual(t, "product", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Happy Birthday", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "early_handoff_addons_1", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "1. Candles")
	_ = contact
}
