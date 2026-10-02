package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/tickermcp"
)

// codedChat adapts *chatNodeCtx to codedflow.Chat.
type codedChat struct {
	ctx *chatNodeCtx
}

func newCodedChat(ctx *chatNodeCtx) codedflow.Chat {
	if ctx == nil {
		ctx = &chatNodeCtx{}
	}
	return &codedChat{ctx: ctx}
}

func (c *codedChat) Session() *models.ChatbotSession { return c.ctx.session }
func (c *codedChat) Account() *models.WhatsAppAccount {
	return c.ctx.account
}
func (c *codedChat) Contact() *models.Contact { return c.ctx.contact }

func (c *codedChat) UserInput() string                  { return c.ctx.userInput }
func (c *codedChat) SetUserInput(v string)              { c.ctx.userInput = v }
func (c *codedChat) ButtonID() string                   { return c.ctx.buttonID }
func (c *codedChat) SetButtonID(v string)               { c.ctx.buttonID = v }
func (c *codedChat) FlowResponseData() map[string]any   { return c.ctx.flowResponseData }
func (c *codedChat) SetFlowResponseData(v map[string]any) {
	c.ctx.flowResponseData = v
}

func (c *codedChat) Consumed() bool     { return c.ctx.consumed }
func (c *codedChat) SetConsumed(v bool) { c.ctx.consumed = v }
func (c *codedChat) Capturing() bool {
	return c.ctx != nil && c.ctx.preview != nil
}

func (c *codedChat) LastTiqr() map[string]any     { return c.ctx.lastTiqr }
func (c *codedChat) SetLastTiqr(v map[string]any) { c.ctx.lastTiqr = v }
func (c *codedChat) LastTiqrErr() string          { return c.ctx.lastTiqrErr }
func (c *codedChat) SetLastTiqrErr(v string)      { c.ctx.lastTiqrErr = v }
func (c *codedChat) LastTiqrStatus() int          { return c.ctx.lastTiqrStatus }
func (c *codedChat) SetLastTiqrStatus(v int)      { c.ctx.lastTiqrStatus = v }

func (c *codedChat) Preview() *codedflow.PreviewSink { return c.ctx.preview }
func (c *codedChat) SetPreview(p *codedflow.PreviewSink) {
	c.ctx.preview = p
}

func (c *codedChat) Native() any { return c.ctx }

func unwrapChat(chat codedflow.Chat) *chatNodeCtx {
	if chat == nil {
		return nil
	}
	if c, ok := chat.(*codedChat); ok {
		return c.ctx
	}
	if n, ok := chat.Native().(*chatNodeCtx); ok {
		return n
	}
	return nil
}

// --- Host logging ---

func (a *App) LogInfo(msg string, args ...any)  { a.Log.Info(msg, args...) }
func (a *App) LogWarn(msg string, args ...any)  { a.Log.Warn(msg, args...) }
func (a *App) LogError(msg string, args ...any) { a.Log.Error(msg, args...) }

func (a *App) httpClient() *http.Client {
	if a != nil && a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}

// --- Node execution ---

func (a *App) ExecChatMessage(chat codedflow.Chat, id string, config map[string]any) (codedflow.NodeResult, error) {
	out, err := a.execChatMessage(&ChatNode{ID: id, Type: ChatNodeMessage, Config: config}, unwrapChat(chat))
	return nodeResult(out), err
}

func (a *App) ExecChatButtons(chat codedflow.Chat, id string, config map[string]any) (codedflow.NodeResult, error) {
	out, err := a.execChatButtons(&ChatNode{ID: id, Type: ChatNodeButtons, Config: config}, unwrapChat(chat))
	return nodeResult(out), err
}

func (a *App) ExecChatWhatsAppFlow(chat codedflow.Chat, id string, config map[string]any) (codedflow.NodeResult, error) {
	out, err := a.execChatWhatsAppFlow(&ChatNode{ID: id, Type: ChatNodeWhatsAppFlow, Config: config}, unwrapChat(chat))
	return nodeResult(out), err
}

func (a *App) ExecChatTiqrStoreAPI(chat codedflow.Chat, id string, config map[string]any) (codedflow.NodeResult, error) {
	out, err := a.execChatTiqrStoreAPI(&ChatNode{ID: id, Type: ChatNodeTiqrStoreAPI, Config: config}, unwrapChat(chat))
	return nodeResult(out), err
}

