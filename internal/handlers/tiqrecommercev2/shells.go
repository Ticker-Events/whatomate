package tiqrecommercev2

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

// ShellChrome is optional translated chrome the agent may supply for non-English sessions.
// Product, collection, and option names must still appear unchanged in Body when they are required.
type ShellChrome struct {
	Body         string
	Header       string
	Footer       string
	Button       string
	ButtonTitles map[string]string
	Deviation    string
	CaptureLabel string
	OptionName   string
	PaymentURL   string
	SummaryText  string
}

// knownWaitingSteps are the V1 waiting steps the agent may name.
func knownWaitingSteps() map[string]bool {
	return map[string]bool{
		stepWelcome:             true,
		stepCollections:         true,
		stepProducts:            true,
		stepOptions:             true,
		stepQuantity:            true,
		stepCapture:             true,
		stepAddons:              true,
		stepCart:                true,
		stepCartEdit:            true,
		stepFulfillment:         true,
		stepDeliveryPin:         true,
		stepDeliveryFallback:    true,
		stepCheckoutPreview:     true,
		stepCheckoutPreviewFail: true,
		stepDetails:             true,
		stepOrderPlaced:         true,
		stepOrderStatus:         true,
		stepOrderStatusSummary:  true,
	}
}

func (rt *Runtime) markInboundConsumed() {
	if rt == nil || rt.Conv == nil || rt.Conv.ChatCtx() == nil {
		return
	}
	rt.Conv.ChatCtx().SetConsumed(true)
}

// sendShell builds and sends the V1 widget for step. An optional deviation line is sent first.
func (rt *Runtime) sendShell(step string, chrome ShellChrome) error {
	step = strings.TrimSpace(strings.ToLower(step))
	if !knownWaitingSteps()[step] {
		return fmt.Errorf("unknown waiting step %q", step)
	}
	if msg := rt.checkShellPrerequisites(step); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	if msg := rt.checkBindingTap(step); msg != "" {
		return fmt.Errorf("%s", msg)
	}

	rt.markInboundConsumed()

	// Show more: advance the page cursor for the current list/carousel before re-sending.
	if rt.BindingShowMore {
		itemsKey := rt.shellItemsKey(step)
		if itemsKey != "" {
			if s := rt.session(); s != nil && s.SessionData != nil {
				codedflow.AdvanceCursor(s.SessionData, itemsKey)
			}
		}
	}

	if line := strings.TrimSpace(chrome.Deviation); line != "" {
		if err := rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), "v2_deviation", line); err != nil {
			return err
		}
		rt.State.appendTranscript("assistant", line)
	}

	english := rt.englishShellCopy(step, chrome)
	body, header, footer, button := english.body, english.header, english.footer, english.button
	titles := english.titles

	useChrome := rt.State.CustomerLanguage != "" && rt.State.CustomerLanguage != "en"
	if useChrome {
		if msg := validateChrome(english, chrome); msg == "" {
			if chrome.Body != "" {
				body = chrome.Body
			}
			if chrome.Header != "" {
				header = chrome.Header
			}
			if chrome.Footer != "" {
				footer = chrome.Footer
			}
			if chrome.Button != "" {
				button = chrome.Button
			}
			for id, title := range chrome.ButtonTitles {
				if titles == nil {
					titles = map[string]string{}
				}
				titles[id] = title
			}
		}
		// Rejected chrome falls back to English (validateChrome non-empty).
	}

	var err error
	switch step {
	case stepWelcome:
		err = rt.sendButtonsShell("v2_welcome", body, []string{fixedBuyProducts, fixedCheckOrderStatus, fixedTalkToStaff}, titles)
	case stepCollections:
		err = rt.sendListShell("v2_collections", body, header, footer, button, "Collections", "collections", "id", "{{name}}", "{{description}}")
	case stepProducts:
		err = rt.sendProductsShell(body)
	case stepOptions:
		err = rt.sendListShell("v2_options", body, header, footer, button, "Options", "options", "id", "{{row_title}}", "{{row_description}}")
	case stepQuantity, stepCapture, stepAddons, stepCartEdit, stepOrderStatusSummary:
		err = rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), "v2_"+step, body)
	case stepCart:
		err = rt.sendButtonsShell("v2_cart", body, []string{fixedAddMore, fixedEditCart, fixedCheckout}, titles)
	case stepFulfillment:
		err = rt.sendButtonsShell("v2_fulfillment", body, []string{fixedPickup, fixedDelivery}, titles)
	case stepDeliveryPin:
		err = rt.Conv.App().DeliverCodedLocationRequest(rt.Conv.ChatCtx(), "v2_location", body)
		rt.State.AwaitingLocation = true
	case stepDeliveryFallback:
		err = rt.sendButtonsShell("v2_delivery_fallback", body, []string{fixedPickup, fixedShareAgain}, titles)
	case stepCheckoutPreview:
		err = rt.sendButtonsShell("v2_preview", body, []string{fixedConfirmItems, fixedEditCart, fixedAddMore}, titles)
	case stepCheckoutPreviewFail:
		err = rt.sendButtonsShell("v2_preview_fail", body, []string{fixedTryAgain, fixedEditCart}, titles)
	case stepDetails:
		err = rt.sendDetailsShell(body)
	case stepOrderPlaced:
		url := strings.TrimSpace(chrome.PaymentURL)
		if url != "" {
			if err = rt.Conv.App().DeliverCodedCTAURL(rt.Conv.ChatCtx(), "v2_pay", body, "Pay now", url); err != nil {
				err = rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), "v2_pay", body+"\n\nPay here: "+url)
			}
		} else {
			err = rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), "v2_order_placed", body)
		}
	case stepOrderStatus:
		err = rt.sendListShell("v2_orders", body, header, footer, button, "Orders", "orders", "display_uid", "{{display_uid}}", "{{placed_at}}")
	default:
		err = fmt.Errorf("shell not implemented for %s", step)
	}
	if err != nil {
		return err
	}

	rt.Replied = true
	rt.State.WaitingStep = step
	rt.State.appendTranscript("assistant", body)
	if s := rt.session(); s != nil {
		s.CurrentStep = "v2_" + step
	}
	if step == stepDetails {
		rt.State.AwaitingDetails = true
	}
	if step == stepOrderStatusSummary || step == stepOrderPlaced {
		// Caller may end_session separately; mark order-status inactive after summary.
		if step == stepOrderStatusSummary {
			rt.State.OrderStatusActive = false
		}
	}
	return nil
}

