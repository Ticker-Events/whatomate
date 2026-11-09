package tiqrleads

import (
	"strconv"
	"strings"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
)

const (
	storeModePickup   = "PICKUP_FROM_STORE"
	storeModeDelivery = "DELIVERY_TO_LOCATION"
)

// tryStoreFactsHelp answers address, delivery, and contact questions from the
// loaded get_store payload. No model and no confidence bar — these are fields.
func tryStoreFactsHelp(c *codedflow.Conv, message string) helpLookupResult {
	if c == nil || c.Session() == nil {
		return helpLookupResult{Kind: helpOutcomeMiss}
	}
	store, ok := asStringMap(c.Session().SessionData["store"])
	if !ok || store == nil {
		return helpLookupResult{Kind: helpOutcomeMiss}
	}
	norm := normalizeHelpText(message)
	if norm == "" {
		return helpLookupResult{Kind: helpOutcomeMiss}
	}

	wantsAddress := helpTextHasAny(norm,
		"where are you", "where is the store", "your address", "store address",
		"location", "located", "directions", "map", "how do i reach", "address",
	) || (strings.Contains(norm, "where") && strings.Contains(norm, "store"))
	wantsDelivery := helpTextHasAny(norm,
		"deliver", "delivery", "pickup", "pick up", "pick-up",
		"shipping area", "delivery area", "delivery radius", "do you ship",
	)
	wantsPhone := helpTextHasAny(norm,
		"phone", "call you", "contact number", "whatsapp number", "mobile number",
	)

	if !wantsAddress && !wantsDelivery && !wantsPhone {
		return helpLookupResult{Kind: helpOutcomeMiss}
	}

	answer := buildStoreFactsAnswer(store, wantsAddress, wantsDelivery, wantsPhone)
	if answer == "" {
		return helpLookupResult{Kind: helpOutcomeMiss}
	}
	return helpLookupResult{Kind: helpOutcomeStoreFacts, Answer: answer}
}

func helpTextHasAny(norm string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(norm, phrase) {
			return true
		}
	}
	return false
}

func buildStoreFactsAnswer(store map[string]any, wantsAddress, wantsDelivery, wantsPhone bool) string {
	parts := make([]string, 0, 4)

	if wantsAddress {
		if line := formatStoreAddressLine(store); line != "" {
			parts = append(parts, line)
		}
		if link := formatStoreMapsLink(store); link != "" {
			parts = append(parts, link)
		}
	}

	if wantsDelivery {
		if line := formatStoreDeliveryLine(store); line != "" {
			parts = append(parts, line)
		}
	}

	if wantsPhone {
		if line := formatStorePhoneLine(store); line != "" {
			parts = append(parts, line)
		}
	}

	// If the customer asked about location but we only have delivery, or vice
	// versa with empty facts for the requested topic, stay a miss.
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func formatStoreAddressLine(store map[string]any) string {
	name := strings.TrimSpace(asString(store["name"]))
	address := strings.TrimSpace(asString(store["address"]))
	switch {
	case name != "" && address != "":
		return name + " is at " + address + "."
	case address != "":
		return "Our store is at " + address + "."
	case name != "":
		return "You can find us at " + name + "."
	default:
		return ""
	}
}

func formatStoreMapsLink(store map[string]any) string {
	lat, okLat := codedflow.AnyToFloat64(store["latitude"])
	lng, okLng := codedflow.AnyToFloat64(store["longitude"])
	if !okLat || !okLng {
		return ""
	}
	return "Map: https://maps.google.com/?q=" +
		strconv.FormatFloat(lat, 'f', 6, 64) + "," +
		strconv.FormatFloat(lng, 'f', 6, 64)
}

func formatStorePhoneLine(store map[string]any) string {
	phone := strings.TrimSpace(asString(store["phone"]))
	if phone == "" {
		phone = strings.TrimSpace(asString(store["phone_number"]))
	}
	if phone == "" {
		if wa, ok := asStringMap(store["wa_project"]); ok {
			phone = strings.TrimSpace(asString(wa["phone_number"]))
		}
	}
	if phone == "" {
		return ""
	}
	return "You can reach us at " + phone + "."
}

func formatStoreDeliveryLine(store map[string]any) string {
	modes := storeDeliveryModeList(store)
	hasPickup := storeModesContain(modes, storeModePickup)
	hasDelivery := storeModesContain(modes, storeModeDelivery)

	var b strings.Builder
	switch {
	case hasPickup && hasDelivery:
		b.WriteString("We offer store pickup and delivery.")
	case hasPickup:
		b.WriteString("We offer store pickup.")
	case hasDelivery:
		b.WriteString("We offer delivery.")
	}

	if hasDelivery {
		freeLabel := storeRadiusKmLabel(store["free_delivery_radius"])
		maxLabel := storeRadiusKmLabel(store["delivery_radius"])
		offers := make([]string, 0, 2)
		if freeLabel != "" {
			offers = append(offers, "Free delivery within "+freeLabel+" km")
		}
		if maxLabel != "" {
			offers = append(offers, "Delivery available up to "+maxLabel+" km")
		}
		if len(offers) > 0 {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(strings.Join(offers, ". "))
			b.WriteByte('.')
		}
	}

	return strings.TrimSpace(b.String())
}

func storeDeliveryModeList(store map[string]any) []string {
	if store == nil {
		return nil
	}
	raw := store["delivery_modes"]
	items, ok := anySlice(raw)
	if !ok {
		switch t := raw.(type) {
		case []string:
			return append([]string{}, t...)
		default:
			return nil
		}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := strings.TrimSpace(asString(item)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func storeModesContain(modes []string, want string) bool {
	for _, mode := range modes {
		if mode == want {
			return true
		}
	}
	return false
}

func storeRadiusKmLabel(v any) string {
	f, ok := codedflow.AnyToFloat64(v)
	if !ok || f <= 0 {
		return ""
	}
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}
