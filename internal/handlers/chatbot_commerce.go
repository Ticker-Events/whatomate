package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/shridarpatil/whatomate/pkg/tickermcp"
)

const (
	commerceMaxToolRounds  = 5
	commerceMaxResultBytes = 12 * 1024
	commerceSystemAddendum = `You are a sales assistant for this store only. Keep replies short for WhatsApp.

CRITICAL — live catalog/orders:
- For ANY question about products, catalog, prices, stock, options, or "what do you sell/have", you MUST call search_products (and get_product when you need detail) BEFORE answering.
- Never say you cannot retrieve products unless a tool returned an error. Do not invent a catalog from memory or static context.
- Static FAQ/context is secondary; tool results are the source of truth for products and orders.

Prices:
- Tool results already convert MCP amounts from paise to rupees. Quote every price/amount as ₹X.XX (Indian Rupees). Never show raw paise or divide again.

Memory — use the conversation history:
- Remember product, product_option id, quantity, email, phone, and delivery mode already provided. Do NOT re-ask for details the user already gave.
- Short replies like "2", "yes", "pickup", or an email address answer your last question — interpret them in that context.
- Only ask for the next missing field needed to place the order.

Tools:
- search_products: find products by query (or list items with an empty/broad query).
- get_product: fetch full details for a product id.
- get_order_status: look up an order by its uuid (internal id).
- create_order: place an order ONLY after the user explicitly confirms a summary you showed them. Always pass confirmed=true only after that confirmation.

Ordering rules:
- Use product_option ids from tool results (never invent ids or prices).
- Collect: product option id(s), quantity, email, and delivery_mode (PICKUP_FROM_STORE or DELIVERY_TO_LOCATION).
- For DELIVERY_TO_LOCATION, collect new_address (name, phone, address_line_1, city, state, country, pincode, email) and latitude/longitude in buyer_meta_data when needed for delivery radius.
- Prefer PICKUP_FROM_STORE when the user has no delivery address.
- Never invent stock or prices — rely on tool data.
- After a successful order, share display_uid (the customer-facing order number) — never share uuid to the user. If payment_url is present, share that link so they can pay.`
)

// commerceBackend is the storefront data source for LLM commerce tools (MCP).
type commerceBackend interface {
	SearchProducts(ctx context.Context, storeID, search string, limit int) ([]ticker.ProductSummary, error)
	GetProduct(ctx context.Context, productID string) (map[string]any, error)
	GetOrder(ctx context.Context, orderUUID string) (map[string]any, error)
	CreateOrder(ctx context.Context, body ticker.CreateOrderRequest) (map[string]any, error)
}

// commerceRuntime holds per-request commerce tool context.
type commerceRuntime struct {
	Client      commerceBackend
	StoreID     string
	PhoneNumber string
}

func commerceConfigured(ai models.AIConfig) bool {
	return ai.CommerceEnabled &&
		strings.TrimSpace(ai.CommerceMCPURL) != "" &&
		strings.TrimSpace(ai.CommerceStoreID) != ""
}

func (a *App) newCommerceRuntime(settings *models.ChatbotSettings, session *models.ChatbotSession) *commerceRuntime {
	if settings == nil || !commerceConfigured(settings.AI) {
		return nil
	}
	phone := ""
	if session != nil {
		phone = session.PhoneNumber
	}
	// Dedicated MCP HTTP client: admin-configured first-party endpoint.
	// App.HTTPClient uses SSRFSafeDialer which blocks loopback/private IPs.
	return &commerceRuntime{
		Client:      tickermcp.NewClient(settings.AI.CommerceMCPURL, settings.AI.CommerceMCPAPIKey, nil),
		StoreID:     strings.TrimSpace(settings.AI.CommerceStoreID),
		PhoneNumber: phone,
	}
}

func buildCommerceSystemPrompt(base, contextData string) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(base) != "" {
		parts = append(parts, strings.TrimSpace(base))
	}
	parts = append(parts, commerceSystemAddendum)
	if strings.TrimSpace(contextData) != "" {
		parts = append(parts, strings.TrimSpace(contextData))
	}
	return strings.Join(parts, "\n\n")
}

