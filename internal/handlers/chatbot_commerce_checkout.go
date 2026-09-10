package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	draftrepo "github.com/shridarpatil/whatomate/internal/commerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

const (
	checkoutSessionKey           = "checkout"
	checkoutButtonID             = "checkout"
	checkoutExploreButtonID      = "checkout_explore"
	checkoutConfirmButtonID      = "checkout_confirm"
	checkoutCancelButtonID       = "checkout_cancel"
	checkoutPickupButtonID       = "checkout_delivery_pickup"
	checkoutDeliveryButtonID     = "checkout_delivery_ship"
	checkoutNewAddressButtonID   = "checkout_address_new"
	checkoutSlotPrefix           = "checkout_slot_"
	checkoutAddressPrefix        = "checkout_address_"
	cartPendingOptionKey         = "cart_pending_option_id"
	checkoutLocationPrompt       = "Please tap Send location to share your delivery pin so we can check if we deliver to you."
	checkoutAddressPrompt        = "Please provide your full delivery address, including your name, phone number, street address, city, state, country, and pincode."
	checkoutAddressMessageBody   = "Thanks for your order! Tell us what address you'd like this order delivered to."
	checkoutExploreAck           = "No problem — keep browsing. Your cart is saved. Tap Checkout when you're ready."
	whatsappAddressIncapableCode = 1026
)

var (
	simpleEmailRE  = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	indianPINRE    = regexp.MustCompile(`\b([1-9]\d{5})\b`)
	indianPhoneRE  = regexp.MustCompile(`(?:\+?91[\s-]*)?([6-9]\d{9})\b`)
	addressLabelRE = regexp.MustCompile(`(?i)^\s*(name|phone|mobile|street|address|city|state|country|pin\s*code|pincode|postal)\s*[:\-]\s*(.+)$`)
	firstQtyRE     = regexp.MustCompile(`(?i)(?:^|\b)(\d{1,4})\b`)
	targetQtyRE    = regexp.MustCompile(`(?i)^(?:change|update|set)?\s*(.+?)\s+(?:to|x)?\s*(\d{1,4})$`)
	removeLineRE   = regexp.MustCompile(`(?i)^(?:remove|delete)\s+(.+)$`)
	addonEditRE    = regexp.MustCompile(`(?i)^(remove\s+)?addon\s+(\d+)(?:\s*(?:x|to)\s*(\d+))?$`)
	relativeTimeRE = regexp.MustCompile(`(?i)^\s*(?:in|after)\s+(\d+)\s*(m|min|mins|minute|minutes|h|hr|hrs|hour|hours)\s*$`)
	asapTimeRE     = regexp.MustCompile(`(?i)^\s*(asap|earliest|soonest|now|as soon as possible)\s*$`)
	absoluteTimeRE = regexp.MustCompile(`(?i)^\s*(today|tomorrow)?\s*(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*$`)
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
	SlotToken        string
	RequestedAt      string
	PromisedAt       string
	Timezone         string
	EarliestAt       string
	Slots            []map[string]any
	SavedAddressID   *int
	CaptureFields    []map[string]any
	CaptureIndex     int
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
	st.SlotToken = asString(raw["slot_token"])
	st.RequestedAt = asString(raw["requested_at"])
	st.PromisedAt = asString(raw["promised_at"])
	st.Timezone = asString(raw["timezone"])
	st.EarliestAt = asString(raw["earliest_at"])
	if slots, ok := raw["slots"].([]any); ok {
		for _, slot := range slots {
			if item, ok := slot.(map[string]any); ok {
				st.Slots = append(st.Slots, item)
			}
		}
	} else if slots, ok := raw["slots"].([]map[string]any); ok {
		st.Slots = slots
	}
	if id := anyToInt(raw["saved_address_id"]); id > 0 {
		st.SavedAddressID = &id
	}
	if fields, ok := raw["capture_fields"].([]any); ok {
		for _, field := range fields {
			if item, ok := field.(map[string]any); ok {
				st.CaptureFields = append(st.CaptureFields, item)
			}
		}
	} else if fields, ok := raw["capture_fields"].([]map[string]any); ok {
		st.CaptureFields = fields
	}
	st.CaptureIndex = anyToInt(raw["capture_index"])
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
		"slot_token":         st.SlotToken,
		"requested_at":       st.RequestedAt,
		"promised_at":        st.PromisedAt,
		"timezone":           st.Timezone,
		"earliest_at":        st.EarliestAt,
		"slots":              st.Slots,
		"saved_address_id":   pointerIntValue(st.SavedAddressID),
		"capture_fields":     st.CaptureFields,
		"capture_index":      st.CaptureIndex,
	}
}

func pointerIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
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
		checkoutPickupButtonID, checkoutDeliveryButtonID, checkoutNewAddressButtonID:
		return true
	default:
		return strings.HasPrefix(buttonID, checkoutSlotPrefix) || strings.HasPrefix(buttonID, checkoutAddressPrefix)
	}
}

