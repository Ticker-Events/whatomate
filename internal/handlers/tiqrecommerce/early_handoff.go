package tiqrecommerce

import (
	"fmt"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	tiqrEcommerceEarlyHandoffIntro = "This is a custom %s request — I’ll collect a few details and connect you with our team."

	tiqrEcommerceEarlyHandoffFail = "We saved your request, but could not connect an expert just now. Please try again shortly."
)

// Catalog handoff_policy value stays "after_capture" (store API). Session keys,
// coded-call names, and checkout flow id use early_handoff_*.

func collectionHandoffEarly(col map[string]any) bool {
	return strings.EqualFold(strings.TrimSpace(asString(col["handoff_policy"])), "after_capture")
}

// MigrateEarlyHandoffSessionKeys rewrites legacy themed_* and after_capture_*
// keys and step names so in-progress sessions continue under early_handoff_*.
func MigrateEarlyHandoffSessionKeys(session *models.ChatbotSession) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	data := session.SessionData

	rewriteLegacyKey := func(oldKey, prefix string) {
		newKey := "early_handoff_" + strings.TrimPrefix(oldKey, prefix)
		if _, exists := data[newKey]; !exists {
			data[newKey] = data[oldKey]
		}
		delete(data, oldKey)
	}
	legacyKeys := make([]string, 0)
	for oldKey := range data {
		if strings.HasPrefix(oldKey, "themed_") || strings.HasPrefix(oldKey, "after_capture_") {
			legacyKeys = append(legacyKeys, oldKey)
		}
	}
	for _, oldKey := range legacyKeys {
		if strings.HasPrefix(oldKey, "themed_") {
			rewriteLegacyKey(oldKey, "themed_")
		} else {
			rewriteLegacyKey(oldKey, "after_capture_")
		}
	}

	if raw, ok := data[checkoutSessionKey].(map[string]any); ok && raw != nil {
		switch asString(raw["flow"]) {
		case "themed", "after_capture":
			raw["flow"] = checkoutFlowEarlyHandoff
			data[checkoutSessionKey] = raw
		}
	}

	switch {
	case strings.HasPrefix(session.CurrentStep, "themed_"):
		session.CurrentStep = "early_handoff_" + strings.TrimPrefix(session.CurrentStep, "themed_")
	case strings.HasPrefix(session.CurrentStep, "after_capture_"):
		session.CurrentStep = "early_handoff_" + strings.TrimPrefix(session.CurrentStep, "after_capture_")
	}

	records, ok := anySlice(data[codedflow.CallsKey])
	if !ok || len(records) == 0 {
		return
	}
	rewriteCallField := func(value string) (string, bool) {
		switch {
		case strings.HasPrefix(value, "themed_"):
			return "early_handoff_" + strings.TrimPrefix(value, "themed_"), true
		case strings.HasPrefix(value, "after_capture_"):
			return "early_handoff_" + strings.TrimPrefix(value, "after_capture_"), true
		default:
			return value, false
		}
	}
	next := make([]any, len(records))
	for i, entry := range records {
		rec, ok := asStringMap(entry)
		if !ok {
			next[i] = entry
			continue
		}
		changed := false
		if name, ok := rewriteCallField(asString(rec["name"])); ok {
			rec["name"] = name
			changed = true
		}
		if key, ok := rewriteCallField(asString(rec["var"])); ok {
			rec["var"] = key
			changed = true
		}
		if changed {
			next[i] = rec
		} else {
			next[i] = entry
		}
	}
	data[codedflow.CallsKey] = next
}

// soleCategoryProductID returns the product id when the collection has exactly
// one product that exposes options. Used only for early-handoff add-on prompts.
func soleCategoryProductID(c *Conv, categoryID string) string {
	products, ok := c.StoreList("early_handoff_products", "list_products", map[string]string{"category_id": categoryID})
	if !ok {
		return ""
	}
	withOptions := make([]map[string]any, 0, len(products))
	listed := make([]map[string]any, 0, len(products))
	for _, entry := range products {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		listed = append(listed, item)
		if n := productOptionCount(item); n > 0 {
			withOptions = append(withOptions, item)
		}
	}
	if len(withOptions) == 1 {
		return fieldString(withOptions[0], "id")
	}
	if len(withOptions) == 0 && len(listed) == 1 {
		return fieldString(listed[0], "id")
	}
	return ""
}

