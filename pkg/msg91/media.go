package msg91

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// UploadMedia is not supported by MSG91. Outbound media must be a public HTTPS URL
// (callers should host on S3/Spaces and pass the link).
func (c *Client) UploadMedia(ctx context.Context, account *whatsapp.Account, data []byte, mimeType, filename string) (string, error) {
	return "", fmt.Errorf("msg91 does not support binary media upload; provide a publicly accessible HTTPS URL")
}

// ResumableUpload is not supported for MSG91.
func (c *Client) ResumableUpload(ctx context.Context, account *whatsapp.Account, data []byte, mimeType, filename string) (string, error) {
	return "", ErrNotSupported
}

// GetMediaURL returns the media URL as-is when it is already an HTTP(S) link.
// MSG91 inbound webhooks typically include direct media URLs.
func (c *Client) GetMediaURL(ctx context.Context, mediaID string, account *whatsapp.Account) (string, error) {
	if strings.HasPrefix(mediaID, "https://") || strings.HasPrefix(mediaID, "http://") {
		return mediaID, nil
	}
	return "", fmt.Errorf("msg91 media id lookup is not supported; expected an HTTPS media URL")
}

// DownloadMedia downloads media bytes from a public URL (no auth required for MSG91 CDN links).
func (c *Client) DownloadMedia(ctx context.Context, mediaURL string, accessToken string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create media download request: %w", err)
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
