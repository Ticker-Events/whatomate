package msg91

import (
	"context"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// CreateFlow is not supported via MSG91 public WhatsApp APIs.
func (c *Client) CreateFlow(ctx context.Context, account *whatsapp.Account, name string, categories []string) (string, error) {
	return "", ErrNotSupported
}

// UpdateFlowJSON is not supported via MSG91 public WhatsApp APIs.
func (c *Client) UpdateFlowJSON(ctx context.Context, account *whatsapp.Account, flowID string, flowJSON *whatsapp.FlowJSON) error {
	return ErrNotSupported
}

// PublishFlow is not supported via MSG91 public WhatsApp APIs.
func (c *Client) PublishFlow(ctx context.Context, account *whatsapp.Account, flowID string) error {
	return ErrNotSupported
}

// DeprecateFlow is not supported via MSG91 public WhatsApp APIs.
func (c *Client) DeprecateFlow(ctx context.Context, account *whatsapp.Account, flowID string) error {
	return ErrNotSupported
}

// DeleteFlow is not supported via MSG91 public WhatsApp APIs.
func (c *Client) DeleteFlow(ctx context.Context, account *whatsapp.Account, flowID string) error {
	return ErrNotSupported
}

// GetFlow is not supported via MSG91 public WhatsApp APIs.
func (c *Client) GetFlow(ctx context.Context, account *whatsapp.Account, flowID string) (*whatsapp.FlowGetResponse, error) {
	return nil, ErrNotSupported
}

// GetFlowAssets is not supported via MSG91 public WhatsApp APIs.
func (c *Client) GetFlowAssets(ctx context.Context, account *whatsapp.Account, flowID string) (*whatsapp.FlowJSON, error) {
	return nil, ErrNotSupported
}

// ListFlows is not supported via MSG91 public WhatsApp APIs.
func (c *Client) ListFlows(ctx context.Context, account *whatsapp.Account) ([]whatsapp.FlowGetResponse, error) {
	return nil, ErrNotSupported
}
