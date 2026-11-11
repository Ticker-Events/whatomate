package tiqrecommerce

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	tiqrTryAgain = "try_again"

	tiqrCheckoutPreviewFail = "I couldn't load your order total just now.\n\nPlease try again, or edit your cart."

	tiqrCheckoutPreviewHandoff = "I couldn't load your order total just now.\n\n"
)

var checkoutPreviewFailButtons = []codedflow.Button{
	{ID: tiqrTryAgain, Title: "Try again"},
	{ID: tiqrEditCart, Title: "Edit cart"},
}

// reviewCheckoutPreview prices the session cart via checkout-preview after
// fulfillment is known, then asks the customer to confirm. ready=true means
// proceed to the details form; ready=false means add more / keep shopping.
func reviewCheckoutPreview(c *Conv) (ready bool, ok bool) {
	attempt := 0
	failures := 0
	for {
		attempt++
		if CartLen(c) == 0 {
			c.Say(tiqrEcommerceCartEmpty)
			return false, true
		}
		previewName := fmt.Sprintf("checkout_preview_%d", attempt)
		preview, previewOK := c.Store(previewName, "checkout_preview", CheckoutPreviewParams(c.Session().SessionData))
		if !previewOK {
			failures++
			if failures >= 2 {
				return false, transferCheckoutPreviewFailure(c)
			}
			choice, got := c.AskButtons(fmt.Sprintf("checkout_preview_fail_%d", attempt), codedflow.ButtonPrompt{
				Body:    tiqrCheckoutPreviewFail,
				Buttons: checkoutPreviewFailButtons,
				Step: codedflow.StepNote{
					Doing:  "Checkout preview failed. Offer try again or edit cart.",
					Expect: "They tap Try again or Edit cart.",
				},
			})
			if !got {
				return false, false
			}
			switch choice.ID {
			case tiqrTryAgain:
				continue
			case tiqrEditCart:
				if !editCartByInstruction(c, attempt) {
					return false, false
				}
				continue
			default:
				continue
			}
		}
		failures = 0
		body := formatCheckoutPreviewBody(c, preview)
		choice, got := askPreviewConfirmButtons(c, fmt.Sprintf("checkout_preview_confirm_%d", attempt), body)
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

func transferCheckoutPreviewFailure(c *Conv) bool {
	msg := tiqrCheckoutPreviewHandoff + FormatFailedOrderHandoff(c.Session().SessionData)
	// Replace the create_order failure opener with preview copy.
	msg = strings.Replace(msg, "Placing order for the following items failed.\n", "Here is what was in the cart:\n", 1)
	_ = c.Transfer(msg)
	return false
}

// CheckoutPreviewParams builds the REST params for checkout_preview from the session.
func CheckoutPreviewParams(data map[string]any) map[string]string {
	deliveryMode := strings.TrimSpace(asString(data["delivery_mode"]))
	if deliveryMode == "" {
		deliveryMode = tiqrModePickup
	}
	items := "[]"
	if cartItems := OrderItemsForAPI(data["tiqr_cart"]); len(cartItems) > 0 {
		if raw, err := json.Marshal(cartItems); err == nil {
			items = string(raw)
		}
	}
	params := map[string]string{
		"items":         items,
		"delivery_mode": deliveryMode,
	}
	if addons := orderAddonsForAPI(data["commerce_addons"]); len(addons) > 0 {
		if raw, err := json.Marshal(addons); err == nil {
			params["addons"] = string(raw)
		}
	}
	if deliveryMode == tiqrModeDelivery {
		if lat, ok := anyToFloat64(data["delivery_latitude"]); ok {
			params["latitude"] = formatOrderCoordinate(lat)
		}
		if lng, ok := anyToFloat64(data["delivery_longitude"]); ok {
			params["longitude"] = formatOrderCoordinate(lng)
		}
	}
	return params
}

func askPreviewConfirmButtons(c *Conv, name, body string) (codedflow.Choice, bool) {
	titles := cartButtonTitles(cartReviewButtons)
	budget := interactiveBodyBudget(c, titles)
	filled := body
	if c != nil && c.Conv != nil {
		filled = fillCartButtonPlaceholders(c.AuthoredText(body), cartButtonPlaceholderData(c))
	} else {
		filled = fillCartButtonPlaceholders(body, cartButtonPlaceholderData(c))
	}
	if utf8.RuneCountInString(filled) <= budget {
		return c.AskButtons(name, codedflow.ButtonPrompt{
			Body:    body,
			Buttons: cartReviewButtons,
			Step: codedflow.StepNote{
				Doing:  "The customer is confirming the priced order before the details form.",
				Expect: "They confirm items, edit the cart, or add more products.",
			},
		})
	}
	details := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), tiqrCartReviewSuffix))
	details = strings.TrimRight(details, "\n")
	filledDetails := fillCartButtonPlaceholders(authoredOrRaw(c, details), cartButtonPlaceholderData(c))
	if utf8.RuneCountInString(filledDetails) <= tiqrTextBodyMaxRunes {
		c.Say(details)
	} else {
		sayTextChunks(c, filledDetails)
	}
	return c.AskButtons(name, codedflow.ButtonPrompt{
		Body:    tiqrCartReviewSuffix,
		Buttons: cartReviewButtons,
		Step: codedflow.StepNote{
			Doing:  "The customer is confirming the priced order before the details form.",
			Expect: "They confirm items, edit the cart, or add more products.",
		},
	})
}

