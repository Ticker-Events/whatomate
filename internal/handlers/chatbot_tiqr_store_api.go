package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/shridarpatil/whatomate/pkg/tickermcp"
)

type tiqrStoreInvoker interface {
	CallTool(ctx context.Context, name string, args map[string]any) (any, error)
	Close() error
}

var newTiqrStoreInvoker = defaultTiqrStoreInvoker

func defaultTiqrStoreInvoker(mcpURL, apiKey string) tiqrStoreInvoker {
	return tickermcp.NewClient(mcpURL, apiKey, nil)
}

var newTiqrStoreRESTClient = defaultTiqrStoreRESTClient

func defaultTiqrStoreRESTClient(baseURL string) *ticker.Client {
	return ticker.NewClient(baseURL, nil)
}

func commerceRESTConfigured(ai models.AIConfig) bool {
	return strings.TrimSpace(ai.CommerceRESTURL) != "" &&
		strings.TrimSpace(ai.CommerceStoreID) != ""
}

func tiqrStoreAPIType(cfg map[string]any) string {
	t := strings.ToLower(strings.TrimSpace(stringFromConfig(cfg, "api_type")))
	if t == "rest" {
		return "rest"
	}
	return "mcp"
}

// execChatTiqrStoreAPI calls a first-party TiQR buyer operation selected in
// node.Config["operation"] via MCP or buyer REST (node.Config["api_type"]).
// Store ID and credentials come from AI settings.
// Outcomes match api_call: "http:2xx" / "http:non2xx".
//
// Config:
//
//	{
//	  "api_type": "mcp",
//	  "operation": "search_products",
//	  "params": { "search": "{{query}}", "limit": "10" },
//	  "response_mapping": { "product_name": "products[0].name" },
//	  "message_template": "Found {{product_name}}"
//	}
func (a *App) execChatTiqrStoreAPI(node *ChatNode, ctx *chatNodeCtx) (nodeOutcome, error) {
	if ctx.session.SessionData == nil {
		ctx.session.SessionData = models.JSONB{}
	}
	sessionData := ctx.session.SessionData
	sessionData["phone_number"] = ctx.session.PhoneNumber

	settings, err := a.getChatbotSettingsCached(ctx.account.OrganizationID, ctx.account.Name)
	apiType := tiqrStoreAPIType(node.Config)
	if err != nil || settings == nil {
		a.Log.Error("tiqr_store_api node missing commerce settings",
			"node", node.ID, "session", ctx.session.ID, "api_type", apiType, "error", err)
		return nodeOutcome{outcome: "http:non2xx"}, nil
	}
	if apiType == "rest" {
		if !commerceRESTConfigured(settings.AI) {
			a.Log.Error("tiqr_store_api node missing REST commerce settings",
				"node", node.ID, "session", ctx.session.ID)
			return nodeOutcome{outcome: "http:non2xx"}, nil
		}
	} else if !commerceConfigured(settings.AI) {
		a.Log.Error("tiqr_store_api node missing MCP commerce settings",
			"node", node.ID, "session", ctx.session.ID)
		return nodeOutcome{outcome: "http:non2xx"}, nil
	}

	storeID := strings.TrimSpace(settings.AI.CommerceStoreID)
	sessionData["store_id"] = storeID

	operation := stringFromConfig(node.Config, "operation")
	replaceVar := func(s string) string { return processTemplate(s, sessionData) }
	params := templateTiqrParams(node.Config["params"], replaceVar)

	callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var raw any
	if apiType == "rest" {
		client := newTiqrStoreRESTClient(settings.AI.CommerceRESTURL)
		raw, err = invokeTiqrStoreRESTOperation(callCtx, client, operation, storeID, ctx.session.PhoneNumber, params)
	} else {
		invoker := newTiqrStoreInvoker(settings.AI.CommerceMCPURL, settings.AI.CommerceMCPAPIKey)
		defer func() { _ = invoker.Close() }()
		raw, err = invokeTiqrStoreOperation(callCtx, invoker, operation, storeID, ctx.session.PhoneNumber, params)
	}
	if err != nil {
		a.Log.Error("tiqr_store_api node request failed",
			"node", node.ID, "session", ctx.session.ID, "api_type", apiType, "operation", operation, "error", err)
		return nodeOutcome{outcome: "http:non2xx"}, nil
	}

	payload := tiqrStoreResultMap(operation, raw)
	applyChatResponseMapping(node.Config, payload, sessionData)

	if tmpl := stringFromConfig(node.Config, "message_template"); tmpl != "" {
		rendered := processTemplate(tmpl, sessionData)
		if rendered != "" {
			if err := a.sendAndSaveTextMessage(ctx.account, ctx.contact, rendered); err != nil {
				a.Log.Error("tiqr_store_api node failed to send message_template",
					"node", node.ID, "session", ctx.session.ID, "error", err)
			} else {
				a.logSessionMessage(ctx.session.ID, models.DirectionOutgoing, rendered, node.ID)
			}
		}
	}

	return nodeOutcome{outcome: "http:2xx"}, nil
}

