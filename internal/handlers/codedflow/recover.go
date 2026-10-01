package codedflow

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
	RecoverTryLater     = "try_later"
	codedRecoverTryLater     = RecoverTryLater
	RecoverMissingField = "missing_field"
	codedRecoverMissingField = RecoverMissingField
	RecoverGiveUp       = "give_up"
	codedRecoverGiveUp       = RecoverGiveUp
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

var RecoverFields = map[string]string{
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
	RecoverCodedFailure = defaultRecoverCodedFailure
	tiqrErrStatusRE     = regexp.MustCompile(`(?i)ticker api error (\d+):\s*(.*)`)
	recoverBannedRE     = regexp.MustCompile(`(?i)https?://|authorization|api[_ ]?key|bearer |curl |token=|whatomate_|password|secret`)
)

type RecoverAsk struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type RecoverResult struct {
	Kind       string            `json:"kind"`
	Field      string            `json:"field"`
	Fields     []RecoverAsk `json:"fields,omitempty"`
	Message    string            `json:"message"`
	Confidence float64           `json:"confidence"`
	Reasoning  string            `json:"reasoning"`
}

type RecoverContext struct {
	Kind      string // fetch | create
	Operation string
	Resource  string // store, collections, products, order
	Status    int
	Hint      string // sanitized field names / short reason for the model only
}

