package handlers

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	codedCallsKey        = "_coded_calls"
	codedTranslationsKey = "_translations"
	customerLanguageKey  = "customer_language"
)

// Choice is one accepted answer. ID is the button or row id, never a translated title.
type Choice struct {
	ID    string
	Title string
}

// Button is one reply button. ID stays in English. Title is shown to the customer.
type Button struct {
	ID    string
	Title string
}

// StepNote describes what a question is doing and what a valid reply looks like.
// The intent role receives both so free text can be matched to this step.
type StepNote struct {
	Doing  string
	Expect string
}

// ButtonPrompt is a reply-button question.
type ButtonPrompt struct {
	Body    string
	Buttons []Button
	Step    StepNote
}

// ListPrompt is a list question. Title and Description are item templates
// and are not translated, so catalog names stay as the store sent them.
type ListPrompt struct {
	Body        string
	Header      string
	Footer      string
	Button      string
	Section     string
	ItemsKey    string
	IDField     string
	Title       string
	Description string
	Select      map[string]string
	Step        StepNote
}

// CarouselPrompt is a product carousel. BodyField and MediaField are item templates.
type CarouselPrompt struct {
	Body          string
	ItemsKey      string
	IDField       string
	StoreAs       string
	BodyField     string
	MediaField    string
	Title         string
	FallbackMedia string
	Select        map[string]string
	Step          StepNote
}

// ImageButtonPrompt is a reply-button question with an image header.
// Body and HeaderImage are session templates. BodyField labels the choice for
// intent matching (same shape as a carousel card body). Title is the button.
type ImageButtonPrompt struct {
	Body          string
	HeaderImage   string
	FallbackMedia string
	ItemsKey      string
	IDField       string
	BodyField     string
	Title         string
	StoreAs       string
	Select        map[string]string
	Step          StepNote
}

// NumberPrompt asks for text that matches Pattern.
type NumberPrompt struct {
	Body    string
	Pattern string
	Step    StepNote
}

// FlowPrompt opens a WhatsApp Flow. Only the opener text is translated.
type FlowPrompt struct {
	FlowID string
	Header string
	Body   string
	CTA    string
	Step   StepNote
}

// LocationPrompt asks the customer to share a WhatsApp location pin.
type LocationPrompt struct {
	Body string
	Step StepNote
}

// LocationPin is a shared WhatsApp location.
type LocationPin struct {
	Latitude  float64
	Longitude float64
	Name      string
	Address   string
}

// Conv is one replay of a coded flow. Finished calls return their saved
// result and do not send or call the store again.
type Conv struct {
	app        *App
	chat       *chatNodeCtx
	seq        int
	stop       bool
	ended      bool
	err        error
	skipIntent bool   // true after a failed normalized answer; avoid a second AI call
	divert     string // codedRouteCheckout when free text should leave the current ask
}

func (c *Conv) session() *models.ChatbotSession {
	return c.chat.session
}

func (c *Conv) fail(err error) {
	if err == nil || c.err != nil {
		return
	}
	c.err = err
	c.stop = true
}

// Say sends one authored message. A replay skips it.
func (c *Conv) Say(message string) {
	if c.stop {
		return
	}
	if _, done := c.doneCall(); done {
		return
	}
	node := &ChatNode{ID: "say", Type: ChatNodeMessage, Config: map[string]any{"message": c.text(message)}}
	if _, err := c.app.execChatMessage(node, c.chat); err != nil {
		c.fail(err)
		return
	}
	c.appendCall(map[string]any{"name": "say", "ok": true})
}

