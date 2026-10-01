package handlers

import (
	"fmt"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	tiqrEcommerceAfterCaptureIntro = "This is a custom %s request — I’ll collect a few details and connect you with our team."

	tiqrEcommerceAfterCaptureHandoffFail = "We saved your request, but could not connect an expert just now. Please try again shortly."
)

// Legacy session keys from when this path was named for themed cakes.
// migrateAfterCaptureSessionKeys rewrites them once on entry.

func collectionHandoffAfterCapture(col map[string]any) bool {
	return strings.EqualFold(strings.TrimSpace(asString(col["handoff_policy"])), "after_capture")
}

// migrateAfterCaptureSessionKeys rewrites legacy themed_* keys and step names
// so in-progress sessions continue under after_capture_* identifiers.
func migrateAfterCaptureSessionKeys(session *models.ChatbotSession) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	data := session.SessionData

	legacyKeys := make([]string, 0)
	for oldKey := range data {
		if strings.HasPrefix(oldKey, "themed_") {
			legacyKeys = append(legacyKeys, oldKey)
		}
	}
	for _, oldKey := range legacyKeys {
		newKey := "after_capture_" + strings.TrimPrefix(oldKey, "themed_")
		if _, exists := data[newKey]; !exists {
			data[newKey] = data[oldKey]
		}
		delete(data, oldKey)
	}

	if raw, ok := data[checkoutSessionKey].(map[string]any); ok && raw != nil {
		if asString(raw["flow"]) == "themed" {
			raw["flow"] = checkoutFlowAfterCapture
			data[checkoutSessionKey] = raw
		}
	}

	if strings.HasPrefix(session.CurrentStep, "themed_") {
		session.CurrentStep = "after_capture_" + strings.TrimPrefix(session.CurrentStep, "themed_")
	}

	records, ok := anySlice(data[codedCallsKey])
	if !ok || len(records) == 0 {
		return
	}
	next := make([]any, len(records))
	for i, entry := range records {
		rec, ok := asStringMap(entry)
		if !ok {
			next[i] = entry
			continue
		}
		changed := false
		if name := asString(rec["name"]); strings.HasPrefix(name, "themed_") {
			rec["name"] = "after_capture_" + strings.TrimPrefix(name, "themed_")
			changed = true
		}
		if key := asString(rec["var"]); strings.HasPrefix(key, "themed_") {
			rec["var"] = "after_capture_" + strings.TrimPrefix(key, "themed_")
			changed = true
		}
		if changed {
			next[i] = rec
		} else {
			next[i] = entry
		}
	}
	data[codedCallsKey] = next
}

