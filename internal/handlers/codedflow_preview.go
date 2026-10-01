package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

const (
	codedPreviewTTL         = 30 * time.Minute
	codedPreviewRedisPrefix = "coded-preview:"
	codedPreviewWaitKey     = "_coded_preview_wait"
	codedPreviewCTAKey      = "_coded_preview_cta"
	codedPreviewPhone       = "910000000000"
	codedPreviewInputNone   = ""
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
type codedPreviewSink struct {
	messages  []CodedPreviewMessage
	input     string
	flowCTA   string
	mock      bool
	mocks     map[string]any
	needsMock string
	session   *models.ChatbotSession
	aiCalls   []CodedPreviewAICall
	pendingAI []CodedPreviewAICall
}

// errCodedPreviewNeedsMock stops a preview turn so the client can supply JSON.
var errCodedPreviewNeedsMock = errors.New("preview needs mock")

func (s *codedPreviewSink) mockFor(operation string) (map[string]any, bool) {
	if s == nil {
		return nil, false
	}
	operation = strings.TrimSpace(operation)
	raw, ok := s.mocks[operation]
	if !ok || raw == nil {
		s.needsMock = operation
		return nil, false
	}
	payload, ok := raw.(map[string]any)
	if !ok {
		s.needsMock = operation
		return nil, false
	}
	return payload, true
}

func (ctx *chatNodeCtx) capturing() bool {
	return ctx != nil && ctx.preview != nil
}

func (s *codedPreviewSink) noteAI(call CodedPreviewAICall) {
	if s == nil {
		return
	}
	s.aiCalls = append(s.aiCalls, call)
	s.pendingAI = append(s.pendingAI, call)
}

func (s *codedPreviewSink) takePendingAI() []CodedPreviewAICall {
	if s == nil || len(s.pendingAI) == 0 {
		return nil
	}
	out := append([]CodedPreviewAICall(nil), s.pendingAI...)
	s.pendingAI = nil
	return out
}

func (s *codedPreviewSink) appendMessage(msg CodedPreviewMessage) {
	if s == nil {
		return
	}
	msg.Context = previewSessionContext(s.session)
	msg.AI = s.takePendingAI()
	s.messages = append(s.messages, msg)
}

func previewSessionContext(session *models.ChatbotSession) map[string]any {
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

func (s *codedPreviewSink) text(step, content string) {
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
func (s *codedPreviewSink) ctaURL(step, body, buttonText, url string) {
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

func (s *codedPreviewSink) expectText() {
	if s == nil {
		return
	}
	s.input = "text"
	s.flowCTA = ""
}

func (s *codedPreviewSink) expectLocation(step, body string) {
	if s == nil {
		return
	}
	s.appendMessage(CodedPreviewMessage{
		Type:        "bot",
		Content:     body,
		Step:        step,
		Interactive: "location_request",
	})
	s.input = "location"
	s.flowCTA = ""
}

func (s *codedPreviewSink) buttons(step, body, mode string, buttons []map[string]any, header, footer, listButton, headerImage string) {
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
	s.input = "button"
	s.flowCTA = ""
}

func (s *codedPreviewSink) carousel(step, body string, cards []map[string]any) {
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
	s.input = "button"
	s.flowCTA = ""
}

func (s *codedPreviewSink) flow(step, header, body, cta string) {
	if s == nil {
		return
	}
	s.appendMessage(CodedPreviewMessage{
		Type:    "bot",
		Content: body,
		Step:    step,
		Header:  header,
	})
	s.input = "whatsapp_flow"
	s.flowCTA = cta
}

// flushPendingAIDebug emits any AI calls that did not attach to an outbound
// message as standalone debug bubbles (for example a routed choice with no
// immediate Say).
func (s *codedPreviewSink) flushPendingAIDebug() {
	if s == nil || len(s.pendingAI) == 0 {
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
		s.messages = append(s.messages, CodedPreviewMessage{
			Type:    "debug",
			Content: content,
			Step:    asString(previewSessionContext(s.session)["_current_step"]),
			Context: previewSessionContext(s.session),
			AI:      []CodedPreviewAICall{call},
		})
	}
}

// deliverCodedText sends a chatbot line, or records it when this run is a preview.
func (a *App) deliverCodedText(ctx *chatNodeCtx, step, text string) error {
	if ctx.capturing() {
		ctx.preview.text(step, text)
		return nil
	}
	if err := a.sendAndSaveTextMessage(ctx.account, ctx.contact, text); err != nil {
		return err
	}
	a.logSessionMessage(ctx.session.ID, models.DirectionOutgoing, text, step)
	a.logCodedFlowWhatsApp(ctx, step, "text", text)
	return nil
}

// deliverCodedCTAURL sends a CTA URL button message, or records it in preview.
// Preview does not wait for a reply — the button opens a URL and is not a choice.
func (a *App) deliverCodedCTAURL(ctx *chatNodeCtx, step, body, buttonText, url string) error {
	if ctx.capturing() {
		ctx.preview.ctaURL(step, body, buttonText, url)
		return nil
	}
	if err := a.sendAndSaveCTAURLButton(ctx.account, ctx.contact, body, buttonText, url); err != nil {
		return err
	}
	a.logSessionMessage(ctx.session.ID, models.DirectionOutgoing, body, step)
	a.logCodedFlowWhatsApp(ctx, step, "cta_url", body)
	return nil
}

// deliverCodedLocationRequest asks for a WhatsApp location pin, or records it in preview.
func (a *App) deliverCodedLocationRequest(ctx *chatNodeCtx, step, body string) error {
	if ctx.capturing() {
		ctx.preview.expectLocation(step, body)
		return nil
	}
	if err := a.sendAndSaveLocationRequest(ctx.account, ctx.contact, body); err != nil {
		return err
	}
	a.logSessionMessage(ctx.session.ID, models.DirectionOutgoing, body, step)
	a.logCodedFlowWhatsApp(ctx, step, "location_request", body)
	return nil
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

type codedPreviewHold struct {
	mu          sync.Mutex
	orgID       uuid.UUID
	userID      uuid.UUID
	flowKey     string
	accountName string
	account     *models.WhatsAppAccount
	contact     *models.Contact
	session     *models.ChatbotSession
	updatedAt   time.Time
	mock        bool
}

var codedPreviewStore = struct {
	sync.Mutex
	items map[uuid.UUID]*codedPreviewHold
}{items: map[uuid.UUID]*codedPreviewHold{}}

type codedPreviewInput struct {
	SessionID    uuid.UUID
	Phone        string
	Text         string
	ButtonID     string
	FlowResponse map[string]any
	Mock         *bool
	Mocks        map[string]any
}

func (in codedPreviewInput) mockOn() bool {
	if in.Mock == nil {
		return true
	}
	return *in.Mock
}

func (a *App) previewCodedTurn(orgID, userID uuid.UUID, accountName, flowKey string, in codedPreviewInput) (CodedPreviewResponse, error) {
	flow := codedFlowByKey(flowKey)
	if flow == nil {
		return CodedPreviewResponse{}, errCodedFlowNotFound
	}
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return CodedPreviewResponse{}, errCodedPreviewAccount
	}

	hold, err := a.previewHold(orgID, userID, accountName, flow.Key, in)
	if err != nil {
		return CodedPreviewResponse{}, err
	}
	hold.mu.Lock()
	defer hold.mu.Unlock()

	phone := strings.TrimSpace(in.Phone)
	if phone != "" {
		hold.contact.PhoneNumber = phone
		hold.session.PhoneNumber = phone
	}

	sink := &codedPreviewSink{mock: hold.mock, mocks: in.Mocks, session: hold.session}
	runErr := a.runCodedFlowPreview(
		hold.account, hold.contact, hold.session, flow,
		in.Text, in.ButtonID, in.FlowResponse, sink,
	)
	if runErr != nil && !errors.Is(runErr, errCodedPreviewNeedsMock) {
		a.Log.Error("Coded flow preview failed", "error", runErr, "flow", flow.Key)
	}
	if errors.Is(runErr, errCodedPreviewNeedsMock) {
		runErr = nil
	}
	sink.flushPendingAIDebug()
	resp := previewResponse(hold, sink, runErr)
	if err := a.saveCodedPreview(hold); err != nil {
		a.Log.Error("Failed to store coded flow preview session", "error", err, "flow", flow.Key)
		return CodedPreviewResponse{}, err
	}
	return resp, nil
}

func previewResponse(hold *codedPreviewHold, sink *codedPreviewSink, runErr error) CodedPreviewResponse {
	session := hold.session
	status := "waiting_input"
	switch {
	case sink.needsMock != "":
		status = "needs_mock"
	case runErr != nil:
		status = "error"
	case session.Status == models.SessionStatusCompleted || codedFlowSessionKey(session) == "":
		status = "completed"
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	input := sink.input
	cta := sink.flowCTA
	if input != "" {
		session.SessionData[codedPreviewWaitKey] = input
		session.SessionData[codedPreviewCTAKey] = cta
	} else if status == "waiting_input" {
		input = asString(session.SessionData[codedPreviewWaitKey])
		cta = asString(session.SessionData[codedPreviewCTAKey])
	}
	if status != "waiting_input" && status != "needs_mock" {
		delete(session.SessionData, codedPreviewWaitKey)
		delete(session.SessionData, codedPreviewCTAKey)
		input = codedPreviewInputNone
		cta = ""
	}
	messages := sink.messages
	if messages == nil {
		messages = []CodedPreviewMessage{}
	}
	aiCalls := sink.aiCalls
	if aiCalls == nil {
		aiCalls = []CodedPreviewAICall{}
	}
	return CodedPreviewResponse{
		SessionID:     hold.session.ID.String(),
		Status:        status,
		Step:          session.CurrentStep,
		Input:         input,
		FlowCTA:       cta,
		MockOperation: sink.needsMock,
		Messages:      messages,
		Context:       previewSessionContext(session),
		AICalls:       aiCalls,
	}
}

func (a *App) previewHold(orgID, userID uuid.UUID, accountName, flowKey string, in codedPreviewInput) (*codedPreviewHold, error) {
	account, err := a.previewAccount(orgID, accountName)
	if err != nil {
		return nil, err
	}
	if in.SessionID != uuid.Nil {
		hold, err := a.loadCodedPreview(in.SessionID)
		if err != nil {
			a.Log.Error("Failed to load coded flow preview session", "error", err, "session_id", in.SessionID)
			return nil, err
		}
		if !previewHoldMatches(hold, orgID, userID, flowKey, accountName) {
			return nil, errCodedPreviewSession
		}
		hold.account = account
		return hold, nil
	}

	phone := strings.TrimSpace(in.Phone)
	if phone == "" {
		phone = codedPreviewPhone
	}
	now := time.Now()
	contact := &models.Contact{
		BaseModel:      models.BaseModel{ID: uuid.New()},
		OrganizationID: orgID,
		PhoneNumber:    phone,
		ProfileName:    "Preview",
	}
	session := &models.ChatbotSession{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  orgID,
		ContactID:       contact.ID,
		WhatsAppAccount: account.Name,
		PhoneNumber:     phone,
		Status:          models.SessionStatusActive,
		SessionData:     models.JSONB{},
		StartedAt:       now,
		LastActivityAt:  now,
	}
	return &codedPreviewHold{
		orgID:       orgID,
		userID:      userID,
		flowKey:     flowKey,
		accountName: account.Name,
		account:     account,
		contact:     contact,
		session:     session,
		updatedAt:   now,
		mock:        in.mockOn(),
	}, nil
}

func previewHoldMatches(hold *codedPreviewHold, orgID, userID uuid.UUID, flowKey, accountName string) bool {
	if hold == nil || hold.orgID != orgID || hold.userID != userID || hold.flowKey != flowKey {
		return false
	}
	name := hold.accountName
	if name == "" && hold.account != nil {
		name = hold.account.Name
	}
	return name == accountName
}

func (a *App) previewAccount(orgID uuid.UUID, accountName string) (*models.WhatsAppAccount, error) {
	var account models.WhatsAppAccount
	err := a.DB.Where("organization_id = ? AND name = ?", orgID, accountName).First(&account).Error
	if err != nil {
		return nil, errCodedPreviewAccount
	}
	return &account, nil
}

// Preview sessions stay in Redis when the API runs more than one replica.
// A process-local map only works for a single server, which is why preview
// continues locally and returns "preview session not found" in production.
func (a *App) loadCodedPreview(id uuid.UUID) (*codedPreviewHold, error) {
	if a.Redis != nil {
		raw, err := a.Redis.Get(context.Background(), codedPreviewRedisKey(id)).Result()
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var state codedPreviewState
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return nil, err
		}
		return state.hold(), nil
	}
	codedPreviewStore.Lock()
	defer codedPreviewStore.Unlock()
	sweepCodedPreviews(time.Now())
	return codedPreviewStore.items[id], nil
}

func (a *App) saveCodedPreview(hold *codedPreviewHold) error {
	if hold == nil || hold.session == nil {
		return errCodedPreviewSession
	}
	hold.updatedAt = time.Now()
	if a.Redis == nil {
		codedPreviewStore.Lock()
		defer codedPreviewStore.Unlock()
		codedPreviewStore.items[hold.session.ID] = hold
		return nil
	}
	data, err := json.Marshal(codedPreviewStateFrom(hold))
	if err != nil {
		return err
	}
	return a.Redis.Set(context.Background(), codedPreviewRedisKey(hold.session.ID), data, codedPreviewTTL).Err()
}

func codedPreviewRedisKey(id uuid.UUID) string {
	return codedPreviewRedisPrefix + id.String()
}

type codedPreviewState struct {
	OrgID       uuid.UUID           `json:"org_id"`
	UserID      uuid.UUID           `json:"user_id"`
	FlowKey     string              `json:"flow_key"`
	AccountName string              `json:"account_name"`
	Mock        bool                `json:"mock"`
	Contact     codedPreviewContact `json:"contact"`
	Session     codedPreviewSession `json:"session"`
}

type codedPreviewContact struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	PhoneNumber    string    `json:"phone_number"`
	ProfileName    string    `json:"profile_name"`
}

type codedPreviewSession struct {
	ID              uuid.UUID            `json:"id"`
	OrganizationID  uuid.UUID            `json:"organization_id"`
	ContactID       uuid.UUID            `json:"contact_id"`
	WhatsAppAccount string               `json:"whatsapp_account"`
	PhoneNumber     string               `json:"phone_number"`
	Status          models.SessionStatus `json:"status"`
	CurrentStep     string               `json:"current_step"`
	StepRetries     int                  `json:"step_retries"`
	SessionData     models.JSONB         `json:"session_data"`
	StartedAt       time.Time            `json:"started_at"`
	LastActivityAt  time.Time            `json:"last_activity_at"`
	CompletedAt     *time.Time           `json:"completed_at,omitempty"`
}

func codedPreviewStateFrom(hold *codedPreviewHold) codedPreviewState {
	contact := codedPreviewContact{}
	if hold.contact != nil {
		contact = codedPreviewContact{
			ID:             hold.contact.ID,
			OrganizationID: hold.contact.OrganizationID,
			PhoneNumber:    hold.contact.PhoneNumber,
			ProfileName:    hold.contact.ProfileName,
		}
	}
	session := codedPreviewSession{}
	if hold.session != nil {
		session = codedPreviewSession{
			ID:              hold.session.ID,
			OrganizationID:  hold.session.OrganizationID,
			ContactID:       hold.session.ContactID,
			WhatsAppAccount: hold.session.WhatsAppAccount,
			PhoneNumber:     hold.session.PhoneNumber,
			Status:          hold.session.Status,
			CurrentStep:     hold.session.CurrentStep,
			StepRetries:     hold.session.StepRetries,
			SessionData:     hold.session.SessionData,
			StartedAt:       hold.session.StartedAt,
			LastActivityAt:  hold.session.LastActivityAt,
			CompletedAt:     hold.session.CompletedAt,
		}
	}
	name := hold.accountName
	if name == "" && hold.account != nil {
		name = hold.account.Name
	}
	return codedPreviewState{
		OrgID:       hold.orgID,
		UserID:      hold.userID,
		FlowKey:     hold.flowKey,
		AccountName: name,
		Mock:        hold.mock,
		Contact:     contact,
		Session:     session,
	}
}

func (state codedPreviewState) hold() *codedPreviewHold {
	contact := &models.Contact{
		BaseModel:      models.BaseModel{ID: state.Contact.ID},
		OrganizationID: state.Contact.OrganizationID,
		PhoneNumber:    state.Contact.PhoneNumber,
		ProfileName:    state.Contact.ProfileName,
	}
	session := &models.ChatbotSession{
		BaseModel:       models.BaseModel{ID: state.Session.ID},
		OrganizationID:  state.Session.OrganizationID,
		ContactID:       state.Session.ContactID,
		WhatsAppAccount: state.Session.WhatsAppAccount,
		PhoneNumber:     state.Session.PhoneNumber,
		Status:          state.Session.Status,
		CurrentStep:     state.Session.CurrentStep,
		StepRetries:     state.Session.StepRetries,
		SessionData:     state.Session.SessionData,
		StartedAt:       state.Session.StartedAt,
		LastActivityAt:  state.Session.LastActivityAt,
		CompletedAt:     state.Session.CompletedAt,
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	return &codedPreviewHold{
		orgID:       state.OrgID,
		userID:      state.UserID,
		flowKey:     state.FlowKey,
		accountName: state.AccountName,
		contact:     contact,
		session:     session,
		mock:        state.Mock,
	}
}

func sweepCodedPreviews(now time.Time) {
	for id, hold := range codedPreviewStore.items {
		if now.Sub(hold.updatedAt) > codedPreviewTTL {
			delete(codedPreviewStore.items, id)
		}
	}
}

var (
	errCodedFlowNotFound   = errors.New("coded flow not found")
	errCodedPreviewAccount = errors.New("whatsapp account not found")
	errCodedPreviewSession = errors.New("preview session not found")
)

// PreviewCodedFlow runs one turn of a compiled-in flow without sending
// WhatsApp messages, creating a transfer, or placing an order.
func (a *App) PreviewCodedFlow(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}
	if !a.HasPermission(userID, models.ResourceFlowsChatbot, models.ActionRead, orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Permission denied", nil, "")
	}

	key, _ := r.RequestCtx.UserValue("key").(string)
	key = strings.TrimSpace(key)

	var req struct {
		Account      string         `json:"account"`
		SessionID    string         `json:"session_id"`
		Phone        string         `json:"phone"`
		Text         string         `json:"text"`
		ButtonID     string         `json:"button_id"`
		FlowResponse map[string]any `json:"flow_response"`
		Mock         *bool          `json:"mock"`
		Mocks        map[string]any `json:"mocks"`
	}
	if len(r.RequestCtx.PostBody()) > 0 {
		if err := json.Unmarshal(r.RequestCtx.PostBody(), &req); err != nil {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid request body", nil, "")
		}
	}
	accountName := strings.TrimSpace(req.Account)
	if accountName == "" {
		accountName = strings.TrimSpace(string(r.RequestCtx.QueryArgs().Peek("account")))
	}
	if accountName == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "WhatsApp account is required", nil, "")
	}

	in := codedPreviewInput{
		Phone:        req.Phone,
		Text:         req.Text,
		ButtonID:     req.ButtonID,
		FlowResponse: req.FlowResponse,
		Mock:         req.Mock,
		Mocks:        req.Mocks,
	}
	if strings.TrimSpace(req.SessionID) != "" {
		id, err := uuid.Parse(strings.TrimSpace(req.SessionID))
		if err != nil {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid preview session", nil, "")
		}
		in.SessionID = id
	}

	resp, err := a.previewCodedTurn(orgID, userID, accountName, key, in)
	switch err {
	case nil:
		return r.SendEnvelope(resp)
	case errCodedFlowNotFound:
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Coded flow not found", nil, "")
	case errCodedPreviewAccount:
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "WhatsApp account not found", nil, "")
	case errCodedPreviewSession:
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Preview session not found", nil, "")
	default:
		a.Log.Error("Coded flow preview failed", "error", err, "flow", key)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to preview coded flow", nil, "")
	}
}
