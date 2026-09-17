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
	ID                     int             `json:"id"`
	Name                   string          `json:"name"`
	Description            string          `json:"description,omitempty"`
	ImageURL               string          `json:"image_url,omitempty"`
	MinPrice               float64         `json:"min_price"`
	MRP                    float64         `json:"mrp,omitempty"`
	Type                   string          `json:"type,omitempty"`
	PreparationTimeMinutes int             `json:"preparation_time_minutes,omitempty"`
	Options                []ProductOption `json:"options"`
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
	Store          int              `json:"store"`
	Items          []OrderItem      `json:"items"`
	Email          string           `json:"email,omitempty"`
	PhoneNumber    string           `json:"phone_number,omitempty"`
	DeliveryMode   string           `json:"delivery_mode,omitempty"`
	Address        *int             `json:"address,omitempty"`
	NewAddress     map[string]any   `json:"new_address,omitempty"`
	BuyerMetaData  map[string]any   `json:"buyer_meta_data,omitempty"`
	Addons         []map[string]any `json:"addons,omitempty"`
	Notes          string           `json:"notes,omitempty"`
	SlotToken      string           `json:"slot_token,omitempty"`
	IdempotencyKey string           `json:"idempotency_key,omitempty"`
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

// ListCategoriesParams filters GET /service/buyer/store/{id}/category/.
type ListCategoriesParams struct {
	Search string
	Tags   []string
	TagsOp string
	Limit  int
	Offset int
}

// ListCategories returns a page shaped like MCP list_categories: categories + count/limit/offset.
func (c *Client) ListCategories(ctx context.Context, storeID string, params ListCategoriesParams) (map[string]any, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store_id is required")
	}
	q := url.Values{}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		q.Set("offset", strconv.Itoa(params.Offset))
	}
	if s := strings.TrimSpace(params.Search); s != "" {
		q.Set("search", s)
	}
	if len(params.Tags) > 0 {
		q.Set("tags", strings.Join(params.Tags, ","))
	}
	if op := strings.TrimSpace(params.TagsOp); op != "" {
		q.Set("tags_op", op)
	}
	path := "/service/buyer/store/" + url.PathEscape(storeID) + "/category/"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	return c.getPageAs(ctx, path, "categories", params.Limit, params.Offset)
}

// ListProductsParams filters GET /service/buyer/product/.
type ListProductsParams struct {
	Search     string
	CategoryID string
	Limit      int
	Offset     int
}

// ListProductsPage returns products + pagination metadata (MCP-compatible keys).
func (c *Client) ListProductsPage(ctx context.Context, storeID string, params ListProductsParams) (map[string]any, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store_id is required")
	}
	q := url.Values{}
	q.Set("store_id", storeID)
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		q.Set("offset", strconv.Itoa(params.Offset))
	}
	if s := strings.TrimSpace(params.Search); s != "" {
		q.Set("search", s)
	}
	if id := strings.TrimSpace(params.CategoryID); id != "" {
		q.Set("category_id", id)
	}
	return c.getPageAs(ctx, "/service/buyer/product/?"+q.Encode(), "products", params.Limit, params.Offset)
}

// ListProductOptions lists options optionally filtered by store and ids.
func (c *Client) ListProductOptions(ctx context.Context, storeID string, ids []int) (any, error) {
	q := url.Values{}
	if strings.TrimSpace(storeID) != "" {
		q.Set("store_id", strings.TrimSpace(storeID))
	}
	if len(ids) > 0 {
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, strconv.Itoa(id))
		}
		q.Set("ids", strings.Join(parts, ","))
	}
	path := "/service/buyer/product-option/"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var raw any
	if err := c.getJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	switch t := raw.(type) {
	case []any:
		return t, nil
	case map[string]any:
		if results, ok := t["results"]; ok {
			return results, nil
		}
		return t, nil
	default:
		return raw, nil
	}
}

// GetStore fetches store details.
func (c *Client) GetStore(ctx context.Context, storeID string) (map[string]any, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store_id is required")
	}
	var raw map[string]any
	if err := c.getJSON(ctx, "/service/buyer/store/"+url.PathEscape(storeID)+"/", &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// GetStoreInfo fetches store policy / about info.
func (c *Client) GetStoreInfo(ctx context.Context, storeID string) (map[string]any, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store_id is required")
	}
	path := "/service/buyer/store/" + url.PathEscape(storeID) + "/store-info/"
	var page struct {
		Results []map[string]any `json:"results"`
	}
	if err := c.getJSON(ctx, path, &page); err == nil && len(page.Results) > 0 {
		return page.Results[0], nil
	}
	var raw map[string]any
	if err := c.getJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	if results, ok := raw["results"].([]any); ok && len(results) > 0 {
		if m, ok := results[0].(map[string]any); ok {
			return m, nil
		}
	}
	return raw, nil
}

// ListFaqs returns active FAQs for a store (array).
func (c *Client) ListFaqs(ctx context.Context, storeID string) (any, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store_id is required")
	}
	path := "/service/buyer/store/" + url.PathEscape(storeID) + "/faq/"
	var page struct {
		Results []map[string]any `json:"results"`
	}
	if err := c.getJSON(ctx, path, &page); err != nil {
		return nil, err
	}
	if page.Results != nil {
		return page.Results, nil
	}
	return []map[string]any{}, nil
}

func (c *Client) getPageAs(ctx context.Context, path, listKey string, limit, offset int) (map[string]any, error) {
	var page struct {
		Results  []map[string]any `json:"results"`
		Count    int              `json:"count"`
		Next     *string          `json:"next"`
		Previous *string          `json:"previous"`
	}
	if err := c.getJSON(ctx, path, &page); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = len(page.Results)
	}
	out := map[string]any{
		listKey:  page.Results,
		"count":  page.Count,
		"limit":  limit,
		"offset": offset,
	}
	if page.Count == 0 {
		out["count"] = len(page.Results)
	}
	hasMore := page.Next != nil && *page.Next != ""
	if !hasMore && page.Count > 0 {
		hasMore = offset+len(page.Results) < page.Count
	}
	out["has_more"] = hasMore
	return out, nil
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
// product payload. Prefers images[].original_url (JPEG/PNG source) over the
// optimized display fields (images[].url / images[].image), which are often WebP.
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
		if url := firstHTTPURL(m, "original_url", "url", "image"); url != "" {
			return url
		}
	}
	return ""
}

func firstHTTPURL(m map[string]any, keys ...string) string {
	for _, key := range keys {
		url := strings.TrimSpace(asString(m[key]))
		if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
			return url
		}
	}
	return ""
}

// CompactProduct builds a tool-friendly product summary with prices in rupees.
func CompactProduct(raw map[string]any) ProductSummary {
	p := ProductSummary{
		ID:                     asInt(raw["id"]),
		Name:                   asString(raw["name"]),
		Description:            asString(raw["description"]),
		ImageURL:               ExtractProductImageURL(raw),
		MinPrice:               PaiseToRupees(asFloat(raw["min_price"])),
		MRP:                    PaiseToRupees(asFloat(raw["mrp"])),
		Type:                   asString(raw["type"]),
		PreparationTimeMinutes: asInt(raw["preparation_time_minutes"]),
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
