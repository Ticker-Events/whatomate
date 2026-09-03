package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
)

const (
	checkoutSessionKey       = "checkout"
	checkoutButtonID         = "checkout"
	checkoutExploreButtonID  = "checkout_explore"
	checkoutConfirmButtonID  = "checkout_confirm"
	checkoutCancelButtonID   = "checkout_cancel"
	checkoutPickupButtonID   = "checkout_delivery_pickup"
	checkoutDeliveryButtonID = "checkout_delivery_ship"
	cartPendingOptionKey     = "cart_pending_option_id"
	checkoutLocationPrompt   = "Please tap Send location to share your delivery pin so we can check if we deliver to you."
	checkoutAddressPrompt    = "Please provide your full delivery address, including your name, phone number, street address, city, state, country, and pincode."
	checkoutExploreAck       = "No problem — keep browsing. Your cart is saved. Tap Checkout when you're ready."
)

var (
	simpleEmailRE  = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	indianPINRE    = regexp.MustCompile(`\b([1-9]\d{5})\b`)
	indianPhoneRE  = regexp.MustCompile(`(?:\+?91[\s-]*)?([6-9]\d{9})\b`)
	addressLabelRE = regexp.MustCompile(`(?i)^\s*(name|phone|mobile|street|address|city|state|country|pin\s*code|pincode|postal)\s*[:\-]\s*(.+)$`)
	firstQtyRE     = regexp.MustCompile(`(?i)(?:^|\b)(\d{1,4})\b`)
)

type checkoutState struct {
	Step             string
	Email            string
	DeliveryMode     string
	NewAddress       map[string]any
	Latitude         float64
	Longitude        float64
	HasLocation      bool
	DeliveryZone     string
	ShippingFeePaise int64
}

func getCheckoutState(session *models.ChatbotSession) *checkoutState {
	if session == nil || session.SessionData == nil {
		return nil
	}
	raw, ok := session.SessionData[checkoutSessionKey].(map[string]any)
	if !ok || raw == nil {
		return nil
	}
	st := &checkoutState{
		Step:         asString(raw["step"]),
		Email:        asString(raw["email"]),
		DeliveryMode: asString(raw["delivery_mode"]),
	}
	if addr, ok := raw["new_address"].(map[string]any); ok {
		st.NewAddress = addr
	} else {
		st.NewAddress = map[string]any{}
	}
	if lat, ok := anyToFloat64(raw["latitude"]); ok {
		st.Latitude = lat
	}
	if lng, ok := anyToFloat64(raw["longitude"]); ok {
		st.Longitude = lng
	}
	if has, ok := raw["has_location"].(bool); ok {
		st.HasLocation = has
	} else {
		st.HasLocation = st.Latitude != 0 || st.Longitude != 0
	}
	st.DeliveryZone = asString(raw["delivery_zone"])
	if fee, ok := anyToFloat64(raw["shipping_fee_paise"]); ok {
		st.ShippingFeePaise = int64(fee)
	}
	if st.Step == "" {
		return nil
	}
	return st
}

func setCheckoutState(session *models.ChatbotSession, st *checkoutState) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	if st == nil || st.Step == "" {
		delete(session.SessionData, checkoutSessionKey)
		return
	}
	session.SessionData[checkoutSessionKey] = map[string]any{
		"step":               st.Step,
		"email":              st.Email,
		"delivery_mode":      st.DeliveryMode,
		"new_address":        st.NewAddress,
		"latitude":           st.Latitude,
		"longitude":          st.Longitude,
		"has_location":       st.HasLocation,
		"delivery_zone":      st.DeliveryZone,
		"shipping_fee_paise": st.ShippingFeePaise,
	}
}