func formatCheckoutPreviewBody(c *Conv, preview map[string]any) string {
	currency := sessionCurrencyCode(c.Session())
	lines := formatCheckoutPreviewLines(c, preview, currency)
	if c != nil && c.Session() != nil {
		if c.Session().SessionData == nil {
			c.Session().SessionData = models.JSONB{}
		}
		c.Session().SessionData["cart_lines"] = lines
		c.Session().SessionData["cart_subtotal"] = formatMoney(asToolFloat(preview["original_amount"]), currency)
		c.Session().SessionData["checkout_preview_total"] = formatMoney(asToolFloat(preview["amount"]), currency)
		delete(c.Session().SessionData, "hidden_count")
	}

	var b strings.Builder
	b.WriteString("*Your order:*\n{{cart_lines}}\n\n*Subtotal:* {{cart_subtotal}}")
	for i, d := range collectPreviewDiscounts(preview) {
		key := fmt.Sprintf("checkout_discount_%d", i+1)
		labelKey := fmt.Sprintf("checkout_discount_label_%d", i+1)
		if c != nil && c.Session() != nil {
			c.Session().SessionData[key] = formatMoney(d.Amount, currency)
			c.Session().SessionData[labelKey] = d.Label
		}
		fmt.Fprintf(&b, "\n*Discount:* {{%s}} −{{%s}}", labelKey, key)
	}

	delivery := c != nil && c.Session() != nil &&
		asString(c.Session().SessionData["delivery_mode"]) == tiqrModeDelivery
	if delivery {
		origShip := asToolFloat(preview["original_shipping_fee"])
		if _, has := preview["original_shipping_fee"]; !has {
			origShip = asToolFloat(preview["shipping_fee"])
		}
		if c.Session().SessionData != nil {
			if origShip <= 0 {
				c.Session().SessionData["checkout_shipping"] = "Free"
			} else {
				c.Session().SessionData["checkout_shipping"] = formatMoney(origShip, currency)
			}
		}
		b.WriteString("\n*Shipping:* {{checkout_shipping}}")
		shipDisc := asToolFloat(preview["shipping_discount_total"])
		if shipDisc > 0 {
			c.Session().SessionData["checkout_shipping_discount"] = formatMoney(shipDisc, currency)
			b.WriteString("\n*Shipping discount:* −{{checkout_shipping_discount}}")
		}
	}
	b.WriteString("\n*Total:* {{checkout_preview_total}}")
	b.WriteString("\n\n")
	b.WriteString(tiqrCartReviewSuffix)
	return b.String()
}

type previewDiscountLine struct {
	ID     string
	Label  string
	Amount float64
}

