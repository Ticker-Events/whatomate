package codedflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	// SessionStoreInfoKey is the session_data key for loaded store information.
	SessionStoreInfoKey = "store_info"

	storeInfoFieldMaxRunes   = 4000
	StoreInfoAnswerThreshold = 0.9
	codedFlowRoleStoreInfo   = "store_info"
)

var storeInfoFieldKeys = []string{
	"about_us",
	"privacy_policy",
	"terms_and_conditions",
	"refund_policy",
	"shipping_policy",
}

// StoreInfoAnswer is the model response for a grounded store-information reply.
type StoreInfoAnswer struct {
	Confident  bool    `json:"confident"`
	Confidence float64 `json:"confidence"`
	Answer     string  `json:"answer"`
}

// AnswerFromStoreInfoAI is swappable in tests.
var AnswerFromStoreInfoAI = defaultAnswerFromStoreInfoAI

func defaultAnswerFromStoreInfoAI(a Host, session *models.ChatbotSession, prompt string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("ai is not configured")
	}
	return a.CompleteCodedRoleText(session, codedFlowRoleStoreInfo, prompt)
}

// LoadStoreInfo loads store information into the session once.
// An empty payload or a failed fetch does not stop the flow: the call is still
// recorded as ok so later turns do not retry.
func (c *Conv) LoadStoreInfo(name, operation string) map[string]any {
	if c.Stop {
		return nil
	}
	if name == "" {
		name = SessionStoreInfoKey
	}
	if _, done := c.doneCall(); done {
		payload, _ := asStringMap(c.session().SessionData[name])
		return payload
	}
	ok, err := c.tiqr(name, operation, "rest", nil)
	if c.waitingForPreviewMock() {
		return nil
	}
	payload := map[string]any{}
	switch {
	case err != nil:
		if c.app != nil {
			c.app.LogWarn("coded flow store info load failed", "error", err)
		}
	case !ok:
		if c.app != nil {
			c.app.LogWarn("coded flow store info load returned non-2xx")
		}
	case c.chat.LastTiqr() != nil:
		payload = normalizeStoreInfoPayload(c.chat.LastTiqr())
	}
	c.session().SessionData[name] = payload
	c.appendCall(map[string]any{"name": name, "ok": true, "var": name, "value": payload})
	return payload
}

func normalizeStoreInfoPayload(raw map[string]any) map[string]any {
	out := map[string]any{}
	if raw == nil {
		return out
	}
	for _, key := range storeInfoFieldKeys {
		value := truncateRunes(strings.TrimSpace(asString(raw[key])), storeInfoFieldMaxRunes)
		if value != "" {
			out[key] = value
		}
	}
	return out
}

func storeInfoFactsFromSession(data models.JSONB) map[string]string {
	out := map[string]string{}
	if data == nil {
		return out
	}
	raw, _ := asStringMap(data[SessionStoreInfoKey])
	if raw == nil {
		return out
	}
	for _, key := range storeInfoFieldKeys {
		value := strings.TrimSpace(asString(raw[key]))
		if value != "" {
			out[key] = value
		}
	}
	return out
}

func hasStoreInfoFacts(facts map[string]string) bool {
	for _, value := range facts {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func buildStoreInfoAnswerPrompt(message string, facts map[string]string) string {
	var b strings.Builder
	b.WriteString(`Answer one customer question using ONLY the store information fields below.

Rules:
- Use only facts present in the fields. Do not invent products, prices, hours, policies, or services.
- Set confident=true only when the fields fully support a clear answer.
- confidence is from 0 to 1. Use 0.9 or higher only when the answer is clearly supported.
- answer must be short plain WhatsApp text (no markdown, no bullet markers).
- When unsure, set confident=false, confidence below 0.9, and answer to "".

Reply with JSON only:
{"confident":false,"confidence":0,"answer":""}

Store information:
`)
	for _, key := range storeInfoFieldKeys {
		value := strings.TrimSpace(facts[key])
		if value == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteByte('\n')
	}
	b.WriteString("\nCustomer question:\n")
	b.WriteString(strings.TrimSpace(message))
	return b.String()
}

func parseStoreInfoAnswer(raw string) (StoreInfoAnswer, error) {
	raw = strings.TrimSpace(raw)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var out StoreInfoAnswer
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return StoreInfoAnswer{}, err
	}
	out.Answer = strings.TrimSpace(out.Answer)
	return out, nil
}

func acceptedStoreInfoAnswer(result StoreInfoAnswer) bool {
	return result.Confident &&
		result.Confidence >= StoreInfoAnswerThreshold &&
		strings.TrimSpace(result.Answer) != ""
}

// TryStoreInfoAnswer returns a grounded answer from loaded store information.
// Person requests and empty store info skip the model. Preview/archive AI is noted.
func (c *Conv) TryStoreInfoAnswer(message string) (string, bool) {
	message = strings.TrimSpace(message)
	if c == nil || message == "" || WantsPerson(message) {
		return "", false
	}
	facts := storeInfoFactsFromSession(c.session().SessionData)
	if !hasStoreInfoFacts(facts) {
		return "", false
	}
	prompt := buildStoreInfoAnswerPrompt(message, facts)
	raw, err := AnswerFromStoreInfoAI(c.app, c.session(), prompt)
	call := CodedPreviewAICall{
		Role:   codedFlowRoleStoreInfo,
		Prompt: prompt,
	}
	if err != nil {
		call.Error = err.Error()
		c.notePreviewAI(call)
		if c.app != nil {
			c.app.LogCodedFlowAI(c.session(), codedFlowRoleStoreInfo, prompt, "", err.Error())
		}
		return "", false
	}
	result, parseErr := parseStoreInfoAnswer(raw)
	call.Response = strings.TrimSpace(raw)
	call.Confidence = result.Confidence
	if parseErr != nil {
		call.Error = parseErr.Error()
		c.notePreviewAI(call)
		if c.app != nil {
			c.app.LogCodedFlowAI(c.session(), codedFlowRoleStoreInfo, prompt, call.Response, parseErr.Error())
		}
		return "", false
	}
	grounded := acceptedStoreInfoAnswer(result)
	call.Grounded = &grounded
	call.Parsed = map[string]any{
		"confident":  result.Confident,
		"confidence": result.Confidence,
		"answer":     result.Answer,
	}
	c.notePreviewAI(call)
	if c.app != nil {
		c.app.LogCodedFlowAI(c.session(), codedFlowRoleStoreInfo, prompt, call.Response, "",
			"confident", result.Confident,
			"confidence", result.Confidence,
			"grounded", grounded,
		)
	}
	if !grounded {
		return "", false
	}
	return result.Answer, true
}

// deliverStoreInfoAnswer sends a grounded store-info reply and stays on the step.
func (c *Conv) deliverStoreInfoAnswer(step, answer string) (Route, bool) {
	text := c.text(answer)
	if err := c.app.DeliverCodedText(c.chat, step, text); err != nil {
		c.fail(err)
		return Route{}, false
	}
	c.Stop = true
	return Route{}, false
}
