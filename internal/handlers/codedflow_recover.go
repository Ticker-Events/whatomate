package handlers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	codedRecoverTryLater     = "try_later"
	codedRecoverMissingField = "missing_field"
	codedRecoverGiveUp       = "give_up"
)

// Customer-facing session keys the recover role may ask to refill on create_order.
// Ask order is fixed so every missing detail is collected before the order is retried.
var codedRecoverFieldOrder = []string{
	"customer_name",
	"customer_phone",
	"customer_email",
	"address_line_one",
	"address_line_two",
	"city",
	"state",
	"country",
	"pincode",
	"customer_notes",
}

var codedRecoverFields = map[string]string{
	"customer_email":   "email address",
	"customer_name":    "name",
	"customer_phone":   "phone number",
	"address_line_one": "address line 1",
	"address_line_two": "address line 2",
	"city":             "city",
	"state":            "state",
	"country":          "country",
	"pincode":          "pincode",
	"customer_notes":   "order notes",
}

// API names that map onto the session keys above.
var recoverFieldAliases = map[string]string{
	"email":          "customer_email",
	"phone":          "customer_phone",
	"phone_number":   "customer_phone",
	"address_line_1": "address_line_one",
	"address_line_2": "address_line_two",
	"name":           "customer_name",
	"notes":          "customer_notes",
}

var (
	recoverCodedFailure = defaultRecoverCodedFailure
	tiqrErrStatusRE     = regexp.MustCompile(`(?i)ticker api error (\d+):\s*(.*)`)
	recoverBannedRE     = regexp.MustCompile(`(?i)https?://|authorization|api[_ ]?key|bearer |curl |token=|whatomate_|password|secret`)
)

type codedRecoverAsk struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type codedRecoverResult struct {
	Kind       string            `json:"kind"`
	Field      string            `json:"field"`
	Fields     []codedRecoverAsk `json:"fields,omitempty"`
	Message    string            `json:"message"`
	Confidence float64           `json:"confidence"`
	Reasoning  string            `json:"reasoning"`
}

type codedRecoverContext struct {
	Kind      string // fetch | create
	Operation string
	Resource  string // store, collections, products, order
	Status    int
	Hint      string // sanitized field names / short reason for the model only
}

func noteTiqrFailure(ctx *chatNodeCtx, status int, errText string) {
	if ctx == nil {
		return
	}
	status, body := parseTiqrErr(status, errText)
	ctx.lastTiqrStatus = status
	ctx.lastTiqrErr = truncateRunes(body, 800)
}

func parseTiqrErr(status int, errText string) (int, string) {
	errText = strings.TrimSpace(errText)
	if m := tiqrErrStatusRE.FindStringSubmatch(errText); len(m) == 3 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			status = n
		}
		return status, strings.TrimSpace(m[2])
	}
	return status, errText
}

func sanitizeRecoverHint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = recoverBannedRE.ReplaceAllString(raw, "[redacted]")
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		fields := sortHintFields(collectValidationFields(parsed, nil))
		if len(fields) > 0 {
			return "validation fields: " + strings.Join(fields, ", ")
		}
		return "structured error without usable field names"
	}
	return truncateRunes(raw, 240)
}

func collectValidationFields(v any, out []string) []string {
	switch t := v.(type) {
	case map[string]any:
		for key, child := range t {
			key = strings.TrimSpace(key)
			if key == "" || strings.EqualFold(key, "detail") || strings.EqualFold(key, "message") {
				out = collectValidationFields(child, out)
				continue
			}
			if _, ok := codedRecoverFields[key]; ok || looksLikeFieldName(key) {
				out = appendUnique(out, key)
			}
			out = collectValidationFields(child, out)
		}
	case []any:
		for _, child := range t {
			out = collectValidationFields(child, out)
		}
	}
	return out
}