func clearCheckoutState(session *models.ChatbotSession) {
	if session != nil && session.SessionData != nil {
		delete(session.SessionData, checkoutSessionKey)
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func anyToFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// IsCheckoutButton reports commerce checkout interactive button ids.
func IsCheckoutButton(buttonID string) bool {
	switch buttonID {
	case checkoutButtonID, checkoutExploreButtonID, checkoutConfirmButtonID, checkoutCancelButtonID,
		checkoutPickupButtonID, checkoutDeliveryButtonID:
		return true
	default:
		return false
	}
}

func (a *App) handleCommerceButtonTap(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, buttonID string) bool {
	if buttonID == "" {
		return false
	}
	switch {
	case IsAddToCartButton(buttonID):
		a.handleAddToCartProductTap(account, contact, session, settings, buttonID)
		return true
	case IsAddOptionButton(buttonID):
		a.handleAddOptionTap(account, contact, session, settings, buttonID)
		return true
	case IsCheckoutButton(buttonID):
		a.handleCheckoutButtonTap(account, contact, session, settings, buttonID)
		return true
	default:
		return false
	}
}

func (a *App) handleCheckoutButtonTap(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, buttonID string) {
	switch buttonID {
	case checkoutButtonID:
		a.startCheckout(account, contact, session, settings)
	case checkoutExploreButtonID:
		a.exitCheckoutToBrowse(account, contact, session, checkoutExploreAck, false)
	case checkoutPickupButtonID:
		a.handleCheckoutDeliveryChoice(account, contact, session, settings, "PICKUP_FROM_STORE")
	case checkoutDeliveryButtonID:
		a.handleCheckoutDeliveryChoice(account, contact, session, settings, "DELIVERY_TO_LOCATION")
	case checkoutConfirmButtonID:
		a.placeCheckoutOrder(account, contact, session, settings)
	case checkoutCancelButtonID:
		a.exitCheckoutToBrowse(account, contact, session, "Checkout cancelled. Your cart is still saved.", true)
	}
}

func (a *App) startCheckout(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings) {
	if cartIsEmpty(session) {
		_ = a.sendAndSaveTextMessage(account, contact, "Your cart is empty. Add items before checking out.")
		return
	}
	clearCartPendingOption(session)
	summary := formatCheckoutCartSummary(session)
	_ = a.sendAndSaveTextMessage(account, contact, summary)

	st := &checkoutState{NewAddress: map[string]any{}}
	st.Step = "email"
	setCheckoutState(session, st)
	_ = a.persistSessionData(session)
	_ = a.sendAndSaveTextMessage(account, contact, "Please share your email address to continue checkout.")
}

func (a *App) sendDeliveryModeButtons(account *models.WhatsAppAccount, contact *models.Contact) {
	_ = a.sendAndSaveInteractiveButtons(account, contact, "How would you like to receive your order?", []map[string]any{
		{"id": checkoutPickupButtonID, "title": "Store Pickup"},
		{"id": checkoutDeliveryButtonID, "title": "Delivery"},
	})
}

func (a *App) handleCheckoutDeliveryChoice(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, mode string) {
	st := getCheckoutState(session)
	if st == nil {
		return
	}
	st.DeliveryMode = mode
	if mode == "DELIVERY_TO_LOCATION" {
		if contact != nil {
			if contact.ProfileName != "" {
				st.NewAddress["name"] = contact.ProfileName
			}
			if contact.PhoneNumber != "" {
				st.NewAddress["phone"] = contact.PhoneNumber
			}
		}
		st.NewAddress["email"] = st.Email
		st.NewAddress["country"] = "India"

		if a.storeRequiresLocationBasedDelivery(session, settings) {
			st.Step = "location"
			st.HasLocation = false
			st.Latitude = 0
			st.Longitude = 0
			st.DeliveryZone = ""
			st.ShippingFeePaise = 0
			setCheckoutState(session, st)
			_ = a.persistSessionData(session)
			_ = a.sendAndSaveLocationRequest(account, contact, checkoutLocationPrompt)
			return
		}

		// Flag off (default): text address only — deliver to all locations.
		st.Step = "address"
		st.HasLocation = false
		st.DeliveryZone = ""
		st.ShippingFeePaise = 0
		setCheckoutState(session, st)
		_ = a.persistSessionData(session)
		_ = a.sendAndSaveTextMessage(account, contact, checkoutAddressPrompt)
		return
	}
	st.Step = "confirm"
	st.DeliveryZone = ""
	st.ShippingFeePaise = 0
	setCheckoutState(session, st)
	_ = a.persistSessionData(session)
	a.sendOrderConfirmPrompt(account, contact, session)
}

// storeRequiresLocationBasedDelivery reports whether the commerce store opts into
// lat/lng delivery checks (Store.location_based_delivery). Defaults to false.
func (a *App) storeRequiresLocationBasedDelivery(session *models.ChatbotSession, settings *models.ChatbotSettings) bool {
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil || rt.Client == nil {
		return false
	}
	store, err := rt.Client.GetStore(context.Background(), rt.StoreID)
	if err != nil || store == nil {
		a.Log.Warn("get_store for location_based_delivery failed; skipping pin check", "error", err)
		return false
	}
	switch v := store["location_based_delivery"].(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || v == "1"
	default:
		return false
	}
}

func (a *App) sendOrderConfirmPrompt(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession) {
	st := getCheckoutState(session)
	msg := formatOrderConfirmSummary(session, st)
	_ = a.sendAndSaveInteractiveButtons(account, contact, msg, []map[string]any{
		{"id": checkoutConfirmButtonID, "title": "Confirm order"},
		{"id": checkoutCancelButtonID, "title": "Cancel"},
	})
}

func (a *App) handleCheckoutConversation(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, messageText, buttonID string) bool {
	if buttonID != "" && IsCheckoutButton(buttonID) {
		return false // handled by handleCommerceButtonTap
	}
	st := getCheckoutState(session)
	if st == nil {
		return false
	}

	text := strings.TrimSpace(messageText)
	if text == "" {
		a.repromptCheckoutStep(account, contact, session, st)
		return true
	}

	// Qty / edit intents take priority over step validation so users can fix the cart mid-checkout.
	if a.handleCheckoutCartEditIntent(account, contact, session, st, text) {
		return true
	}

	switch st.Step {
	case "email":
		if !simpleEmailRE.MatchString(text) {
			_ = a.sendAndSaveTextMessage(account, contact, "Please enter a valid email address.")
			return true
		}
		st.Email = text
		st.Step = "delivery_mode"
		setCheckoutState(session, st)
		_ = a.persistSessionData(session)
		a.sendDeliveryModeButtons(account, contact)
		return true
	case "delivery_mode":
		// Buttons are preferred; free text must not fall through to the LLM.
		lower := strings.ToLower(text)
		switch {
		case strings.Contains(lower, "pickup") || lower == "store pickup":
			a.handleCheckoutDeliveryChoice(account, contact, session, settings, "PICKUP_FROM_STORE")
		case strings.Contains(lower, "deliver") || lower == "ship" || lower == "shipping":
			a.handleCheckoutDeliveryChoice(account, contact, session, settings, "DELIVERY_TO_LOCATION")
		default:
			a.sendDeliveryModeButtons(account, contact)
		}
		return true
	case "location":
		_ = a.sendAndSaveTextMessage(account, contact, "Please use the Send location button to share your delivery pin.")
		_ = a.sendAndSaveLocationRequest(account, contact, checkoutLocationPrompt)
		return true
	case "address", "address_line_1", "city", "state", "pincode":
		// Single full-address reply (legacy multi-step keys still accepted mid-session).
		parsed := parseDeliveryAddressText(text, contact, st)
		if asString(parsed["pincode"]) == "" || asString(parsed["address_line_1"]) == "" {
			_ = a.sendAndSaveTextMessage(account, contact, "Thanks — please include your street address and a 6-digit pincode.\n\n"+checkoutAddressPrompt)
			return true
		}
		st.NewAddress = parsed
		st.Step = "confirm"
		setCheckoutState(session, st)
		_ = a.persistSessionData(session)
		a.sendOrderConfirmPrompt(account, contact, session)
		return true
	case "confirm":
		if isCheckoutConfirmYes(text) {
			a.placeCheckoutOrder(account, contact, session, settings)
			return true
		}
		_ = a.sendAndSaveTextMessage(account, contact, "Please tap Confirm order or Cancel to continue.")
		a.sendOrderConfirmPrompt(account, contact, session)
		return true
	default:
		a.repromptCheckoutStep(account, contact, session, st)
		return true
	}
}

// handleCheckoutLocationPin processes a WhatsApp location share during the location checkout step.
func (a *App) handleCheckoutLocationPin(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, lat, lng float64) bool {
	st := getCheckoutState(session)
	if st == nil || st.Step != "location" {
		return false
	}

	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		_ = a.sendAndSaveTextMessage(account, contact, "Checkout is not available right now. Please try again later.")
		return true
	}

	ctx := context.Background()
	result, err := rt.Client.CheckDeliveryEligibility(ctx, rt.StoreID, lat, lng)
	if err != nil {
		a.Log.Error("check_delivery_eligibility failed", "error", err)
		_ = a.sendAndSaveTextMessage(account, contact, "We couldn’t verify delivery for that location right now. Please try sharing your location again.")
		_ = a.sendAndSaveLocationRequest(account, contact, checkoutLocationPrompt)
		return true
	}

	deliverable, _ := result["deliverable"].(bool)
	msg := asString(result["message"])
	zone := asString(result["zone"])
	feePaise := int64(0)
	if fee, ok := anyToFloat64(result["shipping_fee_paise"]); ok {
		feePaise = int64(fee)
	}

	if !deliverable {
		if msg == "" {
			msg = "Sorry, we can’t deliver to this location yet. Please share a location closer to the store, or choose Store Pickup."
		}
		_ = a.sendAndSaveTextMessage(account, contact, msg)
		a.sendDeliveryModeButtons(account, contact)
		st.Step = "delivery_mode"
		st.HasLocation = false
		setCheckoutState(session, st)
		_ = a.persistSessionData(session)
		return true
	}

	st.Latitude = lat
	st.Longitude = lng
	st.HasLocation = true
	st.DeliveryZone = zone
	st.ShippingFeePaise = feePaise
	st.Step = "address"
	setCheckoutState(session, st)
	_ = a.persistSessionData(session)

	ack := "Thanks — we can deliver to that location."
	if zone == "free" || feePaise == 0 {
		ack = "Great news — delivery is free to that location."
	} else if feePaise > 0 {
		ack = fmt.Sprintf("We can deliver there. Delivery fee: %s.", formatPriceINR(ticker.PaiseToRupees(float64(feePaise))))
	}
	_ = a.sendAndSaveTextMessage(account, contact, ack)
	_ = a.sendAndSaveTextMessage(account, contact, checkoutAddressPrompt)
	return true
}

// handleCheckoutCartEditIntent handles qty updates and exit-to-browse during checkout.
// Returns true when the message was consumed as a cart-edit intent.
func (a *App) handleCheckoutCartEditIntent(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, st *checkoutState, text string) bool {
	if isCheckoutEditExitIntent(text) {
		qty := extractQtyFromText(text)
		if qty > 0 {
			return a.applyCheckoutQtyOrPause(account, contact, session, st, qty)
		}
		a.exitCheckoutToBrowse(account, contact, session, "Checkout paused. Your cart is still saved — update it or keep browsing, then tap Checkout when you're ready.", true)
		return true
	}

	// Bare positive integer → qty for sole cart line.
	if qty := parsePositiveInt(text); qty > 0 {
		return a.applyCheckoutQtyOrPause(account, contact, session, st, qty)
	}

	// Phrases that include a quantity (e.g. "i want 2", "change to 2").
	if isCheckoutQtyPhrase(text) {
		if qty := extractQtyFromText(text); qty > 0 {
			return a.applyCheckoutQtyOrPause(account, contact, session, st, qty)
		}
	}

	if isCheckoutCancelText(text) {
		a.exitCheckoutToBrowse(account, contact, session, "Checkout cancelled. Your cart is still saved.", true)
		return true
	}
	return false
}

func (a *App) applyCheckoutQtyOrPause(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, st *checkoutState, qty int) bool {
	optID, ok := soleCartOptionID(session)
	if !ok {
		clearCheckoutState(session)
		_ = a.persistSessionData(session)
		summary := formatCheckoutCartSummary(session)
		_ = a.sendAndSaveTextMessage(account, contact, summary+"\n\nCheckout paused — your cart has multiple items. Tell me which item to change, or keep browsing and tap Checkout when ready.")
		a.sendCheckoutButtonPrompt(account, contact)
		return true
	}
	optKey := fmt.Sprintf("%d", optID)
	if !setCartLineQty(session, optKey, qty) {
		_ = a.sendAndSaveTextMessage(account, contact, "Couldn't update that quantity. Please try again.")
		return true
	}
	_ = a.persistSessionData(session)
	name := cartOptionName(session, optKey)
	_ = a.sendAndSaveTextMessage(account, contact, fmt.Sprintf("Updated quantity to %d for %s.\n\n%s", qty, name, formatCheckoutCartSummary(session)))
	a.repromptCheckoutStep(account, contact, session, st)
	return true
}

func (a *App) exitCheckoutToBrowse(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, message string, showCart bool) {
	clearCheckoutState(session)
	if optID, ok := soleCartOptionID(session); ok {
		setCartPendingOption(session, optID)
	} else {
		clearCartPendingOption(session)
	}
	_ = a.persistSessionData(session)
	if showCart && !cartIsEmpty(session) {
		_ = a.sendAndSaveTextMessage(account, contact, formatCheckoutCartSummary(session))
	}
	if message != "" {
		_ = a.sendAndSaveTextMessage(account, contact, message)
	}
	a.sendCheckoutButtonPrompt(account, contact)
}

func (a *App) repromptCheckoutStep(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, st *checkoutState) {
	if st == nil {
		return
	}
	switch st.Step {
	case "email":
		_ = a.sendAndSaveTextMessage(account, contact, "Please share your email address to continue checkout.")
	case "delivery_mode":
		a.sendDeliveryModeButtons(account, contact)
	case "location":
		_ = a.sendAndSaveLocationRequest(account, contact, checkoutLocationPrompt)
	case "address", "address_line_1", "city", "state", "pincode":
		_ = a.sendAndSaveTextMessage(account, contact, checkoutAddressPrompt)
	case "confirm":
		a.sendOrderConfirmPrompt(account, contact, session)
	default:
		_ = a.sendAndSaveTextMessage(account, contact, "Please tap Checkout to continue.")
	}
}

func soleCartOptionID(session *models.ChatbotSession) (int, bool) {
	if session == nil || session.SessionData == nil {
		return 0, false
	}
	cart := normalizeCartMap(session.SessionData[cartKey])
	if len(cart) != 1 {
		return 0, false
	}
	for key, line := range cart {
		optID := anyToInt(key)
		if optID <= 0 {
			optID = anyToInt(line["option_id"])
		}
		if optID <= 0 {
			return 0, false
		}
		return optID, true
	}
	return 0, false
}

func extractQtyFromText(text string) int {
	if n := parsePositiveInt(text); n > 0 {
		return n
	}
	m := firstQtyRE.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	return parsePositiveInt(m[1])
}

func isCheckoutEditExitIntent(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	phrases := []string{
		"update cart", "update the cart", "edit cart", "edit the cart",
		"change cart", "change the cart", "change quantity", "change qty",
		"update quantity", "update qty", "modify cart", "explore more", "keep browsing",
		"browse more", "continue browsing", "cancel checkout",
	}
	for _, p := range phrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	// Short forms like "change" / "update" alone or with filler.
	if lower == "change" || lower == "update" || lower == "edit" {
		return true
	}
	if strings.HasPrefix(lower, "change,") || strings.HasPrefix(lower, "change ") {
		return true
	}
	if strings.HasPrefix(lower, "update ") || strings.HasPrefix(lower, "edit ") {
		return true
	}
	return false
}

func isCheckoutQtyPhrase(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if extractQtyFromText(lower) <= 0 {
		return false
	}
	markers := []string{"want", "qty", "quantity", "change", "update", "make it", "set to", "x"}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func isCheckoutConfirmYes(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "yes", "y", "confirm", "ok", "okay", "place order", "confirm order":
		return true
	default:
		return false
	}
}

func isCheckoutCancelText(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "cancel", "no", "stop", "nevermind", "never mind":
		return true
	default:
		return false
	}
}

