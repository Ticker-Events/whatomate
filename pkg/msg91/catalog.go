package msg91

import (
	"context"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// CreateCatalog is not supported via MSG91 public WhatsApp APIs (send-only catalog messages exist).
func (c *Client) CreateCatalog(ctx context.Context, account *whatsapp.Account, name string) (string, error) {
	return "", ErrNotSupported
}

// ListCatalogs is not supported via MSG91 public WhatsApp APIs.
func (c *Client) ListCatalogs(ctx context.Context, account *whatsapp.Account) ([]whatsapp.CatalogInfo, error) {
	return nil, ErrNotSupported
}

// DeleteCatalog is not supported via MSG91 public WhatsApp APIs.
func (c *Client) DeleteCatalog(ctx context.Context, account *whatsapp.Account, catalogID string) error {
	return ErrNotSupported
}

// ListCatalogProducts is not supported via MSG91 public WhatsApp APIs.
func (c *Client) ListCatalogProducts(ctx context.Context, account *whatsapp.Account, catalogID string) ([]whatsapp.ProductInfo, error) {
	return nil, ErrNotSupported
}

// CreateProduct is not supported via MSG91 public WhatsApp APIs.
func (c *Client) CreateProduct(ctx context.Context, account *whatsapp.Account, catalogID string, product *whatsapp.ProductInput) (string, error) {
	return "", ErrNotSupported
}

// UpdateProduct is not supported via MSG91 public WhatsApp APIs.
func (c *Client) UpdateProduct(ctx context.Context, account *whatsapp.Account, productID string, product *whatsapp.ProductInput) error {
	return ErrNotSupported
}

// DeleteProduct is not supported via MSG91 public WhatsApp APIs.
func (c *Client) DeleteProduct(ctx context.Context, account *whatsapp.Account, productID string) error {
	return ErrNotSupported
}