// SayPaymentCTA asks the shopper to pay after create_order, matching ecommerce checkout.
// When payment_url is present it sends a Pay now CTA URL button; otherwise text only.
func (c *Conv) SayPaymentCTA(order map[string]any) {
	if c.stop {
		return
	}
	if _, done := c.doneCall(); done {
		return
	}
	body, paymentURL := codedPaymentCTAContent(order, sessionCurrencyCode(c.session()))
	body = c.text(body)
	if paymentURL == "" {
		node := &ChatNode{ID: "say_payment_cta", Type: ChatNodeMessage, Config: map[string]any{"message": body}}
		if _, err := c.app.execChatMessage(node, c.chat); err != nil {
			c.fail(err)
			return
		}
		c.appendCall(map[string]any{"name": "say_payment_cta", "ok": true})
		return
	}
	if err := c.app.deliverCodedCTAURL(c.chat, "say_payment_cta", body, "Pay now", paymentURL); err != nil {
		fallback := body + "\nPay here: " + paymentURL
		node := &ChatNode{ID: "say_payment_cta", Type: ChatNodeMessage, Config: map[string]any{"message": fallback}}
		if _, err := c.app.execChatMessage(node, c.chat); err != nil {
			c.fail(err)
			return
		}
	}
	c.appendCall(map[string]any{"name": "say_payment_cta", "ok": true})
}

// codedPaymentCTAContent builds the order-placed body and payment URL for coded flows.
// It accepts payment_url from compactOrderCreateResult (nested payment.meta_data) or a
// top-level payment_url (preview mocks).
func codedPaymentCTAContent(order map[string]any, currency string) (string, string) {
	compacted := compactOrderCreateResult(order, currency)
	if asString(compacted["payment_url"]) == "" {
		if u := strings.TrimSpace(asString(order["payment_url"])); u != "" {
			compacted["payment_url"] = u
		}
	}
	return paymentCTAContent(compacted)
}

// AskButtons accepts only a button id from this prompt.
func (c *Conv) AskButtons(name string, prompt ButtonPrompt) (Choice, bool) {
	if choice, done, ok := c.replayChoice(); done {
		return choice, ok
	}
	return c.askChoice(name, c.buttonConfig(prompt))
}

// AskList accepts only a row id from the items just shown.
func (c *Conv) AskList(name string, items []any, prompt ListPrompt) (Choice, bool) {
	if choice, done, ok := c.replayChoice(); done {
		return choice, ok
	}
	if prompt.ItemsKey != "" && items != nil {
		c.session().SessionData[prompt.ItemsKey] = items
	}
	return c.askChoice(name, c.listConfig(prompt))
}

// AskCarousel accepts only a product id from the cards just shown.
func (c *Conv) AskCarousel(name string, items []any, prompt CarouselPrompt) (Choice, bool) {
	if choice, done, ok := c.replayChoice(); done {
		return choice, ok
	}
	if prompt.ItemsKey != "" && items != nil {
		c.session().SessionData[prompt.ItemsKey] = items
	}
	return c.askChoice(name, c.carouselConfig(prompt))
}

// AskImageButtons accepts a reply button from an image-header prompt.
func (c *Conv) AskImageButtons(name string, items []any, prompt ImageButtonPrompt) (Choice, bool) {
	if choice, done, ok := c.replayChoice(); done {
		return choice, ok
	}
	if prompt.ItemsKey != "" && items != nil {
		c.session().SessionData[prompt.ItemsKey] = items
	}
	return c.askChoice(name, c.imageButtonConfig(prompt))
}

// AskNumber accepts only text that matches the prompt pattern.
func (c *Conv) AskNumber(name string, prompt NumberPrompt) (string, bool) {
	if c.stop {
		return "", false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return "", false
		}
		return asString(c.session().SessionData[name]), true
	}
	body := c.text(prompt.Body)
	if c.chat.consumed || strings.TrimSpace(c.chat.userInput) == "" {
		node := &ChatNode{ID: name, Type: ChatNodeMessage, Config: map[string]any{"message": body}}
		if _, err := c.app.execChatMessage(node, c.chat); err != nil {
			c.fail(err)
			return "", false
		}
		if c.chat.capturing() {
			c.chat.preview.expectText()
		}
		c.wait(name)
		return "", false
	}
	re, err := regexp.Compile(prompt.Pattern)
	if err != nil {
		c.fail(err)
		return "", false
	}
	input := strings.TrimSpace(c.chat.userInput)
	if !re.MatchString(input) {
		if c.skipIntent {
			c.skipIntent = false
			return "", false
		}
		cfg := numberConfig(body, prompt)
		route, ok := c.resolveFreeText(name, cfg, RouteOptions{})
		if ok && route.Kind == codedRouteCheckout {
			return "", false
		}
		if !ok || route.Kind != codedRouteAnswer {
			return "", false
		}
		input = strings.TrimSpace(route.Answer)
		if !re.MatchString(input) {
			c.skipIntent = true
			return "", false
		}
	}
	c.chat.consumed = true
	c.session().SessionData[name] = input
	c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": input})
	return input, true
}

