package handlers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/shridarpatil/whatomate/pkg/ticker"
)

// TiQR Ecommerce loads the store and its collections, then lets the
// customer buy, check an order, or talk to an agent.

const (
	tiqrEcommerceKey = "tiqr_ecommerce"

	tiqrEcommerceFlowID = "1557965846018132"

	tiqrEcommerceFallbackMedia = "https://tickerevents.sgp1.cdn.digitaloceanspaces.com/tickerevents/media/images/products/124656/d592fabca6314ee5a749da835b40cf5a-589041f37b35417d8122b31a1b645eee-p.jpg"

	tiqrEcommerceThanks = "Thank you for shopping with us.\n\nMessage us anytime if you need help."

	tiqrEcommerceUnavailable = "Sorry, this item isn't available right now.\n\nPlease choose another item from our collections."

	tiqrEcommerceAdded = "*{{option_name}}* has been added to your cart.\n\n{{cart_summary}}\n\nYou now have *{{cart_count}}* items in your cart. Add something else, edit your cart, or check out when you're ready."

	tiqrEcommerceConfirmed = "Your order is confirmed.\n\nIt's set for store pickup, and we'll have it ready for you.\n\nThank you for shopping with us."

	tiqrEcommerceConfirmedDelivery = "Your order is confirmed.\n\nWe'll deliver it to the address you shared.\n\nThank you for shopping with us."

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
	tiqrChangeQty        = "change_qty"
	tiqrRemoveItem       = "remove_item"
	tiqrDoneEdit         = "done_edit"

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
			if !editCartMenu(c, attempt) {
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
		b.WriteString(fmt.Sprintf("We can deliver there. Delivery fee: %s.", formatPriceINR(ticker.PaiseToRupees(float64(result.ShippingFeePaise)))))
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
	cart, ok := anySlice(c.session().SessionData["tiqr_cart"])
	if !ok || len(cart) == 0 {
		return "Your cart is empty."
	}
	var b strings.Builder
	b.WriteString("*Your cart:*\n")
	var total float64
	hasPrice := false
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
		price, priceOK := anyToFloat64(item["price"])
		if priceOK && price > 0 {
			hasPrice = true
			lineTotal := price * float64(qty)
			total += lineTotal
			fmt.Fprintf(&b, "- *%s* x%d — %s each", name, qty, formatPriceINR(price))
			if qty > 1 {
				fmt.Fprintf(&b, " (%s)", formatPriceINR(lineTotal))
			}
			b.WriteByte('\n')
			continue
		}
		fmt.Fprintf(&b, "- *%s* x%d\n", name, qty)
	}
	if hasPrice {
		fmt.Fprintf(&b, "\n*Subtotal:* %s", formatPriceINR(total))
	}
	return strings.TrimSpace(b.String())
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
		Body:          "*{{products[0].name}}* (₹{{products[0].min_price}})\n\n{{products[0].description}}",
		HeaderImage:   "{{products[0].images[0].original_url}}",
		FallbackMedia: tiqrEcommerceFallbackMedia,
		ItemsKey:      "products",
		IDField:       "id",
		BodyField:     "{{name}} (₹{{min_price}})",
		Title:         "Add to cart",
		StoreAs:       "product_selected",
		Select: map[string]string{
			"options":    "options",
			"product_id": "id",
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
		BodyField:     "{{name}} (₹{{min_price}})",
		MediaField:    "{{images[0].original_url}}",
		Title:         "Add to cart",
		FallbackMedia: tiqrEcommerceFallbackMedia,
		Select: map[string]string{
			"options":    "options",
			"product_id": "id",
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
			Title:       "{{name}} (₹{{price}})",
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
	optionID := asString(c.session().SessionData["option_id"])
	c.Once("cart", func() {
		upsertCartItem(c, optionID, quantity)
	})
	refreshCartSessionFields(c)
	c.Say(tiqrEcommerceAdded)
	return true
}

func upsertCartItem(c *Conv, optionID, quantity string) {
	optionID = strings.TrimSpace(optionID)
	qty := parsePositiveInt(quantity)
	if optionID == "" || qty < 1 {
		return
	}
	cart, _ := anySlice(c.session().SessionData["tiqr_cart"])
	name := strings.TrimSpace(asString(c.session().SessionData["option_name"]))
	price := optionPriceFromSession(c, optionID)
	for i, entry := range cart {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if strings.TrimSpace(asString(item["product_option"])) != optionID {
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
		if price > 0 {
			item["price"] = price
		}
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
	if price > 0 {
		item["price"] = price
	}
	cart = append(cart, item)
	c.session().SessionData["tiqr_cart"] = cart
	refreshCartSessionFields(c)
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

func cartListItems(c *Conv) []any {
	cart, _ := anySlice(c.session().SessionData["tiqr_cart"])
	out := make([]any, 0, len(cart))
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
		qty := strings.TrimSpace(asString(item["quantity"]))
		if qty == "" {
			qty = "1"
		}
		out = append(out, map[string]any{
			"id":       optionID,
			"name":     name,
			"quantity": qty,
		})
	}
	return out
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
			if !editCartMenu(c, attempt) {
				return false, false
			}
		}
	}
}

func editCartMenu(c *Conv, reviewAttempt int) bool {
	for attempt := 1; ; attempt++ {
		if cartLen(c) == 0 {
			return true
		}
		refreshCartSessionFields(c)
		action, ok := c.AskButtons(fmt.Sprintf("cart_edit_%d_%d", reviewAttempt, attempt), ButtonPrompt{
			Body: formatTiqrCartSummary(c) + "\n\nWhat would you like to change?",
			Buttons: []Button{
				{ID: tiqrChangeQty, Title: "Change quantity"},
				{ID: tiqrRemoveItem, Title: "Remove item"},
				{ID: tiqrDoneEdit, Title: "Done"},
			},
			Step: StepNote{
				Doing:  "The customer is editing the cart.",
				Expect: "They change a quantity, remove an item, or finish editing.",
			},
		})
		if !ok {
			return false
		}
		switch action.ID {
		case tiqrDoneEdit:
			return true
		case tiqrChangeQty:
			if !changeCartLineQty(c, reviewAttempt, attempt) {
				return false
			}
		case tiqrRemoveItem:
			if !removeCartLineAsk(c, reviewAttempt, attempt) {
				return false
			}
			if cartLen(c) == 0 {
				return true
			}
		}
	}
}

func changeCartLineQty(c *Conv, reviewAttempt, editAttempt int) bool {
	items := cartListItems(c)
	if len(items) == 0 {
		return true
	}
	picked, ok := c.AskList(fmt.Sprintf("cart_qty_item_%d_%d", reviewAttempt, editAttempt), items, ListPrompt{
		Body:        "Which item should we change the quantity for?",
		Header:      "Change quantity",
		Button:      "Choose",
		Section:     "Cart",
		ItemsKey:    "cart_edit_items",
		IDField:     "id",
		Title:       "{{name}} × {{quantity}}",
		Description: "Tap to update quantity",
		Step: StepNote{
			Doing:  "The customer is picking a cart line to update.",
			Expect: "They pick one of the cart items.",
		},
	})
	if !ok {
		return false
	}
	qty, ok := c.AskNumber(fmt.Sprintf("cart_qty_value_%d_%d", reviewAttempt, editAttempt), NumberPrompt{
		Body:    "What quantity would you like for that item?\n\nReply with a whole number of 1 or more.",
		Pattern: `^[1-9][0-9]*$`,
		Step: StepNote{
			Doing:  "The customer is setting a new quantity for a cart line.",
			Expect: "A whole number of 1 or more.",
		},
	})
	if !ok {
		return false
	}
	c.Once(fmt.Sprintf("cart_qty_apply_%d_%d", reviewAttempt, editAttempt), func() {
		setTiqrCartLineQty(c, picked.ID, parsePositiveInt(qty))
	})
	c.Say("Updated quantity for *" + cartLineName(c, picked.ID) + "* to " + qty + ".\n\n" + formatTiqrCartSummary(c))
	return true
}

func removeCartLineAsk(c *Conv, reviewAttempt, editAttempt int) bool {
	items := cartListItems(c)
	if len(items) == 0 {
		return true
	}
	picked, ok := c.AskList(fmt.Sprintf("cart_remove_item_%d_%d", reviewAttempt, editAttempt), items, ListPrompt{
		Body:        "Which item should we remove from your cart?",
		Header:      "Remove item",
		Button:      "Choose",
		Section:     "Cart",
		ItemsKey:    "cart_edit_items",
		IDField:     "id",
		Title:       "{{name}} × {{quantity}}",
		Description: "Tap to remove",
		Step: StepNote{
			Doing:  "The customer is picking a cart line to remove.",
			Expect: "They pick one of the cart items.",
		},
	})
	if !ok {
		return false
	}
	var removedName string
	c.Once(fmt.Sprintf("cart_remove_apply_%d_%d", reviewAttempt, editAttempt), func() {
		_, removedName = removeTiqrCartLine(c, picked.ID)
	})
	if removedName == "" {
		removedName = strings.TrimSpace(picked.Title)
	}
	if removedName == "" {
		removedName = "that item"
	}
	if cartLen(c) == 0 {
		c.Say("Removed *" + removedName + "* from your cart.\n\nYour cart is empty.")
		return true
	}
	c.Say("Removed *" + removedName + "* from your cart.\n\n" + formatTiqrCartSummary(c))
	return true
}

func cartLineName(c *Conv, optionID string) string {
	cart, _ := anySlice(c.session().SessionData["tiqr_cart"])
	for _, entry := range cart {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		if strings.TrimSpace(asString(item["product_option"])) != strings.TrimSpace(optionID) {
			continue
		}
		if name := strings.TrimSpace(asString(item["option_name"])); name != "" {
			return name
		}
		return "Option " + optionID
	}
	return "Option " + optionID
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
func pickupOrderParams(data map[string]any) map[string]string {
	email := contextEmail(data)
	phone := contextValue(data, "customer_phone", "phone", "phone_number")
	deliveryMode := strings.TrimSpace(asString(data["delivery_mode"]))
	if deliveryMode == "" {
		deliveryMode = tiqrModePickup
	}
	addressFields := map[string]string{
		"name":           contextValue(data, "customer_name", "name"),
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
	if deliveryMode == tiqrModeDelivery {
		if lat, ok := anyToFloat64(data["delivery_latitude"]); ok {
			addressFields["latitude"] = formatOrderCoordinate(lat)
		}
		if lng, ok := anyToFloat64(data["delivery_longitude"]); ok {
			addressFields["longitude"] = formatOrderCoordinate(lng)
		}
	}
	address, err := json.Marshal(addressFields)
	if err != nil {
		address = []byte("{}")
	}
	items := "[]"
	if cartItems := orderItemsForAPI(data["tiqr_cart"]); len(cartItems) > 0 {
		if raw, err := json.Marshal(cartItems); err == nil {
			items = string(raw)
		}
	}
	params := map[string]string{
		"email":         email,
		"items":         items,
		"notes":         contextValue(data, "customer_notes", "notes"),
		"new_address":   string(address),
		"delivery_mode": deliveryMode,
	}
	if deliveryMode == tiqrModeDelivery {
		if meta, ok := codedBuyerMetaJSON(data); ok {
			params["buyer_meta_data"] = meta
		}
	}
	return params
}

// formatOrderCoordinate keeps six decimal places, the address field limit.
func formatOrderCoordinate(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}

func codedBuyerMetaJSON(data map[string]any) (string, bool) {
	meta := map[string]any{}
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

func checkout(c *Conv) error {
	detailsBody := "Please share your name, phone number, and address so we can place your pickup order."
	confirmMsg := tiqrEcommerceConfirmed
	if asString(c.session().SessionData["delivery_mode"]) == tiqrModeDelivery {
		detailsBody = "Please share your name, phone number, and address so we can place your delivery order."
		confirmMsg = tiqrEcommerceConfirmedDelivery
	}
	ok := c.AskFlow("details", FlowPrompt{
		FlowID: tiqrEcommerceFlowID,
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
		_, ok = c.Store(name, "create_order", params)
		if ok {
			c.Say(confirmMsg)
			c.Say(tiqrEcommerceThanks)
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
