package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
)

const (
	checkoutSessionKey       = "checkout"
	checkoutButtonID         = "checkout"
	checkoutConfirmButtonID  = "checkout_confirm"
	checkoutCancelButtonID   = "checkout_cancel"
	checkoutPickupButtonID   = "checkout_delivery_pickup"
	checkoutDeliveryButtonID = "checkout_delivery_ship"
	cartPendingOptionKey     = "cart_pending_option_id"
	checkoutAddressPrompt    = "Please provide your full delivery address, including your name, phone number, street address, city, state, country, and pincode."
)

var (
	simpleEmailRE  = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	indianPINRE    = regexp.MustCompile(`\b([1-9]\d{5})\b`)
	indianPhoneRE  = regexp.MustCompile(`(?:\+?91[\s-]*)?([6-9]\d{9})\b`)
	addressLabelRE = regexp.MustCompile(`(?i)^\s*(name|phone|mobile|street|address|city|state|country|pin\s*code|pincode|postal)\s*[:\-]\s*(.+)$`)
)

type checkoutState struct {
	Step         string
	Email        string
	DeliveryMode string
	NewAddress   map[string]any
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
		"step":          st.Step,
		"email":         st.Email,
		"delivery_mode": st.DeliveryMode,
		"new_address":   st.NewAddress,
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

// IsCheckoutButton reports commerce checkout interactive button ids.
func IsCheckoutButton(buttonID string) bool {
	switch buttonID {
	case checkoutButtonID, checkoutConfirmButtonID, checkoutCancelButtonID,
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
	case checkoutPickupButtonID:
		a.handleCheckoutDeliveryChoice(account, contact, session, "PICKUP_FROM_STORE")
	case checkoutDeliveryButtonID:
		a.handleCheckoutDeliveryChoice(account, contact, session, "DELIVERY_TO_LOCATION")
	case checkoutConfirmButtonID:
		a.placeCheckoutOrder(account, contact, session, settings)
	case checkoutCancelButtonID:
		clearCheckoutState(session)
		_ = a.persistSessionData(session)
		_ = a.sendAndSaveTextMessage(account, contact, "Checkout cancelled. Your cart is still saved.")
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

func (a *App) handleCheckoutDeliveryChoice(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, mode string) {
	st := getCheckoutState(session)
	if st == nil {
		return
	}
	st.DeliveryMode = mode
	if mode == "DELIVERY_TO_LOCATION" {
		st.Step = "address"
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
		setCheckoutState(session, st)
		_ = a.persistSessionData(session)
		_ = a.sendAndSaveTextMessage(account, contact, checkoutAddressPrompt)
		return
	}
	st.Step = "confirm"
	setCheckoutState(session, st)
	_ = a.persistSessionData(session)
	a.sendOrderConfirmPrompt(account, contact, session)
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
	case "address", "address_line_1", "city", "state", "pincode":
		// Single full-address reply (legacy multi-step keys still accepted mid-session).
		if text == "" {
			_ = a.sendAndSaveTextMessage(account, contact, checkoutAddressPrompt)
			return true
		}
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
		Confirmed:    true,
		Items:        orderItems,
		Email:        st.Email,
		DeliveryMode: st.DeliveryMode,
		NewAddress:   st.NewAddress,
	}
	if st.DeliveryMode == "" {
		args.DeliveryMode = "PICKUP_FROM_STORE"
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
	fmt.Fprintf(&b, "\nEstimated total: %s", formatPriceINR(total))
	return b.String()
}

func formatOrderConfirmSummary(session *models.ChatbotSession, st *checkoutState) string {
	var b strings.Builder
	b.WriteString(formatCheckoutCartSummary(session))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Email: %s\n", st.Email)
	mode := st.DeliveryMode
	if mode == "" {
		mode = "PICKUP_FROM_STORE"
	}
	if mode == "PICKUP_FROM_STORE" {
		b.WriteString("Delivery: Store pickup\n")
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
	}
	b.WriteString("\nConfirm your order?")
	return b.String()
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
