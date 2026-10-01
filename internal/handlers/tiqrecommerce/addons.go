package tiqrecommerce

import (
	"encoding/json"
	"fmt"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"regexp"
	"strconv"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	codedAddonIntentSelect  = "select"
	codedAddonIntentSkip    = "skip"
	codedAddonIntentUnclear = "unclear"

	codedAddonPromptHint = "Reply with the item number and how many you want, for example item 1 - 2.\nYou can list more than one item. Say Skip if you don't want any."
)

var (
	parseCodedAddons = defaultParseCodedAddons
	addonIDMentionRE = regexp.MustCompile(`(?i)\b(addon|product|option)[_\s-]?id\b|\bid\s*[:=]\s*\d+`)
)

// AddonParseFunc is the AI parse hook used by catalog add-on prompts.
type AddonParseFunc func(host codedflow.Host, session *models.ChatbotSession, prompt string) (AddonParseResult, error)

// AddonParseResult is the grounded AI parse payload for catalog add-ons.
type AddonParseResult = codedAddonParseResult

// AddonParseItem is one numbered add-on selection from the AI parse.
type AddonParseItem = codedAddonParseItem

const (
	// AddonIntentSelect means the shopper chose one or more add-ons.
	AddonIntentSelect = codedAddonIntentSelect
	// AddonIntentSkip means the shopper declined add-ons.
	AddonIntentSkip = codedAddonIntentSkip
	// AddonIntentUnclear means the reply could not be grounded.
	AddonIntentUnclear = codedAddonIntentUnclear
)

// SetParseCodedAddonsForTest replaces the AI parse hook. Tests must restore via the returned function.
func SetParseCodedAddonsForTest(fn AddonParseFunc) (restore func()) {
	prev := parseCodedAddons
	if fn == nil {
		parseCodedAddons = defaultParseCodedAddons
	} else {
		parseCodedAddons = func(host codedflow.Host, session *models.ChatbotSession, prompt string) (codedAddonParseResult, error) {
			return fn(host, session, prompt)
		}
	}
	return func() { parseCodedAddons = prev }
}

type codedAddonParseItem struct {
	Index    int  `json:"index"`
	Quantity *int `json:"quantity"`
}

type codedAddonParseResult struct {
	Intent          string                `json:"intent"`
	Confidence      float64               `json:"confidence"`
	Items           []codedAddonParseItem `json:"items"`
	MissingQuantity []int                 `json:"missing_quantity"`
	Question        string                `json:"question"`
	Reasoning       string                `json:"reasoning"`
}

type groundedAddonLine struct {
	ID       int
	Name     string
	Quantity int
}

// askCatalogAddons shows numbered catalog add-ons for a product, parses the
// reply with the guide AI role, and stores grounded selections on
// commerce_addons. Returns false when waiting for a reply or after transfer.
// An empty product or no catalog add-ons returns true without asking.
func askCatalogAddons(c *Conv, productID, prefix string) bool {
	if c == nil || c.Stop {
		return false
	}
	productID = strings.TrimSpace(productID)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "product_addons"
	}
	choices := loadCatalogAddonChoices(c, productID, prefix)
	if c.Stop {
		return false
	}
	if len(choices) == 0 {
		return true
	}
	return askStructuredCatalogAddons(c, choices, prefix)
}

// loadCatalogAddonChoices loads active add-ons for one product. Product-level
// add-ons and the selected option's add-ons both count. A failed detail fetch
// still keeps add-ons already present on the product in session.
func loadCatalogAddonChoices(c *Conv, productID, prefix string) []map[string]any {
	productID = resolveCatalogProductID(c, productID)
	if productID == "" || c == nil {
		return nil
	}
	optionID := sessionOptionID(c)
	var choices []map[string]any
	raw, ok := c.Store(prefix+"_product", "get_product", map[string]string{"product_id": productID})
	if c.Stop {
		return nil
	}
	if ok && raw != nil {
		choices = addonChoicesFromProduct(raw, optionID)
	}
	return mergeAddonChoices(choices, addonChoicesFromKnownProducts(c, productID, optionID))
}

