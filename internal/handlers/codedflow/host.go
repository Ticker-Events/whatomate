package codedflow

import (
	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/tickermcp"
)

// NodeResult is the outcome of a chat-node Host call.
// Yield means the flow should wait for the next inbound at this step.
type NodeResult struct {
	Outcome string
	Yield   bool
}

// InboundMedia is a WhatsApp photo or file on the current turn.
type InboundMedia struct {
	MessageID string
	MediaURL  string
	MIMEType  string
	Filename  string
}

// Chat wraps the inbound turn state Conv needs from chatNodeCtx.
// Native returns the underlying *chatNodeCtx for the handlers adapter.
type Chat interface {
	Session() *models.ChatbotSession
	Account() *models.WhatsAppAccount
	Contact() *models.Contact

	UserInput() string
	SetUserInput(string)
	InboundMedia() InboundMedia
	ButtonID() string
	SetButtonID(string)
	FlowResponseData() map[string]any
	SetFlowResponseData(map[string]any)

	Consumed() bool
	SetConsumed(bool)
	Capturing() bool

	LastTiqr() map[string]any
	SetLastTiqr(map[string]any)
	LastTiqrErr() string
	SetLastTiqrErr(string)
	LastTiqrStatus() int
	SetLastTiqrStatus(int)

	Preview() *PreviewSink
	SetPreview(*PreviewSink)

	Native() any
}

// Host is the App surface codedflow and tiqrecommerce need without importing handlers.
type Host interface {
	// Logging
	LogInfo(msg string, args ...any)
	LogWarn(msg string, args ...any)
	LogError(msg string, args ...any)

	// Node execution — handlers builds ChatNode internally.
	ExecChatMessage(chat Chat, id string, config map[string]any) (NodeResult, error)
	ExecChatButtons(chat Chat, id string, config map[string]any) (NodeResult, error)
	ExecChatWhatsAppFlow(chat Chat, id string, config map[string]any) (NodeResult, error)
	ExecChatTiqrStoreAPI(chat Chat, id string, config map[string]any) (NodeResult, error)
	ExecChatTransfer(chat Chat, id string, config map[string]any) (NodeResult, error)

	DeliverCodedCTAURL(chat Chat, step, body, buttonText, url string) error
	DeliverCodedLocationRequest(chat Chat, step, body string) error
	DeliverCodedText(chat Chat, step, text string) error

	PersistChatSession(session *models.ChatbotSession) error
	PersistSessionData(session *models.ChatbotSession) error

	// Graph helpers used by Conv choice matching.
	ButtonsForNode(cfg map[string]any, data models.JSONB) ([]map[string]any, error)
	ApplyButtonSelection(cfg map[string]any, data models.JSONB, buttonID, title string) models.JSONB

	// Payment / order display helpers.
	PaymentCTAContent(order map[string]any) (body, paymentURL string)
	CompactOrderCreateResult(order map[string]any, currency string) map[string]any
	SessionCurrencyCode(session *models.ChatbotSession) string
	StashSessionCurrency(session *models.ChatbotSession, currency string)

	// AI
	CompleteCodedRoleText(session *models.ChatbotSession, role, prompt string) (string, error)
	CompleteCodedText(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string) (string, error)
	CompleteAIGatewayChat(settings *models.ChatbotSettings, prompt string) (string, error)
	CallSystemOne(endpoint JevEndpoint, state map[string]any, questions map[string]any) (JevSystemOneResponse, error)
	GenerateOpenAIResponse(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string) (string, error)
	GenerateAnthropicResponse(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string) (string, error)
	GenerateGoogleResponse(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string) (string, error)

	GetChatbotSettingsCached(orgID uuid.UUID, accountName string) (*models.ChatbotSettings, error)
	IsWithinBusinessHours(hours models.JSONBArray) bool
	SendAndSaveTextMessage(account *models.WhatsAppAccount, contact *models.Contact, text string) error

	EnsureCommerceDraft(contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings) (*models.CommerceDraft, error)
	SyncReferencedCommerceDraft(session *models.ChatbotSession) error
	CreateCommerceTransfer(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, category tickermcp.Category) (*models.AgentTransfer, bool, error)
	StageCommerceHandoffSessionData(session *models.ChatbotSession, category tickermcp.Category)
	CompleteCommerceCaptureWithCategory(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, collection map[string]any) bool
	StageEarlyHandoffCheckout(session *models.ChatbotSession)
	SetSelectedCategoryID(session *models.ChatbotSession, categoryID string)
	SelectedCategoryID(session *models.ChatbotSession) string
	CategoryFromCollectionMap(raw map[string]any) tickermcp.Category
	JSONMapFromSession(session *models.ChatbotSession, key string) models.JSONB
	// CompleteCodedEarlyHandoff stages checkout state, draft, and commerce transfer for after_capture.
	CompleteCodedEarlyHandoff(chat Chat, collection map[string]any) bool

	CodedOrderRetries() int
	LookupLatestOrder(account *models.WhatsAppAccount, session *models.ChatbotSession) (map[string]any, error)

	// ResolveEcommerceMetaFlowID returns the Meta WhatsApp Flow ID for
	// pickup (delivery=false) or delivery (delivery=true) customer-details
	// forms. Falls back to built-in defaults when unset.
	ResolveEcommerceMetaFlowID(orgID uuid.UUID, accountName string, delivery bool) string

	// Ecommerce handoff: returns handled=true when the session is a TiQR ecommerce coded flow.
	TryEcommerceTransfer(chat Chat, message string) (handled bool, err error)

	// Trace
	ShouldTraceCodedFlow(session *models.ChatbotSession) bool
	LogCodedFlow(event string, session *models.ChatbotSession, attrs ...any)
	LogCodedFlowInbound(chat Chat)
	LogCodedFlowContext(session *models.ChatbotSession, reason string)
	LogCodedFlowWhatsApp(chat Chat, step, interactive, content string, extra ...any)
	LogCodedFlowAI(session *models.ChatbotSession, role, prompt, response, errMsg string, extra ...any)
	LogCodedFlowTiqrRequest(session *models.ChatbotSession, apiType, operation, curl string, params map[string]string)
	LogCodedFlowTiqrResponse(session *models.ChatbotSession, apiType, operation string, status int, body any, errMsg string)

	// Preview Redis / DB (optional — preview HTTP may live fully in handlers)
	CodedFlowTraceEnabled() bool
	OrderRetryCount() int
}
