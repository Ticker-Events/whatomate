package codedflow

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type onceTestChat struct {
	legendTestChat
	userInput string
	consumed  bool
}

func (c *onceTestChat) UserInput() string     { return c.userInput }
func (c *onceTestChat) SetUserInput(v string) { c.userInput = v }
func (c *onceTestChat) Consumed() bool        { return c.consumed }
func (c *onceTestChat) SetConsumed(v bool)    { c.consumed = v }

func TestOnceEmptyMarkerSkipsFnOnReplay(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	chat := &onceTestChat{legendTestChat: legendTestChat{session: session}}
	c := NewConv(nil, chat)

	ran := 0
	c.Once("bind", func() {
		ran++
		session.SessionData["bound"] = "1"
	})
	assert.Equal(t, 1, ran)
	assert.Equal(t, "1", asString(session.SessionData["bound"]))
	names := codedCallNames(mustAnySlice(t, session.SessionData[CallsKey]))
	assert.Equal(t, []string{"bind"}, names)

	c.SetSeq(0)
	c.Once("bind", func() {
		ran++
	})
	assert.Equal(t, 1, ran, "empty Once must skip fn when its marker is next")
	assert.Equal(t, 1, c.Seq())
}

func TestOnceNestedSayReplaysBeforeAskText(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	chat := &onceTestChat{legendTestChat: legendTestChat{session: session}, consumed: true}
	c := NewConv(nil, chat)

	// First visit: nested call then Once marker (same append order as Once+Say).
	c.Once("welcome", func() {
		c.AppendCall(map[string]any{"name": "say", "ok": true})
	})
	names := codedCallNames(mustAnySlice(t, session.SessionData[CallsKey]))
	assert.Equal(t, []string{"say", "welcome"}, names)

	// Second turn: inbound name reply must be captured by AskText, not stolen by Once.
	c.SetSeq(0)
	c.Stop = false
	chat.consumed = false
	chat.userInput = "Aswin Divakar"

	sayReplayed := false
	c.Once("welcome", func() {
		if _, done := c.DoneCall(); done {
			sayReplayed = true
			return
		}
		t.Fatal("nested say record should already be present on replay")
	})
	require.True(t, sayReplayed)
	assert.Equal(t, 2, c.Seq(), "Once marker must be consumed after nested say")

	text, ok := c.AskText("lead_f_name", "What is your name?", StepNote{
		Doing:  "Collect lead field Name",
		Expect: "A free-text reply",
	})
	require.True(t, ok)
	assert.Equal(t, "Aswin Divakar", text)
	assert.True(t, chat.consumed, "name reply must be consumed so the next step waits")
	assert.Equal(t, "Aswin Divakar", asString(session.SessionData["lead_f_name"]))
	names = codedCallNames(mustAnySlice(t, session.SessionData[CallsKey]))
	assert.Equal(t, []string{"say", "welcome", "lead_f_name"}, names)
}

func mustAnySlice(t *testing.T, v any) []any {
	t.Helper()
	items, ok := anySlice(v)
	require.True(t, ok)
	return items
}
