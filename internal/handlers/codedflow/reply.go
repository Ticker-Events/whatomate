package codedflow

import (
	"fmt"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	// RouteSelectProduct means the customer picked a previously shown product
	// (for example by quoting an older carousel card).
	RouteSelectProduct = "select_product"

	// RouteCurrent means the quoted reply is still answering the current step.
	RouteCurrent = "current"

	shownProductsKey   = "shown_products"
	quotedInboundKey   = "_quoted_inbound"
	codedReplyGuideKey = "_coded_reply_guide"

	replyUnclearLead = "I could not tell what you meant by that reply to the earlier message."
)

// QuotedMessage is the chat message the customer quoted on this turn.
type QuotedMessage struct {
	ID              string
	WhatsAppID      string
	Content         string
	MessageType     string
	Direction       string
	InteractiveData map[string]any
	IsLatestOutgoing bool
}

// QuotedChoice is one button or row id stored on a quoted interactive message.
type QuotedChoice struct {
	ID    string
	Title string
	Body  string // card body when present (product carousels)
}

// StashQuotedInbound keeps the quoted message for the coded-flow turn.
func StashQuotedInbound(session *models.ChatbotSession, quoted *QuotedMessage) {
	if session == nil || quoted == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	session.SessionData[quotedInboundKey] = map[string]any{
		"id":                 quoted.ID,
		"whatsapp_id":        quoted.WhatsAppID,
		"content":            quoted.Content,
		"message_type":       quoted.MessageType,
		"direction":          quoted.Direction,
		"interactive_data":   quoted.InteractiveData,
		"is_latest_outgoing": quoted.IsLatestOutgoing,
	}
}

// TakeQuotedInbound removes and returns the stashed quoted message.
func TakeQuotedInbound(session *models.ChatbotSession) *QuotedMessage {
	if session == nil || session.SessionData == nil {
		return nil
	}
	raw, ok := asStringMap(session.SessionData[quotedInboundKey])
	delete(session.SessionData, quotedInboundKey)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := &QuotedMessage{
		ID:          asString(raw["id"]),
		WhatsAppID:  asString(raw["whatsapp_id"]),
		Content:     asString(raw["content"]),
		MessageType: asString(raw["message_type"]),
		Direction:   asString(raw["direction"]),
	}
	if v, ok := raw["is_latest_outgoing"].(bool); ok {
		out.IsLatestOutgoing = v
	}
	if data, ok := asStringMap(raw["interactive_data"]); ok {
		out.InteractiveData = data
	}
	return out
}

// MergeShownProducts keeps every product the customer has been shown this session.
func MergeShownProducts(data models.JSONB, items []any) {
	if data == nil || len(items) == 0 {
		return
	}
	existing, _ := anySlice(data[shownProductsKey])
	byID := map[string]map[string]any{}
	order := make([]string, 0, len(existing)+len(items))
	for _, entry := range existing {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		id := fieldString(item, "id")
		if id == "" {
			continue
		}
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = item
	}
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		id := fieldString(item, "id")
		if id == "" {
			continue
		}
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = item
	}
	merged := make([]any, 0, len(order))
	for _, id := range order {
		merged = append(merged, byID[id])
	}
	data[shownProductsKey] = merged
}

// ShownProductByID returns a previously shown product from session history.
func ShownProductByID(data models.JSONB, id string) map[string]any {
	id = strings.TrimSpace(id)
	if data == nil || id == "" {
		return nil
	}
	for _, key := range []string{shownProductsKey, "products"} {
		items, ok := anySlice(data[key])
		if !ok {
			continue
		}
		for _, entry := range items {
			item, ok := asStringMap(entry)
			if !ok {
				continue
			}
			if fieldString(item, "id") == id {
				return item
			}
		}
	}
	return nil
}

