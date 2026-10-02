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
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
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

var (
	errCodedFlowNotFound   = errors.New("coded flow not found")
	errCodedPreviewAccount = errors.New("whatsapp account not found")
	errCodedPreviewSession = errors.New("preview session not found")
)

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
	Mock        bool
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

func (a *App) previewCodedTurn(orgID, userID uuid.UUID, accountName, flowKey string, in codedPreviewInput) (codedflow.CodedPreviewResponse, error) {
	flow := codedflow.ByKey(flowKey)
	if flow == nil {
		return codedflow.CodedPreviewResponse{}, errCodedFlowNotFound
	}
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return codedflow.CodedPreviewResponse{}, errCodedPreviewAccount
	}

	hold, err := a.previewHold(orgID, userID, accountName, flow.Key, in)
	if err != nil {
		return codedflow.CodedPreviewResponse{}, err
	}
	hold.mu.Lock()
	defer hold.mu.Unlock()

	phone := strings.TrimSpace(in.Phone)
	if phone != "" {
		hold.contact.PhoneNumber = phone
		hold.session.PhoneNumber = phone
	}

	sink := &codedflow.PreviewSink{Mock: hold.Mock, Mocks: in.Mocks, Session: hold.session}
	runErr := a.runCodedFlowPreview(
		hold.account, hold.contact, hold.session, flow,
		in.Text, in.ButtonID, in.FlowResponse, sink,
	)
	if runErr != nil && !errors.Is(runErr, codedflow.ErrPreviewNeedsMock) {
		a.Log.Error("Coded flow preview failed", "error", runErr, "flow", flow.Key)
	}
	if errors.Is(runErr, codedflow.ErrPreviewNeedsMock) {
		runErr = nil
	}
	sink.FlushPendingAIDebug()
	resp := previewResponse(hold, sink, runErr)
	if err := a.saveCodedPreview(hold); err != nil {
		a.Log.Error("Failed to store coded flow preview session", "error", err, "flow", flow.Key)
		return codedflow.CodedPreviewResponse{}, err
	}
	return resp, nil
}

func previewResponse(hold *codedPreviewHold, sink *codedflow.PreviewSink, runErr error) codedflow.CodedPreviewResponse {
	session := hold.session
	status := "waiting_input"
	switch {
	case sink.NeedsMock != "":
		status = "needs_mock"
	case runErr != nil:
		status = "error"
	case session.Status == models.SessionStatusCompleted || codedflow.SessionKey(session) == "":
		status = "completed"
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	input := sink.Input
	cta := sink.FlowCTA
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
	messages := sink.Messages
	if messages == nil {
		messages = []codedflow.CodedPreviewMessage{}
	}
	aiCalls := sink.AICalls
	if aiCalls == nil {
		aiCalls = []codedflow.CodedPreviewAICall{}
	}
	apiCalls := sink.APICalls
	if apiCalls == nil {
		apiCalls = []codedflow.CodedPreviewAPICall{}
	}
	return codedflow.CodedPreviewResponse{
		SessionID:     hold.session.ID.String(),
		Status:        status,
		Step:          session.CurrentStep,
		Input:         input,
		FlowCTA:       cta,
		MockOperation: sink.NeedsMock,
		Messages:      messages,
		Context:       codedflow.PreviewSessionContext(session),
		AICalls:       aiCalls,
		APICalls:      apiCalls,
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
		Mock:        in.mockOn(),
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
		Mock: hold.Mock,
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
		Mock:        state.Mock,
	}
}

func sweepCodedPreviews(now time.Time) {
	for id, hold := range codedPreviewStore.items {
		if now.Sub(hold.updatedAt) > codedPreviewTTL {
			delete(codedPreviewStore.items, id)
		}
	}
}

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