// AskText accepts any non-empty reply and stores it under name.
func (c *Conv) AskText(name, body string, step StepNote) (string, bool) {
	if c.stop {
		return "", false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return "", false
		}
		return asString(c.session().SessionData[name]), true
	}
	message := c.text(body)
	if c.chat.consumed || strings.TrimSpace(c.chat.userInput) == "" {
		node := &ChatNode{ID: name, Type: ChatNodeMessage, Config: map[string]any{"message": message}}
		if _, err := c.app.execChatMessage(node, c.chat); err != nil {
			c.fail(err)
			return "", false
		}
		if c.chat.capturing() {
			c.chat.preview.expectText()
		}
		c.wait(name)
		return "", false
	}
	input := strings.TrimSpace(c.chat.userInput)
	if input == "" {
		return "", false
	}
	c.chat.consumed = true
	c.session().SessionData[name] = input
	c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": input})
	_ = step // notes reserved for future intent on this step
	return input, true
}

// AskLocation sends a WhatsApp location request and accepts a pin reply.
func (c *Conv) AskLocation(name string, prompt LocationPrompt) (LocationPin, bool) {
	if c.stop {
		return LocationPin{}, false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return LocationPin{}, false
		}
		return locationPinFromSession(c.session().SessionData, name), true
	}
	body := c.text(prompt.Body)
	if c.chat.consumed || strings.TrimSpace(c.chat.userInput) == "" {
		if err := c.app.deliverCodedLocationRequest(c.chat, name, body); err != nil {
			c.fail(err)
			return LocationPin{}, false
		}
		c.wait(name)
		return LocationPin{}, false
	}
	pin, ok := parseLocationInput(c.chat.userInput)
	if !ok {
		if c.skipIntent {
			c.skipIntent = false
			return LocationPin{}, false
		}
		cfg := map[string]any{"body": body, "mode": "location"}
		putStepNote(cfg, prompt.Step)
		_, _ = c.resolveFreeText(name, cfg, RouteOptions{})
		return LocationPin{}, false
	}
	c.chat.consumed = true
	c.session().SessionData[name] = map[string]any{
		"latitude":  pin.Latitude,
		"longitude": pin.Longitude,
		"name":      pin.Name,
		"address":   pin.Address,
	}
	c.session().SessionData["delivery_latitude"] = pin.Latitude
	c.session().SessionData["delivery_longitude"] = pin.Longitude
	c.appendCall(map[string]any{
		"name": name,
		"ok":   true,
		"var":  name,
		"value": map[string]any{
			"latitude":  pin.Latitude,
			"longitude": pin.Longitude,
		},
	})
	_ = prompt.Step
	return pin, true
}

// AskFlow accepts a WhatsApp Flow submission. Free text is a diversion.
func (c *Conv) AskFlow(name string, prompt FlowPrompt) bool {
	if c.stop {
		return false
	}
	if rec, done := c.doneCall(); done {
		return callOK(rec)
	}
	cfg := map[string]any{
		"flow_id": prompt.FlowID,
		"header":  c.text(prompt.Header),
		"body":    c.text(prompt.Body),
		"cta":     c.text(prompt.CTA),
	}
	putStepNote(cfg, prompt.Step)
	node := &ChatNode{ID: name, Type: ChatNodeWhatsAppFlow, Config: cfg}
	if !c.chat.consumed && len(c.chat.flowResponseData) > 0 {
		if _, err := c.app.execChatWhatsAppFlow(node, c.chat); err != nil {
			c.fail(err)
			return false
		}
		c.appendCall(map[string]any{"name": name, "ok": true, "fields": c.chat.flowResponseData})
		return true
	}
	if c.chat.consumed || (c.chat.buttonID == "" && strings.TrimSpace(c.chat.userInput) == "" && len(c.chat.flowResponseData) == 0) {
		if _, err := c.app.execChatWhatsAppFlow(node, c.chat); err != nil {
			c.fail(err)
			return false
		}
		c.wait(name)
		return false
	}
	_, _ = c.resolveFreeText(name, cfg, RouteOptions{})
	return false
}

