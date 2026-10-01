package codedflow

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	CallsKey            = "_coded_calls"
	codedCallsKey       = CallsKey
	codedTranslationsKey = "_translations"
	CustomerLanguageKey = "customer_language"
	customerLanguageKey = CustomerLanguageKey
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
	app        Host
	chat       Chat
	seq        int
	Stop       bool
	Ended      bool
	err        error
	skipIntent bool   // true after a failed normalized answer; avoid a second AI call
	Divert     string // codedRouteCheckout when free text should leave the current ask
}

func (c *Conv) session() *models.ChatbotSession {
	return c.chat.Session()
}

// Session returns the chatbot session for this turn.
func (c *Conv) Session() *models.ChatbotSession { return c.session() }

// Text translates (when needed) and returns authored copy for the customer.
func (c *Conv) Text(src string) string { return c.text(src) }

// App returns the Host for this conversation.
func (c *Conv) App() Host { return c.app }

// ChatCtx returns the Chat for this turn.
func (c *Conv) ChatCtx() Chat { return c.chat }

// Fail records the first error and stops the turn.
func (c *Conv) Fail(err error) { c.fail(err) }

func (c *Conv) Seq() int { return c.seq }
func (c *Conv) SetSeq(n int) { c.seq = n }
func (c *Conv) AdvanceSeq() { c.seq++ }

func (c *Conv) fail(err error) {
	if err == nil || c.err != nil {
		return
	}
	c.err = err
	c.Stop = true
}

// Say sends one authored message. A replay skips it.
func (c *Conv) Say(message string) {
	if c.Stop {
		return
	}
	if _, done := c.doneCall(); done {
		return
	}
	if _, err := c.app.ExecChatMessage(c.chat, "say", map[string]any{"message": c.text(message)}); err != nil {
		c.fail(err)
		return
	}
	c.appendCall(map[string]any{"name": "say", "ok": true})
}

// SayPaymentCTA asks the shopper to pay after create_order, matching ecommerce checkout.
// When payment_url is present it sends a Pay now CTA URL button; otherwise text only.
func (c *Conv) SayPaymentCTA(order map[string]any) {
	if c.Stop {
		return
	}
	if _, done := c.doneCall(); done {
		return
	}
	body, paymentURL := c.codedPaymentCTAContent(order, c.app.SessionCurrencyCode(c.session()))
	body = c.text(body)
	if paymentURL == "" {
		if _, err := c.app.ExecChatMessage(c.chat, "say_payment_cta", map[string]any{"message": body}); err != nil {
			c.fail(err)
			return
		}
		c.appendCall(map[string]any{"name": "say_payment_cta", "ok": true})
		return
	}
	if err := c.app.DeliverCodedCTAURL(c.chat, "say_payment_cta", body, "Pay now", paymentURL); err != nil {
		fallback := body + "\nPay here: " + paymentURL
		if _, err := c.app.ExecChatMessage(c.chat, "say_payment_cta", map[string]any{"message": fallback}); err != nil {
			c.fail(err)
			return
		}
	}
	c.appendCall(map[string]any{"name": "say_payment_cta", "ok": true})
}

// codedPaymentCTAContent builds the order-placed body and payment URL for coded flows.
// It accepts payment_url from compactOrderCreateResult (nested payment.meta_data) or a
// top-level payment_url (preview mocks).
func (c *Conv) codedPaymentCTAContent(order map[string]any, currency string) (string, string) {
	compacted := c.app.CompactOrderCreateResult(order, currency)
	if asString(compacted["payment_url"]) == "" {
		if u := strings.TrimSpace(asString(order["payment_url"])); u != "" {
			compacted["payment_url"] = u
		}
	}
	return c.app.PaymentCTAContent(compacted)
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
	if c.Stop {
		return "", false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return "", false
		}
		return asString(c.session().SessionData[name]), true
	}
	body := c.text(prompt.Body)
	if c.chat.Consumed() || strings.TrimSpace(c.chat.UserInput()) == "" {
		if _, err := c.app.ExecChatMessage(c.chat, name, map[string]any{"message": body}); err != nil {
			c.fail(err)
			return "", false
		}
		if c.chat.Capturing() {
			c.chat.Preview().ExpectText()
		}
		c.wait(name)
		return "", false
	}
	re, err := regexp.Compile(prompt.Pattern)
	if err != nil {
		c.fail(err)
		return "", false
	}
	input := strings.TrimSpace(c.chat.UserInput())
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
	c.chat.SetConsumed(true)
	c.session().SessionData[name] = input
	c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": input})
	return input, true
}

