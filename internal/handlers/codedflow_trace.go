package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	codedFlowTraceComponent = "coded_flow"
	codedFlowTraceMaxRunes  = 8000
)

// codedFlowTraceEnabled reports whether detailed coded-flow tracing is on.
// WHATOMATE_CODEDFLOW_TRACE defaults to enabled when the variable is unset or blank.
// Set to 0/false/no/off to disable. Config [codedflow].trace is used when the
// env var is unset and Config is loaded.
func (a *App) codedFlowTraceEnabled() bool {
	if v, ok := os.LookupEnv("WHATOMATE_CODEDFLOW_TRACE"); ok {
		return parseTruthyEnv(v, true)
	}
	if a != nil && a.Config != nil {
		return a.Config.CodedFlow.TraceEnabled()
	}
	return true
}

func parseTruthyEnv(raw string, defaultWhenEmpty bool) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultWhenEmpty
	}
	switch strings.ToLower(raw) {
	case "0", "false", "no", "off", "disable", "disabled":
		return false
	default:
		return true
	}
}

func (a *App) shouldTraceCodedFlow(session *models.ChatbotSession) bool {
	if a == nil || !a.codedFlowTraceEnabled() {
		return false
	}
	return codedFlowSessionKey(session) != ""
}

// logCodedFlow writes a structured, SignOz-filterable log line.
// Filter in SignOz with: attributes.component = "coded_flow"
// and attributes.event in {inbound, turn_context, whatsapp_outbound, ai_request, ai_response, tiqr_request, tiqr_response}.
func (a *App) logCodedFlow(event string, session *models.ChatbotSession, attrs ...any) {
	if a == nil || !a.shouldTraceCodedFlow(session) {
		return
	}
	fields := []any{
		"component", codedFlowTraceComponent,
		"event", event,
	}
	if session != nil {
		fields = append(fields,
			"session_id", session.ID.String(),
			"org_id", session.OrganizationID.String(),
			"account", session.WhatsAppAccount,
			"phone", session.PhoneNumber,
			"flow_key", codedFlowSessionKey(session),
			"step", session.CurrentStep,
			"session_status", string(session.Status),
		)
	}
	fields = append(fields, attrs...)
	a.Log.Info("coded_flow."+event, fields...)
}

func (a *App) logCodedFlowInbound(ctx *chatNodeCtx) {
	if ctx == nil || ctx.capturing() || !a.shouldTraceCodedFlow(ctx.session) {
		return
	}
	a.logCodedFlow("inbound", ctx.session,
		"user_text", truncateTrace(ctx.userInput),
		"button_id", strings.TrimSpace(ctx.buttonID),
		"has_flow_response", len(ctx.flowResponseData) > 0,
	)
}

func (a *App) logCodedFlowContext(session *models.ChatbotSession, reason string) {
	if !a.shouldTraceCodedFlow(session) {
		return
	}
	a.logCodedFlow("turn_context", session,
		"reason", reason,
		"context_json", truncateTrace(mustJSON(previewSessionContext(session))),
	)
}

func (a *App) logCodedFlowWhatsApp(ctx *chatNodeCtx, step, interactive, content string, extra ...any) {
	if ctx == nil || ctx.capturing() || !a.shouldTraceCodedFlow(ctx.session) {
		return
	}
	attrs := []any{
		"whatsapp_step", step,
		"interactive", interactive,
		"message_body", truncateTrace(content),
	}
	attrs = append(attrs, extra...)
	a.logCodedFlow("whatsapp_outbound", ctx.session, attrs...)
}

func (a *App) logCodedFlowAI(session *models.ChatbotSession, role, prompt, response, errMsg string, extra ...any) {
	if !a.shouldTraceCodedFlow(session) {
		return
	}
	a.logCodedFlow("ai_request", session,
		append([]any{
			"ai_role", role,
			"prompt", truncateTrace(prompt),
		}, extra...)...,
	)
	attrs := []any{
		"ai_role", role,
		"response", truncateTrace(response),
	}
	if errMsg != "" {
		attrs = append(attrs, "error", errMsg)
	}
	attrs = append(attrs, extra...)
	a.logCodedFlow("ai_response", session, attrs...)
}

func (a *App) logCodedFlowTiqrRequest(session *models.ChatbotSession, apiType, operation, curl string, params map[string]string) {
	if !a.shouldTraceCodedFlow(session) {
		return
	}
	a.logCodedFlow("tiqr_request", session,
		"api_type", apiType,
		"operation", operation,
		"curl", truncateTrace(curl),
		"params_json", truncateTrace(mustJSON(params)),
	)
}

func (a *App) logCodedFlowTiqrResponse(session *models.ChatbotSession, apiType, operation string, status int, body any, errMsg string) {
	if !a.shouldTraceCodedFlow(session) {
		return
	}
	attrs := []any{
		"api_type", apiType,
		"operation", operation,
		"http_status", status,
		"response_json", truncateTrace(mustJSON(body)),
	}
	if errMsg != "" {
		attrs = append(attrs, "error", errMsg)
	}
	a.logCodedFlow("tiqr_response", session, attrs...)
}

func mustJSON(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func truncateTrace(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= codedFlowTraceMaxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:codedFlowTraceMaxRunes]) + "…[truncated]"
}

func formatHTTPCurl(method, rawURL string, headers map[string]string, body string) string {
	var b strings.Builder
	b.WriteString("curl -sS -X ")
	b.WriteString(strings.ToUpper(strings.TrimSpace(method)))
	b.WriteString(" ")
	b.WriteString(shellQuote(rawURL))
	for key, value := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		b.WriteString(" \\\n  -H ")
		b.WriteString(shellQuote(key + ": " + value))
	}
	if body != "" {
		b.WriteString(" \\\n  --data-raw ")
		b.WriteString(shellQuote(body))
	}
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