func resolveCatalogProductID(c *Conv, productID string) string {
	if id := strings.TrimSpace(productID); id != "" {
		return id
	}
	if c == nil || c.Session() == nil || c.Session().SessionData == nil {
		return ""
	}
	if id := strings.TrimSpace(asString(c.Session().SessionData["early_handoff_product_id"])); id != "" {
		return id
	}
	return strings.TrimSpace(asString(c.Session().SessionData["product_id"]))
}

func sessionOptionID(c *Conv) string {
	if c == nil || c.Session() == nil || c.Session().SessionData == nil {
		return ""
	}
	return strings.TrimSpace(asString(c.Session().SessionData["option_id"]))
}

func addonChoicesFromKnownProducts(c *Conv, productID, optionID string) []map[string]any {
	if c == nil || c.Session() == nil || c.Session().SessionData == nil || productID == "" {
		return nil
	}
	var choices []map[string]any
	for _, key := range []string{"products", "early_handoff_products"} {
		items, ok := anySlice(c.Session().SessionData[key])
		if !ok {
			continue
		}
		for _, entry := range items {
			item, ok := asStringMap(entry)
			if !ok || fieldString(item, "id") != productID {
				continue
			}
			choices = mergeAddonChoices(choices, addonChoicesFromProduct(item, optionID))
		}
	}
	return choices
}

func addonChoicesFromProduct(raw map[string]any, optionID string) []map[string]any {
	raw = unwrapProductPayload(raw)
	if raw == nil {
		return nil
	}
	choices := parseProductAddonChoices(raw["addons"])
	options, ok := anySlice(raw["options"])
	if !ok {
		options, ok = anySlice(raw["active_options"])
	}
	if !ok {
		return choices
	}
	optionID = strings.TrimSpace(optionID)
	for _, entry := range options {
		option, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if optionID != "" && fieldString(option, "id") != optionID {
			continue
		}
		choices = mergeAddonChoices(choices, parseProductAddonChoices(option["addons"]))
	}
	return choices
}

func unwrapProductPayload(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	if _, ok := raw["addons"]; ok {
		return raw
	}
	if _, ok := raw["options"]; ok {
		return raw
	}
	if _, ok := raw["active_options"]; ok {
		return raw
	}
	for _, key := range []string{"product", "data", "result"} {
		if nested, ok := asStringMap(raw[key]); ok {
			if found := unwrapProductPayload(nested); found != nil {
				return found
			}
		}
		items, ok := anySlice(raw[key])
		if !ok || len(items) == 0 {
			continue
		}
		nested, ok := asStringMap(items[0])
		if !ok {
			continue
		}
		if found := unwrapProductPayload(nested); found != nil {
			return found
		}
	}
	return raw
}

func mergeAddonChoices(base, extra []map[string]any) []map[string]any {
	if len(extra) == 0 {
		return base
	}
	seen := map[int]bool{}
	for _, choice := range base {
		if id := anyToInt(choice["id"]); id > 0 {
			seen[id] = true
		}
	}
	for _, choice := range extra {
		id := anyToInt(choice["id"])
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		base = append(base, choice)
	}
	return base
}

