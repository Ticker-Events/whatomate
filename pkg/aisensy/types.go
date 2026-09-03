// Package aisensy implements the whatsapp.MessagingClient interface using
// AiSensy's Direct API — a JWT-authenticated proxy over Meta's WhatsApp Cloud
// API that accepts identical Meta-format payloads.
package aisensy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

const (
	// ProviderAiSensy is the provider identifier stored on WhatsAppAccount.
	ProviderAiSensy = "aisensy"
	// ProviderMeta is the default Meta Cloud API provider.
	ProviderMeta = "meta"

	// DefaultBaseURL is the AiSensy Direct API base URL.
	DefaultBaseURL = "https://backend.aisensy.com/direct-apis/t1/api"

	// mediaCachePrefix is the URL scheme returned by GetMediaURL for cached media.
	mediaCachePrefix = "aisensy-media://"
)

// APIError wraps an AiSensy API error response.
type APIError struct {
	StatusCode int
	Message    string
	RawBody    []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aisensy api error %d: %s", e.StatusCode, e.Message)
}

// parseAPIError attempts to extract a useful error message from the response body.
func parseAPIError(statusCode int, body []byte) error {
	// Meta-style nested error (often proxied by AiSensy):
	// {"error":{"message":"(#131009)...","error_data":{"details":"..."}}}
	var meta struct {
		Error struct {
			Message   string `json:"message"`
			Code      int    `json:"code"`
			ErrorData struct {
				Details string `json:"details"`
			} `json:"error_data"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &meta) == nil && meta.Error.Message != "" {
		msg := meta.Error.Message
		if d := strings.TrimSpace(meta.Error.ErrorData.Details); d != "" {
			msg = msg + ": " + d
		}
		return &APIError{StatusCode: statusCode, Message: msg, RawBody: body}
	}

	// Flat JSON error shapes
	var resp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(body, &resp) == nil {
		msg := resp.Error
		if msg == "" {
			msg = resp.Message
		}
		if msg == "" {
			msg = resp.Detail
		}
		if msg != "" {
			return &APIError{StatusCode: statusCode, Message: msg, RawBody: body}
		}
	}
	return &APIError{StatusCode: statusCode, Message: http.StatusText(statusCode), RawBody: body}
}

// ErrNotSupported is returned by methods that AiSensy does not support.
var ErrNotSupported = fmt.Errorf("operation not supported for AiSensy accounts")

// accountFromWA extracts AiSensy-specific fields from a whatsapp.Account.
// Callers that reach the AiSensy client will always have Provider == "aisensy".
func accountFromWA(a *whatsapp.Account) (email, password, projectID, cachedToken string) {
	return a.AiSensyEmail, a.AiSensyPassword, a.AiSensyProjectID, a.AiSensyToken
}
