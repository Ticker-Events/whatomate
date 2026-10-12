package tiqrecommerce

import (
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const deliveryGeocodeCacheKey = "delivery_geocode"

// detailsFlowPrefill builds WhatsApp Flow init values for the details form.
// customer_phone always comes from the session WhatsApp number. Address fields
// are filled from a reverse-geocoded delivery pin when available.
func detailsFlowPrefill(c *Conv) map[string]string {
	out := map[string]string{}
	if phone := sessionWhatsAppPhone(c); phone != "" {
		out["customer_phone"] = phone
	}
	if asString(c.Session().SessionData["delivery_mode"]) != tiqrModeDelivery {
		return out
	}
	for k, v := range cachedOrLookupDeliveryGeocode(c) {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

func sessionWhatsAppPhone(c *Conv) string {
	if c == nil || c.Session() == nil {
		return ""
	}
	if phone := strings.TrimSpace(c.Session().PhoneNumber); phone != "" {
		return phone
	}
	return strings.TrimSpace(asString(c.Session().SessionData["phone_number"]))
}

func cachedOrLookupDeliveryGeocode(c *Conv) map[string]string {
	if c == nil || c.Session() == nil {
		return nil
	}
	session := c.Session()
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	if cached, ok := asStringMap(session.SessionData[deliveryGeocodeCacheKey]); ok {
		return map[string]string{
			"pincode": contextValue(cached, "pincode"),
			"city":    contextValue(cached, "city"),
			"state":   contextValue(cached, "state"),
			"country": contextValue(cached, "country"),
		}
	}
	lat, okLat := anyToFloat64(session.SessionData["delivery_latitude"])
	lng, okLng := anyToFloat64(session.SessionData["delivery_longitude"])
	if !okLat || !okLng || c.App() == nil {
		session.SessionData[deliveryGeocodeCacheKey] = map[string]any{}
		return nil
	}
	parts := c.App().ReverseGeocode(session.OrganizationID, session.WhatsAppAccount, lat, lng)
	cache := map[string]any{}
	out := map[string]string{}
	for _, key := range []string{"pincode", "city", "state", "country"} {
		if v := strings.TrimSpace(parts[key]); v != "" {
			cache[key] = v
			out[key] = v
		}
	}
	session.SessionData[deliveryGeocodeCacheKey] = cache
	return out
}

// fallbackSupportEmail builds support+<digits>@tiqr.events from a phone number.
func fallbackSupportEmail(phone string) string {
	digits := digitsOnlyPhone(phone)
	if digits == "" {
		return ""
	}
	return "support+" + digits + "@tiqr.events"
}

func digitsOnlyPhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
