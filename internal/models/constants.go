package models

import "strings"

// AuditAction represents the type of audit action
type AuditAction string

const (
	AuditActionCreated AuditAction = "created"
	AuditActionUpdated AuditAction = "updated"
	AuditActionDeleted AuditAction = "deleted"
)

// TeamRole represents a user's role within a specific team (not organizational role)
type TeamRole string

const (
	TeamRoleManager TeamRole = "manager"
	TeamRoleAgent   TeamRole = "agent"
)

// Direction represents message direction
type Direction string

const (
	DirectionIncoming Direction = "incoming"
	DirectionOutgoing Direction = "outgoing"
)

// MessageType represents the type of WhatsApp message
type MessageType string

const (
	MessageTypeText        MessageType = "text"
	MessageTypeImage       MessageType = "image"
	MessageTypeVideo       MessageType = "video"
	MessageTypeAudio       MessageType = "audio"
	MessageTypeDocument    MessageType = "document"
	MessageTypeTemplate    MessageType = "template"
	MessageTypeInteractive MessageType = "interactive"
	MessageTypeFlow        MessageType = "flow"
	MessageTypeReaction    MessageType = "reaction"
	MessageTypeLocation    MessageType = "location"
	MessageTypeContact     MessageType = "contact"
)

// MessageStatus represents the delivery status of a message
type MessageStatus string

const (
	MessageStatusPending   MessageStatus = "pending"
	MessageStatusSent      MessageStatus = "sent"
	MessageStatusDelivered MessageStatus = "delivered"
	MessageStatusRead      MessageStatus = "read"
	MessageStatusFailed    MessageStatus = "failed"
	MessageStatusReceived  MessageStatus = "received"
)

// AIProvider represents supported AI providers
type AIProvider string

const (
	AIProviderOpenAI    AIProvider = "openai"
	AIProviderAnthropic AIProvider = "anthropic"
	AIProviderGoogle    AIProvider = "google"
)

// IntentProvider selects the engine used for coded-flow intent identification.
type IntentProvider string

const (
	IntentProviderGeneric IntentProvider = "generic"
	IntentProviderJev     IntentProvider = "jev"
	IntentProviderGateway IntentProvider = "gateway"
)

const (
	// IntentGatewayModelJev is the documented AI Gateway evaluation model.
	IntentGatewayModelJev = "typesafe-ai/jev"
	// IntentGatewayModelGemini25Flash is Gemini 2.5 Flash via AI Gateway.
	IntentGatewayModelGemini25Flash = "google/gemini-2.5-flash"
	// IntentTypeSafeModel is the TypeSafe System One model for direct Jev calls.
	IntentTypeSafeModel = "jev-latest"
)

// IntentGatewayModels is the fixed picklist of AI Gateway models for coded-flow AI.
var IntentGatewayModels = []string{
	IntentGatewayModelJev,
	IntentGatewayModelGemini25Flash,
}

// IntentGatewayChatModels are AI Gateway models that can generate free text.
var IntentGatewayChatModels = []string{
	IntentGatewayModelGemini25Flash,
}

// ValidIntentGatewayModel reports whether model is an allowed AI Gateway id.
func ValidIntentGatewayModel(model string) bool {
	for _, allowed := range IntentGatewayModels {
		if model == allowed {
			return true
		}
	}
	return false
}

// ValidIntentGatewayChatModel reports whether model can be used for chat completions.
func ValidIntentGatewayChatModel(model string) bool {
	for _, allowed := range IntentGatewayChatModels {
		if model == allowed {
			return true
		}
	}
	return false
}

// NormalizeIntentProvider maps empty to generic and rejects unknown values.
func NormalizeIntentProvider(p IntentProvider) (IntentProvider, bool) {
	switch IntentProvider(strings.TrimSpace(string(p))) {
	case "", IntentProviderGeneric:
		return IntentProviderGeneric, true
	case IntentProviderJev:
		return IntentProviderJev, true
	case IntentProviderGateway:
		return IntentProviderGateway, true
	default:
		return "", false
	}
}

// MatchType represents keyword matching strategies
type MatchType string

const (
	MatchTypeExact      MatchType = "exact"
	MatchTypeContains   MatchType = "contains"
	MatchTypeStartsWith MatchType = "starts_with"
	MatchTypeRegex      MatchType = "regex"
)

// ResponseType represents chatbot response types
type ResponseType string

const (
	ResponseTypeText     ResponseType = "text"
	ResponseTypeTemplate ResponseType = "template"
	ResponseTypeMedia    ResponseType = "media"
	ResponseTypeFlow     ResponseType = "flow"
	ResponseTypeScript   ResponseType = "script"
	ResponseTypeTransfer ResponseType = "transfer"
)

// FlowStepType represents chatbot flow step message types
type FlowStepType string

