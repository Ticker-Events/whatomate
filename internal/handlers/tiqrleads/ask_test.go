package tiqrleads

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLooksLikeQuestion(t *testing.T) {
	assert.True(t, looksLikeQuestion("What is your refund policy?"))
	assert.True(t, looksLikeQuestion("how much does it cost"))
	assert.True(t, looksLikeQuestion("Do you deliver?"))
	assert.False(t, looksLikeQuestion("John"))
	assert.False(t, looksLikeQuestion("yes"))
	assert.False(t, looksLikeQuestion("a@b.com"))
}

func TestStepAlreadyAnswered(t *testing.T) {
	assert.False(t, stepAlreadyAnswered(nil, answerKindCriterion, "Budget"))

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			sessionAnswers: []any{
				map[string]any{
					"kind":           answerKindCriterion,
					"criterion_name": "Budget",
					"answer":         "Yes",
				},
				map[string]any{
					"kind":           answerKindField,
					"criterion_name": "Email",
					"answer":         "a@b.com",
				},
			},
		},
	}
	c := codedflow.NewConv(nil, &handoffTestChat{session: session})
	assert.True(t, stepAlreadyAnswered(c, answerKindCriterion, "Budget"))
	assert.True(t, stepAlreadyAnswered(c, answerKindCriterion, "budget"))
	assert.False(t, stepAlreadyAnswered(c, answerKindCriterion, "Need"))
	assert.True(t, stepAlreadyAnswered(c, answerKindField, "Email"))
	assert.False(t, stepAlreadyAnswered(c, answerKindField, "Phone"))
}

func TestHasCollectableAnswersIgnoresProfileName(t *testing.T) {
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			sessionAnswers: []any{
				map[string]any{
					"kind":           answerKindField,
					"criterion_name": "Name",
					"answer":         "Asha",
				},
			},
		},
	}
	assert.False(t, HasCollectableAnswers(session))

	session.SessionData[sessionAnswers] = []any{
		map[string]any{
			"kind":           answerKindField,
			"criterion_name": "Name",
			"answer":         "Asha",
		},
		map[string]any{
			"kind":           answerKindCriterion,
			"criterion_name": "Budget",
			"answer":         "Yes",
			"score":          100,
			"weight":         100,
		},
	}
	assert.True(t, HasCollectableAnswers(session))
}

func TestExpiryCreateLeadParams(t *testing.T) {
	session := &models.ChatbotSession{
		BaseModel:   models.BaseModel{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		PhoneNumber: "919999999999",
		SessionData: models.JSONB{
			"contact_name":  "Asha",
			sessionEvalMode: evalRules,
			sessionCriteria: map[string]any{"thank_you_text": "Thanks"},
			sessionAnswers: []any{
				map[string]any{
					"kind":           answerKindCriterion,
					"criterion_name": "Budget",
					"answer":         "Yes",
					"score":          100,
					"weight":         50,
				},
			},
		},
	}
	params, ok := ExpiryCreateLeadParams(session)
	require.True(t, ok)
	assert.Equal(t, "false", params["qualified"])
	assert.Equal(t, completionSessionExpired, params["completion_action"])
	assert.Contains(t, params["answers_json"], "Budget")
	assert.Equal(t, "919999999999", params["phone_number"])
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", params["session_id"])
}

func TestSeedPartialLeadAnswers(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	c := codedflow.NewConv(nil, &handoffTestChat{session: session})
	seedPartialLeadAnswers(c, map[string]any{
		"id":       9,
		"name":     "Ravi",
		"score":    40,
		"category": "unclassified",
		"answers": []any{
			map[string]any{
				"kind":           answerKindCriterion,
				"criterion_name": "Budget",
				"answer":         "Yes",
			},
		},
	})
	assert.Equal(t, "1", asString(session.SessionData[sessionResumePartial]))
	assert.Equal(t, "Ravi", asString(session.SessionData["lead_f_name"]))
	assert.True(t, stepAlreadyAnswered(c, answerKindCriterion, "Budget"))
}
