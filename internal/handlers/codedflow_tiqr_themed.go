package handlers

import (
	"fmt"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	tiqrEcommerceThemedIntro = "This is a custom %s request — I’ll collect a few details and connect you with our team."

	tiqrEcommerceThemedHandoffFail = "We saved your request, but could not connect an expert just now. Please try again shortly."
)

func collectionHandoffAfterCapture(col map[string]any) bool {
	return strings.EqualFold(strings.TrimSpace(asString(col["handoff_policy"])), "after_capture")
}

// soleCategoryProductID returns the product id when the collection has exactly
// one product that exposes options. Used only for themed add-on prompts.
func soleCategoryProductID(c *Conv, categoryID string) string {
	products, ok := c.StoreList("themed_products", "list_products", map[string]string{"category_id": categoryID})
	if !ok {
		return ""
	}
	withOptions := make([]map[string]any, 0, len(products))
	for _, entry := range products {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if n := productOptionCount(item); n > 0 {
			withOptions = append(withOptions, item)
		}
	}
	if len(withOptions) != 1 {
		return ""
	}
	return fieldString(withOptions[0], "id")
}

func productOptionCount(product map[string]any) int {
	if product == nil {
		return 0
	}
	switch opts := product["options"].(type) {
	case []any:
		return len(opts)
	case []map[string]any:
		return len(opts)
	default:
		return 0
	}
}

func afterCaptureCollectionForProduct(c *Conv, product map[string]any) map[string]any {
	if product == nil {
		return nil
	}
	if cat, ok := asStringMap(product["category"]); ok {
		if id := fieldString(cat, "id"); id != "" {
			if col := collectionByID(c, id); col != nil && collectionHandoffAfterCapture(col) {
				return col
			}
			if collectionHandoffAfterCapture(cat) {
				return cat
			}
		}
	}
	if id := productCategoryID(product); id != "" {
		if col := collectionByID(c, id); col != nil && collectionHandoffAfterCapture(col) {
			return col
		}
	}
	if id := strings.TrimSpace(asString(c.session().SessionData["collection_id"])); id != "" {
		if col := collectionByID(c, id); col != nil && collectionHandoffAfterCapture(col) {
			return col
		}
	}
	return nil
}

// runThemedHandoff collects capture → add-ons → fulfillment → Meta Flow details,
// then creates a commerce agent transfer. It never writes tiqr_cart or create_order.
func runThemedHandoff(c *Conv, collection map[string]any, productID string) error {
	if collection == nil {
		return c.Transfer(codedAgentHandoff)
	}
	categoryID := fieldString(collection, "id")
	name := strings.TrimSpace(asString(collection["name"]))
	if name == "" {
		name = "order"
	}

	c.Once("themed_select_category", func() {
		setSelectedCategoryID(c.session(), categoryID)
		c.session().SessionData["collection_id"] = categoryID
		c.session().SessionData["collection_name"] = name
		if productID != "" {
			c.session().SessionData["product_id"] = productID
			c.session().SessionData["themed_product_id"] = productID
		}
	})

	c.Say(fmt.Sprintf(tiqrEcommerceThemedIntro, name))

	if !askThemedCaptureFields(c, collection) {
		return nil
	}
	if !askThemedAddons(c, productID) {
		return nil
	}
	if !askThemedFulfillmentTime(c) {
		return nil
	}
	if !askThemedCustomerDetails(c) {
		return nil
	}
	return finishCodedThemedHandoff(c, collection)
}

func askThemedCaptureFields(c *Conv, collection map[string]any) bool {
	// Replay every field in order. Skipping a field already stored in
	// commerce_captured_fields would skip its call record and shift later
	// answers (and Skip) onto the wrong steps.
	fields := requiredCaptureFieldsFrom(collection)
	for i, field := range fields {
		name := captureCallName(i, asString(field["key"]))
		value, ok := c.askCaptureFieldNoCheckout(name, field)
		if !ok {
			return false
		}
		saveSessionCapture(c, field, value)
	}
	return !c.stop
}

// askCaptureFieldNoCheckout is askCaptureField without diverting to checkout.
func (c *Conv) askCaptureFieldNoCheckout(name string, field map[string]any) (any, bool) {
	if c.stop {
		return nil, false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return nil, false
		}
		value, ok := rec["value"]
		if !ok {
			return nil, false
		}
		return value, true
	}
	body := strings.TrimSpace(promptCaptureField(field))
	if body == "" {
		body = "Please share " + asString(field["label"]) + "."
	}
	if c.noAnswerYet() {
		if !c.sendCapturePrompt(name, body) {
			return nil, false
		}
		c.wait(name)
		return nil, false
	}
	input := strings.TrimSpace(c.chat.userInput)
	if input == "" || !validCaptureValue(field, input) {
		c.chat.consumed = true
		if !c.sendCapturePrompt(name, "Please provide a valid value.\n"+body) {
			return nil, false
		}
		c.wait(name)
		return nil, false
	}
	value := normalizedCaptureValue(field, input)
	c.chat.consumed = true
	c.appendCall(map[string]any{"name": name, "ok": true, "value": value})
	return value, true
}