func askStructuredCatalogAddons(c *Conv, choices []map[string]any, prefix string) bool {
	step := prefix
	prompt := catalogAddonPrompt(c.Session(), choices)

	for attempt := 1; ; attempt++ {
		askName := fmt.Sprintf("%s_%d", prefix, attempt)
		body := prompt
		if pending := pendingAddonMissing(c.Session(), step); len(pending) > 0 {
			body = missingAddonQuantityQuestion(choices, pending)
		}

		text, ok := c.AskText(askName, body, codedflow.StepNote{
			Doing:  "The customer is choosing catalog add-ons for this product.",
			Expect: "Item numbers with quantities such as item 1 - 2, or Skip.",
		})
		if !ok {
			return false
		}

		lower := strings.ToLower(strings.TrimSpace(text))
		if isAddonSkipReply(lower) {
			c.ClearGuide()
			clearPendingAddonMissing(c.Session(), step)
			c.Once(fmt.Sprintf("%s_done_%d", prefix, attempt), func() {})
			return true
		}

		parseName := fmt.Sprintf("%s_parse_%d", prefix, attempt)
		result, replayed := replayAddonParse(c, parseName)
		if !replayed {
			result = runAddonParse(c, text, choices, pendingAddonMissing(c.Session(), step))
			c.AppendCall(addonParseRecord(parseName, result))
		}

		outcome := groundAddonParse(result, choices)
		switch outcome.kind {
		case codedAddonIntentSelect:
			c.ClearGuide()
			clearPendingAddonMissing(c.Session(), step)
			c.Once(fmt.Sprintf("%s_save_%d", prefix, attempt), func() {
				for _, line := range outcome.lines {
					appendCommerceAddon(c.Session(), line.ID, line.Quantity, line.Name)
				}
			})
			if msg := confirmAddonLines(outcome.lines); msg != "" {
				c.Say(msg)
			}
			return true
		case codedAddonIntentSkip:
			c.ClearGuide()
			clearPendingAddonMissing(c.Session(), step)
			return true
		case "missing_quantity":
			if !noteAddonClarify(c, prefix, attempt, step, outcome) {
				return false
			}
			setPendingAddonMissing(c.Session(), step, outcome.missing)
			prompt = missingAddonQuantityQuestion(choices, outcome.missing)
			continue
		default:
			if !noteAddonClarify(c, prefix, attempt, step, outcome) {
				return false
			}
			clearPendingAddonMissing(c.Session(), step)
			question := sanitizeAddonQuestion(outcome.question)
			if question == "" {
				question = "I didn't catch that. " + codedAddonPromptHint
			}
			prompt = question + "\n\n" + catalogAddonPrompt(c.Session(), choices)
			continue
		}
	}
}

// noteAddonClarify records one clarify turn. Replay skips the guide counter.
// After MaxGuideTurns unclear replies, the next unclear transfers.
func noteAddonClarify(c *Conv, prefix string, attempt int, step string, outcome addonGroundOutcome) bool {
	clarifyName := fmt.Sprintf("%s_clarify_%d", prefix, attempt)
	if rec, done := replayAddonClarify(c, clarifyName); done {
		return codedflow.CallOK(rec)
	}
	turns := c.GuideTurns(step)
	if turns >= codedflow.MaxGuideTurns() {
		c.ClearGuide()
		clearPendingAddonMissing(c.Session(), step)
		c.AppendCall(map[string]any{"name": clarifyName, "ok": false, "kind": "handoff"})
		_ = c.Transfer(codedflow.AgentHandoff)
		return false
	}
	c.SetGuide(step, turns+1)
	c.AppendCall(map[string]any{
		"name": clarifyName,
		"ok":   true,
		"kind": outcome.kind,
	})
	return true
}

func replayAddonClarify(c *Conv, name string) (map[string]any, bool) {
	if c == nil || c.Stop {
		return nil, false
	}
	records := c.CallRecords()
	if c.Seq() >= len(records) {
		return nil, false
	}
	rec := records[c.Seq()]
	if asString(rec["name"]) != name {
		return nil, false
	}
	c.AdvanceSeq()
	return rec, true
}

type addonGroundOutcome struct {
	kind     string
	lines    []groundedAddonLine
	missing  []int
	question string
}

