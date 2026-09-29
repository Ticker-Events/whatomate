package handlers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	codedGuideKey = "_coded_guide"

	codedRouteChoice     = "choice"
	codedRouteCollection = "collection"
	codedRouteProduct    = "product"
	codedRouteHandoff    = "handoff"
	codedRouteUnclear    = "unclear"
)

// Route is one accepted free-text or exact answer from AskRoute.
type Route struct {
	Kind  string // choice, collection, product, handoff
	ID    string // choice_id or collection_id
	Query string // product_query
	Title string
}

// RouteOptions controls which catalog routes the intent role may return.
type RouteOptions struct {
	// AllowCatalog enables collection and product routes using loaded collections.
	AllowCatalog bool
}

// codedAIRoleConfig selects a provider and model for one AI role.
// Empty fields use the WhatsApp account AI. Provider "jev" is reserved.
type codedAIRoleConfig struct {
	Provider string
	Model    string
}

// codedIntentConfig is the intent router threshold and role settings.
type codedIntentConfig struct {
	IntentThreshold float64
	MaxGuideTurns   int
	Intent          codedAIRoleConfig
	Translate       codedAIRoleConfig
	Guide           codedAIRoleConfig
}

var codedIntentSettings = codedIntentConfig{
	IntentThreshold: 0.75,
	MaxGuideTurns:   3,
}

type codedIntentResult struct {
	Language     string  `json:"language"`
	Route        string  `json:"route"`
	ChoiceID     string  `json:"choice_id"`
	CollectionID string  `json:"collection_id"`
	ProductQuery string  `json:"product_query"`
	Confidence   float64 `json:"confidence"`
}

type codedIntentContext struct {
	AllowCatalog bool
	ChoiceIDs    map[string]string // id -> title
	Collections  map[string]string // id -> name
}

var (
	identifyCodedIntent = defaultIdentifyCodedIntent
	guideCodedIntent    = defaultGuideCodedIntent
)

func (c *Conv) askRouteButtons(name string, prompt ButtonPrompt, opts RouteOptions) (Route, bool) {
	if route, done, ok := c.replayRoute(); done {
		return route, ok
	}
	return c.askRoute(name, c.buttonConfig(prompt), opts)
}

func (c *Conv) askRouteList(name string, items []any, prompt ListPrompt, opts RouteOptions) (Route, bool) {
	if route, done, ok := c.replayRoute(); done {
		return route, ok
	}
	if prompt.ItemsKey != "" && items != nil {
		c.session().SessionData[prompt.ItemsKey] = items
	}
	return c.askRoute(name, c.listConfig(prompt), opts)
}

func (c *Conv) askRouteCarousel(name string, items []any, prompt CarouselPrompt, opts RouteOptions) (Route, bool) {
	if route, done, ok := c.replayRoute(); done {
		return route, ok
	}
	if prompt.ItemsKey != "" && items != nil {
		c.session().SessionData[prompt.ItemsKey] = items
	}
	return c.askRoute(name, c.carouselConfig(prompt), opts)
}

func (c *Conv) replayRoute() (Route, bool, bool) {
	if c.stop {
		return Route{}, true, false
	}
	rec, done := c.doneCall()
	if !done {
		return Route{}, false, false
	}
	if !callOK(rec) {
		return Route{}, true, false
	}
	kind := asString(rec["route"])
	if kind == "" {
		kind = codedRouteChoice
	}
	return Route{
		Kind:  kind,
		ID:    asString(rec["id"]),
		Query: asString(rec["query"]),
		Title: asString(rec["title"]),
	}, true, true
}

func (c *Conv) askRoute(name string, cfg map[string]any, opts RouteOptions) (Route, bool) {
	if c.stop {
		return Route{}, false
	}
	if id := c.offeredButtonID(cfg); id != "" {
		c.chat.buttonID = id
		choice, ok := c.acceptButton(name, cfg)
		if !ok {
			return Route{}, false
		}
		c.clearGuide()
		return Route{Kind: codedRouteChoice, ID: choice.ID, Title: choice.Title}, true
	}
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
			return Route{}, false
		}
		if out.yield {
			c.wait(name)
		}
		return Route{}, false
	}
	return c.resolveFreeText(name, cfg, opts)
}

