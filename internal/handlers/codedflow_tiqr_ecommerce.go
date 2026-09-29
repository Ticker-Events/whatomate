package handlers

import "strings"

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

	tiqrEcommerceNewAddress = `{
"name": "{{customer_name}}",
"address_line_1": "{{address_line_one}}",
"address_line_2": "{{address_line_two}}",
"city": "{{city}}",
"state": "{{state}}",
"country": "{{country}}",
"pincode": "{{pincode}}",
"email": "{{customer_email}}",
"phone_number":"{{customer_phone}}",
"phone":"{{customer_phone}}"
}`

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
		c.Say(tiqrEcommerceThanks)
		return c.End()
	}
	collections, ok := c.StoreList("collections", "list_collections", map[string]string{"limit": "20"})
	if !ok {
		c.Say(tiqrEcommerceThanks)
		return c.End()
	}
	choice, ok := c.AskButtons("intent", menuButtons(store))
	if !ok {
		return nil
	}
	switch choice.ID {
	case tiqrBuyProducts:
		return buyProducts(c, collections)
	case tiqrCheckOrderStatus:
		return orderStatus(c)
	default:
		return c.Transfer(codedAgentHandoff)
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
			{ID: tiqrTalkToAgent, Title: "Talk to agent"},
		},
	}
}

func buyProducts(c *Conv, collections []any) error {
	for {
		pick, ok := c.AskList("collection", collections, ListPrompt{
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
		})
		if !ok {
			return nil
		}
		products, ok := c.StoreList("products", "list_products", map[string]string{"category_id": pick.ID})
		if !ok {
			c.Say(tiqrEcommerceThanks)
			return c.End()
		}
		_, ok = c.AskCarousel("product", products, productCards())
		if !ok {
			return nil
		}
		if !addPickedProduct(c) {
			return nil
		}
		next, ok := c.AskButtons("next", ButtonPrompt{
			Body: "Would you like to add anything else, or are you ready to place your order?",
			Buttons: []Button{
				{ID: tiqrAddMore, Title: "Add more items"},
				{ID: tiqrCheckout, Title: "Checkout"},
			},
		})
		if !ok {
			return nil
		}
		if next.ID == tiqrCheckout {
			break
		}
	}
	return checkout(c)
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
	body := "How many *{{option_name}}* would you like?\n\nReply with a whole number, for example 1."
	quantity, ok := c.AskNumber("quantity", body, `^[0-9]+$`)
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

func checkout(c *Conv) error {
	ok := c.AskFlow("details", FlowPrompt{
		FlowID: tiqrEcommerceFlowID,
		CTA:    "Enter details",
		Header: "Your details",
		Body:   "Please share your name, phone number, and address so we can place your pickup order.",
	})
	if !ok {
		return nil
	}
	_, ok = c.Store("order", "create_order", map[string]string{
		"email":         "{{customer_email}}",
		"items":         "{{tiqr_cart}}",
		"notes":         "{{customer_notes}}",
		"new_address":   tiqrEcommerceNewAddress,
		"delivery_mode": "PICKUP_FROM_STORE",
	})
	if ok {
		c.Say(tiqrEcommerceConfirmed)
	} else {
		c.Say(tiqrEcommerceFailed)
	}
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
