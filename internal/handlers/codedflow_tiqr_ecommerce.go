package handlers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/shridarpatil/whatomate/pkg/ticker"
)

// TiQR Ecommerce loads the store and its collections, then lets the
// customer buy, check an order, or talk to an agent.

const (
	tiqrEcommerceKey = "tiqr_ecommerce"

	tiqrEcommerceFlowID       = "1557965846018132"
	tiqrEcommercePickupFlowID = "1484028330223507"

	tiqrEcommerceFallbackMedia = "https://tickerevents.sgp1.cdn.digitaloceanspaces.com/tickerevents/media/images/products/124656/d592fabca6314ee5a749da835b40cf5a-589041f37b35417d8122b31a1b645eee-p.jpg"

	tiqrEcommerceUnavailable = "Sorry, this item isn't available right now.\n\nPlease choose another item from our collections."

	tiqrEcommerceAdded = "*{{option_name}}* has been added to your cart.\n\n{{cart_summary}}\n\nYou now have *{{cart_count}}* items in your cart. Add something else, edit your cart, or check out when you're ready."

	tiqrEcommerceFailed = "We couldn't place your order just now.\n\nPlease try again in a moment. If it still doesn't go through, message us and we'll help you complete it."

	tiqrEcommerceOrderMissing = "I couldn't find a recent order for this phone number."

	tiqrEcommerceSearchEmpty = "I couldn't find that in our store.\n\nPlease choose a collection below, or tell me another item."

	tiqrEcommerceCartEmpty = "Your cart is empty.\n\nPlease choose a collection below to add items, then you can check out."

	tiqrEcommerceHandoffConnect = "So, I'm connecting you with a team member who can help."

	tiqrEcommerceFailBusiness = "I was unable to fetch the business information.\n\n" + tiqrEcommerceHandoffConnect

	tiqrEcommerceFailCollections = "I was unable to fetch our collections.\n\n" + tiqrEcommerceHandoffConnect

	tiqrEcommerceFailProducts = "I was unable to fetch those products.\n\n" + tiqrEcommerceHandoffConnect

	tiqrBuyProducts      = "buy_products"
	tiqrCheckOrderStatus = "check_order_status"
	tiqrTalkToAgent      = "talk_to_agent"
	tiqrAddMore          = "add_more"
	tiqrCheckout         = "checkout"
	tiqrEditCart         = "edit_cart"
	tiqrConfirmItems     = "confirm_items"

	tiqrModePickup   = "PICKUP_FROM_STORE"
	tiqrModeDelivery = "DELIVERY_TO_LOCATION"
	tiqrProceed      = "proceed"
	tiqrPickupMode   = "pickup"
	tiqrDeliveryMode = "delivery"
	tiqrShareAgain   = "share_again"

	tiqrEcommercePickupOnly = "This store offers store pickup only.\n\nYou can browse products and place an order for pickup when you're ready."

	tiqrEcommerceChooseMode = "How would you like to receive your order?"

	tiqrEcommerceLocationPrompt = "Please tap Send location to share your delivery pin so we can check if we deliver to you."
)

func init() {
	registerCodedFlow(newTiqrEcommerceFlow())
}

func newTiqrEcommerceFlow() CodedFlow {
	return CodedFlow{
		Key:  tiqrEcommerceKey,
		Name: "TiQR Ecommerce",
		Description: "Buy products for store pickup or delivery, check an order, or talk to an agent. " +
			"Customer details are collected with a WhatsApp Flow.",
		Steps: []CodedStep{
			{Name: "load_store", Label: "Load store and collections"},
			{Name: "intent", Label: "Buy, order status, or agent"},
			{Name: "buy", Label: "Add items and check out"},
			{Name: "order_status", Label: "Check order status"},
			{Name: "agent", Label: "Talk to an agent"},
		},
		run: tiqrEcommerce,
	}
}

func tiqrEcommerce(c *Conv) error {
	store, ok := c.Store("store", "get_store", nil)
	if !ok {
		return c.Transfer(tiqrEcommerceFailBusiness)
	}
	c.Once("store_currency", func() {
		stashSessionCurrency(c.session(), asString(store["currency"]))
	})
	collections, ok := c.StoreList("collections", "list_collections", map[string]string{"limit": "20"})
	if !ok {
		return c.Transfer(tiqrEcommerceFailCollections)
	}
	route, ok := c.askRouteButtons("intent", menuButtons(store), RouteOptions{AllowCatalog: true})
	if !ok {
		return c.afterCheckoutDivert(collections)
	}
	switch route.Kind {
	case codedRouteCheckout:
		c.divert = codedRouteCheckout
		return c.afterCheckoutDivert(collections)
	case codedRouteCollection:
		return buyProducts(c, collections, route)
	case codedRouteProduct:
		return buyProducts(c, collections, route)
	case codedRouteChoice:
		switch route.ID {
		case tiqrBuyProducts:
			return buyProducts(c, collections, Route{})
		case tiqrCheckOrderStatus:
			return orderStatus(c)
		default:
			return c.Transfer(codedAgentHandoff)
		}
	default:
		return c.Transfer(codedAgentHandoff)
	}
}

// afterCheckoutDivert runs when the menu turn diverted to checkout (or waited).
func (c *Conv) afterCheckoutDivert(collections []any) error {
	res, err := c.applyCheckoutDivert()
	if err != nil || c.ended {
		return err
	}
	switch res {
	case divertEmptyCart:
		return buyProducts(c, collections, Route{})
	default:
		return nil
	}
}

func menuButtons(store map[string]any) ButtonPrompt {
	name := strings.TrimSpace(asString(store["name"]))
	body := "What would you like to do?"
	if name != "" {
		body = "Welcome to " + name + ".\n\nWhat would you like to do?"
	}
	return ButtonPrompt{
		Body: body,
		Buttons: []Button{
			{ID: tiqrBuyProducts, Title: "Buy products"},
			{ID: tiqrCheckOrderStatus, Title: "Check order status"},
			{ID: tiqrTalkToAgent, Title: "Talk to staff"},
		},
		Step: StepNote{
			Doing:  "The customer is at the welcome menu.",
			Expect: "They may pick Buy products, Check order status, or Talk to staff, name a collection or a product, or ask to check out.",
		},
	}
}

func buyProducts(c *Conv, collections []any, first Route) error {
	if !resolveFulfillment(c) {
		return nil
	}
	for {
		route := first
		first = Route{}
		if route.Kind == codedRouteCheckout {
			c.divert = codedRouteCheckout
			res, err := c.applyCheckoutDivert()
			if err != nil || c.ended || c.stop {
				return err
			}
			if res == divertEmptyCart {
				continue
			}
			return nil
		}
		if route.Kind == "" {
			var ok bool
			route, ok = c.askRouteList("collection", collections, ListPrompt{
				Body:        "Choose a collection below and we'll show you the items in it.",
				Header:      "Our collections",
				Footer:      "Tap Browse to continue",
				Button:      "Browse",
				Section:     "Collections",
				ItemsKey:    "collections",
				IDField:     "id",
				Title:       "{{name}}",
				Description: "{{description}}",
				Select: map[string]string{
					"collection_id":   "id",
					"collection_name": "title",
				},
				Step: StepNote{
					Doing:  "The customer is looking at the collection list.",
					Expect: "They may pick a collection, name a product, or ask to check out.",
				},
			}, RouteOptions{AllowCatalog: true})
			if !ok {
				res, err := c.applyCheckoutDivert()
				if err != nil || c.ended || c.stop {
					return err
				}
				if res == divertEmptyCart {
					continue
				}
				return nil
			}
			if route.Kind == codedRouteCheckout {
				c.divert = codedRouteCheckout
				res, err := c.applyCheckoutDivert()
				if err != nil || c.ended || c.stop {
					return err
				}
				if res == divertEmptyCart {
					continue
				}
				return nil
			}
		}
		if route.Kind == codedRouteCollection || (route.Kind == codedRouteChoice && strings.TrimSpace(route.ID) != "") {
			if col := collectionByID(c, route.ID); col != nil && collectionHandoffAfterCapture(col) {
				if route.Kind == codedRouteCollection {
					c.applyCollectionSelection(route)
				} else {
					c.session().SessionData["collection_id"] = route.ID
					if route.Title != "" {
						c.session().SessionData["collection_name"] = route.Title
					}
				}
				productID := soleCategoryProductID(c, route.ID)
				return runAfterCaptureHandoff(c, col, productID)
			}
		}
		products, ok := productsForRoute(c, route)
		if !ok {
			if c.stop || c.ended {
				return nil
			}
			continue
		}
		_, ok = askProducts(c, products)
		if !ok {
			res, err := c.applyCheckoutDivert()
			if err != nil || c.ended || c.stop {
				return err
			}
			if res == divertEmptyCart {
				continue
			}
			return nil
		}
		if product := selectedProductMap(c); product != nil {
			if col := afterCaptureCollectionForProduct(c, product); col != nil {
				return runAfterCaptureHandoff(c, col, fieldString(product, "id"))
			}
		}
		if !addPickedProduct(c) {
			res, err := c.applyCheckoutDivert()
			if err != nil || c.ended || c.stop {
				return err
			}
			if res == divertEmptyCart {
				continue
			}
			return nil
		}
		action, ok := askAfterCartAdd(c)
		if !ok {
			res, err := c.applyCheckoutDivert()
			if err != nil || c.ended || c.stop {
				return err
			}
			if res == divertEmptyCart {
				continue
			}
			return nil
		}
		if action == tiqrCheckout {
			ready, ok := reviewCartBeforeOrder(c)
			if !ok {
				res, err := c.applyCheckoutDivert()
				if err != nil || c.ended || c.stop {
					return err
				}
				if res == divertEmptyCart {
					continue
				}
				return nil
			}
			if !ready {
				continue
			}
			return checkout(c)
		}
		// tiqrAddMore — keep browsing
	}
	return nil
}