func groundAddonParse(raw codedAddonParseResult, choices []map[string]any) addonGroundOutcome {
	raw.Intent = strings.ToLower(strings.TrimSpace(raw.Intent))
	raw.Question = strings.TrimSpace(raw.Question)
	if raw.Intent == codedAddonIntentSkip {
		if raw.Confidence > 0 && raw.Confidence < codedflow.IntentThreshold() {
			return addonGroundOutcome{kind: codedAddonIntentUnclear, question: raw.Question}
		}
		return addonGroundOutcome{kind: codedAddonIntentSkip}
	}
	if raw.Intent != codedAddonIntentSelect {
		return addonGroundOutcome{kind: codedAddonIntentUnclear, question: raw.Question}
	}
	if raw.Confidence < codedflow.IntentThreshold() {
		return addonGroundOutcome{kind: codedAddonIntentUnclear, question: raw.Question}
	}

	missingSet := map[int]bool{}
	for _, idx := range raw.MissingQuantity {
		if idx >= 1 && idx <= len(choices) {
			missingSet[idx] = true
		}
	}
	for _, item := range raw.Items {
		if item.Index < 1 || item.Index > len(choices) {
			return addonGroundOutcome{kind: codedAddonIntentUnclear, question: raw.Question}
		}
		if item.Quantity == nil || *item.Quantity < 1 {
			missingSet[item.Index] = true
		}
	}
	if len(missingSet) > 0 {
		missing := make([]int, 0, len(missingSet))
		for i := 1; i <= len(choices); i++ {
			if missingSet[i] {
				missing = append(missing, i)
			}
		}
		return addonGroundOutcome{kind: "missing_quantity", missing: missing, question: raw.Question}
	}
	if len(raw.Items) == 0 {
		return addonGroundOutcome{kind: codedAddonIntentUnclear, question: raw.Question}
	}

	lines := make([]groundedAddonLine, 0, len(raw.Items))
	seen := map[int]int{}
	for _, item := range raw.Items {
		choice := choices[item.Index-1]
		id := anyToInt(choice["id"])
		if id <= 0 || item.Quantity == nil || *item.Quantity < 1 {
			return addonGroundOutcome{kind: codedAddonIntentUnclear, question: raw.Question}
		}
		qty := *item.Quantity
		if idx, ok := seen[id]; ok {
			lines[idx].Quantity += qty
			continue
		}
		seen[id] = len(lines)
		lines = append(lines, groundedAddonLine{
			ID:       id,
			Name:     asString(choice["name"]),
			Quantity: qty,
		})
	}
	return addonGroundOutcome{kind: codedAddonIntentSelect, lines: lines}
}

func catalogAddonPrompt(session *models.ChatbotSession, choices []map[string]any) string {
	currency := sessionCurrencyCode(session)
	var b strings.Builder
	b.WriteString("This product has the following add-ons:\n")
	for i, choice := range choices {
		fmt.Fprintf(&b, "%d. %s", i+1, asString(choice["name"]))
		if price := asToolFloat(choice["price"]); price > 0 {
			b.WriteString(" — ")
			b.WriteString(formatMoney(price, currency))
		}
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(codedAddonPromptHint)
	return strings.TrimSpace(b.String())
}

func missingAddonQuantityQuestion(choices []map[string]any, indexes []int) string {
	if len(indexes) == 0 {
		return "How many would you like of that add-on? Reply with a whole number of 1 or more."
	}
	parts := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		if idx < 1 || idx > len(choices) {
			continue
		}
		name := asString(choices[idx-1]["name"])
		parts = append(parts, fmt.Sprintf("item %d (%s)", idx, name))
	}
	if len(parts) == 0 {
		return "How many would you like of that add-on? Reply with a whole number of 1 or more."
	}
	if len(parts) == 1 {
		return fmt.Sprintf("How many of %s would you like? Reply with a whole number of 1 or more.", parts[0])
	}
	return "How many would you like of " + strings.Join(parts, ", ") + "? Reply with whole numbers of 1 or more."
}

func confirmAddonLines(lines []groundedAddonLine) string {
	if len(lines) == 0 {
		return ""
	}
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		name := line.Name
		if name == "" {
			name = fmt.Sprintf("Addon #%d", line.ID)
		}
		parts = append(parts, fmt.Sprintf("%s x%d", name, line.Quantity))
	}
	return "Added " + strings.Join(parts, ", ") + "."
}

