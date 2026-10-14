package handlers

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMenuWelcomeBody(t *testing.T) {
	store := map[string]any{"name": "Demo"}
	assert.Equal(t, "Welcome to Demo.\n\nWhat would you like to do?", tiqrecommerce.MenuWelcomeBody(store, ""))
	assert.Equal(t, "What would you like to do?", tiqrecommerce.MenuWelcomeBody(map[string]any{}, ""))
	assert.Equal(t,
		"Warm hello from Demo bakery.\n\nWhat would you like to do?",
		tiqrecommerce.MenuWelcomeBody(store, "Warm hello from Demo bakery."),
	)
	assert.Equal(t,
		"Hi there.\n\nWhat would you like to do?",
		tiqrecommerce.MenuWelcomeBody(store, "Hi there.\n\nWhat would you like to do?"),
	)
}

func TestBuildCodedFlowWelcomeFacts(t *testing.T) {
	store := map[string]any{
		"name":                 "Demo Bakery",
		"description":          "Fresh cakes and sweets",
		"address":              "12 MG Road",
		"delivery_modes":       []any{"PICKUP_FROM_STORE", "DELIVERY_TO_LOCATION"},
		"free_delivery_radius": "3",
		"delivery_radius":      "8",
	}
	info := map[string]any{
		"about_us":             "Family bakery since 1990",
		"privacy_policy":       "We never sell data",
		"refund_policy":        "No refunds",
		"shipping_policy":      "Ships in 2 days",
		"terms_and_conditions": "Terms apply",
	}
	page := map[string]any{
		"results": []any{
			map[string]any{"name": "Cakes", "listing_priority": float64(0)},
			map[string]any{"name": "Sweets", "listing_priority": float64(-1)},
		},
	}
	facts := buildCodedFlowWelcomeFacts(store, info, page)
	assert.Equal(t, "Demo Bakery", facts.Name)
	assert.Equal(t, "Fresh cakes and sweets", facts.Description)
	assert.Equal(t, "Family bakery since 1990", facts.AboutUs)
	assert.Equal(t, "12 MG Road", facts.Address)
	assert.Equal(t, []string{"PICKUP_FROM_STORE", "DELIVERY_TO_LOCATION"}, facts.DeliveryModes)
	assert.Equal(t, "3", facts.FreeDeliveryKm)
	assert.Equal(t, "8", facts.DeliveryRadius)
	assert.Equal(t, []string{"Sweets", "Cakes"}, facts.CollectionNames)

	prompt := composeCodedFlowWelcomePrompt(facts)
	assert.Contains(t, prompt, "Demo Bakery")
	assert.Contains(t, prompt, "Sweets, Cakes")
	assert.Contains(t, prompt, "about_us: Family bakery since 1990")
	assert.NotContains(t, prompt, "privacy_policy")
	assert.NotContains(t, prompt, "refund_policy")
	assert.NotContains(t, prompt, "We never sell data")
}

func TestBuildCodedFlowWelcomeFacts_PickupOnlyOmitsRadii(t *testing.T) {
	facts := buildCodedFlowWelcomeFacts(map[string]any{
		"name":                 "Pickup Only",
		"delivery_modes":       []any{"PICKUP_FROM_STORE"},
		"free_delivery_radius": "3",
		"delivery_radius":      "8",
	}, nil, nil)
	assert.Empty(t, facts.FreeDeliveryKm)
	assert.Empty(t, facts.DeliveryRadius)
}

func TestTiqrEcommerce_UsesCachedWelcome(t *testing.T) {
	srv := newStoreServer(t, twoProducts(nil), nil)
	useStoreREST(t, srv)
	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	require.NoError(t, app.DB.Create(&models.CodedFlowBinding{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  org.ID,
		WhatsAppAccount: account.Name,
		FlowKey:         tiqrecommerce.FlowKey,
		Keywords:        models.StringArray{"shop"},
		IsEnabled:       true,
		Settings: models.JSONB{
			codedSettingWelcomeMessage: "Warm hello from Demo bakery. We bake cakes and sweets.",
		},
	}).Error)

	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Warm hello from Demo bakery")
	assert.Contains(t, blob, "What would you like to do?")
	assert.Contains(t, blob, `"title":"Buy products"`)
	assert.Contains(t, blob, `"title":"Check order status"`)
	assert.Contains(t, blob, `"title":"Talk to staff"`)
	assert.NotContains(t, blob, "Welcome to Demo")
}

func TestRegenerateCodedFlowWelcome_FromREST(t *testing.T) {
	app, org, account, _, _ := newGraphTestFixtures(t)
	srv := newStoreServerWith(t, twoProducts(nil), nil, map[string]any{
		"id":             42,
		"name":           "Demo Bakery",
		"description":    "Cakes and sweets",
		"address":        "12 MG Road",
		"delivery_modes": []any{"PICKUP_FROM_STORE"},
		"test_collections": []any{
			map[string]any{"id": "1", "name": "Sweets"},
			map[string]any{"id": "2", "name": "Cakes"},
		},
	})
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		Enabled:         true,
		Provider:        models.AIProviderGoogle,
		APIKey:          "test-key",
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	settings, err := app.getChatbotSettingsCached(org.ID, account.Name)
	require.NoError(t, err)
	require.NotNil(t, settings)

	var capturedPrompt string
	prev := generateCodedFlowWelcomeAI
	generateCodedFlowWelcomeAI = func(_ *App, _ *models.ChatbotSettings, prompt string) (string, error) {
		capturedPrompt = prompt
		return "Warm hello from Demo Bakery. We sell cakes and sweets. Tap a button below to get started.", nil
	}
	t.Cleanup(func() { generateCodedFlowWelcomeAI = prev })

	welcome, generatedAt, err := app.regenerateCodedFlowWelcome(org.ID, account.Name, settings)
	require.NoError(t, err)
	assert.Contains(t, welcome, "Warm hello from Demo Bakery")
	assert.NotEmpty(t, generatedAt)
	assert.Equal(t, welcome, app.EcommerceWelcomeMessage(org.ID, account.Name))

	assert.Contains(t, capturedPrompt, "Demo Bakery")
	assert.Contains(t, capturedPrompt, "Sweets")
	assert.Contains(t, capturedPrompt, "Cakes")
	assert.Contains(t, capturedPrompt, "Store facts")
	assert.NotContains(t, capturedPrompt, "privacy_policy")
	assert.True(t, strings.Contains(capturedPrompt, "about_us") || !strings.Contains(capturedPrompt, "We never sell"))
}