func invokeTiqrStoreRESTOperation(
	ctx context.Context,
	client *ticker.Client,
	operation, storeID, phone string,
	params map[string]string,
) (any, error) {
	if client == nil || strings.TrimSpace(client.BaseURL) == "" {
		return nil, fmt.Errorf("rest base url is required")
	}
	if strings.TrimSpace(storeID) == "" {
		return nil, fmt.Errorf("store_id is required")
	}

	limit, _ := optionalPositiveInt(params["limit"])
	offset, _ := optionalNonNegativeInt(params["offset"])

	switch strings.TrimSpace(operation) {
	case "list_collections":
		return client.ListCategories(ctx, storeID, ticker.ListCategoriesParams{
			Tags:   splitCSV(params["tags"]),
			TagsOp: strings.TrimSpace(params["tags_op"]),
			Limit:  limit,
			Offset: offset,
		})
	case "search_collections":
		search := strings.TrimSpace(params["search"])
		if search == "" {
			return nil, fmt.Errorf("search is required")
		}
		return client.ListCategories(ctx, storeID, ticker.ListCategoriesParams{
			Search: search,
			Tags:   splitCSV(params["tags"]),
			TagsOp: strings.TrimSpace(params["tags_op"]),
			Limit:  limit,
			Offset: offset,
		})
	case "list_products":
		return client.ListProductsPage(ctx, storeID, ticker.ListProductsParams{
			CategoryID: strings.TrimSpace(params["category_id"]),
			Limit:      limit,
			Offset:     offset,
		})
	case "search_products":
		search := strings.TrimSpace(params["search"])
		if search == "" {
			return nil, fmt.Errorf("search is required")
		}
		return client.ListProductsPage(ctx, storeID, ticker.ListProductsParams{
			Search:     search,
			CategoryID: strings.TrimSpace(params["category_id"]),
			Limit:      limit,
			Offset:     offset,
		})
	case "get_product":
		productID := strings.TrimSpace(params["product_id"])
		if productID == "" {
			return nil, fmt.Errorf("product_id is required")
		}
		return client.GetProduct(ctx, productID)
	case "list_product_options":
		ids, err := parseIntListParam(params["ids"])
		if err != nil {
			return nil, err
		}
		return client.ListProductOptions(ctx, storeID, ids)
	case "get_store":
		return client.GetStore(ctx, storeID)
	case "get_store_info":
		return client.GetStoreInfo(ctx, storeID)
	case "list_faqs":
		return client.ListFaqs(ctx, storeID)
	case "create_order":
		sid, err := strconv.Atoi(strings.TrimSpace(storeID))
		if err != nil || sid <= 0 {
			return nil, fmt.Errorf("store_id is required")
		}
		orderMap, err := buildGuestOrderPayload(sid, phone, params)
		if err != nil {
			return nil, err
		}
		body, err := orderMapToCreateRequest(orderMap)
		if err != nil {
			return nil, err
		}
		return client.CreateOrder(ctx, body)
	case "get_order":
		uuid := strings.TrimSpace(params["order_uuid"])
		if uuid == "" {
			return nil, fmt.Errorf("order_uuid is required")
		}
		return client.GetOrder(ctx, uuid)
	case "check_delivery", "lookup_order_status", "retry_payment":
		return nil, fmt.Errorf("operation %q is not available over REST (use MCP)", operation)
	default:
		return nil, fmt.Errorf("unknown tiqr store operation %q", operation)
	}
}

func orderMapToCreateRequest(order map[string]any) (ticker.CreateOrderRequest, error) {
	b, err := json.Marshal(order)
	if err != nil {
		return ticker.CreateOrderRequest{}, err
	}
	var body ticker.CreateOrderRequest
	if err := json.Unmarshal(b, &body); err != nil {
		return ticker.CreateOrderRequest{}, fmt.Errorf("invalid order payload: %w", err)
	}
	return body, nil
}

