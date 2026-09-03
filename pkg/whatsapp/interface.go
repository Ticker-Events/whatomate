package whatsapp

import "context"

// MessagingClient is the provider-agnostic interface for WhatsApp messaging operations.
// Both the Meta WhatsApp Cloud API client and the AiSensy Direct API client implement it.
type MessagingClient interface {
	// Core messaging
	SendTextMessage(ctx context.Context, account *Account, rcpt Recipient, text string, replyToMsgID ...string) (string, error)
	SendImageMessage(ctx context.Context, account *Account, rcpt Recipient, mediaID, caption string) (string, error)
	SendVideoMessage(ctx context.Context, account *Account, rcpt Recipient, mediaID, caption string) (string, error)
	SendAudioMessage(ctx context.Context, account *Account, rcpt Recipient, mediaID string) (string, error)
	SendDocumentMessage(ctx context.Context, account *Account, rcpt Recipient, mediaID, filename, caption string) (string, error)
	SendInteractiveButtons(ctx context.Context, account *Account, rcpt Recipient, bodyText string, buttons []Button, headerImageURL ...string) (string, error)
	SendLocationRequest(ctx context.Context, account *Account, rcpt Recipient, bodyText string) (string, error)
	SendAddressMessage(ctx context.Context, account *Account, rcpt Recipient, bodyText string, params AddressMessageParams) (string, error)
	SendCTAURLButton(ctx context.Context, account *Account, rcpt Recipient, bodyText, buttonText, url string) (string, error)
	SendVoiceCallButton(ctx context.Context, account *Account, rcpt Recipient, bodyText, displayText string, ttlMinutes int, payload string) (string, error)
	SendTemplateMessage(ctx context.Context, account *Account, rcpt Recipient, templateName, languageCode string, components []map[string]any) (string, error)
	SendFlowMessage(ctx context.Context, account *Account, rcpt Recipient, flowID, headerText, bodyText, ctaText, flowToken, firstScreen string) (string, error)
	MarkMessageRead(ctx context.Context, account *Account, messageID string) error

	// Media
	UploadMedia(ctx context.Context, account *Account, data []byte, mimeType, filename string) (string, error)
	GetMediaURL(ctx context.Context, mediaID string, account *Account) (string, error)
	DownloadMedia(ctx context.Context, mediaURL string, accessToken string) ([]byte, error)
	ResumableUpload(ctx context.Context, account *Account, data []byte, mimeType, filename string) (string, error)

	// Templates
	SubmitTemplate(ctx context.Context, account *Account, template *TemplateSubmission) (string, error)
	FetchTemplates(ctx context.Context, account *Account) ([]MetaTemplate, error)
	DeleteTemplate(ctx context.Context, account *Account, templateName string) error

	// Flows
	CreateFlow(ctx context.Context, account *Account, name string, categories []string) (string, error)
	UpdateFlowJSON(ctx context.Context, account *Account, flowID string, flowJSON *FlowJSON) error
	PublishFlow(ctx context.Context, account *Account, flowID string) error
	DeprecateFlow(ctx context.Context, account *Account, flowID string) error
	DeleteFlow(ctx context.Context, account *Account, flowID string) error
	GetFlow(ctx context.Context, account *Account, flowID string) (*FlowGetResponse, error)
	GetFlowAssets(ctx context.Context, account *Account, flowID string) (*FlowJSON, error)
	ListFlows(ctx context.Context, account *Account) ([]FlowGetResponse, error)

	// Calling
	PreAcceptCall(ctx context.Context, account *Account, callID, sdpAnswer string) error
	AcceptCall(ctx context.Context, account *Account, callID, sdpAnswer string) error
	RejectCall(ctx context.Context, account *Account, callID string) error
	TerminateCall(ctx context.Context, account *Account, callID string) error
	InitiateCall(ctx context.Context, account *Account, rcpt Recipient, sdpOffer string) (string, error)
	SendCallPermissionRequest(ctx context.Context, account *Account, rcpt Recipient, bodyText string) (string, error)
	GetCallPermission(ctx context.Context, account *Account, userPhone string) (string, error)

	// Business Profile
	GetBusinessProfile(ctx context.Context, account *Account) (*BusinessProfile, error)
	UpdateBusinessProfile(ctx context.Context, account *Account, input BusinessProfileInput) error
	UploadProfilePicture(ctx context.Context, account *Account, fileData []byte, mimeType string) (string, error)

	// Catalog
	CreateCatalog(ctx context.Context, account *Account, name string) (string, error)
	ListCatalogs(ctx context.Context, account *Account) ([]CatalogInfo, error)
	DeleteCatalog(ctx context.Context, account *Account, catalogID string) error
	ListCatalogProducts(ctx context.Context, account *Account, catalogID string) ([]ProductInfo, error)
	CreateProduct(ctx context.Context, account *Account, catalogID string, product *ProductInput) (string, error)
	UpdateProduct(ctx context.Context, account *Account, productID string, product *ProductInput) error
	DeleteProduct(ctx context.Context, account *Account, productID string) error

	// Account management
	ValidateCredentials(ctx context.Context, account *Account) (*CredentialsValidationResult, error)
	SubscribeApp(ctx context.Context, account *Account) error
}
