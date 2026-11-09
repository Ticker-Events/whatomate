package codedflow

import (
	"errors"
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeStoreInfoPayloadCapsAndFilters(t *testing.T) {
	long := stringsRepeat("a", storeInfoFieldMaxRunes+50)
	got := normalizeStoreInfoPayload(map[string]any{
		"about_us":        "  Family bakery  ",
		"refund_policy":   long,
		"privacy_policy":  "",
		"extra_ignored":   "nope",
		"shipping_policy": "Ships in 2 days",
	})
	assert.Equal(t, "Family bakery", got["about_us"])
	assert.Equal(t, "Ships in 2 days", got["shipping_policy"])
	assert.NotContains(t, got, "privacy_policy")
	assert.NotContains(t, got, "extra_ignored")
	assert.Equal(t, storeInfoFieldMaxRunes, len([]rune(asString(got["refund_policy"]))))
}

func TestAcceptedStoreInfoAnswer(t *testing.T) {
	assert.True(t, acceptedStoreInfoAnswer(StoreInfoAnswer{
		Confident: true, Confidence: 0.9, Answer: "We bake bread.",
	}))
	assert.False(t, acceptedStoreInfoAnswer(StoreInfoAnswer{
		Confident: true, Confidence: 0.89, Answer: "We bake bread.",
	}))
	assert.False(t, acceptedStoreInfoAnswer(StoreInfoAnswer{
		Confident: false, Confidence: 0.95, Answer: "We bake bread.",
	}))
	assert.False(t, acceptedStoreInfoAnswer(StoreInfoAnswer{
		Confident: true, Confidence: 0.95, Answer: "  ",
	}))
}

func TestParseStoreInfoAnswer(t *testing.T) {
	got, err := parseStoreInfoAnswer(`Here you go: {"confident":true,"confidence":0.95,"answer":"Family bakery since 1990."}`)
	require.NoError(t, err)
	assert.True(t, got.Confident)
	assert.Equal(t, 0.95, got.Confidence)
	assert.Equal(t, "Family bakery since 1990.", got.Answer)
}

func TestTryStoreInfoAnswerConfident(t *testing.T) {
	prev := AnswerFromStoreInfoAI
	AnswerFromStoreInfoAI = func(_ Host, _ *models.ChatbotSession, _ string) (string, error) {
		return `{"confident":true,"confidence":0.94,"answer":"We are a family bakery since 1990."}`, nil
	}
	t.Cleanup(func() { AnswerFromStoreInfoAI = prev })

	sink := &PreviewSink{}
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			SessionStoreInfoKey: map[string]any{
				"about_us": "Family bakery since 1990.",
			},
		},
	}
	c := NewConv(nil, &archiveTestChat{
		legendTestChat: legendTestChat{session: session},
		archive:        sink,
	})

	answer, ok := c.TryStoreInfoAnswer("What is this store?")
	assert.True(t, ok)
	assert.Equal(t, "We are a family bakery since 1990.", answer)
	require.Len(t, sink.AICalls, 1)
	assert.Equal(t, codedFlowRoleStoreInfo, sink.AICalls[0].Role)
	require.NotNil(t, sink.AICalls[0].Grounded)
	assert.True(t, *sink.AICalls[0].Grounded)
}

func TestTryStoreInfoAnswerLowConfidenceIsMiss(t *testing.T) {
	prev := AnswerFromStoreInfoAI
	AnswerFromStoreInfoAI = func(_ Host, _ *models.ChatbotSession, _ string) (string, error) {
		return `{"confident":true,"confidence":0.7,"answer":"Maybe."}`, nil
	}
	t.Cleanup(func() { AnswerFromStoreInfoAI = prev })

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			SessionStoreInfoKey: map[string]any{"about_us": "Family bakery"},
		},
	}
	c := NewConv(nil, &archiveTestChat{
		legendTestChat: legendTestChat{session: session},
		archive:        &PreviewSink{},
	})
	answer, ok := c.TryStoreInfoAnswer("What courses do you offer?")
	assert.False(t, ok)
	assert.Empty(t, answer)
}

func TestTryStoreInfoAnswerBlankFactsSkipsModel(t *testing.T) {
	called := false
	prev := AnswerFromStoreInfoAI
	AnswerFromStoreInfoAI = func(_ Host, _ *models.ChatbotSession, _ string) (string, error) {
		called = true
		return "", errors.New("should not call")
	}
	t.Cleanup(func() { AnswerFromStoreInfoAI = prev })

	c := NewConv(nil, &archiveTestChat{
		legendTestChat: legendTestChat{session: &models.ChatbotSession{SessionData: models.JSONB{}}},
		archive:        &PreviewSink{},
	})
	answer, ok := c.TryStoreInfoAnswer("Who are you?")
	assert.False(t, ok)
	assert.Empty(t, answer)
	assert.False(t, called)
}

func TestTryStoreInfoAnswerPersonRequestSkipsModel(t *testing.T) {
	called := false
	prev := AnswerFromStoreInfoAI
	AnswerFromStoreInfoAI = func(_ Host, _ *models.ChatbotSession, _ string) (string, error) {
		called = true
		return `{"confident":true,"confidence":0.99,"answer":"nope"}`, nil
	}
	t.Cleanup(func() { AnswerFromStoreInfoAI = prev })

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			SessionStoreInfoKey: map[string]any{"about_us": "Family bakery"},
		},
	}
	c := NewConv(nil, &archiveTestChat{
		legendTestChat: legendTestChat{session: session},
		archive:        &PreviewSink{},
	})
	answer, ok := c.TryStoreInfoAnswer("talk to staff")
	assert.False(t, ok)
	assert.Empty(t, answer)
	assert.False(t, called)
}

func TestBuildStoreInfoAnswerPromptIncludesPolicies(t *testing.T) {
	prompt := buildStoreInfoAnswerPrompt("Do you refund?", map[string]string{
		"about_us":      "Bakery",
		"refund_policy": "Refunds within 7 days.",
	})
	assert.Contains(t, prompt, "about_us: Bakery")
	assert.Contains(t, prompt, "refund_policy: Refunds within 7 days.")
	assert.Contains(t, prompt, "Do you refund?")
	assert.NotContains(t, prompt, "privacy_policy")
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