func invokeTiqrStoreOperation(
	ctx context.Context,
	invoker tiqrStoreInvoker,
	operation, storeID, phone string,
	params map[string]string,
) (any, error) {
	sid, err := strconv.Atoi(strings.TrimSpace(storeID))
	if err != nil || sid <= 0 {
		return nil, fmt.Errorf("store_id is required")
	}

	tool, args, err := buildTiqrStoreToolArgs(operation, sid, phone, params)
	if err != nil {
		return nil, err
	}
	return invoker.CallTool(ctx, tool, args)
}

func buildTiqrStoreToolArgs(operation string, storeID int, phone string, params map[string]string) (string, map[string]any, error) {
	switch strings.TrimSpace(operation) {
	case "list_collections":
		args, err := categoryToolArgs(storeID, params, false)
		return "list_categories", args, err
	case "search_collections":
		args, err := categoryToolArgs(storeID, params, true)
		return "list_categories", args, err
	case "list_products":
		args, err := productToolArgs(storeID, params, false)
		return "list_products", args, err
	case "search_products":
		args, err := productToolArgs(storeID, params, true)
		return "list_products", args, err
	case "get_product":
		productID := strings.TrimSpace(params["product_id"])
		if productID == "" {
			return "", nil, fmt.Errorf("product_id is required")
		}
		id, err := strconv.Atoi(productID)
		if err != nil || id <= 0 {
			return "", nil, fmt.Errorf("product_id must be a positive integer")
		}
		return "get_product", map[string]any{"product_id": id}, nil
	case "list_product_options":
		args := map[string]any{"store_id": storeID}
		if ids, err := parseIntListParam(params["ids"]); err != nil {
			return "", nil, err
		} else if len(ids) > 0 {
			args["ids"] = ids
		}
		return "list_product_options", args, nil
	case "get_store":
		return "get_store", map[string]any{"store_id": storeID}, nil
	case "get_store_info":
		return "get_store_info", map[string]any{"store_id": storeID}, nil
	case "list_faqs":
		return "list_faqs", map[string]any{"store_id": storeID}, nil
	case "check_delivery":
		lat, err := parseFloatParam(params["latitude"], "latitude")
		if err != nil {
			return "", nil, err
		}
		lng, err := parseFloatParam(params["longitude"], "longitude")
		if err != nil {
			return "", nil, err
		}
		return "check_delivery_eligibility", map[string]any{
			"store_id":  storeID,
			"latitude":  lat,
			"longitude": lng,
		}, nil
	case "create_order":
		order, err := buildGuestOrderPayload(storeID, phone, params)
		if err != nil {
			return "", nil, err
		}
		return "create_order", map[string]any{"order": order}, nil
	case "get_order":
		uuid := strings.TrimSpace(params["order_uuid"])
		if uuid == "" {
			return "", nil, fmt.Errorf("order_uuid is required")
		}
		return "get_order", map[string]any{"order_uuid": uuid}, nil
	case "lookup_order_status":
		phoneNumber := strings.TrimSpace(params["phone_number"])
		if phoneNumber == "" {
			phoneNumber = strings.TrimSpace(phone)
		}
		if phoneNumber == "" {
			return "", nil, fmt.Errorf("phone_number is required")
		}
		args := map[string]any{
			"store_id":     storeID,
			"phone_number": phoneNumber,
		}
		if orderID := strings.TrimSpace(params["order_id"]); orderID != "" {
			args["order_display_id"] = orderID
		}
		return "lookup_order_status", args, nil
	case "retry_payment":
		uuid := strings.TrimSpace(params["order_uuid"])
		if uuid == "" {
			return "", nil, fmt.Errorf("order_uuid is required")
		}
		return "retry_payment", map[string]any{"order_uuid": uuid}, nil
	default:
		return "", nil, fmt.Errorf("unknown tiqr store operation %q", operation)
	}
}

func categoryToolArgs(storeID int, params map[string]string, searchRequired bool) (map[string]any, error) {
	args := map[string]any{"store_id": storeID}
	search := strings.TrimSpace(params["search"])
	if searchRequired && search == "" {
		return nil, fmt.Errorf("search is required")
	}
	if search != "" {
		args["search"] = search
	}
	if tags := splitCSV(params["tags"]); len(tags) > 0 {
		args["tags"] = tags
	}
	if op := strings.TrimSpace(params["tags_op"]); op != "" {
		args["tags_op"] = op
	}
	if id := strings.TrimSpace(params["category_id"]); id != "" {
		n, err := strconv.Atoi(id)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("category_id must be a positive integer")
		}
		args["category_id"] = n
	}
	applyLimitOffset(args, params)
	return args, nil
}