type shellCopy struct {
	body   string
	header string
	footer string
	button string
	titles map[string]string
}

func (rt *Runtime) englishShellCopy(step string, chrome ShellChrome) shellCopy {
	titles := defaultFixedActions()
	out := shellCopy{titles: titles}
	switch step {
	case stepWelcome:
		out.body = rt.welcomeBody()
	case stepCollections:
		out.body = "Choose a collection below and we'll show you the items in it."
		out.header = "Our collections"
		out.footer = "Tap Browse to continue"
		out.button = "Browse"
	case stepProducts:
		name := strings.TrimSpace(asString(sessionValue(rt, "collection_name")))
		if rt.State.SearchLabel != "" {
			name = rt.State.SearchLabel
		}
		if name == "" {
			name = "our store"
		}
		out.body = fmt.Sprintf("Here is what's available in *%s*.\n\nTap *Add to cart* on the item you'd like.", name)
	case stepOptions:
		out.body = "This item comes in more than one option. Please choose the one you'd like."
		out.header = "Choose an option"
		out.footer = ""
		out.button = "Choose"
	case stepQuantity:
		opt := strings.TrimSpace(chrome.OptionName)
		if opt == "" {
			opt = strings.TrimSpace(asString(sessionValue(rt, "option_name")))
		}
		if opt == "" {
			opt = "this item"
		}
		out.body = fmt.Sprintf("How many *%s* would you like?\n\nReply with a whole number of 1 or more, for example 1.", opt)
	case stepCapture:
		label := strings.TrimSpace(chrome.CaptureLabel)
		if label == "" {
			label = "Please share your answer."
		}
		out.body = label
	case stepAddons:
		out.body = rt.addonsBody()
	case stepCart:
		out.body = rt.cartShellBody(false)
	case stepCartEdit:
		out.body = rt.cartEditBody()
	case stepFulfillment:
		out.body = "How would you like to receive your order?"
	case stepDeliveryPin:
		out.body = "Please tap Send location to share your delivery pin so we can check if we deliver to you.\n\nLive locations are not supported. Please share a fixed location pin by tapping Send location, or use Attach > Location > Share your location."
	case stepDeliveryFallback:
		out.body = "Would you like to pick up from the store instead, or share another location?"
	case stepCheckoutPreview:
		out.body = rt.previewShellBody()
	case stepCheckoutPreviewFail:
		out.body = "I couldn't load your order total just now.\n\nPlease try again, or edit your cart."
	case stepDetails:
		delivery := strings.EqualFold(strings.TrimSpace(asString(sessionValue(rt, "delivery_mode"))), modeDelivery)
		if delivery {
			out.body = "Please share your name, phone number, and address so we can place your delivery order."
		} else {
			out.body = "Please share your name, email, and phone number so we can place your pickup order."
		}
	case stepOrderPlaced:
		out.body = rt.orderPlacedBody()
	case stepOrderStatus:
		out.body = "Here are your recent orders. Pick one to see its status and summary."
		out.header = "Your orders"
		out.button = "Orders"
	case stepOrderStatusSummary:
		out.body = strings.TrimSpace(chrome.SummaryText)
		if out.body == "" {
			out.body = "Here is your order status."
		}
	}
	return out
}