// askAfterCartAdd offers Add more / Edit cart / Checkout until the shopper
// either continues browsing or starts checkout.
func askAfterCartAdd(c *Conv) (string, bool) {
	for attempt := 1; ; attempt++ {
		if cartLen(c) == 0 {
			c.Say(tiqrEcommerceCartEmpty)
			return tiqrAddMore, true
		}
		name := "next"
		if attempt > 1 {
			name = fmt.Sprintf("next_%d", attempt)
		}
		next, ok := c.AskButtons(name, ButtonPrompt{
			Body: "Would you like to add anything else, edit your cart, or place your order?",
			Buttons: []Button{
				{ID: tiqrAddMore, Title: "Add more items"},
				{ID: tiqrEditCart, Title: "Edit cart"},
				{ID: tiqrCheckout, Title: "Checkout"},
			},
			Step: StepNote{
				Doing:  "The customer just added an item and can edit the cart or check out.",
				Expect: "They may add more, edit the cart, or check out.",
			},
		})
		if !ok {
			return "", false
		}
		switch next.ID {
		case tiqrAddMore:
			return tiqrAddMore, true
		case tiqrCheckout:
			return tiqrCheckout, true
		case tiqrEditCart:
			if !editCartByInstruction(c, attempt) {
				return "", false
			}
			if cartLen(c) == 0 {
				c.Say(tiqrEcommerceCartEmpty)
				return tiqrAddMore, true
			}
			c.Say(formatTiqrCartSummary(c))
			continue
		default:
			return tiqrAddMore, true
		}
	}
}

// resolveFulfillment asks for delivery mode based on store.delivery_modes
// before browsing. Missing modes keep the previous pickup-only behaviour.
func resolveFulfillment(c *Conv) bool {
	store, _ := asStringMap(c.session().SessionData["store"])
	modes := storeDeliveryModes(store)
	hasPickup := deliveryModesContain(modes, tiqrModePickup)
	hasDelivery := deliveryModesContain(modes, tiqrModeDelivery)

	switch {
	case !hasPickup && !hasDelivery:
		c.Once("fulfillment_default", func() {
			c.session().SessionData["delivery_mode"] = tiqrModePickup
		})
		return !c.stop
	case hasPickup && !hasDelivery:
		_, ok := c.AskButtons("fulfillment_pickup_only", ButtonPrompt{
			Body: tiqrEcommercePickupOnly,
			Buttons: []Button{
				{ID: tiqrProceed, Title: "Proceed"},
			},
			Step: StepNote{
				Doing:  "The store only supports pickup. The customer is confirming before browsing.",
				Expect: "They tap Proceed, or ask to check out if they already have a cart.",
			},
		})
		if !ok {
			return false
		}
		c.session().SessionData["delivery_mode"] = tiqrModePickup
		return true
	case hasPickup && hasDelivery:
		choice, ok := c.AskButtons("fulfillment_mode", ButtonPrompt{
			Body: tiqrEcommerceChooseMode,
			Buttons: []Button{
				{ID: tiqrPickupMode, Title: "Store pickup"},
				{ID: tiqrDeliveryMode, Title: "Delivery"},
			},
			Step: StepNote{
				Doing:  "The customer is choosing pickup or delivery before browsing.",
				Expect: "They pick Store pickup or Delivery.",
			},
		})
		if !ok {
			return false
		}
		if choice.ID == tiqrPickupMode {
			c.session().SessionData["delivery_mode"] = tiqrModePickup
			return true
		}
		c.session().SessionData["delivery_mode"] = tiqrModeDelivery
		return resolveDeliveryLocation(c, true)
	default:
		c.Once("fulfillment_delivery_only", func() {
			c.session().SessionData["delivery_mode"] = tiqrModeDelivery
		})
		return resolveDeliveryLocation(c, false)
	}
}

func resolveDeliveryLocation(c *Conv, pickupAllowed bool) bool {
	store, _ := asStringMap(c.session().SessionData["store"])
	for attempt := 1; ; attempt++ {
		locName := fmt.Sprintf("delivery_location_%d", attempt)
		pin, ok := c.AskLocation(locName, LocationPrompt{
			Body: tiqrEcommerceLocationPrompt,
			Step: StepNote{
				Doing:  "The customer is sharing a delivery location pin.",
				Expect: "A WhatsApp location pin with latitude and longitude.",
			},
		})
		if !ok {
			return false
		}
		result := evaluateStoreDelivery(store, pin.Latitude, pin.Longitude)
		c.Once(fmt.Sprintf("delivery_check_%d", attempt), func() {
			c.session().SessionData[fmt.Sprintf("delivery_check_%d", attempt)] = map[string]any{
				"deliverable": result.Deliverable,
				"zone":        result.Zone,
				"distance_km": result.DistanceKm,
			}
		})
		if result.Deliverable && result.Zone != deliveryZoneOutOfRange {
			c.Say(formatDeliveryEligibilityMessage(store, result))
			c.session().SessionData["delivery_mode"] = tiqrModeDelivery
			c.session().SessionData["delivery_zone"] = result.Zone
			if result.HasDistance {
				c.session().SessionData["delivery_distance_km"] = result.DistanceKm
			}
			if result.ShippingFeePaise > 0 {
				c.session().SessionData["shipping_fee_paise"] = float64(result.ShippingFeePaise)
			}
			return true
		}
		c.Say(formatOutOfRangeDeliveryMessageWithPickup(store, pickupAllowed))
		if !pickupAllowed {
			continue
		}
		choice, ok := c.AskButtons(fmt.Sprintf("delivery_fallback_%d", attempt), ButtonPrompt{
			Body: "Would you like to pick up from the store instead, or share another location?",
			Buttons: []Button{
				{ID: tiqrPickupMode, Title: "Store pickup"},
				{ID: tiqrShareAgain, Title: "Share again"},
			},
			Step: StepNote{
				Doing:  "Delivery is out of range. The customer can switch to pickup or try another pin.",
				Expect: "They pick Store pickup or Share again.",
			},
		})
		if !ok {
			return false
		}
		if choice.ID == tiqrPickupMode {
			c.session().SessionData["delivery_mode"] = tiqrModePickup
			return true
		}
	}
}