func (a *App) ExecChatTransfer(chat codedflow.Chat, id string, config map[string]any) (codedflow.NodeResult, error) {
	out, err := a.execChatTransfer(&ChatNode{ID: id, Type: ChatNodeTransfer, Config: config}, unwrapChat(chat))
	return nodeResult(out), err
}

func nodeResult(out nodeOutcome) codedflow.NodeResult {
	return codedflow.NodeResult{Outcome: out.outcome, Yield: out.yield}
}

func (a *App) DeliverCodedCTAURL(chat codedflow.Chat, step, body, buttonText, url string) error {
	return a.deliverCodedCTAURL(unwrapChat(chat), step, body, buttonText, url)
}

func (a *App) DeliverCodedLocationRequest(chat codedflow.Chat, step, body string) error {
	return a.deliverCodedLocationRequest(unwrapChat(chat), step, body)
}

func (a *App) DeliverCodedText(chat codedflow.Chat, step, text string) error {
	return a.deliverCodedText(unwrapChat(chat), step, text)
}

func (a *App) PersistChatSession(session *models.ChatbotSession) error {
	return a.persistChatSession(session)
}

func (a *App) PersistSessionData(session *models.ChatbotSession) error {
	return a.persistSessionData(session)
}

func (a *App) ButtonsForNode(cfg map[string]any, data models.JSONB) ([]map[string]any, error) {
	return buttonsForNode(cfg, data)
}

func (a *App) ApplyButtonSelection(cfg map[string]any, data models.JSONB, buttonID, title string) models.JSONB {
	return applyButtonSelection(cfg, data, buttonID, title)
}

func (a *App) PaymentCTAContent(order map[string]any) (string, string) {
	return paymentCTAContent(order)
}

func (a *App) CompactOrderCreateResult(order map[string]any, currency string) map[string]any {
	return compactOrderCreateResult(order, currency)
}

func (a *App) SessionCurrencyCode(session *models.ChatbotSession) string {
	return sessionCurrencyCode(session)
}

func (a *App) StashSessionCurrency(session *models.ChatbotSession, currency string) {
	stashSessionCurrency(session, currency)
}

func (a *App) GetChatbotSettingsCached(orgID uuid.UUID, accountName string) (*models.ChatbotSettings, error) {
	return a.getChatbotSettingsCached(orgID, accountName)
}

func (a *App) IsWithinBusinessHours(hours models.JSONBArray) bool {
	return a.isWithinBusinessHours(hours)
}

func (a *App) SendAndSaveTextMessage(account *models.WhatsAppAccount, contact *models.Contact, text string) error {
	return a.sendAndSaveTextMessage(account, contact, text)
}

func (a *App) EnsureCommerceDraft(contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings) (*models.CommerceDraft, error) {
	return a.ensureCommerceDraft(contact, session, settings)
}

func (a *App) SyncReferencedCommerceDraft(session *models.ChatbotSession) error {
	return a.syncReferencedCommerceDraft(session)
}

func (a *App) CreateCommerceTransfer(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, category tickermcp.Category) (*models.AgentTransfer, bool, error) {
	return a.createCommerceTransfer(account, contact, session, settings, category)
}

func (a *App) StageCommerceHandoffSessionData(session *models.ChatbotSession, category tickermcp.Category) {
	a.stageCommerceHandoffSessionData(session, category)
}

func (a *App) CompleteCommerceCaptureWithCategory(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, collection map[string]any) bool {
	st := earlyHandoffCheckoutStateFromSession(session)
	category := categoryFromCollectionMap(collection)
	return a.completeCommerceCaptureWithCategory(account, contact, session, settings, st, category)
}

func (a *App) StageEarlyHandoffCheckout(session *models.ChatbotSession) {
	st := earlyHandoffCheckoutStateFromSession(session)
	setCheckoutState(session, st)
}

func (a *App) SetSelectedCategoryID(session *models.ChatbotSession, categoryID string) {
	setSelectedCategoryID(session, categoryID)
}

func (a *App) SelectedCategoryID(session *models.ChatbotSession) string {
	return selectedCategoryID(session)
}

func (a *App) CategoryFromCollectionMap(raw map[string]any) tickermcp.Category {
	return categoryFromCollectionMap(raw)
}

func (a *App) JSONMapFromSession(session *models.ChatbotSession, key string) models.JSONB {
	return jsonMapFromSession(session, key)
}

