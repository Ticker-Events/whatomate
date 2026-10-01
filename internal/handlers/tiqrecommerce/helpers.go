package tiqrecommerce

import (
	"fmt"
	"strings"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
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

func stashSessionCurrency(session *models.ChatbotSession, currency string) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	code := strings.ToUpper(strings.TrimSpace(currency))
	if code == "" {
		return
	}
	session.SessionData["currency"] = code
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
	var rows []any
	switch v := raw.(type) {
	case []any:
		rows = v
	case []map[string]any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if choice := normalizeAddonChoice(item); choice != nil {
				out = append(out, choice)
			}
		}
		return out
	default:
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item, _ := row.(map[string]any)
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
