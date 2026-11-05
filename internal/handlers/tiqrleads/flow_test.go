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

