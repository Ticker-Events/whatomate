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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shridarpatil/whatomate/pkg/ticker"
)

const defaultTimeout = 30 * time.Second

// Client calls tiqr-buyer MCP tools (list_products, get_product, create_order, get_order).
type Client struct {
	Endpoint   string
	APIKey     string
	HTTPClient *http.Client
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
	return &Client{Endpoint: endpoint, APIKey: apiKey, HTTPClient: httpClient}
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
	sid, err := strconv.Atoi(strings.TrimSpace(storeID))
	if err != nil || sid <= 0 {
		return nil, fmt.Errorf("store_id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	args := map[string]any{
		"store_id": sid,
		"limit":    limit,
	}
	if q := strings.TrimSpace(search); q != "" {
		args["search"] = q
	}
	raw, err := c.callTool(ctx, "list_products", args)
	if err != nil {
		return nil, err
	}
	items, err := asObjectList(raw)
	if err != nil {
		return nil, err
	}
	out := make([]ticker.ProductSummary, 0, len(items))
	for _, item := range items {
		out = append(out, ticker.CompactProduct(item))
	}
	return out, nil
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
	raw, err := c.callTool(ctx, "get_store", map[string]any{"store_id": sid})
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected get_store result type %T", raw)
	}
	return CompactStore(m), nil
}

// ListCategories maps to MCP list_categories and returns compact category rows.
func (c *Client) ListCategories(ctx context.Context, storeID string) ([]map[string]any, error) {
	sid, err := strconv.Atoi(strings.TrimSpace(storeID))
	if err != nil || sid <= 0 {
		return nil, fmt.Errorf("store_id is required")
	}
	raw, err := c.callTool(ctx, "list_categories", map[string]any{"store_id": sid})
	if err != nil {
		return nil, err
	}
	items, err := asObjectList(raw)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, CompactCategory(item))
	}
	return out, nil
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

// CompactStore keeps only fields useful for a short store intro.
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
	for _, key := range []string{"latitude", "longitude", "delivery_radius", "free_delivery_radius"} {
		if v, ok := m[key]; ok && v != nil {
			out[key] = v
		}
	}
	return out
}

// CompactCategory keeps id/name/description/listing_priority for collection listings.
func CompactCategory(m map[string]any) map[string]any {
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
	if desc, ok := m["description"]; ok && desc != nil && fmt.Sprint(desc) != "" {
		out["description"] = desc
	}
	if prio, ok := m["listing_priority"]; ok && prio != nil {
		out["listing_priority"] = prio
	}
	return out
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
	if body.BuyerMetaData != nil {
		order["buyer_meta_data"] = body.BuyerMetaData
	}
	if len(body.Addons) > 0 {
		order["addons"] = body.Addons
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
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp tool %s: %w", name, err)
	}
	if result.IsError {
		return nil, fmt.Errorf("mcp tool %s failed: %s", name, toolErrorText(result))
	}
	return parseToolResult(result)
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

func looksLikeProductObject(m map[string]any) bool {
	if m == nil {
		return false
	}
	_, hasID := m["id"]
	_, hasName := m["name"]
	return hasID && hasName
}