func looksLikeFieldName(key string) bool {
	if len(key) > 40 || strings.Contains(key, " ") {
		return false
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func appendUnique(list []string, item string) []string {
	for _, existing := range list {
		if existing == item {
			return list
		}
	}
	return append(list, item)
}

func (c *Conv) lastTiqrRecoverContext(operation, resource, kind string) codedRecoverContext {
	ctx := codedRecoverContext{
		Kind:      kind,
		Operation: operation,
		Resource:  resource,
	}
	if c == nil || c.chat == nil {
		return ctx
	}
	ctx.Status = c.chat.lastTiqrStatus
	ctx.Hint = sanitizeRecoverHint(c.chat.lastTiqrErr)
	return ctx
}

func (c *Conv) recoverFetchMessage(operation, resource, fallback string) string {
	result := c.runRecover(c.lastTiqrRecoverContext(operation, resource, "fetch"), fallback)
	if result.Message != "" {
		return result.Message
	}
	return fallback
}

func (c *Conv) runRecover(ctx codedRecoverContext, fallback string) codedRecoverResult {
	fallbackResult := codedRecoverResult{
		Kind:       codedRecoverTryLater,
		Message:    fallback,
		Confidence: 1,
	}
	if c == nil || c.app == nil {
		return fallbackResult
	}
	prompt := buildRecoverPrompt(ctx)
	raw, err := recoverCodedFailure(c.app, c.session(), ctx)
	raw.Reasoning = limitWords(raw.Reasoning, 200)
	call := CodedPreviewAICall{
		Role:      "recover",
		Prompt:    prompt,
		Reasoning: raw.Reasoning,
	}
	if err != nil {
		call.Error = err.Error()
		c.notePreviewAI(call)
		c.app.logCodedFlowAI(c.session(), "recover", prompt, "", err.Error(),
			"operation", ctx.Operation, "kind", ctx.Kind)
		return fallbackResult
	}
	result, ok := validateCodedRecover(raw, ctx)
	if !ok {
		result = fallbackResult
	}
	if ctx.Kind == "create" {
		result = expandRecoverAsks(result, ctx.Hint)
	}
	call.Response = formatRecoverResponse(result)
	call.Parsed = map[string]any{
		"kind":       result.Kind,
		"field":      result.Field,
		"fields":     recoverAskFields(result.Fields),
		"message":    result.Message,
		"confidence": raw.Confidence,
		"reasoning":  raw.Reasoning,
	}
	call.Confidence = raw.Confidence
	grounded := ok
	call.Grounded = &grounded
	c.notePreviewAI(call)
	c.app.logCodedFlowAI(c.session(), "recover", prompt, call.Response, "",
		"kind", result.Kind,
		"field", result.Field,
		"fields", strings.Join(recoverAskFields(result.Fields), ","),
		"grounded", ok,
		"operation", ctx.Operation,
		"reasoning", raw.Reasoning,
	)
	if result.Kind == codedRecoverMissingField && len(result.Fields) > 0 {
		return result
	}
	if !ok || result.Message == "" {
		return fallbackResult
	}
	return result
}

// createRecoverPlan returns the saved create_order recovery for this attempt,
// or asks the model once and stores that plan. Later turns replay the plan so
// every missing field is asked before the order is retried.
func (c *Conv) createRecoverPlan(name, fallback string) codedRecoverResult {
	if plan, ok := c.replayRecoverPlan(name); ok {
		return plan
	}
	ctx := c.lastTiqrRecoverContext("create_order", "order", "create")
	result := c.runRecover(ctx, fallback)
	result = expandRecoverAsks(result, ctx.Hint)
	c.appendCall(recoverPlanRecord(name, result))
	return result
}

func (c *Conv) replayRecoverPlan(name string) (codedRecoverResult, bool) {
	if c == nil || c.stop {
		return codedRecoverResult{}, false
	}
	records := c.callRecords()
	if c.seq >= len(records) {
		return codedRecoverResult{}, false
	}
	rec := records[c.seq]
	if asString(rec["name"]) != name || asString(rec["plan"]) != "recover" {
		return codedRecoverResult{}, false
	}
	c.seq++
	c.restore(rec)
	return recoverResultFromRecord(rec), true
}

func recoverPlanRecord(name string, result codedRecoverResult) map[string]any {
	asks := make([]any, 0, len(result.Fields))
	for _, ask := range result.Fields {
		asks = append(asks, map[string]any{
			"field":   ask.Field,
			"message": ask.Message,
		})
	}
	return map[string]any{
		"name":    name,
		"plan":    "recover",
		"ok":      true,
		"kind":    result.Kind,
		"field":   result.Field,
		"message": result.Message,
		"asks":    asks,
	}
}

func recoverResultFromRecord(rec map[string]any) codedRecoverResult {
	result := codedRecoverResult{
		Kind:       asString(rec["kind"]),
		Field:      asString(rec["field"]),
		Message:    asString(rec["message"]),
		Confidence: 1,
	}
	items, ok := anySlice(rec["asks"])
	if !ok {
		return result
	}
	for _, item := range items {
		ask, ok := asStringMap(item)
		if !ok {
			continue
		}
		result.Fields = append(result.Fields, codedRecoverAsk{
			Field:   asString(ask["field"]),
			Message: asString(ask["message"]),
		})
	}
	return result
}

func formatRecoverResponse(raw codedRecoverResult) string {
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(b)
}

func validateCodedRecover(raw codedRecoverResult, ctx codedRecoverContext) (codedRecoverResult, bool) {
	raw.Kind = strings.ToLower(strings.TrimSpace(raw.Kind))
	raw.Field = strings.TrimSpace(raw.Field)
	raw.Message = strings.TrimSpace(raw.Message)
	raw.Reasoning = limitWords(raw.Reasoning, 200)
	if raw.Message != "" && recoverBannedRE.MatchString(raw.Message) {
		return raw, false
	}
	switch raw.Kind {
	case codedRecoverTryLater, codedRecoverGiveUp:
		if raw.Message == "" || raw.Field != "" || len(raw.Fields) > 0 {
			return raw, false
		}
		return raw, true
	case codedRecoverMissingField:
		if ctx.Kind != "create" {
			return raw, false
		}
		asks := append([]codedRecoverAsk{}, raw.Fields...)
		if raw.Field != "" {
			asks = append(asks, codedRecoverAsk{Field: raw.Field, Message: raw.Message})
		}
		asks = orderRecoverAsks(asks)
		if len(asks) == 0 {
			return raw, false
		}
		raw.Fields = asks
		raw.Field = asks[0].Field
		if raw.Message == "" {
			raw.Message = asks[0].Message
		}
		return raw, true
	default:
		return raw, false
	}
}

func canonicalizeRecoverField(field string) (string, bool) {
	field = strings.TrimSpace(field)
	if field == "" {
		return "", false
	}
	if canon, ok := recoverFieldAliases[field]; ok {
		return canon, true
	}
	if _, ok := codedRecoverFields[field]; ok {
		return field, true
	}
	lower := strings.ToLower(field)
	if lower != field {
		return canonicalizeRecoverField(lower)
	}
	for key := range codedRecoverFields {
		if strings.ToLower(key) == lower {
			return canonicalizeRecoverField(key)
		}
	}
	for key := range recoverFieldAliases {
		if strings.ToLower(key) == lower {
			return canonicalizeRecoverField(key)
		}
	}
	return "", false
}

func orderRecoverAsks(asks []codedRecoverAsk) []codedRecoverAsk {
	rank := recoverFieldRanks()
	type item struct {
		ask  codedRecoverAsk
		rank int
	}
	seen := map[string]struct{}{}
	items := make([]item, 0, len(asks))
	for _, ask := range asks {
		canon, ok := canonicalizeRecoverField(ask.Field)
		if !ok {
			continue
		}
		if _, dup := seen[canon]; dup {
			continue
		}
		seen[canon] = struct{}{}
		msg := strings.TrimSpace(ask.Message)
		if msg == "" || recoverBannedRE.MatchString(msg) {
			msg = defaultRecoverAsk(canon)
		}
		r, ok := rank[canon]
		if !ok {
			r = len(rank)
		}
		items = append(items, item{ask: codedRecoverAsk{Field: canon, Message: msg}, rank: r})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].rank < items[j].rank
	})
	out := make([]codedRecoverAsk, len(items))
	for i, it := range items {
		out[i] = it.ask
	}
	return out
}