func (a *App) CompleteCodedEarlyHandoff(chat codedflow.Chat, collection map[string]any) bool {
	ctx := unwrapChat(chat)
	if ctx == nil || ctx.account == nil || ctx.contact == nil || ctx.session == nil {
		return false
	}
	settings, err := a.getChatbotSettingsCached(ctx.account.OrganizationID, ctx.account.Name)
	if err != nil || settings == nil {
		return false
	}
	st := earlyHandoffCheckoutStateFromSession(ctx.session)
	setCheckoutState(ctx.session, st)
	setSelectedCategoryID(ctx.session, fieldString(collection, "id"))
	tiqrecommerce.StageEarlyHandoffProductOptionCart(ctx.session)
	tiqrecommerce.StageCommerceOrderNotes(ctx.session)
	if _, err := a.ensureCommerceDraft(ctx.contact, ctx.session, settings); err != nil {
		a.Log.Error("early_handoff draft create failed", "error", err)
		return false
	}
	_ = a.syncReferencedCommerceDraft(ctx.session)
	category := categoryFromCollectionMap(collection)
	return a.completeCommerceCaptureWithCategory(ctx.account, ctx.contact, ctx.session, settings, st, category)
}

func (a *App) CodedOrderRetries() int {
	if a != nil && a.Config != nil {
		return a.Config.CodedFlow.OrderRetryCount()
	}
	return 2
}

func (a *App) OrderRetryCount() int { return a.CodedOrderRetries() }

var lookupLatestOrder = defaultLookupLatestOrder

func (a *App) LookupLatestOrder(account *models.WhatsAppAccount, session *models.ChatbotSession) (map[string]any, error) {
	return lookupLatestOrder(a, account, session)
}

func defaultLookupLatestOrder(a *App, account *models.WhatsAppAccount, session *models.ChatbotSession) (map[string]any, error) {
	if account == nil || session == nil {
		return nil, fmt.Errorf("order status is unavailable")
	}
	settings, err := a.getChatbotSettingsCached(account.OrganizationID, account.Name)
	if err != nil || settings == nil {
		return nil, fmt.Errorf("order status is unavailable")
	}
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		return nil, fmt.Errorf("order status is unavailable")
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := rt.Client.LookupOrderStatus(ctx, rt.StoreID, rt.PhoneNumber, "")
	if err != nil {
		return nil, err
	}
	return compactOrderStatus(raw, ensureCommerceCurrency(ctx, rt)), nil
}

func (a *App) TryEcommerceTransfer(chat codedflow.Chat, message string) (bool, error) {
	// Implemented in codedflow_ecommerce_host.go to avoid an init-time cycle
	// with the blank-imported tiqrecommerce package registration.
	return tryEcommerceTransfer(a, chat, message)
}

func (a *App) GenerateOpenAIResponse(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string) (string, error) {
	return a.generateOpenAIResponse(settings, session, userMessage, contextData)
}

func (a *App) GenerateAnthropicResponse(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string) (string, error) {
	return a.generateAnthropicResponse(settings, session, userMessage, contextData)
}

func (a *App) GenerateGoogleResponse(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string) (string, error) {
	return a.generateGoogleResponse(settings, session, userMessage, contextData)
}

func (a *App) CompleteCodedText(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string) (string, error) {
	switch settings.AI.Provider {
	case models.AIProviderOpenAI:
		return a.generateOpenAIResponse(settings, session, userMessage, contextData)
	case models.AIProviderAnthropic:
		return a.generateAnthropicResponse(settings, session, userMessage, contextData)
	case models.AIProviderGoogle:
		return a.generateGoogleResponse(settings, session, userMessage, contextData)
	default:
		return "", fmt.Errorf("unsupported AI provider: %s", settings.AI.Provider)
	}
}

func (a *App) CompleteCodedRoleText(session *models.ChatbotSession, role, prompt string) (string, error) {
	if a == nil || session == nil {
		return "", fmt.Errorf("ai is not configured")
	}
	settings, err := a.getChatbotSettingsCached(session.OrganizationID, session.WhatsAppAccount)
	if err != nil || settings == nil {
		return "", fmt.Errorf("ai is not configured")
	}
	switch codedFlowRoleProvider(settings, role) {
	case models.IntentProviderGateway:
		return a.CompleteAIGatewayChat(settings, prompt)
	case models.IntentProviderJev:
		return "", fmt.Errorf("TypeSafe Jev cannot generate text for %s; use Account AI or AI Gateway with a language model", role)
	default:
		if !settings.AI.Enabled || settings.AI.Provider == "" || strings.TrimSpace(settings.AI.APIKey) == "" {
			return "", fmt.Errorf("ai is not configured")
		}
		return a.CompleteCodedText(settings, session, prompt, "")
	}
}