func NoteTiqrFailure(chat Chat, status int, errText string) {
	if chat == nil {
		return
	}
	status, body := parseTiqrErr(status, errText)
	chat.SetLastTiqrStatus(status)
	chat.SetLastTiqrErr(truncateRunes(body, 800))
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

func SanitizeRecoverHint(raw string) string {
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
			if _, ok := RecoverFields[key]; ok || looksLikeFieldName(key) {
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

func (c *Conv) lastTiqrRecoverContext(operation, resource, kind string) RecoverContext {
	ctx := RecoverContext{
		Kind:      kind,
		Operation: operation,
		Resource:  resource,
	}
	if c == nil || c.chat == nil {
		return ctx
	}
	ctx.Status = c.chat.LastTiqrStatus()
	ctx.Hint = SanitizeRecoverHint(c.chat.LastTiqrErr())
	return ctx
}

func (c *Conv) recoverFetchMessage(operation, resource, fallback string) string {
	result := c.runRecover(c.lastTiqrRecoverContext(operation, resource, "fetch"), fallback)
	if result.Message != "" {
		return result.Message
	}
	return fallback
}

func (c *Conv) runRecover(ctx RecoverContext, fallback string) RecoverResult {
	fallbackResult := RecoverResult{
		Kind:       codedRecoverTryLater,
		Message:    fallback,
		Confidence: 1,
	}
	if c == nil || c.app == nil {
		return fallbackResult
	}
	prompt := BuildRecoverPrompt(ctx)
	raw, err := RecoverCodedFailure(c.app, c.session(), ctx)
	raw.Reasoning = LimitWords(raw.Reasoning, 200)
	call := CodedPreviewAICall{
		Role:      "recover",
		Prompt:    prompt,
		Reasoning: raw.Reasoning,
	}
	if err != nil {
		call.Error = err.Error()
		c.notePreviewAI(call)
		c.app.LogCodedFlowAI(c.session(), "recover", prompt, "", err.Error(),
			"operation", ctx.Operation, "kind", ctx.Kind)
		return fallbackResult
	}
	result, ok := ValidateCodedRecover(raw, ctx)
	if !ok {
		result = fallbackResult
	}
	if ctx.Kind == "create" {
		result = ExpandRecoverAsks(result, ctx.Hint)
	}
	call.Response = formatRecoverResponse(result)
	call.Parsed = map[string]any{
		"kind":       result.Kind,
		"field":      result.Field,
		"fields":     RecoverAskFields(result.Fields),
		"message":    result.Message,
		"confidence": raw.Confidence,
		"reasoning":  raw.Reasoning,
	}
	call.Confidence = raw.Confidence
	grounded := ok
	call.Grounded = &grounded
	c.notePreviewAI(call)
	c.app.LogCodedFlowAI(c.session(), "recover", prompt, call.Response, "",
		"kind", result.Kind,
		"field", result.Field,
		"fields", strings.Join(RecoverAskFields(result.Fields), ","),
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
func (c *Conv) CreateRecoverPlan(name, fallback string) RecoverResult {
	if plan, ok := c.replayRecoverPlan(name); ok {
		return plan
	}
	ctx := c.lastTiqrRecoverContext("create_order", "order", "create")
	result := c.runRecover(ctx, fallback)
	result = ExpandRecoverAsks(result, ctx.Hint)
	c.appendCall(recoverPlanRecord(name, result))
	return result
}

func (c *Conv) replayRecoverPlan(name string) (RecoverResult, bool) {
	if c == nil || c.Stop {
		return RecoverResult{}, false
	}
	records := c.callRecords()
	if c.seq >= len(records) {
		return RecoverResult{}, false
	}
	rec := records[c.seq]
	if asString(rec["name"]) != name || asString(rec["plan"]) != "recover" {
		return RecoverResult{}, false
	}
	c.seq++
	c.restore(rec)
	return recoverResultFromRecord(rec), true
}

func recoverPlanRecord(name string, result RecoverResult) map[string]any {
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

func recoverResultFromRecord(rec map[string]any) RecoverResult {
	result := RecoverResult{
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
		result.Fields = append(result.Fields, RecoverAsk{
			Field:   asString(ask["field"]),
			Message: asString(ask["message"]),
		})
	}
	return result
}

func formatRecoverResponse(raw RecoverResult) string {
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(b)
}

func ValidateCodedRecover(raw RecoverResult, ctx RecoverContext) (RecoverResult, bool) {
	raw.Kind = strings.ToLower(strings.TrimSpace(raw.Kind))
	raw.Field = strings.TrimSpace(raw.Field)
	raw.Message = strings.TrimSpace(raw.Message)
	raw.Reasoning = LimitWords(raw.Reasoning, 200)
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
		asks := append([]RecoverAsk{}, raw.Fields...)
		if raw.Field != "" {
			asks = append(asks, RecoverAsk{Field: raw.Field, Message: raw.Message})
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
	if _, ok := RecoverFields[field]; ok {
		return field, true
	}
	lower := strings.ToLower(field)
	if lower != field {
		return canonicalizeRecoverField(lower)
	}
	for key := range RecoverFields {
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

func orderRecoverAsks(asks []RecoverAsk) []RecoverAsk {
	rank := recoverFieldRanks()
	type item struct {
		ask  RecoverAsk
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
			msg = DefaultRecoverAsk(canon)
		}
		r, ok := rank[canon]
		if !ok {
			r = len(rank)
		}
		items = append(items, item{ask: RecoverAsk{Field: canon, Message: msg}, rank: r})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].rank < items[j].rank
	})
	out := make([]RecoverAsk, len(items))
	for i, it := range items {
		out[i] = it.ask
	}
	return out
}

func ExpandRecoverAsks(result RecoverResult, hint string) RecoverResult {
	wanted := CanonicalFieldsFromHint(hint)
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
	asks := make([]RecoverAsk, 0, len(wanted))
	for _, field := range wanted {
		msg := strings.TrimSpace(messages[field])
		if msg == "" || recoverBannedRE.MatchString(msg) {
			msg = DefaultRecoverAsk(field)
		}
		asks = append(asks, RecoverAsk{Field: field, Message: msg})
	}
	result.Kind = codedRecoverMissingField
	result.Fields = asks
	result.Field = asks[0].Field
	if strings.TrimSpace(result.Message) == "" {
		result.Message = asks[0].Message
	}
	return result
}

func CanonicalFieldsFromHint(hint string) []string {
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

func DefaultRecoverAsk(field string) string {
	label := RecoverFields[field]
	if label == "" {
		label = strings.ReplaceAll(field, "_", " ")
	}
	return "Could you share your " + label + "?"
}

func RecoverAskFields(asks []RecoverAsk) []string {
	out := make([]string, 0, len(asks))
	for _, ask := range asks {
		if ask.Field != "" {
			out = append(out, ask.Field)
		}
	}
	return out
}

func defaultRecoverCodedFailure(a Host, session *models.ChatbotSession, ctx RecoverContext) (RecoverResult, error) {
	prompt := BuildRecoverPrompt(ctx)
	answer, err := a.CompleteCodedRoleText(session, codedFlowRoleRecover, prompt)
	if err != nil {
		return RecoverResult{}, err
	}
	return ParseCodedRecover(answer)
}

func ParseCodedRecover(raw string) (RecoverResult, error) {
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
		return RecoverResult{}, err
	}
	return RecoverResult{
		Kind:       payload.Kind,
		Field:      payload.Field,
		Fields:     decodeRecoverAsks(payload.Fields),
		Message:    payload.Message,
		Confidence: payload.Confidence,
		Reasoning:  LimitWords(payload.Reasoning, 200),
	}, nil
}

func decodeRecoverAsks(raw json.RawMessage) []RecoverAsk {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var asks []RecoverAsk
	if err := json.Unmarshal(raw, &asks); err == nil {
		return asks
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil
	}
	asks = make([]RecoverAsk, 0, len(names))
	for _, name := range names {
		asks = append(asks, RecoverAsk{Field: name})
	}
	return asks
}

func BuildRecoverPrompt(ctx RecoverContext) string {
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
		fields = append(fields, key+" ("+RecoverFields[key]+")")
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

func codedOrderRetries(h Host) int {
	if h != nil {
		return h.CodedOrderRetries()
	}
	return IntentSettings.OrderRetries
}


type codedRecoverContext = RecoverContext
