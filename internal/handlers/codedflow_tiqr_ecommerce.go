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

	tiqrEcommerceAdded = "*{{option_name}}* has been added to your cart.\n\nYou now have *{{cart_count}}* in your cart. Add something else, or check out when you're ready."

	tiqrEcommerceConfirmed = "Your order is confirmed.\n\nIt's set for store pickup, and we'll have it ready for you.\n\nThank you for shopping with us."

	tiqrEcommerceConfirmedDelivery = "Your order is confirmed.\n\nWe'll deliver it to the address you shared.\n\nThank you for shopping with us."

	tiqrEcommerceFailed = "We couldn't place your order just now.\n\nPlease try again in a moment. If it still doesn't go through, message us and we'll help you complete it."

	tiqrEcommerceOrderMissing = "I couldn't find a recent order for this phone number."

	tiqrEcommerceSearchEmpty = "I couldn't find that in our store.\n\nPlease choose a collection below, or tell me another item."

	tiqrEcommerceCartEmpty = "Your cart is empty.\n\nPlease choose a collection below to add items, then you can check out."

	tiqrEcommerceFetchStore = "I couldn't load the store right now.\n\nPlease try again in a bit."

	tiqrEcommerceFetchCatalog = "I couldn't load our collections right now.\n\nPlease try again in a bit."

	tiqrEcommerceFetchProducts = "I couldn't load those products right now.\n\nPlease try again in a bit, or choose another collection."

	tiqrBuyProducts      = "buy_products"
	tiqrCheckOrderStatus = "check_order_status"
	tiqrTalkToAgent      = "talk_to_agent"
	tiqrAddMore          = "add_more"
	tiqrCheckout         = "checkout"

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
		c.Say(c.recoverFetchMessage("get_store", "store", tiqrEcommerceFetchStore))
		return c.End()
	}
	collections, ok := c.StoreList("collections", "list_collections", map[string]string{"limit": "20"})
	if !ok {
		c.Say(c.recoverFetchMessage("list_collections", "collections", tiqrEcommerceFetchCatalog))
		return c.End()
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
		next, ok := c.AskButtons("next", ButtonPrompt{
			Body: "Would you like to add anything else, or are you ready to place your order?",
			Buttons: []Button{
				{ID: tiqrAddMore, Title: "Add more items"},
				{ID: tiqrCheckout, Title: "Checkout"},
			},
			Step: StepNote{
				Doing:  "The customer just added an item.",
				Expect: "They may add more or check out.",
			},
		})
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
		if next.ID == tiqrCheckout {
			break
		}
	}
	return checkout(c)
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
// Non-empty cart: run checkout.
func (c *Conv) applyCheckoutDivert() (divertResult, error) {
	if c.divert != codedRouteCheckout {
		return divertNone, nil
	}
	c.divert = ""
	if cartLen(c) == 0 {
		c.Say(tiqrEcommerceCartEmpty)
		return divertEmptyCart, nil
	}
	return divertCheckoutDone, checkout(c)
}

func cartLen(c *Conv) int {
	items, ok := anySlice(c.session().SessionData["tiqr_cart"])
	if !ok {
		return 0
	}
	return len(items)
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
				c.Say(c.recoverFetchMessage("search_products", "products", tiqrEcommerceFetchProducts))
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
				c.Say(c.recoverFetchMessage("list_products", "products", tiqrEcommerceFetchProducts))
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
		Body:    "How many *{{option_name}}* would you like?\n\nReply with a whole number, for example 1.",
		Pattern: `^[0-9]+$`,
		Step: StepNote{
			Doing:  "The customer is saying how many units to add.",
			Expect: "A whole number, as digits or as a number word such as two.",
		},
	})
	if !ok {
		return false
	}
	optionID := asString(c.session().SessionData["option_id"])
	c.Once("cart", func() {
		appendCartItem(c, optionID, quantity)
	})
	c.Say(tiqrEcommerceAdded)
	return true
}

func appendCartItem(c *Conv, optionID, quantity string) {
	cart, _ := anySlice(c.session().SessionData["tiqr_cart"])
	cart = append(cart, map[string]any{
		"product_option": optionID,
		"quantity":       quantity,
	})
	c.session().SessionData["tiqr_cart"] = cart
	c.session().SessionData["cart_count"] = float64(len(cart))
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
	if raw, err := json.Marshal(data["tiqr_cart"]); err == nil && string(raw) != "null" {
		items = string(raw)
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
		msg := result.Message
		if msg == "" {
			msg = tiqrEcommerceFailed
		}
		c.Say(msg)
		c.Say(tiqrEcommerceThanks)
		return c.End()
	}
	c.Say(tiqrEcommerceFailed)
	c.Say(tiqrEcommerceThanks)
	return c.End()
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