func isAddonSkipReply(lower string) bool {
	switch lower {
	case "skip", "no", "none", "done", "continue":
		return true
	default:
		return false
	}
}

func sanitizeAddonQuestion(question string) string {
	question = strings.TrimSpace(question)
	if question == "" || addonIDMentionRE.MatchString(question) {
		return ""
	}
	return question
}

const pendingAddonKey = "_coded_addon_pending"

func pendingAddonMissing(session *models.ChatbotSession, step string) []int {
	if session == nil || session.SessionData == nil {
		return nil
	}
	raw, ok := asStringMap(session.SessionData[pendingAddonKey])
	if !ok || asString(raw["step"]) != step {
		return nil
	}
	items, ok := anySlice(raw["missing"])
	if !ok {
		return nil
	}
	out := make([]int, 0, len(items))
	for _, item := range items {
		n := anyToInt(item)
		if n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func setPendingAddonMissing(session *models.ChatbotSession, step string, missing []int) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	values := make([]any, 0, len(missing))
	for _, n := range missing {
		values = append(values, n)
	}
	session.SessionData[pendingAddonKey] = map[string]any{
		"step":    step,
		"missing": values,
	}
}

func clearPendingAddonMissing(session *models.ChatbotSession, step string) {
	if session == nil || session.SessionData == nil {
		return
	}
	raw, ok := asStringMap(session.SessionData[pendingAddonKey])
	if ok && asString(raw["step"]) != "" && asString(raw["step"]) != step {
		return
	}
	delete(session.SessionData, pendingAddonKey)
}

func runAddonParse(c *Conv, text string, choices []map[string]any, pending []int) codedAddonParseResult {
	fallback := codedAddonParseResult{
		Intent:     codedAddonIntentUnclear,
		Confidence: 0,
		Question:   "I didn't catch that. " + codedAddonPromptHint,
	}
	if c == nil || c.App() == nil {
		return fallback
	}
	prompt := buildAddonParsePrompt(text, choices, pending)
	raw, err := parseCodedAddons(c.App(), c.Session(), prompt)
	call := codedflow.CodedPreviewAICall{Role: "guide", Prompt: prompt}
	if err != nil {
		call.Error = err.Error()
		c.NotePreviewAI(call)
		c.App().LogCodedFlowAI(c.Session(), "guide", prompt, "", err.Error(), "role", "addon_parse")
		return fallback
	}
	raw.Reasoning = codedflow.LimitWords(raw.Reasoning, 200)
	call.Response = formatAddonParseResponse(raw)
	call.Parsed = map[string]any{
		"intent":           raw.Intent,
		"confidence":       raw.Confidence,
		"items":            raw.Items,
		"missing_quantity": raw.MissingQuantity,
		"question":         raw.Question,
		"reasoning":        raw.Reasoning,
	}
	call.Confidence = raw.Confidence
	c.NotePreviewAI(call)
	c.App().LogCodedFlowAI(c.Session(), "guide", prompt, call.Response, "",
		"role", "addon_parse",
		"intent", raw.Intent,
		"confidence", fmt.Sprintf("%.2f", raw.Confidence),
	)
	return raw
}

func defaultParseCodedAddons(a codedflow.Host, session *models.ChatbotSession, prompt string) (codedAddonParseResult, error) {
	answer, err := a.CompleteCodedRoleText(session, "guide", prompt)
	if err != nil {
		return codedAddonParseResult{}, err
	}
	return parseAddonParseJSON(answer)
}

func parseAddonParseJSON(raw string) (codedAddonParseResult, error) {
	raw = strings.TrimSpace(raw)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var result codedAddonParseResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return codedAddonParseResult{}, err
	}
	return result, nil
}

func formatAddonParseResponse(raw codedAddonParseResult) string {
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(b)
}

