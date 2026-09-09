// Package tickermcp calls ticker-events buyer tools over the MCP streamable-HTTP transport.
package tickermcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shridarpatil/whatomate/pkg/ticker"
)

const (
	defaultTimeout = 30 * time.Second
	storeCacheTTL  = 30 * time.Second
)

// Client calls tiqr-buyer MCP tools (list_products, get_product, create_order, get_order).
type Client struct {
	Endpoint   string
	APIKey     string
	HTTPClient *http.Client

	mu         sync.Mutex
	session    *mcp.ClientSession
	storeCache map[string]storeCacheEntry
}

type storeCacheEntry struct {
	value     map[string]any
	expiresAt time.Time
}

// PageMetadata preserves stable pagination information returned by MCP list tools.
type PageMetadata struct {
	Count    int    `json:"count,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	Next     string `json:"next,omitempty"`
	Previous string `json:"previous,omitempty"`
	HasMore  bool   `json:"has_more,omitempty"`
}

// CaptureField is a validated category field the assistant may collect.
type CaptureField struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
	HelpText string   `json:"help_text,omitempty"`
}

// Category is the authenticated collection contract exposed to Whatomate.
type Category struct {
	ID                    int            `json:"id"`
	Name                  string         `json:"name"`
	Description           string         `json:"description"`
	ListingPriority       int            `json:"listing_priority"`
	Image                 string         `json:"image"`
	Tags                  []string       `json:"tags,omitempty"`
	AIInstructions        string         `json:"ai_instructions,omitempty"`
	RequiredCaptureFields []CaptureField `json:"required_capture_fields,omitempty"`
	HandoffPolicy         string         `json:"handoff_policy,omitempty"`
	HandoffMessage        string         `json:"handoff_message,omitempty"`
	VisualTags            []string       `json:"visual_tags,omitempty"`
}

type CategoryPage struct {
	Results []Category `json:"results"`
	PageMetadata
}

type ProductPage struct {
	Results []ticker.ProductSummary `json:"results"`
	PageMetadata
}

type FulfillmentSlot struct {
	RequestedFulfillmentAt string `json:"requested_fulfillment_at"`
	PromisedReadyAt        string `json:"promised_ready_at"`
	Timezone               string `json:"timezone"`
	DeliveryMode           string `json:"delivery_mode"`
	PreparationTimeMinutes int    `json:"preparation_time_minutes"`
	Token                  string `json:"token"`
}

type FulfillmentSlotList struct {
	StoreID int               `json:"store_id"`
	Slots   []FulfillmentSlot `json:"slots"`
}

type FulfillmentSlotValidation struct {
	Valid                  bool   `json:"valid"`
	RequestedFulfillmentAt string `json:"requested_fulfillment_at"`
	PromisedReadyAt        string `json:"promised_ready_at"`
}

type CustomerAddress struct {
	ID                int            `json:"id"`
	Name              string         `json:"name"`
	Phone             string         `json:"phone"`
	AddressLine1      string         `json:"address_line_1"`
	AddressLine2      *string        `json:"address_line_2,omitempty"`
	City              string         `json:"city"`
	State             string         `json:"state"`
	Country           string         `json:"country"`
	Pincode           string         `json:"pincode"`
	Landmark          string         `json:"landmark,omitempty"`
	Latitude          *float64       `json:"latitude,omitempty"`
	Longitude         *float64       `json:"longitude,omitempty"`
	MetaData          map[string]any `json:"meta_data,omitempty"`
	AuthorizedAddress bool           `json:"authorized_address"`
}

type PaymentRetry struct {
	OrderUUID  string  `json:"order_uuid"`
	Status     string  `json:"status"`
	Retryable  bool    `json:"retryable"`
	PaymentURL *string `json:"payment_url,omitempty"`
}

// NewClient returns an MCP client for the given streamable-HTTP endpoint
// (e.g. http://127.0.0.1:8100/mcp). If the URL has an empty path, /mcp is appended.
func NewClient(endpoint, apiKey string, httpClient *http.Client) *Client {
	endpoint = normalizeEndpoint(endpoint)
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	if apiKey != "" {
		base := httpClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		httpClient = &http.Client{
			Timeout: httpClient.Timeout,
			Transport: &headerRoundTripper{
				base:   base,
				apiKey: apiKey,
			},
		}
	}
	return &Client{
		Endpoint:   endpoint,
		APIKey:     apiKey,
		HTTPClient: httpClient,
		storeCache: make(map[string]storeCacheEntry),
	}
}

type headerRoundTripper struct {
	base   http.RoundTripper
	apiKey string
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("X-MCP-API-Key", rt.apiKey)
	return rt.base.RoundTrip(r)
}

func normalizeEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return strings.TrimRight(raw, "/")
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/mcp"
	}
	return strings.TrimRight(u.String(), "/")
}

// SearchProducts maps to MCP list_products and returns compact summaries (prices in rupees).
func (c *Client) SearchProducts(ctx context.Context, storeID, search string, limit int) ([]ticker.ProductSummary, error) {
	page, err := c.ListProducts(ctx, storeID, search, "", limit, 0)
	return page.Results, err
}

// ListProducts maps to MCP list_products with category filtering and stable pagination.
func (c *Client) ListProducts(ctx context.Context, storeID, search, categoryID string, limit, offset int) (ProductPage, error) {
	sid, err := strconv.Atoi(strings.TrimSpace(storeID))
	if err != nil || sid <= 0 {
		return ProductPage{}, fmt.Errorf("store_id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	args, err := productListArgs(sid, search, categoryID, limit, offset)
	if err != nil {
		return ProductPage{}, err
	}
	raw, err := c.callTool(ctx, "list_products", args)
	if err != nil {
		return ProductPage{}, err
	}
	items, meta, err := asObjectPage(raw, "products")
	if err != nil {
		return ProductPage{}, err
	}
	out := make([]ticker.ProductSummary, 0, len(items))
	for _, item := range items {
		out = append(out, ticker.CompactProduct(item))
	}
	meta.Limit = defaultInt(meta.Limit, limit)
	meta.Offset = defaultInt(meta.Offset, offset)
	return ProductPage{Results: out, PageMetadata: meta}, nil
}

// GetProduct maps to MCP get_product.
func (c *Client) GetProduct(ctx context.Context, productID string) (map[string]any, error) {
	pid, err := strconv.Atoi(strings.TrimSpace(productID))
	if err != nil || pid <= 0 {
		return nil, fmt.Errorf("product_id is required")
	}
	raw, err := c.callTool(ctx, "get_product", map[string]any{"product_id": pid})
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected get_product result type %T", raw)
	}
	return m, nil
}

// GetStore maps to MCP get_store and returns a compact store summary for the LLM.
func (c *Client) GetStore(ctx context.Context, storeID string) (map[string]any, error) {
	sid, err := strconv.Atoi(strings.TrimSpace(storeID))
	if err != nil || sid <= 0 {
		return nil, fmt.Errorf("store_id is required")
	}
	cacheKey := strconv.Itoa(sid)
	c.mu.Lock()
	if cached, ok := c.storeCache[cacheKey]; ok && time.Now().Before(cached.expiresAt) {
		value := cloneMap(cached.value)
		c.mu.Unlock()
		return value, nil
	}
	c.mu.Unlock()

	raw, err := c.callTool(ctx, "get_store", map[string]any{"store_id": sid})
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected get_store result type %T", raw)
	}
	store := CompactStore(m)
	c.mu.Lock()
	c.storeCache[cacheKey] = storeCacheEntry{
		value:     cloneMap(store),
		expiresAt: time.Now().Add(storeCacheTTL),
	}
	c.mu.Unlock()
	return store, nil
}

// ListCategories maps to MCP list_categories and returns compact category rows.
func (c *Client) ListCategories(ctx context.Context, storeID string) ([]map[string]any, error) {
	page, err := c.ListCategoryPage(ctx, storeID, "", 50, 0)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(page.Results))
	for _, category := range page.Results {
		out = append(out, categoryMap(category))
	}
	return out, nil
}

// ListCategoryPage maps to MCP list_categories and preserves paging metadata.
// categoryID is optional and is useful for loading one selected category's config.
func (c *Client) ListCategoryPage(ctx context.Context, storeID, categoryID string, limit, offset int) (CategoryPage, error) {
	sid, err := strconv.Atoi(strings.TrimSpace(storeID))
	if err != nil || sid <= 0 {
		return CategoryPage{}, fmt.Errorf("store_id is required")
	}
	if limit <= 0 {
		limit = 50
	}
	args, err := categoryListArgs(sid, categoryID, limit, offset)
	if err != nil {
		return CategoryPage{}, err
	}
	raw, err := c.callTool(ctx, "list_categories", args)
	if err != nil {
		return CategoryPage{}, err
	}
	items, meta, err := asObjectPage(raw, "categories")
	if err != nil {
		return CategoryPage{}, err
	}
	out := make([]Category, 0, len(items))
	for _, item := range items {
		out = append(out, decodeCategory(item))
	}
	meta.Limit = defaultInt(meta.Limit, limit)
	meta.Offset = defaultInt(meta.Offset, offset)
	return CategoryPage{Results: out, PageMetadata: meta}, nil
}

// CheckDeliveryEligibility maps to MCP check_delivery_eligibility.
func (c *Client) CheckDeliveryEligibility(ctx context.Context, storeID string, latitude, longitude float64) (map[string]any, error) {
	sid, err := strconv.Atoi(strings.TrimSpace(storeID))
	if err != nil || sid <= 0 {
		return nil, fmt.Errorf("store_id is required")
	}
	raw, err := c.callTool(ctx, "check_delivery_eligibility", map[string]any{
		"store_id":  sid,
		"latitude":  latitude,
		"longitude": longitude,
	})
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected check_delivery_eligibility result type %T", raw)
	}
	return m, nil
}

func (c *Client) ListFulfillmentSlots(ctx context.Context, storeID, deliveryMode string, productOptionIDs []int) (FulfillmentSlotList, error) {
	sid, err := positiveStoreID(storeID)
	if err != nil {
		return FulfillmentSlotList{}, err
	}
	args := map[string]any{"store_id": sid, "delivery_mode": strings.TrimSpace(deliveryMode)}
	if len(productOptionIDs) > 0 {
		args["product_option_ids"] = productOptionIDs
	}
	raw, err := c.callTool(ctx, "list_fulfillment_slots", args)
	if err != nil {
		return FulfillmentSlotList{}, err
	}
	var result FulfillmentSlotList
	if err := decodeInto(raw, &result); err != nil {
		return result, fmt.Errorf("decode fulfillment slots: %w", err)
	}
	return result, nil
}

func (c *Client) ValidateFulfillmentSlot(ctx context.Context, storeID, deliveryMode, token string, productOptionIDs []int) (FulfillmentSlotValidation, error) {
	sid, err := positiveStoreID(storeID)
	if err != nil {
		return FulfillmentSlotValidation{}, err
	}
	args := map[string]any{"store_id": sid, "delivery_mode": strings.TrimSpace(deliveryMode), "token": strings.TrimSpace(token)}
	if len(productOptionIDs) > 0 {
		args["product_option_ids"] = productOptionIDs
	}
	raw, err := c.callTool(ctx, "validate_fulfillment_slot", args)
	if err != nil {
		return FulfillmentSlotValidation{}, err
	}
	var result FulfillmentSlotValidation
	err = decodeInto(raw, &result)
	return result, err
}

func (c *Client) ListCustomerAddresses(ctx context.Context, storeID, phoneNumber string) ([]CustomerAddress, error) {
	sid, err := positiveStoreID(storeID)
	if err != nil {
		return nil, err
	}
	raw, err := c.callTool(ctx, "list_customer_addresses", map[string]any{
		"store_id": sid, "phone_number": strings.TrimSpace(phoneNumber),
	})
	if err != nil {
		return nil, err
	}
	var result []CustomerAddress
	err = decodeInto(raw, &result)
	return result, err
}

func (c *Client) CreateCustomerAddress(ctx context.Context, storeID, phoneNumber string, address map[string]any) (CustomerAddress, error) {
	sid, err := positiveStoreID(storeID)
	if err != nil {
		return CustomerAddress{}, err
	}
	raw, err := c.callTool(ctx, "create_customer_address", map[string]any{
		"store_id": sid, "phone_number": strings.TrimSpace(phoneNumber), "address": address,
	})
	if err != nil {
		return CustomerAddress{}, err
	}
	var result CustomerAddress
	err = decodeInto(raw, &result)
	return result, err
}

func (c *Client) RetryPayment(ctx context.Context, orderUUID string) (PaymentRetry, error) {
	raw, err := c.callTool(ctx, "retry_payment", map[string]any{"order_uuid": strings.TrimSpace(orderUUID)})
	if err != nil {
		return PaymentRetry{}, err
	}
	var result PaymentRetry
	err = decodeInto(raw, &result)
	return result, err
}

// CompactStore keeps name, description, address, country, delivery modes, and delivery radii.
func CompactStore(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if id, ok := m["id"]; ok {
		out["id"] = id
	}
	if name, ok := m["name"]; ok {
		out["name"] = name
	}
	if desc, ok := m["description"]; ok {
		out["description"] = desc
	}
	if modes, ok := m["delivery_modes"]; ok {
		out["delivery_modes"] = modes
	}
	if version, ok := m["commerce_contract_version"]; ok {
		out["commerce_contract_version"] = version
	}
	if capabilities, ok := m["capabilities"]; ok {
		out["capabilities"] = capabilities
	}
	for _, key := range []string{
		"address",
		"country",
		"latitude",
		"longitude",
		"delivery_radius",
		"free_delivery_radius",
		"location_based_delivery",
	} {
		if v, ok := m[key]; ok && v != nil {
			out[key] = v
		}
	}
	return out
}

// CompactCategory keeps the full authenticated collection contract.
func CompactCategory(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return categoryMap(decodeCategory(m))
}

// LookupOrderStatus maps to MCP lookup_order_status (phone-verified buyer lookup).
func (c *Client) LookupOrderStatus(ctx context.Context, storeID, phoneNumber, orderID string) (map[string]any, error) {
	sid, err := strconv.Atoi(strings.TrimSpace(storeID))
	if err != nil || sid <= 0 {
		return nil, fmt.Errorf("store_id is required")
	}
	phoneNumber = strings.TrimSpace(phoneNumber)
	if phoneNumber == "" {
		return nil, fmt.Errorf("phone_number is required")
	}
	args := map[string]any{
		"store_id":     sid,
		"phone_number": phoneNumber,
	}
	orderID = strings.TrimSpace(orderID)
	if orderID != "" {
		args["order_display_id"] = orderID
	}
	raw, err := c.callTool(ctx, "lookup_order_status", args)
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected lookup_order_status result type %T", raw)
	}
	return m, nil
}

// GetOrder maps to MCP get_order.
func (c *Client) GetOrder(ctx context.Context, orderUUID string) (map[string]any, error) {
	orderUUID = strings.TrimSpace(orderUUID)
	if orderUUID == "" {
		return nil, fmt.Errorf("order_uuid is required")
	}
	raw, err := c.callTool(ctx, "get_order", map[string]any{"order_uuid": orderUUID})
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected get_order result type %T", raw)
	}
	return m, nil
}

// CreateOrder maps to MCP create_order (nested order payload).
func (c *Client) CreateOrder(ctx context.Context, body ticker.CreateOrderRequest) (map[string]any, error) {
	order := map[string]any{
		"store":         body.Store,
		"items":         body.Items,
		"email":         body.Email,
		"phone_number":  body.PhoneNumber,
		"delivery_mode": body.DeliveryMode,
	}
	if body.NewAddress != nil {
		order["new_address"] = body.NewAddress
	}
	if body.Address != nil {
		order["address"] = *body.Address
	}
	if body.BuyerMetaData != nil {
		order["buyer_meta_data"] = body.BuyerMetaData
	}
	if len(body.Addons) > 0 {
		order["addons"] = body.Addons
	}
	if body.Notes != "" {
		order["notes"] = body.Notes
	}
	if body.SlotToken != "" {
		order["slot_token"] = body.SlotToken
	}
	if body.IdempotencyKey != "" {
		order["idempotency_key"] = body.IdempotencyKey
	}
	raw, err := c.callTool(ctx, "create_order", map[string]any{"order": order})
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected create_order result type %T", raw)
	}
	return m, nil
}

func (c *Client) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	if c == nil || c.Endpoint == "" {
		return nil, fmt.Errorf("mcp client is not configured")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.callToolLocked(ctx, name, args)
	if err != nil && isReadOnlyTool(name) {
		c.closeSessionLocked()
		result, err = c.callToolLocked(ctx, name, args)
	}
	if err != nil {
		c.closeSessionLocked()
		return nil, fmt.Errorf("mcp tool %s: %w", name, err)
	}
	if result.IsError {
		return nil, fmt.Errorf("mcp tool %s failed: %s", name, toolErrorText(result))
	}
	return parseToolResult(result)
}

func (c *Client) callToolLocked(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	if c.session == nil {
		client := mcp.NewClient(&mcp.Implementation{
			Name:    "whatomate-commerce",
			Version: "1.0.0",
		}, nil)
		transport := &mcp.StreamableClientTransport{
			Endpoint:             c.Endpoint,
			HTTPClient:           c.HTTPClient,
			DisableStandaloneSSE: true, // FastMCP runs with stateless_http=True
			MaxRetries:           -1,
		}
		session, err := client.Connect(ctx, transport, nil)
		if err != nil {
			return nil, fmt.Errorf("mcp connect: %w", err)
		}
		c.session = session
	}

	return c.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
}

// Close releases the reusable MCP session. A later call can reconnect.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeSessionLocked()
}

func (c *Client) closeSessionLocked() error {
	if c.session == nil {
		return nil
	}
	err := c.session.Close()
	c.session = nil
	return err
}

func isReadOnlyTool(name string) bool {
	switch name {
	case "get_store", "list_categories", "list_products", "get_product",
		"check_delivery_eligibility", "lookup_order_status", "get_order",
		"list_fulfillment_slots", "validate_fulfillment_slot", "list_customer_addresses":
		return true
	default:
		return false
	}
}

func positiveStoreID(storeID string) (int, error) {
	sid, err := strconv.Atoi(strings.TrimSpace(storeID))
	if err != nil || sid <= 0 {
		return 0, fmt.Errorf("store_id is required")
	}
	return sid, nil
}

func decodeInto(value, destination any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, destination)
}

func parseToolResult(result *mcp.CallToolResult) (any, error) {
	if result == nil {
		return nil, fmt.Errorf("empty mcp tool result")
	}
	if result.StructuredContent != nil {
		if err := toolPayloadError(result.StructuredContent); err != nil {
			return nil, err
		}
		return normalizeJSON(result.StructuredContent), nil
	}
	var texts []string
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok && tc.Text != "" {
			texts = append(texts, tc.Text)
		}
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("mcp tool returned no content")
	}

	// Prefer a single JSON value (object/array). FastMCP often emits one TextContent
	// per list item, so fall back to decoding each part and assembling an array.
	joined := strings.Join(texts, "\n")
	var decoded any
	if err := json.Unmarshal([]byte(joined), &decoded); err == nil {
		if err := toolPayloadError(decoded); err != nil {
			return nil, err
		}
		return normalizeJSON(decoded), nil
	}

	if len(texts) > 1 {
		items := make([]any, 0, len(texts))
		for _, t := range texts {
			var part any
			if err := json.Unmarshal([]byte(t), &part); err != nil {
				return nil, fmt.Errorf("%s", joined)
			}
			if err := toolPayloadError(part); err != nil {
				return nil, err
			}
			items = append(items, normalizeJSON(part))
		}
		return items, nil
	}

	// Non-JSON text — surface as error string for the LLM.
	return nil, fmt.Errorf("%s", joined)
}

func toolPayloadError(v any) error {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	errVal, hasErr := m["error"]
	if !hasErr || errVal == nil {
		return nil
	}
	msg := strings.TrimSpace(fmt.Sprint(errVal))
	if msg == "" || msg == "<nil>" {
		return nil
	}
	if details, ok := m["details"]; ok && details != nil {
		return fmt.Errorf("%s: %v", msg, details)
	}
	return fmt.Errorf("%s", msg)
}

func toolErrorText(result *mcp.CallToolResult) string {
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok && tc.Text != "" {
			return tc.Text
		}
	}
	return "unknown error"
}

func normalizeJSON(v any) any {
	switch t := v.(type) {
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(t, &decoded); err != nil {
			return v
		}
		return normalizeJSON(decoded)
	default:
		return v
	}
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		switch typed := value.(type) {
		case map[string]any:
			dst[key] = cloneMap(typed)
		case []any:
			items := make([]any, len(typed))
			copy(items, typed)
			dst[key] = items
		default:
			dst[key] = value
		}
	}
	return dst
}

func asObjectList(v any) ([]map[string]any, error) {
	switch t := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("unexpected list item type %T", item)
			}
			out = append(out, m)
		}
		return out, nil
	case []map[string]any:
		return t, nil
	case map[string]any:
		// Some gateways wrap lists as {results:[...]} or {products:[...]}.
		for _, key := range []string{"results", "products", "data", "items"} {
			if nested, ok := t[key]; ok {
				return asObjectList(nested)
			}
		}
		// FastMCP emits one TextContent per list item. A single-item list
		// therefore arrives as a bare product object, not an array.
		if looksLikeProductObject(t) {
			return []map[string]any{t}, nil
		}
		return nil, fmt.Errorf("expected product list, got object")
	case nil:
		return []map[string]any{}, nil
	default:
		return nil, fmt.Errorf("expected product list, got %T", v)
	}
}

func productListArgs(storeID int, search, categoryID string, limit, offset int) (map[string]any, error) {
	if storeID <= 0 {
		return nil, fmt.Errorf("store_id is required")
	}
	args := map[string]any{"store_id": storeID, "limit": limit, "offset": offset}
	if q := strings.TrimSpace(search); q != "" {
		args["search"] = q
	}
	if raw := strings.TrimSpace(categoryID); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("category_id must be a positive integer")
		}
		args["category_id"] = id
	}
	return args, nil
}

func categoryListArgs(storeID int, categoryID string, limit, offset int) (map[string]any, error) {
	if storeID <= 0 {
		return nil, fmt.Errorf("store_id is required")
	}
	args := map[string]any{"store_id": storeID, "limit": limit, "offset": offset}
	if raw := strings.TrimSpace(categoryID); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("category_id must be a positive integer")
		}
		args["category_id"] = id
	}
	return args, nil
}

func asObjectPage(v any, preferredKey string) ([]map[string]any, PageMetadata, error) {
	meta := PageMetadata{}
	if m, ok := v.(map[string]any); ok {
		meta.Count = asInt(m["count"])
		meta.Limit = asInt(m["limit"])
		meta.Offset = asInt(m["offset"])
		meta.Next = stringValue(m["next"])
		meta.Previous = stringValue(m["previous"])
		if b, ok := m["has_more"].(bool); ok {
			meta.HasMore = b
		}
		keys := []string{preferredKey, "results", "data", "items", "products", "categories"}
		for _, key := range keys {
			if nested, exists := m[key]; exists {
				items, err := asObjectList(nested)
				if err != nil {
					return nil, meta, err
				}
				if meta.Count == 0 {
					meta.Count = len(items)
				}
				if !meta.HasMore {
					meta.HasMore = meta.Next != "" || meta.Offset+len(items) < meta.Count
				}
				return items, meta, nil
			}
		}
	}
	items, err := asObjectList(v)
	if err != nil {
		return nil, meta, err
	}
	meta.Count = len(items)
	return items, meta, nil
}

func decodeCategory(m map[string]any) Category {
	var category Category
	// Decode the typed fields without image first. Backend's ImageMediaReadSerializer
	// returns image as an object, while older MCP versions returned a URL string.
	fields := make(map[string]any, len(m))
	for key, value := range m {
		if key != "image" {
			fields[key] = value
		}
	}
	data, err := json.Marshal(fields)
	if err == nil {
		_ = json.Unmarshal(data, &category)
	}
	if category.ID == 0 {
		category.ID = asInt(m["id"])
	}
	category.Image = categoryImageURL(m["image"])
	category.HandoffPolicy = strings.ToLower(strings.TrimSpace(category.HandoffPolicy))
	if category.HandoffPolicy != "after_capture" {
		category.HandoffPolicy = "none"
	}
	valid := make([]CaptureField, 0, len(category.RequiredCaptureFields))
	for _, field := range category.RequiredCaptureFields {
		field.Key = strings.TrimSpace(field.Key)
		field.Label = strings.TrimSpace(field.Label)
		field.Type = strings.TrimSpace(field.Type)
		field.HelpText = strings.TrimSpace(field.HelpText)
		if field.Key == "" || field.Label == "" || field.Type == "" {
			continue
		}
		valid = append(valid, field)
	}
	category.RequiredCaptureFields = valid
	return category
}

func categoryImageURL(value any) string {
	switch image := value.(type) {
	case string:
		return strings.TrimSpace(image)
	case map[string]any:
		for _, key := range []string{"image", "url", "original_url"} {
			if candidate := categoryImageURL(image[key]); candidate != "" {
				return candidate
			}
		}
	case json.RawMessage:
		var decoded any
		if json.Unmarshal(image, &decoded) == nil {
			return categoryImageURL(decoded)
		}
	}
	return ""
}

func categoryMap(category Category) map[string]any {
	data, _ := json.Marshal(category)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	// Tags are used internally for visual handoff routing, not rendered in the
	// compact category payload shown to the shopper.
	delete(out, "tags")
	delete(out, "visual_tags")
	return out
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "<nil>" {
		return ""
	}
	return s
}

func defaultInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func looksLikeProductObject(m map[string]any) bool {
	if m == nil {
		return false
	}
	_, hasID := m["id"]
	_, hasName := m["name"]
	return hasID && hasName
}