// AskText accepts any non-empty reply and stores it under name.
func (c *Conv) AskText(name, body string, step StepNote) (string, bool) {
	if c.Stop {
		return "", false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return "", false
		}
		return asString(c.session().SessionData[name]), true
	}
	message := c.text(body)
	if c.chat.Consumed() || strings.TrimSpace(c.chat.UserInput()) == "" {
		if _, err := c.app.ExecChatMessage(c.chat, name, map[string]any{"message": message}); err != nil {
			c.fail(err)
			return "", false
		}
		if c.chat.Capturing() {
			c.chat.Preview().ExpectText()
		}
		c.wait(name)
		return "", false
	}
	input := strings.TrimSpace(c.chat.UserInput())
	if input == "" {
		return "", false
	}
	c.chat.SetConsumed(true)
	c.session().SessionData[name] = input
	c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": input})
	_ = step // notes reserved for future intent on this step
	return input, true
}

// AskLocation sends a WhatsApp location request and accepts a pin reply.
func (c *Conv) AskLocation(name string, prompt LocationPrompt) (LocationPin, bool) {
	if c.Stop {
		return LocationPin{}, false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return LocationPin{}, false
		}
		return locationPinFromSession(c.session().SessionData, name), true
	}
	body := c.text(prompt.Body)
	if c.chat.Consumed() || strings.TrimSpace(c.chat.UserInput()) == "" {
		if err := c.app.DeliverCodedLocationRequest(c.chat, name, body); err != nil {
			c.fail(err)
			return LocationPin{}, false
		}
		c.wait(name)
		return LocationPin{}, false
	}
	pin, ok := parseLocationInput(c.chat.UserInput())
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
	c.chat.SetConsumed(true)
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
	if c.Stop {
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
	if !c.chat.Consumed() && len(c.chat.FlowResponseData()) > 0 {
		if _, err := c.app.ExecChatWhatsAppFlow(c.chat, name, cfg); err != nil {
			c.fail(err)
			return false
		}
		c.appendCall(map[string]any{"name": name, "ok": true, "fields": c.chat.FlowResponseData()})
		return true
	}
	if c.chat.Consumed() || (c.chat.ButtonID() == "" && strings.TrimSpace(c.chat.UserInput()) == "" && len(c.chat.FlowResponseData()) == 0) {
		if _, err := c.app.ExecChatWhatsAppFlow(c.chat, name, cfg); err != nil {
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
	if c.Stop {
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
	if !ok || c.chat.LastTiqr() == nil {
		c.appendCall(map[string]any{"name": name, "ok": false})
		return nil, false
	}
	c.session().SessionData[name] = c.chat.LastTiqr()
	c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": c.chat.LastTiqr()})
	return c.chat.LastTiqr(), true
}

// StoreList calls a TiQR list operation once and returns its results.
func (c *Conv) StoreList(name, operation string, params map[string]string) ([]any, bool) {
	if c.Stop {
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
	if ok && c.chat.LastTiqr() != nil {
		items, ok = anySlice(c.chat.LastTiqr()["results"])
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
	if c.Stop {
		return nil, false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return nil, false
		}
		order, _ := asStringMap(c.session().SessionData[name])
		return order, true
	}
	if c.chat.Capturing() && c.chat.Preview().Mock {
		order, ok := c.chat.Preview().MockFor("lookup_order_status")
		if !ok {
			c.Stop = true
			return nil, false
		}
		c.session().SessionData[name] = order
		c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": order})
		return order, true
	}
	order, err := c.app.LookupLatestOrder(c.chat.Account(), c.session())
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
	if c.Stop {
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
// TiQR ecommerce sessions snapshot the cart and checkout fields onto a
// commerce draft before creating the agent transfer.
func (c *Conv) Transfer(message string) error {
	if c.Ended {
		return nil
	}
	if !c.chat.Capturing() {
		if handled, err := c.app.TryEcommerceTransfer(c.chat, message); handled {
			if err != nil {
				c.app.LogWarn("tiqr ecommerce handoff snapshot failed; falling back to queue transfer", "error", err)
			} else {
				c.Stop = true
				c.Ended = true
				return nil
			}
		}
	}
	_, err := c.app.ExecChatTransfer(c.chat, "transfer", map[string]any{"body": c.text(message)})
	finishCodedSession(c.session())
	c.Stop = true
	c.Ended = true
	return err
}

// End marks the coded flow finished.
func (c *Conv) End() error {
	if c.Ended {
		return nil
	}
	if c.chat.Capturing() && c.chat.Preview().NeedsMock != "" {
		c.Stop = true
		return nil
	}
	finishCodedSession(c.session())
	c.Stop = true
	c.Ended = true
	return nil
}

func (c *Conv) replayChoice() (Choice, bool, bool) {
	if c.Stop {
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
	if c.Stop {
		return Choice{}, false
	}
	if id := c.offeredButtonID(cfg); id != "" {
		c.chat.SetButtonID(id)
		return c.acceptButton(name, cfg)
	}
	// A button tap that is not one of this step's choices is not a request
	// for an agent. Show the choices again.
	if !c.noAnswerYet() && strings.TrimSpace(c.chat.ButtonID()) != "" {
		c.app.LogWarn("Coded flow button did not match this step",
			"step", name,
			"button_id", c.chat.ButtonID(),
			"text", c.chat.UserInput(),
		)
		c.chat.SetButtonID("")
		c.chat.SetUserInput("")
		c.chat.SetConsumed(false)
		c.chat.SetFlowResponseData(nil)
	}
	if c.noAnswerYet() {
		out, err := c.app.ExecChatButtons(c.chat, name, cfg)
		if err != nil {
			c.fail(err)
			return Choice{}, false
		}
		if out.Yield {
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

const AgentHandoff = "I'm connecting you with a team member who can help."

func (c *Conv) matchingButton(cfg map[string]any) bool {
	return c.offeredButtonID(cfg) != ""
}

// offeredButtonID returns the primary id of a button offered by this step
// when the inbound reply matches by id (exact after trim) or by title
// (case-insensitive). Empty means no match.
func (c *Conv) offeredButtonID(cfg map[string]any) string {
	if c.chat.Consumed() {
		return ""
	}
	buttons, err := c.app.ButtonsForNode(cfg, c.session().SessionData)
	if err != nil || len(buttons) == 0 {
		return ""
	}
	buttonID := strings.TrimSpace(c.chat.ButtonID())
	userInput := strings.TrimSpace(c.chat.UserInput())
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

func (c *Conv) NoAnswerYet() bool { return c.noAnswerYet() }

func (c *Conv) noAnswerYet() bool {
	return c.chat.Consumed() || (c.chat.ButtonID() == "" && strings.TrimSpace(c.chat.UserInput()) == "" && len(c.chat.FlowResponseData()) == 0)
}

func (c *Conv) acceptButton(name string, cfg map[string]any) (Choice, bool) {
	if _, err := c.app.ExecChatButtons(c.chat, name, cfg); err != nil {
		c.fail(err)
		return Choice{}, false
	}
	choice := Choice{ID: c.chat.ButtonID(), Title: c.chat.UserInput()}
	c.appendCall(map[string]any{
		"name":   name,
		"ok":     true,
		"id":     choice.ID,
		"title":  choice.Title,
		"fields": snapshotFields(c.session().SessionData, cfg),
	})
	return choice, true
}

func (c *Conv) Wait(name string) { c.wait(name) }

func (c *Conv) wait(name string) {
	c.session().CurrentStep = name
	c.Stop = true
}

func (c *Conv) waitingForPreviewMock() bool {
	if !c.chat.Capturing() || c.chat.Preview().NeedsMock == "" {
		return false
	}
	c.Stop = true
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
	out, err := c.app.ExecChatTiqrStoreAPI(c.chat, name, map[string]any{
		"api_type":  apiType,
		"operation": operation,
		"params":    raw,
	})
	if err != nil {
		return false, err
	}
	return out.Outcome == "http:2xx", nil
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

func (c *Conv) ImageButtonConfig(prompt ImageButtonPrompt) map[string]any { return c.imageButtonConfig(prompt) }

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

func (c *Conv) DoneCall() (map[string]any, bool) { return c.doneCall() }

func (c *Conv) doneCall() (map[string]any, bool) {
	if c.Stop {
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

// CallRecords returns saved call records for this session.
func (c *Conv) CallRecords() []map[string]any { return c.callRecords() }

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

// AppendCall records a finished call for replay.
func (c *Conv) AppendCall(rec map[string]any) { c.appendCall(rec) }

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

// CallOK reports whether a saved call succeeded.
func CallOK(rec map[string]any) bool { return callOK(rec) }

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
