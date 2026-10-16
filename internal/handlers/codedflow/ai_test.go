package codedflow

import (
	"strings"
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type legendTestChat struct {
	session *models.ChatbotSession
}

func (c *legendTestChat) Session() *models.ChatbotSession             { return c.session }
func (c *legendTestChat) Account() *models.WhatsAppAccount           { return nil }
func (c *legendTestChat) Contact() *models.Contact                   { return nil }
func (c *legendTestChat) UserInput() string                          { return "" }
func (c *legendTestChat) SetUserInput(string)                        {}
func (c *legendTestChat) InboundMedia() InboundMedia                 { return InboundMedia{} }
func (c *legendTestChat) ButtonID() string                           { return "" }
func (c *legendTestChat) SetButtonID(string)                         {}
func (c *legendTestChat) FlowResponseData() map[string]any           { return nil }
func (c *legendTestChat) SetFlowResponseData(map[string]any)         {}
func (c *legendTestChat) QuotedMessage() *QuotedMessage              { return nil }
func (c *legendTestChat) Consumed() bool                             { return false }
func (c *legendTestChat) SetConsumed(bool)                           {}
func (c *legendTestChat) Capturing() bool                            { return false }
func (c *legendTestChat) LastTiqr() map[string]any                   { return nil }
func (c *legendTestChat) SetLastTiqr(map[string]any)                 {}
func (c *legendTestChat) LastTiqrErr() string                        { return "" }
func (c *legendTestChat) SetLastTiqrErr(string)                      {}
func (c *legendTestChat) LastTiqrStatus() int                        { return 0 }
func (c *legendTestChat) SetLastTiqrStatus(int)                      {}
func (c *legendTestChat) Preview() *PreviewSink                      { return nil }
func (c *legendTestChat) SetPreview(*PreviewSink)                    {}
func (c *legendTestChat) Archive() *PreviewSink                      { return nil }
func (c *legendTestChat) SetArchive(*PreviewSink)                    {}
func (c *legendTestChat) Native() any                                { return nil }

func TestTranslationPromptRules(t *testing.T) {
	got := translationPrompt("manglish", "Hello {{name}}")
	assert.Contains(t, got, "Translate the entire message into manglish")
	assert.Contains(t, got, "Hello {{name}}")
	assert.Contains(t, got, "never leave part of the message untranslated")
	assert.Contains(t, got, "exactly one language and one script")
	assert.Contains(t, got, "only English letters a-z and A-Z")
	assert.Contains(t, got, "Never translate IDs, product names, collection names, option names")
	assert.Contains(t, got, "Return only the translation")
}

func TestLimitDoesNotTranslate(t *testing.T) {
	prev := TranslateCodedLine
	TranslateCodedLine = func(_ Host, _ *models.ChatbotSession, _, text string) (string, error) {
		t.Fatalf("limit must not translate %q", text)
		return "", nil
	}
	t.Cleanup(func() { TranslateCodedLine = prev })

	c := NewConv(nil, &legendTestChat{session: &models.ChatbotSession{
		SessionData: models.JSONB{CustomerLanguageKey: "hi"},
	}})
	assert.Equal(t, "Buy products", c.limit("Buy products", 20))
	assert.Equal(t, "Check order sta", c.limit("Check order status now", 15))
}

func TestBodyWithOptionLegendBelowCutoff(t *testing.T) {
	prev := TranslateCodedLine
	TranslateCodedLine = func(_ Host, _ *models.ChatbotSession, _, text string) (string, error) {
		return "HI:" + text, nil
	}
	t.Cleanup(func() { TranslateCodedLine = prev })

	c := NewConv(nil, &legendTestChat{session: &models.ChatbotSession{
		SessionData: models.JSONB{CustomerLanguageKey: "hi"},
	}})
	got := c.bodyWithOptionLegend("What would you like to do?", []string{
		"Buy products",
		"Check order status",
		"Talk to staff",
	})
	require.Contains(t, got, "What would you like to do?")
	assert.Contains(t, got, "Buy products — HI:Buy products")
	assert.Contains(t, got, "Check order status — HI:Check order status")
	assert.Contains(t, got, "Talk to staff — HI:Talk to staff")
}

func TestBodyWithOptionLegendSkipsAtCutoff(t *testing.T) {
	prev := TranslateCodedLine
	called := false
	TranslateCodedLine = func(_ Host, _ *models.ChatbotSession, _, text string) (string, error) {
		called = true
		return "HI:" + text, nil
	}
	t.Cleanup(func() { TranslateCodedLine = prev })

	c := NewConv(nil, &legendTestChat{session: &models.ChatbotSession{
		SessionData: models.JSONB{CustomerLanguageKey: "hi"},
	}})
	got := c.bodyWithOptionLegend("Choose a collection", []string{"One", "Two", "Three", "Four"})
	assert.Equal(t, "Choose a collection", got)
	assert.False(t, called, "legend must not translate when option count is at the cutoff")
}

func TestBodyWithOptionLegendDedupesTitles(t *testing.T) {
	prev := TranslateCodedLine
	TranslateCodedLine = func(_ Host, _ *models.ChatbotSession, _, text string) (string, error) {
		return "HI:" + text, nil
	}
	t.Cleanup(func() { TranslateCodedLine = prev })

	c := NewConv(nil, &legendTestChat{session: &models.ChatbotSession{
		SessionData: models.JSONB{CustomerLanguageKey: "ml"},
	}})
	got := c.bodyWithOptionLegend("Pick one", []string{"Add to cart", "Add to cart", "Checkout"})
	assert.Contains(t, got, "Add to cart — HI:Add to cart")
	assert.Contains(t, got, "Checkout — HI:Checkout")
	assert.Equal(t, 1, strings.Count(got, "Add to cart — HI:Add to cart"))
}