// Store calls a TiQR REST operation once and returns the payload.
func (c *Conv) Store(name, operation string, params map[string]string) (map[string]any, bool) {
	return c.storeAPI(name, operation, "rest", params)
}

// StoreMCP calls a TiQR MCP operation once and returns the payload.
func (c *Conv) StoreMCP(name, operation string, params map[string]string) (map[string]any, bool) {
	return c.storeAPI(name, operation, "mcp", params)
}

func (c *Conv) storeAPI(name, operation, apiType string, params map[string]string) (map[string]any, bool) {
	if c.stop {
		return nil, false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return nil, false
		}
		payload, _ := asStringMap(c.session().SessionData[name])
		return payload, true
	}
	ok, err := c.tiqr(name, operation, apiType, params)
	if c.waitingForPreviewMock() {
		return nil, false
	}
	if err != nil {
		c.fail(err)
		return nil, false
	}
	if !ok || c.chat.lastTiqr == nil {
		c.appendCall(map[string]any{"name": name, "ok": false})
		return nil, false
	}
	c.session().SessionData[name] = c.chat.lastTiqr
	c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": c.chat.lastTiqr})
	return c.chat.lastTiqr, true
}

// StoreList calls a TiQR list operation once and returns its results.
func (c *Conv) StoreList(name, operation string, params map[string]string) ([]any, bool) {
	if c.stop {
		return nil, false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return nil, false
		}
		items, _ := anySlice(c.session().SessionData[name])
		return items, true
	}
	ok, err := c.tiqr(name, operation, "rest", params)
	if c.waitingForPreviewMock() {
		return nil, false
	}
	if err != nil {
		c.fail(err)
		return nil, false
	}
	var items []any
	if ok && c.chat.lastTiqr != nil {
		items, ok = anySlice(c.chat.lastTiqr["results"])
	}
	if !ok || len(items) == 0 {
		c.appendCall(map[string]any{"name": name, "ok": false})
		return nil, false
	}
	c.session().SessionData[name] = items
	c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": items})
	return items, true
}

// LookupOrder loads the latest order for this WhatsApp number. The lookup
// is MCP-only, so it does not go through the REST store operations.
func (c *Conv) LookupOrder(name string) (map[string]any, bool) {
	if c.stop {
		return nil, false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return nil, false
		}
		order, _ := asStringMap(c.session().SessionData[name])
		return order, true
	}
	if c.chat.capturing() && c.chat.preview.mock {
		order, ok := c.chat.preview.mockFor("lookup_order_status")
		if !ok {
			c.stop = true
			return nil, false
		}
		c.session().SessionData[name] = order
		c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": order})
		return order, true
	}
	order, err := lookupLatestOrder(c.app, c.chat.account, c.session())
	if err != nil || order == nil {
		c.appendCall(map[string]any{"name": name, "ok": false})
		return nil, false
	}
	c.session().SessionData[name] = order
	c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": order})
	return order, true
}

// Once runs fn on the first visit and skips it on replay.
func (c *Conv) Once(name string, fn func()) bool {
	if c.stop {
		return false
	}
	if _, done := c.doneCall(); done {
		return true
	}
	fn()
	c.appendCall(map[string]any{"name": name, "ok": true})
	return true
}

