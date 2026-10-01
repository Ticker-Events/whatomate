package codedflow

import (
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)
var TranslateCodedLine = defaultTranslateCodedLine

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
	translated, err := TranslateCodedLine(c.app, c.session(), lang, src)
	call := CodedPreviewAICall{
		Role:     "translate",
		Prompt:   prompt,
		Language: lang,
	}
	if err != nil {
		call.Error = err.Error()
		c.notePreviewAI(call)
		c.app.LogCodedFlowAI(c.session(), "translate", prompt, "", err.Error(), "language", lang)
		translated = src
	} else if strings.TrimSpace(translated) == "" {
		call.Error = "empty translation"
		c.notePreviewAI(call)
		c.app.LogCodedFlowAI(c.session(), "translate", prompt, "", "empty translation", "language", lang)
		translated = src
	} else {
		call.Response = translated
		c.notePreviewAI(call)
		c.app.LogCodedFlowAI(c.session(), "translate", prompt, translated, "", "language", lang)
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

func defaultTranslateCodedLine(a Host, session *models.ChatbotSession, lang, text string) (string, error) {
	prompt := "Translate the message into " + lang + ", matching how the customer writes that language. Keep {{placeholders}}, asterisks, numbers, prices, and line breaks unchanged. Do not add products, prices, or ids. Return only the translation.\n\n" + text
	return a.CompleteCodedRoleText(session, codedFlowRoleTranslate, prompt)
}


func codedAISettings(a Host, session *models.ChatbotSession) (*models.ChatbotSettings, bool) {
	if a == nil || session == nil {
		return nil, false
	}
	settings, err := a.GetChatbotSettingsCached(session.OrganizationID, session.WhatsAppAccount)
	if err != nil || settings == nil || !settings.AI.Enabled || settings.AI.Provider == "" || strings.TrimSpace(settings.AI.APIKey) == "" {
		return nil, false
	}
	return settings, true
}

const (
	codedFlowRoleIntent    = "intent"
	codedFlowRoleTranslate = "translate"
	codedFlowRoleGuide     = "guide"
	codedFlowRoleRecover   = "recover"
)

func codedFlowRoleProvider(settings *models.ChatbotSettings, role string) models.IntentProvider {
	if settings == nil {
		return models.IntentProviderGeneric
	}
	var raw models.IntentProvider
	switch role {
	case codedFlowRoleIntent:
		raw = settings.AI.IntentProvider
	case codedFlowRoleTranslate:
		raw = settings.AI.TranslateProvider
	case codedFlowRoleGuide:
		raw = settings.AI.GuideProvider
	case codedFlowRoleRecover:
		raw = settings.AI.RecoverProvider
	default:
		raw = models.IntentProviderGeneric
	}
	p, ok := models.NormalizeIntentProvider(raw)
	if !ok {
		return models.IntentProviderGeneric
	}
	return p
}

// completeCodedRoleText runs a free-text coded-flow role (translate, guide, recover).
// Account AI uses the WhatsApp provider. AI Gateway uses chat completions.
// TypeSafe Jev cannot generate free text.