func validateChrome(english shellCopy, chrome ShellChrome) string {
	if chrome.Body != "" {
		if utf8.RuneCountInString(chrome.Body) > 1024 {
			return "chrome body too long"
		}
		// Catalog names present in English body must still appear in translated body.
		for _, name := range extractStarNames(english.body) {
			if name != "" && !strings.Contains(chrome.Body, name) {
				return "chrome body drops catalog name"
			}
		}
	}
	for id, title := range chrome.ButtonTitles {
		if utf8.RuneCountInString(title) > 20 {
			return fmt.Sprintf("chrome title for %s too long", id)
		}
	}
	if chrome.Header != "" && utf8.RuneCountInString(chrome.Header) > 60 {
		return "chrome header too long"
	}
	if chrome.Footer != "" && utf8.RuneCountInString(chrome.Footer) > 60 {
		return "chrome footer too long"
	}
	if chrome.Button != "" && utf8.RuneCountInString(chrome.Button) > 20 {
		return "chrome button too long"
	}
	return ""
}

func extractStarNames(body string) []string {
	var out []string
	for {
		i := strings.Index(body, "*")
		if i < 0 {
			break
		}
		rest := body[i+1:]
		j := strings.Index(rest, "*")
		if j < 0 {
			break
		}
		name := strings.TrimSpace(rest[:j])
		if name != "" {
			out = append(out, name)
		}
		body = rest[j+1:]
	}
	return out
}

func (rt *Runtime) checkShellPrerequisites(step string) string {
	switch step {
	case stepCheckoutPreview, stepDetails, stepOrderPlaced:
		if len(rt.cart()) == 0 {
			return "cart is empty; cannot show " + step
		}
	case stepOptions:
		opts, _ := anySlice(sessionValue(rt, "options"))
		if len(opts) < 2 {
			return "options shell needs at least two options in session"
		}
	case stepCollections:
		rows, _ := anySlice(sessionValue(rt, "collections"))
		if len(rows) == 0 {
			return "collections not loaded; call list_collections first"
		}
	case stepProducts:
		rows, _ := anySlice(sessionValue(rt, "products"))
		if len(rows) == 0 {
			return "products not loaded; call list_products or search_products first"
		}
	case stepOrderStatus:
		rows, _ := anySlice(sessionValue(rt, "orders"))
		if len(rows) == 0 {
			return "orders not loaded; call list_orders first"
		}
	}
	return ""
}