func askThemedAddons(c *Conv, productID string) bool {
	productID = strings.TrimSpace(productID)
	choices := loadThemedAddonChoices(c, productID)
	if len(choices) > 0 {
		return askThemedStructuredAddons(c, choices)
	}
	text, ok := c.AskText("themed_addons_free",
		"Any add-ons (candles, flowers, etc.)? Reply with details, or say Skip.",
		StepNote{
			Doing:  "Collecting optional add-on notes for a custom request.",
			Expect: "Add-on details, or Skip.",
		},
	)
	if !ok {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "skip" || lower == "no" || lower == "none" || lower == "done" || lower == "continue" {
		return true
	}
	c.Once("themed_addons_free_save", func() {
		notes := jsonMapFromSession(c.session(), "commerce_notes")
		notes["addon_requests"] = strings.TrimSpace(text)
		c.session().SessionData["commerce_notes"] = map[string]any(notes)
	})
	return true
}

func loadThemedAddonChoices(c *Conv, productID string) []map[string]any {
	if productID == "" {
		return nil
	}
	raw, ok := c.Store("themed_product", "get_product", map[string]string{"product_id": productID})
	if !ok || raw == nil {
		return nil
	}
	return parseProductAddonChoices(raw["addons"])
}

func askThemedStructuredAddons(c *Conv, choices []map[string]any) bool {
	currency := sessionCurrencyCode(c.session())
	var b strings.Builder
	b.WriteString("Would you like any add-ons?\n")
	for i, choice := range choices {
		fmt.Fprintf(&b, "%d. %s", i+1, asString(choice["name"]))
		if price := asToolFloat(choice["price"]); price > 0 {
			b.WriteString(" — ")
			b.WriteString(formatMoney(price, currency))
		}
		b.WriteByte('\n')
	}
	b.WriteString("Reply with a number to add one, or say Skip.")

	for attempt := 1; ; attempt++ {
		text, ok := c.AskText(fmt.Sprintf("themed_addons_%d", attempt), strings.TrimSpace(b.String()), StepNote{
			Doing:  "The customer is picking catalog add-ons for a custom request.",
			Expect: "A listed number, or Skip.",
		})
		if !ok {
			return false
		}
		lower := strings.ToLower(strings.TrimSpace(text))
		if lower == "skip" || lower == "no" || lower == "none" || lower == "done" || lower == "continue" {
			return true
		}
		if n := parsePositiveInt(text); n >= 1 && n <= len(choices) {
			id := anyToInt(choices[n-1]["id"])
			if id > 0 {
				appendCommerceAddon(c.session(), id, 1)
				c.Once(fmt.Sprintf("themed_addon_added_%d_%d", attempt, id), func() {})
				c.Say("Added " + asString(choices[n-1]["name"]) + ".")
				continue
			}
		}
		c.Say("Please reply with a listed number, or say Skip.")
	}
}

func askThemedFulfillmentTime(c *Conv) bool {
	mode := strings.TrimSpace(asString(c.session().SessionData["delivery_mode"]))
	if mode == "" {
		mode = tiqrModePickup
	}

	slotsPayload, ok := c.Store("fulfillment_slots", "list_fulfillment_slots", map[string]string{
		"delivery_mode": mode,
	})
	if !ok {
		c.Say("Fulfillment times are temporarily unavailable.")
		return false
	}

	earliestAt, tzName := earliestSlotFromPayload(slotsPayload)
	if tzName == "" {
		tzName = "UTC"
		if store, ok := asStringMap(c.session().SessionData["store"]); ok {
			if tz := asString(store["timezone"]); tz != "" {
				tzName = tz
			}
		}
	}
	c.Once("themed_slot_meta", func() {
		c.session().SessionData["themed_earliest_at"] = earliestAt
		c.session().SessionData["themed_timezone"] = tzName
		c.session().SessionData["delivery_mode"] = mode
	})

	modeWord := "pickup"
	if mode == tiqrModeDelivery {
		modeWord = "delivery"
	}
	prompt := fmt.Sprintf(
		"When would you like %s?\nReply with a time like \"in 45 minutes\", \"today 5:30pm\", or \"tomorrow 10am\".\nEarliest available: %s",
		modeWord,
		checkoutSlotLabel(earliestAt),
	)

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
		tzName = "UTC"
	}

	for attempt := 1; ; attempt++ {
		body := prompt
		if attempt > 1 {
			body = "I couldn't understand that time. Try \"in 30 minutes\", \"today 5pm\", or \"tomorrow 10:30am\".\n\n" + prompt
		}
		text, ok := c.AskText(fmt.Sprintf("fulfillment_time_%d", attempt), body, StepNote{
			Doing:  "Collecting a pickup or delivery time for a custom request.",
			Expect: "A relative or absolute time such as in 45 minutes or tomorrow 10am.",
		})
		if !ok {
			return false
		}
		now := time.Now().In(loc)
		requested, parseErr := parseFulfillmentTimeText(text, now, loc, earliestAt)
		if parseErr != nil {
			continue
		}
		slot, ok := c.Store(fmt.Sprintf("proposed_slot_%d", attempt), "propose_fulfillment_time", map[string]string{
			"delivery_mode":            mode,
			"requested_fulfillment_at": requested.Format(time.RFC3339),
		})
		if !ok {
			c.Say("That time isn't available. Please choose another time within store hours.")
			continue
		}
		c.Once(fmt.Sprintf("themed_slot_saved_%d", attempt), func() {
			c.session().SessionData["fulfillment_slot_token"] = asString(slot["token"])
			c.session().SessionData["requested_fulfillment_at"] = asString(slot["requested_fulfillment_at"])
			if promised := asString(slot["promised_ready_at"]); promised != "" {
				c.session().SessionData["promised_ready_at"] = promised
			}
			c.session().SessionData["themed_timezone"] = firstNonEmpty(asString(slot["timezone"]), tzName)
		})
		return true
	}
}

