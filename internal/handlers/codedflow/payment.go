package codedflow

import (
	"fmt"
	"strings"
)

// PaymentCTAForTest builds payment CTA content without a Host.
// Mirrors handlers.formatOrderSuccessMessage / paymentCTAContent for unit tests.
func PaymentCTAForTest(order map[string]any, currency string) (string, string) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "INR"
	}
	url := asString(order["payment_url"])
	if url == "" {
		if payment, ok := asStringMap(order["payment"]); ok {
			if meta, ok := asStringMap(payment["meta_data"]); ok {
				url = asString(meta["url_to_redirect"])
			}
		}
	}

	var b strings.Builder
	if uid := asString(order["display_uid"]); uid != "" {
		fmt.Fprintf(&b, "Order placed! Your order number is %s.", uid)
	} else {
		b.WriteString("Order placed!")
	}
	if fee, ok := AnyToFloat64(order["shipping_fee"]); ok {
		if fee > 0 {
			fmt.Fprintf(&b, "\nDelivery fee: %s", formatMoneyForCTA(fee, currency))
		} else {
			b.WriteString("\nDelivery fee: Free")
		}
	}
	if amount, ok := order["amount"].(float64); ok && amount > 0 {
		fmt.Fprintf(&b, "\nTotal: %s", formatMoneyForCTA(amount, currency))
	}
	return strings.TrimSpace(b.String()), url
}

func formatMoneyForCTA(amount float64, currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "INR"
	}
	switch currency {
	case "INR":
		return fmt.Sprintf("₹%.2f", amount)
	case "USD":
		return fmt.Sprintf("$%.2f", amount)
	case "EUR":
		return fmt.Sprintf("€%.2f", amount)
	case "GBP":
		return fmt.Sprintf("£%.2f", amount)
	default:
		return fmt.Sprintf("%s %.2f", currency, amount)
	}
}