func (a *App) handleCommerceButtonTap(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, buttonID string) bool {
	if buttonID == "" {
		return false
	}
	if a.handleDeterministicCommerceAction(account, contact, session, settings, buttonID) {
		return true
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
	if strings.HasPrefix(buttonID, checkoutSlotPrefix) {
		a.handleCheckoutSlotChoice(account, contact, session, settings, buttonID)
		return
	}
	if strings.HasPrefix(buttonID, checkoutAddressPrefix) && buttonID != checkoutNewAddressButtonID {
		a.handleCheckoutSavedAddressChoice(account, contact, session, settings, buttonID)
		return
	}
	switch buttonID {
	case checkoutButtonID:
		a.startCheckout(account, contact, session, settings)
	case checkoutExploreButtonID:
		a.exitCheckoutToBrowse(account, contact, session, checkoutExploreAck, false)
	case checkoutPickupButtonID:
		a.handleCheckoutDeliveryChoice(account, contact, session, settings, "PICKUP_FROM_STORE")
	case checkoutDeliveryButtonID:
		a.handleCheckoutDeliveryChoice(account, contact, session, settings, "DELIVERY_TO_LOCATION")
	case checkoutNewAddressButtonID:
		a.beginNewCheckoutAddress(account, contact, session, settings)
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

	st := &checkoutState{NewAddress: map[string]any{}, CaptureFields: a.checkoutCaptureFields(session, settings)}
	if len(st.CaptureFields) > 0 {
		st.Step = "capture"
	} else {
		st.Step = "email"
	}
	setCheckoutState(session, st)
	if _, err := a.ensureCommerceDraft(contact, session, settings); err != nil {
		a.Log.Error("create durable commerce draft failed", "error", err)
		_ = a.sendAndSaveTextMessage(account, contact, "Checkout is temporarily unavailable. Please try again.")
		return
	}
	_ = a.persistSessionData(session)
	a.repromptCheckoutStep(account, contact, session, settings, st)
}

func (a *App) checkoutCaptureFields(session *models.ChatbotSession, settings *models.ChatbotSettings) []map[string]any {
	categoryID := selectedCategoryID(session)
	if categoryID == "" {
		return nil
	}
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		return nil
	}
	defer rt.Close()
	page, err := rt.Client.ListCategoryPage(context.Background(), rt.StoreID, categoryID, 1, 0)
	if err != nil || len(page.Results) != 1 {
		return nil
	}
	captured := jsonMapFromSession(session, "commerce_captured_fields")
	fields := make([]map[string]any, 0, len(page.Results[0].RequiredCaptureFields))
	for _, field := range page.Results[0].RequiredCaptureFields {
		if !field.Required || captured[field.Key] != nil {
			continue
		}
		fields = append(fields, map[string]any{
			"key": field.Key, "label": field.Label, "type": field.Type, "options": field.Options, "help_text": field.HelpText,
		})
	}
	return fields
}

func promptCaptureField(field map[string]any) string {
	label := asString(field["label"])
	if help := asString(field["help_text"]); help != "" {
		label += "\n" + help
	}
	if options, ok := field["options"].([]any); ok && len(options) > 0 {
		values := make([]string, 0, len(options))
		for _, option := range options {
			values = append(values, fmt.Sprint(option))
		}
		label += "\nOptions: " + strings.Join(values, ", ")
	} else if options, ok := field["options"].([]string); ok && len(options) > 0 {
		label += "\nOptions: " + strings.Join(options, ", ")
	}
	return label
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
	st.Step = "slot"
	setCheckoutState(session, st)
	_ = a.persistSessionData(session)
	a.sendFulfillmentSlots(account, contact, session, settings, st)
}

func (a *App) sendFulfillmentSlots(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState) {
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		_ = a.sendAndSaveTextMessage(account, contact, "Fulfillment times are temporarily unavailable.")
		return
	}
	defer rt.Close()

	tzName := "UTC"
	earliestLabel := ""
	if store, err := rt.Client.GetStore(context.Background(), rt.StoreID); err == nil {
		if tz := asString(store["timezone"]); tz != "" {
			tzName = tz
		}
	}
	result, err := rt.Client.ListFulfillmentSlots(context.Background(), rt.StoreID, st.DeliveryMode, cartProductOptionIDs(session))
	if err != nil {
		a.Log.Warn("list fulfillment slots failed", "error", err)
		_ = a.sendAndSaveTextMessage(account, contact, "Couldn't load pickup/delivery times right now. Please try again in a moment.")
		a.sendDeliveryModeButtons(account, contact)
		return
	}
	if len(result.Slots) == 0 {
		a.Log.Warn("list fulfillment slots returned no options", "store_id", rt.StoreID, "mode", st.DeliveryMode)
		_ = a.sendAndSaveTextMessage(account, contact, "No pickup/delivery times are available over the next few days. Please try another mode or contact the store.")
		a.sendDeliveryModeButtons(account, contact)
		return
	}
	first := result.Slots[0]
	if first.Timezone != "" {
		tzName = first.Timezone
	}
	st.Timezone = tzName
	st.EarliestAt = first.RequestedFulfillmentAt
	st.Slots = nil
	earliestLabel = checkoutSlotLabel(first.RequestedFulfillmentAt)
	setCheckoutState(session, st)
	_ = a.persistSessionData(session)

	modeWord := "pickup"
	if st.DeliveryMode == "DELIVERY_TO_LOCATION" {
		modeWord = "delivery"
	}
	prompt := fmt.Sprintf(
		"When would you like %s?\nReply with a time like \"in 45 minutes\", \"today 5:30pm\", or \"tomorrow 10am\".\nEarliest available: %s",
		modeWord,
		earliestLabel,
	)
	_ = a.sendAndSaveTextMessage(account, contact, prompt)
}

func checkoutSlotLabel(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return truncateRunes(value, 20)
	}
	return parsed.Format("02 Jan 3:04 PM")
}

func cartProductOptionIDs(session *models.ChatbotSession) []int {
	items := cartOrderItems(session)
	out := make([]int, 0, len(items))
	for _, item := range items {
		out = append(out, item.ProductOption)
	}
	return out
}

func (a *App) handleCheckoutSlotChoice(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, buttonID string) {
	// Legacy interactive slot buttons (kept for mid-session checkouts).
	st := getCheckoutState(session)
	if st == nil || st.Step != "slot" {
		return
	}
	index, err := strconv.Atoi(strings.TrimPrefix(buttonID, checkoutSlotPrefix))
	if err != nil || index < 0 || index >= len(st.Slots) {
		a.sendFulfillmentSlots(account, contact, session, settings, st)
		return
	}
	slot := st.Slots[index]
	a.applyCheckoutSlot(account, contact, session, settings, st, asString(slot["token"]), asString(slot["requested_at"]), asString(slot["promised_at"]))
}

