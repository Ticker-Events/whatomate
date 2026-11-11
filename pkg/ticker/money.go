package ticker

// PaiseToRupees converts ticker API money (integer minor units) to major units.
func PaiseToRupees(paise float64) float64 {
	return paise / 100
}

var productMoneyKeys = []string{"min_price", "mrp", "price", "amount"}
var orderMoneyKeys = []string{"amount", "shipping_fee", "total", "subtotal"}

// NormalizeProductMoney converts product money fields from minor units (paise/
// cents) to major units in place. Call once immediately after an API response.
// Not idempotent — do not call twice on the same payload.
func NormalizeProductMoney(raw map[string]any) {
	if raw == nil {
		return
	}
	divideMoneyKeys(raw, productMoneyKeys)
	normalizeMoneyList(raw["options"], productMoneyKeys)
	normalizeMoneyList(raw["addons"], productMoneyKeys)
}

// NormalizeProductListMoney converts each product map in a list.
func NormalizeProductListMoney(products []any) {
	for _, entry := range products {
		if item, ok := entry.(map[string]any); ok {
			NormalizeProductMoney(item)
		}
	}
}

// NormalizeOrderMoney converts order money fields from minor units to major
// units in place. Call once immediately after an API response.
func NormalizeOrderMoney(raw map[string]any) {
	if raw == nil {
		return
	}
	divideMoneyKeys(raw, orderMoneyKeys)
	if payment, ok := raw["payment"].(map[string]any); ok {
		divideMoneyKeys(payment, orderMoneyKeys)
	}
}

var checkoutPreviewMoneyKeys = []string{
	"original_amount",
	"amount",
	"original_shipping_fee",
	"shipping_fee",
	"order_discount_total",
	"shipping_discount_total",
	"discount_total",
	"items_discount_total",
}

var checkoutPreviewDiscountMoneyKeys = []string{
	"amount", "price_before", "price_after", "price_change",
}

// NormalizeCheckoutPreviewMoney converts checkout-preview money fields from
// paise to major units in place. Call once immediately after an API response.
func NormalizeCheckoutPreviewMoney(raw map[string]any) {
	if raw == nil {
		return
	}
	divideMoneyKeys(raw, checkoutPreviewMoneyKeys)
	normalizeCheckoutPreviewLines(raw["items"])
	normalizeCheckoutPreviewAddons(raw["addons"])
	normalizeCheckoutPreviewDiscounts(raw["applied_discounts"])
	if delivery, ok := raw["delivery"].(map[string]any); ok {
		NormalizeDeliveryMoney(delivery)
	}
}

func normalizeCheckoutPreviewLines(raw any) {
	switch list := raw.(type) {
	case []any:
		for _, entry := range list {
			item, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			divideMoneyKeys(item, []string{"original_amount", "amount"})
			normalizeCheckoutPreviewDiscounts(item["applied_discounts"])
		}
	case []map[string]any:
		for _, item := range list {
			divideMoneyKeys(item, []string{"original_amount", "amount"})
			normalizeCheckoutPreviewDiscounts(item["applied_discounts"])
		}
	}
}

func normalizeCheckoutPreviewAddons(raw any) {
	normalizeMoneyList(raw, []string{"amount"})
}

func normalizeCheckoutPreviewDiscounts(raw any) {
	normalizeMoneyList(raw, checkoutPreviewDiscountMoneyKeys)
}

// NormalizeDeliveryMoney adds shipping_fee in major units from shipping_fee_paise.
// Leaves shipping_fee_paise unchanged for callers that still need minor units.
func NormalizeDeliveryMoney(raw map[string]any) {
	if raw == nil {
		return
	}
	if fee, ok := raw["shipping_fee_paise"]; ok {
		raw["shipping_fee"] = PaiseToRupees(asFloat(fee))
	}
}

func normalizeMoneyList(raw any, keys []string) {
	switch list := raw.(type) {
	case []any:
		for _, entry := range list {
			if item, ok := entry.(map[string]any); ok {
				divideMoneyKeys(item, keys)
			}
		}
	case []map[string]any:
		for _, item := range list {
			divideMoneyKeys(item, keys)
		}
	}
}

func divideMoneyKeys(raw map[string]any, keys []string) {
	for _, key := range keys {
		if _, ok := raw[key]; ok {
			raw[key] = PaiseToRupees(asFloat(raw[key]))
		}
	}
}