// parseDeliveryAddressText builds a new_address map from a single free-form reply.
// Prefills name/phone/email/country from checkout state and contact when missing.
func parseDeliveryAddressText(text string, contact *models.Contact, st *checkoutState) map[string]any {
	out := map[string]any{}
	if st != nil && st.NewAddress != nil {
		for k, v := range st.NewAddress {
			out[k] = v
		}
	}
	if contact != nil {
		if asString(out["name"]) == "" && contact.ProfileName != "" {
			out["name"] = contact.ProfileName
		}
		if asString(out["phone"]) == "" && contact.PhoneNumber != "" {
			out["phone"] = contact.PhoneNumber
		}
	}
	if st != nil && st.Email != "" {
		out["email"] = st.Email
	}
	if asString(out["country"]) == "" {
		out["country"] = "India"
	}

	// Labeled lines: "City: Bengaluru"
	var unlabeled []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := addressLabelRE.FindStringSubmatch(line); len(m) == 3 {
			key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(m[1]), " ", ""))
			val := strings.TrimSpace(m[2])
			switch key {
			case "name":
				out["name"] = val
			case "phone", "mobile":
				out["phone"] = val
			case "street", "address":
				out["address_line_1"] = val
			case "city":
				out["city"] = val
			case "state":
				out["state"] = val
			case "country":
				out["country"] = val
			case "pincode", "pin", "postal":
				out["pincode"] = val
			}
			continue
		}
		unlabeled = append(unlabeled, line)
	}

	blob := strings.Join(unlabeled, ", ")
	if blob == "" {
		blob = strings.TrimSpace(text)
	}

	if pin := indianPINRE.FindString(blob); pin != "" {
		out["pincode"] = pin
	}
	if asString(out["phone"]) == "" {
		if m := indianPhoneRE.FindStringSubmatch(blob); len(m) == 2 {
			out["phone"] = m[1]
		}
	}

	// Comma-separated freeform: street..., city, state[, country][, pin]
	parts := splitAddressParts(blob)
	if asString(out["address_line_1"]) == "" && len(parts) > 0 {
		// Drop trailing pin/phone-only tokens from street when possible.
		streetParts := make([]string, 0, len(parts))
		for _, p := range parts {
			if indianPINRE.MatchString(p) && len(strings.TrimSpace(p)) == 6 {
				continue
			}
			if indianPhoneRE.MatchString(p) && len(digitsOnly(p)) >= 10 {
				continue
			}
			streetParts = append(streetParts, p)
		}
		if len(streetParts) >= 3 {
			out["state"] = streetParts[len(streetParts)-1]
			out["city"] = streetParts[len(streetParts)-2]
			out["address_line_1"] = strings.Join(streetParts[:len(streetParts)-2], ", ")
		} else if len(streetParts) == 2 {
			out["city"] = streetParts[1]
			out["address_line_1"] = streetParts[0]
		} else if len(streetParts) == 1 {
			out["address_line_1"] = streetParts[0]
		}
	}
	if asString(out["address_line_1"]) == "" {
		out["address_line_1"] = blob
	}
	if asString(out["city"]) == "" {
		out["city"] = "—"
	}
	if asString(out["state"]) == "" {
		out["state"] = "—"
	}
	return out
}

