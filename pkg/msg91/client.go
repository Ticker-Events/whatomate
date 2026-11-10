package msg91

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/zerodha/logf"
)

// Client implements whatsapp.MessagingClient using MSG91 WhatsApp APIs.
type Client struct {
	HTTPClient *http.Client
	Log        logf.Logger
	baseURL    string
}

// Ensure Client implements MessagingClient.
var _ whatsapp.MessagingClient = (*Client)(nil)

// New creates a new MSG91 client from application config.
func New(cfg config.MSG91Config, log logf.Logger) *Client {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Log:        log,
		baseURL:    baseURL,
	}
}

// NewWithBaseURL creates a client with a custom base URL (for tests).
func NewWithBaseURL(log logf.Logger, baseURL string) *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Log:        log,
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
}

func (c *Client) url(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.baseURL + path
}

// doRequest executes an authenticated JSON request against the MSG91 API.
func (c *Client) doRequest(ctx context.Context, method, path string, body any, authKey string) ([]byte, error) {
	if authKey == "" {
		return nil, fmt.Errorf("msg91 authkey is required")
	}

	var reqBody io.Reader
	var rawBody []byte
	if body != nil {
		var err error
		rawBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal msg91 request: %w", err)
		}
		reqBody = bytes.NewReader(rawBody)
	}

	reqURL := c.url(path)
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create msg91 request: %w", err)
	}
	req.Header.Set("authkey", authKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("msg91 request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read msg91 response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, respBody)
	}

	// MSG91 often returns HTTP 200 with hasError=true for logical failures.
	var envelope struct {
		HasError bool `json:"hasError"`
		Status   string `json:"status"`
	}
	if json.Unmarshal(respBody, &envelope) == nil {
		if envelope.HasError || strings.EqualFold(envelope.Status, "error") || strings.EqualFold(envelope.Status, "fail") {
			return nil, parseAPIError(resp.StatusCode, respBody)
		}
	}

	return respBody, nil
}

// ValidateCredentials verifies MSG91 credentials by fetching activated WhatsApp numbers
// and ensuring the configured integrated_number (PhoneID) is present.
func (c *Client) ValidateCredentials(ctx context.Context, account *whatsapp.Account) (*whatsapp.CredentialsValidationResult, error) {
	authKey, integrated := accountFromWA(account)
	if authKey == "" {
		return nil, fmt.Errorf("msg91 authkey is required")
	}
	if integrated == "" {
		return nil, fmt.Errorf("msg91 integrated_number (phone_id) is required")
	}

	respBody, err := c.doRequest(ctx, http.MethodGet, activationPath, nil, authKey)
	if err != nil {
		return nil, fmt.Errorf("msg91 credential validation failed: %w", err)
	}

	numbers := extractIntegratedNumbers(respBody)
	want := normalizePhone(integrated)
	found := false
	for _, n := range numbers {
		if normalizePhone(n) == want {
			found = true
			break
		}
	}
	if !found && len(numbers) > 0 {
		return nil, fmt.Errorf("msg91 integrated_number %s not found among activated numbers", integrated)
	}
	// If the activation API returns an opaque/empty shape, accept authkey as valid
	// when the HTTP call succeeded (some accounts return nested structures we don't map).

	return &whatsapp.CredentialsValidationResult{
		VerifiedName: "MSG91 " + integrated,
		PhoneNumber:  integrated,
	}, nil
}

// SubscribeApp is a no-op for MSG91 (webhook subscription is managed in the MSG91 dashboard).
func (c *Client) SubscribeApp(ctx context.Context, account *whatsapp.Account) error {
	return nil
}

func normalizePhone(n string) string {
	n = strings.TrimSpace(n)
	n = strings.TrimPrefix(n, "+")
	n = strings.ReplaceAll(n, " ", "")
	n = strings.ReplaceAll(n, "-", "")
	return n
}

// extractIntegratedNumbers best-effort parses activated WhatsApp numbers from MSG91.
func extractIntegratedNumbers(body []byte) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	// Common shapes: {data:[{integrated_number|number|whatsapp_number:...}]} or flat arrays.
	var asMap map[string]any
	if json.Unmarshal(body, &asMap) == nil {
		collectNumbers(asMap, add)
		return out
	}
	var asArr []any
	if json.Unmarshal(body, &asArr) == nil {
		for _, item := range asArr {
			collectNumbers(item, add)
		}
	}
	return out
}

func collectNumbers(v any, add func(string)) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			lk := strings.ToLower(k)
			if lk == "integrated_number" || lk == "integratednumber" || lk == "number" ||
				lk == "whatsapp_number" || lk == "phone" || lk == "phone_number" {
				if s, ok := val.(string); ok {
					add(s)
				} else if n, ok := val.(float64); ok {
					add(fmt.Sprintf("%.0f", n))
				}
			}
			collectNumbers(val, add)
		}
	case []any:
		for _, item := range t {
			collectNumbers(item, add)
		}
	}
}
