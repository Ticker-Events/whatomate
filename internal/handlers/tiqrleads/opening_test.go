package tiqrleads

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type helpArchiveChat struct {
	session *models.ChatbotSession
	archive *codedflow.PreviewSink
}

func (c *helpArchiveChat) Session() *models.ChatbotSession             { return c.session }
func (c *helpArchiveChat) Account() *models.WhatsAppAccount           { return nil }
func (c *helpArchiveChat) Contact() *models.Contact                   { return nil }
func (c *helpArchiveChat) UserInput() string                          { return "" }
func (c *helpArchiveChat) SetUserInput(string)                        {}
func (c *helpArchiveChat) InboundMedia() codedflow.InboundMedia       { return codedflow.InboundMedia{} }
func (c *helpArchiveChat) ButtonID() string                           { return "" }
func (c *helpArchiveChat) SetButtonID(string)                         {}
func (c *helpArchiveChat) FlowResponseData() map[string]any           { return nil }
func (c *helpArchiveChat) SetFlowResponseData(map[string]any)         {}
func (c *helpArchiveChat) QuotedMessage() *codedflow.QuotedMessage    { return nil }
func (c *helpArchiveChat) Consumed() bool                             { return false }
func (c *helpArchiveChat) SetConsumed(bool)                           {}
func (c *helpArchiveChat) Capturing() bool                            { return false }
func (c *helpArchiveChat) LastTiqr() map[string]any                   { return nil }
func (c *helpArchiveChat) SetLastTiqr(map[string]any)                 {}
func (c *helpArchiveChat) LastTiqrErr() string                        { return "" }
func (c *helpArchiveChat) SetLastTiqrErr(string)                      {}
func (c *helpArchiveChat) LastTiqrStatus() int                        { return 0 }
func (c *helpArchiveChat) SetLastTiqrStatus(int)                      {}
func (c *helpArchiveChat) Preview() *codedflow.PreviewSink            { return nil }
func (c *helpArchiveChat) SetPreview(*codedflow.PreviewSink)          {}
func (c *helpArchiveChat) Archive() *codedflow.PreviewSink            { return c.archive }
func (c *helpArchiveChat) SetArchive(p *codedflow.PreviewSink)        { c.archive = p }
func (c *helpArchiveChat) Native() any                                { return nil }

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

func TestClassifyHelpQuestionNotesLocalFAQMatch(t *testing.T) {
	sink := &codedflow.PreviewSink{}
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			"faqs": []any{
				map[string]any{"id": "1", "question": "Is there a refund for the fee?", "answer": "Yes, within 7 days."},
			},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: sink})

	got := classifyHelpQuestion(c, "is there a refund for the fee")
	assert.Equal(t, helpOutcomeFAQ, got.Kind)
	assert.Equal(t, "Yes, within 7 days.", got.Answer)

	require.Len(t, sink.AICalls, 1)
	assert.Equal(t, "(local exact match)", sink.AICalls[0].Prompt)
	assert.Equal(t, codedflow.RouteFAQ, sink.AICalls[0].Route)
}

func TestClassifyHelpQuestionNotesIntentSuccess(t *testing.T) {
	prev := codedflow.IdentifyCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, _ string, _ codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Route:      codedflow.RouteFAQ,
			FaqID:      "1",
			Confidence: 0.95,
			Reasoning:  "customer asked about refunds",
		}, nil
	}
	t.Cleanup(func() { codedflow.IdentifyCodedIntent = prev })

	sink := &codedflow.PreviewSink{}
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			"faqs": []any{
				map[string]any{"id": "1", "question": "Is there a refund for the fee?", "answer": "Yes, within 7 days."},
			},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: sink})

	got := classifyHelpQuestion(c, "can I get my money back?")
	assert.Equal(t, helpOutcomeFAQ, got.Kind)
	assert.Equal(t, "Yes, within 7 days.", got.Answer)

	require.Len(t, sink.AICalls, 1)
	assert.Equal(t, "intent", sink.AICalls[0].Role)
	assert.NotEqual(t, "(local exact match)", sink.AICalls[0].Prompt)
	assert.Empty(t, sink.AICalls[0].Error)
	assert.Equal(t, 0.95, sink.AICalls[0].Confidence)
}

func TestClassifyHelpQuestionNotesIntentErrorAsMiss(t *testing.T) {
	prev := codedflow.IdentifyCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, _ string, _ codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{}, errors.New("ai is not configured")
	}
	t.Cleanup(func() { codedflow.IdentifyCodedIntent = prev })

	sink := &codedflow.PreviewSink{}
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			"faqs": []any{
				map[string]any{"id": "1", "question": "Is there a refund for the fee?", "answer": "Yes, within 7 days."},
			},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: sink})

	got := classifyHelpQuestion(c, "Pg courses available?")
	assert.Equal(t, helpOutcomeMiss, got.Kind)

	require.Len(t, sink.AICalls, 1)
	assert.Equal(t, "ai is not configured", sink.AICalls[0].Error)
}