func productToolArgs(storeID int, params map[string]string, searchRequired bool) (map[string]any, error) {
	args := map[string]any{"store_id": storeID}
	search := strings.TrimSpace(params["search"])
	if searchRequired && search == "" {
		return nil, fmt.Errorf("search is required")
	}
	if search != "" {
		args["search"] = search
	}
	if id := strings.TrimSpace(params["category_id"]); id != "" {
		n, err := strconv.Atoi(id)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("category_id must be a positive integer")
		}
		args["category_id"] = n
	}
	applyLimitOffset(args, params)
	return args, nil
}

func buildGuestOrderPayload(storeID int, phone string, params map[string]string) (map[string]any, error) {
	itemsRaw := strings.TrimSpace(params["items"])
	if itemsRaw == "" {
		return nil, fmt.Errorf("items is required")
	}
	items, err := parseJSONParam(itemsRaw)
	if err != nil {
		return nil, fmt.Errorf("items must be JSON: %w", err)
	}
	email := strings.TrimSpace(params["email"])
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	deliveryMode := strings.TrimSpace(params["delivery_mode"])
	if deliveryMode == "" {
		return nil, fmt.Errorf("delivery_mode is required")
	}
	phoneNumber := strings.TrimSpace(params["phone_number"])
	if phoneNumber == "" {
		phoneNumber = strings.TrimSpace(phone)
	}
	order := map[string]any{
		"store":         storeID,
		"items":         items,
		"email":         email,
		"delivery_mode": deliveryMode,
	}
	if phoneNumber != "" {
		order["phone_number"] = phoneNumber
	}
	if addr := strings.TrimSpace(params["new_address"]); addr != "" {
		parsed, err := parseJSONParam(addr)
		if err != nil {
			return nil, fmt.Errorf("new_address must be JSON: %w", err)
		}
		order["new_address"] = parsed
	}
	if notes := strings.TrimSpace(params["notes"]); notes != "" {
		order["notes"] = notes
	}
	if token := strings.TrimSpace(params["slot_token"]); token != "" {
		order["slot_token"] = token
	}
	return order, nil
}

func applyLimitOffset(args map[string]any, params map[string]string) {
	if n, ok := optionalPositiveInt(params["limit"]); ok {
		args["limit"] = n
	}
	if n, ok := optionalNonNegativeInt(params["offset"]); ok {
		args["offset"] = n
	}
}

func templateTiqrParams(raw any, replace func(string) string) map[string]string {
	out := map[string]string{}
	m, ok := raw.(map[string]any)
	if !ok {
		return out
	}
	for key, value := range m {
		var s string
		switch v := value.(type) {
		case string:
			s = v
		case nil:
			continue
		default:
			s = fmt.Sprint(v)
		}
		out[key] = replace(s)
	}
	return out
}

func tiqrStoreResultMap(operation string, raw any) map[string]any {
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	switch operation {
	case "list_faqs":
		return map[string]any{"faqs": raw}
	case "list_product_options":
		return map[string]any{"options": raw}
	default:
		return map[string]any{"data": raw}
	}
}

func applyChatResponseMapping(cfg map[string]any, payload map[string]any, sessionData models.JSONB) {
	mapping, ok := cfg["response_mapping"].(map[string]any)
	if !ok || len(mapping) == 0 || payload == nil {
		return
	}
	mappingStrings := make(map[string]string, len(mapping))
	for varName, path := range mapping {
		if pathStr, ok := path.(string); ok {
			mappingStrings[varName] = pathStr
		}
	}
	extracted := extractResponseMapping(payload, mappingStrings)
	maps.Copy(sessionData, extracted)
}

func parseJSONParam(raw string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func parseIntListParam(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var ids []int
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			return nil, fmt.Errorf("ids must be JSON integers or a comma-separated list")
		}
		return ids, nil
	}
	parts := splitCSV(raw)
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("ids must be positive integers")
		}
		out = append(out, n)
	}
	return out, nil
}

func parseFloatParam(raw, name string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	return n, nil
}

func optionalPositiveInt(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func optionalNonNegativeInt(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}