func storeDeliveryModes(store map[string]any) []string {
	if store == nil {
		return nil
	}
	raw, ok := store["delivery_modes"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, mode := range v {
			if mode = strings.TrimSpace(mode); mode != "" {
				out = append(out, mode)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if mode := strings.TrimSpace(asString(item)); mode != "" {
				out = append(out, mode)
			}
		}
		return out
	default:
		return nil
	}
}

func deliveryModesContain(modes []string, want string) bool {
	for _, mode := range modes {
		if strings.EqualFold(strings.TrimSpace(mode), want) {
			return true
		}
	}
	return false
}

func formatDeliveryEligibilityMessage(store map[string]any, result storeDeliveryEligibility) string {
	var b strings.Builder
	switch {
	case result.Zone == deliveryZoneFree:
		b.WriteString("Great news — delivery is free to that location.")
	case result.Zone == deliveryZonePaid && result.ShippingFeePaise > 0:
		currency := "INR"
		if store != nil {
			if code := strings.TrimSpace(asString(store["currency"])); code != "" {
				currency = strings.ToUpper(code)
			}
		}
		b.WriteString(fmt.Sprintf("We can deliver there. Delivery fee: %s.", formatMoney(ticker.PaiseToRupees(float64(result.ShippingFeePaise)), currency)))
	case result.Zone == deliveryZonePaid:
		b.WriteString("We can deliver there. An additional delivery fee may apply.")
	default:
		b.WriteString("Thanks — we can deliver to that location.")
	}

	var freeLabel, maxLabel string
	if store != nil {
		freeLabel = radiusKmLabel(store["free_delivery_radius"])
		maxLabel = radiusKmLabel(store["delivery_radius"])
	}
	offers := make([]string, 0, 2)
	if freeLabel != "" {
		offers = append(offers, "- Free delivery within "+freeLabel+" km of our store")
	}
	if maxLabel != "" {
		offers = append(offers, "- Delivery up to "+maxLabel+" km for an additional delivery fee")
	}
	if len(offers) > 0 {
		b.WriteString("\n\nFor reference, we offer:\n\n")
		b.WriteString(strings.Join(offers, "\n"))
	}
	return b.String()
}

type divertResult int

const (
	divertNone divertResult = iota
	divertEmptyCart
	divertCheckoutDone
)

// applyCheckoutDivert clears divert. Empty cart: message and continue shopping.
// Non-empty cart: review items, then run checkout.
func (c *Conv) applyCheckoutDivert() (divertResult, error) {
	if c.divert != codedRouteCheckout {
		return divertNone, nil
	}
	c.divert = ""
	if cartLen(c) == 0 {
		c.Say(tiqrEcommerceCartEmpty)
		return divertEmptyCart, nil
	}
	ready, ok := reviewCartBeforeOrder(c)
	if !ok {
		return divertCheckoutDone, nil
	}
	if !ready {
		return divertEmptyCart, nil
	}
	return divertCheckoutDone, checkout(c)
}

func cartLen(c *Conv) int {
	items, ok := anySlice(c.session().SessionData["tiqr_cart"])
	if !ok {
		return 0
	}
	n := 0
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if strings.TrimSpace(asString(item["product_option"])) == "" {
			continue
		}
		n++
	}
	return n
}

func cartUnitCount(c *Conv) int {
	items, ok := anySlice(c.session().SessionData["tiqr_cart"])
	if !ok {
		return 0
	}
	total := 0
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if strings.TrimSpace(asString(item["product_option"])) == "" {
			continue
		}
		qty := parsePositiveInt(asString(item["quantity"]))
		if qty < 1 {
			continue
		}
		total += qty
	}
	return total
}

func refreshCartSessionFields(c *Conv) {
	c.session().SessionData["cart_count"] = float64(cartUnitCount(c))
	c.session().SessionData["cart_summary"] = formatTiqrCartSummary(c)
}

func formatTiqrCartSummary(c *Conv) string {
	lines := tiqrCartLines(c)
	if len(lines) == 0 {
		return "Your cart is empty."
	}
	currency := sessionCurrencyCode(c.session())
	var b strings.Builder
	b.WriteString("*Your cart:*\n")
	var total float64
	for i, line := range lines {
		qty := line.Qty
		if qty < 1 {
			qty = 1
		}
		// Cart line prices are stored in major currency units after catalog conversion.
		lineTotal := line.Price * float64(qty)
		total += lineTotal
		fmt.Fprintf(&b, "%d. *%s* x%d — %s\n", i+1, tiqrCartLineDisplayName(line), qty, formatMoney(lineTotal, currency))
		if extra := formatLineCapture(line); extra != "" {
			b.WriteString(extra)
		}
	}
	fmt.Fprintf(&b, "\n*Subtotal:* %s", formatMoney(total, currency))
	return strings.TrimSpace(b.String())
}

type tiqrCartLine struct {
	OptionID    string
	Name        string // option name; used for edit matching
	ProductName string
	Qty         int
	Price       float64
	Capture     map[string]any
	Labels      map[string]string
}

// tiqrCartLineDisplayName returns "Product Name(Option Name)" when both are
// present, otherwise the option name alone (or Option {id} fallback).
func tiqrCartLineDisplayName(line tiqrCartLine) string {
	name := strings.TrimSpace(line.Name)
	productName := strings.TrimSpace(line.ProductName)
	if productName != "" && name != "" {
		return productName + "(" + name + ")"
	}
	if name != "" {
		return name
	}
	if productName != "" {
		return productName
	}
	if line.OptionID != "" {
		return "Option " + line.OptionID
	}
	return ""
}

func tiqrCartLines(c *Conv) []tiqrCartLine {
	cart, ok := anySlice(c.session().SessionData["tiqr_cart"])
	if !ok {
		return nil
	}
	out := make([]tiqrCartLine, 0, len(cart))
	for _, entry := range cart {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		optionID := strings.TrimSpace(asString(item["product_option"]))
		if optionID == "" {
			continue
		}
		name := strings.TrimSpace(asString(item["option_name"]))
		if name == "" {
			name = "Option " + optionID
		}
		qty := parsePositiveInt(asString(item["quantity"]))
		if qty < 1 {
			qty = 1
		}
		price, _ := anyToFloat64(item["price"])
		out = append(out, tiqrCartLine{
			OptionID:    optionID,
			Name:        name,
			ProductName: strings.TrimSpace(asString(item["product_name"])),
			Qty:         qty,
			Price:       price,
			Capture:     lineCaptureMap(item["capture_fields"]),
			Labels:      lineLabelMap(item["capture_labels"]),
		})
	}
	return out
}

func lineCaptureMap(raw any) map[string]any {
	fields, ok := asStringMap(raw)
	if !ok || len(fields) == 0 {
		return nil
	}
	return fields
}