func (a *App) handleCheckoutSlotText(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState, text string) {
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		_ = a.sendAndSaveTextMessage(account, contact, "Fulfillment times are temporarily unavailable.")
		return
	}
	defer rt.Close()

	tzName := st.Timezone
	if tzName == "" {
		tzName = "UTC"
		if store, err := rt.Client.GetStore(context.Background(), rt.StoreID); err == nil {
			if tz := asString(store["timezone"]); tz != "" {
				tzName = tz
			}
		}
		st.Timezone = tzName
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
		tzName = "UTC"
	}

	now := time.Now().In(loc)
	requested, parseErr := parseFulfillmentTimeText(text, now, loc, st.EarliestAt)
	if parseErr != nil {
		_ = a.sendAndSaveTextMessage(account, contact, "I couldn't understand that time. Try \"in 30 minutes\", \"today 5pm\", or \"tomorrow 10:30am\".")
		a.sendFulfillmentSlots(account, contact, session, settings, st)
		return
	}

	slot, err := rt.Client.ProposeFulfillmentTime(context.Background(), rt.StoreID, st.DeliveryMode, requested.Format(time.RFC3339), cartProductOptionIDs(session))
	if err != nil {
		a.Log.Warn("propose fulfillment time failed", "error", err, "requested", requested)
		msg := "That time isn't available. "
		if st.EarliestAt != "" {
			msg += "Earliest available is " + checkoutSlotLabel(st.EarliestAt) + ". "
		}
		msg += "Please choose another time within store hours."
		_ = a.sendAndSaveTextMessage(account, contact, msg)
		a.sendFulfillmentSlots(account, contact, session, settings, st)
		return
	}
	a.applyCheckoutSlot(account, contact, session, settings, st, slot.Token, slot.RequestedFulfillmentAt, slot.PromisedReadyAt)
}

func (a *App) applyCheckoutSlot(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState, token, requestedAt, promisedAt string) {
	st.SlotToken = token
	st.RequestedAt = requestedAt
	st.PromisedAt = promisedAt
	if st.DeliveryMode == "PICKUP_FROM_STORE" {
		st.Step = "confirm"
		setCheckoutState(session, st)
		_ = a.persistSessionData(session)
		a.sendOrderConfirmPrompt(account, contact, session)
		return
	}
	a.sendSavedAddressChoices(account, contact, session, settings, st)
}

func (a *App) sendSavedAddressChoices(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState) {
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		a.beginNewCheckoutAddress(account, contact, session, settings)
		return
	}
	defer rt.Close()
	addresses, err := rt.Client.ListCustomerAddresses(context.Background(), rt.StoreID, contact.PhoneNumber)
	if err != nil || len(addresses) == 0 {
		a.beginNewCheckoutAddress(account, contact, session, settings)
		return
	}
	st.Step = "saved_address"
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	addressMap := map[string]any{}
	buttons := make([]map[string]any, 0, len(addresses)+1)
	for _, address := range addresses {
		if !address.AuthorizedAddress || len(buttons) >= 9 {
			continue
		}
		key := strconv.Itoa(address.ID)
		data, _ := json.Marshal(address)
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		addressMap[key] = raw
		buttons = append(buttons, map[string]any{"id": checkoutAddressPrefix + key, "title": truncateRunes(address.AddressLine1, 20)})
	}
	buttons = append(buttons, map[string]any{"id": checkoutNewAddressButtonID, "title": "Use new address"})
	session.SessionData["checkout_saved_addresses"] = addressMap
	setCheckoutState(session, st)
	_ = a.persistSessionData(session)
	_ = a.sendAndSaveInteractiveButtons(account, contact, "Choose a saved delivery address:", buttons)
}

func (a *App) handleCheckoutSavedAddressChoice(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, buttonID string) {
	st := getCheckoutState(session)
	id, err := strconv.Atoi(strings.TrimPrefix(buttonID, checkoutAddressPrefix))
	if st == nil || err != nil || id <= 0 {
		return
	}
	addresses, _ := session.SessionData["checkout_saved_addresses"].(map[string]any)
	raw, _ := addresses[strconv.Itoa(id)].(map[string]any)
	if raw == nil {
		a.sendSavedAddressChoices(account, contact, session, settings, st)
		return
	}
	st.SavedAddressID = &id
	st.NewAddress = raw
	if lat, ok := anyToFloat64(raw["latitude"]); ok {
		st.Latitude, st.HasLocation = lat, true
	}
	if lng, ok := anyToFloat64(raw["longitude"]); ok {
		st.Longitude = lng
	}
	st.Step = "confirm"
	setCheckoutState(session, st)
	_ = a.persistSessionData(session)
	a.sendOrderConfirmPrompt(account, contact, session)
}

func (a *App) beginNewCheckoutAddress(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings) {
	st := getCheckoutState(session)
	if st == nil {
		return
	}
	st.SavedAddressID = nil
	if st.NewAddress == nil {
		st.NewAddress = map[string]any{}
	}
	if contact != nil {
		if contact.ProfileName != "" {
			st.NewAddress["name"] = contact.ProfileName
		}
		if contact.PhoneNumber != "" {
			st.NewAddress["phone"] = contact.PhoneNumber
		}
	}
	st.NewAddress["email"] = st.Email
	if country := a.storeAddressCountry(session, settings); country != "" {
		st.NewAddress["country"] = country
	}

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

	st.Step = "address"
	st.HasLocation = false
	st.DeliveryZone = ""
	st.ShippingFeePaise = 0
	setCheckoutState(session, st)
	_ = a.persistSessionData(session)
	a.promptCheckoutAddress(account, contact, session, settings, st)
}

