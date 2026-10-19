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
	prompt := translationPrompt(lang, src)
	translated, err := TranslateCodedLine(c.app, c.session(), lang, src)
	call := CodedPreviewAICall{
		Role:     "translate",
		Prompt:   prompt,
		Language: lang,
	}
	if err != nil {
		call.Error = err.Error()
		c.notePreviewAI(call)
		if c.app != nil {
			c.app.LogCodedFlowAI(c.session(), "translate", prompt, "", err.Error(), "language", lang)
		}
		translated = src
	} else if strings.TrimSpace(translated) == "" {
		call.Error = "empty translation"
		c.notePreviewAI(call)
		if c.app != nil {
			c.app.LogCodedFlowAI(c.session(), "translate", prompt, "", "empty translation", "language", lang)
		}
		translated = src
	} else {
		call.Response = translated
		c.notePreviewAI(call)
		if c.app != nil {
			c.app.LogCodedFlowAI(c.session(), "translate", prompt, translated, "", "language", lang)
		}
	}
	cache[src] = translated
	c.session().SessionData[codedTranslationsKey] = cache
	return translated
}

// limit truncates authored interactive chrome (button titles, list opener,
// section titles, headers, footers, CTAs). It does not translate: WhatsApp
// option labels have short hard caps, and many translations exceed them.
func (c *Conv) limit(src string, max int) string {
	return truncateRunes(src, max)
}

// EnglishLanguage reports whether lang is unset or English (en / en-*).
func EnglishLanguage(lang string) bool {
	return englishLanguage(lang)
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

func translationPrompt(lang, text string) string {
	target := replyLanguageLabel(lang)
	return "Translate the source message into " + target + ".\n" +
		"Write every word in " + target + "'s native script only " +
		"(for example Malayalam in Malayalam script, Hindi in Devanagari). " +
		"Never use Manglish, Latin romanization, or a mix of scripts.\n" +
		"Return a simple translation of the source text only. " +
		"If any other context appears in this prompt, use it only to keep the wording valid — " +
		"do not paste, summarize, or restate that context in the output.\n" +
		"Preserve the full meaning of the source. Do not invent content or add extra sentences.\n" +
		"Keep every line of the source, including numbered lists; never omit a line.\n" +
		"Translate every sentence of the source; never leave part of it untranslated.\n" +
		"Keep {{placeholders}}, asterisks, numbers, prices, currency symbols, and line breaks unchanged.\n" +
		"Copy product names, collection names, option names, addon names, prices, currency symbols, and ids " +
		"verbatim from the source — do not translate or drop them. " +
		"Translate only the surrounding instructional prose into the target native script.\n" +
		"Do not invent products, prices, or ids that are not already in the source. Return only the translation.\n\n" + text
}

func defaultTranslateCodedLine(a Host, session *models.ChatbotSession, lang, text string) (string, error) {
	return a.CompleteCodedRoleText(session, codedFlowRoleTranslate, translationPrompt(lang, text))
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
