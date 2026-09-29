package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
)

type codedDiversion struct {
	Handled bool
	Reply   string
}

var (
	detectCodedLanguage  = defaultDetectCodedLanguage
	translateCodedLine   = defaultTranslateCodedLine
	answerCodedDiversion = defaultAnswerCodedDiversion
	lookupLatestOrder    = defaultLookupLatestOrder
)

func (c *Conv) ensureLanguage(text string) {
	if strings.TrimSpace(asString(c.session().SessionData[customerLanguageKey])) != "" {
		return
	}
	lang := "en"
	if strings.TrimSpace(text) != "" {
		detected, err := detectCodedLanguage(c.app, c.session(), text)
		if err == nil {
			if code := languageCode(detected); code != "" {
				lang = code
			}
		}
	}
	c.session().SessionData[customerLanguageKey] = lang
}

func (c *Conv) text(src string) string {
	if src == "" || englishLanguage(asString(c.session().SessionData[customerLanguageKey])) {
		return src
	}
	cache := c.translationCache()
	if hit, ok := cache[src]; ok {
		return hit
	}
	translated, err := translateCodedLine(c.app, c.session(), asString(c.session().SessionData[customerLanguageKey]), src)
	if err != nil || strings.TrimSpace(translated) == "" {
		translated = src
	}
	cache[src] = translated
	c.session().SessionData[codedTranslationsKey] = cache
	return translated
}

func (c *Conv) limit(src string, max int) string {
	return truncateRunes(c.text(src), max)
}

func (c *Conv) translationCache() map[string]string {
	out := map[string]string{}
	switch raw := c.session().SessionData[codedTranslationsKey].(type) {
	case map[string]string:
		for key, value := range raw {
			out[key] = value
		}
	case map[string]any:
		for key, value := range raw {
			out[key] = asString(value)
		}
	}
	return out
}

func englishLanguage(lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	return lang == "" || lang == "en" || strings.HasPrefix(lang, "en-")
}

func languageCode(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	field := strings.Fields(raw)[0]
	field = strings.Trim(field, ".,;:\"'`")
	if len(field) < 2 || len(field) > 8 {
		return ""
	}
	for _, r := range field {
		if (r < 'a' || r > 'z') && r != '-' {
			return ""
		}
	}
	return field
}

func defaultDetectCodedLanguage(a *App, session *models.ChatbotSession, text string) (string, error) {
	settings, ok := codedAISettings(a, session)
	if !ok {
		return "en", nil
	}
	answer, err := a.completeCodedText(settings, session, "Reply with only the ISO 639-1 language code of this customer message. If it is English, reply en.\n\n"+text, "")
	if err != nil {
		return "", err
	}
	return languageCode(answer), nil
}

func defaultTranslateCodedLine(a *App, session *models.ChatbotSession, lang, text string) (string, error) {
	settings, ok := codedAISettings(a, session)
	if !ok {
		return "", fmt.Errorf("ai is not configured")
	}
	prompt := "Translate the message into " + lang + ". Keep {{placeholders}}, asterisks, and line breaks. Return only the translation.\n\n" + text
	return a.completeCodedText(settings, session, prompt, "")
}

func defaultAnswerCodedDiversion(a *App, account *models.WhatsAppAccount, session *models.ChatbotSession, question, expected, userText string) (codedDiversion, error) {
	if account == nil {
		return codedDiversion{}, fmt.Errorf("ai is not configured")
	}
	settings, ok := codedAISettings(a, session)
	if !ok {
		return codedDiversion{}, fmt.Errorf("ai is not configured")
	}
	prompt := fmt.Sprintf(
		"The customer left the scripted shopping path. They were expected to: %s. Current step: %s.\nStore and collections already loaded:\n%s\nCustomer message: %s\nReply with JSON only. If you can answer from that context, use {\"handled\":true,\"reply\":\"...\"}. If a person needs to take over, use {\"handled\":false,\"reply\":\"\"}.",
		expected, question, codedStoreContext(session), userText,
	)
	raw, err := a.generateAIResponse(settings, session, prompt)
	if err != nil {
		return codedDiversion{}, err
	}
	return parseCodedDiversion(raw)
}

func defaultLookupLatestOrder(a *App, account *models.WhatsAppAccount, session *models.ChatbotSession) (map[string]any, error) {
	if account == nil || session == nil {
		return nil, fmt.Errorf("order status is unavailable")
	}
	settings, err := a.getChatbotSettingsCached(account.OrganizationID, account.Name)
	if err != nil || settings == nil {
		return nil, fmt.Errorf("order status is unavailable")
	}
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		return nil, fmt.Errorf("order status is unavailable")
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := rt.Client.LookupOrderStatus(ctx, rt.StoreID, rt.PhoneNumber, "")
	if err != nil {
		return nil, err
	}
	return compactOrderStatus(raw), nil
}

func codedAISettings(a *App, session *models.ChatbotSession) (*models.ChatbotSettings, bool) {
	if a == nil || session == nil {
		return nil, false
	}
	settings, err := a.getChatbotSettingsCached(session.OrganizationID, session.WhatsAppAccount)
	if err != nil || settings == nil || !settings.AI.Enabled || settings.AI.Provider == "" || strings.TrimSpace(settings.AI.APIKey) == "" {
		return nil, false
	}
	return settings, true
}

func (a *App) completeCodedText(settings *models.ChatbotSettings, session *models.ChatbotSession, userMessage, contextData string) (string, error) {
	switch settings.AI.Provider {
	case models.AIProviderOpenAI:
		return a.generateOpenAIResponse(settings, session, userMessage, contextData)
	case models.AIProviderAnthropic:
		return a.generateAnthropicResponse(settings, session, userMessage, contextData)
	case models.AIProviderGoogle:
		return a.generateGoogleResponse(settings, session, userMessage, contextData)
	default:
		return "", fmt.Errorf("unsupported AI provider: %s", settings.AI.Provider)
	}
}

func parseCodedDiversion(raw string) (codedDiversion, error) {
	raw = strings.TrimSpace(raw)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var body struct {
		Handled bool   `json:"handled"`
		Reply   string `json:"reply"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return codedDiversion{}, err
	}
	return codedDiversion{Handled: body.Handled, Reply: strings.TrimSpace(body.Reply)}, nil
}

func codedStoreContext(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return ""
	}
	var b strings.Builder
	if store, ok := asStringMap(session.SessionData["store"]); ok {
		if name := asString(store["name"]); name != "" {
			fmt.Fprintf(&b, "Store: %s\n", name)
		}
	}
	items, _ := anySlice(session.SessionData["collections"])
	if len(items) == 0 {
		return b.String()
	}
	b.WriteString("Collections:")
	for _, item := range items {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "\n- %s (id %s)", asString(row["name"]), asString(row["id"]))
	}
	return b.String()
}