func lineLabelMap(raw any) map[string]string {
	fields, ok := asStringMap(raw)
	if !ok || len(fields) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range fields {
		if label := strings.TrimSpace(asString(value)); label != "" {
			out[key] = label
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func formatLineCapture(line tiqrCartLine) string {
	if len(line.Capture) == 0 {
		return ""
	}
	keys := make([]string, 0, len(line.Capture))
	for key := range line.Capture {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		value := formatCaptureAnswer(line.Capture[key])
		if value == "" {
			continue
		}
		label := strings.TrimSpace(line.Labels[key])
		if label == "" {
			label = key
		}
		fmt.Fprintf(&b, "   %s: %s\n", label, value)
	}
	return b.String()
}

func formatCaptureAnswer(value any) string {
	switch typed := value.(type) {
	case []string:
		return strings.Join(nonEmpty(typed...), ", ")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "<nil>" {
			return ""
		}
		return text
	}
}

func productsForRoute(c *Conv, route Route) ([]any, bool) {
	switch route.Kind {
	case codedRouteProduct:
		query := strings.TrimSpace(route.Query)
		if query == "" {
			c.Say(tiqrEcommerceSearchEmpty)
			return nil, false
		}
		c.session().SessionData["collection_name"] = query
		payload, ok := c.Store("products", "search_products", map[string]string{
			"search": query,
			"limit":  "20",
		})
		if !ok {
			if c.chat != nil && strings.TrimSpace(c.chat.lastTiqrErr) != "" {
				_ = c.Transfer(tiqrEcommerceFailProducts)
			} else {
				c.Say(tiqrEcommerceSearchEmpty)
			}
			return nil, false
		}
		items, _ := anySlice(payload["results"])
		if len(items) == 0 {
			c.Say(tiqrEcommerceSearchEmpty)
			return nil, false
		}
		c.session().SessionData["products"] = items
		return items, true
	case codedRouteChoice, codedRouteCollection:
		id := strings.TrimSpace(route.ID)
		if id == "" {
			c.Say(tiqrEcommerceSearchEmpty)
			return nil, false
		}
		if route.Kind == codedRouteCollection {
			c.applyCollectionSelection(route)
		}
		products, ok := c.StoreList("products", "list_products", map[string]string{"category_id": id})
		if !ok {
			if c.chat != nil && strings.TrimSpace(c.chat.lastTiqrErr) != "" {
				_ = c.Transfer(tiqrEcommerceFailProducts)
			} else {
				c.Say(tiqrEcommerceSearchEmpty)
			}
			return nil, false
		}
		return products, true
	default:
		_ = c.Transfer(codedAgentHandoff)
		return nil, false
	}
}

// askProducts shows one product as an image reply with Add to cart. WhatsApp
// carousels need at least two cards, and several collections have one product.
func askProducts(c *Conv, products []any) (Choice, bool) {
	if len(products) < 2 {
		return c.AskImageButtons("product", products, singleProductCTA())
	}
	return c.AskCarousel("product", products, productCards())
}

func singleProductCTA() ImageButtonPrompt {
	return ImageButtonPrompt{
		Body:          "*{{products[0].name}}* ({{currency_symbol}}{{products[0].min_price}})\n\n{{products[0].description}}",
		HeaderImage:   "{{products[0].images[0].original_url}}",
		FallbackMedia: tiqrEcommerceFallbackMedia,
		ItemsKey:      "products",
		IDField:       "id",
		BodyField:     "{{name}} ({{currency_symbol}}{{min_price}})",
		Title:         "Add to cart",
		StoreAs:       "product_selected",
		Select: map[string]string{
			"options":      "options",
			"product_id":   "id",
			"product_name": "name",
		},
		Step: StepNote{
			Doing:  "One product is on screen.",
			Expect: "They may accept it or name it.",
		},
	}
}

func productCards() CarouselPrompt {
	return CarouselPrompt{
		Body:          "Here is what's available in *{{collection_name}}*.\n\nTap *Add to cart* on the item you'd like.",
		ItemsKey:      "products",
		IDField:       "{{id}}",
		StoreAs:       "product_selected",
		BodyField:     "{{name}} ({{currency_symbol}}{{min_price}})",
		MediaField:    "{{images[0].original_url}}",
		Title:         "Add to cart",
		FallbackMedia: tiqrEcommerceFallbackMedia,
		Select: map[string]string{
			"options":      "options",
			"product_id":   "id",
			"product_name": "name",
		},
		Step: StepNote{
			Doing:  "Several products are on screen.",
			Expect: "They name one of those products.",
		},
	}
}

func addPickedProduct(c *Conv) bool {
	switch n := listLen(c, "options"); {
	case n == 0:
		c.Say(tiqrEcommerceUnavailable)
	case n == 1:
		id, name, ok := optionAt(c, 0)
		if !ok {
			c.Say(tiqrEcommerceUnavailable)
			break
		}
		c.session().SessionData["option_id"] = id
		c.session().SessionData["option_name"] = name
		if !askQuantityAndAdd(c) {
			return false
		}
	default:
		_, ok := c.AskList("option", nil, ListPrompt{
			Body:        "This item comes in more than one option. Please choose the one you'd like.",
			Header:      "Choose an option",
			Button:      "Choose",
			Section:     "Options",
			ItemsKey:    "options",
			IDField:     "id",
			Title:       "{{name}} ({{currency_symbol}}{{price}})",
			Description: "{{description}}",
			Select: map[string]string{
				"option_id":   "id",
				"option_name": "name",
			},
			Step: StepNote{
				Doing:  "The product has more than one option on screen.",
				Expect: "They pick one of those options.",
			},
		})
		if !ok {
			return false
		}
		if !askQuantityAndAdd(c) {
			return false
		}
	}
	return true
}

func askQuantityAndAdd(c *Conv) bool {
	quantity, ok := c.AskNumber("quantity", NumberPrompt{
		Body:    "How many *{{option_name}}* would you like?\n\nReply with a whole number of 1 or more, for example 1.",
		Pattern: `^[1-9][0-9]*$`,
		Step: StepNote{
			Doing:  "The customer is saying how many units to add.",
			Expect: "A whole number of 1 or more, as digits or as a number word such as two.",
		},
	})
	if !ok {
		return false
	}
	captured, order, ok := askCollectionCaptureFields(c)
	if !ok {
		return false
	}
	optionID := asString(c.session().SessionData["option_id"])
	c.Once("cart", func() {
		upsertCartItem(c, optionID, quantity, captured, order)
	})
	refreshCartSessionFields(c)
	c.Say(tiqrEcommerceAdded)
	return true
}

// askCollectionCaptureFields asks each required collection field as its own
// question before the option is added to the cart. Answers are stored on the
// session and returned so the cart line can keep the values from this add.
// order is the field key sequence as asked.
func askCollectionCaptureFields(c *Conv) (map[string]any, []string, bool) {
	fields := currentCollectionCaptureFields(c)
	captured := map[string]any{}
	order := make([]string, 0, len(fields))
	for i, field := range fields {
		name := captureCallName(i, asString(field["key"]))
		value, ok := c.askCaptureField(name, field)
		if !ok {
			return nil, nil, false
		}
		key := asString(field["key"])
		captured[key] = value
		order = append(order, key)
		saveSessionCapture(c, field, value)
	}
	return captured, order, true
}

func (c *Conv) askCaptureField(name string, field map[string]any) (any, bool) {
	if c.stop {
		return nil, false
	}
	if rec, done := c.doneCall(); done {
		if !callOK(rec) {
			return nil, false
		}
		value, ok := rec["value"]
		if !ok {
			return nil, false
		}
		return value, true
	}
	body := strings.TrimSpace(promptCaptureField(field))
	if body == "" {
		body = "Please share " + asString(field["label"]) + "."
	}
	if c.noAnswerYet() {
		if !c.sendCapturePrompt(name, body) {
			return nil, false
		}
		c.wait(name)
		return nil, false
	}
	input := strings.TrimSpace(c.chat.userInput)
	if isCheckoutStartIntent(input) && !validCaptureValue(field, input) {
		c.chat.consumed = true
		c.divert = codedRouteCheckout
		return nil, false
	}
	if input == "" || !validCaptureValue(field, input) {
		c.chat.consumed = true
		if !c.sendCapturePrompt(name, "Please provide a valid value.\n"+body) {
			return nil, false
		}
		c.wait(name)
		return nil, false
	}
	value := normalizedCaptureValue(field, input)
	c.chat.consumed = true
	c.appendCall(map[string]any{"name": name, "ok": true, "value": value})
	return value, true
}

func (c *Conv) sendCapturePrompt(name, body string) bool {
	node := &ChatNode{ID: name, Type: ChatNodeMessage, Config: map[string]any{"message": c.text(body)}}
	if _, err := c.app.execChatMessage(node, c.chat); err != nil {
		c.fail(err)
		return false
	}
	if c.chat.capturing() {
		c.chat.preview.expectText()
	}
	return true
}

func captureCallName(index int, key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(key)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	safe := strings.Trim(b.String(), "_")
	if safe == "" {
		safe = "field"
	}
	return fmt.Sprintf("capture_%d_%s", index, safe)
}

func saveSessionCapture(c *Conv, field map[string]any, value any) {
	key := asString(field["key"])
	if key == "" || c == nil || c.session() == nil {
		return
	}
	captured := jsonMapFromSession(c.session(), "commerce_captured_fields")
	captured[key] = value
	c.session().SessionData["commerce_captured_fields"] = map[string]any(captured)
	labels := jsonMapFromSession(c.session(), "commerce_capture_labels")
	if label := asString(field["label"]); label != "" {
		labels[key] = label
	}
	c.session().SessionData["commerce_capture_labels"] = map[string]any(labels)
}

func currentCollectionCaptureFields(c *Conv) []map[string]any {
	if product := selectedProductMap(c); product != nil {
		if cat, ok := asStringMap(product["category"]); ok {
			if fields := requiredCaptureFieldsFrom(cat); len(fields) > 0 {
				return fields
			}
			if id := fieldString(cat, "id"); id != "" {
				if col := collectionByID(c, id); col != nil {
					return requiredCaptureFieldsFrom(col)
				}
				return nil
			}
		}
		if id := productCategoryID(product); id != "" {
			if col := collectionByID(c, id); col != nil {
				return requiredCaptureFieldsFrom(col)
			}
			return nil
		}
	}
	if id := strings.TrimSpace(asString(c.session().SessionData["collection_id"])); id != "" {
		if col := collectionByID(c, id); col != nil {
			return requiredCaptureFieldsFrom(col)
		}
	}
	return nil
}

func selectedProductMap(c *Conv) map[string]any {
	want := strings.TrimSpace(asString(c.session().SessionData["product_id"]))
	if want == "" {
		return nil
	}
	items, ok := anySlice(c.session().SessionData["products"])
	if !ok {
		return nil
	}
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if fieldString(item, "id") == want {
			return item
		}
	}
	return nil
}

func productCategoryID(product map[string]any) string {
	if product == nil {
		return ""
	}
	if cat, ok := asStringMap(product["category"]); ok {
		if id := fieldString(cat, "id"); id != "" && id != "0" {
			return id
		}
	}
	for _, key := range []string{"category_id", "collection_id"} {
		if id := fieldString(product, key); id != "" && id != "0" {
			return id
		}
	}
	if _, isMap := asStringMap(product["category"]); !isMap {
		if id := fieldString(product, "category"); id != "" && id != "0" && !strings.HasPrefix(id, "map[") {
			return id
		}
	}
	return ""
}

func collectionByID(c *Conv, id string) map[string]any {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	items, ok := anySlice(c.session().SessionData["collections"])
	if !ok {
		return nil
	}
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if fieldString(item, "id") == id {
			return item
		}
	}
	return nil
}

// requiredCaptureFieldsFrom keeps required collection fields that have a key,
// label, and type. Optional and incomplete fields are not asked.
func requiredCaptureFieldsFrom(raw map[string]any) []map[string]any {
	if raw == nil {
		return nil
	}
	var items []any
	switch fields := raw["required_capture_fields"].(type) {
	case []any:
		items = fields
	case []map[string]any:
		items = make([]any, 0, len(fields))
		for _, field := range fields {
			items = append(items, field)
		}
	default:
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, entry := range items {
		field, ok := asStringMap(entry)
		if !ok || !captureFieldRequired(field["required"]) {
			continue
		}
		key := strings.TrimSpace(asString(field["key"]))
		label := strings.TrimSpace(asString(field["label"]))
		fieldType := strings.TrimSpace(asString(field["type"]))
		if key == "" || label == "" || fieldType == "" {
			continue
		}
		out = append(out, map[string]any{
			"key":       key,
			"label":     label,
			"type":      fieldType,
			"help_text": strings.TrimSpace(asString(field["help_text"])),
			"options":   captureOptionList(field["options"]),
		})
	}
	return out
}

func captureFieldRequired(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	case float64:
		return typed != 0
	case int:
		return typed != 0
	default:
		return false
	}
}

func captureOptionList(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	case []string:
		out := make([]any, len(typed))
		for i, option := range typed {
			out[i] = option
		}
		return out
	default:
		return nil
	}
}

