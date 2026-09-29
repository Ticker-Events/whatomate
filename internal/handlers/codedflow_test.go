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

func TestPickupOrderParams_MapsFlowContext(t *testing.T) {
	params := pickupOrderParams(map[string]any{
		"customer_name":    "Aswin Divakar",
		"customer_email":   "buyer@example.com",
		"customer_phone":   "9846435358",
		"phone_number":     "919846435358",
		"address_line_one": "Infopark Rd",
		"address_line_two": "TCS",
		"city":             "Kakkanad",
		"state":            "Keralam",
		"country":          "India",
		"pincode":          "682042",
		"customer_notes":   "Note",
		"tiqr_cart":        []any{map[string]any{"product_option": "1312", "quantity": "2"}},
	})

	assert.Equal(t, "buyer@example.com", params["email"])
	assert.Equal(t, "Note", params["notes"])
	assert.Equal(t, "PICKUP_FROM_STORE", params["delivery_mode"])
	assert.JSONEq(t, `[{"product_option":"1312","quantity":"2"}]`, params["items"])
	assert.JSONEq(t, `{
		"name": "Aswin Divakar",
		"address_line_1": "Infopark Rd",
		"address_line_2": "TCS",
		"city": "Kakkanad",
		"state": "Keralam",
		"country": "India",
		"pincode": "682042",
		"email": "buyer@example.com",
		"phone_number": "9846435358",
		"phone": "9846435358"
	}`, params["new_address"])
}

func TestPickupOrderParams_DoesNotUsePhoneAsEmail(t *testing.T) {
	params := pickupOrderParams(map[string]any{
		"customer_email": "9846435358",
		"customer_phone": "9846435358",
		"phone_number":   "919846435358",
		"email":          "buyer@example.com",
	})
	assert.Equal(t, "buyer@example.com", params["email"])

	var address map[string]string
	require.NoError(t, json.Unmarshal([]byte(params["new_address"]), &address))
	assert.Equal(t, "buyer@example.com", address["email"])
	assert.Equal(t, "9846435358", address["phone"])
}

func useStoreREST(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := newTiqrStoreRESTClient
	newTiqrStoreRESTClient = func(baseURL string) *ticker.Client {
		return ticker.NewClient(baseURL, srv.Client())
	}
	t.Cleanup(func() { newTiqrStoreRESTClient = prev })
}

func enableTiqrEcommerce(t *testing.T, app *App, orgID uuid.UUID, accountName, keyword string, enabled bool) {
	t.Helper()
	binding := models.CodedFlowBinding{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  orgID,
		WhatsAppAccount: accountName,
		FlowKey:         tiqrEcommerceKey,
		Keywords:        models.StringArray{keyword},
		IsEnabled:       enabled,
	}
	require.NoError(t, app.DB.Create(&binding).Error)
	if !enabled {
		require.NoError(t, app.DB.Model(&binding).Update("is_enabled", false).Error)
	}
}

func useCodedTranslate(t *testing.T, translate func(lang, text string) (string, error)) {
	t.Helper()
	prev := translateCodedLine
	translateCodedLine = func(_ *App, _ *models.ChatbotSession, lang, text string) (string, error) {
		if translate == nil {
			t.Errorf("translated %q", text)
			return text, nil
		}
		return translate(lang, text)
	}
	t.Cleanup(func() { translateCodedLine = prev })
}

func useCodedIntent(t *testing.T, intent func(string, codedIntentContext) (codedIntentResult, error), guide func(string) (string, error)) {
	t.Helper()
	prevIntent, prevGuide := identifyCodedIntent, guideCodedIntent
	identifyCodedIntent = func(_ *App, _ *models.ChatbotSession, message string, ctx codedIntentContext) (codedIntentResult, error) {
		if intent == nil {
			t.Fatal("intent identifier was called")
		}
		return intent(message, ctx)
	}
	guideCodedIntent = func(_ *App, _ *models.ChatbotSession, message, _ string, _ codedIntentContext) (string, error) {
		if guide == nil {
			t.Fatal("guide was called")
		}
		return guide(message)
	}
	t.Cleanup(func() {
		identifyCodedIntent = prevIntent
		guideCodedIntent = prevGuide
	})
}