// QuotedChoices extracts selectable ids from a quoted interactive message.
func QuotedChoices(quoted *QuotedMessage) []QuotedChoice {
	if quoted == nil || quoted.InteractiveData == nil {
		return nil
	}
	data := quoted.InteractiveData
	out := make([]QuotedChoice, 0)
	switch strings.ToLower(asString(data["type"])) {
	case "carousel":
		cards, _ := anySlice(data["cards"])
		for _, entry := range cards {
			card, ok := asStringMap(entry)
			if !ok {
				continue
			}
			body := fieldString(card, "body")
			buttons, _ := anySlice(card["buttons"])
			for _, b := range buttons {
				btn, ok := asStringMap(b)
				if !ok {
					continue
				}
				id := fieldString(btn, "id")
				if id == "" {
					continue
				}
				out = append(out, QuotedChoice{
					ID:    id,
					Title: fieldString(btn, "title"),
					Body:  body,
				})
			}
		}
	case "list":
		rows, _ := anySlice(data["rows"])
		for _, entry := range rows {
			row, ok := asStringMap(entry)
			if !ok {
				continue
			}
			id := fieldString(row, "id")
			if id == "" {
				continue
			}
			out = append(out, QuotedChoice{
				ID:    id,
				Title: fieldString(row, "title"),
				Body:  fieldString(row, "description"),
			})
		}
	case "button", "":
		buttons, _ := anySlice(data["buttons"])
		for _, entry := range buttons {
			btn, ok := asStringMap(entry)
			if !ok {
				continue
			}
			id := fieldString(btn, "id")
			if id == "" {
				continue
			}
			out = append(out, QuotedChoice{
				ID:    id,
				Title: fieldString(btn, "title"),
			})
		}
	}
	return out
}

// HasReplyRoute reports whether a quoted-reply action is pending.
func (c *Conv) HasReplyRoute() bool {
	return c != nil && strings.TrimSpace(c.ReplyRoute.Kind) != ""
}

// TakeReplyRoute returns and clears the pending quoted-reply route.
func (c *Conv) TakeReplyRoute() Route {
	if c == nil {
		return Route{}
	}
	route := c.ReplyRoute
	c.ReplyRoute = Route{}
	return route
}

// SetReplyRoute stores a pending quoted-reply action for the ecommerce caller.
func (c *Conv) SetReplyRoute(route Route) {
	if c == nil {
		return
	}
	c.ReplyRoute = route
}

