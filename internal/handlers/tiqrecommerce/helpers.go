package tiqrecommerce

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

// WhatsApp interactive limits that can hide a trailing price on the name line.
const (
	listTitleMaxRunes     = 24
	carouselBodyMaxRunes  = 160
)

func asString(v any) string { return codedflow.AsString(v) }
func asStringMap(v any) (map[string]any, bool) {
	return codedflow.AsStringMap(v)
}
func anySlice(v any) ([]any, bool) { return codedflow.AnySlice(v) }
func fieldString(obj map[string]any, key string) string {
	return codedflow.FieldString(obj, key)
}
func anyToFloat64(v any) (float64, bool) { return codedflow.AnyToFloat64(v) }
func anyToInt(v any) int                 { return codedflow.AnyToInt(v) }
func firstNonEmpty(values ...string) string {
	return codedflow.FirstNonEmpty(values...)
}

// resolveEcommerceMetaFlowID returns the account-configured Meta flow ID for
// pickup or delivery customer-details forms, falling back to built-in defaults.
func resolveEcommerceMetaFlowID(c *Conv, delivery bool) string {
	fallback := TiqrEcommercePickupFlowID
	if delivery {
		fallback = EcommerceFlowID
	}
	if c == nil || c.App() == nil || c.Session() == nil {
		return fallback
	}
	return c.App().ResolveEcommerceMetaFlowID(c.Session().OrganizationID, c.Session().WhatsAppAccount, delivery)
}

func stashSessionCurrency(session *models.ChatbotSession, currency string) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	code := strings.ToUpper(strings.TrimSpace(currency))
	if code == "" {
		code = "INR"
	}
	session.SessionData["currency"] = code
	session.SessionData["currency_symbol"] = currencySymbol(code)
}

// currencySymbol returns the display symbol for a store ISO currency code.
func currencySymbol(currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	switch currency {
	case "INR", "":
		return "₹"
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	default:
		return currency + " "
	}
}

func sessionCurrencyCode(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return "INR"
	}
	code := strings.ToUpper(strings.TrimSpace(asString(session.SessionData["currency"])))
	if code == "" {
		return "INR"
	}
	return code
}

func sessionCurrencySymbol(session *models.ChatbotSession) string {
	if session != nil && session.SessionData != nil {
		if s := strings.TrimSpace(asString(session.SessionData["currency_symbol"])); s != "" {
			return s
		}
	}
	return currencySymbol(sessionCurrencyCode(session))
}

func pricedName(name, symbol, amount string) string {
	name = strings.TrimSpace(name)
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return name
	}
	return name + " (" + symbol + amount + ")"
}

func priceInDescription(symbol, amount, description string) string {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return strings.TrimSpace(description)
	}
	price := symbol + amount
	description = strings.TrimSpace(description)
	if description == "" {
		return price
	}
	return price + " — " + description
}

// enrichOptionsForList sets row_title / row_description so the currency price
// appears once: on the title when name+price fits in 24 runes, otherwise only
// in the description (where WhatsApp allows 72 runes).
func enrichOptionsForList(session *models.ChatbotSession, options []any) []any {
	symbol := sessionCurrencySymbol(session)
	out := make([]any, 0, len(options))
	for _, entry := range options {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		row := cloneJSONMap(item)
		name := asString(row["name"])
		amount := asString(row["price"])
		desc := asString(row["description"])
		priced := pricedName(name, symbol, amount)
		if amount == "" || utf8.RuneCountInString(priced) <= listTitleMaxRunes {
			row["row_title"] = priced
			row["row_description"] = desc
		} else {
			row["row_title"] = name
			row["row_description"] = priceInDescription(symbol, amount, desc)
		}
		out = append(out, map[string]any(row))
	}
	return out
}

// enrichProductsForCarousel sets card_body so the currency price appears once:
// on the name line when it fits in 160 runes, otherwise only on the second line.
func enrichProductsForCarousel(session *models.ChatbotSession, products []any) []any {
	symbol := sessionCurrencySymbol(session)
	out := make([]any, 0, len(products))
	for _, entry := range products {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		row := cloneJSONMap(item)
		row["card_body"] = productCardBody(
			asString(row["name"]),
			symbol,
			asString(row["min_price"]),
			asString(row["description"]),
		)
		out = append(out, map[string]any(row))
	}
	return out
}

