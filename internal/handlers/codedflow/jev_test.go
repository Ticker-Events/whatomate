package codedflow

import (
	"encoding/json"
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCodedAnswer(t *testing.T) {
	assert.Equal(t, "2", normalizeCodedAnswer("two", `^[0-9]+$`))
	assert.Equal(t, "2", normalizeCodedAnswer("2", `^[0-9]+$`))
	assert.Equal(t, "12", normalizeCodedAnswer("twelve", `^[0-9]+$`))
}

func TestComposeJevIntent_Choice(t *testing.T) {
	ctx := codedIntentContext{
		ChoiceIDs: map[string]string{"buy_products": "Buy products"},
	}
	raw := JevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":     rawJSON(t, map[string]any{"type": "choice", "choice": "choice", "confidence": 0.91, "probabilities": map[string]float64{"choice": 0.9, "unclear": 0.1}}),
			"choice_id": rawJSON(t, map[string]any{"type": "choice", "choice": "buy_products", "confidence": 0.88, "probabilities": map[string]float64{"buy_products": 0.88, "none": 0.12}}),
			"language":  rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.99, "probabilities": map[string]float64{"en": 0.99}}),
		},
	}
	got, err := composeJevIntent(raw, "buy products", ctx)
	require.NoError(t, err)
	assert.Equal(t, RouteChoice, got.Route)
	assert.Equal(t, "buy_products", got.ChoiceID)
	assert.Equal(t, "en", got.Language)
	assert.GreaterOrEqual(t, got.Confidence, 0.75)
}

func TestComposeJevIntent_ProductSpan(t *testing.T) {
	ctx := codedIntentContext{AllowCatalog: true}
	raw := JevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":        rawJSON(t, map[string]any{"type": "choice", "choice": "product", "confidence": 0.8, "probabilities": map[string]float64{"product": 0.8}}),
			"product_span": rawJSON(t, map[string]any{"type": "choice", "choice": "Themed Cake", "confidence": 0.85, "probabilities": map[string]float64{"Themed Cake": 0.85}}),
			"language":     rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.9, "probabilities": map[string]float64{"en": 0.9}}),
		},
	}
	got, err := composeJevIntent(raw, "I want Themed Cake please", ctx)
	require.NoError(t, err)
	assert.Equal(t, RouteProduct, got.Route)
	assert.Equal(t, "Themed Cake", got.ProductQuery)
}

func TestComposeJevIntent_QuantityWord(t *testing.T) {
	ctx := codedIntentContext{Pattern: `^[0-9]+$`}
	raw := JevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":           rawJSON(t, map[string]any{"type": "choice", "choice": "answer", "confidence": 0.7, "probabilities": map[string]float64{"answer": 0.7}}),
			"states_quantity": rawJSON(t, map[string]any{"type": "noul", "noul": 0.95}),
			"language":        rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.9, "probabilities": map[string]float64{"en": 0.9}}),
		},
	}
	got, err := composeJevIntent(raw, "two", ctx)
	require.NoError(t, err)
	assert.Equal(t, RouteAnswer, got.Route)
	assert.Equal(t, "2", got.Answer)
}

func TestComposeJevIntent_CatalogOverlapUnclear(t *testing.T) {
	ctx := codedIntentContext{
		AllowCatalog: true,
		Collections:  map[string]string{"1": "Cakes"},
	}
	raw := JevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":            rawJSON(t, map[string]any{"type": "choice", "choice": "product", "confidence": 0.6, "probabilities": map[string]float64{"product": 0.5, "collection": 0.5}}),
			"names_collection": rawJSON(t, map[string]any{"type": "noul", "noul": 0.9}),
			"names_product":    rawJSON(t, map[string]any{"type": "noul", "noul": 0.85}),
			"language":         rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.9, "probabilities": map[string]float64{"en": 0.9}}),
		},
	}
	got, err := composeJevIntent(raw, "cakes", ctx)
	require.NoError(t, err)
	assert.Equal(t, RouteUnclear, got.Route)
}

func TestComposeJevIntent_CatalogJudgeOverridesSplitRoute(t *testing.T) {
	ctx := codedIntentContext{
		AllowCatalog: true,
		Collections:  map[string]string{"1": "Cakes"},
	}
	raw := JevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":            rawJSON(t, map[string]any{"type": "choice", "choice": "unclear", "confidence": 0.4, "probabilities": map[string]float64{"unclear": 0.4, "collection": 0.3, "product": 0.3}}),
			"names_collection": rawJSON(t, map[string]any{"type": "noul", "noul": 0.92}),
			"names_product":    rawJSON(t, map[string]any{"type": "noul", "noul": 0.1}),
			"collection_id":    rawJSON(t, map[string]any{"type": "choice", "choice": "1", "confidence": 0.9, "probabilities": map[string]float64{"1": 0.9, "none": 0.1}}),
			"language":         rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.9, "probabilities": map[string]float64{"en": 0.9}}),
		},
	}
	got, err := composeJevIntent(raw, "Cakes", ctx)
	require.NoError(t, err)
	assert.Equal(t, RouteCollection, got.Route)
	assert.Equal(t, "1", got.CollectionID)
}

func TestComposeJevIntent_MissingConfidenceUnclear(t *testing.T) {
	raw := JevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route": rawJSON(t, map[string]any{"type": "choice", "choice": "choice", "confidence": 0, "probabilities": map[string]float64{}}),
		},
	}
	got, err := composeJevIntent(raw, "hello", codedIntentContext{})
	require.NoError(t, err)
	assert.Equal(t, RouteUnclear, got.Route)
}


func TestJevEndpointFor(t *testing.T) {
	_, err := jevEndpointFor(&models.ChatbotSettings{AI: models.AIConfig{IntentProvider: models.IntentProviderJev}})
	require.Error(t, err)

	ep, err := jevEndpointFor(&models.ChatbotSettings{AI: models.AIConfig{
		IntentProvider: models.IntentProviderJev,
		TypeSafeAPIKey: "k",
	}})
	require.NoError(t, err)
	assert.Equal(t, models.IntentTypeSafeModel, ep.Model)
	assert.Contains(t, ep.URL, "api.typesafe.ai")

	ep, err = jevEndpointFor(&models.ChatbotSettings{AI: models.AIConfig{
		IntentProvider: models.IntentProviderGateway,
		GatewayAPIKey:  "g",
	}})
	require.NoError(t, err)
	assert.Equal(t, models.IntentGatewayModelJev, ep.Model)
	assert.Contains(t, ep.URL, "ai-gateway.vercel.sh")

	_, err = jevEndpointFor(&models.ChatbotSettings{AI: models.AIConfig{
		IntentProvider: models.IntentProviderGateway,
		GatewayAPIKey:  "g",
		GatewayModel:   "openai/gpt-6-astra",
	}})
	require.Error(t, err)
}

func TestProductSpanCandidates(t *testing.T) {
	spans := productSpanCandidates("Themed Cake please")
	assert.Contains(t, spans, "Themed Cake please")
	assert.Contains(t, spans, "Themed")
	assert.Contains(t, spans, "Themed Cake")
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
