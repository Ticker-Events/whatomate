package aisensy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/zerodha/logf"
)

// tokenCacheEntry holds a cached JWT and its expiry time.
type tokenCacheEntry struct {
	token     string
	expiresAt time.Time
}

// Client implements whatsapp.MessagingClient using AiSensy's Direct API.
// It manages JWT generation and automatic refresh on 401 responses.
type Client struct {
	HTTPClient *http.Client
	Log        logf.Logger
	baseURL    string

	// tokenCache caches JWT tokens per project ID to avoid unnecessary regeneration.
	// key: projectID, value: tokenCacheEntry
	tokenCache sync.Map

	// mediaCache holds pre-fetched AiSensy media bytes keyed by a nonce.
	// key: nonce (UUID), value: []byte
	mediaCache sync.Map
}

// New creates a new AiSensy client from application config.
func New(cfg config.AiSensyConfig, log logf.Logger) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Log:        log,
		baseURL:    baseURL,
	}
}

// GenerateToken authenticates with AiSensy and returns a fresh JWT.
// The credential string is base64(email:password:projectID) as per AiSensy docs.
func (c *Client) GenerateToken(ctx context.Context, email, password, projectID string) (string, error) {
	raw := fmt.Sprintf("%s:%s:%s", email, password, projectID)
	b64 := base64.StdEncoding.EncodeToString([]byte(raw))

	c.Log.Info("aisensy GenerateToken request",
		"curl", fmt.Sprintf(
			"curl --request POST --url %s/users/regenrate-token --header 'Accept: application/json' --header 'Authorization: Bearer %s' --header 'Content-Type: application/json' --data '{\"direct_api\": true}'",
			c.baseURL, b64,
		),
		"decoded_credential", raw,
	)

	payload := map[string]bool{
		"direct_api": true,
	}
	body, _ := json.Marshal(payload)

	url := c.baseURL + "/users/regenrate-token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+b64)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to generate aisensy token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	c.Log.Info("aisensy GenerateToken response",
		"status", resp.StatusCode,
		"body", string(respBody),
	)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("aisensy token generation failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Users []struct {
			Token string `json:"token"`
		} `json:"users"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}
	if len(result.Users) == 0 || result.Users[0].Token == "" {
		return "", fmt.Errorf("no token returned from aisensy")
	}

	return result.Users[0].Token, nil
}

// getToken returns a valid JWT for the given account, generating a new one if
// the cache is empty or expired. It uses a 5-minute safety margin before
// the JWT expires to avoid racing the server-side expiry.
func (c *Client) getToken(ctx context.Context, account *whatsapp.Account) (string, error) {
	email, password, projectID, cached := accountFromWA(account)
	if projectID == "" {
		return "", fmt.Errorf("aisensy project_id is required")
	}

	// Check in-memory cache first
	if v, ok := c.tokenCache.Load(projectID); ok {
		entry := v.(tokenCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.token, nil
		}
	}

	// Try the cached DB token before generating a new one
	if cached != "" {
		exp := jwtExpiry(cached)
		if exp.After(time.Now().Add(5 * time.Minute)) {
			c.tokenCache.Store(projectID, tokenCacheEntry{token: cached, expiresAt: exp})
			return cached, nil
		}
	}

	// Generate a fresh token
	token, err := c.GenerateToken(ctx, email, password, projectID)
	if err != nil {
		return "", err
	}

	exp := jwtExpiry(token)
	if exp.IsZero() {
		exp = time.Now().Add(24 * time.Hour) // conservative fallback
	}
	c.tokenCache.Store(projectID, tokenCacheEntry{token: token, expiresAt: exp})
	return token, nil
}

// invalidateToken clears the cached token for a project so the next call
// triggers a fresh generation.
func (c *Client) invalidateToken(projectID string) {
	c.tokenCache.Delete(projectID)
}

// doRequest executes an authenticated JSON request against the AiSensy API.
// On 401 it refreshes the token once and retries.
func (c *Client) doRequest(ctx context.Context, method, url string, body any, account *whatsapp.Account) ([]byte, error) {
	token, err := c.getToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("failed to get aisensy token: %w", err)
	}

	respBody, statusCode, err := c.rawRequest(ctx, method, url, body, token)
	if err != nil {
		return nil, err
	}

	// Retry once on 401 with a fresh token
	if statusCode == http.StatusUnauthorized {
		_, _, projectID, _ := accountFromWA(account)
		c.invalidateToken(projectID)

		token, err = c.GenerateToken(ctx, account.AiSensyEmail, account.AiSensyPassword, account.AiSensyProjectID)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh aisensy token: %w", err)
		}
		exp := jwtExpiry(token)
		if exp.IsZero() {
			exp = time.Now().Add(24 * time.Hour)
		}
		c.tokenCache.Store(account.AiSensyProjectID, tokenCacheEntry{token: token, expiresAt: exp})

		respBody, statusCode, err = c.rawRequest(ctx, method, url, body, token)
		if err != nil {
			return nil, err
		}
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, parseAPIError(statusCode, respBody)
	}

	return respBody, nil
}

// rawRequest performs the HTTP call and returns body + status code.
func (c *Client) rawRequest(ctx context.Context, method, url string, body any, token string) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// messagesURL returns the AiSensy messages endpoint for the given phone ID.
func (c *Client) messagesURL(account *whatsapp.Account) string {
	return fmt.Sprintf("%s/messages/", c.baseURL)
}

// jwtExpiry extracts the `exp` claim from a JWT without verifying the signature.
// Returns zero time on any parse error.
func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}

	payload := parts[1]
	// Add padding if needed
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return time.Time{}
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return time.Time{}
	}

	if claims.Exp == 0 {
		return time.Time{}
	}

	return time.Unix(claims.Exp, 0)
}

// ValidateCredentials verifies AiSensy credentials by attempting to generate a token.
func (c *Client) ValidateCredentials(ctx context.Context, account *whatsapp.Account) (*whatsapp.CredentialsValidationResult, error) {
	email, password, projectID, _ := accountFromWA(account)
	if email == "" || password == "" || projectID == "" {
		return nil, fmt.Errorf("aisensy email, password, and project_id are all required")
	}

	token, err := c.GenerateToken(ctx, email, password, projectID)
	if err != nil {
		return nil, fmt.Errorf("aisensy credential validation failed: %w", err)
	}

	// Cache the new token
	exp := jwtExpiry(token)
	if exp.IsZero() {
		exp = time.Now().Add(24 * time.Hour)
	}
	c.tokenCache.Store(projectID, tokenCacheEntry{token: token, expiresAt: exp})

	return &whatsapp.CredentialsValidationResult{
		VerifiedName: "AiSensy Project " + projectID,
		PhoneNumber:  account.PhoneID,
		Token:        token,
	}, nil
}

// SubscribeApp is a no-op for AiSensy (webhook subscription is managed by AiSensy).
func (c *Client) SubscribeApp(ctx context.Context, account *whatsapp.Account) error {
	return nil
}
