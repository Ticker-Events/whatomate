package tiqrecommercev2

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
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
	}, instrument(rt, "get_store", func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		payload, ok := rt.callTiqr("store", "get_store", nil)
		if !ok {
			if rt.NeedsMock {
				return errResult("preview needs mock for get_store"), nil
			}
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
	}))); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "list_collections",
		Description: "List product collections. Grounds collection ids for later tools.",
	}, instrument(rt, "list_collections", func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
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
		if s := rt.session(); s != nil {
			if s.SessionData == nil {
				s.SessionData = models.JSONB{}
			}
			s.SessionData["collections"] = rows
			codedflow.SetCursor(s.SessionData, "collections", 0)
		}
		return okResult(summaries, ""), nil
	}))); err != nil {
		return nil, err
	}

	type listProductsArgs struct {
		CollectionID string `json:"collection_id"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "list_products",
		Description: "List products in a grounded collection_id.",
	}, instrument(rt, "list_products", func(_ agent.ToolContext, in listProductsArgs) (toolResult, error) {
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
		grounded := groundProducts(rt, payload, "")
		storeProductsSession(rt, grounded, in.CollectionID)
		return okResult(grounded, ""), nil
	}))); err != nil {
		return nil, err
	}

	type searchArgs struct {
		Query string `json:"query"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "search_products",
		Description: "Search products by free-text query. Results are labeled as search results, not a collection.",
	}, instrument(rt, "search_products", func(_ agent.ToolContext, in searchArgs) (toolResult, error) {
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
		grounded := groundProducts(rt, payload, q)
		storeProductsSession(rt, grounded, "")
		return okResult(grounded, "search results for: "+q), nil
	}))); err != nil {
		return nil, err
	}

	type getProductArgs struct {
		ProductID string `json:"product_id"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "get_product",
		Description: "Load one grounded product including options and add-ons.",
	}, instrument(rt, "get_product", func(_ agent.ToolContext, in getProductArgs) (toolResult, error) {
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
		if s := rt.session(); s != nil {
			if s.SessionData == nil {
				s.SessionData = models.JSONB{}
			}
			if opts, ok := anySlice(payload["options"]); ok {
				s.SessionData["options"] = enrichOptionsForShell(opts)
			}
			if addons, ok := anySlice(payload["addons"]); ok {
				s.SessionData["product_addons"] = addons
			}
			s.SessionData["product_id"] = fieldID(payload)
			s.SessionData["product_name"] = asString(payload["name"])
		}
		return okResult(payload, ""), nil
	}))); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "list_faqs",
		Description: "Load store FAQs.",
	}, instrument(rt, "list_faqs", func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
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
	}))); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "get_store_info",
		Description: "About us and policy fields.",
	}, instrument(rt, "get_store_info", func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		payload, ok := rt.callTiqr("store_info", "get_store_info", nil)
		if !ok {
			return okResult(map[string]any{}, "no store info"), nil
		}
		return okResult(payload, ""), nil
	}))); err != nil {
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
	}, instrument(rt, "add_cart_line", func(_ agent.ToolContext, in addCartArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		line, errMsg := rt.AddCartLine(in.OptionID, in.ProductName, in.OptionName, in.Quantity, in.CaptureFields, in.CaptureLabels, in.CaptureOrder)
		if errMsg != "" {
			return errResult(errMsg), nil
		}
		return okResult(line, formatCartSummary(rt.cart())), nil
	}))); err != nil {
		return nil, err
	}

	type updateCartArgs struct {
		LineID   string `json:"line_id"`
		Quantity int    `json:"quantity"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "update_cart_line",
		Description: "Set quantity on a cart line_id (not option id).",
	}, instrument(rt, "update_cart_line", func(_ agent.ToolContext, in updateCartArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		if msg := rt.UpdateCartLine(in.LineID, in.Quantity); msg != "" {
			return errResult(msg), nil
		}
		return okResult(nil, formatCartSummary(rt.cart())), nil
	}))); err != nil {
		return nil, err
	}

	type removeCartArgs struct {
		LineID string `json:"line_id"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "remove_cart_line",
		Description: "Remove one cart line by line_id.",
	}, instrument(rt, "remove_cart_line", func(_ agent.ToolContext, in removeCartArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		name, msg := rt.RemoveCartLine(in.LineID)
		if msg != "" {
			return errResult(msg), nil
		}
		return okResult(map[string]any{"removed": name}, formatCartSummary(rt.cart())), nil
	}))); err != nil {
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
	}, instrument(rt, "set_addons", func(_ agent.ToolContext, in addonsArgs) (toolResult, error) {
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
	}))); err != nil {
		return nil, err
	}

	type fulfillmentArgs struct {
		Mode string `json:"mode"` // pickup | delivery | PICKUP_FROM_STORE | DELIVERY_TO_LOCATION
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "set_fulfillment",
		Description: "Set delivery_mode to pickup or delivery.",
	}, instrument(rt, "set_fulfillment", func(_ agent.ToolContext, in fulfillmentArgs) (toolResult, error) {
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
	}))); err != nil {
		return nil, err
	}

	type pinArgs struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "set_delivery_pin",
		Description: "Check a delivery pin. Does not quote a fee. Caps out-of-range retries at 3.",
	}, instrument(rt, "set_delivery_pin", func(_ agent.ToolContext, in pinArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		store, _ := sessionMap(rt, "store")
		data, msg := rt.SetDeliveryPin(in.Latitude, in.Longitude, store, storeHasPickup(store))
		if msg != "" {
			return errResult(msg), nil
		}
		return okResult(data, asString(data["message"])), nil
	}))); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "checkout_preview",
		Description: "Priced quote for the session cart. This is the only price the customer should see.",
	}, instrument(rt, "checkout_preview", func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
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
			summary := formatCartSummary(lines)
			if s := rt.session(); s != nil {
				if s.SessionData == nil {
					s.SessionData = models.JSONB{}
				}
				s.SessionData["checkout_preview"] = map[string]any{"customer_summary": summary}
			}
			return okResult(map[string]any{
				"cart":    lines,
				"summary": summary,
				"note":    "preview API unavailable; reply step=checkout_preview then details after confirm",
			}, summary), nil
		}
		if s := rt.session(); s != nil {
			if s.SessionData == nil {
				s.SessionData = models.JSONB{}
			}
			s.SessionData["checkout_preview"] = payload
		}
		return okResult(payload, "reply step=checkout_preview; confirm_items opens details"), nil
	}))); err != nil {
		return nil, err
	}

	type detailsArgs struct {
		Delivery bool `json:"delivery"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "open_details_flow",
		Description: "Open the details waiting-step shell (WhatsApp Flow). Prefer reply step=details after confirm_items.",
	}, instrument(rt, "open_details_flow", func(_ agent.ToolContext, in detailsArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		if s := rt.session(); s != nil {
			if s.SessionData == nil {
				s.SessionData = models.JSONB{}
			}
			if in.Delivery {
				s.SessionData["delivery_mode"] = modeDelivery
			} else if strings.TrimSpace(asString(s.SessionData["delivery_mode"])) == "" {
				s.SessionData["delivery_mode"] = modePickup
			}
		}
		if err := rt.sendShell(stepDetails, ShellChrome{}); err != nil {
			return errResult(err.Error()), nil
		}
		return okResult(nil, "details form sent"), nil
	}))); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "create_order",
		Description: "Place the order after the details Flow is submitted. Refused on confirm_items alone.",
	}, instrument(rt, "create_order", func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		if msg := rt.rejectContradictingBindingTool("create_order", ""); msg != "" {
			return errResult(msg), nil
		}
		if msg := rt.GateCreateOrder(); msg != "" {
			return errResult(msg), nil
		}
		lines := rt.cart()
		if len(lines) == 0 {
			return errResult("cart is empty"), nil
		}
		if rt.capturing() {
			payload := map[string]any{
				"display_uid": "PREVIEW-ORDER",
				"preview":     true,
			}
			if s := rt.session(); s != nil {
				if s.SessionData == nil {
					s.SessionData = models.JSONB{}
				}
				s.SessionData["order"] = payload
			}
			return okResult(payload, "preview-only order; reply step=order_placed then end_session"), nil
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
			if rt.NeedsMock {
				return errResult("preview needs mock for create_order"), nil
			}
			return errResult("create_order failed; ask missing fields or transfer_to_staff"), nil
		}
		if s := rt.session(); s != nil && s.SessionData != nil {
			s.SessionData["order"] = payload
		}
		return okResult(payload, "order placed; reply with order number and Pay now if payment_url is present, then end_session"), nil
	}))); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "list_orders",
		Description: "List recent orders for this WhatsApp number.",
	}, instrument(rt, "list_orders", func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
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
		if s := rt.session(); s != nil {
			if s.SessionData == nil {
				s.SessionData = models.JSONB{}
			}
			s.SessionData["orders"] = rows
		}
		return okResult(rows, ""), nil
	}))); err != nil {
		return nil, err
	}

	type lookupArgs struct {
		OrderDisplayID string `json:"order_display_id"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "lookup_order",
		Description: "Lookup one order by display id. After summarizing, call end_session. Do not attach a pay link.",
	}, instrument(rt, "lookup_order", func(_ agent.ToolContext, in lookupArgs) (toolResult, error) {
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
	}))); err != nil {
		return nil, err
	}

	type replyArgs struct {
		Step          string `json:"step"`
		DeviationLine string `json:"deviation_line"`
		ChromeBody    string `json:"chrome_body"`
		ChromeHeader  string `json:"chrome_header"`
		ChromeFooter  string `json:"chrome_footer"`
		ChromeButton  string `json:"chrome_button"`
		ChromeTitles  string `json:"chrome_titles"` // "id=Title,id2=Title2"
		PaymentURL    string `json:"payment_url"`
		CaptureLabel  string `json:"capture_label"`
		OptionName    string `json:"option_name"`
		SummaryText   string `json:"summary_text"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name: "reply",
		Description: "Name the V1 waiting step so Go sends that WhatsApp shell. Call exactly once per turn. " +
			"Steps: welcome, collections, products, options, quantity, capture, addons, cart, cart_edit, " +
			"fulfillment, delivery_pin, delivery_fallback, checkout_preview, checkout_preview_fail, details, " +
			"order_placed, order_status, order_status_summary. " +
			"Optional deviation_line is one text bubble before the shell. " +
			"chrome_* is translated chrome only when the session language is not English.",
	}, instrument(rt, "reply", func(_ agent.ToolContext, in replyArgs) (toolResult, error) {
		if !rt.bumpRound() {
			return errResult("tool round limit"), nil
		}
		if rt.NeedsMock {
			return errResult("preview needs mock; shell not sent"), nil
		}
		step := strings.TrimSpace(strings.ToLower(in.Step))
		if step == "" {
			return errResult("step is required (name a V1 waiting step)"), nil
		}
		if !knownWaitingSteps()[step] {
			return errResult("unknown waiting step; use a documented V1 step name"), nil
		}
		if msg := rt.rejectContradictingBindingTool("reply", step); msg != "" {
			return errResult(msg), nil
		}
		chrome := ShellChrome{
			Body:         strings.TrimSpace(in.ChromeBody),
			Header:       strings.TrimSpace(in.ChromeHeader),
			Footer:       strings.TrimSpace(in.ChromeFooter),
			Button:       strings.TrimSpace(in.ChromeButton),
			ButtonTitles: parseChromeTitles(in.ChromeTitles),
			Deviation:    strings.TrimSpace(in.DeviationLine),
			CaptureLabel: strings.TrimSpace(in.CaptureLabel),
			OptionName:   strings.TrimSpace(in.OptionName),
			PaymentURL:   strings.TrimSpace(in.PaymentURL),
			SummaryText:  strings.TrimSpace(in.SummaryText),
		}
		if err := rt.sendShell(step, chrome); err != nil {
			return errResult(err.Error()), nil
		}
		return okResult(map[string]any{"step": step}, "shell sent"), nil
	}))); err != nil {
		return nil, err
	}

	type transferArgs struct {
		Message string `json:"message"`
	}
	if err := add(functiontool.New(functiontool.Config{
		Name:        "transfer_to_staff",
		Description: "Hand off to staff. Refused until the customer asked for staff or agreed.",
	}, instrument(rt, "transfer_to_staff", func(_ agent.ToolContext, in transferArgs) (toolResult, error) {
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
		if rt.capturing() {
			rt.Transfer = true
			rt.Replied = true
			_ = rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), "v2_transfer_preview", message)
			return okResult(map[string]any{"preview": true}, "preview-only transfer recorded"), nil
		}
		rt.Transfer = true
		rt.Replied = true
		if err := rt.Conv.Transfer(message); err != nil {
			return errResult(err.Error()), nil
		}
		return okResult(nil, "transferred"), nil
	}))); err != nil {
		return nil, err
	}

	if err := add(functiontool.New(functiontool.Config{
		Name:        "end_session",
		Description: "End the shopping session (after order placed or order status summary).",
	}, instrument(rt, "end_session", func(_ agent.ToolContext, _ emptyArgs) (toolResult, error) {
		rt.Ended = true
		_ = rt.Conv.End()
		return okResult(nil, "ended"), nil
	}))); err != nil {
		return nil, err
	}

	return tools, nil
}