func outgoingBlob(t *testing.T, app *App, session *models.ChatbotSession) string {
	t.Helper()
	var b strings.Builder
	var logs []models.ChatbotSessionMessage
	require.NoError(t, app.DB.Where("session_id = ?", session.ID).Find(&logs).Error)
	for _, msg := range logs {
		b.WriteString(msg.Message)
		b.WriteByte('\n')
	}
	var msgs []models.Message
	require.NoError(t, app.DB.Where("contact_id = ?", session.ContactID).Find(&msgs).Error)
	for _, msg := range msgs {
		b.WriteString(msg.Content)
		b.WriteByte('\n')
		if msg.InteractiveData != nil {
			raw, err := json.Marshal(msg.InteractiveData)
			require.NoError(t, err)
			b.Write(raw)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func reloadSession(t *testing.T, app *App, session *models.ChatbotSession) {
	t.Helper()
	require.NoError(t, app.DB.First(session, session.ID).Error)
}

type storeCounts struct {
	store       int
	collections int
	products    int
	searches    int
	lastSearch  string
}

func newStoreServer(t *testing.T, products []any, counts *storeCounts) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			if counts != nil {
				counts.collections++
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 2,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
					map[string]any{"id": "58", "name": "Cakes", "description": "Celebration cakes"},
				},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			if counts != nil {
				counts.products++
				if q := r.URL.Query().Get("search"); q != "" {
					counts.searches++
					counts.lastSearch = q
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count":   len(products),
				"results": products,
			})
		default:
			if counts != nil {
				counts.store++
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func startEcommerce(t *testing.T, products []any, counts *storeCounts) (*App, *models.WhatsAppAccount, *models.Contact, *models.ChatbotSession) {
	t.Helper()
	srv := newStoreServer(t, products, counts)
	useStoreREST(t, srv)
	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NotNil(t, flow)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.Equal(t, "intent", session.CurrentStep)
	return app, account, contact, session
}

func twoProducts(options []any) []any {
	return []any{
		map[string]any{
			"id": "101", "name": "Kunafa", "min_price": "120", "options": options,
		},
		map[string]any{
			"id": "102", "name": "Canape", "min_price": "80",
			"options": []any{map[string]any{"id": "1", "name": "Default", "price": "10"}},
		},
	}
}

func TestMatchCodedFlowTrigger_ContainsAndIgnoresEmpty(t *testing.T) {
	app, org, account, _, _ := newGraphTestFixtures(t)
	enableTiqrEcommerce(t, app, org.ID, account.Name, "shop", true)

	flow := app.matchCodedFlowTrigger(org.ID, account.Name, "I want to Shop")
	require.NotNil(t, flow)
	assert.Equal(t, tiqrEcommerceKey, flow.Key)
	assert.Equal(t, "TiQR Ecommerce", flow.Name)

	assert.Nil(t, app.matchCodedFlowTrigger(org.ID, account.Name, "hello"))

	empty := models.CodedFlowBinding{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  org.ID,
		WhatsAppAccount: account.Name + "-other",
		FlowKey:         tiqrEcommerceKey,
		Keywords:        models.StringArray{""},
		IsEnabled:       true,
	}
	require.NoError(t, app.DB.Create(&empty).Error)
	assert.Nil(t, app.matchCodedFlowTrigger(org.ID, account.Name+"-other", "anything"))
}

func TestMatchCodedFlowTrigger_DisabledDoesNotMatch(t *testing.T) {
	app, org, account, _, _ := newGraphTestFixtures(t)
	enableTiqrEcommerce(t, app, org.ID, account.Name, "shop", false)
	assert.Nil(t, app.matchCodedFlowTrigger(org.ID, account.Name, "shop"))
}

func TestTiqrEcommerce_KeywordDoesNotSelectMenu(t *testing.T) {
	var counts storeCounts
	app, account, _, session := startEcommerce(t, twoProducts(nil), &counts)
	assert.Equal(t, models.SessionStatusActive, session.Status)
	assert.Equal(t, tiqrEcommerceKey, session.SessionData[codedFlowDataKey])
	assert.Nil(t, session.SessionData["collection_id"])
	_, ok := session.SessionData["collections"]
	assert.True(t, ok)
	assert.Equal(t, 1, counts.store)
	assert.Equal(t, 1, counts.collections)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Welcome to Demo")
	assert.Contains(t, blob, "buy_products")
	assert.Contains(t, blob, "check_order_status")
	assert.Contains(t, blob, "talk_to_agent")
	_ = account
}

func TestTiqrEcommerce_MissingCommerceEnds(t *testing.T) {
	app, _, account, contact, session := newGraphTestFixtures(t)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NotNil(t, flow)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)

	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Empty(t, session.CurrentStep)
	assert.Nil(t, session.SessionData[codedFlowDataKey])
	assert.Contains(t, outgoingBlob(t, app, session), "Thank you for shopping with us.")
}

func TestTiqrEcommerce_OptionBranches(t *testing.T) {
	cases := []struct {
		name     string
		options  []any
		wantStep string
	}{
		{name: "none", options: []any{}, wantStep: "next"},
		{
			name:     "one",
			options:  []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
			wantStep: "quantity",
		},
		{
			name: "many",
			options: []any{
				map[string]any{"id": "9", "name": "Regular", "price": "40"},
				map[string]any{"id": "10", "name": "Large", "price": "60"},
			},
			wantStep: "option",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, account, contact, session := startEcommerce(t, twoProducts(tc.options), nil)
			flow := codedFlowByKey(tiqrEcommerceKey)
			require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
			reloadSession(t, app, session)
			require.Equal(t, "collection", session.CurrentStep)

			require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
			reloadSession(t, app, session)
			require.Equal(t, "product", session.CurrentStep)

			require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
			reloadSession(t, app, session)
			assert.Equal(t, tc.wantStep, session.CurrentStep)
			if tc.name == "one" {
				assert.Equal(t, "9", session.SessionData["option_id"])
				assert.Equal(t, "Regular", session.SessionData["option_name"])
			}
			if tc.name == "none" {
				assert.Contains(t, outgoingBlob(t, app, session), "isn't available")
			}
		})
	}
}

func TestTiqrEcommerce_AppendsCartAndAddMoreSkipsCollections(t *testing.T) {
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{Language: "en", Route: codedRouteUnclear, Confidence: 0.4}, nil
	}, func(string) (string, error) {
		return "Send a whole number.", nil
	})
	var counts storeCounts
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerce(t, products, &counts)
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.Equal(t, "quantity", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "nope", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "quantity", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "Send a whole number.")
	assert.Equal(t, models.SessionStatusActive, session.Status)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "2", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "next", session.CurrentStep)
	assert.Equal(t, float64(1), session.SessionData["cart_count"])

	cart, ok := session.SessionData["tiqr_cart"].([]any)
	require.True(t, ok)
	require.Len(t, cart, 1)
	item, ok := cart[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "9", item["product_option"])
	assert.Equal(t, "2", item["quantity"])

	before := counts.collections
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add more items", tiqrAddMore, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, before, counts.collections)
	assert.Equal(t, 1, counts.store)
}

