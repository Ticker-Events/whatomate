package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	prompt := "Translate the message into " + lang + ", matching how the customer writes that language. Keep {{placeholders}}, asterisks, numbers, prices, and line breaks unchanged. Do not add products, prices, or ids. Return only the translation.\n\n" + text
	return a.completeCodedRoleText(session, codedFlowRoleTranslate, prompt)
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
func (a *App) completeCodedRoleText(session *models.ChatbotSession, role, prompt string) (string, error) {
	if a == nil || session == nil {
		return "", fmt.Errorf("ai is not configured")
	}
	settings, err := a.getChatbotSettingsCached(session.OrganizationID, session.WhatsAppAccount)
	if err != nil || settings == nil {
		return "", fmt.Errorf("ai is not configured")
	}
	switch codedFlowRoleProvider(settings, role) {
	case models.IntentProviderGateway:
		return a.completeAIGatewayChat(settings, prompt)
	case models.IntentProviderJev:
		return "", fmt.Errorf("TypeSafe Jev cannot generate text for %s; use Account AI or AI Gateway with a language model", role)
	default:
		roleSettings, ok := codedRoleSettings(a, session, codedAIRoleConfig{})
		if !ok {
			return "", fmt.Errorf("ai is not configured")
		}
		return a.completeCodedText(roleSettings, session, prompt, "")
	}
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

const aiGatewayChatURL = "https://ai-gateway.vercel.sh/v1/chat/completions"

func (a *App) completeAIGatewayChat(settings *models.ChatbotSettings, prompt string) (string, error) {
	if settings == nil {
		return "", fmt.Errorf("ai gateway is not configured")
	}
	key := strings.TrimSpace(settings.AI.GatewayAPIKey)
	if key == "" {
		return "", fmt.Errorf("ai gateway api key is not configured")
	}
	model := strings.TrimSpace(settings.AI.GatewayModel)
	if model == "" {
		model = models.IntentGatewayModelGemini25Flash
	}
	if !models.ValidIntentGatewayChatModel(model) {
		return "", fmt.Errorf("AI Gateway model %s cannot generate text; choose %s", model, models.IntentGatewayModelGemini25Flash)
	}

	payload, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal gateway payload: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		req, err := http.NewRequest(http.MethodPost, aiGatewayChatURL, bytes.NewReader(payload))
		if err != nil {
			return "", fmt.Errorf("failed to create gateway request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)

		client := a.HTTPClient
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("gateway request failed: %w", err)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("failed to read gateway response: %w", readErr)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 {
			lastErr = fmt.Errorf("gateway temporary error: %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("gateway error %d: %s", resp.StatusCode, truncateRunes(string(body), 200))
		}
		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return "", fmt.Errorf("failed to parse gateway response: %w", err)
		}
		if len(parsed.Choices) == 0 {
			return "", fmt.Errorf("gateway returned no choices")
		}
		return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("gateway request failed")
	}
	return "", lastErr
}