const codedflowAgentHandoff = "I'm connecting you with a team member who can help."

func storeProductsSession(rt *Runtime, grounded map[string]any, collectionID string) {
	s := rt.session()
	if s == nil {
		return
	}
	if s.SessionData == nil {
		s.SessionData = models.JSONB{}
	}
	items, _ := anySlice(grounded["items"])
	s.SessionData["products"] = items
	codedflow.SetCursor(s.SessionData, "products", 0)
	if collectionID != "" {
		rt.State.ActiveCollection = collectionID
		s.SessionData["collection_id"] = collectionID
		// Resolve collection name from loaded collections.
		if rows, ok := anySlice(s.SessionData["collections"]); ok {
			for _, row := range rows {
				m, ok := asStringMap(row)
				if !ok {
					continue
				}
				if fieldID(m) == collectionID {
					name := asString(m["name"])
					if name == "" {
						name = asString(m["title"])
					}
					s.SessionData["collection_name"] = name
					if ai := strings.TrimSpace(asString(m["ai_instructions"])); ai != "" {
						rt.State.AIInstructions = ai
					}
					break
				}
			}
		}
		rt.State.SearchLabel = ""
	}
}

func enrichOptionsForShell(opts []any) []any {
	out := make([]any, 0, len(opts))
	for _, opt := range opts {
		m, ok := asStringMap(opt)
		if !ok {
			continue
		}
		name := asString(m["name"])
		price := asString(m["price"])
		if price == "" {
			price = asString(m["amount"])
		}
		title := name
		if price != "" {
			title = name + " — " + price
		}
		m["row_title"] = title
		m["row_description"] = asString(m["description"])
		out = append(out, m)
	}
	return out
}