func TestTiqrEcommerce_GuideUnclearStays(t *testing.T) {
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{Language: "en", Route: codedRouteUnclear, Confidence: 0.4}, nil
	}, func(string) (string, error) {
		return "Would you like to buy something, check an order, or talk to staff?", nil
	})
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "what are your hours?", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, models.SessionStatusActive, session.Status)
	assert.Nil(t, session.SessionData["collection_id"])
	assert.Contains(t, outgoingBlob(t, app, session), "Would you like to buy something")
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Zero(t, transfers)
}

func TestTiqrEcommerce_HandoffTransfers(t *testing.T) {
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{Language: "en", Route: codedRouteHandoff, Confidence: 0.95}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "I want to talk to a person", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Nil(t, session.SessionData[codedFlowDataKey])
	assert.Nil(t, session.SessionData["collection_id"])
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_AIErrorTransfers(t *testing.T) {
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{}, assert.AnError
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "hello", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_TalkToAgentSkipsAI(t *testing.T) {
	useCodedIntent(t, nil, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Talk to staff", tiqrTalkToAgent, nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Contains(t, outgoingBlob(t, app, session), "connecting you with a team member")
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_IntentTitleOnlySelectsBuy(t *testing.T) {
	useCodedIntent(t, nil, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.NotContains(t, outgoingBlob(t, app, session), codedAgentHandoff)
}

func TestTiqrEcommerce_IntentPaddedButtonIDSelectsBuy(t *testing.T) {
	useCodedIntent(t, nil, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", " buy_products ", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.NotContains(t, outgoingBlob(t, app, session), codedAgentHandoff)
}

func TestTiqrEcommerce_IntentTitleOnlyOrderStatus(t *testing.T) {
	useCodedIntent(t, nil, nil)
	prev := lookupLatestOrder
	lookupLatestOrder = func(*App, *models.WhatsAppAccount, *models.ChatbotSession) (map[string]any, error) {
		return map[string]any{"display_uid": "ST-1", "status": "CONFIRMED"}, nil
	}
	t.Cleanup(func() { lookupLatestOrder = prev })

	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Check order status", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Order ST-1 is confirmed.")
	assert.NotContains(t, blob, codedAgentHandoff)
}

func TestTiqrEcommerce_UnknownButtonRepromptsIntent(t *testing.T) {
	useCodedIntent(t, nil, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Nope", "not_a_menu_button", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "intent", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "What would you like to do?")
	assert.NotContains(t, blob, codedAgentHandoff)
}

func TestTiqrEcommerce_OrderStatus(t *testing.T) {
	prev := lookupLatestOrder
	lookupLatestOrder = func(*App, *models.WhatsAppAccount, *models.ChatbotSession) (map[string]any, error) {
		return map[string]any{"display_uid": "ST-1", "status": "CONFIRMED"}, nil
	}
	t.Cleanup(func() { lookupLatestOrder = prev })

	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Check order status", tiqrCheckOrderStatus, nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Contains(t, outgoingBlob(t, app, session), "Order ST-1 is confirmed.")
}

func TestTiqrEcommerce_OrderStatusMissing(t *testing.T) {
	prev := lookupLatestOrder
	lookupLatestOrder = func(*App, *models.WhatsAppAccount, *models.ChatbotSession) (map[string]any, error) {
		return nil, assert.AnError
	}
	t.Cleanup(func() { lookupLatestOrder = prev })

	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Check order status", tiqrCheckOrderStatus, nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Contains(t, outgoingBlob(t, app, session), "couldn't find a recent order")
}

func TestTiqrEcommerce_PreferredLanguageFromIntent(t *testing.T) {
	var welcomeCalls int
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{
			Language:   "hi",
			Route:      codedRouteChoice,
			ChoiceID:   tiqrBuyProducts,
			Confidence: 0.92,
		}, nil
	}, nil)
	useCodedTranslate(t, func(_, text string) (string, error) {
		if strings.Contains(text, "Welcome to") {
			welcomeCalls++
		}
		return "HI:" + text, nil
	})
	app, org, account, contact, session := newGraphTestFixtures(t)
	srv := newStoreServer(t, twoProducts(nil), nil)
	useStoreREST(t, srv)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "intent", session.CurrentStep)
	assert.Empty(t, session.SessionData[customerLanguageKey])
	assert.Contains(t, outgoingBlob(t, app, session), "Welcome to Demo")
	assert.Zero(t, welcomeCalls)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "mujhe kharidna hai", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "hi", session.SessionData[customerLanguageKey])
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "HI:Choose a collection")
}

func TestTiqrEcommerce_EnglishSkipsTranslation(t *testing.T) {
	useCodedIntent(t, nil, nil)
	useCodedTranslate(t, nil)
	app, _, _, session := startEcommerce(t, twoProducts(nil), nil)
	assert.Empty(t, session.SessionData[customerLanguageKey])
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Welcome to Demo")
	assert.NotContains(t, blob, "HI:")
}

func TestTiqrEcommerce_ProductSearchFromMenu(t *testing.T) {
	var counts storeCounts
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{
			Language:     "en",
			Route:        codedRouteProduct,
			ProductQuery: "Themed Cake",
			Confidence:   0.91,
		}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), &counts)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "I want to purchase Themed Cake", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "product", session.CurrentStep)
	assert.Equal(t, 1, counts.searches)
	assert.Equal(t, "Themed Cake", counts.lastSearch)
	assert.Equal(t, "Themed Cake", session.SessionData["collection_name"])
	assert.Nil(t, session.SessionData["collection_id"])
}