func splitAddressParts(s string) []string {
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// checkoutBuyerMeta mirrors web checkout: store UI reads shipping_address from
// buyer_meta_data (Order.address FK alone is not enough for tiqr.store).
func checkoutBuyerMeta(st *checkoutState, contact *models.Contact) map[string]any {
	meta := map[string]any{}
	if st == nil {
		return meta
	}
	if st.Email != "" {
		meta["email"] = st.Email
	}
	addr := st.NewAddress
	if addr == nil {
		addr = map[string]any{}
	}
	if name := asString(addr["name"]); name != "" {
		meta["name"] = name
	} else if contact != nil && contact.ProfileName != "" {
		meta["name"] = contact.ProfileName
	}
	if phone := asString(addr["phone"]); phone != "" {
		meta["phone"] = phone
	} else if contact != nil && contact.PhoneNumber != "" {
		meta["phone"] = contact.PhoneNumber
	}
	if st.HasLocation {
		meta["latitude"] = st.Latitude
		meta["longitude"] = st.Longitude
	}
	if st.DeliveryMode == "DELIVERY_TO_LOCATION" && len(addr) > 0 {
		// Copy without mutating session state; drop write-only email for display blob.
		shipping := make(map[string]any, len(addr))
		for k, v := range addr {
			if k == "email" {
				continue
			}
			shipping[k] = v
		}
		if len(shipping) > 0 {
			meta["shipping_address"] = shipping
			meta["billing_address"] = shipping
		}
	}
	return meta
}

func (a *App) placeCheckoutOrder(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings) {
	st := getCheckoutState(session)
	if st == nil || cartIsEmpty(session) {
		_ = a.sendAndSaveTextMessage(account, contact, "Checkout session expired. Tap Checkout to try again.")
		clearCheckoutState(session)
		_ = a.persistSessionData(session)
		return
	}
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		_ = a.sendAndSaveTextMessage(account, contact, "Checkout is not available right now. Please try again later.")
		return
	}

	items := cartOrderItems(session)
	orderItems := make([]map[string]any, 0, len(items))
	for _, it := range items {
		orderItems = append(orderItems, map[string]any{
			"product_option": it.ProductOption,
			"quantity":       it.Quantity,
		})
	}
	args := createOrderArgs{
		Confirmed:     true,
		Items:         orderItems,
		Email:         st.Email,
		DeliveryMode:  st.DeliveryMode,
		NewAddress:    st.NewAddress,
		BuyerMetaData: checkoutBuyerMeta(st, contact),
	}
	if st.DeliveryMode == "" {
		args.DeliveryMode = "PICKUP_FROM_STORE"
	}
	// Address.phone is max_length=15 on the order server.
	if args.NewAddress != nil {
		if phone := asString(args.NewAddress["phone"]); len(phone) > 15 {
			args.NewAddress["phone"] = phone[len(phone)-15:]
		}
	}

	ctx := context.Background()
	result, err := placeCommerceOrder(ctx, rt, args)
	if err != nil {
		a.Log.Error("checkout create order failed", "error", err)
		_ = a.sendAndSaveTextMessage(account, contact, "Sorry, we could not place your order. Please try again.")
		return
	}

	clearCart(session)
	clearCheckoutState(session)
	clearCartPendingOption(session)
	_ = a.persistSessionData(session)

	msg := formatOrderSuccessMessage(result)
	_ = a.sendAndSaveTextMessage(account, contact, msg)
}