func buildAddonParsePrompt(text string, choices []map[string]any, pending []int) string {
	var list strings.Builder
	for i, choice := range choices {
		fmt.Fprintf(&list, "%d. %s\n", i+1, asString(choice["name"]))
	}
	pendingNote := "(none)"
	if len(pending) > 0 {
		parts := make([]string, 0, len(pending))
		for _, idx := range pending {
			parts = append(parts, strconv.Itoa(idx))
		}
		pendingNote = strings.Join(parts, ", ")
	}
	return fmt.Sprintf(`You parse a customer's WhatsApp reply about optional product add-ons.
Return JSON only. Never invent, guess, or output an add-on id, product id, option id, or any other id.
Refer to listed items by their index number only.

Allowed items (index and name only):
%s
Indexes waiting for a quantity (if any): %s

Customer reply:
%q

intent select: the customer clearly chose one or more listed items.
intent skip: the customer does not want add-ons.
intent unclear: the reply does not clearly map onto the list.

Rules:
- items[].index must be a listed index from 1 to %d. Never invent an index.
- Copy quantity only when the customer stated it (digits or a number word), including forms like "item 1 - 2", "1 x 2", or several lines.
- If they named an item and left the quantity out, put that index in missing_quantity and leave quantity null. Do not assume 1.
- question is one short WhatsApp question only for unclear or missing quantity. Name the list number and add-on name. Do not mention ids.
- confidence from 0 to 1. Use a low value when the mapping is uncertain.
- reasoning is for operators only, at most 100 words.

Return JSON only:
{"intent":"select","confidence":0,"items":[{"index":1,"quantity":2}],"missing_quantity":[],"question":"","reasoning":""}
`, strings.TrimRight(list.String(), "\n"), pendingNote, text, len(choices))
}

func replayAddonParse(c *Conv, name string) (codedAddonParseResult, bool) {
	if c == nil || c.Stop {
		return codedAddonParseResult{}, false
	}
	records := c.CallRecords()
	if c.Seq() >= len(records) {
		return codedAddonParseResult{}, false
	}
	rec := records[c.Seq()]
	if asString(rec["name"]) != name || asString(rec["plan"]) != "addon_parse" {
		return codedAddonParseResult{}, false
	}
	c.AdvanceSeq()
	return addonParseFromRecord(rec), true
}

func addonParseRecord(name string, result codedAddonParseResult) map[string]any {
	items := make([]any, 0, len(result.Items))
	for _, item := range result.Items {
		row := map[string]any{"index": item.Index}
		if item.Quantity != nil {
			row["quantity"] = *item.Quantity
		}
		items = append(items, row)
	}
	missing := make([]any, 0, len(result.MissingQuantity))
	for _, idx := range result.MissingQuantity {
		missing = append(missing, idx)
	}
	return map[string]any{
		"name":             name,
		"plan":             "addon_parse",
		"ok":               true,
		"intent":           result.Intent,
		"confidence":       result.Confidence,
		"items":            items,
		"missing_quantity": missing,
		"question":         result.Question,
	}
}

func addonParseFromRecord(rec map[string]any) codedAddonParseResult {
	result := codedAddonParseResult{
		Intent:     asString(rec["intent"]),
		Confidence: asToolFloat(rec["confidence"]),
		Question:   asString(rec["question"]),
	}
	if items, ok := anySlice(rec["items"]); ok {
		for _, entry := range items {
			row, ok := asStringMap(entry)
			if !ok {
				continue
			}
			item := codedAddonParseItem{Index: anyToInt(row["index"])}
			if _, has := row["quantity"]; has {
				qty := anyToInt(row["quantity"])
				item.Quantity = &qty
			}
			result.Items = append(result.Items, item)
		}
	}
	if missing, ok := anySlice(rec["missing_quantity"]); ok {
		for _, entry := range missing {
			if n := anyToInt(entry); n > 0 {
				result.MissingQuantity = append(result.MissingQuantity, n)
			}
		}
	}
	return result
}
