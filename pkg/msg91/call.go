package msg91

import (
	"context"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// PreAcceptCall is not supported via MSG91 public WhatsApp APIs.
func (c *Client) PreAcceptCall(ctx context.Context, account *whatsapp.Account, callID, sdpAnswer string) error {
	return ErrNotSupported
}

// AcceptCall is not supported via MSG91 public WhatsApp APIs.
func (c *Client) AcceptCall(ctx context.Context, account *whatsapp.Account, callID, sdpAnswer string) error {
	return ErrNotSupported
}

// RejectCall is not supported via MSG91 public WhatsApp APIs.
func (c *Client) RejectCall(ctx context.Context, account *whatsapp.Account, callID string) error {
	return ErrNotSupported
}

// TerminateCall is not supported via MSG91 public WhatsApp APIs.
func (c *Client) TerminateCall(ctx context.Context, account *whatsapp.Account, callID string) error {
	return ErrNotSupported
}

// InitiateCall is not supported via MSG91 public WhatsApp APIs in this integration.
func (c *Client) InitiateCall(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, sdpOffer string) (string, error) {
	return "", ErrNotSupported
}

// SendCallPermissionRequest is not supported via MSG91 public WhatsApp APIs.
func (c *Client) SendCallPermissionRequest(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText string) (string, error) {
	return "", ErrNotSupported
}

// GetCallPermission is not supported via MSG91 public WhatsApp APIs.
func (c *Client) GetCallPermission(ctx context.Context, account *whatsapp.Account, userPhone string) (string, error) {
	return "", ErrNotSupported
}
