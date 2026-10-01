package codedflow

import (
	"errors"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	codedPreviewWaitKey = "_coded_preview_wait"
	codedPreviewCTAKey  = "_coded_preview_cta"
)

// CodedPreviewButton is one reply, list row, or carousel action.
type CodedPreviewButton struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
	URL         string `json:"url,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
}

// CodedPreviewCard is one carousel card, in the shape the phone preview renders.
type CodedPreviewCard struct {
	MediaType string               `json:"media_type"`
	MediaURL  string               `json:"media_url"`
	Body      string               `json:"body,omitempty"`
	Buttons   []CodedPreviewButton `json:"buttons"`
}

// CodedPreviewMessage is one outbound bubble produced by a preview turn.
type CodedPreviewMessage struct {
	Type        string               `json:"type"`
	Content     string               `json:"content"`
	Step        string               `json:"step,omitempty"`
	Interactive string               `json:"interactive,omitempty"`
	Buttons     []CodedPreviewButton `json:"buttons,omitempty"`
	Cards       []CodedPreviewCard   `json:"cards,omitempty"`
	Header      string               `json:"header,omitempty"`
	HeaderImage string               `json:"header_image,omitempty"`
	Footer      string               `json:"footer,omitempty"`
	ListButton  string               `json:"list_button,omitempty"`
	Context     map[string]any       `json:"context,omitempty"`
	AI          []CodedPreviewAICall `json:"ai,omitempty"`
}

// CodedPreviewAICall is one intent, guide, or translate call made during a preview turn.
type CodedPreviewAICall struct {
	Role       string         `json:"role"`
	Prompt     string         `json:"prompt,omitempty"`
	Response   string         `json:"response,omitempty"`
	Parsed     map[string]any `json:"parsed,omitempty"`
	Error      string         `json:"error,omitempty"`
	Language   string         `json:"language,omitempty"`
	Route      string         `json:"route,omitempty"`
	Confidence float64        `json:"confidence,omitempty"`
	Grounded   *bool          `json:"grounded,omitempty"`
	Reasoning  string         `json:"reasoning,omitempty"`
}

// CodedPreviewResponse is one turn of a coded-flow preview.
// Status needs_mock means the next TiQR call is waiting for JSON.
type CodedPreviewResponse struct {
	SessionID     string                `json:"session_id"`
	Status        string                `json:"status"`
	Step          string                `json:"step"`
	Input         string                `json:"input"`
	FlowCTA       string                `json:"flow_cta,omitempty"`
	MockOperation string                `json:"mock_operation,omitempty"`
	Messages      []CodedPreviewMessage `json:"messages"`
	Context       map[string]any        `json:"context,omitempty"`
	AICalls       []CodedPreviewAICall  `json:"ai_calls,omitempty"`
}

// codedPreviewSink collects messages a coded flow would have sent.
// mock is true when TiQR calls must use supplied JSON instead of the store.
type PreviewSink struct {
	Messages  []CodedPreviewMessage
	Input     string
	FlowCTA   string
	Mock      bool
	Mocks     map[string]any
	NeedsMock string
	Session   *models.ChatbotSession
	AICalls   []CodedPreviewAICall
	PendingAI []CodedPreviewAICall
}

// ErrPreviewNeedsMock stops a preview turn so the client can supply JSON.
var ErrPreviewNeedsMock = errors.New("preview needs mock")

func (s *PreviewSink) MockFor(operation string) (map[string]any, bool) {
	if s == nil {
		return nil, false
	}
	operation = strings.TrimSpace(operation)
	raw, ok := s.Mocks[operation]
	if !ok || raw == nil {
		s.NeedsMock = operation
		return nil, false
	}
	payload, ok := raw.(map[string]any)
	if !ok {
		s.NeedsMock = operation
		return nil, false
	}
	return payload, true
}

func (s *PreviewSink) NoteAI(call CodedPreviewAICall) {
	if s == nil {
		return
	}
	s.AICalls = append(s.AICalls, call)
	s.PendingAI = append(s.PendingAI, call)
}

func (s *PreviewSink) takePendingAI() []CodedPreviewAICall {
	if s == nil || len(s.PendingAI) == 0 {
		return nil
	}
	out := append([]CodedPreviewAICall(nil), s.PendingAI...)
	s.PendingAI = nil
	return out
}

func (s *PreviewSink) appendMessage(msg CodedPreviewMessage) {
	if s == nil {
		return
	}
	msg.Context = PreviewSessionContext(s.Session)
	msg.AI = s.takePendingAI()
	s.Messages = append(s.Messages, msg)
}

func PreviewSessionContext(session *models.ChatbotSession) map[string]any {
	if session == nil || session.SessionData == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(session.SessionData)+2)
	for key, value := range session.SessionData {
		switch key {
		case codedPreviewWaitKey, codedPreviewCTAKey:
			continue
		case codedCallsKey:
			items, _ := anySlice(value)
			out[key] = map[string]any{
				"count": len(items),
				"names": codedCallNames(items),
			}
			continue
		case codedTranslationsKey:
			switch typed := value.(type) {
			case map[string]string:
				out[key] = map[string]any{"count": len(typed)}
			case map[string]any:
				out[key] = map[string]any{"count": len(typed)}
			default:
				out[key] = map[string]any{"count": 0}
			}
			continue
		}
		out[key] = value
	}
	if step := strings.TrimSpace(session.CurrentStep); step != "" {
		out["_current_step"] = step
	}
	out["_session_status"] = string(session.Status)
	return out
}

func codedCallNames(items []any) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		rec, ok := asStringMap(item)
		if !ok {
			continue
		}
		if name := asString(rec["name"]); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func (s *PreviewSink) Text(step, content string) {
	if s == nil || strings.TrimSpace(content) == "" {
		return
	}
	s.appendMessage(CodedPreviewMessage{
		Type:    "bot",
		Content: content,
		Step:    step,
	})
}

// ctaURL records a Pay now-style URL button. It does not wait for a reply.
func (s *PreviewSink) CTAURL(step, body, buttonText, url string) {
	if s == nil {
		return
	}
	s.appendMessage(CodedPreviewMessage{
		Type:        "bot",
		Content:     body,
		Step:        step,
		Interactive: "cta_url",
		Buttons: []CodedPreviewButton{{
			ID:    "pay_now",
			Title: buttonText,
			Type:  "url",
			URL:   url,
		}},
	})
}

func (s *PreviewSink) ExpectText() {
	if s == nil {
		return
	}
	s.Input = "text"
	s.FlowCTA = ""
}

func (s *PreviewSink) ExpectLocation(step, body string) {
	if s == nil {
		return
	}
	s.appendMessage(CodedPreviewMessage{
		Type:        "bot",
		Content:     body,
		Step:        step,
		Interactive: "location_request",
	})
	s.Input = "location"
	s.FlowCTA = ""
}

func (s *PreviewSink) Buttons(step, body, mode string, buttons []map[string]any, header, footer, listButton, headerImage string) {
	if s == nil {
		return
	}
	s.appendMessage(CodedPreviewMessage{
		Type:        "bot",
		Content:     body,
		Step:        step,
		Interactive: mode,
		Buttons:     previewButtons(buttons),
		Header:      header,
		HeaderImage: headerImage,
		Footer:      footer,
		ListButton:  listButton,
	})
	s.Input = "button"
	s.FlowCTA = ""
}

func (s *PreviewSink) Carousel(step, body string, cards []map[string]any) {
	if s == nil {
		return
	}
	rendered, flat := previewCards(cards)
	s.appendMessage(CodedPreviewMessage{
		Type:        "bot",
		Content:     body,
		Step:        step,
		Interactive: "carousel",
		Buttons:     flat,
		Cards:       rendered,
	})
	s.Input = "button"
	s.FlowCTA = ""
}

func (s *PreviewSink) Flow(step, header, body, cta string) {
	if s == nil {
		return
	}
	s.appendMessage(CodedPreviewMessage{
		Type:    "bot",
		Content: body,
		Step:    step,
		Header:  header,
	})
	s.Input = "whatsapp_flow"
	s.FlowCTA = cta
}

// flushPendingAIDebug emits any AI calls that did not attach to an outbound
// message as standalone debug bubbles (for example a routed choice with no
// immediate Say).
func (s *PreviewSink) FlushPendingAIDebug() {
	if s == nil || len(s.PendingAI) == 0 {
		return
	}
	for _, call := range s.takePendingAI() {
		content := "AI " + call.Role
		if call.Route != "" {
			content += " → " + call.Route
		}
		if call.Error != "" {
			content += ": " + call.Error
		}
		s.Messages = append(s.Messages, CodedPreviewMessage{
			Type:    "debug",
			Content: content,
			Step:    asString(PreviewSessionContext(s.Session)["_current_step"]),
			Context: PreviewSessionContext(s.Session),
			AI:      []CodedPreviewAICall{call},
		})
	}
}

func previewButtons(buttons []map[string]any) []CodedPreviewButton {
	out := make([]CodedPreviewButton, 0, len(buttons))
	for _, btn := range buttons {
		title := fieldString(btn, "title")
		if title == "" {
			continue
		}
		out = append(out, CodedPreviewButton{
			ID:          fieldString(btn, "id"),
			Title:       title,
			Description: fieldString(btn, "description"),
			Type:        fieldString(btn, "type"),
			URL:         fieldString(btn, "url"),
			PhoneNumber: fieldString(btn, "phone_number"),
		})
	}
	return out
}

func previewCards(cards []map[string]any) ([]CodedPreviewCard, []CodedPreviewButton) {
	out := make([]CodedPreviewCard, 0, len(cards))
	var flat []CodedPreviewButton
	for _, card := range cards {
		buttons := []CodedPreviewButton{{
			ID:    fieldString(card, "id"),
			Title: fieldString(card, "title"),
			Type:  fieldString(card, "type"),
			URL:   fieldString(card, "url"),
		}}
		if title := fieldString(card, "title_2"); title != "" {
			buttons = append(buttons, CodedPreviewButton{
				ID:    fieldString(card, "id_2"),
				Title: title,
				Type:  "reply",
			})
		}
		mediaType := fieldString(card, "media_type")
		if mediaType == "" {
			mediaType = "image"
		}
		out = append(out, CodedPreviewCard{
			MediaType: mediaType,
			MediaURL:  fieldString(card, "media_url"),
			Body:      fieldString(card, "body"),
			Buttons:   buttons,
		})
		flat = append(flat, buttons...)
	}
	return out, flat
}