func (c *Conv) resolveFreeText(name string, cfg map[string]any, opts RouteOptions) (Route, bool) {
	c.chat.consumed = true
	c.session().CurrentStep = name
	ctx := c.intentContext(cfg, opts)
	prompt := buildIntentPrompt(c.chat.userInput, ctx)
	raw, err := identifyCodedIntent(c.app, c.session(), c.chat.userInput, ctx)
	call := CodedPreviewAICall{
		Role:     "intent",
		Prompt:   prompt,
		Language: raw.Language,
		Route:    raw.Route,
	}
	if err != nil {
		call.Error = err.Error()
		c.notePreviewAI(call)
		c.app.logCodedFlowAI(c.session(), "intent", prompt, "", err.Error())
		_ = c.Transfer(codedAgentHandoff)
		return Route{}, false
	}
	call.Response = formatIntentResponse(raw)
	call.Parsed = map[string]any{
		"language":      raw.Language,
		"route":         raw.Route,
		"choice_id":     raw.ChoiceID,
		"collection_id": raw.CollectionID,
		"product_query": raw.ProductQuery,
		"confidence":    raw.Confidence,
	}
	call.Confidence = raw.Confidence
	result, grounded := validateCodedIntent(raw, ctx)
	call.Grounded = &grounded
	call.Route = result.Route
	call.Language = result.Language
	c.notePreviewAI(call)
	c.app.logCodedFlowAI(c.session(), "intent", prompt, call.Response, "",
		"route", result.Route,
		"confidence", result.Confidence,
		"grounded", grounded,
		"choice_id", result.ChoiceID,
		"collection_id", result.CollectionID,
		"product_query", result.ProductQuery,
		"language", result.Language,
	)
	c.rememberLanguage(result.Language)
	if !grounded {
		_ = c.Transfer(codedAgentHandoff)
		return Route{}, false
	}
	if result.Route == codedRouteHandoff && result.Confidence >= codedIntentSettings.IntentThreshold {
		c.clearGuide()
		_ = c.Transfer(codedAgentHandoff)
		return Route{}, false
	}
	if result.Confidence >= codedIntentSettings.IntentThreshold {
		switch result.Route {
		case codedRouteChoice:
			c.clearGuide()
			route := Route{Kind: codedRouteChoice, ID: result.ChoiceID, Title: ctx.ChoiceIDs[result.ChoiceID]}
			c.applyRouteSelection(cfg, route)
			c.appendRouteCall(name, route, cfg)
			return route, true
		case codedRouteCollection:
			c.clearGuide()
			route := Route{Kind: codedRouteCollection, ID: result.CollectionID, Title: ctx.Collections[result.CollectionID]}
			c.applyCollectionSelection(route)
			c.appendRouteCall(name, route, cfg)
			return route, true
		case codedRouteProduct:
			c.clearGuide()
			route := Route{Kind: codedRouteProduct, Query: result.ProductQuery}
			c.appendRouteCall(name, route, cfg)
			return route, true
		}
	}
	return c.askGuide(name, ctx)
}

func (c *Conv) askGuide(name string, ctx codedIntentContext) (Route, bool) {
	turns := c.guideTurns(name)
	if turns >= codedIntentSettings.MaxGuideTurns {
		c.clearGuide()
		_ = c.Transfer(codedAgentHandoff)
		return Route{}, false
	}
	lang := asString(c.session().SessionData[customerLanguageKey])
	if lang == "" {
		lang = "en"
	}
	prompt := buildGuidePrompt(c.chat.userInput, lang, ctx)
	question, err := guideCodedIntent(c.app, c.session(), c.chat.userInput, lang, ctx)
	call := CodedPreviewAICall{
		Role:     "guide",
		Prompt:   prompt,
		Language: lang,
	}
	if err != nil {
		call.Error = err.Error()
		c.notePreviewAI(call)
		c.app.logCodedFlowAI(c.session(), "guide", prompt, "", err.Error(), "language", lang)
		_ = c.Transfer(codedAgentHandoff)
		return Route{}, false
	}
	call.Response = question
	if strings.TrimSpace(question) == "" {
		call.Error = "empty guide question"
		c.notePreviewAI(call)
		c.app.logCodedFlowAI(c.session(), "guide", prompt, "", "empty guide question", "language", lang)
		_ = c.Transfer(codedAgentHandoff)
		return Route{}, false
	}
	c.notePreviewAI(call)
	c.app.logCodedFlowAI(c.session(), "guide", prompt, question, "", "language", lang)
	c.setGuide(name, turns+1)
	if err := c.app.deliverCodedText(c.chat, name, question); err != nil {
		c.fail(err)
		return Route{}, false
	}
	c.stop = true
	return Route{}, false
}

func (c *Conv) notePreviewAI(call CodedPreviewAICall) {
	if c == nil || c.chat == nil || !c.chat.capturing() {
		return
	}
	c.chat.preview.noteAI(call)
}

func formatIntentResponse(raw codedIntentResult) string {
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(b)
}

