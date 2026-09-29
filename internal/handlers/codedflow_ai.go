package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
)

var (
	translateCodedLine = defaultTranslateCodedLine
	lookupLatestOrder  = defaultLookupLatestOrder
)

func (c *Conv) text(src string) string {
	if src == "" || englishLanguage(asString(c.session().SessionData[customerLanguageKey])) {
		return src
	}
	cache := c.translationCache()
	if hit, ok := cache[src]; ok {
		return hit
	}
	lang := asString(c.session().SessionData[customerLanguageKey])
	prompt := "Translate the message into " + lang + ", matching how the customer writes that language. Keep {{placeholders}}, asterisks, numbers, prices, and line breaks unchanged. Do not add products, prices, or ids. Return only the translation.\n\n" + src
	translated, err := translateCodedLine(c.app, c.session(), lang, src)
	call := CodedPreviewAICall{
		Role:     "translate",
		Prompt:   prompt,
		Language: lang,
	}
	if err != nil {
		call.Error = err.Error()
		c.notePreviewAI(call)
		c.app.logCodedFlowAI(c.session(), "translate", prompt, "", err.Error(), "language", lang)
		translated = src
	} else if strings.TrimSpace(translated) == "" {
		call.Error = "empty translation"
		c.notePreviewAI(call)
		c.app.logCodedFlowAI(c.session(), "translate", prompt, "", "empty translation", "language", lang)
		translated = src
	} else {
		call.Response = translated
		c.notePreviewAI(call)
		c.app.logCodedFlowAI(c.session(), "translate", prompt, translated, "", "language", lang)
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

func defaultTranslateCodedLine(a *App, session *models.ChatbotSession, lang, text string) (string, error) {
	settings, ok := codedRoleSettings(a, session, codedIntentSettings.Translate)
	if !ok {
		return "", fmt.Errorf("ai is not configured")
	}
	prompt := "Translate the message into " + lang + ", matching how the customer writes that language. Keep {{placeholders}}, asterisks, numbers, prices, and line breaks unchanged. Do not add products, prices, or ids. Return only the translation.\n\n" + text
	return a.completeCodedText(settings, session, prompt, "")
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
