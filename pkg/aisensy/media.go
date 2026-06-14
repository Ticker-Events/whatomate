package aisensy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// GetMediaURL fetches media content from AiSensy via POST /get-media/ and
// caches the raw bytes. It returns a sentinel URL of the form
// "aisensy-media://<nonce>" that DownloadMedia recognises.
func (c *Client) GetMediaURL(ctx context.Context, mediaID string, account *whatsapp.Account) (string, error) {
	token, err := c.getToken(ctx, account)
	if err != nil {
		return "", fmt.Errorf("failed to get aisensy token for media: %w", err)
	}

	url := c.baseURL + "/get-media/"
	payload := map[string]string{"id": mediaID}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create get-media request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get-media request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read get-media response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", parseAPIError(resp.StatusCode, respBody)
	}

	// AiSensy returns {"data": [byte_values]} — a JSON array of numbers
	var result struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse get-media response: %w", err)
	}

	// The data field may be a JSON array of byte integers or a base64 string
	var mediaBytes []byte
	if len(result.Data) > 0 && result.Data[0] == '[' {
		// JSON number array
		var nums []byte
		if err := json.Unmarshal(result.Data, &nums); err != nil {
			return "", fmt.Errorf("failed to decode media byte array: %w", err)
		}
		mediaBytes = nums
	} else {
		// Raw bytes or base64 string — treat as-is
		mediaBytes = bytes.Trim(result.Data, `"`)
	}

	nonce := uuid.New().String()
	c.mediaCache.Store(nonce, mediaBytes)

	c.Log.Debug("AiSensy media cached", "media_id", mediaID, "size", len(mediaBytes))
	return mediaCachePrefix + nonce, nil
}

// DownloadMedia returns cached media bytes for AiSensy sentinel URLs, or
// performs a standard HTTP download for regular URLs (for backward compat).
func (c *Client) DownloadMedia(ctx context.Context, mediaURL string, accessToken string) ([]byte, error) {
	if strings.HasPrefix(mediaURL, mediaCachePrefix) {
		nonce := strings.TrimPrefix(mediaURL, mediaCachePrefix)
		if v, ok := c.mediaCache.LoadAndDelete(nonce); ok {
			return v.([]byte), nil
		}
		return nil, fmt.Errorf("aisensy media cache miss for nonce: %s", nonce)
	}

	// Fallback: standard HTTP download (should not normally be reached for AiSensy)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("media download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read media body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("media download returned status %d", resp.StatusCode)
	}

	return data, nil
}

// UploadMedia uploads media to AiSensy via its media upload endpoint.
// AiSensy's Direct API accepts the same multipart upload format as Meta.
func (c *Client) UploadMedia(ctx context.Context, account *whatsapp.Account, data []byte, mimeType, filename string) (string, error) {
	token, err := c.getToken(ctx, account)
	if err != nil {
		return "", fmt.Errorf("failed to get aisensy token for upload: %w", err)
	}

	// AiSensy uses the same /media endpoint pattern as Meta but under their base URL
	url := fmt.Sprintf("%s/media/", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to create upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mimeType)
	if filename != "" {
		req.Header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("media upload failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read upload response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", parseAPIError(resp.StatusCode, respBody)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse upload response: %w", err)
	}

	return result.ID, nil
}

// ResumableUpload is not supported for AiSensy (returns ErrNotSupported).
// Template header media should be uploaded via UploadMedia instead.
func (c *Client) ResumableUpload(ctx context.Context, account *whatsapp.Account, data []byte, mimeType, filename string) (string, error) {
	return "", ErrNotSupported
}
