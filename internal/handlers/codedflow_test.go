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

func useCodedLanguage(t *testing.T, detect func(string) string, translate func(string) (string, error)) {
	t.Helper()
	prevDetect, prevTranslate := detectCodedLanguage, translateCodedLine
	detectCodedLanguage = func(_ *App, _ *models.ChatbotSession, text string) (string, error) {
		if detect == nil {
			return "en", nil
		}
		return detect(text), nil
	}
	translateCodedLine = func(_ *App, _ *models.ChatbotSession, _, text string) (string, error) {
		if translate == nil {
			t.Errorf("translated %q", text)
			return text, nil
		}
		return translate(text)
	}
	t.Cleanup(func() {
		detectCodedLanguage = prevDetect
		translateCodedLine = prevTranslate
	})
}

func useCodedDiversion(t *testing.T, fn func(string) (codedDiversion, error)) {
	t.Helper()
	prev := answerCodedDiversion
	answerCodedDiversion = func(_ *App, _ *models.WhatsAppAccount, _ *models.ChatbotSession, _, _, userText string) (codedDiversion, error) {
		if fn == nil {
			t.Fatal("AI diversion was called")
		}
		return fn(userText)
	}
	t.Cleanup(func() { answerCodedDiversion = prev })
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
				"count": 1,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				},
			})
		case strings.Contains(r.URL.Path, "/product/"):
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
	useCodedDiversion(t, func(string) (codedDiversion, error) {
		return codedDiversion{Handled: true, Reply: "Send a whole number."}, nil
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

func TestTiqrEcommerce_HandledDiversionStays(t *testing.T) {
	useCodedDiversion(t, func(string) (codedDiversion, error) {
		return codedDiversion{Handled: true, Reply: "We sell sweets. Please choose a collection."}, nil
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
	assert.Contains(t, outgoingBlob(t, app, session), "We sell sweets.")
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Zero(t, transfers)
}

func TestTiqrEcommerce_UnhandledDiversionTransfers(t *testing.T) {
	useCodedDiversion(t, func(string) (codedDiversion, error) {
		return codedDiversion{Handled: false}, nil
	})
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "I want a refund", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Nil(t, session.SessionData[codedFlowDataKey])
	assert.Nil(t, session.SessionData["collection_id"])
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_AIErrorTransfers(t *testing.T) {
	useCodedDiversion(t, func(string) (codedDiversion, error) {
		return codedDiversion{}, assert.AnError
	})
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
	useCodedDiversion(t, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Talk to agent", tiqrTalkToAgent, nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Contains(t, outgoingBlob(t, app, session), "connecting you with a team member")
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
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

func TestTiqrEcommerce_TranslatesAndCaches(t *testing.T) {
	var welcomeCalls int
	useCodedLanguage(t,
		func(text string) string {
			if strings.Contains(strings.ToLower(text), "hola") {
				return "es"
			}
			return "en"
		},
		func(text string) (string, error) {
			if strings.Contains(text, "Welcome to") {
				welcomeCalls++
			}
			return "ES:" + text, nil
		},
	)
	app, org, account, contact, session := newGraphTestFixtures(t)
	srv := newStoreServer(t, twoProducts(nil), nil)
	useStoreREST(t, srv)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "hola shop", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "es", session.SessionData[customerLanguageKey])
	assert.Contains(t, outgoingBlob(t, app, session), "ES:Welcome to Demo")
	assert.Equal(t, 1, welcomeCalls)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	assert.Equal(t, 1, welcomeCalls)
	assert.Contains(t, outgoingBlob(t, app, session), "ES:Choose a collection")
}

func TestTiqrEcommerce_EnglishSkipsTranslation(t *testing.T) {
	useCodedLanguage(t, func(string) string { return "en" }, nil)
	app, _, _, session := startEcommerce(t, twoProducts(nil), nil)
	assert.Equal(t, "en", session.SessionData[customerLanguageKey])
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Welcome to Demo")
	assert.NotContains(t, blob, "ES:")
}