func parseChromeTitles(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, title, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		id = strings.TrimSpace(id)
		title = strings.TrimSpace(title)
		if id != "" && title != "" {
			out[id] = title
		}
	}
	return out
}

// rejectContradictingBindingTool rejects tools that disagree with a binding tap.
func (rt *Runtime) rejectContradictingBindingTool(toolName, replyStep string) string {
	id := strings.TrimSpace(rt.BindingButtonID)
	if id == "" {
		return ""
	}
	if codedflow.IsShowMoreID(id) {
		return ""
	}
	switch id {
	case fixedTalkToStaff:
		if toolName == "reply" {
			return "binding tap talk_to_agent requires transfer_to_staff, not a shell"
		}
		return ""
	case fixedConfirmItems:
		if toolName == "create_order" {
			return "binding tap confirm_items requires the details shell first; create_order waits for the Flow submission"
		}
		if toolName == "reply" && replyStep != stepDetails {
			return "binding tap confirm_items requires step details"
		}
	case fixedBuyProducts, fixedAddMore:
		if toolName == "reply" && replyStep != stepCollections {
			return "binding tap requires step collections"
		}
	case fixedCheckOrderStatus:
		if toolName == "reply" && replyStep != stepOrderStatus {
			return "binding tap requires step order_status"
		}
	case fixedEditCart:
		if toolName == "reply" && replyStep != stepCartEdit {
			return "binding tap requires step cart_edit"
		}
	case fixedCheckout:
		if toolName == "reply" && replyStep != stepFulfillment && replyStep != stepDeliveryPin && replyStep != stepCheckoutPreview {
			return "binding tap checkout requires fulfillment, delivery_pin, or checkout_preview"
		}
	}
	return ""
}

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