func expandRecoverAsks(result codedRecoverResult, hint string) codedRecoverResult {
	wanted := canonicalFieldsFromHint(hint)
	if len(wanted) == 0 {
		return result
	}
	messages := map[string]string{}
	for _, ask := range result.Fields {
		if strings.TrimSpace(ask.Message) != "" {
			messages[ask.Field] = ask.Message
		}
	}
	if result.Kind == codedRecoverMissingField && result.Field != "" && messages[result.Field] == "" {
		messages[result.Field] = result.Message
	}
	asks := make([]codedRecoverAsk, 0, len(wanted))
	for _, field := range wanted {
		msg := strings.TrimSpace(messages[field])
		if msg == "" || recoverBannedRE.MatchString(msg) {
			msg = defaultRecoverAsk(field)
		}
		asks = append(asks, codedRecoverAsk{Field: field, Message: msg})
	}
	result.Kind = codedRecoverMissingField
	result.Fields = asks
	result.Field = asks[0].Field
	if strings.TrimSpace(result.Message) == "" {
		result.Message = asks[0].Message
	}
	return result
}

func canonicalFieldsFromHint(hint string) []string {
	hint = strings.TrimSpace(hint)
	rest, ok := strings.CutPrefix(hint, "validation fields:")
	if !ok {
		return nil
	}
	var names []string
	for _, part := range strings.Split(rest, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			names = append(names, part)
		}
	}
	return orderCanonicalNames(names)
}