func productCardBody(name, symbol, amount, description string) string {
	name = strings.TrimSpace(name)
	amount = strings.TrimSpace(amount)
	description = strings.TrimSpace(description)
	priced := pricedName(name, symbol, amount)
	if amount == "" {
		if description == "" {
			return name
		}
		return name + "\n" + description
	}
	if utf8.RuneCountInString(priced) <= carouselBodyMaxRunes {
		if description == "" {
			return priced
		}
		withDesc := priced + "\n" + description
		if utf8.RuneCountInString(withDesc) <= carouselBodyMaxRunes {
			return withDesc
		}
		return priced
	}
	// Name+price exceeds the body limit: put price only on the second line and
	// shorten the name so a 160-rune truncate cannot drop the price.
	priceLine := priceInDescription(symbol, amount, description)
	nameBudget := carouselBodyMaxRunes - utf8.RuneCountInString(priceLine) - 1
	if nameBudget < 1 {
		return priceLine
	}
	return codedflow.TruncateRunes(name, nameBudget) + "\n" + priceLine
}

func jsonMapFromSession(session *models.ChatbotSession, key string) models.JSONB {
	if session == nil || session.SessionData == nil {
		return models.JSONB{}
	}
	raw, ok := asStringMap(session.SessionData[key])
	if !ok || raw == nil {
		return models.JSONB{}
	}
	return models.JSONB(raw)
}

const (
	checkoutSessionKey       = "checkout"
	checkoutFlowEarlyHandoff = "early_handoff"
)

func parseProductAddonChoices(raw any) []map[string]any {
	if item, ok := asStringMap(raw); ok {
		if choice := normalizeAddonChoice(item); choice != nil {
			return []map[string]any{choice}
		}
		return nil
	}
	rows, ok := anySlice(raw)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item, ok := asStringMap(row)
		if !ok {
			continue
		}
		if choice := normalizeAddonChoice(item); choice != nil {
			out = append(out, choice)
		}
	}
	return out
}

func normalizeAddonChoice(item map[string]any) map[string]any {
	if item == nil {
		return nil
	}
	id := anyToInt(item["id"])
	if id <= 0 {
		return nil
	}
	if active, ok := item["is_active"].(bool); ok && !active {
		return nil
	}
	name := asString(item["name"])
	if name == "" {
		name = "Addon #" + strings.TrimSpace(asString(item["id"]))
		if name == "Addon #" {
			name = "Addon"
		}
	}
	return map[string]any{
		"id":    id,
		"name":  name,
		"price": item["price"],
	}
}

func appendCommerceAddon(session *models.ChatbotSession, addonID, qty int, name string) {
	if session == nil || addonID <= 0 {
		return
	}
	if qty < 1 {
		qty = 1
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	raw, _ := session.SessionData["commerce_addons"].([]any)
	next := make([]any, 0, len(raw)+1)
	found := false
	for _, value := range raw {
		addon, _ := value.(map[string]any)
		if anyToInt(addon["addon"]) == addonID {
			addon["quantity"] = anyToInt(addon["quantity"]) + qty
			if name != "" && strings.TrimSpace(asString(addon["name"])) == "" {
				addon["name"] = name
			}
			next = append(next, addon)
			found = true
			continue
		}
		next = append(next, value)
	}
	if !found {
		item := map[string]any{"addon": addonID, "quantity": qty}
		if strings.TrimSpace(name) != "" {
			item["name"] = strings.TrimSpace(name)
		}
		next = append(next, item)
	}
	session.SessionData["commerce_addons"] = next
}

func asToolFloat(v any) float64 {
	f, ok := anyToFloat64(v)
	if !ok {
		return 0
	}
	return f
}

func formatMoney(amount float64, currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "INR"
	}
	switch currency {
	case "INR":
		return fmt.Sprintf("₹%.2f", amount)
	case "USD":
		return fmt.Sprintf("$%.2f", amount)
	case "EUR":
		return fmt.Sprintf("€%.2f", amount)
	case "GBP":
		return fmt.Sprintf("£%.2f", amount)
	default:
		return fmt.Sprintf("%s %.2f", currency, amount)
	}
}

func cloneJSONMap(source map[string]any) models.JSONB {
	if source == nil {
		return models.JSONB{}
	}
	out := make(models.JSONB, len(source))
	for k, v := range source {
		out[k] = v
	}
	return out
}
