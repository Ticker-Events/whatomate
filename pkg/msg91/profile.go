package msg91

import (
	"context"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// GetBusinessProfile is not supported via MSG91 public WhatsApp APIs.
func (c *Client) GetBusinessProfile(ctx context.Context, account *whatsapp.Account) (*whatsapp.BusinessProfile, error) {
	return nil, ErrNotSupported
}

// UpdateBusinessProfile is not supported via MSG91 public WhatsApp APIs.
func (c *Client) UpdateBusinessProfile(ctx context.Context, account *whatsapp.Account, input whatsapp.BusinessProfileInput) error {
	return ErrNotSupported
}

// UploadProfilePicture is not supported via MSG91 public WhatsApp APIs.
func (c *Client) UploadProfilePicture(ctx context.Context, account *whatsapp.Account, fileData []byte, mimeType string) (string, error) {
	return "", ErrNotSupported
}