// prepareQuotedProductSelection restores a quoted product into the current
// product ask so the normal button matcher can accept it.
func (c *Conv) prepareQuotedProductSelection(cfg map[string]any) bool {
	if c == nil || c.Stop || c.chat == nil {
		return false
	}
	quoted := c.chat.QuotedMessage()
	if quoted == nil || quoted.IsLatestOutgoing || c.chat.Consumed() {
		return false
	}
	itemsKey := strings.TrimSpace(asString(cfg["items_var"]))
	if itemsKey == "" {
		itemsKey = "products"
	}
	if itemsKey != "products" && asString(cfg["mode"]) != "carousel" && asString(cfg["mode"]) != "reply" {
		return false
	}
	buttonID := strings.TrimSpace(c.chat.ButtonID())
	if buttonID == "" {
		return false
	}
	choices := QuotedChoices(quoted)
	found := false
	for _, ch := range choices {
		if ch.ID == buttonID {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	route := c.routeFromQuotedChoice(QuotedChoice{ID: buttonID}, quoted, RouteOptions{})
	if route.Kind != RouteSelectProduct {
		return false
	}
	if !c.restoreQuotedProduct(route.ID, route.Title) {
		return false
	}
	c.clearReplyGuide()
	return true
}

// restoreQuotedProduct copies a previously shown product into the active
// session fields used by product selection.
func (c *Conv) restoreQuotedProduct(productID, title string) bool {
	productID = strings.TrimSpace(productID)
	if productID == "" || c.session() == nil {
		return false
	}
	product := ShownProductByID(c.session().SessionData, productID)
	if product == nil {
		return false
	}
	name := fieldString(product, "name")
	if name == "" {
		name = strings.TrimSpace(title)
	}
	c.session().SessionData["product_id"] = productID
	c.session().SessionData["product_name"] = name
	c.session().SessionData["product_selected"] = name
	if opts, ok := anySlice(product["options"]); ok {
		c.session().SessionData["options"] = opts
	} else {
		c.session().SessionData["options"] = []any{}
	}
	c.session().SessionData["products"] = []any{product}
	return true
}

// tryQuotedReply handles an inbound that quotes an earlier message.
// When it returns true, the current ask should yield without matching this step.
func (c *Conv) tryQuotedReply(step string, cfg map[string]any, opts RouteOptions) bool {
	if c == nil || c.Stop || c.chat == nil {
		return false
	}
	quoted := c.chat.QuotedMessage()
	if quoted == nil || quoted.IsLatestOutgoing {
		return false
	}
	if c.chat.Consumed() {
		return false
	}
	buttonID := strings.TrimSpace(c.chat.ButtonID())
	choices := QuotedChoices(quoted)
	choiceByID := map[string]QuotedChoice{}
	for _, ch := range choices {
		choiceByID[ch.ID] = ch
	}

	if buttonID != "" {
		if ch, ok := choiceByID[buttonID]; ok {
			route := c.routeFromQuotedChoice(ch, quoted, opts)
			if route.Kind == "" || route.Kind == RouteCurrent {
				return false
			}
			// Product taps on a product ask are handled by prepareQuotedProductSelection.
			if route.Kind == RouteSelectProduct && c.prepareQuotedProductSelection(cfg) {
				return false
			}
			return c.acceptQuotedRoute(step, route)
		}
		// Button id is not on the quoted message; fall through only when there
		// is also free text. Otherwise leave normal mismatch handling alone.
		if strings.TrimSpace(c.chat.UserInput()) == "" {
			return false
		}
	}

	text := strings.TrimSpace(c.chat.UserInput())
	if text == "" {
		return false
	}
	route, ok := c.resolveQuotedFreeText(step, cfg, opts, quoted, choices)
	if !ok {
		return true // handoff or guide already sent
	}
	if route.Kind == "" || route.Kind == RouteCurrent {
		return false
	}
	if route.Kind == RouteSelectProduct && c.restoreQuotedProduct(route.ID, route.Title) {
		if itemsKey := strings.TrimSpace(asString(cfg["items_var"])); itemsKey == "products" || asString(cfg["mode"]) == "carousel" || asString(cfg["mode"]) == "reply" {
			c.chat.SetButtonID(route.ID)
			c.clearReplyGuide()
			return false
		}
	}
	return c.acceptQuotedRoute(step, route)
}

func (c *Conv) acceptQuotedRoute(step string, route Route) bool {
	c.clearReplyGuide()
	c.chat.SetConsumed(true)
	// Record a skip so this ask is not re-opened on replay. Do not store
	// selection fields here; the reply route handler applies them.
	c.appendCall(map[string]any{
		"name":         step,
		"ok":           true,
		"quoted_skip":  true,
		"route":        route.Kind,
		"id":           route.ID,
		"query":        route.Query,
		"title":        route.Title,
	})
	switch route.Kind {
	case RouteCheckout:
		c.Divert = codedRouteCheckout
		c.ReplyRoute = Route{}
		return true
	case codedRouteHandoff:
		_ = c.Transfer(AgentHandoff)
		return true
	default:
		c.ReplyRoute = route
		return true
	}
}

func (c *Conv) routeFromQuotedChoice(ch QuotedChoice, quoted *QuotedMessage, opts RouteOptions) Route {
	id := strings.TrimSpace(ch.ID)
	title := strings.TrimSpace(ch.Title)
	if title == "" {
		title = strings.TrimSpace(ch.Body)
	}
	switch id {
	case "buy_products", "check_order_status", "talk_to_agent",
		"checkout", "confirm_items", "edit_cart", "add_more",
		"proceed", "pickup", "delivery", "share_again":
		if id == "checkout" {
			return Route{Kind: RouteCheckout}
		}
		if id == "talk_to_agent" {
			return Route{Kind: RouteHandoff}
		}
		return Route{Kind: RouteChoice, ID: id, Title: title}
	}
	typ := ""
	if quoted != nil && quoted.InteractiveData != nil {
		typ = strings.ToLower(asString(quoted.InteractiveData["type"]))
	}
	if product := ShownProductByID(c.session().SessionData, id); product != nil {
		name := fieldString(product, "name")
		if name == "" {
			name = title
		}
		return Route{Kind: RouteSelectProduct, ID: id, Title: name}
	}
	if opts.AllowCatalog || typ == "list" {
		if name := collectionNameByID(c.session().SessionData, id); name != "" {
			return Route{Kind: RouteCollection, ID: id, Title: name}
		}
	}
	switch typ {
	case "carousel":
		return Route{Kind: RouteSelectProduct, ID: id, Title: title}
	case "list":
		return Route{Kind: RouteCollection, ID: id, Title: title}
	default:
		return Route{Kind: RouteChoice, ID: id, Title: title}
	}
}

func collectionNameByID(data models.JSONB, id string) string {
	id = strings.TrimSpace(id)
	if data == nil || id == "" {
		return ""
	}
	items, ok := anySlice(data["collections"])
	if !ok {
		return ""
	}
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if fieldString(item, "id") != id {
			continue
		}
		name := fieldString(item, "name")
		if name == "" {
			name = fieldString(item, "title")
		}
		return name
	}
	return ""
}

func (c *Conv) resolveQuotedFreeText(step string, cfg map[string]any, opts RouteOptions, quoted *QuotedMessage, choices []QuotedChoice) (Route, bool) {
	// Quoted replies keep loaded session collections; FAQs stay off unless the step enables them.
	ctx := c.intentContext(cfg, RouteOptions{AllowCatalog: opts.AllowCatalog})
	ctx.FAQs = map[string]string{}
	ctx.AllowFAQ = false
	ctx.QuotedContent = strings.TrimSpace(quoted.Content)
	if ctx.QuotedContent == "" && quoted.InteractiveData != nil {
		ctx.QuotedContent = strings.TrimSpace(asString(quoted.InteractiveData["body"]))
	}
	ctx.QuotedKind = "text"
	if quoted.InteractiveData != nil {
		if t := strings.TrimSpace(asString(quoted.InteractiveData["type"])); t != "" {
			ctx.QuotedKind = t
		}
	}
	ctx.QuotedChoices = map[string]string{}
	for _, ch := range choices {
		label := ch.Title
		if ch.Body != "" {
			if label != "" {
				label = ch.Body + " — " + label
			} else {
				label = ch.Body
			}
		}
		ctx.QuotedChoices[ch.ID] = label
		// Quoted ids are also valid choice targets for this turn.
		if _, exists := ctx.ChoiceIDs[ch.ID]; !exists {
			ctx.ChoiceIDs[ch.ID] = label
		}
	}
	for id := range ctx.QuotedChoices {
		if product := ShownProductByID(c.session().SessionData, id); product != nil {
			if n := fieldString(product, "name"); n != "" {
				ctx.QuotedChoices[id] = n
			}
		}
	}

	prompt := BuildQuotedReplyIntentPrompt(c.chat.UserInput(), ctx)
	raw, err := IdentifyCodedIntent(c.app, c.session(), c.chat.UserInput(), ctx)
	raw.Reasoning = LimitWords(raw.Reasoning, 200)
	call := CodedPreviewAICall{
		Role:      "intent",
		Prompt:    prompt,
		Language:  raw.Language,
		Route:     raw.Route,
		Reasoning: raw.Reasoning,
	}
	if err != nil {
		call.Error = err.Error()
		c.notePreviewAI(call)
		c.app.LogCodedFlowAI(c.session(), "intent", prompt, "", err.Error())
		_ = c.Transfer(AgentHandoff)
		return Route{}, false
	}
	call.Response = formatIntentResponse(raw)
	result, grounded := ValidateQuotedReplyIntent(raw, ctx)
	call.Grounded = &grounded
	call.Route = result.Route
	call.Language = result.Language
	call.Parsed = map[string]any{
		"language":      result.Language,
		"route":         result.Route,
		"choice_id":     result.ChoiceID,
		"collection_id": result.CollectionID,
		"product_query": result.ProductQuery,
		"answer":        result.Answer,
		"faq_id":        result.FaqID,
		"confidence":    result.Confidence,
		"reasoning":     raw.Reasoning,
	}
	call.Confidence = result.Confidence
	c.notePreviewAI(call)
	c.app.LogCodedFlowAI(c.session(), "intent", prompt, call.Response, "",
		"route", result.Route,
		"confidence", result.Confidence,
		"grounded", grounded,
		"faq_id", result.FaqID,
		"quoted", true,
	)
	c.rememberLanguage(result.Language)
	if !grounded {
		return c.askQuotedReplyGuide(step, ctx)
	}
	if result.Confidence < IntentSettings.IntentThreshold {
		return c.askQuotedReplyGuide(step, ctx)
	}
	switch result.Route {
	case RouteCurrent, codedRouteAnswer:
		return Route{Kind: RouteCurrent}, true
	case codedRouteChoice:
		ch := QuotedChoice{ID: result.ChoiceID, Title: ctx.ChoiceIDs[result.ChoiceID]}
		if qc, ok := ctx.QuotedChoices[result.ChoiceID]; ok {
			ch.Title = qc
		}
		return c.routeFromQuotedChoice(ch, quoted, opts), true
	case RouteSelectProduct:
		title := ctx.QuotedChoices[result.ChoiceID]
		if title == "" {
			title = result.ChoiceID
		}
		return Route{Kind: RouteSelectProduct, ID: result.ChoiceID, Title: title}, true
	case codedRouteCollection:
		return Route{Kind: RouteCollection, ID: result.CollectionID, Title: ctx.Collections[result.CollectionID]}, true
	case codedRouteProduct:
		return Route{Kind: RouteProduct, Query: result.ProductQuery}, true
	case codedRouteCheckout:
		return Route{Kind: RouteCheckout}, true
	case codedRouteHandoff:
		return Route{Kind: RouteHandoff}, true
	case codedRouteFAQ:
		return c.deliverFAQAnswer(step, result.FaqID)
	default:
		return c.askQuotedReplyGuide(step, ctx)
	}
}

func (c *Conv) askQuotedReplyGuide(step string, ctx IntentContext) (Route, bool) {
	turns := c.replyGuideTurns()
	if turns >= 1 {
		c.clearReplyGuide()
		_ = c.Transfer(AgentHandoff)
		return Route{}, false
	}
	lang := asString(c.session().SessionData[customerLanguageKey])
	if lang == "" {
		lang = "en"
	}
	prompt := buildQuotedReplyGuidePrompt(c.chat.UserInput(), lang, ctx)
	question, err := GuideCodedIntent(c.app, c.session(), c.chat.UserInput(), lang, ctx)
	call := CodedPreviewAICall{Role: "guide", Prompt: prompt, Language: lang}
	if err != nil || strings.TrimSpace(question) == "" {
		if err != nil {
			call.Error = err.Error()
		} else {
			call.Error = "empty guide question"
		}
		c.notePreviewAI(call)
		c.app.LogCodedFlowAI(c.session(), "guide", prompt, question, call.Error, "quoted", true)
		_ = c.Transfer(AgentHandoff)
		return Route{}, false
	}
	call.Response = question
	c.notePreviewAI(call)
	c.app.LogCodedFlowAI(c.session(), "guide", prompt, question, "", "quoted", true, "language", lang)
	c.setReplyGuide(turns + 1)
	c.chat.SetConsumed(true)
	message := c.text(replyUnclearLead) + "\n\n" + strings.TrimSpace(question)
	if err := c.app.DeliverCodedText(c.chat, step, message); err != nil {
		c.fail(err)
		return Route{}, false
	}
	c.Stop = true
	return Route{}, false
}

func (c *Conv) replyGuideTurns() int {
	raw, ok := asStringMap(c.session().SessionData[codedReplyGuideKey])
	if !ok {
		return 0
	}
	switch n := raw["turns"].(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func (c *Conv) setReplyGuide(turns int) {
	c.session().SessionData[codedReplyGuideKey] = map[string]any{"turns": turns}
}

func (c *Conv) clearReplyGuide() {
	if c.session() == nil || c.session().SessionData == nil {
		return
	}
	delete(c.session().SessionData, codedReplyGuideKey)
}

// BuildQuotedReplyIntentPrompt asks the model to interpret a quote-reply.
func BuildQuotedReplyIntentPrompt(message string, ctx IntentContext) string {
	var choices strings.Builder
	if len(ctx.QuotedChoices) == 0 {
		choices.WriteString("(none)")
	} else {
		for _, id := range sortedStringMapKeys(ctx.QuotedChoices) {
			fmt.Fprintf(&choices, "\n- %s (%s)", id, ctx.QuotedChoices[id])
		}
	}
	var collections strings.Builder
	if len(ctx.Collections) == 0 {
		collections.WriteString("(none)")
	} else {
		for _, id := range sortedStringMapKeys(ctx.Collections) {
			fmt.Fprintf(&collections, "\n- %s (id %s)", ctx.Collections[id], id)
		}
	}
	var faqs strings.Builder
	faqRoute := ""
	if len(ctx.FAQs) == 0 {
		faqs.WriteString("(none)")
	} else {
		for _, id := range sortedStringMapKeys(ctx.FAQs) {
			fmt.Fprintf(&faqs, "\n- %s (id %s)", ctx.FAQs[id], id)
		}
		faqRoute = "route faq: they ask a question that one of the loaded FAQs already answers, including a paraphrase. Set faq_id. Leave other ids empty.\n"
	}
	quoted := strings.TrimSpace(ctx.QuotedContent)
	if quoted == "" {
		quoted = "(empty)"
	}
	current := strings.TrimSpace(ctx.Question)
	if current == "" {
		current = "(not available)"
	}
	return fmt.Sprintf(`You map one customer message that quotes an earlier chat message. You do not reply to the customer. You do not invent ids, products, collections, prices, or stock. You do not write FAQ answers; code sends the stored answer for a faq_id.

The customer quoted this earlier message (%s):
%s

Selectable ids from that quoted message (use only these ids for choice or select_product):
%s

Collections already loaded (use only these ids for collection):
%s

Store FAQs already loaded (use only these ids for faq):
%s

Current question on screen (not the quoted message):
%s

What the current step is doing:
%s

What a valid reply to the current step looks like:
%s

When the customer says "buy …", "I want …", "I need …", "show me …", or a close variant, they are almost always naming a collection or a product. If the name matches a loaded collection, use route collection. If it does not, use route product with product_query from their words.

route current: their words answer the current question on screen, not the quoted message. Leave ids empty.
route choice: they picked one of the quoted selectable ids that is a menu or action button (for example buy_products). Set choice_id.
route select_product: they want the product from the quoted message. Set choice_id to that product id from the quoted list.
route collection: they want the collection from the quoted message or name a loaded collection. Set collection_id.
route product: they name a product to search for. Set product_query from their words.
route checkout: they want to check out with the cart they already have.
%sroute handoff: they ask for a person, or you would have to invent an id.
route unclear: you cannot tell whether they mean the quoted message or the current question.

confidence is from 0 to 1. Use 0.9 or higher when the quoted message clearly supplies the target. Use below 0.75 when unsure.
%s
reasoning explains the choice for an operator. At most 200 words.

Reply with JSON only:
{"language":"en","route":"unclear","choice_id":"","collection_id":"","product_query":"","answer":"","faq_id":"","confidence":0,"reasoning":""}

Customer message:
%s`, ctx.QuotedKind, quoted, choices.String(), collections.String(), faqs.String(), current,
		strings.TrimSpace(ctx.Doing), strings.TrimSpace(ctx.Expect), faqRoute,
		languagePromptInstruction(ctx.SupportedLanguages), message)
}

func buildQuotedReplyGuidePrompt(message, lang string, ctx IntentContext) string {
	quoted := strings.TrimSpace(ctx.QuotedContent)
	if quoted == "" {
		quoted = "(empty)"
	}
	names := make([]string, 0, len(ctx.QuotedChoices))
	for _, title := range ctx.QuotedChoices {
		if title != "" {
			names = append(names, title)
		}
	}
	hint := "(none)"
	if len(names) > 0 {
		hint = strings.Join(names, ", ")
	}
	return fmt.Sprintf(`The customer replied to an earlier message but the intent is unclear. Ask one short question so their next reply can be matched.
You do not invent products, collections, prices, or ids. You may mention only these labels from the quoted message: %s.

Quoted message:
%s

Write the question in %s, matching how the customer is writing. Return only the question.

Customer message:
%s`, hint, quoted, lang, message)
}

// ValidateQuotedReplyIntent grounds a quote-reply intent result.
func ValidateQuotedReplyIntent(raw IntentResult, ctx IntentContext) (IntentResult, bool) {
	raw.Route = strings.ToLower(strings.TrimSpace(raw.Route))
	raw.ChoiceID = strings.TrimSpace(raw.ChoiceID)
	raw.CollectionID = strings.TrimSpace(raw.CollectionID)
	raw.ProductQuery = strings.TrimSpace(raw.ProductQuery)
	raw.Answer = strings.TrimSpace(raw.Answer)
	raw.FaqID = strings.TrimSpace(raw.FaqID)
	raw.Language = normalizeDetectedLanguage(raw.Language, ctx.SupportedLanguages)
	raw.Reasoning = LimitWords(raw.Reasoning, 200)
	switch raw.Route {
	case RouteCurrent:
		if raw.ChoiceID != "" || raw.CollectionID != "" || raw.ProductQuery != "" || raw.FaqID != "" {
			return raw, false
		}
		return raw, true
	case RouteSelectProduct:
		if raw.ChoiceID == "" || raw.CollectionID != "" || raw.ProductQuery != "" || raw.Answer != "" || raw.FaqID != "" {
			return raw, false
		}
		if _, ok := ctx.QuotedChoices[raw.ChoiceID]; !ok {
			return raw, false
		}
		return raw, true
	case codedRouteChoice:
		if raw.ChoiceID == "" || raw.CollectionID != "" || raw.ProductQuery != "" || raw.Answer != "" || raw.FaqID != "" {
			return raw, false
		}
		if _, ok := ctx.QuotedChoices[raw.ChoiceID]; ok {
			return raw, true
		}
		if _, ok := ctx.ChoiceIDs[raw.ChoiceID]; ok {
			return raw, true
		}
		return raw, false
	case codedRouteCollection:
		if raw.CollectionID == "" || raw.ChoiceID != "" || raw.ProductQuery != "" || raw.Answer != "" || raw.FaqID != "" {
			return raw, false
		}
		if _, ok := ctx.Collections[raw.CollectionID]; !ok {
			return raw, false
		}
		return raw, true
	case codedRouteProduct:
		if raw.ProductQuery == "" || raw.ChoiceID != "" || raw.CollectionID != "" || raw.Answer != "" || raw.FaqID != "" {
			return raw, false
		}
		return raw, true
	case codedRouteCheckout, codedRouteHandoff:
		if raw.ChoiceID != "" || raw.CollectionID != "" || raw.ProductQuery != "" || raw.Answer != "" || raw.FaqID != "" {
			return raw, false
		}
		return raw, true
	case codedRouteFAQ:
		if len(ctx.FAQs) == 0 {
			return raw, false
		}
		if raw.FaqID == "" || raw.ChoiceID != "" || raw.CollectionID != "" || raw.ProductQuery != "" || raw.Answer != "" {
			return raw, false
		}
		if _, ok := ctx.FAQs[raw.FaqID]; !ok {
			return raw, false
		}
		return raw, true
	case codedRouteUnclear, "":
		raw.Route = codedRouteUnclear
		return raw, true
	case codedRouteAnswer:
		// Treat as answering the current step.
		raw.Route = RouteCurrent
		return raw, true
	default:
		return raw, false
	}
}
