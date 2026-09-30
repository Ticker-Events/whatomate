package ticker_test

import (
	"testing"

	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeProductMoneyOnce(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"min_price": 25000,
		"mrp":       30000,
		"options": []any{
			map[string]any{"id": 1, "price": 25000, "mrp": 30000},
		},
		"addons": []any{
			map[string]any{"id": 9, "price": 5000},
		},
	}
	ticker.NormalizeProductMoney(raw)
	assert.Equal(t, 250.0, raw["min_price"])
	assert.Equal(t, 300.0, raw["mrp"])
	opt := raw["options"].([]any)[0].(map[string]any)
	assert.Equal(t, 250.0, opt["price"])
	addon := raw["addons"].([]any)[0].(map[string]any)
	assert.Equal(t, 50.0, addon["price"])

	summary := ticker.CompactProduct(raw)
	assert.Equal(t, 250.0, summary.MinPrice)
	assert.Equal(t, 250.0, summary.Options[0].Price)
}

func TestNormalizeOrderAndDeliveryMoney(t *testing.T) {
	t.Parallel()
	order := map[string]any{"amount": 10000, "shipping_fee": 5000}
	ticker.NormalizeOrderMoney(order)
	assert.Equal(t, 100.0, order["amount"])
	assert.Equal(t, 50.0, order["shipping_fee"])

	delivery := map[string]any{"shipping_fee_paise": 2500, "zone": "paid"}
	ticker.NormalizeDeliveryMoney(delivery)
	assert.Equal(t, 25.0, delivery["shipping_fee"])
	assert.Equal(t, 2500, delivery["shipping_fee_paise"])
}
