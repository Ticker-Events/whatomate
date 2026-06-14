package aisensy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// CreateCatalog creates a new product catalog via AiSensy.
func (c *Client) CreateCatalog(ctx context.Context, account *whatsapp.Account, name string) (string, error) {
	url := c.baseURL + "/catalog/"

	payload := map[string]string{"name": name}

	respBody, err := c.doRequest(ctx, http.MethodPost, url, payload, account)
	if err != nil {
		return "", fmt.Errorf("failed to create catalog via aisensy: %w", err)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse create catalog response: %w", err)
	}

	return result.ID, nil
}

// ListCatalogs lists all product catalogs via AiSensy.
func (c *Client) ListCatalogs(ctx context.Context, account *whatsapp.Account) ([]whatsapp.CatalogInfo, error) {
	url := c.baseURL + "/catalog/"

	respBody, err := c.doRequest(ctx, http.MethodGet, url, nil, account)
	if err != nil {
		return nil, fmt.Errorf("failed to list catalogs via aisensy: %w", err)
	}

	var result struct {
		Data []whatsapp.CatalogInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse catalog list response: %w", err)
	}

	return result.Data, nil
}

// DeleteCatalog deletes a catalog via AiSensy.
func (c *Client) DeleteCatalog(ctx context.Context, account *whatsapp.Account, catalogID string) error {
	url := fmt.Sprintf("%s/catalog/%s/", c.baseURL, catalogID)

	_, err := c.doRequest(ctx, http.MethodDelete, url, nil, account)
	if err != nil {
		return fmt.Errorf("failed to delete catalog via aisensy: %w", err)
	}

	return nil
}

// ListCatalogProducts lists all products in a catalog via AiSensy.
func (c *Client) ListCatalogProducts(ctx context.Context, account *whatsapp.Account, catalogID string) ([]whatsapp.ProductInfo, error) {
	url := fmt.Sprintf("%s/catalog/%s/products/", c.baseURL, catalogID)

	respBody, err := c.doRequest(ctx, http.MethodGet, url, nil, account)
	if err != nil {
		return nil, fmt.Errorf("failed to list products via aisensy: %w", err)
	}

	var result struct {
		Data []whatsapp.ProductInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse product list response: %w", err)
	}

	return result.Data, nil
}

// CreateProduct adds a product to a catalog via AiSensy.
func (c *Client) CreateProduct(ctx context.Context, account *whatsapp.Account, catalogID string, product *whatsapp.ProductInput) (string, error) {
	url := c.baseURL + "/product/"

	body := map[string]string{
		"catalog_id":  catalogID,
		"name":        product.Name,
		"price":       strconv.FormatInt(product.Price, 10),
		"currency":    product.Currency,
		"url":         product.URL,
		"image_url":   product.ImageURL,
		"retailer_id": product.RetailerID,
	}
	if product.Description != "" {
		body["description"] = product.Description
	}

	respBody, err := c.doRequest(ctx, http.MethodPost, url, body, account)
	if err != nil {
		return "", fmt.Errorf("failed to create product via aisensy: %w", err)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse create product response: %w", err)
	}

	return result.ID, nil
}

// UpdateProduct updates a product via AiSensy.
func (c *Client) UpdateProduct(ctx context.Context, account *whatsapp.Account, productID string, product *whatsapp.ProductInput) error {
	url := fmt.Sprintf("%s/product/%s/", c.baseURL, productID)

	body := make(map[string]string)
	if product.Name != "" {
		body["name"] = product.Name
	}
	if product.Price > 0 {
		body["price"] = strconv.FormatInt(product.Price, 10)
	}
	if product.Currency != "" {
		body["currency"] = product.Currency
	}
	if product.URL != "" {
		body["url"] = product.URL
	}
	if product.ImageURL != "" {
		body["image_url"] = product.ImageURL
	}
	if product.Description != "" {
		body["description"] = product.Description
	}

	_, err := c.doRequest(ctx, http.MethodPost, url, body, account)
	if err != nil {
		return fmt.Errorf("failed to update product via aisensy: %w", err)
	}

	return nil
}

// DeleteProduct deletes a product via AiSensy.
func (c *Client) DeleteProduct(ctx context.Context, account *whatsapp.Account, productID string) error {
	url := fmt.Sprintf("%s/product/%s/", c.baseURL, productID)

	_, err := c.doRequest(ctx, http.MethodDelete, url, nil, account)
	if err != nil {
		return fmt.Errorf("failed to delete product via aisensy: %w", err)
	}

	return nil
}