func formatOrderSuccessMessage(result map[string]any) string {
	var b strings.Builder
	displayUID, _ := result["display_uid"].(string)
	if displayUID != "" {
		fmt.Fprintf(&b, "Order placed! Your order number is %s.", displayUID)
	} else {
		b.WriteString("Order placed!")
	}
	if fee, ok := anyToFloat64(result["shipping_fee"]); ok {
		if fee > 0 {
			fmt.Fprintf(&b, "\nDelivery fee: %s", formatPriceINR(fee))
		} else {
			b.WriteString("\nDelivery fee: Free")
		}
	}
	if amount, ok := result["amount"].(float64); ok && amount > 0 {
		fmt.Fprintf(&b, "\nTotal: %s", formatPriceINR(amount))
	}
	if url, _ := result["payment_url"].(string); url != "" {
		fmt.Fprintf(&b, "\nPay here: %s", url)
	}
	return strings.TrimSpace(b.String())
}

func formatCheckoutCartSummary(session *models.ChatbotSession) string {
	cart := normalizeCartMap(session.SessionData[cartKey])
	if len(cart) == 0 {
		return "Your cart is empty."
	}
	var b strings.Builder
	b.WriteString("Your cart:\n")
	var total float64
	for _, line := range cart {
		meta, _ := line["product"].(map[string]any)
		name := cartLineOptionName(meta)
		qty := anyToInt(line["qty"])
		if qty < 1 {
			qty = 1
		}
		price := cartLinePrice(meta)
		lineTotal := price * float64(qty)
		total += lineTotal
		fmt.Fprintf(&b, "- %s x%d — %s\n", name, qty, formatPriceINR(lineTotal))
	}
	fmt.Fprintf(&b, "\nSubtotal: %s", formatPriceINR(total))
	return b.String()
}

