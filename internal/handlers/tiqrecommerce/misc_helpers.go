package tiqrecommerce

import (
	"fmt"
	"strconv"
	"strings"
	"github.com/shridarpatil/whatomate/internal/models"
)

func isCheckoutStartIntent(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch lower {
	case "checkout", "check out", "check-out", "place order", "place my order", "ready to checkout", "ready to check out":
		return true
	default:
		return false
	}
}

func parsePositiveInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
		if n > 9999 {
			return 0
		}
	}
	return n
}

func radiusKmLabel(v any) string {
	f, ok := anyToFloat64(v)
	if !ok || f <= 0 {
		return ""
	}
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

// formatOutOfRangeDeliveryMessage builds the WhatsApp copy shown when a pin
// is outside delivery_radius. It uses store.address, free_delivery_radius,
// and delivery_radius from get_store — never invents those values.
func formatOutOfRangeDeliveryMessageWithPickup(store map[string]any, offerPickup bool) string {
	var b strings.Builder
	b.WriteString("Sorry, we're currently unable to deliver to this location.")

	address := ""
	var freeLabel, maxLabel string
	if store != nil {
		address = asString(store["address"])
		freeLabel = radiusKmLabel(store["free_delivery_radius"])
		maxLabel = radiusKmLabel(store["delivery_radius"])
	}

	offers := make([]string, 0, 2)
	if freeLabel != "" {
		offers = append(offers, "- Free delivery within "+freeLabel+" km of our store")
	}
	if maxLabel != "" {
		offers = append(offers, "- Delivery up to "+maxLabel+" km for an additional delivery fee")
	}

	if address != "" || len(offers) > 0 {
		b.WriteString("\n\n")
		if address != "" {
			b.WriteString("Our store is located in ")
			b.WriteString(address)
			b.WriteString(".")
			if len(offers) > 0 {
				b.WriteString(" We offer:")
			}
		} else {
			b.WriteString("We offer:")
		}
		if len(offers) > 0 {
			b.WriteString("\n\n")
			b.WriteString(strings.Join(offers, "\n"))
		}
	}

	if offerPickup {
		b.WriteString("\n\nPlease provide a location within our delivery radius, or choose Store Pickup as your preferred option.\n\nThank you for your understanding.")
	} else {
		b.WriteString("\n\nPlease provide a location within our delivery radius.\n\nThank you for your understanding.")
	}
	return b.String()
}

func formatDirectOrderStatus(order map[string]any) string {
	id := asString(order["display_uid"])
	status := strings.ReplaceAll(strings.ToLower(asString(order["status"])), "_", " ")
	if status == "" {
		status = "unknown"
	}
	if id == "" {
		return "Your latest order status is " + status + "."
	}
	return fmt.Sprintf("Order %s is %s.", id, status)
}

func sessionCollectionByID(session *models.ChatbotSession, id string) map[string]any {
	id = strings.TrimSpace(id)
	if session == nil || session.SessionData == nil || id == "" {
		return nil
	}
	items, ok := anySlice(session.SessionData["collections"])
	if !ok {
		return nil
	}
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if fieldString(item, "id") == id {
			return item
		}
	}
	return nil
}

