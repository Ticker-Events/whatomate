package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
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
)

func init() {
	registerCodedFlow(newTiqrEcommerceFlow())
}

func newTiqrEcommerceFlow() CodedFlow {
	return CodedFlow{
		Key:  tiqrEcommerceKey,
		Name: "TiQR Ecommerce",
		Description: "Buy products for store pickup, check an order, or talk to an agent. " +
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
			Doing:  "Opening menu: the customer chooses buy products, check order status, talk to staff, or check out with items already in the cart.",
			Expect: "One of the three button titles, free text that matches them, a loaded collection or product, or a request to check out.",
		},
	}
}

func buyProducts(c *Conv, collections []any, first Route) error {
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
					Doing:  "The customer is picking a store collection to browse, naming a product to search, or asking to check out.",
					Expect: "A collection row from the list (including a close typo of its name), a product name to search, or a checkout request.",
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
				Doing:  "The customer just added an item and chooses whether to keep shopping or check out.",
				Expect: "Add more items or Checkout, including a close paraphrase of either, or a free-text checkout request.",
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
		Body: "*{{products[0].name}}* (₹{{products[0].min_price}})\n\n{{products[0].description}}",
		HeaderImage: "{{products[0].images[0].original_url}}",
		FallbackMedia: tiqrEcommerceFallbackMedia,
		ItemsKey: "products",
		IDField:  "id",
		BodyField: "{{name}} (₹{{min_price}})",
		Title:     "Add to cart",
		StoreAs:   "product_selected",
		Select: map[string]string{
			"options":    "options",
			"product_id": "id",
		},
		Step: StepNote{
			Doing:  "The customer is adding the only product shown for this collection or search.",
			Expect: "Add to cart, or the product name (including a close typo).",
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
			Doing:  "The customer is choosing one product from the carousel for this collection or search.",
			Expect: "A product from the cards (match by product name even if they do not say Add to cart).",
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
				Doing:  "The selected product has more than one option; the customer must pick one.",
				Expect: "An option row from the list, including a close typo of the option name.",
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
			Doing:  "The customer is saying how many units of the chosen option to add to the cart.",
			Expect: "A whole number of 0 or more. Words such as two must be normalized to digits such as 2.",
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
	address, err := json.Marshal(map[string]string{
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
	})
	if err != nil {
		address = []byte("{}")
	}
	items := "[]"
	if raw, err := json.Marshal(data["tiqr_cart"]); err == nil && string(raw) != "null" {
		items = string(raw)
	}
	return map[string]string{
		"email":         email,
		"items":         items,
		"notes":         contextValue(data, "customer_notes", "notes"),
		"new_address":   string(address),
		"delivery_mode": "PICKUP_FROM_STORE",
	}
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
	ok := c.AskFlow("details", FlowPrompt{
		FlowID: tiqrEcommerceFlowID,
		CTA:    "Enter details",
		Header: "Your details",
		Body:   "Please share your name, phone number, and address so we can place your pickup order.",
		Step: StepNote{
			Doing:  "Checkout: the customer must submit the WhatsApp Flow with pickup details.",
			Expect: "A completed WhatsApp Flow submission. Free text is not a valid answer here.",
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
			c.Say(tiqrEcommerceConfirmed)
			c.Say(tiqrEcommerceThanks)
			return c.End()
		}
		if c.stop || c.ended {
			return nil
		}
		recoverCtx := c.lastTiqrRecoverContext("create_order", "order", "create")
		result := c.runRecover(recoverCtx, tiqrEcommerceFailed)
		if result.Kind == codedRecoverMissingField && attempt < retries {
			label := codedRecoverFields[result.Field]
			if label == "" {
				label = result.Field
			}
			askBody := result.Message
			if askBody == "" {
				askBody = "Could you share your " + label + " again?"
			}
			value, got := c.AskText(name+"_fix_"+result.Field, askBody, StepNote{
				Doing:  "Collecting a missing checkout field after create_order failed.",
				Expect: "A plain reply with the missing " + label + ".",
			})
			if !got {
				return nil
			}
			c.session().SessionData[result.Field] = value
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

func orderStatus(c *Conv) error {
	order, ok := c.LookupOrder("order_status")
	if !ok {
		c.Say(tiqrEcommerceOrderMissing)
		return c.End()
	}
	c.Say(formatDirectOrderStatus(order))
	return c.End()
}