// Transfer tells the customer, queues the chat, and ends the flow.
func (c *Conv) Transfer(message string) error {
	if c.ended {
		return nil
	}
	node := &ChatNode{ID: "transfer", Type: ChatNodeTransfer, Config: map[string]any{"body": c.text(message)}}
	_, err := c.app.execChatTransfer(node, c.chat)
	finishCodedSession(c.session())
	c.stop = true
	c.ended = true
	return err
}

// End marks the coded flow finished.
func (c *Conv) End() error {
	if c.ended {
		return nil
	}
	if c.chat.capturing() && c.chat.preview.needsMock != "" {
		c.stop = true
		return nil
	}
	finishCodedSession(c.session())
	c.stop = true
	c.ended = true
	return nil
}

func (c *Conv) replayChoice() (Choice, bool, bool) {
	if c.stop {
		return Choice{}, true, false
	}
	rec, done := c.doneCall()
	if !done {
		return Choice{}, false, false
	}
	if !callOK(rec) {
		return Choice{}, true, false
	}
	return Choice{ID: asString(rec["id"]), Title: asString(rec["title"])}, true, true
}

func (c *Conv) askChoice(name string, cfg map[string]any) (Choice, bool) {
	if c.stop {
		return Choice{}, false
	}
	if id := c.offeredButtonID(cfg); id != "" {
		c.chat.buttonID = id
		return c.acceptButton(name, cfg)
	}
	// A button tap that is not one of this step's choices is not a request
	// for an agent. Show the choices again.
	if !c.noAnswerYet() && strings.TrimSpace(c.chat.buttonID) != "" {
		c.app.Log.Warn("Coded flow button did not match this step",
			"step", name,
			"button_id", c.chat.buttonID,
			"text", c.chat.userInput,
		)
		c.chat.buttonID = ""
		c.chat.userInput = ""
		c.chat.consumed = false
		c.chat.flowResponseData = nil
	}
	if c.noAnswerYet() {
		node := &ChatNode{ID: name, Type: ChatNodeButtons, Config: cfg}
		out, err := c.app.execChatButtons(node, c.chat)
		if err != nil {
			c.fail(err)
			return Choice{}, false
		}
		if out.yield {
			c.wait(name)
		}
		return Choice{}, false
	}
	route, ok := c.resolveFreeText(name, cfg, RouteOptions{})
	if !ok || route.Kind != codedRouteChoice {
		return Choice{}, false
	}
	return Choice{ID: route.ID, Title: route.Title}, true
}

const codedAgentHandoff = "I'm connecting you with a team member who can help."

func (c *Conv) matchingButton(cfg map[string]any) bool {
	return c.offeredButtonID(cfg) != ""
}

// offeredButtonID returns the primary id of a button offered by this step
// when the inbound reply matches by id (exact after trim) or by title
// (case-insensitive). Empty means no match.
func (c *Conv) offeredButtonID(cfg map[string]any) string {
	if c.chat.consumed {
		return ""
	}
	buttons, err := buttonsForNode(cfg, c.session().SessionData)
	if err != nil || len(buttons) == 0 {
		return ""
	}
	buttonID := strings.TrimSpace(c.chat.buttonID)
	userInput := strings.TrimSpace(c.chat.userInput)
	titleMatch := ""
	for _, button := range buttons {
		id := fieldString(button, "id")
		id2 := fieldString(button, "id_2")
		primary := id
		if primary == "" {
			primary = id2
		}
		if buttonID != "" && (buttonID == id || buttonID == id2) {
			return primary
		}
		if titleMatch == "" && userInput != "" && strings.EqualFold(userInput, fieldString(button, "title")) {
			titleMatch = primary
		}
	}
	return titleMatch
}

func (c *Conv) noAnswerYet() bool {
	return c.chat.consumed || (c.chat.buttonID == "" && strings.TrimSpace(c.chat.userInput) == "" && len(c.chat.flowResponseData) == 0)
}

