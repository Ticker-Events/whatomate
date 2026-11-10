package handlers

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodedTranslationCache_CrossSessionHit(t *testing.T) {
	mr := miniredis.RunT(t)
	app := &App{Redis: redis.NewClient(&redis.Options{Addr: mr.Addr()})}

	calls := 0
	prev := codedflow.TranslateCodedLine
	codedflow.TranslateCodedLine = func(_ codedflow.Host, _ *models.ChatbotSession, _, text string) (string, error) {
		calls++
		return "HI:" + text, nil
	}
	t.Cleanup(func() { codedflow.TranslateCodedLine = prev })

	src := "What would you like to do?"
	first := codedflow.NewConv(app, &legendHostChat{session: &models.ChatbotSession{
		SessionData: models.JSONB{codedflow.CustomerLanguageKey: "hi"},
	}})
	assert.Equal(t, "HI:"+src, first.Text(src))
	assert.Equal(t, 1, calls)

	second := codedflow.NewConv(app, &legendHostChat{session: &models.ChatbotSession{
		SessionData: models.JSONB{codedflow.CustomerLanguageKey: "hi"},
	}})
	assert.Equal(t, "HI:"+src, second.Text(src))
	assert.Equal(t, 1, calls, "second session must reuse Redis translation")
}

func TestCodedTranslationCache_FailedTranslationNotStored(t *testing.T) {
	mr := miniredis.RunT(t)
	app := &App{Redis: redis.NewClient(&redis.Options{Addr: mr.Addr()})}

	calls := 0
	prev := codedflow.TranslateCodedLine
	codedflow.TranslateCodedLine = func(_ codedflow.Host, _ *models.ChatbotSession, _, text string) (string, error) {
		calls++
		return "", assert.AnError
	}
	t.Cleanup(func() { codedflow.TranslateCodedLine = prev })

	src := "Buy products"
	c := codedflow.NewConv(app, &legendHostChat{session: &models.ChatbotSession{
		SessionData: models.JSONB{codedflow.CustomerLanguageKey: "ml"},
	}})
	assert.Equal(t, src, c.Text(src))
	_, ok := app.GetCodedTranslation("ml", src)
	assert.False(t, ok)

	// Retry still calls the model because the failure was not cached.
	c2 := codedflow.NewConv(app, &legendHostChat{session: &models.ChatbotSession{
		SessionData: models.JSONB{codedflow.CustomerLanguageKey: "ml"},
	}})
	assert.Equal(t, src, c2.Text(src))
	assert.Equal(t, 2, calls)
}

type legendHostChat struct {
	session *models.ChatbotSession
}

func (c *legendHostChat) Session() *models.ChatbotSession             { return c.session }
func (c *legendHostChat) Account() *models.WhatsAppAccount           { return nil }
func (c *legendHostChat) Contact() *models.Contact                   { return nil }
func (c *legendHostChat) UserInput() string                          { return "" }
func (c *legendHostChat) SetUserInput(string)                        {}
func (c *legendHostChat) InboundMedia() codedflow.InboundMedia       { return codedflow.InboundMedia{} }
func (c *legendHostChat) ButtonID() string                           { return "" }
func (c *legendHostChat) SetButtonID(string)                         {}
func (c *legendHostChat) FlowResponseData() map[string]any           { return nil }
func (c *legendHostChat) SetFlowResponseData(map[string]any)         {}
func (c *legendHostChat) QuotedMessage() *codedflow.QuotedMessage    { return nil }
func (c *legendHostChat) Consumed() bool                             { return false }
func (c *legendHostChat) SetConsumed(bool)                           {}
func (c *legendHostChat) Capturing() bool                            { return false }
func (c *legendHostChat) LastTiqr() map[string]any                   { return nil }
func (c *legendHostChat) SetLastTiqr(map[string]any)                 {}
func (c *legendHostChat) LastTiqrErr() string                        { return "" }
func (c *legendHostChat) SetLastTiqrErr(string)                      {}
func (c *legendHostChat) LastTiqrStatus() int                        { return 0 }
func (c *legendHostChat) SetLastTiqrStatus(int)                      {}
func (c *legendHostChat) Preview() *codedflow.PreviewSink            { return nil }
func (c *legendHostChat) SetPreview(*codedflow.PreviewSink)          {}
func (c *legendHostChat) Archive() *codedflow.PreviewSink            { return nil }
func (c *legendHostChat) SetArchive(*codedflow.PreviewSink)          {}
func (c *legendHostChat) Native() any                                { return nil }

func TestCodedTranslationCache_NilRedisKeepsSessionOnly(t *testing.T) {
	app := &App{}
	calls := 0
	prev := codedflow.TranslateCodedLine
	codedflow.TranslateCodedLine = func(_ codedflow.Host, _ *models.ChatbotSession, _, text string) (string, error) {
		calls++
		return "ML:" + text, nil
	}
	t.Cleanup(func() { codedflow.TranslateCodedLine = prev })

	session := &models.ChatbotSession{SessionData: models.JSONB{codedflow.CustomerLanguageKey: "ml"}}
	c := codedflow.NewConv(app, &legendHostChat{session: session})
	require.Equal(t, "ML:Hello", c.Text("Hello"))
	require.Equal(t, "ML:Hello", c.Text("Hello"))
	assert.Equal(t, 1, calls, "session map still dedupes within one chat")
}