const (
	FlowStepTypeText         FlowStepType = "text"
	FlowStepTypeTemplate     FlowStepType = "template"
	FlowStepTypeScript       FlowStepType = "script"
	FlowStepTypeAPIFetch     FlowStepType = "api_fetch"
	FlowStepTypeButtons      FlowStepType = "buttons"
	FlowStepTypeTransfer     FlowStepType = "transfer"
	FlowStepTypeWhatsAppFlow FlowStepType = "whatsapp_flow"
)

// SessionStatus represents chatbot session states
type SessionStatus string

const (
	SessionStatusActive    SessionStatus = "active"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusCancelled SessionStatus = "cancelled"
	SessionStatusTimeout   SessionStatus = "timeout"
)

// TransferStatus represents agent transfer states
type TransferStatus string

const (
	TransferStatusActive  TransferStatus = "active"
	TransferStatusResumed TransferStatus = "resumed"
	TransferStatusExpired TransferStatus = "expired"
)

// TransferSource represents how a transfer was initiated
type TransferSource string

const (
	TransferSourceManual          TransferSource = "manual"
	TransferSourceFlow            TransferSource = "flow"
	TransferSourceKeyword         TransferSource = "keyword"
	TransferSourceChatbotDisabled TransferSource = "chatbot_disabled"
	TransferSourceCommerce        TransferSource = "commerce"
)

// CampaignStatus represents bulk message campaign states
type CampaignStatus string

const (
	CampaignStatusDraft      CampaignStatus = "draft"
	CampaignStatusScheduled  CampaignStatus = "scheduled"
	CampaignStatusQueued     CampaignStatus = "queued"
	CampaignStatusProcessing CampaignStatus = "processing"
	CampaignStatusPaused     CampaignStatus = "paused"
	CampaignStatusCompleted  CampaignStatus = "completed"
	CampaignStatusCancelled  CampaignStatus = "cancelled"
	CampaignStatusFailed     CampaignStatus = "failed"
)

// TemplateStatus represents WhatsApp template approval states
type TemplateStatus string

const (
	TemplateStatusPending  TemplateStatus = "PENDING"
	TemplateStatusApproved TemplateStatus = "APPROVED"
	TemplateStatusRejected TemplateStatus = "REJECTED"
)

// TemplateCategory represents WhatsApp template categories
type TemplateCategory string

const (
	TemplateCategoryMarketing      TemplateCategory = "MARKETING"
	TemplateCategoryUtility        TemplateCategory = "UTILITY"
	TemplateCategoryAuthentication TemplateCategory = "AUTHENTICATION"
)

// ContextType represents AI context types
type ContextType string

const (
	ContextTypeStatic ContextType = "static"
	ContextTypeAPI    ContextType = "api"
)

// InputType represents chatbot flow step input types
type InputType string

const (
	InputTypeNone         InputType = "none"
	InputTypeText         InputType = "text"
	InputTypeNumber       InputType = "number"
	InputTypeEmail        InputType = "email"
	InputTypePhone        InputType = "phone"
	InputTypeDate         InputType = "date"
	InputTypeSelect       InputType = "select"
	InputTypeButton       InputType = "button"
	InputTypeWhatsAppFlow InputType = "whatsapp_flow"
)

// AssignmentStrategy represents team assignment strategies
type AssignmentStrategy string

const (
	AssignmentStrategyRoundRobin   AssignmentStrategy = "round_robin"
	AssignmentStrategyLoadBalanced AssignmentStrategy = "load_balanced"
	AssignmentStrategyManual       AssignmentStrategy = "manual"
)

// SSOProviderType represents supported SSO providers
type SSOProviderType string

const (
	SSOProviderGoogle    SSOProviderType = "google"
	SSOProviderMicrosoft SSOProviderType = "microsoft"
	SSOProviderGitHub    SSOProviderType = "github"
	SSOProviderFacebook  SSOProviderType = "facebook"
	SSOProviderCustom    SSOProviderType = "custom"
)

// WebhookEvent represents webhook event types
type WebhookEvent string

const (
	WebhookEventMessageIncoming  WebhookEvent = "message.incoming"
	WebhookEventMessageOutgoing  WebhookEvent = "message.outgoing"
	WebhookEventMessageSent      WebhookEvent = "message.sent"
	WebhookEventContactCreated   WebhookEvent = "contact.created"
	WebhookEventTransferCreated  WebhookEvent = "transfer.created"
	WebhookEventTransferResumed  WebhookEvent = "transfer.resumed"
	WebhookEventTransferAssigned WebhookEvent = "transfer.assigned"
)

// ActionType represents custom action types
type ActionType string

const (
	ActionTypeWebhook    ActionType = "webhook"
	ActionTypeURL        ActionType = "url"
	ActionTypeJavascript ActionType = "javascript"
)