func formatOrderConfirmSummary(session *models.ChatbotSession, st *checkoutState) string {
	var b strings.Builder
	cartSubtotal := checkoutCartSubtotal(session)
	b.WriteString(formatCheckoutCartSummary(session))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Email: %s\n", st.Email)
	mode := st.DeliveryMode
	if mode == "" {
		mode = "PICKUP_FROM_STORE"
	}
	if mode == "PICKUP_FROM_STORE" {
		b.WriteString("Delivery: Store pickup\n")
		b.WriteString("Delivery fee: Free (pickup)\n")
	} else {
		b.WriteString("Delivery: To your address\n")
		if st.NewAddress != nil {
			if v := asString(st.NewAddress["address_line_1"]); v != "" {
				fmt.Fprintf(&b, "Address: %s", v)
				if c := asString(st.NewAddress["city"]); c != "" {
					fmt.Fprintf(&b, ", %s", c)
				}
				if s := asString(st.NewAddress["state"]); s != "" {
					fmt.Fprintf(&b, ", %s", s)
				}
				if p := asString(st.NewAddress["pincode"]); p != "" {
					fmt.Fprintf(&b, " %s", p)
				}
				b.WriteString("\n")
			}
		}
		if st.DeliveryZone == "free" || st.ShippingFeePaise == 0 {
			b.WriteString("Delivery fee: Free\n")
		} else {
			fmt.Fprintf(&b, "Delivery fee: %s\n", formatPriceINR(ticker.PaiseToRupees(float64(st.ShippingFeePaise))))
		}
		grand := cartSubtotal + ticker.PaiseToRupees(float64(st.ShippingFeePaise))
		fmt.Fprintf(&b, "Estimated total: %s\n", formatPriceINR(grand))
	}
	b.WriteString("\nConfirm your order?")
	return b.String()
}