// commerceToolDefs returns OpenAI-style tool definitions (also mapped for other providers).
func commerceToolDefs() []map[string]any {
	return []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "search_products",
				"description": "Search or list products for the configured store. Prices in the result are in INR (rupees), already converted from paise.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Search text matching product or option names. Empty returns a page of products.",
						},
						"limit": map[string]any{
							"type":        "integer",
							"description": "Max products to return (default 20, max 50).",
						},
					},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "get_product",
				"description": "Get full details for a product by numeric id. Prices in the result are in INR (rupees).",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"product_id": map[string]any{
							"type":        "string",
							"description": "Product id from search_products.",
						},
					},
					"required": []string{"product_id"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "get_order_status",
				"description": "Get status for an existing order by uuid.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"order_uuid": map[string]any{
							"type":        "string",
							"description": "Order uuid returned when the order was created.",
						},
					},
					"required": []string{"order_uuid"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "create_order",
				"description": "Place a guest order. Call only after the user confirms the order summary. Requires confirmed=true. On success, show display_uid and payment_url to the user (not uuid).",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confirmed": map[string]any{
							"type":        "boolean",
							"description": "Must be true only after the user confirmed the order summary in chat.",
						},
						"items": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"product_option": map[string]any{"type": "integer"},
									"quantity":       map[string]any{"type": "integer"},
								},
								"required": []string{"product_option", "quantity"},
							},
						},
						"email": map[string]any{
							"type": "string",
						},
						"phone_number": map[string]any{
							"type": "string",
						},
						"delivery_mode": map[string]any{
							"type":        "string",
							"description": "PICKUP_FROM_STORE or DELIVERY_TO_LOCATION",
						},
						"new_address": map[string]any{
							"type":        "object",
							"description": "Required for guest delivery (and guest checkout address). Include email for guest.",
						},
						"buyer_meta_data": map[string]any{
							"type": "object",
						},
					},
					"required": []string{"confirmed", "items", "email", "delivery_mode"},
				},
			},
		},
	}
}

func anthropicToolDefs() []map[string]any {
	out := make([]map[string]any, 0, 4)
	for _, t := range commerceToolDefs() {
		fn, _ := t["function"].(map[string]any)
		out = append(out, map[string]any{
			"name":         fn["name"],
			"description":  fn["description"],
			"input_schema": fn["parameters"],
		})
	}
	return out
}

func googleToolDefs() []map[string]any {
	decls := make([]map[string]any, 0, 4)
	for _, t := range commerceToolDefs() {
		fn, _ := t["function"].(map[string]any)
		decls = append(decls, map[string]any{
			"name":        fn["name"],
			"description": fn["description"],
			"parameters":  fn["parameters"],
		})
	}
	return []map[string]any{
		{"functionDeclarations": decls},
	}
}

