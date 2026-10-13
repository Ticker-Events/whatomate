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

func TestComposeJevIntent_ProductQueryFromMessage(t *testing.T) {
	ctx := codedIntentContext{AllowCatalog: true}
	raw := JevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":    rawJSON(t, map[string]any{"type": "choice", "choice": "product", "confidence": 0.8, "probabilities": map[string]float64{"product": 0.8}}),
			"language": rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.9, "probabilities": map[string]float64{"en": 0.9}}),
		},
	}
	got, err := composeJevIntent(raw, "I want Themed Cake please", ctx)
	require.NoError(t, err)
	assert.Equal(t, RouteProduct, got.Route)
	assert.Equal(t, "Themed Cake please", got.ProductQuery)
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

func TestProductQueryFromMessage(t *testing.T) {
	assert.Equal(t, "Themed Cake please", productQueryFromMessage("I want Themed Cake please"))
	assert.Equal(t, "kunafa", productQueryFromMessage("show me kunafa"))
	assert.Equal(t, "Themed Cake", productQueryFromMessage("Themed Cake"))
}

func TestBuildJevIntentRequest_NoFAQOrProductSpanOnRouteCall(t *testing.T) {
	state, questions := buildJevIntentRequest("what are your hours", IntentContext{
		AllowFAQ: true,
		FAQs:     map[string]string{"1": "What are your hours?", "2": "Do you deliver?"},
	})
	_, hasFaqs := state["faqs"]
	assert.False(t, hasFaqs)

	route, ok := questions["route"].(map[string]any)
	require.True(t, ok)
	criteria, ok := route["criteria"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, criteria, "faq")
	_, hasFaqID := questions["faq_id"]
	assert.False(t, hasFaqID)
	_, hasSpan := questions["product_span"]
	assert.False(t, hasSpan)

	_, questionsEmpty := buildJevIntentRequest("hello", IntentContext{})
	routeEmpty, ok := questionsEmpty["route"].(map[string]any)
	require.True(t, ok)
	criteriaEmpty, ok := routeEmpty["criteria"].(map[string]any)
	require.True(t, ok)
	_, hasFAQ := criteriaEmpty["faq"]
	assert.False(t, hasFAQ)
}

func TestBuildJevIntentRequest_OmitsCollectionsAlreadyChoices(t *testing.T) {
	state, questions := buildJevIntentRequest("Cakes", IntentContext{
		AllowCatalog: true,
		ChoiceIDs:    map[string]string{"1": "Cakes", "2": "Sweets"},
		Collections:  map[string]string{"1": "Cakes", "2": "Sweets"},
	})
	_, hasCollections := state["collections"]
	assert.False(t, hasCollections)
	route, ok := questions["route"].(map[string]any)
	require.True(t, ok)
	criteria, ok := route["criteria"].(map[string]any)
	require.True(t, ok)
	_, hasCollectionRoute := criteria["collection"]
	assert.False(t, hasCollectionRoute)
	assert.Contains(t, criteria, "product")
	_, hasCollectionID := questions["collection_id"]
	assert.False(t, hasCollectionID)
	assert.Contains(t, questions, "names_product")
}

func TestBuildJevIntentRequest_IncludesExtraCollections(t *testing.T) {
	state, questions := buildJevIntentRequest("Cakes", IntentContext{
		AllowCatalog: true,
		ChoiceIDs:    map[string]string{"buy_products": "Buy products"},
		Collections:  map[string]string{"1": "Cakes"},
	})
	cols, ok := state["collections"].([]map[string]string)
	require.True(t, ok)
	require.Len(t, cols, 1)
	assert.Equal(t, "1", cols[0]["id"])
	assert.Contains(t, questions, "collection_id")
	choiceCrit := questions["choice_id"].(map[string]any)["criteria"].(map[string]any)
	assert.Equal(t, "Buy products", choiceCrit["buy_products"])
}

func TestBuildJevFAQRequest_RanksTopEight(t *testing.T) {
	faqs := map[string]string{}
	for i := 1; i <= 12; i++ {
		faqs[string(rune('a'+i-1))] = "Question about topic " + string(rune('a'+i-1))
	}
	faqs["hours"] = "What are your store hours today?"
	state, questions := buildJevFAQRequest("what are your store hours", faqs)
	items, ok := state["faqs"].([]map[string]string)
	require.True(t, ok)
	require.Len(t, items, maxFAQCandidates)
	assert.Equal(t, "hours", items[0]["id"])
	faqQ := questions["faq_id"].(map[string]any)
	criteria := faqQ["criteria"].(map[string]any)
	assert.Contains(t, criteria, "none")
	assert.Contains(t, criteria, "hours")
	assert.Equal(t, "What are your store hours today?", criteria["hours"])
}

func TestMatchLocalIntent(t *testing.T) {
	ctx := IntentContext{
		AllowCatalog: true,
		AllowFAQ:     true,
		ChoiceIDs:    map[string]string{"buy_products": "Buy products"},
		Collections:  map[string]string{"1": "Cakes"},
		FAQs:         map[string]string{"9": "What are your hours?"},
	}
	got, ok := matchLocalIntent("buy products", ctx)
	require.True(t, ok)
	assert.Equal(t, RouteChoice, got.Route)
	assert.Equal(t, "buy_products", got.ChoiceID)

	got, ok = matchLocalIntent("Cakes!", ctx)
	require.True(t, ok)
	assert.Equal(t, RouteCollection, got.Route)
	assert.Equal(t, "1", got.CollectionID)

	got, ok = matchLocalIntent("What are your hours?", ctx)
	require.True(t, ok)
	assert.Equal(t, RouteFAQ, got.Route)
	assert.Equal(t, "9", got.FaqID)

	_, ok = matchLocalIntent("something else", ctx)
	assert.False(t, ok)
}

func TestComposeJevIntent_FAQ(t *testing.T) {
	ctx := codedIntentContext{
		AllowFAQ: true,
		FAQs:     map[string]string{"1": "What are your hours?"},
	}
	raw := JevSystemOneResponse{
		Answers: map[string]json.RawMessage{
			"route":    rawJSON(t, map[string]any{"type": "choice", "choice": "faq", "confidence": 0.9, "probabilities": map[string]float64{"faq": 0.9}}),
			"faq_id":   rawJSON(t, map[string]any{"type": "choice", "choice": "1", "confidence": 0.88, "probabilities": map[string]float64{"1": 0.88, "none": 0.12}}),
			"language": rawJSON(t, map[string]any{"type": "choice", "choice": "en", "confidence": 0.99, "probabilities": map[string]float64{"en": 0.99}}),
		},
	}
	got, err := composeJevIntent(raw, "what time do you close", ctx)
	require.NoError(t, err)
	assert.Equal(t, RouteFAQ, got.Route)
	assert.Equal(t, "1", got.FaqID)
	assert.GreaterOrEqual(t, got.Confidence, 0.75)
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
