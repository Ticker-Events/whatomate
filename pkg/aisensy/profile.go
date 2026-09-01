package aisensy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// GetBusinessProfile retrieves the business profile via AiSensy.
func (c *Client) GetBusinessProfile(ctx context.Context, account *whatsapp.Account) (*whatsapp.BusinessProfile, error) {
	url := fmt.Sprintf("%s/whatsapp-business-profile/", c.baseURL)

	respBody, err := c.doRequest(ctx, http.MethodGet, url, nil, account)
	if err != nil {
		return nil, fmt.Errorf("failed to get business profile via aisensy: %w", err)
	}

	// AiSensy may return the profile directly or wrapped in a data array
	var direct whatsapp.BusinessProfile
	if err := json.Unmarshal(respBody, &direct); err == nil && (direct.Description != "" || direct.About != "" || direct.MessagingProduct != "") {
		return &direct, nil
	}

	var wrapped struct {
		Data []whatsapp.BusinessProfile `json:"data"`
	}
	if err := json.Unmarshal(respBody, &wrapped); err != nil {
		return nil, fmt.Errorf("failed to parse business profile response: %w", err)
	}

	if len(wrapped.Data) == 0 {
		return nil, fmt.Errorf("no business profile found")
	}

	return &wrapped.Data[0], nil
}

// UpdateBusinessProfile updates the business profile via AiSensy.
func (c *Client) UpdateBusinessProfile(ctx context.Context, account *whatsapp.Account, input whatsapp.BusinessProfileInput) error {
	url := fmt.Sprintf("%s/whatsapp-business-profile/", c.baseURL)

	input.MessagingProduct = "whatsapp"

	_, err := c.doRequest(ctx, http.MethodPost, url, input, account)
	if err != nil {
		return fmt.Errorf("failed to update business profile via aisensy: %w", err)
	}

	return nil
}

// UploadProfilePicture uploads a profile picture via AiSensy using UploadMedia.
// AiSensy does not support resumable uploads, so we use the standard upload instead.
func (c *Client) UploadProfilePicture(ctx context.Context, account *whatsapp.Account, fileData []byte, mimeType string) (string, error) {
	handle, err := c.UploadMedia(ctx, account, fileData, mimeType, "profile_picture")
	if err != nil {
		return "", fmt.Errorf("failed to upload profile picture via aisensy: %w", err)
	}
	return handle, nil
}