func (rt *Runtime) checkBindingTap(step string) string {
	id := strings.TrimSpace(rt.BindingButtonID)
	if id == "" {
		return ""
	}
	if codedflow.IsShowMoreID(id) {
		if step != rt.State.WaitingStep && rt.State.WaitingStep != "" {
			return fmt.Sprintf("binding tap show_more requires step %s", rt.State.WaitingStep)
		}
		return ""
	}
	required := bindingRequiredStep(id)
	if required == "" {
		// Catalog / grounded id taps: agent must advance toward that entity.
		if rt.State.isGrounded(id) {
			return ""
		}
		return ""
	}
	if step != required {
		return fmt.Sprintf("binding tap %s requires step %s", id, required)
	}
	return ""
}

func bindingRequiredStep(buttonID string) string {
	switch strings.TrimSpace(buttonID) {
	case fixedBuyProducts, fixedAddMore:
		return stepCollections
	case fixedCheckOrderStatus:
		return stepOrderStatus
	case fixedTalkToStaff:
		return "" // transfer tool, not a shell
	case fixedCheckout:
		return stepFulfillment
	case fixedEditCart:
		return stepCartEdit
	case fixedConfirmItems:
		return stepDetails
	case fixedPickup, fixedDelivery:
		return "" // set_fulfillment then next shell
	case fixedShareAgain:
		return stepDeliveryPin
	case fixedTryAgain:
		return stepCheckoutPreview
	default:
		return ""
	}
}

func (rt *Runtime) shellItemsKey(step string) string {
	switch step {
	case stepCollections:
		return "collections"
	case stepProducts:
		return "products"
	case stepOptions:
		return "options"
	case stepOrderStatus:
		return "orders"
	default:
		return ""
	}
}

func (rt *Runtime) sendButtonsShell(id, body string, buttonIDs []string, titles map[string]string) error {
	buttons := make([]any, 0, len(buttonIDs))
	for _, bid := range buttonIDs {
		title := bid
		if titles != nil {
			if t := titles[bid]; t != "" {
				title = t
			}
		}
		if t, ok := rt.State.FixedActions[bid]; ok && (titles == nil || titles[bid] == "") {
			title = t
		}
		buttons = append(buttons, map[string]any{
			"id":    bid,
			"type":  "reply",
			"title": truncate(title, 20),
		})
		rt.State.rememberID(bid)
	}
	// Split long cart/preview bodies like V1.
	if utf8.RuneCountInString(body) > 1024 {
		question := body
		if idx := strings.LastIndex(body, "\n\n"); idx > 0 {
			details := body[:idx]
			question = strings.TrimSpace(body[idx+2:])
			_ = rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), id+"_text", details)
			body = question
		}
	}
	_, err := rt.Conv.App().ExecChatButtons(rt.Conv.ChatCtx(), id, map[string]any{
		"body":    body,
		"mode":    "reply",
		"source":  "static",
		"buttons": buttons,
	})
	return err
}

func (rt *Runtime) sendListShell(id, body, header, footer, button, section, itemsKey, idField, titleField, descField string) error {
	s := rt.session()
	if s == nil {
		return fmt.Errorf("no session")
	}
	if s.SessionData == nil {
		s.SessionData = models.JSONB{}
	}
	items, _ := anySlice(s.SessionData[itemsKey])
	if len(items) == 0 {
		return fmt.Errorf("%s is empty", itemsKey)
	}
	// Ground all row ids.
	for _, row := range items {
		if m, ok := asStringMap(row); ok {
			rt.State.rememberID(fieldID(m))
			rt.State.rememberID(asString(m["display_uid"]))
		}
	}
	cfg := map[string]any{
		"body":              body,
		"mode":              "list",
		"header":            truncate(header, 60),
		"footer":            truncate(footer, 60),
		"source":            "dynamic",
		"id_field":          idField,
		"items_var":         itemsKey,
		"list_button":       truncate(button, 20),
		"title_field":       titleField,
		"section_title":     truncate(section, 24),
		"description_field": descField,
	}
	_, err := rt.Conv.App().ExecChatButtons(rt.Conv.ChatCtx(), id, cfg)
	return err
}

