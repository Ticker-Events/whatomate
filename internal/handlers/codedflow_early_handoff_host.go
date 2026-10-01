package handlers

import (
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

// earlyHandoffCheckoutStateFromSession builds checkoutState for coded-flow
// early handoff / ecommerce handoff staging. Lives in handlers so tiqrecommerce
// never imports the checkoutState type.
func earlyHandoffCheckoutStateFromSession(session *models.ChatbotSession) *checkoutState {
	if session == nil {
		return &checkoutState{Flow: checkoutFlowEarlyHandoff, Step: "confirm", NewAddress: map[string]any{}}
	}
	data := session.SessionData
	if data == nil {
		data = models.JSONB{}
	}
	st := &checkoutState{
		Flow:             checkoutFlowEarlyHandoff,
		Step:             "confirm",
		NewAddress:       map[string]any{},
		DeliveryMode:     asString(data["delivery_mode"]),
		SlotToken:        asString(data["fulfillment_slot_token"]),
		RequestedAt:      asString(data["requested_fulfillment_at"]),
		PromisedAt:       asString(data["promised_ready_at"]),
		Timezone:         asString(data["early_handoff_timezone"]),
		EarliestAt:       asString(data["early_handoff_earliest_at"]),
		PendingProductID: asString(data["early_handoff_product_id"]),
		Email:            contextEmail(data),
	}
	if st.DeliveryMode == "" {
		st.DeliveryMode = "PICKUP_FROM_STORE"
	}
	st.NewAddress = earlyHandoffAddressFromFlowHandlers(data)
	if lat, ok := anyToFloat64(data["delivery_latitude"]); ok {
		st.Latitude, st.HasLocation = lat, true
	}
	if lng, ok := anyToFloat64(data["delivery_longitude"]); ok {
		st.Longitude = lng
		st.HasLocation = true
	}
	if zone := asString(data["delivery_zone"]); zone != "" {
		st.DeliveryZone = zone
	}
	if fee, ok := anyToFloat64(data["shipping_fee_paise"]); ok {
		st.ShippingFeePaise = int64(fee)
	}
	if notes := strings.TrimSpace(contextValue(data, "customer_notes", "notes")); notes != "" {
		existing := jsonMapFromSession(session, "commerce_notes")
		if asString(existing["customer_notes"]) == "" {
			existing["customer_notes"] = notes
			session.SessionData["commerce_notes"] = map[string]any(existing)
		}
	}
	return st
}

func earlyHandoffAddressFromFlowHandlers(data map[string]any) map[string]any {
	name := contextValue(data, "customer_name", "name")
	phone := contextValue(data, "customer_phone", "phone", "phone_number")
	email := contextEmail(data)
	addr := map[string]any{}
	if name != "" {
		addr["name"] = name
	}
	if phone != "" {
		addr["phone"] = phone
		addr["phone_number"] = phone
	}
	if email != "" {
		addr["email"] = email
	}
	if line := contextValue(data, "address_line_one", "address_line_1"); line != "" {
		addr["address_line_1"] = line
	}
	if line := contextValue(data, "address_line_two", "address_line_2"); line != "" {
		addr["address_line_2"] = line
	}
	for _, key := range []string{"city", "state", "country", "pincode"} {
		if value := contextValue(data, key); value != "" {
			addr[key] = value
		}
	}
	if lat, ok := anyToFloat64(data["delivery_latitude"]); ok {
		addr["latitude"] = lat
	}
	if lng, ok := anyToFloat64(data["delivery_longitude"]); ok {
		addr["longitude"] = lng
	}
	return addr
}


func contextEmail(data map[string]any) string {
	return contextValue(data, "customer_email", "email")
}

func contextValue(data map[string]any, keys ...string) string {
	if data == nil {
		return ""
	}
	for _, key := range keys {
		if v := strings.TrimSpace(asString(data[key])); v != "" {
			return v
		}
	}
	return ""
}
