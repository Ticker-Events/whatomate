package tiqrleads

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestTryStoreFactsHelpDelivery(t *testing.T) {
	prevIntent := codedflow.IdentifyCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, _ string, _ codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{Route: codedflow.RouteUnclear, Confidence: 0.1}, nil
	}
	t.Cleanup(func() { codedflow.IdentifyCodedIntent = prevIntent })

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			"store": map[string]any{
				"name":                 "Demo Store",
				"address":              "MG Road, Kochi",
				"delivery_modes":       []any{"PICKUP_FROM_STORE", "DELIVERY_TO_LOCATION"},
				"free_delivery_radius": 3,
				"delivery_radius":      10,
				"latitude":             9.9312,
				"longitude":            76.2673,
			},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: &codedflow.PreviewSink{}})

	got := classifyHelpQuestion(c, "Do you deliver to Kochi?")
	assert.Equal(t, helpOutcomeStoreFacts, got.Kind)
	assert.Contains(t, got.Answer, "delivery")
	assert.Contains(t, got.Answer, "10 km")
}

func TestTryStoreFactsHelpAddress(t *testing.T) {
	prevIntent := codedflow.IdentifyCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, _ string, _ codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{Route: codedflow.RouteUnclear, Confidence: 0.1}, nil
	}
	t.Cleanup(func() { codedflow.IdentifyCodedIntent = prevIntent })

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			"store": map[string]any{
				"name":      "Demo Store",
				"address":   "MG Road, Kochi",
				"latitude":  9.9312,
				"longitude": 76.2673,
			},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: &codedflow.PreviewSink{}})

	got := classifyHelpQuestion(c, "Where are you located?")
	assert.Equal(t, helpOutcomeStoreFacts, got.Kind)
	assert.Contains(t, got.Answer, "MG Road, Kochi")
	assert.Contains(t, got.Answer, "maps.google.com")
}

func TestTryStoreFactsHelpIgnoresUnrelated(t *testing.T) {
	prevIntent := codedflow.IdentifyCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, _ string, _ codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{Route: codedflow.RouteUnclear, Confidence: 0.1}, nil
	}
	t.Cleanup(func() { codedflow.IdentifyCodedIntent = prevIntent })

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			"store": map[string]any{
				"address": "MG Road, Kochi",
				"delivery_modes": []any{"DELIVERY_TO_LOCATION"},
				"delivery_radius": 10,
			},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: &codedflow.PreviewSink{}})

	got := classifyHelpQuestion(c, "What is the price of the blue one?")
	assert.Equal(t, helpOutcomeMiss, got.Kind)
}

func TestShouldDivertFreeTextMatchesStoreFactsWithoutQuestionMark(t *testing.T) {
	prevIntent := codedflow.IdentifyCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, _ string, _ codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{Route: codedflow.RouteUnclear, Confidence: 0.1}, nil
	}
	t.Cleanup(func() { codedflow.IdentifyCodedIntent = prevIntent })

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			"store": map[string]any{
				"delivery_modes":  []any{"DELIVERY_TO_LOCATION"},
				"delivery_radius": 8,
			},
		},
	}
	c := codedflow.NewConv(nil, &helpArchiveChat{session: session, archive: &codedflow.PreviewSink{}})

	assert.True(t, shouldDivertFreeText(c, "Need delivery in Kochi tomorrow"))
	assert.False(t, looksLikeQuestion("Need delivery in Kochi tomorrow"))
	assert.False(t, shouldDivertFreeText(c, "John"))
}