func productOptionCount(product map[string]any) int {
	if product == nil {
		return 0
	}
	if opts, ok := anySlice(product["options"]); ok {
		return len(opts)
	}
	if opts, ok := anySlice(product["active_options"]); ok {
		return len(opts)
	}
	return 0
}

func earlyHandoffCollectionForProduct(c *Conv, product map[string]any) map[string]any {
	if product == nil {
		return nil
	}
	if cat, ok := asStringMap(product["category"]); ok {
		if id := fieldString(cat, "id"); id != "" {
			if col := collectionByID(c, id); col != nil && collectionHandoffEarly(col) {
				return col
			}
			if collectionHandoffEarly(cat) {
				return cat
			}
		}
	}
	if id := productCategoryID(product); id != "" {
		if col := collectionByID(c, id); col != nil && collectionHandoffEarly(col) {
			return col
		}
	}
	if id := strings.TrimSpace(asString(c.Session().SessionData["collection_id"])); id != "" {
		if col := collectionByID(c, id); col != nil && collectionHandoffEarly(col) {
			return col
		}
	}
	return nil
}

// runEarlyHandoff collects capture → add-ons → Meta Flow details,
// then creates a commerce agent transfer. It does not write tiqr_cart or place
// an order. The known product option is copied onto the handoff cart so the
// store order form can select it. Fulfillment time is skipped for now.
func runEarlyHandoff(c *Conv, collection map[string]any, productID string) error {
	if collection == nil {
		return c.Transfer(codedflow.AgentHandoff)
	}
	MigrateEarlyHandoffSessionKeys(c.Session())

	categoryID := fieldString(collection, "id")
	name := strings.TrimSpace(asString(collection["name"]))
	if name == "" {
		name = "order"
	}

	c.Once("early_handoff_select_category", func() {
		c.App().SetSelectedCategoryID(c.Session(), categoryID)
		c.Session().SessionData["collection_id"] = categoryID
		c.Session().SessionData["collection_name"] = name
		if productID != "" {
			c.Session().SessionData["product_id"] = productID
			c.Session().SessionData["early_handoff_product_id"] = productID
		}
	})

	c.Say(fmt.Sprintf(tiqrEcommerceEarlyHandoffIntro, name))

	if !askEarlyHandoffCaptureFields(c, collection) {
		return nil
	}
	if !askEarlyHandoffAddons(c, productID) {
		return nil
	}
	if !askEarlyHandoffCustomerDetails(c) {
		return nil
	}
	return finishCodedEarlyHandoff(c, collection)
}

func askEarlyHandoffCaptureFields(c *Conv, collection map[string]any) bool {
	// Replay every field in order. Skipping a field already stored in
	// commerce_captured_fields would skip its call record and shift later
	// answers (and Skip) onto the wrong steps.
	fields := RequiredCaptureFieldsFrom(collection)
	for i, field := range fields {
		name := captureCallName(i, asString(field["key"]))
		value, ok := c.askCaptureFieldNoCheckout(name, field)
		if !ok {
			return false
		}
		saveSessionCapture(c, field, value)
	}
	return !c.Stop
}