func (c *Conv) intentContext(cfg map[string]any, opts RouteOptions) codedIntentContext {
	ctx := codedIntentContext{
		AllowCatalog: opts.AllowCatalog,
		ChoiceIDs:    map[string]string{},
		Collections:  map[string]string{},
	}
	buttons, err := buttonsForNode(cfg, c.session().SessionData)
	if err == nil {
		for _, button := range buttons {
			id := fieldString(button, "id")
			if id == "" {
				id = fieldString(button, "id_2")
			}
			if id == "" {
				continue
			}
			ctx.ChoiceIDs[id] = fieldString(button, "title")
		}
	}
	if opts.AllowCatalog {
		items, _ := anySlice(c.session().SessionData["collections"])
		for _, item := range items {
			row, ok := asStringMap(item)
			if !ok {
				continue
			}
			id := fieldString(row, "id")
			if id == "" {
				continue
			}
			name := fieldString(row, "name")
			if name == "" {
				name = fieldString(row, "title")
			}
			ctx.Collections[id] = name
		}
	}
	return ctx
}

func (c *Conv) appendRouteCall(name string, route Route, cfg map[string]any) {
	rec := map[string]any{
		"name":  name,
		"ok":    true,
		"route": route.Kind,
		"id":    route.ID,
		"query": route.Query,
		"title": route.Title,
	}
	if route.Kind == codedRouteChoice {
		rec["fields"] = snapshotFields(c.session().SessionData, cfg)
	}
	c.appendCall(rec)
}

func (c *Conv) applyRouteSelection(cfg map[string]any, route Route) {
	if route.Kind != codedRouteChoice || route.ID == "" {
		return
	}
	c.session().SessionData = applyButtonSelection(cfg, c.session().SessionData, route.ID, route.Title)
}

func (c *Conv) applyCollectionSelection(route Route) {
	if route.ID == "" {
		return
	}
	c.session().SessionData["collection_id"] = route.ID
	name := route.Title
	if name == "" {
		name = route.ID
	}
	c.session().SessionData["collection_name"] = name
}

func (c *Conv) rememberLanguage(lang string) {
	if strings.TrimSpace(asString(c.session().SessionData[customerLanguageKey])) != "" {
		return
	}
	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = "en"
	}
	c.session().SessionData[customerLanguageKey] = lang
}