func orderCanonicalNames(names []string) []string {
	rank := recoverFieldRanks()
	type item struct {
		field string
		rank  int
	}
	seen := map[string]struct{}{}
	items := make([]item, 0, len(names))
	for _, name := range names {
		canon, ok := canonicalizeRecoverField(name)
		if !ok {
			continue
		}
		if _, dup := seen[canon]; dup {
			continue
		}
		seen[canon] = struct{}{}
		r, ok := rank[canon]
		if !ok {
			r = len(rank)
		}
		items = append(items, item{canon, r})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].rank < items[j].rank
	})
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.field
	}
	return out
}

func recoverFieldRanks() map[string]int {
	rank := make(map[string]int, len(codedRecoverFieldOrder))
	for i, key := range codedRecoverFieldOrder {
		rank[key] = i
	}
	return rank
}

func sortHintFields(names []string) []string {
	rank := recoverFieldRanks()
	type item struct {
		name string
		rank int
	}
	items := make([]item, len(names))
	for i, name := range names {
		r := len(rank)
		if canon, ok := canonicalizeRecoverField(name); ok {
			if n, known := rank[canon]; known {
				r = n
			}
		}
		items[i] = item{name: name, rank: r}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].rank != items[j].rank {
			return items[i].rank < items[j].rank
		}
		return items[i].name < items[j].name
	})
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.name
	}
	return out
}

func defaultRecoverAsk(field string) string {
	label := codedRecoverFields[field]
	if label == "" {
		label = strings.ReplaceAll(field, "_", " ")
	}
	return "Could you share your " + label + "?"
}

func recoverAskFields(asks []codedRecoverAsk) []string {
	out := make([]string, 0, len(asks))
	for _, ask := range asks {
		if ask.Field != "" {
			out = append(out, ask.Field)
		}
	}
	return out
}