func checkoutCartSubtotal(session *models.ChatbotSession) float64 {
	if session == nil || session.SessionData == nil {
		return 0
	}
	cart := normalizeCartMap(session.SessionData[cartKey])
	var total float64
	for _, line := range cart {
		meta, _ := line["product"].(map[string]any)
		qty := anyToInt(line["qty"])
		if qty < 1 {
			qty = 1
		}
		total += cartLinePrice(meta) * float64(qty)
	}
	return total
}

func (a *App) handleCartQuantityReply(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, messageText string) bool {
	pendingID := getCartPendingOptionID(session)
	if pendingID == "" {
		return false
	}
	qty := parsePositiveInt(messageText)
	if qty <= 0 {
		clearCartPendingOption(session)
		_ = a.persistSessionData(session)
		return false
	}
	if !setCartLineQty(session, pendingID, qty) {
		clearCartPendingOption(session)
		_ = a.persistSessionData(session)
		return false
	}
	clearCartPendingOption(session)
	_ = a.persistSessionData(session)
	name := cartOptionName(session, pendingID)
	msg := fmt.Sprintf("Updated quantity to %d for %s.", qty, name)
	_ = a.sendAndSaveTextMessage(account, contact, msg)
	a.sendCheckoutButtonPrompt(account, contact)
	return true
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

func getCartPendingOptionID(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return ""
	}
	id, _ := session.SessionData[cartPendingOptionKey].(string)
	return strings.TrimSpace(id)
}