func (rt *Runtime) sendProductsShell(body string) error {
	s := rt.session()
	if s == nil || s.SessionData == nil {
		return fmt.Errorf("no products")
	}
	products, _ := anySlice(s.SessionData["products"])
	if len(products) == 0 {
		return fmt.Errorf("products is empty")
	}
	for _, row := range products {
		if m, ok := asStringMap(row); ok {
			rt.State.rememberID(fieldID(m))
		}
	}
	if len(products) == 1 {
		m, _ := asStringMap(products[0])
		name := asString(m["name"])
		price := asString(m["min_price"])
		desc := asString(m["description"])
		img := productImageURL(m)
		singleBody := fmt.Sprintf("*%s* (%s)\n\n%s", name, price, desc)
		if strings.TrimSpace(desc) == "" {
			singleBody = fmt.Sprintf("*%s* (%s)", name, price)
		}
		cfg := map[string]any{
			"body":               singleBody,
			"mode":               "reply",
			"source":             "dynamic",
			"dynamic_type":       "reply",
			"id_field":           "id",
			"items_var":          "products",
			"body_field":         "{{name}}",
			"title_field":        "Add to cart",
			"header_image":       img,
			"fallback_media_url": shellFallbackMedia,
			"store_as":           "product_selected",
			"selection_mapping": map[string]any{
				"options":      "options",
				"product_id":   "id",
				"product_name": "name",
			},
		}
		_, err := rt.Conv.App().ExecChatButtons(rt.Conv.ChatCtx(), "v2_product", cfg)
		return err
	}
	// Enrich card bodies for carousel.
	enriched := make([]any, 0, len(products))
	for _, row := range products {
		m, ok := asStringMap(row)
		if !ok {
			continue
		}
		name := asString(m["name"])
		price := asString(m["min_price"])
		m["card_body"] = fmt.Sprintf("%s (%s)", name, price)
		if img := productImageURL(m); img != "" {
			m["images"] = []any{map[string]any{"original_url": img}}
		}
		enriched = append(enriched, m)
	}
	s.SessionData["products"] = enriched
	cfg := map[string]any{
		"body":               body,
		"mode":               "carousel",
		"source":             "dynamic",
		"id_field":           "{{id}}",
		"items_var":          "products",
		"body_field":         "{{card_body}}",
		"media_field":        "{{images[0].original_url}}",
		"title_field":        "Add to cart",
		"fallback_media_url": shellFallbackMedia,
		"store_as":           "product_selected",
		"selection_mapping": map[string]any{
			"options":      "options",
			"product_id":   "id",
			"product_name": "name",
		},
	}
	_, err := rt.Conv.App().ExecChatButtons(rt.Conv.ChatCtx(), "v2_products", cfg)
	return err
}

func productImageURL(m map[string]any) string {
	if u := strings.TrimSpace(asString(m["image_url"])); u != "" {
		return u
	}
	if images, ok := anySlice(m["images"]); ok && len(images) > 0 {
		if im, ok := asStringMap(images[0]); ok {
			if u := strings.TrimSpace(asString(im["original_url"])); u != "" {
				return u
			}
			if u := strings.TrimSpace(asString(im["url"])); u != "" {
				return u
			}
		}
	}
	return shellFallbackMedia
}

func (rt *Runtime) sendDetailsShell(body string) error {
	account := rt.Conv.ChatCtx().Account()
	if account == nil {
		return fmt.Errorf("no account")
	}
	delivery := strings.EqualFold(strings.TrimSpace(asString(sessionValue(rt, "delivery_mode"))), modeDelivery)
	flowID := metaFlowIDFromBinding(account.OrganizationID, account.Name, delivery)
	if flowID == "" {
		flowID = rt.Conv.App().ResolveEcommerceMetaFlowID(account.OrganizationID, account.Name, delivery)
	}
	_, err := rt.Conv.App().ExecChatWhatsAppFlow(rt.Conv.ChatCtx(), "details", map[string]any{
		"flow_id":     flowID,
		"body":        body,
		"header":      "Your details",
		"button_text": "Enter details",
	})
	return err
}