func defaultRecoverCodedFailure(a *App, session *models.ChatbotSession, ctx codedRecoverContext) (codedRecoverResult, error) {
	settings, ok := codedRoleSettings(a, session, codedIntentSettings.Recover)
	if !ok {
		return codedRecoverResult{}, fmt.Errorf("ai is not configured")
	}
	prompt := buildRecoverPrompt(ctx)
	answer, err := a.completeCodedText(settings, session, prompt, "")
	if err != nil {
		return codedRecoverResult{}, err
	}
	return parseCodedRecover(answer)
}

func parseCodedRecover(raw string) (codedRecoverResult, error) {
	raw = strings.TrimSpace(raw)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var payload struct {
		Kind       string          `json:"kind"`
		Field      string          `json:"field"`
		Fields     json.RawMessage `json:"fields"`
		Message    string          `json:"message"`
		Confidence float64         `json:"confidence"`
		Reasoning  string          `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return codedRecoverResult{}, err
	}
	return codedRecoverResult{
		Kind:       payload.Kind,
		Field:      payload.Field,
		Fields:     decodeRecoverAsks(payload.Fields),
		Message:    payload.Message,
		Confidence: payload.Confidence,
		Reasoning:  limitWords(payload.Reasoning, 200),
	}, nil
}

func decodeRecoverAsks(raw json.RawMessage) []codedRecoverAsk {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var asks []codedRecoverAsk
	if err := json.Unmarshal(raw, &asks); err == nil {
		return asks
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil
	}
	asks = make([]codedRecoverAsk, 0, len(names))
	for _, name := range names {
		asks = append(asks, codedRecoverAsk{Field: name})
	}
	return asks
}

func buildRecoverPrompt(ctx codedRecoverContext) string {
	kind := ctx.Kind
	if kind == "" {
		kind = "fetch"
	}
	resource := ctx.Resource
	if resource == "" {
		resource = "catalog"
	}
	status := "(unknown)"
	if ctx.Status > 0 {
		status = strconv.Itoa(ctx.Status)
	}
	hint := strings.TrimSpace(ctx.Hint)
	if hint == "" {
		hint = "(none)"
	}
	fields := make([]string, 0, len(codedRecoverFieldOrder))
	for _, key := range codedRecoverFieldOrder {
		fields = append(fields, key+" ("+codedRecoverFields[key]+")")
	}
	return fmt.Sprintf(`You help a shopping chatbot recover from a store lookup or order failure.
You write only what the customer should see. Never mention APIs, URLs, HTTP, tokens, keys, payloads, curl, databases, servers, or internal ids.

Failure kind: %s
Customer resource: %s
HTTP status if known: %s
Sanitized hint for you only (may list missing field names): %s

Allowed missing_field values when kind is create, in the order to ask them: %s

kind try_later: temporary fetch/create problem. Ask them to try again shortly. Leave field empty and fields empty.
kind missing_field: only when kind is create and the hint clearly names one or more missing customer details. Put every named detail in fields, using only allowed values, in the order listed above. Skip allowed values the hint does not name. Each fields item is {"field":"<allowed value>","message":"<one or two short sentences asking for that value only>"}. Also set field to the first fields item. Do not stop after one missing detail.
kind give_up: cannot recover. Apologize briefly and suggest trying later or messaging staff. Leave field empty and fields empty.

message must be one or two short sentences the customer can read on WhatsApp. For missing_field it can repeat the first question. confidence from 0 to 1.
reasoning explains why you chose this kind, for an operator debugging the flow. At most 100 words. The customer never sees it.

Return JSON only:
{"kind":"try_later","field":"","fields":[],"message":"","confidence":0,"reasoning":""}
`, kind, resource, status, hint, strings.Join(fields, ", "))
}

func (a *App) codedOrderRetries() int {
	if a != nil && a.Config != nil {
		return a.Config.CodedFlow.OrderRetryCount()
	}
	return codedIntentSettings.OrderRetries
}
