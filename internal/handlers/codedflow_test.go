package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useStoreREST(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := newTiqrStoreRESTClient
	newTiqrStoreRESTClient = func(baseURL string) *ticker.Client {
		return ticker.NewClient(baseURL, srv.Client())
	}
	t.Cleanup(func() { newTiqrStoreRESTClient = prev })
}

func enableStorePickup(t *testing.T, app *App, orgID uuid.UUID, accountName, keyword string, enabled bool) {
	t.Helper()
	binding := models.CodedFlowBinding{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  orgID,
		WhatsAppAccount: accountName,
		FlowKey:         storePickupKey,
		Keywords:        models.StringArray{keyword},
		IsEnabled:       enabled,
	}
	require.NoError(t, app.DB.Create(&binding).Error)
	if !enabled {
		require.NoError(t, app.DB.Model(&binding).Update("is_enabled", false).Error)
	}
}

func TestMatchCodedFlowTrigger_ContainsAndIgnoresEmpty(t *testing.T) {
	app, org, account, _, _ := newGraphTestFixtures(t)
	enableStorePickup(t, app, org.ID, account.Name, "shop", true)

	flow := app.matchCodedFlowTrigger(org.ID, account.Name, "I want to Shop")
	require.NotNil(t, flow)
	assert.Equal(t, storePickupKey, flow.Key)

	assert.Nil(t, app.matchCodedFlowTrigger(org.ID, account.Name, "hello"))

	empty := models.CodedFlowBinding{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  org.ID,
		WhatsAppAccount: account.Name + "-other",
		FlowKey:         storePickupKey,
		Keywords:        models.StringArray{""},
		IsEnabled:       true,
	}
	require.NoError(t, app.DB.Create(&empty).Error)
	assert.Nil(t, app.matchCodedFlowTrigger(org.ID, account.Name+"-other", "anything"))
}

func TestMatchCodedFlowTrigger_DisabledDoesNotMatch(t *testing.T) {
	app, org, account, _, _ := newGraphTestFixtures(t)
	enableStorePickup(t, app, org.ID, account.Name, "shop", false)
	assert.Nil(t, app.matchCodedFlowTrigger(org.ID, account.Name, "shop"))
}

func TestStorePickup_KeywordDoesNotConsumeTrigger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count": 1,
			"results": []any{
				map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
			},
		})
	}))
	defer srv.Close()
	useStoreREST(t, srv)

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	enableStorePickup(t, app, org.ID, account.Name, "shop", true)

	flow := app.matchCodedFlowTrigger(org.ID, account.Name, "please shop")
	require.NotNil(t, flow)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "please shop", "", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)

	assert.Equal(t, "show_collections", session.CurrentStep)
	assert.Equal(t, models.SessionStatusActive, session.Status)
	assert.Equal(t, storePickupKey, session.SessionData[codedFlowDataKey])
	assert.Nil(t, session.SessionData["collection_id"])
	_, ok := session.SessionData["collections"]
	assert.True(t, ok)
}

func TestStorePickup_MissingCommerceEnds(t *testing.T) {
	app, _, account, contact, session := newGraphTestFixtures(t)
	flow := codedFlowByKey(storePickupKey)
	require.NotNil(t, flow)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)

	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Empty(t, session.CurrentStep)
	assert.Nil(t, session.SessionData[codedFlowDataKey])

	var msgs []models.ChatbotSessionMessage
	require.NoError(t, app.DB.Where("session_id = ? AND direction = ?", session.ID, models.DirectionOutgoing).Find(&msgs).Error)
	joined := ""
	for _, msg := range msgs {
		joined += msg.Message
	}
	assert.Contains(t, joined, "Thank you for shopping with us.")
}

func TestStorePickup_OptionBranches(t *testing.T) {
	cases := []struct {
		name     string
		options  []any
		buttonID string
		wantStep string
	}{
		{
			name:     "none",
			options:  []any{},
			buttonID: "101",
			wantStep: "more_or_checkout",
		},
		{
			name: "one",
			options: []any{
				map[string]any{"id": "9", "name": "Regular", "price": "40"},
			},
			buttonID: "101",
			wantStep: "ask_quantity",
		},
		{
			name: "many",
			options: []any{
				map[string]any{"id": "9", "name": "Regular", "price": "40"},
				map[string]any{"id": "10", "name": "Large", "price": "60"},
			},
			buttonID: "101",
			wantStep: "choose_option",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			products := []any{
				map[string]any{
					"id": "101", "name": "Kunafa", "min_price": "120", "options": tc.options,
				},
				map[string]any{
					"id": "102", "name": "Canape", "min_price": "80",
					"options": []any{map[string]any{"id": "1", "name": "Default", "price": "10"}},
				},
			}
			app, account, contact, session := runStorePickupToProducts(t, products)
			require.NoError(t, app.runCodedFlow(account, contact, session, codedFlowByKey(storePickupKey), "Kunafa", tc.buttonID, nil))
			require.NoError(t, app.DB.First(session, session.ID).Error)
			assert.Equal(t, tc.wantStep, session.CurrentStep)
			if tc.name == "one" {
				assert.Equal(t, "9", session.SessionData["option_id"])
				assert.Equal(t, "Regular", session.SessionData["option_name"])
			}
			if tc.name == "none" {
				var msgs []models.ChatbotSessionMessage
				require.NoError(t, app.DB.Where("session_id = ?", session.ID).Find(&msgs).Error)
				found := false
				for _, msg := range msgs {
					if strings.Contains(msg.Message, "isn't available") {
						found = true
					}
				}
				assert.True(t, found)
			}
		})
	}
}

func TestStorePickup_AppendsCartAndAddMoreSkipsCollections(t *testing.T) {
	var collectionCalls int
	products := []any{
		map[string]any{
			"id": "101", "name": "Kunafa", "min_price": "120",
			"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
		},
		map[string]any{
			"id": "102", "name": "Canape", "min_price": "80",
			"options": []any{map[string]any{"id": "1", "name": "Default", "price": "10"}},
		},
	}
	app, account, contact, session := runStorePickupToProductsCounting(t, products, &collectionCalls)
	flow := codedFlowByKey(storePickupKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)
	require.Equal(t, "ask_quantity", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "nope", "", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)
	assert.Equal(t, "ask_quantity", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "2", "", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)
	assert.Equal(t, "more_or_checkout", session.CurrentStep)
	assert.Equal(t, float64(1), session.SessionData["cart_count"])

	cart, ok := session.SessionData["tiqr_cart"].([]any)
	require.True(t, ok)
	require.Len(t, cart, 1)
	item, ok := cart[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "9", item["product_option"])
	assert.Equal(t, "2", item["quantity"])

	before := collectionCalls
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add more items", storePickupAddMore, nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)
	assert.Equal(t, "show_collections", session.CurrentStep)
	assert.Equal(t, before, collectionCalls)
}

func runStorePickupToProducts(t *testing.T, products []any) (*App, *models.WhatsAppAccount, *models.Contact, *models.ChatbotSession) {
	t.Helper()
	return runStorePickupToProductsCounting(t, products, nil)
}

func runStorePickupToProductsCounting(
	t *testing.T,
	products []any,
	collectionCalls *int,
) (*App, *models.WhatsAppAccount, *models.Contact, *models.ChatbotSession) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/category/") {
			if collectionCalls != nil {
				*collectionCalls++
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count":   len(products),
			"results": products,
		})
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedFlowByKey(storePickupKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)
	require.Equal(t, "show_collections", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)
	require.Equal(t, "show_products", session.CurrentStep)
	return app, account, contact, session
}