func codedFlowRoleProvider(settings *models.ChatbotSettings, role string) models.IntentProvider {
	if settings == nil {
		return models.IntentProviderGeneric
	}
	var raw models.IntentProvider
	switch role {
	case "intent":
		raw = settings.AI.IntentProvider
	case "translate":
		raw = settings.AI.TranslateProvider
	case "guide":
		raw = settings.AI.GuideProvider
	case "recover":
		raw = settings.AI.RecoverProvider
	default:
		raw = models.IntentProviderGeneric
	}
	p, ok := models.NormalizeIntentProvider(raw)
	if !ok {
		return models.IntentProviderGeneric
	}
	return p
}

const aiGatewayChatURL = "https://ai-gateway.vercel.sh/v1/chat/completions"

func (a *App) CompleteAIGatewayChat(settings *models.ChatbotSettings, prompt string) (string, error) {
	if settings == nil {
		return "", fmt.Errorf("ai gateway is not configured")
	}
	key := strings.TrimSpace(settings.AI.GatewayAPIKey)
	if key == "" {
		return "", fmt.Errorf("ai gateway api key is not configured")
	}
	model := strings.TrimSpace(settings.AI.GatewayModel)
	if model == "" {
		model = models.IntentGatewayModelGemini25Flash
	}
	if !models.ValidIntentGatewayChatModel(model) {
		return "", fmt.Errorf("AI Gateway model %s cannot generate text; choose %s", model, models.IntentGatewayModelGemini25Flash)
	}
	payload, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal gateway payload: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		req, err := http.NewRequest(http.MethodPost, aiGatewayChatURL, bytes.NewReader(payload))
		if err != nil {
			return "", fmt.Errorf("failed to create gateway request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := a.httpClient().Do(req)
		if err != nil {
			lastErr = fmt.Errorf("gateway request failed: %w", err)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("failed to read gateway response: %w", readErr)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 {
			lastErr = fmt.Errorf("gateway temporary error: %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("gateway error %d: %s", resp.StatusCode, truncateRunes(string(body), 200))
		}
		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return "", fmt.Errorf("failed to parse gateway response: %w", err)
		}
		if len(parsed.Choices) == 0 {
			return "", fmt.Errorf("gateway returned no choices")
		}
		return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("gateway request failed")
	}
	return "", lastErr
}

func (a *App) CallSystemOne(endpoint codedflow.JevEndpoint, state map[string]any, questions map[string]any) (codedflow.JevSystemOneResponse, error) {
	payload, err := json.Marshal(map[string]any{
		"model":     endpoint.Model,
		"state":     state,
		"questions": questions,
	})
	if err != nil {
		return codedflow.JevSystemOneResponse{}, fmt.Errorf("failed to marshal typesafe payload: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		req, err := http.NewRequest(http.MethodPost, endpoint.URL, bytes.NewReader(payload))
		if err != nil {
			return codedflow.JevSystemOneResponse{}, fmt.Errorf("failed to create typesafe request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+endpoint.Key)
		resp, err := a.httpClient().Do(req)
		if err != nil {
			lastErr = fmt.Errorf("typesafe request failed: %w", err)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("failed to read typesafe response: %w", readErr)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 {
			lastErr = fmt.Errorf("typesafe temporary error: %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return codedflow.JevSystemOneResponse{}, fmt.Errorf("typesafe error %d: %s", resp.StatusCode, truncateRunes(string(body), 200))
		}
		var parsed codedflow.JevSystemOneResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return codedflow.JevSystemOneResponse{}, fmt.Errorf("failed to parse typesafe response: %w", err)
		}
		return parsed, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("typesafe request failed")
	}
	return codedflow.JevSystemOneResponse{}, lastErr
}

// --- Trace ---

func (a *App) CodedFlowTraceEnabled() bool {
	if enabled, set := codedflow.CodedFlowTraceEnv(); set {
		return enabled
	}
	if a != nil && a.Config != nil {
		return a.Config.CodedFlow.TraceEnabled()
	}
	return true
}

func (a *App) ShouldTraceCodedFlow(session *models.ChatbotSession) bool {
	if a == nil || !a.CodedFlowTraceEnabled() {
		return false
	}
	return codedflow.SessionKey(session) != ""
}

func (a *App) LogCodedFlow(event string, session *models.ChatbotSession, attrs ...any) {
	if a == nil || !a.ShouldTraceCodedFlow(session) {
		return
	}
	fields := []any{
		"component", codedflow.CodedFlowTraceComponent,
		"event", event,
	}
	if session != nil {
		fields = append(fields,
			"session_id", session.ID.String(),
			"org_id", session.OrganizationID.String(),
			"account", session.WhatsAppAccount,
			"phone", session.PhoneNumber,
			"flow_key", codedflow.SessionKey(session),
			"step", session.CurrentStep,
			"session_status", string(session.Status),
		)
	}
	fields = append(fields, attrs...)
	a.Log.Info("coded_flow."+event, fields...)
}

func (a *App) LogCodedFlowInbound(chat codedflow.Chat) {
	ctx := unwrapChat(chat)
	if ctx == nil || ctx.preview != nil || !a.ShouldTraceCodedFlow(ctx.session) {
		return
	}
	a.LogCodedFlow("inbound", ctx.session,
		"user_text", codedflow.TruncateTrace(ctx.userInput),
		"button_id", strings.TrimSpace(ctx.buttonID),
		"has_flow_response", len(ctx.flowResponseData) > 0,
	)
}

func (a *App) LogCodedFlowContext(session *models.ChatbotSession, reason string) {
	if !a.ShouldTraceCodedFlow(session) {
		return
	}
	a.LogCodedFlow("turn_context", session,
		"reason", reason,
		"context_json", codedflow.TruncateTrace(codedflow.MustJSON(codedflow.PreviewSessionContext(session))),
	)
}

func (a *App) LogCodedFlowWhatsApp(chat codedflow.Chat, step, interactive, content string, extra ...any) {
	ctx := unwrapChat(chat)
	if ctx == nil || ctx.preview != nil || !a.ShouldTraceCodedFlow(ctx.session) {
		return
	}
	attrs := []any{
		"whatsapp_step", step,
		"interactive", interactive,
		"message_body", codedflow.TruncateTrace(content),
	}
	attrs = append(attrs, extra...)
	a.LogCodedFlow("whatsapp_outbound", ctx.session, attrs...)
}

func (a *App) LogCodedFlowAI(session *models.ChatbotSession, role, prompt, response, errMsg string, extra ...any) {
	if !a.ShouldTraceCodedFlow(session) {
		return
	}
	a.LogCodedFlow("ai_request", session,
		append([]any{
			"ai_role", role,
			"prompt", codedflow.TruncateTrace(prompt),
		}, extra...)...,
	)
	attrs := []any{
		"ai_role", role,
		"response", codedflow.TruncateTrace(response),
	}
	if errMsg != "" {
		attrs = append(attrs, "error", errMsg)
	}
	attrs = append(attrs, extra...)
	a.LogCodedFlow("ai_response", session, attrs...)
}

func (a *App) LogCodedFlowTiqrRequest(session *models.ChatbotSession, apiType, operation, curl string, params map[string]string) {
	if !a.ShouldTraceCodedFlow(session) {
		return
	}
	a.LogCodedFlow("tiqr_request", session,
		"api_type", apiType,
		"operation", operation,
		"curl", codedflow.TruncateTrace(curl),
		"params_json", codedflow.TruncateTrace(codedflow.MustJSON(params)),
	)
}

func (a *App) LogCodedFlowTiqrResponse(session *models.ChatbotSession, apiType, operation string, status int, body any, errMsg string) {
	if !a.ShouldTraceCodedFlow(session) {
		return
	}
	attrs := []any{
		"api_type", apiType,
		"operation", operation,
		"http_status", status,
		"response_json", codedflow.TruncateTrace(codedflow.MustJSON(body)),
	}
	if errMsg != "" {
		attrs = append(attrs, "error", errMsg)
	}
	a.LogCodedFlow("tiqr_response", session, attrs...)
}

// Ensure *App implements codedflow.Host at compile time.
var _ codedflow.Host = (*App)(nil)