func (a *App) executeCommerceTool(rt *commerceRuntime, name, argsJSON string) string {
	if rt == nil || rt.Client == nil {
		return toolErrorJSON("commerce tools are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	var (
		result any
		err    error
	)
	switch name {
	case "search_products":
		result, err = a.toolSearchProducts(ctx, rt, argsJSON)
	case "get_product":
		result, err = a.toolGetProduct(ctx, rt, argsJSON)
	case "get_order_status":
		result, err = a.toolGetOrderStatus(ctx, rt, argsJSON)
	case "create_order":
		result, err = a.toolCreateOrder(ctx, rt, argsJSON)
	default:
		err = fmt.Errorf("unknown tool: %s", name)
	}

	a.Log.Info("commerce tool executed",
		"tool", name,
		"duration_ms", time.Since(start).Milliseconds(),
		"error", errString(err),
	)

	if err != nil {
		return toolErrorJSON(err.Error())
	}
	return truncateJSON(result, commerceMaxResultBytes)
}

func (a *App) toolSearchProducts(ctx context.Context, rt *commerceRuntime, argsJSON string) (any, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	if args.Limit <= 0 {
		args.Limit = 20
	}
	if args.Limit > 50 {
		args.Limit = 50
	}
	products, err := rt.Client.SearchProducts(ctx, rt.StoreID, args.Query, args.Limit)
	if err != nil {
		return nil, err
	}
	// Object wrapper: Gemini functionResponse.response must be a JSON object, not an array.
	// Prices are already converted from paise to rupees by the ticker client.
	return map[string]any{
		"count":    len(products),
		"currency": "INR",
		"products": products,
	}, nil
}

func (a *App) toolGetProduct(ctx context.Context, rt *commerceRuntime, argsJSON string) (any, error) {
	var args struct {
		ProductID string `json:"product_id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.ProductID == "" {
		return nil, fmt.Errorf("product_id is required")
	}
	raw, err := rt.Client.GetProduct(ctx, args.ProductID)
	if err != nil {
		return nil, err
	}
	return convertProductMoneyToRupees(raw), nil
}

func (a *App) toolGetOrderStatus(ctx context.Context, rt *commerceRuntime, argsJSON string) (any, error) {
	var args struct {
		OrderUUID string `json:"order_uuid"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.OrderUUID == "" {
		return nil, fmt.Errorf("order_uuid is required")
	}
	raw, err := rt.Client.GetOrder(ctx, args.OrderUUID)
	if err != nil {
		return nil, err
	}
	return compactOrderStatus(raw), nil
}

func (a *App) toolCreateOrder(ctx context.Context, rt *commerceRuntime, argsJSON string) (any, error) {
	var args struct {
		Confirmed     bool             `json:"confirmed"`
		Items         []map[string]any `json:"items"`
		Email         string           `json:"email"`
		PhoneNumber   string           `json:"phone_number"`
		DeliveryMode  string           `json:"delivery_mode"`
		NewAddress    map[string]any   `json:"new_address"`
		BuyerMetaData map[string]any   `json:"buyer_meta_data"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if !args.Confirmed {
		return nil, fmt.Errorf("create_order requires confirmed=true after the user confirms the order summary")
	}
	if len(args.Items) == 0 {
		return nil, fmt.Errorf("items are required")
	}
	storeID, err := strconv.Atoi(rt.StoreID)
	if err != nil || storeID <= 0 {
		return nil, fmt.Errorf("invalid configured store id")
	}

	items := make([]ticker.OrderItem, 0, len(args.Items))
	for _, it := range args.Items {
		optID := asToolInt(it["product_option"])
		qty := asToolInt(it["quantity"])
		if optID <= 0 || qty <= 0 {
			return nil, fmt.Errorf("each item needs product_option and quantity > 0")
		}
		items = append(items, ticker.OrderItem{ProductOption: optID, Quantity: qty})
	}

	phone := strings.TrimSpace(args.PhoneNumber)
	if phone == "" {
		phone = strings.TrimSpace(rt.PhoneNumber)
	}
	deliveryMode := strings.TrimSpace(args.DeliveryMode)
	if deliveryMode == "" {
		deliveryMode = "PICKUP_FROM_STORE"
	}

	req := ticker.CreateOrderRequest{
		Store:         storeID,
		Items:         items,
		Email:         strings.TrimSpace(args.Email),
		PhoneNumber:   phone,
		DeliveryMode:  deliveryMode,
		NewAddress:    args.NewAddress,
		BuyerMetaData: args.BuyerMetaData,
	}

	// Guest delivery requires new_address with email.
	if deliveryMode == "DELIVERY_TO_LOCATION" && req.NewAddress == nil {
		return nil, fmt.Errorf("new_address is required for DELIVERY_TO_LOCATION")
	}
	if req.NewAddress != nil {
		if _, ok := req.NewAddress["email"]; !ok && req.Email != "" {
			req.NewAddress["email"] = req.Email
		}
	}

	raw, err := rt.Client.CreateOrder(ctx, req)
	if err != nil {
		return nil, err
	}
	return compactOrderCreateResult(raw), nil
}

func compactOrderStatus(raw map[string]any) map[string]any {
	out := map[string]any{
		"display_uid": raw["display_uid"],
		"uuid":        raw["uuid"], // internal; use for get_order_status only — do not show to user
		"status":      raw["status"],
		"amount":      ticker.PaiseToRupees(asToolFloat(raw["amount"])),
		"currency":    "INR",
	}
	if payment, ok := raw["payment"].(map[string]any); ok {
		out["payment_status"] = payment["status"]
		// payment.meta_data.url_to_redirect is the customer payment link
		if meta, ok := payment["meta_data"].(map[string]any); ok {
			if u, ok := meta["url_to_redirect"]; ok {
				out["payment_url"] = u
			}
		}
	}
	return out
}

func compactOrderCreateResult(raw map[string]any) map[string]any {
	out := compactOrderStatus(raw)
	out["id"] = raw["id"]
	return out
}

// convertProductMoneyToRupees copies a product payload and converts paise money fields to rupees.
func convertProductMoneyToRupees(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		out[k] = v
	}
	for _, key := range []string{"min_price", "mrp", "price", "amount"} {
		if _, ok := out[key]; ok {
			out[key] = ticker.PaiseToRupees(asToolFloat(out[key]))
		}
	}
	if opts, ok := out["options"].([]any); ok {
		converted := make([]any, 0, len(opts))
		for _, o := range opts {
			om, ok := o.(map[string]any)
			if !ok {
				converted = append(converted, o)
				continue
			}
			opt := make(map[string]any, len(om))
			for k, v := range om {
				opt[k] = v
			}
			for _, key := range []string{"price", "mrp", "amount"} {
				if _, ok := opt[key]; ok {
					opt[key] = ticker.PaiseToRupees(asToolFloat(opt[key]))
				}
			}
			converted = append(converted, opt)
		}
		out["options"] = converted
	}
	out["currency"] = "INR"
	return out
}

func asToolFloat(v any) float64 {
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

func truncateJSON(v any, maxBytes int) string {
	b, err := json.Marshal(v)
	if err != nil {
		return toolErrorJSON("failed to encode tool result")
	}
	if len(b) <= maxBytes {
		return string(b)
	}
	// Truncate with a marker object.
	trimmed := string(b[:maxBytes])
	return fmt.Sprintf(`{"truncated":true,"preview":%q}`, trimmed)
}

func toolErrorJSON(msg string) string {
	b, _ := json.Marshal(map[string]any{"error": msg})
	return string(b)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func asToolInt(v any) int {
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