func upsertCartItem(c *Conv, optionID, quantity string, captured map[string]any, captureOrder ...[]string) {
	optionID = strings.TrimSpace(optionID)
	qty := parsePositiveInt(quantity)
	if optionID == "" || qty < 1 {
		return
	}
	cart, _ := anySlice(c.session().SessionData["tiqr_cart"])
	name := strings.TrimSpace(asString(c.session().SessionData["option_name"]))
	productName := strings.TrimSpace(asString(c.session().SessionData["product_name"]))
	price := optionPriceFromSession(c, optionID)
	labels := lineCaptureLabels(c, captured)
	var order []string
	if len(captureOrder) > 0 {
		order = captureOrder[0]
	}
	for i, entry := range cart {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if strings.TrimSpace(asString(item["product_option"])) != optionID {
			continue
		}
		if captureDiffers(item["capture_fields"], captured) {
			continue
		}
		existing := parsePositiveInt(asString(item["quantity"]))
		if existing < 0 {
			existing = 0
		}
		item["quantity"] = strconv.Itoa(existing + qty)
		if name != "" {
			item["option_name"] = name
		}
		if productName != "" {
			item["product_name"] = productName
		}
		if price > 0 {
			item["price"] = price
		}
		applyLineCapture(item, captured, labels, order)
		cart[i] = item
		c.session().SessionData["tiqr_cart"] = cart
		refreshCartSessionFields(c)
		return
	}
	item := map[string]any{
		"product_option": optionID,
		"quantity":       strconv.Itoa(qty),
	}
	if name != "" {
		item["option_name"] = name
	}
	if productName != "" {
		item["product_name"] = productName
	}
	if price > 0 {
		item["price"] = price
	}
	applyLineCapture(item, captured, labels, order)
	cart = append(cart, item)
	c.session().SessionData["tiqr_cart"] = cart
	refreshCartSessionFields(c)
}