func collectPreviewDiscounts(preview map[string]any) []previewDiscountLine {
	seen := map[string]bool{}
	var out []previewDiscountLine
	add := func(raw any) {
		item, ok := asStringMap(raw)
		if !ok {
			return
		}
		amount := asToolFloat(item["amount"])
		if amount <= 0 {
			return
		}
		id := strings.TrimSpace(asString(item["id"]))
		if id == "" {
			id = strings.TrimSpace(asString(item["code"]))
		}
		if id == "" {
			id = strings.TrimSpace(asString(item["title"]))
		}
		if id != "" && seen[id] {
			return
		}
		if id != "" {
			seen[id] = true
		}
		label := strings.TrimSpace(asString(item["display_name"]))
		if label == "" {
			label = strings.TrimSpace(asString(item["title"]))
		}
		if label == "" {
			label = strings.TrimSpace(asString(item["code"]))
		}
		if label == "" {
			label = "Discount"
		}
		out = append(out, previewDiscountLine{ID: id, Label: label, Amount: amount})
	}
	if list, ok := anySlice(preview["applied_discounts"]); ok {
		for _, entry := range list {
			add(entry)
		}
	}
	if items, ok := anySlice(preview["items"]); ok {
		for _, entry := range items {
			item, ok := asStringMap(entry)
			if !ok {
				continue
			}
			if list, ok := anySlice(item["applied_discounts"]); ok {
				for _, d := range list {
					add(d)
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	if asToolFloat(preview["discount_total"]) <= 0 && asToolFloat(preview["order_discount_total"]) <= 0 {
		return nil
	}
	return out
}

func formatCheckoutPreviewLines(c *Conv, preview map[string]any, currency string) string {
	items, _ := anySlice(preview["items"])
	addons, _ := anySlice(preview["addons"])
	capturesByOption := sessionCaptureByOption(c)
	var b strings.Builder
	n := 0
	for _, entry := range items {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		option := previewOptionMap(item["product_option"])
		optionID := strings.TrimSpace(asString(option["id"]))
		if optionID == "" {
			optionID = strings.TrimSpace(asString(item["product_option"]))
		}
		name := previewLineName(option)
		qty := anyToInt(item["quantity"])
		if qty < 1 {
			qty = 1
		}
		amount := asToolFloat(item["amount"])
		n++
		fmt.Fprintf(&b, "%d. *%s* x%d — %s\n", n, name, qty, formatMoney(amount, currency))
		for _, capture := range capturesByOption[optionID] {
			b.WriteString(capture)
		}
	}
	for _, entry := range addons {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		addon := previewOptionMap(item["addon"])
		name := strings.TrimSpace(asString(addon["name"]))
		if name == "" {
			name = "Add-on"
		}
		qty := anyToInt(item["quantity"])
		if qty < 1 {
			qty = 1
		}
		amount := asToolFloat(item["amount"])
		n++
		fmt.Fprintf(&b, "%d. *%s* x%d — %s\n", n, name, qty, formatMoney(amount, currency))
	}
	return strings.TrimRight(b.String(), "\n")
}

func previewOptionMap(raw any) map[string]any {
	if m, ok := asStringMap(raw); ok {
		return m
	}
	return map[string]any{}
}

func previewLineName(option map[string]any) string {
	optionName := strings.TrimSpace(asString(option["name"]))
	productName := ""
	if product, ok := asStringMap(option["product"]); ok {
		productName = strings.TrimSpace(asString(product["name"]))
	}
	switch {
	case productName != "" && optionName != "":
		return productName + "(" + optionName + ")"
	case optionName != "":
		return optionName
	case productName != "":
		return productName
	default:
		if id := strings.TrimSpace(asString(option["id"])); id != "" {
			return "Option " + id
		}
		return "Item"
	}
}

func sessionCaptureByOption(c *Conv) map[string][]string {
	out := map[string][]string{}
	if c == nil {
		return out
	}
	for _, line := range tiqrCartLines(c) {
		extra := formatLineCapture(line)
		if extra == "" {
			continue
		}
		out[line.OptionID] = append(out[line.OptionID], extra)
	}
	return out
}