// soleCategoryProductID returns the product id when the collection has exactly
// one product that exposes options. Used only for after-capture add-on prompts.
func soleCategoryProductID(c *Conv, categoryID string) string {
	products, ok := c.StoreList("after_capture_products", "list_products", map[string]string{"category_id": categoryID})
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

// runAfterCaptureHandoff collects capture → add-ons → Meta Flow details,
// then creates a commerce agent transfer. It does not write tiqr_cart or place
// an order. The known product option is copied onto the handoff cart so the
// store order form can select it. Fulfillment time is skipped for now.
func runAfterCaptureHandoff(c *Conv, collection map[string]any, productID string) error {
	if collection == nil {
		return c.Transfer(codedAgentHandoff)
	}
	migrateAfterCaptureSessionKeys(c.session())

	categoryID := fieldString(collection, "id")
	name := strings.TrimSpace(asString(collection["name"]))
	if name == "" {
		name = "order"
	}

	c.Once("after_capture_select_category", func() {
		setSelectedCategoryID(c.session(), categoryID)
		c.session().SessionData["collection_id"] = categoryID
		c.session().SessionData["collection_name"] = name
		if productID != "" {
			c.session().SessionData["product_id"] = productID
			c.session().SessionData["after_capture_product_id"] = productID
		}
	})

	c.Say(fmt.Sprintf(tiqrEcommerceAfterCaptureIntro, name))

	if !askAfterCaptureCaptureFields(c, collection) {
		return nil
	}
	if !askAfterCaptureAddons(c, productID) {
		return nil
	}
	if !askAfterCaptureCustomerDetails(c) {
		return nil
	}
	return finishCodedAfterCaptureHandoff(c, collection)
}

func askAfterCaptureCaptureFields(c *Conv, collection map[string]any) bool {
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

func askAfterCaptureAddons(c *Conv, productID string) bool {
	productID = strings.TrimSpace(productID)
	if !askCatalogAddons(c, productID, "after_capture_addons") {
		return false
	}
	choices := loadCatalogAddonChoicesReplay(c, "after_capture_addons")
	if len(choices) > 0 {
		return true
	}
	text, ok := c.AskText("after_capture_addons_free",
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
	c.Once("after_capture_addons_free_save", func() {
		notes := jsonMapFromSession(c.session(), "commerce_notes")
		notes["addon_requests"] = strings.TrimSpace(text)
		c.session().SessionData["commerce_notes"] = map[string]any(notes)
	})
	return true
}

// loadCatalogAddonChoicesReplay reads choices already fetched by askCatalogAddons.
func loadCatalogAddonChoicesReplay(c *Conv, prefix string) []map[string]any {
	if c == nil || c.session() == nil {
		return nil
	}
	raw, ok := asStringMap(c.session().SessionData[prefix+"_product"])
	if !ok || raw == nil {
		return nil
	}
	return parseProductAddonChoices(raw["addons"])
}

func askAfterCaptureFulfillmentTime(c *Conv) bool {
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
	c.Once("after_capture_slot_meta", func() {
		c.session().SessionData["after_capture_earliest_at"] = earliestAt
		c.session().SessionData["after_capture_timezone"] = tzName
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
		c.Once(fmt.Sprintf("after_capture_slot_saved_%d", attempt), func() {
			c.session().SessionData["fulfillment_slot_token"] = asString(slot["token"])
			c.session().SessionData["requested_fulfillment_at"] = asString(slot["requested_fulfillment_at"])
			if promised := asString(slot["promised_ready_at"]); promised != "" {
				c.session().SessionData["promised_ready_at"] = promised
			}
			c.session().SessionData["after_capture_timezone"] = firstNonEmpty(asString(slot["timezone"]), tzName)
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

func askAfterCaptureCustomerDetails(c *Conv) bool {
	detailsBody := "Please share your name, email, and phone number so we can continue your request."
	flowID := tiqrEcommercePickupFlowID
	if asString(c.session().SessionData["delivery_mode"]) == tiqrModeDelivery {
		detailsBody = "Please share your name, phone number, and address so we can continue your request."
		flowID = tiqrEcommerceFlowID
	}
	return c.AskFlow("after_capture_details", FlowPrompt{
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

func finishCodedAfterCaptureHandoff(c *Conv, collection map[string]any) error {
	if c.stop || c.ended {
		return nil
	}
	account := c.chat.account
	contact := c.chat.contact
	session := c.session()
	if account == nil || contact == nil || session == nil {
		c.Say(tiqrEcommerceAfterCaptureHandoffFail)
		c.stop = true
		return nil
	}

	settings, err := c.app.getChatbotSettingsCached(account.OrganizationID, account.Name)
	if err != nil || settings == nil {
		c.Say(tiqrEcommerceAfterCaptureHandoffFail)
		c.stop = true
		return nil
	}

	st := afterCaptureCheckoutStateFromSession(session)
	setCheckoutState(session, st)
	setSelectedCategoryID(session, fieldString(collection, "id"))
	stageAfterCaptureProductOptionCart(session)

	if _, err := c.app.ensureCommerceDraft(contact, session, settings); err != nil {
		c.app.Log.Error("after_capture draft create failed", "error", err)
		c.Say(tiqrEcommerceAfterCaptureHandoffFail)
		c.stop = true
		return nil
	}
	_ = c.app.syncReferencedCommerceDraft(session)

	category := categoryFromCollectionMap(collection)
	if !c.app.completeCommerceCaptureWithCategory(account, contact, session, settings, st, category) {
		c.Say(tiqrEcommerceAfterCaptureHandoffFail)
		c.stop = true
		return nil
	}

	// createCommerceTransfer cancels the session in the DB. Keep the in-memory
	// session cancelled so persistChatSession does not revive it as completed.
	clearCodedAfterCaptureSession(session)
	c.stop = true
	c.ended = true
	return nil
}

func afterCaptureCheckoutStateFromSession(session *models.ChatbotSession) *checkoutState {
	data := session.SessionData
	st := &checkoutState{
		Flow:             checkoutFlowAfterCapture,
		Step:             "confirm",
		NewAddress:       map[string]any{},
		DeliveryMode:     asString(data["delivery_mode"]),
		SlotToken:        asString(data["fulfillment_slot_token"]),
		RequestedAt:      asString(data["requested_fulfillment_at"]),
		PromisedAt:       asString(data["promised_ready_at"]),
		Timezone:         asString(data["after_capture_timezone"]),
		EarliestAt:       asString(data["after_capture_earliest_at"]),
		PendingProductID: asString(data["after_capture_product_id"]),
		Email:            contextEmail(data),
	}
	if st.DeliveryMode == "" {
		st.DeliveryMode = tiqrModePickup
	}
	st.NewAddress = afterCaptureAddressFromFlow(data)
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

// afterCaptureAddressFromFlow builds the draft address snapshot from WhatsApp Flow fields.
func afterCaptureAddressFromFlow(data map[string]any) map[string]any {
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

// stageAfterCaptureProductOptionCart copies the product's option onto the
// handoff cart. A custom request never builds tiqr_cart, so without this the
// order form has no product option to select.
func stageAfterCaptureProductOptionCart(session *models.ChatbotSession) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	if len(tiqrCartLinesFromData(session.SessionData)) > 0 {
		session.SessionData[commerceHandoffCartKey] = map[string]any(tiqrEcommerceCartSnapshot(session))
		return
	}
	line, ok := afterCaptureProductOptionLine(session.SessionData)
	if !ok {
		return
	}
	session.SessionData[commerceHandoffCartKey] = map[string]any{
		"source": tiqrEcommerceCartSource,
		"lines":  []any{line},
	}
}

func afterCaptureProductOptionLine(data map[string]any) (map[string]any, bool) {
	if data == nil {
		return nil, false
	}
	productID := strings.TrimSpace(asString(data["after_capture_product_id"]))
	if productID == "" {
		productID = strings.TrimSpace(asString(data["product_id"]))
	}
	product := findSessionProduct(data, productID)
	productName := strings.TrimSpace(asString(data["product_name"]))
	if product != nil {
		if name := fieldString(product, "name"); name != "" {
			productName = name
		}
	}
	options := optionMapsFrom(nil)
	if product != nil {
		options = optionMapsFrom(product["options"])
	}
	if len(options) == 0 {
		options = optionMapsFrom(data["options"])
	}
	optionID, optionName, ok := pickAfterCaptureProductOption(data, options)
	if !ok {
		return nil, false
	}
	line := map[string]any{
		"product_option": optionID,
		"quantity":       "1",
	}
	if optionName != "" {
		line["option_name"] = optionName
	}
	if productName != "" {
		line["product_name"] = productName
	}
	return line, true
}

func findSessionProduct(data map[string]any, productID string) map[string]any {
	for _, key := range []string{"after_capture_products", "products"} {
		items, ok := anySlice(data[key])
		if !ok {
			continue
		}
		for _, entry := range items {
			item, ok := asStringMap(entry)
			if !ok {
				continue
			}
			if productID == "" || fieldString(item, "id") == productID {
				if productID != "" || productOptionCount(item) > 0 {
					return item
				}
			}
		}
	}
	if raw, ok := asStringMap(data["after_capture_product"]); ok {
		if nested, ok := asStringMap(raw["product"]); ok {
			raw = nested
		}
		if productID == "" || fieldString(raw, "id") == productID {
			return raw
		}
	}
	return nil
}

func optionMapsFrom(raw any) []map[string]any {
	items, ok := anySlice(raw)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok || fieldString(item, "id") == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func pickAfterCaptureProductOption(data map[string]any, options []map[string]any) (string, string, bool) {
	want := strings.TrimSpace(asString(data["option_id"]))
	if want != "" {
		for _, option := range options {
			if fieldString(option, "id") == want {
				name := fieldString(option, "name")
				if name == "" {
					name = strings.TrimSpace(asString(data["option_name"]))
				}
				return want, name, true
			}
		}
		return want, strings.TrimSpace(asString(data["option_name"])), true
	}
	if len(options) == 0 {
		return "", "", false
	}
	id := fieldString(options[0], "id")
	if id == "" {
		return "", "", false
	}
	return id, fieldString(options[0], "name"), true
}

func clearCodedAfterCaptureSession(session *models.ChatbotSession) {
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