func (rt *Runtime) welcomeBody() string {
	welcome := ""
	if account := rt.Conv.ChatCtx().Account(); account != nil {
		if settings := bindingSettings(account.OrganizationID, account.Name); settings != nil {
			welcome, _ = settings["welcome_message"].(string)
			welcome = strings.TrimSpace(welcome)
		}
	}
	const menuCTA = "What would you like to do?"
	if strings.HasSuffix(welcome, menuCTA) {
		welcome = strings.TrimSpace(strings.TrimSuffix(welcome, menuCTA))
	}
	store, _ := sessionMap(rt, "store")
	body := welcome
	if body == "" && store != nil {
		name := strings.TrimSpace(asString(store["name"]))
		if name != "" {
			body = "Welcome to " + name + "."
		}
	}
	if sum := fulfillmentSummary(store); sum != "" {
		if body != "" {
			body += "\n\n" + sum
		} else {
			body = sum
		}
	}
	if body == "" {
		return menuCTA
	}
	if !strings.Contains(strings.ToLower(body), "what would you like") {
		body += "\n\n" + menuCTA
	}
	return body
}

func (rt *Runtime) cartShellBody(afterEdit bool) string {
	lines := rt.cart()
	if len(lines) == 0 {
		return "Your cart is empty.\n\nPlease choose a collection below to add items, then you can check out."
	}
	summary := formatCartSummary(lines)
	question := "Would you like to add anything else, edit your cart, or place your order?"
	if afterEdit {
		return summary + "\n\n" + question
	}
	opt := strings.TrimSpace(asString(sessionValue(rt, "option_name")))
	if opt == "" && len(lines) > 0 {
		opt = lines[len(lines)-1].OptionName
	}
	added := ""
	if opt != "" {
		added = fmt.Sprintf("*%s* has been added to your cart.\n\n", opt)
	}
	units := 0
	for _, l := range lines {
		units += l.Quantity
	}
	return fmt.Sprintf("%s%s\n\nYou now have *%d* items in your cart.\n\n%s", added, summary, units, question)
}

func (rt *Runtime) cartEditBody() string {
	summary := formatCartSummary(rt.cart())
	return summary + "\n\nTell me what to change. For example:\n- remove item 1\n- remove Kunafa\n- change item 2 to 1\n- reduce Chocolate to 1"
}

func (rt *Runtime) previewShellBody() string {
	summary := formatCartSummary(rt.cart())
	if preview, ok := sessionMap(rt, "checkout_preview"); ok {
		if t := strings.TrimSpace(asString(preview["customer_summary"])); t != "" {
			summary = t
		}
	}
	return summary + "\n\nPlease confirm these items before we continue checkout.\n\nYou can edit quantities or remove items first."
}

func (rt *Runtime) orderPlacedBody() string {
	uid := ""
	if order, ok := sessionMap(rt, "order"); ok {
		uid = strings.TrimSpace(asString(order["display_uid"]))
		if uid == "" {
			uid = strings.TrimSpace(asString(order["order_display_id"]))
		}
	}
	body := "Order placed!"
	if uid != "" {
		body += "\n\nYour order number is " + uid + "."
	}
	return body
}

func (rt *Runtime) addonsBody() string {
	rows, _ := anySlice(sessionValue(rt, "product_addons"))
	if len(rows) == 0 {
		rows, _ = anySlice(sessionValue(rt, "commerce_addon_catalog"))
	}
	var b strings.Builder
	b.WriteString("This product has the following add-ons:\n")
	for i, row := range rows {
		m, ok := asStringMap(row)
		if !ok {
			continue
		}
		name := asString(m["name"])
		price := asString(m["price"])
		line := fmt.Sprintf("%d. %s", i+1, name)
		if price != "" && price != "0" {
			line += " — " + price
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("\nReply with the item number and how many you want, for example item 1 - 2.\n")
	b.WriteString("You can list more than one item. Say Skip if you don't want any.")
	return b.String()
}

func sessionValue(rt *Runtime, key string) any {
	s := rt.session()
	if s == nil || s.SessionData == nil {
		return nil
	}
	return s.SessionData[key]
}

// resendCurrentShell re-sends the waiting step shell with no deviation line.
func (rt *Runtime) resendCurrentShell() error {
	step := strings.TrimSpace(rt.State.WaitingStep)
	if step == "" {
		step = stepWelcome
	}
	return rt.sendShell(step, ShellChrome{})
}