func (c *Conv) acceptButton(name string, cfg map[string]any) (Choice, bool) {
	node := &ChatNode{ID: name, Type: ChatNodeButtons, Config: cfg}
	if _, err := c.app.execChatButtons(node, c.chat); err != nil {
		c.fail(err)
		return Choice{}, false
	}
	choice := Choice{ID: c.chat.buttonID, Title: c.chat.userInput}
	c.appendCall(map[string]any{
		"name":   name,
		"ok":     true,
		"id":     choice.ID,
		"title":  choice.Title,
		"fields": snapshotFields(c.session().SessionData, cfg),
	})
	return choice, true
}

func (c *Conv) wait(name string) {
	c.session().CurrentStep = name
	c.stop = true
}

func (c *Conv) waitingForPreviewMock() bool {
	if !c.chat.capturing() || c.chat.preview.needsMock == "" {
		return false
	}
	c.stop = true
	return true
}

func (c *Conv) tiqr(name, operation, apiType string, params map[string]string) (bool, error) {
	if apiType == "" {
		apiType = "rest"
	}
	raw := make(map[string]any, len(params))
	for key, value := range params {
		raw[key] = value
	}
	node := &ChatNode{
		ID:   name,
		Type: ChatNodeTiqrStoreAPI,
		Config: map[string]any{
			"api_type":  apiType,
			"operation": operation,
			"params":    raw,
		},
	}
	out, err := c.app.execChatTiqrStoreAPI(node, c.chat)
	if err != nil {
		return false, err
	}
	return out.outcome == "http:2xx", nil
}

