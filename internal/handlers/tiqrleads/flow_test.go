package tiqrleads

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlowRegistered(t *testing.T) {
	flow := codedflow.ByKey(FlowKey)
	require.NotNil(t, flow)
	assert.Equal(t, "TiQR Leads", flow.Name)
	assert.Equal(t, tiqrLeadsKey, flow.Key)
	assert.NotEmpty(t, flow.Steps)
}

func TestScoreFromSessionData(t *testing.T) {
	data := models.JSONB{
		sessionHighMin:   75,
		sessionMediumMin: 50,
		sessionLowMin:    25,
		sessionAnswers: []any{
			map[string]any{"score": 100, "weight": 50},
			map[string]any{"score": 40, "weight": 50},
		},
	}
	score, category := scoreFromSessionData(data)
	assert.Equal(t, 70, score)
	assert.Equal(t, "medium", category)

	data[sessionAnswers] = []any{
		map[string]any{"kind": answerKindField, "score": 0, "weight": 0},
		map[string]any{"kind": answerKindCriterion, "score": 80, "weight": 100},
	}
	score, category = scoreFromSessionData(data)
	assert.Equal(t, 80, score)
	assert.Equal(t, "high", category)

	data[sessionAnswers] = []any{
		map[string]any{"score": 100, "weight": 100},
	}
	score, category = scoreFromSessionData(data)
	assert.Equal(t, 100, score)
	assert.Equal(t, "high", category)

	data[sessionAnswers] = []any{
		map[string]any{"score": 10, "weight": 100},
	}
	score, category = scoreFromSessionData(data)
	assert.Equal(t, 10, score)
	assert.Equal(t, "unqualified", category)
}

func TestBuildHandoffSummaryQualified(t *testing.T) {
	session := &models.ChatbotSession{
		PhoneNumber: "919999999999",
		SessionData: models.JSONB{
			"contact_name":   "Asha",
			sessionHighMin:   75,
			sessionMediumMin: 50,
			sessionLowMin:    25,
			sessionLeadSaved: "1",
			sessionCriteria: map[string]any{
				"criteria": []any{
					map[string]any{"id": "1"},
					map[string]any{"id": "2"},
				},
			},
			sessionAnswers: []any{
				map[string]any{
					"criterion_name": "Budget",
					"answer":         "High",
					"score":          100,
					"weight":         50,
				},
				map[string]any{
					"criterion_name": "Need",
					"answer":         "Urgent",
					"score":          80,
					"weight":         50,
				},
			},
		},
	}
	summary := buildHandoffSummary(session)
	assert.Contains(t, summary, "Asha")
	assert.Contains(t, summary, "919999999999")
	assert.Contains(t, summary, "Qualified: true")
	assert.Contains(t, summary, "Status: complete")
	assert.Contains(t, summary, "Budget: High")
}

func TestBuildHandoffSummaryPartial(t *testing.T) {
	session := &models.ChatbotSession{
		PhoneNumber: "918888888888",
		SessionData: models.JSONB{
			sessionCriteria: map[string]any{
				"criteria": []any{map[string]any{"id": "1"}, map[string]any{"id": "2"}},
			},
			sessionAnswers: []any{
				map[string]any{
					"criterion_name": "Budget",
					"answer":         "Low",
					"score":          20,
					"weight":         50,
				},
			},
		},
	}
	summary := buildHandoffSummary(session)
	assert.Contains(t, summary, "Qualified: false")
	assert.Contains(t, summary, "Status: partial")
}

func TestStepsFromCriteriaPayloadPrefersSteps(t *testing.T) {
	payload := map[string]any{
		"welcome_text": "Hello lead",
		"criteria": []any{
			map[string]any{"id": "1", "name": "Budget"},
		},
		"steps": []any{
			map[string]any{"kind": "field", "key": "email", "label": "Email", "type": "text"},
			map[string]any{"kind": "criterion", "id": "1", "name": "Budget"},
			map[string]any{"kind": "criterion", "id": "2", "name": "Need"},
		},
	}
	steps := stepsFromCriteriaPayload(payload)
	require.Len(t, steps, 3)
	first, ok := asStringMap(steps[0])
	require.True(t, ok)
	assert.Equal(t, "field", asString(first["kind"]))
	assert.Equal(t, "email", asString(first["key"]))
}

func TestBuildHandoffSummaryWithFieldAnswers(t *testing.T) {
	session := &models.ChatbotSession{
		PhoneNumber: "917777777777",
		SessionData: models.JSONB{
			sessionHighMin:   75,
			sessionMediumMin: 50,
			sessionLowMin:    25,
			sessionCriteria: map[string]any{
				"steps": []any{
					map[string]any{"kind": "field", "key": "email"},
					map[string]any{"kind": "criterion", "id": "1"},
				},
			},
			sessionAnswers: []any{
				map[string]any{
					"kind":           answerKindField,
					"criterion_name": "Email",
					"answer":         "a@b.com",
					"score":          0,
					"weight":         0,
				},
				map[string]any{
					"kind":           answerKindCriterion,
					"criterion_name": "Budget",
					"answer":         "Yes",
					"score":          100,
					"weight":         100,
				},
			},
		},
	}
	summary := buildHandoffSummary(session)
	assert.Contains(t, summary, "Qualified: true")
	assert.Contains(t, summary, "Email: a@b.com")
	assert.Contains(t, summary, "Budget: Yes")
	assert.NotContains(t, summary, "Email: a@b.com (score")
}

func TestIsLeadsSession(t *testing.T) {
	assert.False(t, IsLeadsSession(nil))
	assert.False(t, IsLeadsSession(&models.ChatbotSession{}))
	assert.True(t, IsLeadsSession(&models.ChatbotSession{
		SessionData: models.JSONB{"_coded_flow": FlowKey},
	}))
}

func TestLeadIsQualified(t *testing.T) {
	assert.False(t, leadIsQualified(nil))
	assert.False(t, leadIsQualified(map[string]any{}))
	assert.False(t, leadIsQualified(map[string]any{"id": "1", "qualified": false}))
	assert.True(t, leadIsQualified(map[string]any{"id": "1", "qualified": true}))
	assert.True(t, leadIsQualified(map[string]any{"id": "1", "qualified": "true"}))
}

func TestWriteClassificationFromLeadNoopOnNilConv(t *testing.T) {
	writeClassificationFromLead(nil, map[string]any{"id": 1, "qualified": true, "score": 80})
	writeClassificationFromSession(nil, true)
}

