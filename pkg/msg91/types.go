// Package msg91 implements the whatsapp.MessagingClient interface using
// MSG91's WhatsApp APIs (control.msg91.com / api.msg91.com).
package msg91

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

const (
	// ProviderMSG91 is the provider identifier stored on WhatsAppAccount.
	ProviderMSG91 = "msg91"
	// ProviderMeta is the default Meta Cloud API provider.
	ProviderMeta = "meta"
	// ProviderAiSensy is the AiSensy Direct API provider.
	ProviderAiSensy = "aisensy"

	// DefaultBaseURL is the MSG91 Control API base URL used for most WhatsApp endpoints.
	DefaultBaseURL = "https://control.msg91.com"

	outboundPath       = "/api/v5/whatsapp/whatsapp-outbound-message/"
	outboundBulkPath   = "/api/v5/whatsapp/whatsapp-outbound-message/bulk/"
	activationPath     = "/api/v5/whatsapp/whatsapp-activation/"
	getTemplatesPath   = "/api/v5/whatsapp/get-template-client/"
	clientTemplatePath = "/api/v5/whatsapp/client-panel-template/"
	typingPath         = "/api/v5/whatsapp/typing-indicator/"
)

// APIError wraps an MSG91 API error response.
type APIError struct {
	StatusCode int
	Message    string
	RawBody    []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("msg91 api error %d: %s", e.StatusCode, e.Message)
}

// parseAPIError attempts to extract a useful error message from the response body.
func parseAPIError(statusCode int, body []byte) error {
	var resp struct {
		Status   string `json:"status"`
		HasError bool   `json:"hasError"`
		Errors   any    `json:"errors"`
		Message  string `json:"message"`
		Error    string `json:"error"`
		Data     any    `json:"data"`
	}
	if json.Unmarshal(body, &resp) == nil {
		msg := strings.TrimSpace(resp.Message)
		if msg == "" {
			msg = strings.TrimSpace(resp.Error)
		}
		if msg == "" && resp.Errors != nil {
			switch e := resp.Errors.(type) {
			case string:
				msg = e
			default:
				if b, err := json.Marshal(e); err == nil {
					msg = string(b)
				}
			}
		}
		if msg != "" {
			return &APIError{StatusCode: statusCode, Message: msg, RawBody: body}
		}
		if resp.HasError || strings.EqualFold(resp.Status, "error") || strings.EqualFold(resp.Status, "fail") {
			return &APIError{StatusCode: statusCode, Message: http.StatusText(statusCode), RawBody: body}
		}
	}
	return &APIError{StatusCode: statusCode, Message: http.StatusText(statusCode), RawBody: body}
}

// ErrNotSupported is returned by methods that MSG91 does not support.
var ErrNotSupported = fmt.Errorf("operation not supported for MSG91 accounts")

// accountFromWA extracts MSG91-specific fields from a whatsapp.Account.
// PhoneID stores the integrated WhatsApp number (with country code).
func accountFromWA(a *whatsapp.Account) (authKey, integratedNumber string) {
	if a == nil {
		return "", ""
	}
	return a.MSG91AuthKey, a.PhoneID
}
