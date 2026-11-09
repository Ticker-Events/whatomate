package codedflow

import (
	"errors"
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type archiveTestChat struct {
	legendTestChat
	archive *PreviewSink
}

func (c *archiveTestChat) Archive() *PreviewSink { return c.archive }

func TestNoteLocalIntentRecordsAICall(t *testing.T) {
	sink := &PreviewSink{}
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	c := NewConv(nil, &archiveTestChat{
		legendTestChat: legendTestChat{session: session},
		archive:        sink,
	})

	c.NoteLocalIntent(IntentResult{
		Route:      RouteFAQ,
		FaqID:      "42",
		Confidence: 1,
		Reasoning:  "exact FAQ question match",
	})

	require.Len(t, sink.AICalls, 1)
	call := sink.AICalls[0]
	assert.Equal(t, "intent", call.Role)
	assert.Equal(t, "(local exact match)", call.Prompt)
	assert.Equal(t, RouteFAQ, call.Route)
	assert.Equal(t, 1.0, call.Confidence)
	require.NotNil(t, call.Grounded)
	assert.True(t, *call.Grounded)
	assert.Equal(t, "42", call.Parsed["faq_id"])
}

func TestClassifyIntentRecordsSuccess(t *testing.T) {
	prev := IdentifyCodedIntent
	IdentifyCodedIntent = func(_ Host, _ *models.ChatbotSession, message string, _ IntentContext) (IntentResult, error) {
		return IntentResult{
			Route:      RouteFAQ,
			FaqID:      "7",
			Confidence: 0.91,
			Reasoning:  "paraphrase of FAQ",
			Answer:     message,
		}, nil
	}
	t.Cleanup(func() { IdentifyCodedIntent = prev })

	sink := &PreviewSink{}
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	c := NewConv(nil, &archiveTestChat{
		legendTestChat: legendTestChat{session: session},
		archive:        sink,
	})

	raw, err := c.ClassifyIntent("do you refund fees?", IntentContext{
		AllowFAQ: true,
		FAQs:     map[string]string{"7": "Is there a refund for the fee?"},
	})
	require.NoError(t, err)
	assert.Equal(t, RouteFAQ, raw.Route)

	require.Len(t, sink.AICalls, 1)
	call := sink.AICalls[0]
	assert.Equal(t, "intent", call.Role)
	assert.NotEmpty(t, call.Prompt)
	assert.NotContains(t, call.Prompt, "(local exact match)")
	assert.Equal(t, RouteFAQ, call.Route)
	assert.Equal(t, 0.91, call.Confidence)
	assert.Empty(t, call.Error)
	assert.Contains(t, call.Response, `"faq_id":"7"`)
}

func TestClassifyIntentRecordsError(t *testing.T) {
	prev := IdentifyCodedIntent
	IdentifyCodedIntent = func(_ Host, _ *models.ChatbotSession, _ string, _ IntentContext) (IntentResult, error) {
		return IntentResult{}, errors.New("ai is not configured")
	}
	t.Cleanup(func() { IdentifyCodedIntent = prev })

	sink := &PreviewSink{}
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	c := NewConv(nil, &archiveTestChat{
		legendTestChat: legendTestChat{session: session},
		archive:        sink,
	})

	_, err := c.ClassifyIntent("hello", IntentContext{})
	require.Error(t, err)

	require.Len(t, sink.AICalls, 1)
	assert.Equal(t, "ai is not configured", sink.AICalls[0].Error)
	assert.Equal(t, "intent", sink.AICalls[0].Role)
}
