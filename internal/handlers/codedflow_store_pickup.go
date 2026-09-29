package handlers

// Store pickup is the shopping conversation that used to live as a visual
// graph. Each function is one step. Branches are ordinary if statements.

const (
	storePickupKey = "store_pickup"

	storePickupFlowID = "1557965846018132"

	storePickupFallbackMedia = "https://tickerevents.sgp1.cdn.digitaloceanspaces.com/tickerevents/media/images/products/124656/d592fabca6314ee5a749da835b40cf5a-589041f37b35417d8122b31a1b645eee-p.jpg"

	storePickupThanks = "Thank you for shopping with us.\n\nMessage us anytime if you need help."

	storePickupUnavailable = "Sorry, this item isn't available right now.\n\nPlease choose another item from our collections."

	storePickupAdded = "*{{option_name}}* has been added to your cart.\n\nYou now have *{{cart_count}}* in your cart. Add something else, or check out when you're ready."

	storePickupConfirmed = "Your order is confirmed.\n\nIt's set for store pickup, and we'll have it ready for you.\n\nThank you for shopping with us."

	storePickupFailed = "We couldn't place your order just now.\n\nPlease try again in a moment. If it still doesn't go through, message us and we'll help you complete it."

	storePickupCartItem = `{
  "product_option": "{{option_id}}",
  "quantity": "{{quantity}}"
}`

	storePickupNewAddress = `{
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

	storePickupAddMore  = "add_more"
	storePickupCheckout = "checkout"
)

func init() {
	registerCodedFlow(newStorePickupFlow())
}

func newStorePickupFlow() CodedFlow {
	return CodedFlow{
		Key:  storePickupKey,
		Name: "Store pickup order",
		Description: "Browse collections, add items, and place a store pickup order. " +
			"Customer details are collected with a WhatsApp Flow.",
		Steps: []CodedStep{
			{Name: "list_collections", Label: "List collections"},
			{Name: "show_collections", Label: "Show collections"},
			{Name: "list_products", Label: "List products"},
			{Name: "show_products", Label: "Show products"},
			{Name: "product_unavailable", Label: "Product not available"},
			{Name: "choose_option", Label: "Choose an option"},
			{Name: "assign_option", Label: "Use the only option"},
			{Name: "ask_quantity", Label: "Ask quantity"},
			{Name: "add_to_cart", Label: "Add to cart"},
			{Name: "cart_added", Label: "Confirm cart"},
			{Name: "more_or_checkout", Label: "Add more or checkout"},
			{Name: "ask_details", Label: "Collect customer details"},
			{Name: "create_order", Label: "Place pickup order"},
			{Name: "order_confirmed", Label: "Order confirmed"},
			{Name: "order_failed", Label: "Order failed"},
			{Name: "end", Label: "End"},
		},
		start: "list_collections",
		fns: map[string]codedStepFn{
			"list_collections":    stepStoreListCollections,
			"show_collections":    stepStoreShowCollections,
			"list_products":       stepStoreListProducts,
			"show_products":       stepStoreShowProducts,
			"after_product":       stepStoreAfterProduct,
			"product_unavailable": stepStoreUnavailable,
			"choose_option":       stepStoreChooseOption,
			"assign_option":       stepStoreAssignOption,
			"ask_quantity":        stepStoreAskQuantity,
			"add_to_cart":         stepStoreAddToCart,
			"cart_added":          stepStoreCartAdded,
			"more_or_checkout":    stepStoreMoreOrCheckout,
			"ask_details":         stepStoreAskDetails,
			"create_order":        stepStoreCreateOrder,
			"order_confirmed":     stepStoreOrderConfirmed,
			"order_failed":        stepStoreOrderFailed,
			"end":                 stepStoreEnd,
		},
	}
}

func stepStoreListCollections(c *codedCtx) (codedJump, error) {
	ok, err := c.tiqr("list_collections", map[string]any{
		"api_type":  "rest",
		"operation": "list_collections",
		"params":    map[string]any{"limit": "20"},
		"response_mapping": map[string]any{
			"count":       "count",
			"collections": "results",
		},
	})
	if err != nil {
		return codedJump{}, err
	}
	if !ok {
		return goTo("end"), nil
	}
	return goTo("show_collections"), nil
}

func stepStoreShowCollections(c *codedCtx) (codedJump, error) {
	jump, err := c.buttons("show_collections", map[string]any{
		"body":              "Choose a collection below and we'll show you the items in it.",
		"mode":              "list",
		"footer":            "Tap Browse to continue",
		"header":            "Our collections",
		"source":            "dynamic",
		"id_field":          "id",
		"items_var":         "{{collections}}",
		"list_button":       "Browse",
		"title_field":       "{{name}}",
		"section_title":     "Collections",
		"description_field": "{{description}}",
		"selection_mapping": map[string]any{
			"collection_id":   "id",
			"collection_name": "title",
		},
	})
	if err != nil || jump.yield {
		return jump, err
	}
	return goTo("list_products"), nil
}

func stepStoreListProducts(c *codedCtx) (codedJump, error) {
	ok, err := c.tiqr("list_products", map[string]any{
		"api_type":  "rest",
		"operation": "list_products",
		"params":    map[string]any{"category_id": "{{collection_id}}"},
		"response_mapping": map[string]any{
			"count":    "count",
			"products": "results",
		},
	})
	if err != nil {
		return codedJump{}, err
	}
	if !ok {
		return goTo("end"), nil
	}
	return goTo("show_products"), nil
}

func stepStoreShowProducts(c *codedCtx) (codedJump, error) {
	jump, err := c.buttons("show_products", map[string]any{
		"body":        "Here is what's available in *{{collection_name}}*.\n\nTap *Add to cart* on the item you'd like.",
		"mode":        "carousel",
		"source":      "dynamic",
		"id_field":    "{{id}}",
		"store_as":    "product_selected",
		"items_var":   "{{products}}",
		"body_field":  "{{name}} (₹{{min_price}})",
		"media_field": "{{images[0].original_url}}",
		"title_field": "Add to cart",
		"selection_mapping": map[string]any{
			"options":    "options",
			"product_id": "id",
		},
		"fallback_media_url": storePickupFallbackMedia,
	})
	if err != nil || jump.yield {
		return jump, err
	}
	return goTo("after_product"), nil
}

func stepStoreAfterProduct(c *codedCtx) (codedJump, error) {
	switch n := c.listLen("options"); {
	case n == 0:
		return goTo("product_unavailable"), nil
	case n == 1:
		return goTo("assign_option"), nil
	default:
		return goTo("choose_option"), nil
	}
}

func stepStoreUnavailable(c *codedCtx) (codedJump, error) {
	if err := c.say("product_unavailable", storePickupUnavailable); err != nil {
		return codedJump{}, err
	}
	return goTo("more_or_checkout"), nil
}

func stepStoreChooseOption(c *codedCtx) (codedJump, error) {
	jump, err := c.buttons("choose_option", map[string]any{
		"body":              "This item comes in more than one option. Please choose the one you'd like.",
		"mode":              "list",
		"header":            "Choose an option",
		"source":            "dynamic",
		"id_field":          "id",
		"items_var":         "options",
		"list_button":       "Choose",
		"title_field":       "{{name}} (₹{{price}})",
		"section_title":     "Options",
		"description_field": "{{description}}",
		"selection_mapping": map[string]any{
			"option_id":   "id",
			"option_name": "name",
		},
	})
	if err != nil || jump.yield {
		return jump, err
	}
	return goTo("ask_quantity"), nil
}

func stepStoreAssignOption(c *codedCtx) (codedJump, error) {
	if err := c.set("assign_option", []any{
		map[string]any{"name": "option_id", "value": "options[0].id"},
		map[string]any{"name": "option_name", "value": "options[0].name"},
	}); err != nil {
		return codedJump{}, err
	}
	return goTo("ask_quantity"), nil
}

func stepStoreAskQuantity(c *codedCtx) (codedJump, error) {
	jump, err := c.prompt("ask_quantity", map[string]any{
		"body":             "How many *{{option_name}}* would you like?\n\nReply with a whole number, for example 1.",
		"store_as":         "quantity",
		"input_type":       "number",
		"validation_error": "Please send a whole number, such as 1 or 2.",
		"validation_regex": "^[0-9]+$",
	})
	if err != nil || jump.yield {
		return jump, err
	}
	if jump.outcome == "max_retries" {
		return finishCoded(), nil
	}
	return goTo("add_to_cart"), nil
}

func stepStoreAddToCart(c *codedCtx) (codedJump, error) {
	if err := c.set("add_to_cart", []any{
		map[string]any{
			"op":         "append",
			"name":       "tiqr_cart",
			"value":      storePickupCartItem,
			"value_type": "json",
		},
		map[string]any{
			"op":         "set",
			"name":       "cart_count",
			"value":      "len(tiqr_cart)",
			"value_type": "expression",
		},
	}); err != nil {
		return codedJump{}, err
	}
	return goTo("cart_added"), nil
}

func stepStoreCartAdded(c *codedCtx) (codedJump, error) {
	if err := c.say("cart_added", storePickupAdded); err != nil {
		return codedJump{}, err
	}
	return goTo("more_or_checkout"), nil
}

func stepStoreMoreOrCheckout(c *codedCtx) (codedJump, error) {
	jump, err := c.buttons("more_or_checkout", map[string]any{
		"body":   "Would you like to add anything else, or are you ready to place your order?",
		"mode":   "reply",
		"source": "static",
		"buttons": []any{
			map[string]any{"id": storePickupAddMore, "type": "reply", "title": "Add more items"},
			map[string]any{"id": storePickupCheckout, "type": "reply", "title": "Checkout"},
		},
	})
	if err != nil || jump.yield {
		return jump, err
	}
	switch jump.outcome {
	case "button:" + storePickupAddMore:
		return goTo("show_collections"), nil
	case "button:" + storePickupCheckout:
		return goTo("ask_details"), nil
	default:
		return finishCoded(), nil
	}
}

func stepStoreAskDetails(c *codedCtx) (codedJump, error) {
	jump, err := c.whatsappFlow("ask_details", map[string]any{
		"cta":     "Enter details",
		"body":    "Please share your name, phone number, and address so we can place your pickup order.",
		"header":  "Your details",
		"flow_id": storePickupFlowID,
	})
	if err != nil || jump.yield {
		return jump, err
	}
	return goTo("create_order"), nil
}

func stepStoreCreateOrder(c *codedCtx) (codedJump, error) {
	ok, err := c.tiqr("create_order", map[string]any{
		"api_type":  "rest",
		"operation": "create_order",
		"params": map[string]any{
			"email":         "{{customer_email}}",
			"items":         "{{tiqr_cart}}",
			"notes":         "{{customer_notes}}",
			"new_address":   storePickupNewAddress,
			"delivery_mode": "PICKUP_FROM_STORE",
		},
	})
	if err != nil {
		return codedJump{}, err
	}
	if ok {
		return goTo("order_confirmed"), nil
	}
	return goTo("order_failed"), nil
}

func stepStoreOrderConfirmed(c *codedCtx) (codedJump, error) {
	if err := c.say("order_confirmed", storePickupConfirmed); err != nil {
		return codedJump{}, err
	}
	return goTo("end"), nil
}

func stepStoreOrderFailed(c *codedCtx) (codedJump, error) {
	if err := c.say("order_failed", storePickupFailed); err != nil {
		return codedJump{}, err
	}
	return goTo("end"), nil
}

func stepStoreEnd(c *codedCtx) (codedJump, error) {
	if err := c.say("end", storePickupThanks); err != nil {
		return codedJump{}, err
	}
	return finishCoded(), nil
}