func (c *Conv) guideTurns(step string) int {
	raw, ok := asStringMap(c.session().SessionData[codedGuideKey])
	if !ok {
		return 0
	}
	if asString(raw["step"]) != step {
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

func (c *Conv) setGuide(step string, turns int) {
	c.session().SessionData[codedGuideKey] = map[string]any{
		"step":  step,
		"turns": turns,
	}
}

func (c *Conv) clearGuide() {
	delete(c.session().SessionData, codedGuideKey)
}

func validateCodedIntent(raw codedIntentResult, ctx codedIntentContext) (codedIntentResult, bool) {
	raw.Route = strings.ToLower(strings.TrimSpace(raw.Route))
	raw.ChoiceID = strings.TrimSpace(raw.ChoiceID)
	raw.CollectionID = strings.TrimSpace(raw.CollectionID)
	raw.ProductQuery = strings.TrimSpace(raw.ProductQuery)
	raw.Language = strings.TrimSpace(raw.Language)
	if raw.Language == "" {
		raw.Language = "en"
	}
	switch raw.Route {
	case codedRouteChoice:
		if raw.ChoiceID == "" || raw.CollectionID != "" || raw.ProductQuery != "" {
			return raw, false
		}
		if _, ok := ctx.ChoiceIDs[raw.ChoiceID]; !ok {
			return raw, false
		}
		return raw, true
	case codedRouteCollection:
		if !ctx.AllowCatalog {
			return raw, false
		}
		if raw.CollectionID == "" || raw.ChoiceID != "" || raw.ProductQuery != "" {
			return raw, false
		}
		if _, ok := ctx.Collections[raw.CollectionID]; !ok {
			return raw, false
		}
		return raw, true
	case codedRouteProduct:
		if !ctx.AllowCatalog {
			return raw, false
		}
		if raw.ProductQuery == "" || raw.ChoiceID != "" || raw.CollectionID != "" {
			return raw, false
		}
		return raw, true
	case codedRouteHandoff:
		if raw.ChoiceID != "" || raw.CollectionID != "" || raw.ProductQuery != "" {
			return raw, false
		}
		return raw, true
	case codedRouteUnclear, "":
		raw.Route = codedRouteUnclear
		if raw.ChoiceID != "" || raw.CollectionID != "" {
			return raw, false
		}
		return raw, true
	default:
		return raw, false
	}
}

func defaultIdentifyCodedIntent(a *App, session *models.ChatbotSession, message string, ctx codedIntentContext) (codedIntentResult, error) {
	settings, ok := codedRoleSettings(a, session, codedIntentSettings.Intent)
	if !ok {
		return codedIntentResult{}, fmt.Errorf("ai is not configured")
	}
	prompt := buildIntentPrompt(message, ctx)
	answer, err := a.completeCodedText(settings, session, prompt, "")
	if err != nil {
		return codedIntentResult{}, err
	}
	return parseCodedIntent(answer)
}

func defaultGuideCodedIntent(a *App, session *models.ChatbotSession, message, lang string, ctx codedIntentContext) (string, error) {
	settings, ok := codedRoleSettings(a, session, codedIntentSettings.Guide)
	if !ok {
		return "", fmt.Errorf("ai is not configured")
	}
	prompt := buildGuidePrompt(message, lang, ctx)
	return a.completeCodedText(settings, session, prompt, "")
}

func codedRoleSettings(a *App, session *models.ChatbotSession, role codedAIRoleConfig) (*models.ChatbotSettings, bool) {
	if strings.EqualFold(strings.TrimSpace(role.Provider), "jev") {
		return nil, false
	}
	settings, ok := codedAISettings(a, session)
	if !ok {
		return nil, false
	}
	if settings == nil {
		return nil, false
	}
	out := *settings
	if p := strings.TrimSpace(role.Provider); p != "" {
		out.AI.Provider = models.AIProvider(p)
	}
	if m := strings.TrimSpace(role.Model); m != "" {
		out.AI.Model = m
	}
	return &out, true
}

func buildIntentPrompt(message string, ctx codedIntentContext) string {
	var choices strings.Builder
	if len(ctx.ChoiceIDs) == 0 {
		choices.WriteString("(none)")
	} else {
		for id, title := range ctx.ChoiceIDs {
			fmt.Fprintf(&choices, "\n- %s (%s)", id, title)
		}
	}
	var collections strings.Builder
	if !ctx.AllowCatalog || len(ctx.Collections) == 0 {
		collections.WriteString("(none)")
	} else {
		for id, name := range ctx.Collections {
			fmt.Fprintf(&collections, "\n- %s (id %s)", name, id)
		}
	}
	return fmt.Sprintf(`You map one customer message onto one route. You do not reply to the customer. You do not invent ids, products, collections, prices, or stock.

Allowed choices, use only these ids:
%s

Collections already loaded, use only these ids. If this list is empty, do not use the collection route:
%s

route choice: they picked one of the choices above, or they want to buy or check an order. Set choice_id. Do not use this for a request to talk to a person unless talk_to_agent is in the allowed choices and that is what they picked.
route collection: they want the items in one collection, or they ask what is available in that collection. Set collection_id from the list above. Do not use this for a single product name.
route product: they name a product or ask to buy a specific item. Set product_query to a short search string taken from their words. Leave both ids empty.
route handoff: they ask for a human, agent, or staff, they seem confused or stuck, or you would have to invent a product, collection, price, or id to help them. Leave both ids empty.
route unclear: more than one shopping route fits, or none of them fit, and they are not asking for a person and do not seem stuck.

confidence is from 0 to 1. Use 0.9 or higher for an obvious route, including a clear request for a person and a clearly confused customer. Use a value below 0.75 when you are unsure. If you are unsure whether a catalog id exists, use handoff instead of guessing an id.
language is a short label for the language the customer is writing in. English is en. Any other language, including mixed forms such as Manglish, gets its own label. Do not translate the message.

Reply with JSON only:
{"language":"en","route":"unclear","choice_id":"","collection_id":"","product_query":"","confidence":0}

Customer message:
%s`, choices.String(), collections.String(), message)
}

func buildGuidePrompt(message, lang string, ctx codedIntentContext) string {
	names := make([]string, 0, len(ctx.Collections))
	for _, name := range ctx.Collections {
		if name != "" {
			names = append(names, name)
		}
	}
	collectionNames := "(none)"
	if len(names) > 0 {
		collectionNames = strings.Join(names, ", ")
	}
	return fmt.Sprintf(`The customer has not chosen a path yet. Ask one short question so their next reply can be matched to a path.
You do not invent products, collections, prices, or ids. You may mention only these collection names: %s.
Paths: Buy products, Check order status, Talk to staff.
If they named an item, ask whether they want to buy that item, see a collection, check an order, or talk to staff. Do not say the item is available.
Write the question in %s, matching how the customer is writing. Return only the question.

Customer message:
%s`, collectionNames, lang, message)
}

func parseCodedIntent(raw string) (codedIntentResult, error) {
	raw = strings.TrimSpace(raw)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var body codedIntentResult
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return codedIntentResult{}, err
	}
	return body, nil
}
