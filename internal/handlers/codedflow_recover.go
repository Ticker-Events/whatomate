package handlers

import (
	"encoding/json"
	"fmt"
	"regexp"
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
	"email":            "email address",
	"phone":            "phone number",
	"address_line_1":   "address line 1",
	"address_line_2":   "address line 2",
}

var (
	recoverCodedFailure = defaultRecoverCodedFailure
	tiqrErrStatusRE     = regexp.MustCompile(`(?i)ticker api error (\d+):\s*(.*)`)
	recoverBannedRE     = regexp.MustCompile(`(?i)https?://|authorization|api[_ ]?key|bearer |curl |token=|whatomate_|password|secret`)
)

type codedRecoverResult struct {
	Kind       string  `json:"kind"`
	Field      string  `json:"field"`
	Message    string  `json:"message"`
	Confidence float64 `json:"confidence"`
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
		fields := collectValidationFields(parsed, nil)
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
	call := CodedPreviewAICall{
		Role:   "recover",
		Prompt: prompt,
	}
	if err != nil {
		call.Error = err.Error()
		c.notePreviewAI(call)
		c.app.logCodedFlowAI(c.session(), "recover", prompt, "", err.Error(),
			"operation", ctx.Operation, "kind", ctx.Kind)
		return fallbackResult
	}
	call.Response = formatRecoverResponse(raw)
	call.Parsed = map[string]any{
		"kind":       raw.Kind,
		"field":      raw.Field,
		"message":    raw.Message,
		"confidence": raw.Confidence,
	}
	call.Confidence = raw.Confidence
	result, ok := validateCodedRecover(raw, ctx)
	grounded := ok
	call.Grounded = &grounded
	c.notePreviewAI(call)
	c.app.logCodedFlowAI(c.session(), "recover", prompt, call.Response, "",
		"kind", result.Kind,
		"field", result.Field,
		"grounded", ok,
		"operation", ctx.Operation,
	)
	if !ok || result.Message == "" {
		return fallbackResult
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
	if raw.Message == "" || recoverBannedRE.MatchString(raw.Message) {
		return raw, false
	}
	switch raw.Kind {
	case codedRecoverTryLater, codedRecoverGiveUp:
		if raw.Field != "" {
			return raw, false
		}
		return raw, true
	case codedRecoverMissingField:
		if ctx.Kind != "create" {
			return raw, false
		}
		canon, ok := canonicalizeRecoverField(raw.Field)
		if !ok {
			return raw, false
		}
		raw.Field = canon
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
	if _, ok := codedRecoverFields[field]; ok {
		switch field {
		case "email":
			return "customer_email", true
		case "phone":
			return "customer_phone", true
		case "address_line_1":
			return "address_line_one", true
		case "address_line_2":
			return "address_line_two", true
		default:
			return field, true
		}
	}
	lower := strings.ToLower(field)
	for key := range codedRecoverFields {
		if strings.ToLower(key) == lower {
			return canonicalizeRecoverField(key)
		}
	}
	return "", false
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
	var body codedRecoverResult
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return codedRecoverResult{}, err
	}
	return body, nil
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
	fields := make([]string, 0, len(codedRecoverFields))
	for key, label := range codedRecoverFields {
		if key == "email" || key == "phone" || key == "address_line_1" || key == "address_line_2" {
			continue
		}
		fields = append(fields, key+" ("+label+")")
	}
	return fmt.Sprintf(`You help a shopping chatbot recover from a store lookup or order failure.
You write only what the customer should see. Never mention APIs, URLs, HTTP, tokens, keys, payloads, curl, databases, servers, or internal ids.

Failure kind: %s
Customer resource: %s
HTTP status if known: %s
Sanitized hint for you only (may list missing field names): %s

Allowed missing_field values when kind is create: %s

Return JSON only:
{"kind":"try_later","field":"","message":"","confidence":0}

kind try_later: temporary fetch/create problem. Ask them to try again shortly. Leave field empty.
kind missing_field: only when kind is create and the hint clearly names a missing customer detail. Set field to one allowed value. Ask them to reply with that value only.
kind give_up: cannot recover. Apologize briefly and suggest trying later or messaging staff. Leave field empty.

message must be one or two short sentences the customer can read on WhatsApp. confidence from 0 to 1.
`, kind, resource, status, hint, strings.Join(fields, ", "))
}

func (a *App) codedOrderRetries() int {
	if a != nil && a.Config != nil {
		return a.Config.CodedFlow.OrderRetryCount()
	}
	return codedIntentSettings.OrderRetries
}