func earliestSlotFromPayload(payload map[string]any) (earliestAt, timezone string) {
	slots, ok := anySlice(payload["slots"])
	if !ok || len(slots) == 0 {
		return "", ""
	}
	first, ok := asStringMap(slots[0])
	if !ok {
		return "", ""
	}
	return asString(first["requested_fulfillment_at"]), asString(first["timezone"])
}

func askThemedCustomerDetails(c *Conv) bool {
	detailsBody := "Please share your name, email, and phone number so we can continue your request."
	flowID := tiqrEcommercePickupFlowID
	if asString(c.session().SessionData["delivery_mode"]) == tiqrModeDelivery {
		detailsBody = "Please share your name, phone number, and address so we can continue your request."
		flowID = tiqrEcommerceFlowID
	}
	return c.AskFlow("themed_details", FlowPrompt{
		FlowID: flowID,
		CTA:    "Enter details",
		Header: "Your details",
		Body:   detailsBody,
		Step: StepNote{
			Doing:  "Collecting customer details for a custom request before agent handoff.",
			Expect: "A WhatsApp Flow submission with contact details.",
		},
	})
}

func finishCodedThemedHandoff(c *Conv, collection map[string]any) error {
	if c.stop || c.ended {
		return nil
	}
	account := c.chat.account
	contact := c.chat.contact
	session := c.session()
	if account == nil || contact == nil || session == nil {
		c.Say(tiqrEcommerceThemedHandoffFail)
		c.stop = true
		return nil
	}

	settings, err := c.app.getChatbotSettingsCached(account.OrganizationID, account.Name)
	if err != nil || settings == nil {
		c.Say(tiqrEcommerceThemedHandoffFail)
		c.stop = true
		return nil
	}

	st := themedCheckoutStateFromSession(session)
	setCheckoutState(session, st)
	setSelectedCategoryID(session, fieldString(collection, "id"))

	if _, err := c.app.ensureCommerceDraft(contact, session, settings); err != nil {
		c.app.Log.Error("themed draft create failed", "error", err)
		c.Say(tiqrEcommerceThemedHandoffFail)
		c.stop = true
		return nil
	}
	_ = c.app.syncReferencedCommerceDraft(session)

	category := categoryFromCollectionMap(collection)
	if !c.app.completeCommerceCaptureWithCategory(account, contact, session, settings, st, category) {
		c.Say(tiqrEcommerceThemedHandoffFail)
		c.stop = true
		return nil
	}

	// createCommerceTransfer cancels the session in the DB. Keep the in-memory
	// session cancelled so persistChatSession does not revive it as completed.
	clearCodedThemedSession(session)
	c.stop = true
	c.ended = true
	return nil
}

func themedCheckoutStateFromSession(session *models.ChatbotSession) *checkoutState {
	data := session.SessionData
	st := &checkoutState{
		Flow:             checkoutFlowThemed,
		Step:             "confirm",
		NewAddress:       map[string]any{},
		DeliveryMode:     asString(data["delivery_mode"]),
		SlotToken:        asString(data["fulfillment_slot_token"]),
		RequestedAt:      asString(data["requested_fulfillment_at"]),
		PromisedAt:       asString(data["promised_ready_at"]),
		Timezone:         asString(data["themed_timezone"]),
		EarliestAt:       asString(data["themed_earliest_at"]),
		PendingProductID: asString(data["themed_product_id"]),
		Email:            contextEmail(data),
	}
	if st.DeliveryMode == "" {
		st.DeliveryMode = tiqrModePickup
	}
	st.NewAddress = themedAddressFromFlow(data)
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

// themedAddressFromFlow builds the draft address snapshot from WhatsApp Flow fields.
func themedAddressFromFlow(data map[string]any) map[string]any {
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

func clearCodedThemedSession(session *models.ChatbotSession) {
	if session == nil {
		return
	}
	if session.SessionData != nil {
		delete(session.SessionData, codedFlowDataKey)
	}
	session.CurrentStep = ""
	session.StepRetries = 0
	session.CurrentFlowID = nil
	session.Status = models.SessionStatusCancelled
	now := time.Now()
	session.CompletedAt = &now
}