func parseLocationInput(raw string) (LocationPin, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw[0] != '{' {
		return LocationPin{}, false
	}
	var payload struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Name      string  `json:"name"`
		Address   string  `json:"address"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return LocationPin{}, false
	}
	if payload.Latitude == 0 && payload.Longitude == 0 {
		return LocationPin{}, false
	}
	return LocationPin{
		Latitude:  payload.Latitude,
		Longitude: payload.Longitude,
		Name:      payload.Name,
		Address:   payload.Address,
	}, true
}

func locationPinFromSession(data models.JSONB, name string) LocationPin {
	if data == nil {
		return LocationPin{}
	}
	raw, ok := asStringMap(data[name])
	if !ok {
		return LocationPin{}
	}
	lat, _ := anyToFloat64(raw["latitude"])
	lng, _ := anyToFloat64(raw["longitude"])
	return LocationPin{
		Latitude:  lat,
		Longitude: lng,
		Name:      asString(raw["name"]),
		Address:   asString(raw["address"]),
	}
}

func (c *Conv) buttonConfig(prompt ButtonPrompt) map[string]any {
	buttons := make([]any, 0, len(prompt.Buttons))
	for _, button := range prompt.Buttons {
		buttons = append(buttons, map[string]any{
			"id":    button.ID,
			"type":  "reply",
			"title": c.limit(button.Title, 20),
		})
	}
	cfg := map[string]any{
		"body":    c.text(prompt.Body),
		"mode":    "reply",
		"source":  "static",
		"buttons": buttons,
	}
	putStepNote(cfg, prompt.Step)
	return cfg
}

func (c *Conv) listConfig(prompt ListPrompt) map[string]any {
	cfg := map[string]any{
		"body":              c.text(prompt.Body),
		"mode":              "list",
		"header":            c.limit(prompt.Header, 60),
		"footer":            c.limit(prompt.Footer, 60),
		"source":            "dynamic",
		"id_field":          prompt.IDField,
		"items_var":         prompt.ItemsKey,
		"list_button":       c.limit(prompt.Button, 20),
		"title_field":       prompt.Title,
		"section_title":     c.limit(prompt.Section, 24),
		"description_field": prompt.Description,
		"selection_mapping": stringMapAny(prompt.Select),
	}
	putStepNote(cfg, prompt.Step)
	return cfg
}

func (c *Conv) carouselConfig(prompt CarouselPrompt) map[string]any {
	cfg := map[string]any{
		"body":               c.text(prompt.Body),
		"mode":               "carousel",
		"source":             "dynamic",
		"id_field":           prompt.IDField,
		"store_as":           prompt.StoreAs,
		"items_var":          prompt.ItemsKey,
		"body_field":         prompt.BodyField,
		"media_field":        prompt.MediaField,
		"title_field":        c.limit(prompt.Title, 20),
		"fallback_media_url": prompt.FallbackMedia,
		"selection_mapping":  stringMapAny(prompt.Select),
	}
	putStepNote(cfg, prompt.Step)
	return cfg
}

func (c *Conv) imageButtonConfig(prompt ImageButtonPrompt) map[string]any {
	cfg := map[string]any{
		"body":               c.text(prompt.Body),
		"mode":               "reply",
		"source":             "dynamic",
		"dynamic_type":       "reply",
		"id_field":           prompt.IDField,
		"store_as":           prompt.StoreAs,
		"items_var":          prompt.ItemsKey,
		"body_field":         prompt.BodyField,
		"title_field":        c.limit(prompt.Title, 20),
		"header_image":       prompt.HeaderImage,
		"fallback_media_url": prompt.FallbackMedia,
		"selection_mapping":  stringMapAny(prompt.Select),
	}
	putStepNote(cfg, prompt.Step)
	return cfg
}

func numberConfig(body string, prompt NumberPrompt) map[string]any {
	cfg := map[string]any{
		"body":    body,
		"pattern": prompt.Pattern,
	}
	putStepNote(cfg, prompt.Step)
	return cfg
}

func putStepNote(cfg map[string]any, step StepNote) {
	if strings.TrimSpace(step.Doing) != "" {
		cfg["step_doing"] = step.Doing
	}
	if strings.TrimSpace(step.Expect) != "" {
		cfg["step_expect"] = step.Expect
	}
}

func (c *Conv) doneCall() (map[string]any, bool) {
	if c.stop {
		return nil, false
	}
	records := c.callRecords()
	if c.seq >= len(records) {
		return nil, false
	}
	rec := records[c.seq]
	c.seq++
	c.restore(rec)
	return rec, true
}

func (c *Conv) restore(rec map[string]any) {
	data := c.session().SessionData
	if fields, ok := asStringMap(rec["fields"]); ok {
		for key, value := range fields {
			data[key] = value
		}
	}
	if key := asString(rec["var"]); key != "" {
		if value, ok := rec["value"]; ok {
			data[key] = value
		}
	}
}

func (c *Conv) callRecords() []map[string]any {
	items, ok := anySlice(c.session().SessionData[codedCallsKey])
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rec, ok := asStringMap(item)
		if ok {
			out = append(out, rec)
		}
	}
	return out
}

func (c *Conv) appendCall(rec map[string]any) {
	items, _ := anySlice(c.session().SessionData[codedCallsKey])
	next := make([]any, 0, len(items)+1)
	next = append(next, items...)
	next = append(next, rec)
	c.session().SessionData[codedCallsKey] = next
	// This call just ran. Move the cursor past it so the next step in this
	// same turn does not replay it as its own saved result.
	c.seq = len(c.callRecords())
}

func callOK(rec map[string]any) bool {
	ok, _ := rec["ok"].(bool)
	return ok
}

func snapshotFields(data models.JSONB, cfg map[string]any) map[string]any {
	keys := map[string]struct{}{}
	if mapping, ok := cfg["selection_mapping"].(map[string]any); ok {
		for key := range mapping {
			keys[key] = struct{}{}
		}
	}
	if storeAs := stringFromConfig(cfg, "store_as"); storeAs != "" {
		keys[storeAs] = struct{}{}
	}
	out := make(map[string]any, len(keys))
	for key := range keys {
		if value, ok := data[key]; ok {
			out[key] = value
		}
	}
	return out
}

func buttonOffered(cfg map[string]any, data models.JSONB, buttonID string) bool {
	buttonID = strings.TrimSpace(buttonID)
	if buttonID == "" {
		return false
	}
	buttons, err := buttonsForNode(cfg, data)
	if err != nil {
		return false
	}
	for _, button := range buttons {
		if fieldString(button, "id") == buttonID || fieldString(button, "id_2") == buttonID {
			return true
		}
	}
	return false
}

func stringMapAny(in map[string]string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