func TestClassifyHelpQuestionAnswersFromStoreInfo(t *testing.T) {
	prevIntent := codedflow.IdentifyCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, _ string, _ codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Route:      codedflow.RouteUnclear,
			Confidence: 0.4,
			Reasoning:  "no faq",
		}, nil
	}
	t.Cleanup(func() { codedflow.IdentifyCodedIntent = prevIntent })

	prevStore := codedflow.AnswerFromStoreInfoAI
	codedflow.AnswerFromStoreInfoAI = func(_ codedflow.Host, _ *models.ChatbotSession, _ string) (string, error) {
		return `{"confident":true,"confidence":0.96,"answer":"We are a family bakery since 1990."}`, nil
	}
	t.Cleanup(func() { codedflow.AnswerFromStoreInfoAI = prevStore })

	sink := &codedflow.PreviewSink{}
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			"faqs": []any{},
			codedflow.SessionStoreInfoKey: map[string]any{
				"about_us": "Family bakery since 1990.",
			},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: sink})

	got := classifyHelpQuestion(c, "What kind of store is this?")
	assert.Equal(t, helpOutcomeStoreInfo, got.Kind)
	assert.Equal(t, "We are a family bakery since 1990.", got.Answer)
}

func TestClassifyHelpQuestionRefundPolicyFromStoreInfo(t *testing.T) {
	prevIntent := codedflow.IdentifyCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, _ string, _ codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Route:      codedflow.RouteHandoff,
			Confidence: 0.9,
			Reasoning:  "no faq match",
		}, nil
	}
	t.Cleanup(func() { codedflow.IdentifyCodedIntent = prevIntent })

	prevStore := codedflow.AnswerFromStoreInfoAI
	codedflow.AnswerFromStoreInfoAI = func(_ codedflow.Host, _ *models.ChatbotSession, _ string) (string, error) {
		return `{"confident":true,"confidence":0.93,"answer":"Refunds are available within 7 days."}`, nil
	}
	t.Cleanup(func() { codedflow.AnswerFromStoreInfoAI = prevStore })

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			codedflow.SessionStoreInfoKey: map[string]any{
				"refund_policy": "Refunds within 7 days of purchase.",
			},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: &codedflow.PreviewSink{}})

	got := classifyHelpQuestion(c, "Can I get a refund?")
	assert.Equal(t, helpOutcomeStoreInfo, got.Kind)
	assert.Equal(t, "Refunds are available within 7 days.", got.Answer)
}

func TestClassifyHelpQuestionStaffSkipsStoreInfo(t *testing.T) {
	prevIntent := codedflow.IdentifyCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, _ string, _ codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Route:      codedflow.RouteHandoff,
			Confidence: 0.95,
			Reasoning:  "asked for staff",
		}, nil
	}
	t.Cleanup(func() { codedflow.IdentifyCodedIntent = prevIntent })

	called := false
	prevStore := codedflow.AnswerFromStoreInfoAI
	codedflow.AnswerFromStoreInfoAI = func(_ codedflow.Host, _ *models.ChatbotSession, _ string) (string, error) {
		called = true
		return `{"confident":true,"confidence":0.99,"answer":"nope"}`, nil
	}
	t.Cleanup(func() { codedflow.AnswerFromStoreInfoAI = prevStore })

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			codedflow.SessionStoreInfoKey: map[string]any{"about_us": "Family bakery"},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: &codedflow.PreviewSink{}})

	got := classifyHelpQuestion(c, "talk to staff")
	assert.Equal(t, helpOutcomeStaff, got.Kind)
	assert.False(t, called)
}

func TestClassifyHelpQuestionStoreInfoMissStaysMiss(t *testing.T) {
	prevIntent := codedflow.IdentifyCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, _ string, _ codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Route:      codedflow.RouteUnclear,
			Confidence: 0.2,
		}, nil
	}
	t.Cleanup(func() { codedflow.IdentifyCodedIntent = prevIntent })

	prevStore := codedflow.AnswerFromStoreInfoAI
	codedflow.AnswerFromStoreInfoAI = func(_ codedflow.Host, _ *models.ChatbotSession, _ string) (string, error) {
		return `{"confident":false,"confidence":0.2,"answer":""}`, nil
	}
	t.Cleanup(func() { codedflow.AnswerFromStoreInfoAI = prevStore })

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			codedflow.SessionStoreInfoKey: map[string]any{"about_us": "Family bakery"},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: &codedflow.PreviewSink{}})

	got := classifyHelpQuestion(c, "What is your API key?")
	assert.Equal(t, helpOutcomeMiss, got.Kind)
}

func TestTiqrLeadsDoesNotCallIdentifyCodedIntentDirectly(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", e.Name()))
		require.NoError(t, err)
		assert.NotContains(t, string(body), "IdentifyCodedIntent",
			"%s must use Conv.ClassifyIntent so preview AI is always noted", e.Name())
	}
}