// askCaptureFieldNoCheckout is askCaptureField without diverting to checkout.
func (c *Conv) askCaptureFieldNoCheckout(name string, field map[string]any) (any, bool) {
	if c.Stop {
		return nil, false
	}
	if rec, done := c.DoneCall(); done {
		if !codedflow.CallOK(rec) {
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
	if c.NoAnswerYet() {
		if !c.sendCapturePrompt(name, body) {
			return nil, false
		}
		c.Wait(name)
		return nil, false
	}
	input := strings.TrimSpace(c.ChatCtx().UserInput())
	if input == "" || !validCaptureValue(field, input) {
		c.ChatCtx().SetConsumed(true)
		if !c.sendCapturePrompt(name, "Please provide a valid value.\n"+body) {
			return nil, false
		}
		c.Wait(name)
		return nil, false
	}
	value := normalizedCaptureValue(field, input)
	c.ChatCtx().SetConsumed(true)
	c.AppendCall(map[string]any{"name": name, "ok": true, "value": value})
	return value, true
}

func askEarlyHandoffAddons(c *Conv, productID string) bool {
	// A session already waiting on the free-text step must keep that call in
	// place. Inserting get_product records in front of it would consume the
	// saved reply as a product fetch.
	if earlyHandoffFreeTextAlreadyRecorded(c) {
		return askEarlyHandoffFreeTextAddons(c)
	}
	choices := collectEarlyHandoffAddonChoices(c, productID)
	if c.Stop {
		return false
	}
	if len(choices) > 0 {
		return askStructuredCatalogAddons(c, choices, "early_handoff_addons")
	}
	return askEarlyHandoffFreeTextAddons(c)
}

func earlyHandoffFreeTextAlreadyRecorded(c *Conv) bool {
	if c == nil {
		return false
	}
	records := c.CallRecords()
	if c.Seq() >= len(records) {
		return false
	}
	switch asString(records[c.Seq()]["name"]) {
	case "early_handoff_addons_free", "themed_addons_free", "after_capture_addons_free":
		return true
	default:
		return false
	}
}

func askEarlyHandoffFreeTextAddons(c *Conv) bool {
	text, ok := c.AskText("early_handoff_addons_free",
		"Any add-ons (candles, flowers, etc.)? Reply with details, or say Skip.",
		codedflow.StepNote{
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
	c.Once("early_handoff_addons_free_save", func() {
		notes := jsonMapFromSession(c.Session(), "commerce_notes")
		notes["addon_requests"] = strings.TrimSpace(text)
		c.Session().SessionData["commerce_notes"] = map[string]any(notes)
	})
	return true
}

// collectEarlyHandoffAddonChoices loads catalog add-ons for the handoff product.
// When the collection was not narrowed to one product, each listed product is
// loaded so the numbered catalog step can run instead of the free-text prompt.
func collectEarlyHandoffAddonChoices(c *Conv, productID string) []map[string]any {
	ids := earlyHandoffAddonProductIDs(c, productID)
	var choices []map[string]any
	for i, id := range ids {
		prefix := "early_handoff_addons"
		if len(ids) > 1 {
			prefix = fmt.Sprintf("early_handoff_addons_%d", i)
		}
		choices = mergeAddonChoices(choices, loadCatalogAddonChoices(c, id, prefix))
		if c != nil && c.Stop {
			return nil
		}
	}
	return choices
}

func earlyHandoffAddonProductIDs(c *Conv, productID string) []string {
	productID = strings.TrimSpace(productID)
	if productID == "" && c != nil && c.Session() != nil && c.Session().SessionData != nil {
		productID = strings.TrimSpace(asString(c.Session().SessionData["early_handoff_product_id"]))
	}
	if productID != "" {
		return []string{productID}
	}
	if c == nil || c.Session() == nil || c.Session().SessionData == nil {
		return nil
	}
	items, ok := anySlice(c.Session().SessionData["early_handoff_products"])
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		id := fieldString(item, "id")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func askEarlyHandoffFulfillmentTime(c *Conv) bool {
	mode := strings.TrimSpace(asString(c.Session().SessionData["delivery_mode"]))
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
		if store, ok := asStringMap(c.Session().SessionData["store"]); ok {
			if tz := asString(store["timezone"]); tz != "" {
				tzName = tz
			}
		}
	}
	c.Once("early_handoff_slot_meta", func() {
		c.Session().SessionData["early_handoff_earliest_at"] = earliestAt
		c.Session().SessionData["early_handoff_timezone"] = tzName
		c.Session().SessionData["delivery_mode"] = mode
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
		text, ok := c.AskText(fmt.Sprintf("fulfillment_time_%d", attempt), body, codedflow.StepNote{
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
		c.Once(fmt.Sprintf("early_handoff_slot_saved_%d", attempt), func() {
			c.Session().SessionData["fulfillment_slot_token"] = asString(slot["token"])
			c.Session().SessionData["requested_fulfillment_at"] = asString(slot["requested_fulfillment_at"])
			if promised := asString(slot["promised_ready_at"]); promised != "" {
				c.Session().SessionData["promised_ready_at"] = promised
			}
			c.Session().SessionData["early_handoff_timezone"] = firstNonEmpty(asString(slot["timezone"]), tzName)
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

func askEarlyHandoffCustomerDetails(c *Conv) bool {
	detailsBody := "Please share your name, email, and phone number so we can continue your request."
	flowID := TiqrEcommercePickupFlowID
	if asString(c.Session().SessionData["delivery_mode"]) == tiqrModeDelivery {
		detailsBody = "Please share your name, phone number, and address so we can continue your request."
		flowID = tiqrEcommerceFlowID
	}
	return c.AskFlow("early_handoff_details", codedflow.FlowPrompt{
		FlowID: flowID,
		CTA:    "Enter details",
		Header: "Your details",
		Body:   detailsBody,
		Step: codedflow.StepNote{
			Doing:  "Collecting customer details for a custom request before agent handoff.",
			Expect: "A WhatsApp Flow submission with contact details.",
		},
	})
}

func finishCodedEarlyHandoff(c *Conv, collection map[string]any) error {
	if c.Stop || c.Ended {
		return nil
	}
	session := c.Session()
	if c.ChatCtx() == nil || c.ChatCtx().Account() == nil || c.ChatCtx().Contact() == nil || session == nil {
		c.Say(tiqrEcommerceEarlyHandoffFail)
		c.Stop = true
		return nil
	}
	stageEarlyHandoffProductOptionCart(session)
	if !c.App().CompleteCodedEarlyHandoff(c.ChatCtx(), collection) {
		c.Say(tiqrEcommerceEarlyHandoffFail)
		c.Stop = true
		return nil
	}
	clearCodedEarlyHandoffSession(session)
	c.Stop = true
	c.Ended = true
	return nil
}

// earlyHandoffAddressFromFlow builds the draft address snapshot from WhatsApp Flow fields.
func earlyHandoffAddressFromFlow(data map[string]any) map[string]any {
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

// stageEarlyHandoffProductOptionCart copies the product's option onto the
// handoff cart. A custom request never builds tiqr_cart, so without this the
// order form has no product option to select.
func stageEarlyHandoffProductOptionCart(session *models.ChatbotSession) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	if len(tiqrCartLinesFromData(session.SessionData)) > 0 {
		session.SessionData[HandoffCartKey] = map[string]any(tiqrEcommerceCartSnapshot(session))
		return
	}
	line, ok := EarlyHandoffProductOptionLine(session.SessionData)
	if !ok {
		return
	}
	session.SessionData[HandoffCartKey] = map[string]any{
		"source": CartSource,
		"lines":  []any{line},
	}
}

func EarlyHandoffProductOptionLine(data map[string]any) (map[string]any, bool) {
	if data == nil {
		return nil, false
	}
	productID := strings.TrimSpace(asString(data["early_handoff_product_id"]))
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
	optionID, optionName, ok := pickEarlyHandoffProductOption(data, options)
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
	for _, key := range []string{"early_handoff_products", "products"} {
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
	if raw, ok := asStringMap(data["early_handoff_product"]); ok {
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

func pickEarlyHandoffProductOption(data map[string]any, options []map[string]any) (string, string, bool) {
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

func clearCodedEarlyHandoffSession(session *models.ChatbotSession) {
	if session == nil {
		return
	}
	if session.SessionData != nil {
		delete(session.SessionData, codedflow.DataKey)
	}
	session.CurrentStep = ""
	session.StepRetries = 0
	session.CurrentFlowID = nil
	session.Status = models.SessionStatusCancelled
	now := time.Now()
	session.CompletedAt = &now
}
