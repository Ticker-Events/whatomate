// Package ticker is an HTTP client for ticker-events buyer APIs.
package ticker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout   = 30 * time.Second
	maxResponseBytes = 1 << 20 // 1MB
)

// Client calls ticker-events buyer endpoints.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient returns a client for the given origin (no trailing slash).
func NewClient(baseURL string, httpClient *http.Client) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{BaseURL: baseURL, HTTPClient: httpClient}
}

// ProductOption is a compact option for tool results.
type ProductOption struct {
	ID                int     `json:"id"`
	Name              string  `json:"name"`
	Price             float64 `json:"price"`
	MRP               float64 `json:"mrp,omitempty"`
	AvailableQuantity *int    `json:"available_quantity,omitempty"`
	StockStatus       string  `json:"stock_status,omitempty"`
}

// ProductSummary is a compact product for search results.
type ProductSummary struct {
	ID          int             `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	ImageURL    string          `json:"image_url,omitempty"`
	MinPrice    float64         `json:"min_price"`
	MRP         float64         `json:"mrp,omitempty"`
	Type        string          `json:"type,omitempty"`
	Options     []ProductOption `json:"options"`
}

// SearchProducts lists active products for a store, optionally filtered by search.
func (c *Client) SearchProducts(ctx context.Context, storeID, search string, limit int) ([]ProductSummary, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store_id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	q := url.Values{}
	q.Set("store_id", storeID)
	q.Set("limit", strconv.Itoa(limit))
	if strings.TrimSpace(search) != "" {
		q.Set("search", strings.TrimSpace(search))
	}

	var page struct {
		Results []map[string]any `json:"results"`
		Count   int              `json:"count"`
	}
	if err := c.getJSON(ctx, "/service/buyer/product/?"+q.Encode(), &page); err != nil {
		return nil, err
	}

	out := make([]ProductSummary, 0, len(page.Results))
	for _, raw := range page.Results {
		out = append(out, CompactProduct(raw))
	}
	return out, nil
}

// GetProduct fetches a single product by ID.
func (c *Client) GetProduct(ctx context.Context, productID string) (map[string]any, error) {
	if productID == "" {
		return nil, fmt.Errorf("product_id is required")
	}
	var raw map[string]any
	if err := c.getJSON(ctx, "/service/buyer/product/"+url.PathEscape(productID)+"/", &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// CreateOrderRequest is the guest checkout body for POST /service/buyer/order/.
type CreateOrderRequest struct {
	Store         int              `json:"store"`
	Items         []OrderItem      `json:"items"`
	Email         string           `json:"email,omitempty"`
	PhoneNumber   string           `json:"phone_number,omitempty"`
	DeliveryMode  string           `json:"delivery_mode,omitempty"`
	NewAddress    map[string]any   `json:"new_address,omitempty"`
	BuyerMetaData map[string]any   `json:"buyer_meta_data,omitempty"`
	Addons        []map[string]any `json:"addons,omitempty"`
}

// OrderItem is a line item on create order.
type OrderItem struct {
	ProductOption int `json:"product_option"`
	Quantity      int `json:"quantity"`
}

// CreateOrder places a guest order.
func (c *Client) CreateOrder(ctx context.Context, body CreateOrderRequest) (map[string]any, error) {
	var raw map[string]any
	if err := c.postJSON(ctx, "/service/buyer/order/", body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// GetOrder fetches an order by uuid.
func (c *Client) GetOrder(ctx context.Context, orderUUID string) (map[string]any, error) {
	if orderUUID == "" {
		return nil, fmt.Errorf("order_uuid is required")
	}
	var raw map[string]any
	if err := c.getJSON(ctx, "/service/buyer/order/"+url.PathEscape(orderUUID)+"/", &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	return c.doJSON(req, dest)
}

func (c *Client) postJSON(ctx context.Context, path string, body any, dest any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.doJSON(req, dest)
}

func (c *Client) doJSON(req *http.Request, dest any) error {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("response too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 500 {
			msg = msg[:500] + "…"
		}
		return fmt.Errorf("ticker api error %d: %s", resp.StatusCode, msg)
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("failed to parse ticker response: %w", err)
	}
	return nil
}

// PaiseToRupees converts ticker API money (integer paise) to rupees.
func PaiseToRupees(paise float64) float64 {
	return paise / 100
}

// ExtractProductImageURL returns the first HTTPS product image from a ticker
// product payload (images[].image), or empty when none is present.
func ExtractProductImageURL(raw map[string]any) string {
	if raw == nil {
		return ""
	}
	images, ok := raw["images"].([]any)
	if !ok || len(images) == 0 {
		return ""
	}
	for _, item := range images {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		url := strings.TrimSpace(asString(m["image"]))
		if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
			return url
		}
	}
	return ""
}

// CompactProduct builds a tool-friendly product summary with prices in rupees.
func CompactProduct(raw map[string]any) ProductSummary {
	p := ProductSummary{
		ID:          asInt(raw["id"]),
		Name:        asString(raw["name"]),
		Description: asString(raw["description"]),
		ImageURL:    ExtractProductImageURL(raw),
		MinPrice:    PaiseToRupees(asFloat(raw["min_price"])),
		MRP:         PaiseToRupees(asFloat(raw["mrp"])),
		Type:        asString(raw["type"]),
	}
	var minFromOpts float64
	if opts, ok := raw["options"].([]any); ok {
		for _, o := range opts {
			om, ok := o.(map[string]any)
			if !ok {
				continue
			}
			opt := ProductOption{
				ID:          asInt(om["id"]),
				Name:        asString(om["name"]),
				Price:       PaiseToRupees(asFloat(om["price"])),
				MRP:         PaiseToRupees(asFloat(om["mrp"])),
				StockStatus: asString(om["stock_status"]),
			}
			if aq, ok := om["available_quantity"]; ok && aq != nil {
				v := asInt(aq)
				opt.AvailableQuantity = &v
			}
			p.Options = append(p.Options, opt)
			if opt.Price > 0 && (minFromOpts == 0 || opt.Price < minFromOpts) {
				minFromOpts = opt.Price
			}
		}
	}
	if p.MinPrice <= 0 && minFromOpts > 0 {
		p.MinPrice = minFromOpts
	}
	return p
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(t)
		return i
	default:
		return 0
	}
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}
