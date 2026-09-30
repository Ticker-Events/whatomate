package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		ChoiceIDs: map[string]string{tiqrBuyProducts: "Buy products"},
	}
	raw := jevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":     rawJSON(t, map[string]any{"type": "choice", "choice": "choice", "confidence": 0.91, "probabilities": map[string]float64{"choice": 0.9, "unclear": 0.1}}),
			"choice_id": rawJSON(t, map[string]any{"type": "choice", "choice": tiqrBuyProducts, "confidence": 0.88, "probabilities": map[string]float64{tiqrBuyProducts: 0.88, "none": 0.12}}),
			"language":  rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.99, "probabilities": map[string]float64{"en": 0.99}}),
		},
	}
	got, err := composeJevIntent(raw, "buy products", ctx)
	require.NoError(t, err)
	assert.Equal(t, codedRouteChoice, got.Route)
	assert.Equal(t, tiqrBuyProducts, got.ChoiceID)
	assert.Equal(t, "en", got.Language)
	assert.GreaterOrEqual(t, got.Confidence, 0.75)
}

func TestComposeJevIntent_ProductSpan(t *testing.T) {
	ctx := codedIntentContext{AllowCatalog: true}
	raw := jevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":        rawJSON(t, map[string]any{"type": "choice", "choice": "product", "confidence": 0.8, "probabilities": map[string]float64{"product": 0.8}}),
			"product_span": rawJSON(t, map[string]any{"type": "choice", "choice": "Themed Cake", "confidence": 0.85, "probabilities": map[string]float64{"Themed Cake": 0.85}}),
			"language":     rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.9, "probabilities": map[string]float64{"en": 0.9}}),
		},
	}
	got, err := composeJevIntent(raw, "I want Themed Cake please", ctx)
	require.NoError(t, err)
	assert.Equal(t, codedRouteProduct, got.Route)
	assert.Equal(t, "Themed Cake", got.ProductQuery)
}

func TestComposeJevIntent_QuantityWord(t *testing.T) {
	ctx := codedIntentContext{Pattern: `^[0-9]+$`}
	raw := jevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":           rawJSON(t, map[string]any{"type": "choice", "choice": "answer", "confidence": 0.7, "probabilities": map[string]float64{"answer": 0.7}}),
			"states_quantity": rawJSON(t, map[string]any{"type": "noul", "noul": 0.95}),
			"language":        rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.9, "probabilities": map[string]float64{"en": 0.9}}),
		},
	}
	got, err := composeJevIntent(raw, "two", ctx)
	require.NoError(t, err)
	assert.Equal(t, codedRouteAnswer, got.Route)
	assert.Equal(t, "2", got.Answer)
}

func TestComposeJevIntent_CatalogOverlapUnclear(t *testing.T) {
	ctx := codedIntentContext{
		AllowCatalog: true,
		Collections:  map[string]string{"1": "Cakes"},
	}
	raw := jevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":            rawJSON(t, map[string]any{"type": "choice", "choice": "product", "confidence": 0.6, "probabilities": map[string]float64{"product": 0.5, "collection": 0.5}}),
			"names_collection": rawJSON(t, map[string]any{"type": "noul", "noul": 0.9}),
			"names_product":    rawJSON(t, map[string]any{"type": "noul", "noul": 0.85}),
			"language":         rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.9, "probabilities": map[string]float64{"en": 0.9}}),
		},
	}
	got, err := composeJevIntent(raw, "cakes", ctx)
	require.NoError(t, err)
	assert.Equal(t, codedRouteUnclear, got.Route)
}

func TestComposeJevIntent_CatalogJudgeOverridesSplitRoute(t *testing.T) {
	ctx := codedIntentContext{
		AllowCatalog: true,
		Collections:  map[string]string{"1": "Cakes"},
	}
	raw := jevSystemOneResponse{
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
	assert.Equal(t, codedRouteCollection, got.Route)
	assert.Equal(t, "1", got.CollectionID)
}

func TestComposeJevIntent_MissingConfidenceUnclear(t *testing.T) {
	raw := jevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route": rawJSON(t, map[string]any{"type": "choice", "choice": "choice", "confidence": 0, "probabilities": map[string]float64{}}),
		},
	}
	got, err := composeJevIntent(raw, "hello", codedIntentContext{})
	require.NoError(t, err)
	assert.Equal(t, codedRouteUnclear, got.Route)
}

func TestCallSystemOne_HTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/systemone", r.URL.Path)
		assert.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), `"model":"jev-latest"`)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "jev-1.13.0",
			"answers": map[string]any{
				"route": map[string]any{
					"type": "choice", "choice": "choice", "confidence": 0.9,
					"probabilities": map[string]float64{"choice": 0.9},
				},
				"choice_id": map[string]any{
					"type": "choice", "choice": tiqrBuyProducts, "confidence": 0.9,
					"probabilities": map[string]float64{tiqrBuyProducts: 0.9},
				},
				"language": map[string]any{
					"type": "choice", "choice": "en", "confidence": 0.99,
					"probabilities": map[string]float64{"en": 0.99},
				},
			},
		})
	}))
	defer server.Close()

	app := &App{HTTPClient: server.Client()}
	endpoint := jevEndpoint{URL: server.URL + "/v1/systemone", Key: "test-key", Model: models.IntentTypeSafeModel}
	state, questions := buildJevIntentRequest("buy", codedIntentContext{
		ChoiceIDs: map[string]string{tiqrBuyProducts: "Buy products"},
	})
	raw, err := app.callSystemOne(endpoint, state, questions)
	require.NoError(t, err)
	got, err := composeJevIntent(raw, "buy", codedIntentContext{
		ChoiceIDs: map[string]string{tiqrBuyProducts: "Buy products"},
	})
	require.NoError(t, err)
	assert.Equal(t, codedRouteChoice, got.Route)
	assert.Equal(t, tiqrBuyProducts, got.ChoiceID)
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