func setCartPendingOption(session *models.ChatbotSession, optionID int) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	session.SessionData[cartPendingOptionKey] = fmt.Sprintf("%d", optionID)
}

func clearCartPendingOption(session *models.ChatbotSession) {
	if session != nil && session.SessionData != nil {
		delete(session.SessionData, cartPendingOptionKey)
	}
}

func clearCart(session *models.ChatbotSession) {
	if session != nil && session.SessionData != nil {
		delete(session.SessionData, cartKey)
	}
}

func cartIsEmpty(session *models.ChatbotSession) bool {
	if session == nil || session.SessionData == nil {
		return true
	}
	return len(normalizeCartMap(session.SessionData[cartKey])) == 0
}

func cartOrderItems(session *models.ChatbotSession) []ticker.OrderItem {
	if session == nil || session.SessionData == nil {
		return nil
	}
	cart := normalizeCartMap(session.SessionData[cartKey])
	items := make([]ticker.OrderItem, 0, len(cart))
	for optKey, line := range cart {
		optID := anyToInt(optKey)
		if optID <= 0 {
			optID = anyToInt(line["option_id"])
		}
		qty := anyToInt(line["qty"])
		if optID <= 0 || qty <= 0 {
			continue
		}
		items = append(items, ticker.OrderItem{ProductOption: optID, Quantity: qty})
	}
	return items
}