// storeRequiresLocationBasedDelivery reports whether the commerce store opts into
// lat/lng delivery checks (Store.location_based_delivery). Defaults to false.
func (a *App) storeRequiresLocationBasedDelivery(session *models.ChatbotSession, settings *models.ChatbotSettings) bool {
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil || rt.Client == nil {
		return false
	}
	defer rt.Close()
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

func isCheckoutAddressStep(step string) bool {
	switch step {
	case "address", "address_line_1", "city", "state", "pincode":
		return true
	default:
		return false
	}
}

func isIndiaStoreCountry(country string) bool {
	c := strings.ToUpper(strings.TrimSpace(country))
	return c == "IN" || c == "INDIA"
}

// isIndiaWhatsAppNumber reports whether phone is an Indian WhatsApp id:
// country code 91 followed by a 10-digit mobile ([6-9]…).
func isIndiaWhatsAppNumber(phone string) bool {
	d := digitsOnly(phone)
	if len(d) != 12 || !strings.HasPrefix(d, "91") {
		return false
	}
	return d[2] >= '6' && d[2] <= '9'
}

func (a *App) canSendAddressMessage(session *models.ChatbotSession, settings *models.ChatbotSettings, contact *models.Contact) bool {
	if contact == nil {
		return false
	}
	return a.storeCountryIsIndia(session, settings) && isIndiaWhatsAppNumber(contact.PhoneNumber)
}

func (a *App) commerceStoreMap(session *models.ChatbotSession, settings *models.ChatbotSettings) map[string]any {
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil || rt.Client == nil {
		return nil
	}
	defer rt.Close()
	store, err := rt.Client.GetStore(context.Background(), rt.StoreID)
	if err != nil || store == nil {
		a.Log.Warn("get_store for checkout address failed", "error", err)
		return nil
	}
	return store
}

func (a *App) storeCountryIsIndia(session *models.ChatbotSession, settings *models.ChatbotSettings) bool {
	store := a.commerceStoreMap(session, settings)
	if store == nil {
		return false
	}
	return isIndiaStoreCountry(asString(store["country"]))
}

func (a *App) storeAddressCountry(session *models.ChatbotSession, settings *models.ChatbotSettings) string {
	store := a.commerceStoreMap(session, settings)
	if store == nil {
		return ""
	}
	country := strings.TrimSpace(asString(store["country"]))
	if isIndiaStoreCountry(country) {
		return "India"
	}
	return country
}

func (a *App) promptCheckoutAddress(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState) {
	a.promptCheckoutAddressForm(account, contact, session, settings, st, nil, nil)
}

func (a *App) promptCheckoutAddressForm(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState, extraValues, validationErrors map[string]string) {
	if a.canSendAddressMessage(session, settings, contact) {
		params := whatsapp.AddressMessageParams{
			Country:          "IN",
			Values:           addressMessagePrefill(contact, st, extraValues),
			ValidationErrors: validationErrors,
		}
		if err := a.sendAndSaveAddressMessage(account, contact, checkoutAddressMessageBody, params); err != nil {
			a.Log.Warn("address_message send failed; falling back to text", "error", err)
			_ = a.sendAndSaveTextMessage(account, contact, checkoutAddressPrompt)
		}
		return
	}
	_ = a.sendAndSaveTextMessage(account, contact, checkoutAddressPrompt)
}

func (a *App) repromptCheckoutAddressInvalid(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState, parsed map[string]any) {
	if a.canSendAddressMessage(session, settings, contact) {
		errs := map[string]string{}
		if asString(parsed["pincode"]) == "" || indianPINRE.FindString(asString(parsed["pincode"])) == "" {
			errs["in_pin_code"] = "Please enter a valid 6-digit pin code."
		}
		if asString(parsed["address_line_1"]) == "" {
			errs["address"] = "Please enter your street address."
		}
		if len(errs) == 0 {
			errs["address"] = "Please complete your delivery address."
		}
		a.promptCheckoutAddressForm(account, contact, session, settings, st, newAddressToMetaValues(parsed), errs)
		return
	}
	_ = a.sendAndSaveTextMessage(account, contact, "Thanks — please include your street address and a 6-digit pincode.\n\n"+checkoutAddressPrompt)
}

func (a *App) handleCheckoutAddressMessage(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, nfmName string, responseData map[string]any) bool {
	if !strings.EqualFold(strings.TrimSpace(nfmName), "address_message") {
		return false
	}
	st := getCheckoutState(session)
	if st == nil || !isCheckoutAddressStep(st.Step) {
		return false
	}
	parsed := mapAddressMessageToNewAddress(extractAddressMessageValues(responseData), contact, st)
	if !deliveryAddressComplete(parsed) {
		a.repromptCheckoutAddressInvalid(account, contact, session, settings, st, parsed)
		return true
	}
	st.NewAddress = parsed
	a.persistCustomerAddress(session, settings, contact, st)
	st.Step = "confirm"
	setCheckoutState(session, st)
	_ = a.persistSessionData(session)
	a.sendOrderConfirmPrompt(account, contact, session)
	return true
}

func (a *App) sendOrderConfirmPrompt(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession) {
	st := getCheckoutState(session)
	msg := formatOrderConfirmSummary(session, st)
	_ = a.sendAndSaveInteractiveButtons(account, contact, msg, []map[string]any{
		{"id": checkoutConfirmButtonID, "title": "Confirm order"},
		{"id": checkoutCancelButtonID, "title": "Cancel"},
	})
}

func (a *App) handleCheckoutConversation(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, messageText, buttonID string, persistedMessage *models.Message) bool {
	if buttonID != "" && IsCheckoutButton(buttonID) {
		return false // handled by handleCommerceButtonTap
	}
	st := getCheckoutState(session)
	if st == nil {
		return false
	}

	text := strings.TrimSpace(messageText)
	if text == "" {
		a.repromptCheckoutStep(account, contact, session, settings, st)
		return true
	}

	// Qty / edit intents take priority over step validation so users can fix the cart mid-checkout.
	if a.handleCheckoutCartEditIntent(account, contact, session, settings, st, text) {
		return true
	}

	switch st.Step {
	case "capture":
		if st.CaptureIndex < 0 || st.CaptureIndex >= len(st.CaptureFields) {
			st.Step = "email"
			setCheckoutState(session, st)
			_ = a.persistSessionData(session)
			a.repromptCheckoutStep(account, contact, session, settings, st)
			return true
		}
		field := st.CaptureFields[st.CaptureIndex]
		if captureFieldAcceptsAttachment(field) {
			attachments := attachmentFromMessage(persistedMessage, asString(field["key"]))
			if len(attachments) == 0 {
				_ = a.sendAndSaveTextMessage(account, contact, "Please attach the requested file.\n"+promptCaptureField(field))
				return true
			}
			captured := jsonMapFromSession(session, "commerce_captured_fields")
			captured[asString(field["key"])] = attachmentsJSON(attachments)
			session.SessionData["commerce_captured_fields"] = map[string]any(captured)
			a.appendDraftAttachments(session, attachments)
			st.CaptureIndex++
			if st.CaptureIndex >= len(st.CaptureFields) {
				if a.completeCommerceCapture(account, contact, session, settings, st) {
					return true
				}
				st.Step = "email"
			}
			setCheckoutState(session, st)
			_ = a.persistSessionData(session)
			a.repromptCheckoutStep(account, contact, session, settings, st)
			return true
		}
		if !validCaptureValue(field, text) {
			_ = a.sendAndSaveTextMessage(account, contact, "Please provide a valid value.\n"+promptCaptureField(field))
			return true
		}
		captured := jsonMapFromSession(session, "commerce_captured_fields")
		captured[asString(field["key"])] = normalizedCaptureValue(field, text)
		session.SessionData["commerce_captured_fields"] = map[string]any(captured)
		st.CaptureIndex++
		if st.CaptureIndex >= len(st.CaptureFields) {
			if a.completeCommerceCapture(account, contact, session, settings, st) {
				return true
			}
			st.Step = "email"
		}
		setCheckoutState(session, st)
		_ = a.persistSessionData(session)
		a.repromptCheckoutStep(account, contact, session, settings, st)
		return true
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
	case "slot":
		a.handleCheckoutSlotText(account, contact, session, settings, st, text)
		return true
	case "location":
		_ = a.sendAndSaveTextMessage(account, contact, "Please use the Send location button to share your delivery pin.")
		_ = a.sendAndSaveLocationRequest(account, contact, checkoutLocationPrompt)
		return true
	case "address", "address_line_1", "city", "state", "pincode":
		// Single full-address reply (legacy multi-step keys still accepted mid-session).
		parsed := parseDeliveryAddressText(text, contact, st)
		if !deliveryAddressComplete(parsed) {
			a.repromptCheckoutAddressInvalid(account, contact, session, settings, st, parsed)
			return true
		}
		st.NewAddress = parsed
		a.persistCustomerAddress(session, settings, contact, st)
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
		a.repromptCheckoutStep(account, contact, session, settings, st)
		return true
	}
}

func captureFieldAcceptsAttachment(field map[string]any) bool {
	fieldType := strings.ToLower(asString(field["type"]))
	key := strings.ToLower(asString(field["key"]))
	switch fieldType {
	case "image", "images", "file", "document", "media", "attachment":
		return true
	}
	return strings.Contains(key, "reference") && strings.Contains(key, "image")
}

func validCaptureValue(field map[string]any, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if asString(field["type"]) == "number" {
		_, err := strconv.ParseFloat(value, 64)
		return err == nil
	}
	var options []string
	switch raw := field["options"].(type) {
	case []string:
		options = raw
	case []any:
		for _, option := range raw {
			options = append(options, fmt.Sprint(option))
		}
	}
	if len(options) > 0 {
		values := []string{value}
		if asString(field["type"]) == "multi_select" {
			values = strings.Split(value, ",")
		} else if asString(field["type"]) != "single_select" {
			return true
		}
		for _, candidate := range values {
			found := false
			for _, option := range options {
				if strings.EqualFold(strings.TrimSpace(option), strings.TrimSpace(candidate)) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return true
}

func normalizedCaptureValue(field map[string]any, value string) any {
	if asString(field["type"]) != "multi_select" {
		return strings.TrimSpace(value)
	}
	raw := strings.Split(value, ",")
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func (a *App) persistCustomerAddress(session *models.ChatbotSession, settings *models.ChatbotSettings, contact *models.Contact, st *checkoutState) {
	if st == nil || st.SavedAddressID != nil || contact == nil {
		return
	}
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		return
	}
	defer rt.Close()
	address, err := rt.Client.CreateCustomerAddress(context.Background(), rt.StoreID, contact.PhoneNumber, st.NewAddress)
	if err != nil {
		a.Log.Warn("create customer address failed; continuing with order snapshot", "error", err)
		return
	}
	st.SavedAddressID = &address.ID
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
	defer rt.Close()

	ctx := context.Background()
	result, err := rt.Client.CheckDeliveryEligibility(ctx, rt.StoreID, lat, lng)
	if err != nil {
		a.Log.Error("check_delivery_eligibility failed", "error", err)
		_ = a.sendAndSaveTextMessage(account, contact, "We couldn’t verify delivery for that location right now. Please try sharing your location again.")
		_ = a.sendAndSaveLocationRequest(account, contact, checkoutLocationPrompt)
		return true
	}

	deliverable, _ := result["deliverable"].(bool)
	zone := asString(result["zone"])
	feePaise := int64(0)
	if fee, ok := anyToFloat64(result["shipping_fee_paise"]); ok {
		feePaise = int64(fee)
	}

	if !deliverable {
		store, storeErr := rt.Client.GetStore(ctx, rt.StoreID)
		if storeErr != nil {
			a.Log.Warn("get_store for out-of-range delivery copy failed", "error", storeErr)
			store = nil
		}
		msg := formatOutOfRangeDeliveryMessage(store)
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
	a.promptCheckoutAddress(account, contact, session, settings, st)
	return true
}

// handleCheckoutCartEditIntent handles qty updates and exit-to-browse during checkout.
// Returns true when the message was consumed as a cart-edit intent.
func (a *App) handleCheckoutCartEditIntent(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState, text string) bool {
	if handleCheckoutAddonEdit(session, text) {
		_ = a.persistSessionData(session)
		_ = a.sendAndSaveTextMessage(account, contact, formatCheckoutCartSummary(session))
		a.repromptCheckoutStep(account, contact, session, settings, st)
		return true
	}
	if changed, message := applyTargetedCartEdit(session, text); changed {
		_ = a.persistSessionData(session)
		_ = a.sendAndSaveTextMessage(account, contact, message+"\n\n"+formatCheckoutCartSummary(session))
		if cartIsEmpty(session) {
			clearCheckoutState(session)
			return true
		}
		a.repromptCheckoutStep(account, contact, session, settings, st)
		return true
	}
	if isCheckoutEditExitIntent(text) {
		qty := extractQtyFromText(text)
		if qty > 0 {
			return a.applyCheckoutQtyOrPause(account, contact, session, settings, st, qty)
		}
		a.exitCheckoutToBrowse(account, contact, session, "Checkout paused. Your cart is still saved — update it or keep browsing, then tap Checkout when you're ready.", true)
		return true
	}

	// Bare positive integer → qty for sole cart line.
	if qty := parsePositiveInt(text); qty > 0 {
		return a.applyCheckoutQtyOrPause(account, contact, session, settings, st, qty)
	}

	// Phrases that include a quantity (e.g. "i want 2", "change to 2").
	if isCheckoutQtyPhrase(text) {
		if qty := extractQtyFromText(text); qty > 0 {
			return a.applyCheckoutQtyOrPause(account, contact, session, settings, st, qty)
		}
	}

	if isCheckoutCancelText(text) {
		a.exitCheckoutToBrowse(account, contact, session, "Checkout cancelled. Your cart is still saved.", true)
		return true
	}
	return false
}

func applyTargetedCartEdit(session *models.ChatbotSession, text string) (bool, string) {
	cart := normalizeCartMap(session.SessionData[cartKey])
	if match := removeLineRE.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 2 {
		if key, name := findCartLine(cart, match[1]); key != "" {
			delete(cart, key)
			session.SessionData[cartKey] = cartToAny(cart)
			return true, "Removed " + name + " from your cart."
		}
	}
	if match := targetQtyRE.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 3 {
		qty := parsePositiveInt(match[2])
		if key, name := findCartLine(cart, match[1]); key != "" && qty > 0 {
			cart[key]["qty"] = qty
			session.SessionData[cartKey] = cartToAny(cart)
			return true, fmt.Sprintf("Updated %s to quantity %d.", name, qty)
		}
	}
	return false, ""
}

func findCartLine(cart map[string]map[string]any, target string) (string, string) {
	target = strings.ToLower(strings.TrimSpace(target))
	for key, line := range cart {
		meta, _ := line["product"].(map[string]any)
		name := cartLineOptionName(meta)
		if target == key || strings.Contains(strings.ToLower(name), target) {
			return key, name
		}
	}
	return "", ""
}

func cartToAny(cart map[string]map[string]any) map[string]any {
	out := make(map[string]any, len(cart))
	for key, line := range cart {
		out[key] = line
	}
	return out
}

func handleCheckoutAddonEdit(session *models.ChatbotSession, text string) bool {
	match := addonEditRE.FindStringSubmatch(strings.TrimSpace(text))
	if len(match) == 0 {
		return false
	}
	id := parsePositiveInt(match[2])
	qty := 1
	if len(match) > 3 && match[3] != "" {
		qty = parsePositiveInt(match[3])
	}
	raw, _ := session.SessionData["commerce_addons"].([]any)
	next := make([]any, 0, len(raw)+1)
	for _, value := range raw {
		addon, _ := value.(map[string]any)
		if asToolInt(addon["addon"]) != id {
			next = append(next, value)
		}
	}
	if match[1] == "" && id > 0 && qty > 0 {
		next = append(next, map[string]any{"addon": id, "quantity": qty})
	}
	session.SessionData["commerce_addons"] = next
	return true
}

func (a *App) applyCheckoutQtyOrPause(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState, qty int) bool {
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
	a.repromptCheckoutStep(account, contact, session, settings, st)
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

func (a *App) repromptCheckoutStep(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState) {
	if st == nil {
		return
	}
	switch st.Step {
	case "capture":
		if st.CaptureIndex >= 0 && st.CaptureIndex < len(st.CaptureFields) {
			_ = a.sendAndSaveTextMessage(account, contact, promptCaptureField(st.CaptureFields[st.CaptureIndex]))
		}
	case "email":
		_ = a.sendAndSaveTextMessage(account, contact, "Please share your email address to continue checkout.")
	case "delivery_mode":
		a.sendDeliveryModeButtons(account, contact)
	case "slot":
		a.sendFulfillmentSlots(account, contact, session, settings, st)
	case "saved_address":
		a.sendSavedAddressChoices(account, contact, session, settings, st)
	case "location":
		_ = a.sendAndSaveLocationRequest(account, contact, checkoutLocationPrompt)
	case "address", "address_line_1", "city", "state", "pincode":
		a.promptCheckoutAddress(account, contact, session, settings, st)
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

func deliveryAddressComplete(addr map[string]any) bool {
	if asString(addr["address_line_1"]) == "" {
		return false
	}
	pin := asString(addr["pincode"])
	return pin != "" && indianPINRE.FindString(pin) != ""
}

func extractAddressMessageValues(responseData map[string]any) map[string]any {
	if responseData == nil {
		return map[string]any{}
	}
	if inner, ok := responseData["values"].(map[string]any); ok && inner != nil {
		return inner
	}
	return responseData
}

func mapValueString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	default:
		return asString(v)
	}
}

func joinNonEmpty(sep string, parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func newAddressToMetaValues(addr map[string]any) map[string]string {
	out := map[string]string{}
	if v := asString(addr["name"]); v != "" {
		out["name"] = v
	}
	if v := asString(addr["phone"]); v != "" {
		out["phone_number"] = v
	}
	if v := asString(addr["pincode"]); v != "" {
		out["in_pin_code"] = v
	}
	if v := asString(addr["city"]); v != "" {
		out["city"] = v
	}
	if v := asString(addr["state"]); v != "" {
		out["state"] = v
	}
	if v := asString(addr["address_line_1"]); v != "" {
		out["address"] = v
	}
	if v := asString(addr["landmark"]); v != "" {
		out["landmark_area"] = v
	}
	return out
}

func addressMessagePrefill(contact *models.Contact, st *checkoutState, extra map[string]string) map[string]string {
	out := map[string]string{}
	if contact != nil {
		if contact.ProfileName != "" {
			out["name"] = contact.ProfileName
		}
		if contact.PhoneNumber != "" {
			out["phone_number"] = indiaE164Phone(contact.PhoneNumber)
		}
	}
	if st != nil && st.NewAddress != nil {
		for k, v := range newAddressToMetaValues(st.NewAddress) {
			if v != "" {
				out[k] = v
			}
		}
	}
	for k, v := range extra {
		if strings.TrimSpace(v) != "" {
			out[k] = strings.TrimSpace(v)
		}
	}
	if v := out["phone_number"]; v != "" {
		out["phone_number"] = indiaE164Phone(v)
	}
	return out
}

// indiaE164Phone formats an Indian WhatsApp id as Meta expects in address_message values.
// Returns empty string when the number is not a valid Indian mobile E.164 value —
// Meta rejects invalid phone_number prefills with (#131009).
func indiaE164Phone(phone string) string {
	d := digitsOnly(phone)
	switch {
	case len(d) == 12 && strings.HasPrefix(d, "91") && d[2] >= '6' && d[2] <= '9':
		return "+" + d
	case len(d) == 10 && d[0] >= '6' && d[0] <= '9':
		return "+91" + d
	default:
		return ""
	}
}

func mapAddressMessageToNewAddress(values map[string]any, contact *models.Contact, st *checkoutState) map[string]any {
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
	if v := mapValueString(values, "name"); v != "" {
		out["name"] = v
	}
	if v := mapValueString(values, "phone_number"); v != "" {
		out["phone"] = v
	}
	if v := mapValueString(values, "in_pin_code"); v != "" {
		out["pincode"] = v
	}
	if v := mapValueString(values, "city"); v != "" {
		out["city"] = v
	}
	if v := mapValueString(values, "state"); v != "" {
		out["state"] = v
	}
	if v := mapValueString(values, "landmark_area"); v != "" {
		out["landmark"] = v
	}
	line := joinNonEmpty(", ",
		mapValueString(values, "house_number"),
		mapValueString(values, "floor_number"),
		mapValueString(values, "tower_number"),
		mapValueString(values, "building_name"),
		mapValueString(values, "address"),
	)
	if line != "" {
		out["address_line_1"] = line
	}
	if asString(out["country"]) == "" {
		out["country"] = "India"
	}
	return out
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
	defer rt.Close()

	draftID := commerceDraftID(session)
	if draftID == uuid.Nil {
		a.Log.Error("checkout submission blocked without durable draft")
		_ = a.sendAndSaveTextMessage(account, contact, "Checkout is temporarily unavailable. Your cart is still saved; please try again.")
		return
	}
	repo := draftrepo.NewDraftRepository(a.DB)
	current, loadErr := repo.Get(draftID, session.OrganizationID)
	if loadErr != nil {
		a.Log.Error("checkout draft load failed closed", "error", loadErr, "draft_id", draftID)
		_ = a.sendAndSaveTextMessage(account, contact, "Checkout is temporarily unavailable. Your cart is still saved; please try again.")
		return
	}

	items := cartOrderItems(session)
	if st.SlotToken == "" {
		_ = a.sendAndSaveTextMessage(account, contact, "Please enter a fulfillment time before confirming.")
		st.Step = "slot"
		setCheckoutState(session, st)
		a.sendFulfillmentSlots(account, contact, session, settings, st)
		return
	}
	if current.Status != "submitting" {
		validation, err := rt.Client.ValidateFulfillmentSlot(context.Background(), rt.StoreID, st.DeliveryMode, st.SlotToken, cartProductOptionIDs(session))
		if err != nil || !validation.Valid {
			st.Step = "slot"
			st.SlotToken = ""
			setCheckoutState(session, st)
			_ = a.persistSessionData(session)
			_ = a.sendAndSaveTextMessage(account, contact, "That fulfillment time is no longer available. Please choose another slot.")
			a.sendFulfillmentSlots(account, contact, session, settings, st)
			return
		}
	}
	orderItems := make([]map[string]any, 0, len(items))
	for _, it := range items {
		orderItems = append(orderItems, map[string]any{
			"product_option": it.ProductOption,
			"quantity":       it.Quantity,
		})
	}
	idempotencyKey := current.SubmissionKey
	draft := current
	if idempotencyKey == "" {
		candidateKey := "whatomate-" + uuid.NewString()
		claimed, claimErr := repo.ClaimSubmission(draftID, session.OrganizationID, current.Version, candidateKey)
		if claimErr != nil && !errors.Is(claimErr, draftrepo.ErrAlreadySubmitted) {
			_ = a.sendAndSaveTextMessage(account, contact, "Your order is already being submitted. Please wait a moment.")
			return
		}
		if claimed == nil || claimed.SubmissionKey == "" {
			a.Log.Error("checkout submission claim returned no durable key", "draft_id", draftID)
			_ = a.sendAndSaveTextMessage(account, contact, "Checkout is temporarily unavailable. Your cart is still saved; please try again.")
			return
		}
		draft = claimed
		idempotencyKey = claimed.SubmissionKey
	}
	args := createOrderArgs{
		Confirmed:      true,
		Items:          orderItems,
		Email:          st.Email,
		DeliveryMode:   st.DeliveryMode,
		NewAddress:     st.NewAddress,
		BuyerMetaData:  checkoutBuyerMeta(st, contact),
		AddressID:      st.SavedAddressID,
		Addons:         checkoutAddons(session),
		Notes:          checkoutNotes(session),
		SlotToken:      st.SlotToken,
		IdempotencyKey: idempotencyKey,
	}
	if st.SavedAddressID != nil {
		args.NewAddress = nil
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
	orderID := firstNonEmpty(asString(result["uuid"]), asString(result["id"]))
	paymentID := ""
	if payment, ok := result["payment"].(map[string]any); ok {
		paymentID = asString(payment["id"])
	}
	if _, err := repo.CompleteSubmission(draft.ID, session.OrganizationID, idempotencyKey, orderID, paymentID); err != nil {
		a.Log.Error("backend order succeeded but durable draft save failed", "error", err, "draft_id", draft.ID, "order_id", orderID)
		_ = a.sendAndSaveTextMessage(account, contact, "Your order was received, but confirmation is still syncing. Please retry Confirm; you will not be charged twice.")
		return
	}

	clearCart(session)
	clearCheckoutState(session)
	clearCartPendingOption(session)
	_ = a.persistSessionData(session)

	a.sendOrderPaymentCTA(account, contact, result)
}

func checkoutAddons(session *models.ChatbotSession) []map[string]any {
	raw, _ := session.SessionData["commerce_addons"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, value := range raw {
		if addon, ok := value.(map[string]any); ok {
			out = append(out, addon)
		}
	}
	return out
}

func checkoutNotes(session *models.ChatbotSession) string {
	payload := map[string]any{}
	for _, key := range []string{"commerce_captured_fields", "commerce_notes"} {
		if values, ok := session.SessionData[key].(map[string]any); ok {
			for name, value := range values {
				payload[name] = value
			}
		}
	}
	if len(payload) == 0 {
		return ""
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (a *App) sendOrderPaymentCTA(account *models.WhatsAppAccount, contact *models.Contact, result map[string]any) {
	body, paymentURL := paymentCTAContent(result)
	if paymentURL == "" {
		_ = a.sendAndSaveTextMessage(account, contact, body)
		return
	}
	if err := a.sendAndSaveCTAURLButton(account, contact, body, "Pay now", paymentURL); err != nil {
		_ = a.sendAndSaveTextMessage(account, contact, body+"\nPay here: "+paymentURL)
	}
}

func paymentCTAContent(result map[string]any) (string, string) {
	paymentURL := asString(result["payment_url"])
	copyResult := make(map[string]any, len(result))
	for key, value := range result {
		if key != "payment_url" {
			copyResult[key] = value
		}
	}
	return formatOrderSuccessMessage(copyResult), paymentURL
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
		b.WriteString("Fulfillment: Store pickup\n")
		b.WriteString("Delivery fee: Free (pickup)\n")
	} else {
		b.WriteString("Fulfillment: Delivery to your address\n")
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
	if st.RequestedAt != "" {
		fmt.Fprintf(&b, "Requested time: %s\n", checkoutSlotLabel(st.RequestedAt))
	}
	if addons := checkoutAddons(session); len(addons) > 0 {
		b.WriteString("Add-ons:")
		for _, addon := range addons {
			fmt.Fprintf(&b, " #%d x%d", asToolInt(addon["addon"]), asToolInt(addon["quantity"]))
		}
		b.WriteString("\n")
	}
	if notes := checkoutNotes(session); notes != "" {
		fmt.Fprintf(&b, "Notes: %s\n", notes)
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

// parseFulfillmentTimeText converts buyer free text into a store-local datetime.
// Relative phrases are resolved from now; "asap" uses earliestAt when present.
func parseFulfillmentTimeText(text string, now time.Time, loc *time.Location, earliestAt string) (time.Time, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}, errors.New("empty time")
	}
	if asapTimeRE.MatchString(text) {
		if earliestAt != "" {
			if parsed, err := time.Parse(time.RFC3339, earliestAt); err == nil {
				return parsed.In(loc), nil
			}
		}
		return now.Add(time.Minute).Truncate(time.Minute), nil
	}
	if m := relativeTimeRE.FindStringSubmatch(text); len(m) == 3 {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			return time.Time{}, errors.New("invalid relative amount")
		}
		unit := strings.ToLower(m[2])
		switch unit {
		case "h", "hr", "hrs", "hour", "hours":
			return now.Add(time.Duration(n) * time.Hour).Truncate(time.Minute), nil
		default:
			return now.Add(time.Duration(n) * time.Minute).Truncate(time.Minute), nil
		}
	}
	if m := absoluteTimeRE.FindStringSubmatch(text); len(m) == 5 {
		dayOffset := 0
		switch strings.ToLower(m[1]) {
		case "tomorrow":
			dayOffset = 1
		}
		hour, err := strconv.Atoi(m[2])
		if err != nil {
			return time.Time{}, err
		}
		minute := 0
		if m[3] != "" {
			minute, err = strconv.Atoi(m[3])
			if err != nil || minute > 59 {
				return time.Time{}, errors.New("invalid minutes")
			}
		}
		ampm := strings.ToLower(m[4])
		if ampm == "pm" || ampm == "am" {
			if hour < 1 || hour > 12 {
				return time.Time{}, errors.New("invalid hour")
			}
			if ampm == "pm" && hour < 12 {
				hour += 12
			}
			if ampm == "am" && hour == 12 {
				hour = 0
			}
		} else if hour > 23 {
			return time.Time{}, errors.New("invalid hour")
		}
		base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, dayOffset)
		candidate := time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, loc)
		// Bare clock times that already passed today roll to tomorrow.
		if dayOffset == 0 && m[1] == "" && !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate, nil
	}
	return time.Time{}, errors.New("unrecognized time")
}
