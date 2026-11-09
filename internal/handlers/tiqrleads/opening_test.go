package tiqrleads

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyOpeningKind(t *testing.T) {
	assert.Equal(t, openingKindGeneric, classifyOpeningKind(""))
	assert.Equal(t, openingKindGeneric, classifyOpeningKind("Hi"))
	assert.Equal(t, openingKindGeneric, classifyOpeningKind("hello!"))
	assert.Equal(t, openingKindGeneric, classifyOpeningKind("Hi\nThanks"))
	assert.Equal(t, openingKindQuestion, classifyOpeningKind("Hi, is there a refund for the fee?"))
	assert.Equal(t, openingKindQuestion, classifyOpeningKind("What are your hours?"))
	assert.Equal(t, openingKindQuestion, classifyOpeningKind("Hi\nIs there a refund?"))
}

func TestIsDeclineHelp(t *testing.T) {
	assert.True(t, isDeclineHelp("no"))
	assert.True(t, isDeclineHelp("No thanks"))
	assert.True(t, isDeclineHelp("that's all"))
	assert.False(t, isDeclineHelp("is there a refund?"))
	assert.False(t, isDeclineHelp(""))
}

func TestEnsureOpeningText(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	EnsureOpeningText(session, "hello", []string{"hi", "is there a refund?"})
	assert.Equal(t, "hi\nis there a refund?\nhello", asString(session.SessionData[sessionOpeningText]))

	EnsureOpeningText(session, "ignored", []string{"other"})
	assert.Equal(t, "hi\nis there a refund?\nhello", asString(session.SessionData[sessionOpeningText]))
}

func TestEnsureOpeningTextPreviewFallback(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	EnsureOpeningText(session, "What is the fee?", nil)
	assert.Equal(t, "What is the fee?", asString(session.SessionData[sessionOpeningText]))
}

func TestUnansweredInboundTexts(t *testing.T) {
	msgs := []models.ChatbotSessionMessage{
		{Direction: models.DirectionIncoming, Message: "old"},
		{Direction: models.DirectionOutgoing, Message: "bot"},
		{Direction: models.DirectionIncoming, Message: "hi"},
		{Direction: models.DirectionIncoming, Message: "refund?"},
	}
	got := UnansweredInboundTexts(msgs)
	require.Equal(t, []string{"hi", "refund?"}, got)
}

func TestExactFAQID(t *testing.T) {
	faqs := map[string]string{"1": "Is there a refund for the fee?"}
	assert.Equal(t, "1", exactFAQID("is there a refund for the fee", faqs))
	assert.Equal(t, "", exactFAQID("what is your store id?", faqs))
}

func TestBuildHandoffSummaryIncludesPendingQuestion(t *testing.T) {
	session := &models.ChatbotSession{
		PhoneNumber: "919999999999",
		SessionData: models.JSONB{
			"contact_name":         "Asha",
			sessionPendingQuestion: "What is your API key?",
			sessionAnswers: []any{
				map[string]any{
					"kind":           answerKindField,
					"criterion_name": "Name",
					"answer":         "Asha",
				},
			},
		},
	}
	summary := buildHandoffSummary(session)
	assert.Contains(t, summary, "Name: Asha")
	assert.Contains(t, summary, "Unanswered question: What is your API key?")
}
