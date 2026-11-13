package tiqrecommercev2

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
)

type emptyArgs struct{}

type toolResult struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

func okResult(data any, message string) toolResult {
	return toolResult{OK: true, Data: data, Message: message}
}

func errResult(msg string) toolResult {
	return toolResult{OK: false, Error: msg}
}

func buildTools(rt *Runtime) ([]tool.Tool, error) {
	var tools []tool.Tool
	add := func(t tool.Tool, err error) error {
		if err != nil {
			return err
		}
		tools = append(tools, t)
		return nil
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "get_store",
		Description: "Load store profile and delivery modes. Call first when store is unknown.",
	}, func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		payload, ok := rt.callTiqr("store", "get_store", nil)
		if !ok {
			rt.State.StoreLoadRetries++
			if rt.State.StoreLoadRetries >= 2 {
				return errResult("store load failed after retry; transfer_to_staff"), nil
			}
			return errResult("store load failed; retry get_store once"), nil
		}
		rt.State.StoreLoadRetries = 0
		if s := rt.session(); s != nil {
			if s.SessionData == nil {
				s.SessionData = models.JSONB{}
			}
			s.SessionData["store"] = payload
		}
		return okResult(compactStore(payload), ""), nil
	})); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "list_collections",
		Description: "List product collections. Grounds collection ids for later tools.",
	}, func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		payload, ok := rt.callTiqr("collections", "list_collections", map[string]string{"limit": "20"})
		if !ok {
			rt.State.StoreLoadRetries++
			if rt.State.StoreLoadRetries >= 2 {
				return errResult("collections load failed after retry; transfer_to_staff"), nil
			}
			return errResult("collections load failed; retry list_collections once"), nil
		}
		rows, _ := pageResults(payload)
		if len(rows) == 0 {
			return errResult("no collections; transfer_to_staff"), nil
		}
		summaries := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			m, ok := asStringMap(row)
			if !ok {
				continue
			}
			id := fieldID(m)
			rt.State.rememberID(id)
			if ai := strings.TrimSpace(asString(m["ai_instructions"])); ai != "" && id != "" {
				// Kept on collection row for when this collection is chosen.
			}
			summaries = append(summaries, map[string]any{
				"id":              id,
				"name":            asString(m["name"]),
				"description":     asString(m["description"]),
				"handoff_policy":  asString(m["handoff_policy"]),
				"ai_instructions": asString(m["ai_instructions"]),
			})
		}
		if s := rt.session(); s != nil && s.SessionData != nil {
			s.SessionData["collections"] = rows
		}
		return okResult(summaries, ""), nil
	})); err != nil {
		return nil, err
	}

	type listProductsArgs struct {
		CollectionID string `json:"collection_id"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "list_products",
		Description: "List products in a grounded collection_id.",
	}, func(_ agent.ToolContext, in listProductsArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		if !rt.State.isGrounded(in.CollectionID) {
			return errResult(rt.rejectUngrounded(in.CollectionID)), nil
		}
		payload, ok := rt.callTiqr("products", "list_products", map[string]string{"category_id": strings.TrimSpace(in.CollectionID)})
		if !ok {
			return errResult("list_products failed"), nil
		}
		return okResult(groundProducts(rt, payload, ""), ""), nil
	})); err != nil {
		return nil, err
	}

	type searchArgs struct {
		Query string `json:"query"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "search_products",
		Description: "Search products by free-text query. Results are labeled as search results, not a collection.",
	}, func(_ agent.ToolContext, in searchArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		q := strings.TrimSpace(in.Query)
		if q == "" {
			return errResult("query is required"), nil
		}
		payload, ok := rt.callTiqr("products", "search_products", map[string]string{"search": q, "limit": "20"})
		if !ok {
			return errResult("search_products failed"), nil
		}
		rt.State.SearchLabel = q
		return okResult(groundProducts(rt, payload, q), "search results for: "+q), nil
	})); err != nil {
		return nil, err
	}

	type getProductArgs struct {
		ProductID string `json:"product_id"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "get_product",
		Description: "Load one grounded product including options and add-ons.",
	}, func(_ agent.ToolContext, in getProductArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		if !rt.State.isGrounded(in.ProductID) {
			return errResult(rt.rejectUngrounded(in.ProductID)), nil
		}
		payload, ok := rt.callTiqr("product", "get_product", map[string]string{"id": strings.TrimSpace(in.ProductID)})
		if !ok {
			return errResult("get_product failed"), nil
		}
		groundProductDetail(rt, payload)
		return okResult(payload, ""), nil
	})); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "list_faqs",
		Description: "Load store FAQs.",
	}, func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		payload, ok := rt.callTiqr("faqs", "list_faqs", nil)
		if !ok {
			return okResult([]any{}, "no faqs"), nil
		}
		rows, _ := pageResults(payload)
		for _, row := range rows {
			if m, ok := asStringMap(row); ok {
				rt.State.rememberID(fieldID(m))
			}
		}
		return okResult(rows, ""), nil
	})); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "get_store_info",
		Description: "About us and policy fields.",
	}, func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		payload, ok := rt.callTiqr("store_info", "get_store_info", nil)
		if !ok {
			return okResult(map[string]any{}, "no store info"), nil
		}
		return okResult(payload, ""), nil
	})); err != nil {
		return nil, err
	}

	type addCartArgs struct {
		OptionID      string            `json:"option_id"`
		ProductName   string            `json:"product_name"`
		OptionName    string            `json:"option_name"`
		Quantity      int               `json:"quantity"`
		CaptureFields map[string]string `json:"capture_fields"`
		CaptureLabels map[string]string `json:"capture_labels"`
		CaptureOrder  []string          `json:"capture_order"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "add_cart_line",
		Description: "Add a cart line. Same option with different capture answers creates a separate line_id.",
	}, func(_ agent.ToolContext, in addCartArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		line, errMsg := rt.AddCartLine(in.OptionID, in.ProductName, in.OptionName, in.Quantity, in.CaptureFields, in.CaptureLabels, in.CaptureOrder)
		if errMsg != "" {
			return errResult(errMsg), nil
		}
		return okResult(line, formatCartSummary(rt.cart())), nil
	})); err != nil {
		return nil, err
	}

	type updateCartArgs struct {
		LineID   string `json:"line_id"`
		Quantity int    `json:"quantity"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "update_cart_line",
		Description: "Set quantity on a cart line_id (not option id).",
	}, func(_ agent.ToolContext, in updateCartArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		if msg := rt.UpdateCartLine(in.LineID, in.Quantity); msg != "" {
			return errResult(msg), nil
		}
		return okResult(nil, formatCartSummary(rt.cart())), nil
	})); err != nil {
		return nil, err
	}

	type removeCartArgs struct {
		LineID string `json:"line_id"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "remove_cart_line",
		Description: "Remove one cart line by line_id.",
	}, func(_ agent.ToolContext, in removeCartArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		name, msg := rt.RemoveCartLine(in.LineID)
		if msg != "" {
			return errResult(msg), nil
		}
		return okResult(map[string]any{"removed": name}, formatCartSummary(rt.cart())), nil
	})); err != nil {
		return nil, err
	}

	type addonsArgs struct {
		Items []struct {
			AddonID  string `json:"addon_id"`
			Quantity int    `json:"quantity"`
			Name     string `json:"name"`
		} `json:"items"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "set_addons",
		Description: "Set session catalog add-ons from grounded addon ids.",
	}, func(_ agent.ToolContext, in addonsArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		rows := make([]any, 0, len(in.Items))
		for _, item := range in.Items {
			if !rt.State.isGrounded(item.AddonID) {
				return errResult(rt.rejectUngrounded(item.AddonID)), nil
			}
			if item.Quantity < 1 {
				return errResult("addon quantity must be 1 or more"), nil
			}
			rows = append(rows, map[string]any{
				"addon":    item.AddonID,
				"quantity": item.Quantity,
				"name":     item.Name,
			})
		}
		if s := rt.session(); s != nil {
			if s.SessionData == nil {
				s.SessionData = models.JSONB{}
			}
			s.SessionData["commerce_addons"] = rows
		}
		return okResult(rows, "addons saved"), nil
	})); err != nil {
		return nil, err
	}

	type fulfillmentArgs struct {
		Mode string `json:"mode"` // pickup | delivery | PICKUP_FROM_STORE | DELIVERY_TO_LOCATION
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "set_fulfillment",
		Description: "Set delivery_mode to pickup or delivery.",
	}, func(_ agent.ToolContext, in fulfillmentArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		mode := strings.TrimSpace(in.Mode)
		switch strings.ToLower(mode) {
		case "pickup", "store pickup", strings.ToLower(modePickup):
			mode = modePickup
		case "delivery", strings.ToLower(modeDelivery):
			mode = modeDelivery
			rt.State.AwaitingLocation = true
		default:
			return errResult("mode must be pickup or delivery"), nil
		}
		if s := rt.session(); s != nil {
			if s.SessionData == nil {
				s.SessionData = models.JSONB{}
			}
			s.SessionData["delivery_mode"] = mode
		}
		return okResult(map[string]any{"delivery_mode": mode}, ""), nil
	})); err != nil {
		return nil, err
	}

	type pinArgs struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "set_delivery_pin",
		Description: "Check a delivery pin. Does not quote a fee. Caps out-of-range retries at 3.",
	}, func(_ agent.ToolContext, in pinArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		store, _ := sessionMap(rt, "store")
		data, msg := rt.SetDeliveryPin(in.Latitude, in.Longitude, store, storeHasPickup(store))
		if msg != "" {
			return errResult(msg), nil
		}
		return okResult(data, asString(data["message"])), nil
	})); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "checkout_preview",
		Description: "Priced quote for the session cart. This is the only price the customer should see.",
	}, func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		lines := rt.cart()
		if len(lines) == 0 {
			return errResult("cart is empty"), nil
		}
		params := map[string]string{}
		// Guest checkout-preview goes through REST create-style body via operation.
		items, _ := json.Marshal(OrderItemsForAPI(lines))
		params["items_json"] = string(items)
		if s := rt.session(); s != nil && s.SessionData != nil {
			params["delivery_mode"] = strings.TrimSpace(asString(s.SessionData["delivery_mode"]))
			if params["delivery_mode"] == "" {
				params["delivery_mode"] = modePickup
			}
			if lat, ok := anyToFloat64(s.SessionData["delivery_latitude"]); ok {
				params["latitude"] = fmt.Sprintf("%.6f", lat)
			}
			if lng, ok := anyToFloat64(s.SessionData["delivery_longitude"]); ok {
				params["longitude"] = fmt.Sprintf("%.6f", lng)
			}
		}
		payload, ok := rt.callTiqr("checkout_preview", "checkout_preview", params)
		if !ok {
			// Fallback: surface session cart for the agent to describe while
			// still requiring a successful preview before create_order in production.
			return okResult(map[string]any{
				"cart":    lines,
				"summary": formatCartSummary(lines),
				"note":    "preview API unavailable in this environment; show cart and ask confirm before create_order",
			}, formatCartSummary(lines)), nil
		}
		return okResult(payload, "show this priced summary and ask the customer to confirm"), nil
	})); err != nil {
		return nil, err
	}

	type detailsArgs struct {
		Delivery bool `json:"delivery"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "open_details_flow",
		Description: "Open the WhatsApp Flow for customer details (pickup or delivery).",
	}, func(_ agent.ToolContext, in detailsArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		account := rt.Conv.ChatCtx().Account()
		if account == nil {
			return errResult("no account"), nil
		}
		flowID := metaFlowIDFromBinding(account.OrganizationID, account.Name, in.Delivery)
		if flowID == "" {
			flowID = rt.Conv.App().ResolveEcommerceMetaFlowID(account.OrganizationID, account.Name, in.Delivery)
		}
		body := "Please share your name, email, and phone number so we can place your pickup order."
		if in.Delivery {
			body = "Please share your name, phone number, and address so we can place your delivery order."
		}
		_, err := rt.Conv.App().ExecChatWhatsAppFlow(rt.Conv.ChatCtx(), "details", map[string]any{
			"flow_id":     flowID,
			"body":        body,
			"header":      "Your details",
			"button_text": "Enter details",
		})
		if err != nil {
			return errResult(err.Error()), nil
		}
		rt.State.AwaitingDetails = true
		rt.Replied = true
		rt.State.appendTranscript("assistant", body)
		return okResult(map[string]any{"flow_id": flowID}, "details form sent"), nil
	})); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "create_order",
		Description: "Place the order after the customer confirmed. Refused until confirmation.",
	}, func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		if msg := rt.GateCreateOrder(); msg != "" {
			return errResult(msg), nil
		}
		lines := rt.cart()
		if len(lines) == 0 {
			return errResult("cart is empty"), nil
		}
		params := map[string]string{}
		items, _ := json.Marshal(OrderItemsForAPI(lines))
		params["items_json"] = string(items)
		if s := rt.session(); s != nil && s.SessionData != nil {
			params["delivery_mode"] = strings.TrimSpace(asString(s.SessionData["delivery_mode"]))
			if params["delivery_mode"] == "" {
				params["delivery_mode"] = modePickup
			}
			params["notes"] = tiqrecommerce.CodedOrderNotes(map[string]any(s.SessionData))
		}
		payload, ok := rt.callTiqr("order", "create_order", params)
		if !ok {
			return errResult("create_order failed; ask missing fields or transfer_to_staff"), nil
		}
		if s := rt.session(); s != nil && s.SessionData != nil {
			s.SessionData["order"] = payload
		}
		return okResult(payload, "order placed; reply with order number and Pay now if payment_url is present, then end_session"), nil
	})); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "list_orders",
		Description: "List recent orders for this WhatsApp number.",
	}, func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		payload, ok := rt.callTiqrMCP("orders_page", "list_orders_by_phone", map[string]string{"limit": "10"})
		if !ok {
			return errResult("order status unavailable"), nil
		}
		rt.State.OrderStatusActive = true
		rows, _ := pageResults(payload)
		if len(rows) == 0 {
			rows, _ = anySlice(payload["orders"])
		}
		for _, row := range rows {
			if m, ok := asStringMap(row); ok {
				rt.State.rememberID(asString(m["display_uid"]))
				rt.State.rememberID(fieldID(m))
			}
		}
		return okResult(rows, ""), nil
	})); err != nil {
		return nil, err
	}

	type lookupArgs struct {
		OrderDisplayID string `json:"order_display_id"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "lookup_order",
		Description: "Lookup one order by display id. After summarizing, call end_session. Do not attach a pay link.",
	}, func(_ agent.ToolContext, in lookupArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		id := strings.TrimSpace(in.OrderDisplayID)
		if id != "" && !rt.State.isGrounded(id) {
			return errResult(rt.rejectUngrounded(id)), nil
		}
		payload, ok := rt.callTiqrMCP("selected_order", "lookup_order_status", map[string]string{
			"order_id": id,
		})
		if !ok {
			return errResult("lookup failed"), nil
		}
		// Strip payment URLs so the agent cannot show Pay now on status.
		delete(payload, "payment_url")
		if pay, ok := asStringMap(payload["payment"]); ok {
			delete(pay, "meta_data")
			payload["payment"] = pay
		}
		return okResult(payload, "summarize status with no pay link, then end_session"), nil
	})); err != nil {
		return nil, err
	}

	type replyArgs struct {
		Body        string `json:"body"`
		Kind        string `json:"kind"` // text | buttons | location
		ButtonIDs   string `json:"button_ids"`
		ButtonTitles string `json:"button_titles"`
		PaymentURL  string `json:"payment_url"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "reply",
		Description: "Send the customer-facing WhatsApp message for this turn. Call exactly once. button_ids/titles are comma-separated and must be grounded or fixed actions.",
	}, func(_ agent.ToolContext, in replyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		body := strings.TrimSpace(in.Body)
		if body == "" {
			return errResult("body is required"), nil
		}
		kind := strings.ToLower(strings.TrimSpace(in.Kind))
		if kind == "" {
			kind = "text"
		}
		switch kind {
		case "location":
			if err := rt.Conv.App().DeliverCodedLocationRequest(rt.Conv.ChatCtx(), "v2_location", body); err != nil {
				return errResult(err.Error()), nil
			}
			rt.State.AwaitingLocation = true
		case "buttons":
			ids := splitCSV(in.ButtonIDs)
			titles := splitCSV(in.ButtonTitles)
			buttons := make([]map[string]any, 0, len(ids))
			for i, id := range ids {
				if !rt.State.isGrounded(id) {
					return errResult(rt.rejectUngrounded(id)), nil
				}
				title := id
				if i < len(titles) && titles[i] != "" {
					title = titles[i]
				} else if t, ok := rt.State.FixedActions[id]; ok {
					title = t
				}
				buttons = append(buttons, map[string]any{"id": id, "title": truncate(title, 20)})
			}
			if len(buttons) == 0 || len(buttons) > 3 {
				return errResult("buttons need 1–3 grounded ids"), nil
			}
			_, err := rt.Conv.App().ExecChatButtons(rt.Conv.ChatCtx(), "v2_buttons", map[string]any{
				"body":    body,
				"buttons": buttons,
			})
			if err != nil {
				return errResult(err.Error()), nil
			}
		case "cta", "pay":
			url := strings.TrimSpace(in.PaymentURL)
			if url == "" {
				return errResult("payment_url required for pay reply"), nil
			}
			if err := rt.Conv.App().DeliverCodedCTAURL(rt.Conv.ChatCtx(), "v2_pay", body, "Pay now", url); err != nil {
				_ = rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), "v2_pay", body+"\n\nPay here: "+url)
			}
		default:
			if err := rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), "v2_reply", body); err != nil {
				return errResult(err.Error()), nil
			}
		}
		rt.Replied = true
		rt.State.appendTranscript("assistant", body)
		rt.session().CurrentStep = "v2_wait"
		return okResult(nil, "sent"), nil
	})); err != nil {
		return nil, err
	}

	type transferArgs struct {
		Message string `json:"message"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "transfer_to_staff",
		Description: "Hand off to staff. Refused until the customer asked for staff or agreed.",
	}, func(_ agent.ToolContext, in transferArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		if msg := rt.GateTransfer(); msg != "" {
			return errResult(msg), nil
		}
		message := strings.TrimSpace(in.Message)
		if message == "" {
			message = codedflowAgentHandoff
		}
		rt.Transfer = true
		rt.Replied = true
		if err := rt.Conv.Transfer(message); err != nil {
			return errResult(err.Error()), nil
		}
		return okResult(nil, "transferred"), nil
	})); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "end_session",
		Description: "End the shopping session (after order placed or order status summary).",
	}, func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		rt.Ended = true
		_ = rt.Conv.End()
		return okResult(nil, "ended"), nil
	})); err != nil {
		return nil, err
	}

	return tools, nil
}

const codedflowAgentHandoff = "I'm connecting you with a team member who can help."

func groundProducts(rt *Runtime, payload map[string]any, searchQuery string) map[string]any {
	rows, _ := pageResults(payload)
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		m, ok := asStringMap(row)
		if !ok {
			continue
		}
		id := fieldID(m)
		rt.State.rememberID(id)
		if opts, ok := anySlice(m["options"]); ok {
			for _, opt := range opts {
				if om, ok := asStringMap(opt); ok {
					rt.State.rememberID(fieldID(om))
				}
			}
		}
		items = append(items, map[string]any{
			"id":          id,
			"name":        asString(m["name"]),
			"description": asString(m["description"]),
			"min_price":   m["min_price"],
			"image_url":   asString(m["image_url"]),
			"options":     m["options"],
		})
	}
	label := "collection"
	title := ""
	if searchQuery != "" {
		label = "search"
		title = searchQuery
	}
	return map[string]any{
		"label": label,
		"title": title,
		"items": items,
	}
}

func groundProductDetail(rt *Runtime, payload map[string]any) {
	if payload == nil {
		return
	}
	rt.State.rememberID(fieldID(payload))
	if opts, ok := anySlice(payload["options"]); ok {
		for _, opt := range opts {
			om, ok := asStringMap(opt)
			if !ok {
				continue
			}
			rt.State.rememberID(fieldID(om))
			if addons, ok := anySlice(om["addons"]); ok {
				for _, a := range addons {
					if am, ok := asStringMap(a); ok {
						rt.State.rememberID(fieldID(am))
					}
				}
			}
		}
	}
	if addons, ok := anySlice(payload["addons"]); ok {
		for _, a := range addons {
			if am, ok := asStringMap(a); ok {
				rt.State.rememberID(fieldID(am))
			}
		}
	}
}

func pageResults(payload map[string]any) ([]any, bool) {
	if payload == nil {
		return nil, false
	}
	if rows, ok := anySlice(payload["results"]); ok {
		return rows, true
	}
	return anySlice(payload)
}

func fieldID(m map[string]any) string {
	for _, key := range []string{"id", "uuid", "pk"} {
		if v := strings.TrimSpace(asString(m[key])); v != "" {
			return v
		}
	}
	return ""
}

func compactStore(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for _, k := range []string{"id", "name", "description", "currency", "delivery_modes", "address", "free_delivery_radius", "delivery_radius", "supported_languages"} {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

func sessionMap(rt *Runtime, key string) (map[string]any, bool) {
	s := rt.session()
	if s == nil || s.SessionData == nil {
		return nil, false
	}
	return asStringMap(s.SessionData[key])
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