func lineCaptureLabels(c *Conv, captured map[string]any) map[string]any {
	if len(captured) == 0 || c == nil || c.session() == nil {
		return nil
	}
	all := jsonMapFromSession(c.session(), "commerce_capture_labels")
	out := map[string]any{}
	for key := range captured {
		if label := asString(all[key]); label != "" {
			out[key] = label
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func captureDiffers(existing any, next map[string]any) bool {
	if len(next) == 0 {
		return false
	}
	current := lineCaptureMap(existing)
	if len(current) == 0 {
		return false
	}
	if len(current) != len(next) {
		return true
	}
	for key, value := range next {
		if formatCaptureAnswer(current[key]) != formatCaptureAnswer(value) {
			return true
		}
	}
	return false
}

func applyLineCapture(item, captured, labels map[string]any, captureOrder []string) {
	if len(captured) == 0 {
		return
	}
	item["capture_fields"] = map[string]any(cloneJSONMap(captured))
	if len(labels) > 0 {
		item["capture_labels"] = map[string]any(cloneJSONMap(labels))
	}
	order := make([]any, 0, len(captureOrder))
	seen := map[string]bool{}
	for _, key := range captureOrder {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		if _, ok := captured[key]; !ok {
			continue
		}
		seen[key] = true
		order = append(order, key)
	}
	if len(order) == 0 {
		keys := make([]string, 0, len(captured))
		for key := range captured {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			order = append(order, key)
		}
	}
	item["capture_order"] = order
}

func optionPriceFromSession(c *Conv, optionID string) float64 {
	items, ok := anySlice(c.session().SessionData["options"])
	if !ok {
		return 0
	}
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if fieldString(item, "id") != optionID {
			continue
		}
		if p, ok := anyToFloat64(item["price"]); ok {
			return p
		}
	}
	return 0
}

func setTiqrCartLineQty(c *Conv, optionID string, qty int) bool {
	optionID = strings.TrimSpace(optionID)
	if optionID == "" || qty < 1 {
		return false
	}
	cart, ok := anySlice(c.session().SessionData["tiqr_cart"])
	if !ok {
		return false
	}
	for i, entry := range cart {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if strings.TrimSpace(asString(item["product_option"])) != optionID {
			continue
		}
		item["quantity"] = strconv.Itoa(qty)
		cart[i] = item
		c.session().SessionData["tiqr_cart"] = cart
		refreshCartSessionFields(c)
		return true
	}
	return false
}

func removeTiqrCartLine(c *Conv, optionID string) (bool, string) {
	optionID = strings.TrimSpace(optionID)
	if optionID == "" {
		return false, ""
	}
	cart, ok := anySlice(c.session().SessionData["tiqr_cart"])
	if !ok {
		return false, ""
	}
	next := make([]any, 0, len(cart))
	removedName := ""
	for _, entry := range cart {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if strings.TrimSpace(asString(item["product_option"])) == optionID {
			removedName = strings.TrimSpace(asString(item["option_name"]))
			if removedName == "" {
				removedName = "Option " + optionID
			}
			continue
		}
		next = append(next, item)
	}
	if removedName == "" {
		return false, ""
	}
	c.session().SessionData["tiqr_cart"] = next
	refreshCartSessionFields(c)
	return true, removedName
}

// reviewCartBeforeOrder shows the full cart and waits for confirm, edit, or add more.
// ready=true means proceed to order details; ready=false means keep shopping.
func reviewCartBeforeOrder(c *Conv) (ready bool, ok bool) {
	for attempt := 1; ; attempt++ {
		if cartLen(c) == 0 {
			c.Say(tiqrEcommerceCartEmpty)
			return false, true
		}
		refreshCartSessionFields(c)
		choice, got := c.AskButtons(fmt.Sprintf("cart_review_%d", attempt), ButtonPrompt{
			Body: formatTiqrCartSummary(c) + "\n\nPlease confirm these items before we continue checkout.\n\nYou can edit quantities or remove items first.",
			Buttons: []Button{
				{ID: tiqrConfirmItems, Title: "Confirm items"},
				{ID: tiqrEditCart, Title: "Edit cart"},
				{ID: tiqrAddMore, Title: "Add more"},
			},
			Step: StepNote{
				Doing:  "The customer is reviewing the full cart before checkout.",
				Expect: "They confirm items, edit the cart, or add more products.",
			},
		})
		if !got {
			return false, false
		}
		switch choice.ID {
		case tiqrConfirmItems:
			return true, true
		case tiqrAddMore:
			return false, true
		case tiqrEditCart:
			if !editCartByInstruction(c, attempt) {
				return false, false
			}
		}
	}
}

type tiqrCartEditIntent struct {
	Action     string // "remove" or "set_qty"
	Index      int    // 1-based; 0 if unknown
	NameQuery  string // optional name fragment
	Qty        int    // for set_qty; 0 if unknown
	Ambiguous  bool
	Incomplete bool
}

var (
	tiqrRemoveItemRE = regexp.MustCompile(`(?i)^\s*(?:remove|delete)\s+(?:item\s*#?\s*)?(.+?)\s*$`)
	tiqrSetQtyRE     = regexp.MustCompile(`(?i)^\s*(?:change|update|set|reduce|make)?\s*(?:quantity\s+(?:of\s+)?)?(?:item\s*#?\s*)?(.+?)\s+(?:to|x|=)\s*(\d{1,4})\s*$`)
	tiqrItemIndexRE  = regexp.MustCompile(`(?i)^\s*(?:item\s*#?\s*)?(\d{1,4})\s*$`)
)

func editCartByInstruction(c *Conv, attempt int) bool {
	if cartLen(c) == 0 {
		c.Say(tiqrEcommerceCartEmpty)
		return true
	}
	refreshCartSessionFields(c)
	prompt := formatTiqrCartSummary(c) + "\n\nTell me what to change. For example:\n" +
		"- remove item 1\n" +
		"- remove Kunafa\n" +
		"- change item 2 to 1\n" +
		"- reduce Chocolate to 1"
	text, ok := c.AskText(fmt.Sprintf("cart_edit_text_%d", attempt), prompt, StepNote{
		Doing:  "The customer is editing the cart with a free-text instruction.",
		Expect: "A remove or quantity change, using an item number or item name.",
	})
	if !ok {
		return false
	}

	intent := parseTiqrCartEdit(text, tiqrCartLines(c))
	if intent.Incomplete && intent.Action == "" {
		// Treat bare text as a target; ask what to do.
		follow, ok := c.AskText(fmt.Sprintf("cart_edit_action_%d", attempt),
			"Should I remove that item, or change its quantity?\n\nReply like: remove, or change to 2.",
			StepNote{
				Doing:  "Clarifying whether to remove or change quantity.",
				Expect: "remove, or a quantity like change to 2.",
			})
		if !ok {
			return false
		}
		intent = mergeTiqrCartEditFollowUp(intent, follow, tiqrCartLines(c))
	}

	if intent.Action == "" {
		c.Say("I couldn't understand that edit. Please try again, for example: remove item 1, or change Kunafa to 2.")
		return true
	}

	optionID, name, ok := resolveTiqrCartEditTarget(c, attempt, intent)
	if !ok {
		return false
	}
	if optionID == "" {
		c.Say("I couldn't find that item in your cart.\n\n" + formatTiqrCartSummary(c))
		return true
	}

	if intent.Action == "remove" {
		var removed string
		c.Once(fmt.Sprintf("cart_edit_remove_%d", attempt), func() {
			_, removed = removeTiqrCartLine(c, optionID)
		})
		if removed == "" {
			removed = name
		}
		if removed == "" {
			removed = "that item"
		}
		if cartLen(c) == 0 {
			c.Say("Removed *" + removed + "* from your cart.\n\nYour cart is empty.")
			return true
		}
		c.Say("Removed *" + removed + "* from your cart.\n\n" + formatTiqrCartSummary(c))
		return true
	}

	qty := intent.Qty
	if qty < 1 {
		qtyText, ok := c.AskNumber(fmt.Sprintf("cart_edit_qty_%d", attempt), NumberPrompt{
			Body:    "What quantity would you like for *" + name + "*?\n\nReply with a whole number of 1 or more.",
			Pattern: `^[1-9][0-9]*$`,
			Step: StepNote{
				Doing:  "Collecting the new quantity for a cart line.",
				Expect: "A whole number of 1 or more.",
			},
		})
		if !ok {
			return false
		}
		qty = parsePositiveInt(qtyText)
	}
	c.Once(fmt.Sprintf("cart_edit_setqty_%d", attempt), func() {
		setTiqrCartLineQty(c, optionID, qty)
	})
	c.Say(fmt.Sprintf("Updated *%s* to quantity %d.\n\n%s", name, qty, formatTiqrCartSummary(c)))
	return true
}

func parseTiqrCartEdit(text string, lines []tiqrCartLine) tiqrCartEditIntent {
	text = strings.TrimSpace(text)
	if text == "" {
		return tiqrCartEditIntent{Incomplete: true}
	}
	lower := strings.ToLower(text)

	if match := tiqrRemoveItemRE.FindStringSubmatch(text); len(match) == 2 {
		target := strings.TrimSpace(match[1])
		intent := tiqrCartEditIntent{Action: "remove"}
		return attachTiqrCartTarget(intent, target, lines)
	}

	if match := tiqrSetQtyRE.FindStringSubmatch(text); len(match) == 3 {
		target := strings.TrimSpace(match[1])
		// Strip leading verbs left on the target ("quantity of Kunafa").
		target = strings.TrimSpace(regexp.MustCompile(`(?i)^(quantity\s+of\s+|qty\s+of\s+)`).ReplaceAllString(target, ""))
		intent := tiqrCartEditIntent{
			Action: "set_qty",
			Qty:    parsePositiveInt(match[2]),
		}
		return attachTiqrCartTarget(intent, target, lines)
	}

	// "Kunafa to 2" without change/set prefix — already covered by set qty RE with optional verb.
	// Bare "remove" / "change quantity".
	switch lower {
	case "remove", "delete":
		return tiqrCartEditIntent{Action: "remove", Incomplete: true}
	case "change", "update", "change quantity", "update quantity", "change qty", "update qty":
		return tiqrCartEditIntent{Action: "set_qty", Incomplete: true}
	}

	// Bare item reference — need action follow-up.
	intent := tiqrCartEditIntent{Incomplete: true}
	return attachTiqrCartTarget(intent, text, lines)
}

func attachTiqrCartTarget(intent tiqrCartEditIntent, target string, lines []tiqrCartLine) tiqrCartEditIntent {
	target = strings.TrimSpace(target)
	if target == "" {
		intent.Incomplete = true
		return intent
	}
	if match := tiqrItemIndexRE.FindStringSubmatch(target); len(match) == 2 {
		intent.Index = parsePositiveInt(match[1])
		if intent.Index < 1 || intent.Index > len(lines) {
			intent.Incomplete = true
		}
		return intent
	}
	// Strip leading "item " if present with a name.
	target = strings.TrimSpace(regexp.MustCompile(`(?i)^item\s+`).ReplaceAllString(target, ""))
	intent.NameQuery = target
	matches := findTiqrCartLinesByName(lines, target)
	switch len(matches) {
	case 0:
		intent.Incomplete = true
	case 1:
		intent.Index = matches[0]
	default:
		intent.Ambiguous = true
		intent.Incomplete = true
	}
	return intent
}

func findTiqrCartLinesByName(lines []tiqrCartLine, query string) []int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	var exact, partial []int
	for i, line := range lines {
		name := strings.ToLower(line.Name)
		if name == query {
			exact = append(exact, i+1)
			continue
		}
		if strings.Contains(name, query) {
			partial = append(partial, i+1)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

func mergeTiqrCartEditFollowUp(base tiqrCartEditIntent, follow string, lines []tiqrCartLine) tiqrCartEditIntent {
	follow = strings.TrimSpace(follow)
	lower := strings.ToLower(follow)
	if strings.HasPrefix(lower, "remove") || lower == "delete" {
		base.Action = "remove"
		if match := tiqrRemoveItemRE.FindStringSubmatch(follow); len(match) == 2 {
			return attachTiqrCartTarget(tiqrCartEditIntent{Action: "remove"}, match[1], lines)
		}
		base.Incomplete = base.Index < 1 && base.NameQuery == ""
		return base
	}
	if match := tiqrSetQtyRE.FindStringSubmatch(follow); len(match) == 3 {
		intent := tiqrCartEditIntent{Action: "set_qty", Qty: parsePositiveInt(match[2])}
		if base.Index > 0 {
			intent.Index = base.Index
			return intent
		}
		if base.NameQuery != "" {
			return attachTiqrCartTarget(intent, base.NameQuery, lines)
		}
		return attachTiqrCartTarget(intent, match[1], lines)
	}
	if qty := parsePositiveInt(follow); qty > 0 && (strings.Contains(lower, "to") || strings.Contains(lower, "change") || strings.HasPrefix(lower, "x") || regexp.MustCompile(`^\d+$`).MatchString(follow)) {
		base.Action = "set_qty"
		base.Qty = qty
		base.Incomplete = base.Index < 1 && base.NameQuery == ""
		return base
	}
	parsed := parseTiqrCartEdit(follow, lines)
	if parsed.Action != "" {
		return parsed
	}
	return base
}

func resolveTiqrCartEditTarget(c *Conv, attempt int, intent tiqrCartEditIntent) (optionID, name string, ok bool) {
	lines := tiqrCartLines(c)
	if len(lines) == 0 {
		return "", "", true
	}

	index := intent.Index
	if intent.Ambiguous || (index < 1 && intent.NameQuery != "") {
		prompt := "Which item number did you mean?\n\n" + formatTiqrCartSummary(c) + "\n\nReply with a number, for example 1."
		numText, got := c.AskNumber(fmt.Sprintf("cart_edit_which_%d", attempt), NumberPrompt{
			Body:    prompt,
			Pattern: `^[1-9][0-9]*$`,
			Step: StepNote{
				Doing:  "Disambiguating which cart line to edit.",
				Expect: "A cart item number.",
			},
		})
		if !got {
			return "", "", false
		}
		index = parsePositiveInt(numText)
	}
	if index < 1 {
		prompt := "Which item should I update?\n\n" + formatTiqrCartSummary(c) + "\n\nReply with an item number or name."
		target, got := c.AskText(fmt.Sprintf("cart_edit_target_%d", attempt), prompt, StepNote{
			Doing:  "Collecting which cart line to edit.",
			Expect: "An item number or item name.",
		})
		if !got {
			return "", "", false
		}
		resolved := attachTiqrCartTarget(tiqrCartEditIntent{}, target, lines)
		if resolved.Ambiguous || resolved.Index < 1 {
			numText, got := c.AskNumber(fmt.Sprintf("cart_edit_which2_%d", attempt), NumberPrompt{
				Body:    "Please reply with the item number from the list.\n\n" + formatTiqrCartSummary(c),
				Pattern: `^[1-9][0-9]*$`,
				Step: StepNote{
					Doing:  "Collecting a cart item number.",
					Expect: "A cart item number.",
				},
			})
			if !got {
				return "", "", false
			}
			index = parsePositiveInt(numText)
		} else {
			index = resolved.Index
		}
	}
	if index < 1 || index > len(lines) {
		return "", "", true
	}
	line := lines[index-1]
	return line.OptionID, line.Name, true
}

func optionAt(c *Conv, index int) (string, string, bool) {
	items, ok := anySlice(c.session().SessionData["options"])
	if !ok || index < 0 || index >= len(items) {
		return "", "", false
	}
	item, ok := asStringMap(items[index])
	if !ok {
		return "", "", false
	}
	id := fieldString(item, "id")
	name := fieldString(item, "name")
	return id, name, id != ""
}

func listLen(c *Conv, key string) int {
	items, ok := anySlice(c.session().SessionData[key])
	if !ok {
		return 0
	}
	return len(items)
}

// pickupOrderParams maps the WhatsApp Flow session values onto create_order.
// The email is the form's email field. Phone numbers are never used as the email.
// Pickup omits new_address; delivery keeps address lines and the delivery pin.
func pickupOrderParams(data map[string]any) map[string]string {
	email := contextEmail(data)
	phone := contextValue(data, "customer_phone", "phone", "phone_number")
	name := contextValue(data, "customer_name", "name")
	deliveryMode := strings.TrimSpace(asString(data["delivery_mode"]))
	if deliveryMode == "" {
		deliveryMode = tiqrModePickup
	}
	items := "[]"
	if cartItems := orderItemsForAPI(data["tiqr_cart"]); len(cartItems) > 0 {
		if raw, err := json.Marshal(cartItems); err == nil {
			items = string(raw)
		}
	}
	notes := codedOrderNotes(data)
	params := map[string]string{
		"email":         email,
		"items":         items,
		"notes":         notes,
		"delivery_mode": deliveryMode,
	}
	if phone != "" {
		params["phone_number"] = phone
	}
	if deliveryMode == tiqrModeDelivery {
		addressFields := map[string]string{
			"name":           name,
			"address_line_1": contextValue(data, "address_line_one", "address_line_1"),
			"address_line_2": contextValue(data, "address_line_two", "address_line_2"),
			"city":           contextValue(data, "city"),
			"state":          contextValue(data, "state"),
			"country":        contextValue(data, "country"),
			"pincode":        contextValue(data, "pincode"),
			"email":          email,
			"phone_number":   phone,
			"phone":          phone,
		}
		// Location-based stores read the pin from new_address, not buyer_meta_data,
		// whenever an address object is present.
		if lat, ok := anyToFloat64(data["delivery_latitude"]); ok {
			addressFields["latitude"] = formatOrderCoordinate(lat)
		}
		if lng, ok := anyToFloat64(data["delivery_longitude"]); ok {
			addressFields["longitude"] = formatOrderCoordinate(lng)
		}
		address, err := json.Marshal(addressFields)
		if err != nil {
			address = []byte("{}")
		}
		params["new_address"] = string(address)
	}
	if meta, ok := codedBuyerMetaJSON(data, notes); ok {
		params["buyer_meta_data"] = meta
	}
	return params
}

// formatOrderCoordinate keeps six decimal places, the address field limit.
func formatOrderCoordinate(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}

// codedBuyerMetaJSON builds buyer_meta_data for pickup and delivery. Contact
// fields and the same notes string as the order notes are included when set.
// Delivery also keeps the location pin.
func codedBuyerMetaJSON(data map[string]any, notes string) (string, bool) {
	meta := map[string]any{}
	if name := contextValue(data, "customer_name", "name"); name != "" {
		meta["name"] = name
	}
	if email := contextEmail(data); email != "" {
		meta["email"] = email
	}
	if phone := contextValue(data, "customer_phone", "phone", "phone_number"); phone != "" {
		meta["phone"] = phone
		meta["phone_number"] = phone
	}
	if notes = strings.TrimSpace(notes); notes != "" {
		meta["notes"] = notes
	}
	if lat, ok := anyToFloat64(data["delivery_latitude"]); ok {
		meta["latitude"] = lat
	}
	if lng, ok := anyToFloat64(data["delivery_longitude"]); ok {
		meta["longitude"] = lng
	}
	if len(meta) == 0 {
		return "", false
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func contextEmail(data map[string]any) string {
	for _, key := range []string{"customer_email", "email"} {
		if value := contextValue(data, key); simpleEmailRE.MatchString(value) {
			return value
		}
	}
	for key, value := range data {
		if isPhoneContextKey(key) {
			continue
		}
		text := strings.TrimSpace(asString(value))
		if simpleEmailRE.MatchString(text) {
			return text
		}
	}
	return ""
}

func isPhoneContextKey(key string) bool {
	switch key {
	case "customer_phone", "phone_number", "phone":
		return true
	default:
		return false
	}
}

func contextValue(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(asString(data[key])); value != "" {
			return value
		}
	}
	return ""
}

// codedOrderNotes builds the order notes string from cart capture answers.
// A plain customer note stays plain text when nothing was captured. With
// answers, each line is:
//
//	{product_name}({option_name})
//	- {label}: {value}
func codedOrderNotes(data map[string]any) string {
	customerNote := contextValue(data, "customer_notes", "notes")
	blocks := codedCaptureNoteBlocks(data["tiqr_cart"])
	if len(blocks) == 0 {
		captured := lineCaptureMap(data["commerce_captured_fields"])
		if len(captured) == 0 {
			return customerNote
		}
		labels := lineLabelMap(data["commerce_capture_labels"])
		block := formatCodedCaptureBlock("", "", captured, labels, nil)
		if block != "" {
			blocks = append(blocks, block)
		}
	}
	if len(blocks) == 0 {
		return customerNote
	}
	text := strings.Join(blocks, "\n\n")
	if customerNote != "" {
		text += "\n\nNote: " + customerNote
	}
	return text
}

func codedCaptureNoteBlocks(raw any) []string {
	cart, ok := anySlice(raw)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(cart))
	for _, entry := range cart {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		fields := lineCaptureMap(item["capture_fields"])
		if len(fields) == 0 {
			continue
		}
		block := formatCodedCaptureBlock(
			strings.TrimSpace(asString(item["product_name"])),
			strings.TrimSpace(asString(item["option_name"])),
			fields,
			lineLabelMap(item["capture_labels"]),
			captureOrderFrom(item["capture_order"]),
		)
		if block != "" {
			out = append(out, block)
		}
	}
	return out
}

func captureOrderFrom(raw any) []string {
	items, ok := anySlice(raw)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, entry := range items {
		key := strings.TrimSpace(asString(entry))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

func formatCodedCaptureBlock(productName, optionName string, fields map[string]any, labels map[string]string, order []string) string {
	if len(fields) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, key := range order {
		if _, ok := fields[key]; !ok || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) < len(fields) {
		rest := make([]string, 0, len(fields)-len(keys))
		for key := range fields {
			if seen[key] {
				continue
			}
			rest = append(rest, key)
		}
		sort.Strings(rest)
		keys = append(keys, rest...)
	}
	var b strings.Builder
	switch {
	case productName != "" && optionName != "":
		fmt.Fprintf(&b, "%s(%s)\n", productName, optionName)
	case productName != "":
		fmt.Fprintf(&b, "%s\n", productName)
	case optionName != "":
		fmt.Fprintf(&b, "%s\n", optionName)
	}
	wroteField := false
	for _, key := range keys {
		value := formatCaptureAnswer(fields[key])
		if value == "" {
			continue
		}
		label := strings.TrimSpace(labels[key])
		if label == "" {
			label = key
		}
		fmt.Fprintf(&b, "- %s: %s\n", label, value)
		wroteField = true
	}
	if !wroteField {
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

func checkout(c *Conv) error {
	detailsBody := "Please share your name, email, and phone number so we can place your pickup order."
	flowID := tiqrEcommercePickupFlowID
	if asString(c.session().SessionData["delivery_mode"]) == tiqrModeDelivery {
		detailsBody = "Please share your name, phone number, and address so we can place your delivery order."
		flowID = tiqrEcommerceFlowID
	}
	ok := c.AskFlow("details", FlowPrompt{
		FlowID: flowID,
		CTA:    "Enter details",
		Header: "Your details",
		Body:   detailsBody,
		Step: StepNote{
			Doing:  "The customer is asked to submit the details form.",
			Expect: "A typed message is not the form.",
		},
	})
	if !ok {
		return nil
	}
	retries := codedIntentSettings.OrderRetries
	if c.app != nil {
		retries = c.app.codedOrderRetries()
	}
	for attempt := 0; attempt <= retries; attempt++ {
		params := pickupOrderParams(c.session().SessionData)
		if params["email"] == "" && attempt == 0 {
			c.app.Log.Error("tiqr order is missing a valid email from the WhatsApp flow",
				"session", c.session().ID,
				"customer_email", contextValue(c.session().SessionData, "customer_email", "email"),
			)
		}
		name := "order"
		if attempt > 0 {
			name = fmt.Sprintf("order_retry_%d", attempt)
		}
		order, ok := c.Store(name, "create_order", params)
		if ok {
			c.SayPaymentCTA(order)
			return c.End()
		}
		if c.stop || c.ended {
			return nil
		}
		result := c.createRecoverPlan(name+"_recover", tiqrEcommerceFailed)
		if result.Kind == codedRecoverMissingField && len(result.Fields) > 0 && attempt < retries {
			if !collectRecoverFields(c, name, result.Fields) {
				return nil
			}
			continue
		}
		return c.Transfer(formatFailedOrderHandoff(c.session().SessionData))
	}
	return c.Transfer(formatFailedOrderHandoff(c.session().SessionData))
}

// orderItemsForAPI returns cart lines with only product_option and quantity for create_order.
// Duplicate options are merged; quantities below 1 are skipped.
func orderItemsForAPI(raw any) []map[string]any {
	cart, ok := anySlice(raw)
	if !ok || len(cart) == 0 {
		return nil
	}
	merged := map[string]int{}
	order := make([]string, 0, len(cart))
	for _, entry := range cart {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		optionID := strings.TrimSpace(asString(item["product_option"]))
		qty := parsePositiveInt(asString(item["quantity"]))
		if optionID == "" || qty < 1 {
			continue
		}
		if _, seen := merged[optionID]; !seen {
			order = append(order, optionID)
		}
		merged[optionID] += qty
	}
	out := make([]map[string]any, 0, len(order))
	for _, optionID := range order {
		out = append(out, map[string]any{
			"product_option": optionID,
			"quantity":       strconv.Itoa(merged[optionID]),
		})
	}
	return out
}

// formatFailedOrderHandoff builds the agent handoff text when create_order fails.
func formatFailedOrderHandoff(data map[string]any) string {
	var b strings.Builder
	b.WriteString("Placing order for the following items failed.\n")
	cart, _ := anySlice(data["tiqr_cart"])
	for _, entry := range cart {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		name := strings.TrimSpace(asString(item["option_name"]))
		optionID := strings.TrimSpace(asString(item["product_option"]))
		qty := strings.TrimSpace(asString(item["quantity"]))
		if qty == "" {
			qty = "1"
		}
		if name == "" {
			if optionID == "" {
				continue
			}
			name = "Option " + optionID
		}
		b.WriteString("\n")
		b.WriteString(name)
		b.WriteString(" x ")
		b.WriteString(qty)
		if extra := formatLineCapture(tiqrCartLine{
			Capture: lineCaptureMap(item["capture_fields"]),
			Labels:  lineLabelMap(item["capture_labels"]),
		}); extra != "" {
			b.WriteByte('\n')
			b.WriteString(strings.TrimRight(extra, "\n"))
		}
	}
	if addr := formatCheckoutAddress(data); addr != "" {
		b.WriteString("\n\nAddress\n")
		b.WriteString(addr)
	}
	b.WriteString("\n\n")
	b.WriteString(tiqrEcommerceHandoffConnect)
	return b.String()
}

// formatCheckoutAddress formats name and shipping lines from the checkout session.
func formatCheckoutAddress(data map[string]any) string {
	name := contextValue(data, "customer_name", "name")
	line1 := contextValue(data, "address_line_one", "address_line_1")
	line2 := contextValue(data, "address_line_two", "address_line_2")
	city := contextValue(data, "city")
	state := contextValue(data, "state")
	country := contextValue(data, "country")
	pincode := contextValue(data, "pincode")
	if name == "" && line1 == "" && line2 == "" && city == "" && state == "" && country == "" && pincode == "" {
		return ""
	}
	var lines []string
	if name != "" {
		lines = append(lines, name)
	}
	street := strings.TrimSpace(strings.Join(nonEmpty(line1, line2), ", "))
	if street != "" {
		lines = append(lines, street)
	}
	cityState := strings.TrimSpace(strings.Join(nonEmpty(city, state), ", "))
	if cityState != "" {
		lines = append(lines, cityState)
	}
	countryPin := strings.TrimSpace(strings.Join(nonEmpty(country, pincode), " "))
	if countryPin != "" {
		lines = append(lines, countryPin)
	}
	return strings.Join(lines, "\n")
}

func nonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// collectRecoverFields asks for every missing checkout field, one reply at a
// time, and writes each answer onto the session before the caller retries.
func collectRecoverFields(c *Conv, name string, asks []codedRecoverAsk) bool {
	for _, ask := range asks {
		label := codedRecoverFields[ask.Field]
		if label == "" {
			label = ask.Field
		}
		body := strings.TrimSpace(ask.Message)
		if body == "" {
			body = defaultRecoverAsk(ask.Field)
		}
		value, got := c.AskText(name+"_fix_"+ask.Field, body, StepNote{
			Doing:  "Collecting every missing checkout field after create_order failed. Ask each one before the order is retried.",
			Expect: "A plain reply with the missing " + label + ".",
		})
		if !got {
			return false
		}
		c.session().SessionData[ask.Field] = strings.TrimSpace(value)
	}
	return true
}

func orderStatus(c *Conv) error {
	order, ok := c.LookupOrder("order_status")
	if !ok {
		c.Say(tiqrEcommerceOrderMissing)
		return c.End()
	}
	c.Say(formatDirectOrderStatus(order))
	return c.End()
}