func TestTiqrEcommerce_CollectionRouteFromMenu(t *testing.T) {
	var counts storeCounts
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{
			Language:     "en",
			Route:        codedRouteCollection,
			CollectionID: "58",
			Confidence:   0.9,
		}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), &counts)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "what cakes are available", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "product", session.CurrentStep)
	assert.Equal(t, "58", session.SessionData["collection_id"])
	assert.Equal(t, "Cakes", session.SessionData["collection_name"])
	assert.Equal(t, 1, counts.products)
	assert.Zero(t, counts.searches)
}

func TestTiqrEcommerce_InventedCollectionTransfers(t *testing.T) {
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{
			Language:     "en",
			Route:        codedRouteCollection,
			CollectionID: "999",
			Confidence:   0.99,
		}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "show me muffins", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_GuideExhaustionTransfers(t *testing.T) {
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{Language: "en", Route: codedRouteUnclear, Confidence: 0.2}, nil
	}, func(string) (string, error) {
		return "Please choose buy, order status, or staff.", nil
	})
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	for i := 0; i < codedIntentSettings.MaxGuideTurns; i++ {
		require.NoError(t, app.runCodedFlow(account, contact, session, flow, "hmm", "", nil))
		reloadSession(t, app, session)
		assert.Equal(t, "intent", session.CurrentStep)
		assert.Equal(t, models.SessionStatusActive, session.Status)
	}
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "still lost", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestValidateCodedIntent_RejectsInventedIDs(t *testing.T) {
	ctx := codedIntentContext{
		AllowCatalog: true,
		ChoiceIDs:    map[string]string{tiqrBuyProducts: "Buy products"},
		Collections:  map[string]string{"57": "Sweets"},
	}
	_, ok := validateCodedIntent(codedIntentResult{Route: codedRouteCollection, CollectionID: "999", Confidence: 1}, ctx)
	assert.False(t, ok)
	_, ok = validateCodedIntent(codedIntentResult{Route: codedRouteProduct, ProductQuery: "Cake", ChoiceID: "x", Confidence: 1}, ctx)
	assert.False(t, ok)
	_, ok = validateCodedIntent(codedIntentResult{Route: codedRouteChoice, ChoiceID: tiqrBuyProducts, Confidence: 1}, ctx)
	assert.True(t, ok)
	_, ok = validateCodedIntent(codedIntentResult{Route: codedRouteHandoff, Confidence: 1}, ctx)
	assert.True(t, ok)
}

// A step that records a call must not hand that record to the next step in
// the same turn. Otherwise the collections lookup is replayed as the menu
// answer and an empty choice transfers the chat.
func TestCodedCallCursorSkipsCallsMadeThisRun(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	c := &Conv{chat: &chatNodeCtx{session: session}}

	c.appendCall(map[string]any{"name": "store", "ok": true, "var": "store", "value": map[string]any{"name": "Demo"}})
	_, done := c.doneCall()
	assert.False(t, done)

	c.seq = 0
	rec, done := c.doneCall()
	require.True(t, done)
	assert.Equal(t, "store", rec["name"])
}
