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
		"tiqr_cart": []any{map[string]any{
			"product_option": "1312",
			"quantity":       "2",
			"option_name":    "Kunafa",
		}},
	})

	assert.Equal(t, "buyer@example.com", params["email"])
	assert.Equal(t, "Note", params["notes"])
	assert.Equal(t, "PICKUP_FROM_STORE", params["delivery_mode"])
	assert.JSONEq(t, `[{"product_option":"1312","quantity":"2"}]`, params["items"])
	assert.NotContains(t, params["items"], "option_name")
	assert.NotContains(t, params["items"], "Kunafa")
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

func TestFormatFailedOrderHandoff(t *testing.T) {
	msg := formatFailedOrderHandoff(map[string]any{
		"customer_name":    "Aswin Divakar",
		"address_line_one": "Infopark Rd",
		"address_line_two": "TCS",
		"city":             "Kakkanad",
		"state":            "Keralam",
		"country":          "India",
		"pincode":          "682042",
		"tiqr_cart": []any{
			map[string]any{"option_name": "Kunafa", "product_option": "9", "quantity": "2"},
			map[string]any{"product_option": "10", "quantity": "1"},
		},
	})
	assert.Contains(t, msg, "Placing order for the following items failed.")
	assert.Contains(t, msg, "Kunafa x 2")
	assert.Contains(t, msg, "Option 10 x 1")
	assert.Contains(t, msg, "Address\nAswin Divakar\nInfopark Rd, TCS\nKakkanad, Keralam\nIndia 682042")
	assert.Contains(t, msg, tiqrEcommerceHandoffConnect)
}

func TestPickupOrderParams_UsesStoredDeliveryModeAndBuyerMeta(t *testing.T) {
	params := pickupOrderParams(map[string]any{
		"customer_email":      "buyer@example.com",
		"customer_phone":      "9846435358",
		"delivery_mode":       "DELIVERY_TO_LOCATION",
		"delivery_latitude":   12.97,
		"delivery_longitude":  77.59,
		"tiqr_cart":           []any{map[string]any{"product_option": "1", "quantity": "1"}},
	})
	assert.Equal(t, "DELIVERY_TO_LOCATION", params["delivery_mode"])
	assert.JSONEq(t, `{"latitude":12.97,"longitude":77.59}`, params["buyer_meta_data"])

	var address map[string]string
	require.NoError(t, json.Unmarshal([]byte(params["new_address"]), &address))
	assert.Equal(t, "12.970000", address["latitude"])
	assert.Equal(t, "77.590000", address["longitude"])
}

func TestPickupOrderParams_RoundsDeliveryPinOntoAddress(t *testing.T) {
	params := pickupOrderParams(map[string]any{
		"delivery_mode":      "DELIVERY_TO_LOCATION",
		"delivery_latitude":  11.5545985,
		"delivery_longitude": 75.6326679,
	})
	var address map[string]string
	require.NoError(t, json.Unmarshal([]byte(params["new_address"]), &address))
	assert.Equal(t, "11.554599", address["latitude"])
	assert.Equal(t, "75.632668", address["longitude"])
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

func TestOrderItemsForAPI_MergesAndSkipsInvalidQty(t *testing.T) {
	t.Parallel()
	items := orderItemsForAPI([]any{
		map[string]any{"product_option": "9", "quantity": "2"},
		map[string]any{"product_option": "9", "quantity": "3"},
		map[string]any{"product_option": "8", "quantity": "0"},
		map[string]any{"product_option": "7", "quantity": "1"},
	})
	require.Len(t, items, 2)
	assert.Equal(t, "9", items[0]["product_option"])
	assert.Equal(t, "5", items[0]["quantity"])
	assert.Equal(t, "7", items[1]["product_option"])
	assert.Equal(t, "1", items[1]["quantity"])
}

func TestTiqrCartUpsertAndUnitCount(t *testing.T) {
	t.Parallel()
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"option_name": "Kunafa",
		"options": []any{
			map[string]any{"id": "9", "name": "Kunafa", "price": 40.0},
		},
	}}
	c := &Conv{chat: &chatNodeCtx{session: session}}

	upsertCartItem(c, "9", "2")
	assert.Equal(t, 1, cartLen(c))
	assert.Equal(t, 2, cartUnitCount(c))
	assert.Equal(t, float64(2), session.SessionData["cart_count"])

	upsertCartItem(c, "9", "3")
	assert.Equal(t, 1, cartLen(c))
	assert.Equal(t, 5, cartUnitCount(c))

	upsertCartItem(c, "", "1") // empty option ignored
	assert.Equal(t, 1, cartLen(c))
	upsertCartItem(c, "8", "0") // qty 0 ignored
	assert.Equal(t, 1, cartLen(c))

	summary := formatTiqrCartSummary(c)
	assert.Contains(t, summary, "*Kunafa* x5")
	assert.Contains(t, summary, "₹40.00 each")
	assert.Contains(t, summary, "*Subtotal:*")

	ok, name := removeTiqrCartLine(c, "9")
	require.True(t, ok)
	assert.Equal(t, "Kunafa", name)
	assert.Equal(t, 0, cartLen(c))
	assert.Equal(t, 0, cartUnitCount(c))
}

func TestTiqrCartSetQty(t *testing.T) {
	t.Parallel()
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"tiqr_cart": []any{
			map[string]any{"product_option": "9", "quantity": "2", "option_name": "Kunafa", "price": 40.0},
		},
	}}
	c := &Conv{chat: &chatNodeCtx{session: session}}
	require.True(t, setTiqrCartLineQty(c, "9", 4))
	assert.Equal(t, 4, cartUnitCount(c))
	assert.False(t, setTiqrCartLineQty(c, "9", 0))
	assert.Equal(t, 4, cartUnitCount(c))
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

func useCodedRecover(t *testing.T, recover func(codedRecoverContext) (codedRecoverResult, error)) {
	t.Helper()
	prev := recoverCodedFailure
	recoverCodedFailure = func(_ *App, _ *models.ChatbotSession, ctx codedRecoverContext) (codedRecoverResult, error) {
		if recover == nil {
			t.Fatal("recover was called")
		}
		return recover(ctx)
	}
	t.Cleanup(func() { recoverCodedFailure = prev })
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
	return newStoreServerWith(t, products, counts, map[string]any{"id": 42, "name": "Demo"})
}

func newStoreServerWith(t *testing.T, products []any, counts *storeCounts, store map[string]any) *httptest.Server {
	t.Helper()
	if store == nil {
		store = map[string]any{"id": 42, "name": "Demo"}
	}
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
			_ = json.NewEncoder(w).Encode(store)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func startEcommerce(t *testing.T, products []any, counts *storeCounts) (*App, *models.WhatsAppAccount, *models.Contact, *models.ChatbotSession) {
	t.Helper()
	return startEcommerceWithStore(t, products, counts, map[string]any{"id": 42, "name": "Demo"}, nil)
}

func startEcommerceWithStore(
	t *testing.T,
	products []any,
	counts *storeCounts,
	store map[string]any,
	extraAI *models.AIConfig,
) (*App, *models.WhatsAppAccount, *models.Contact, *models.ChatbotSession) {
	t.Helper()
	srv := newStoreServerWith(t, products, counts, store)
	useStoreREST(t, srv)
	app, org, account, contact, session := newGraphTestFixtures(t)
	ai := models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	}
	if extraAI != nil {
		if extraAI.CommerceEnabled {
			ai.CommerceEnabled = true
		}
		if extraAI.CommerceMCPURL != "" {
			ai.CommerceMCPURL = extraAI.CommerceMCPURL
		}
		if extraAI.CommerceMCPAPIKey != "" {
			ai.CommerceMCPAPIKey = extraAI.CommerceMCPAPIKey
		}
	}
	createChatbotSettings(t, app, org.ID, account.Name, ai)
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

func TestSingleProductCTA_Config(t *testing.T) {
	prompt := singleProductCTA()
	assert.Contains(t, prompt.Body, "{{products[0].name}}")
	assert.Contains(t, prompt.Body, "{{products[0].description}}")
	assert.Equal(t, "{{products[0].images[0].original_url}}", prompt.HeaderImage)
	assert.Equal(t, "Add to cart", prompt.Title)
	assert.Equal(t, "product_selected", prompt.StoreAs)
	assert.Equal(t, "{{name}} (₹{{min_price}})", prompt.BodyField)
	assert.Equal(t, tiqrEcommerceFallbackMedia, prompt.FallbackMedia)

	c := &Conv{chat: &chatNodeCtx{session: &models.ChatbotSession{
		SessionData: models.JSONB{customerLanguageKey: "en"},
	}}}
	cfg := c.imageButtonConfig(prompt)
	assert.Equal(t, "reply", cfg["mode"])
	assert.Equal(t, "dynamic", cfg["source"])
	assert.Equal(t, "products", cfg["items_var"])
	assert.Equal(t, prompt.HeaderImage, cfg["header_image"])
	assert.Equal(t, tiqrEcommerceFallbackMedia, cfg["fallback_media_url"])
	assert.Equal(t, "Add to cart", cfg["title_field"])
	assert.Equal(t, prompt.BodyField, cfg["body_field"])
}

func TestTiqrEcommerce_SingleProductImageCTA(t *testing.T) {
	product := []any{
		map[string]any{
			"id":          "101",
			"name":        "Kunafa",
			"min_price":   "120",
			"description": "Crispy shredded pastry",
			"images": []any{
				map[string]any{"original_url": "https://cdn.example.com/kunafa.jpg"},
			},
			"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
		},
	}
	app, account, contact, session := startEcommerce(t, product, nil)
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.Equal(t, "product", session.CurrentStep)

	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Kunafa")
	assert.Contains(t, blob, "120")
	assert.Contains(t, blob, "Crispy shredded pastry")
	assert.Contains(t, blob, "Add to cart")
	assert.Contains(t, blob, "https://cdn.example.com/kunafa.jpg")
	assert.NotContains(t, blob, `"type":"carousel"`)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add to cart", "101", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "quantity", session.CurrentStep)
	assert.Equal(t, "101", session.SessionData["product_id"])
	assert.Equal(t, "9", session.SessionData["option_id"])
	_ = contact
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
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "business information")
	assert.Contains(t, blob, "connecting you with a team member")
	assert.NotContains(t, strings.ToLower(blob), "fetch the store")
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
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
	assert.Equal(t, float64(2), session.SessionData["cart_count"])

	cart, ok := session.SessionData["tiqr_cart"].([]any)
	require.True(t, ok)
	require.Len(t, cart, 1)
	item, ok := cart[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "9", item["product_option"])
	assert.Equal(t, "2", item["quantity"])
	assert.Contains(t, asString(session.SessionData["cart_summary"]), "Your cart")
	assert.Contains(t, outgoingBlob(t, app, session), "Edit cart")
	assert.Contains(t, outgoingBlob(t, app, session), "Checkout")

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

func TestValidateCodedIntent_AnswerRoute(t *testing.T) {
	ctx := codedIntentContext{Pattern: `^[0-9]+$`}
	got, ok := validateCodedIntent(codedIntentResult{Route: codedRouteAnswer, Answer: "2", Confidence: 0.9}, ctx)
	assert.True(t, ok)
	assert.Equal(t, "2", got.Answer)

	_, ok = validateCodedIntent(codedIntentResult{Route: codedRouteAnswer, Answer: "2", ChoiceID: "x", Confidence: 0.9}, ctx)
	assert.False(t, ok)
	_, ok = validateCodedIntent(codedIntentResult{Route: codedRouteAnswer, Answer: "two", Confidence: 0.9}, ctx)
	assert.False(t, ok)
	_, ok = validateCodedIntent(codedIntentResult{Route: codedRouteAnswer, Answer: "2", Confidence: 0.9}, codedIntentContext{})
	assert.False(t, ok)
}

func TestBuildIntentPrompt_IncludesStepContext(t *testing.T) {
	prompt := buildIntentPrompt("Kunafaa", codedIntentContext{
		Question:  "Here is what's available in *Sweets*.",
		Doing:     "The customer is choosing one product from the carousel.",
		Expect:    "A product from the cards.",
		ChoiceIDs: map[string]string{"101": "Kunafa (₹40) — Add to cart"},
	})
	assert.Contains(t, prompt, "Here is what's available in *Sweets*.")
	assert.Contains(t, prompt, "The customer is choosing one product from the carousel.")
	assert.Contains(t, prompt, "A product from the cards.")
	assert.Contains(t, prompt, "Kunafa (₹40) — Add to cart")
	assert.Contains(t, prompt, "route answer:")
	assert.Contains(t, prompt, "At most 200 words")
	assert.Contains(t, prompt, `"reasoning":""`)
}

func TestLimitWords_CapsAtOneHundred(t *testing.T) {
	words := make([]string, 120)
	for i := range words {
		words[i] = "word"
	}
	got := limitWords(strings.Join(words, " "), 200)
	assert.Equal(t, 100, len(strings.Fields(got)))
	assert.Equal(t, "keep this", limitWords("  keep   this  ", 100))
}

func TestTiqrEcommerce_QuantityWordAccepted(t *testing.T) {
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{
			Language:   "en",
			Route:      codedRouteAnswer,
			Answer:     "2",
			Confidence: 0.95,
		}, nil
	}, nil)
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerce(t, products, nil)
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.Equal(t, "quantity", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Two", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "next", session.CurrentStep)
	assert.Equal(t, "2", session.SessionData["quantity"])
	assert.NotContains(t, outgoingBlob(t, app, session), "Send a whole number.")
	cart, ok := session.SessionData["tiqr_cart"].([]any)
	require.True(t, ok)
	require.Len(t, cart, 1)
	item, ok := cart[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2", item["quantity"])
}

func TestTiqrEcommerce_CollectionTypoSelectsChoice(t *testing.T) {
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{
			Language:   "en",
			Route:      codedRouteChoice,
			ChoiceID:   "57",
			Confidence: 0.9,
		}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.Equal(t, "collection", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweetss", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "57", session.SessionData["collection_id"])
	assert.Equal(t, "product", session.CurrentStep)
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

func TestValidateCodedIntent_CheckoutRoute(t *testing.T) {
	got, ok := validateCodedIntent(codedIntentResult{Route: codedRouteCheckout, Confidence: 0.9}, codedIntentContext{})
	assert.True(t, ok)
	assert.Equal(t, codedRouteCheckout, got.Route)
	_, ok = validateCodedIntent(codedIntentResult{Route: codedRouteCheckout, ChoiceID: "x", Confidence: 0.9}, codedIntentContext{})
	assert.False(t, ok)
}

func TestSanitizeRecoverHint_DropsURLsAndKeepsFields(t *testing.T) {
	hint := sanitizeRecoverHint(`{"email":["This field is required."],"detail":"see https://example.com/docs?token=abc"}`)
	assert.Contains(t, hint, "email")
	assert.NotContains(t, hint, "https://")
	assert.NotContains(t, hint, "token=")
}

func TestValidateCodedRecover_MissingField(t *testing.T) {
	ctx := codedRecoverContext{Kind: "create"}
	got, ok := validateCodedRecover(codedRecoverResult{
		Kind: codedRecoverMissingField, Field: "email", Message: "Please share your email.", Confidence: 0.9,
	}, ctx)
	assert.True(t, ok)
	assert.Equal(t, "customer_email", got.Field)

	_, ok = validateCodedRecover(codedRecoverResult{
		Kind: codedRecoverMissingField, Field: "email", Message: "Call https://api.example/x", Confidence: 0.9,
	}, ctx)
	assert.False(t, ok)

	_, ok = validateCodedRecover(codedRecoverResult{
		Kind: codedRecoverMissingField, Field: "email", Message: "Please share your email.", Confidence: 0.9,
	}, codedRecoverContext{Kind: "fetch"})
	assert.False(t, ok)
}

func TestCanonicalFieldsFromHint_OrdersEveryMissingField(t *testing.T) {
	hint := sanitizeRecoverHint(`{"pincode":["required"],"new_address":{"city":["required"]},"email":["required"],"phone_number":["required"]}`)
	assert.Equal(t, []string{
		"customer_phone",
		"customer_email",
		"city",
		"pincode",
	}, canonicalFieldsFromHint(hint))
}

func TestExpandRecoverAsks_AsksEveryHintField(t *testing.T) {
	hint := sanitizeRecoverHint(`{"pincode":["required"],"email":["required"]}`)
	got := expandRecoverAsks(codedRecoverResult{
		Kind:    codedRecoverMissingField,
		Field:   "customer_email",
		Message: "Could you reply with your email address?",
		Fields: []codedRecoverAsk{{
			Field: "customer_email", Message: "Could you reply with your email address?",
		}},
	}, hint)
	require.Equal(t, codedRecoverMissingField, got.Kind)
	require.Len(t, got.Fields, 2)
	assert.Equal(t, "customer_email", got.Fields[0].Field)
	assert.Equal(t, "Could you reply with your email address?", got.Fields[0].Message)
	assert.Equal(t, "pincode", got.Fields[1].Field)
	assert.Equal(t, "Could you share your pincode?", got.Fields[1].Message)
}

func TestParseCodedRecover_AllFields(t *testing.T) {
	got, err := parseCodedRecover(`{"kind":"missing_field","field":"pincode","fields":[{"field":"pincode","message":"What is your pincode?"},{"field":"email","message":"What is your email?"}],"message":"I need a couple of details.","confidence":0.9,"reasoning":"both missing"}`)
	require.NoError(t, err)
	valid, ok := validateCodedRecover(got, codedRecoverContext{Kind: "create"})
	require.True(t, ok)
	require.Len(t, valid.Fields, 2)
	assert.Equal(t, "customer_email", valid.Fields[0].Field)
	assert.Equal(t, "What is your email?", valid.Fields[0].Message)
	assert.Equal(t, "pincode", valid.Fields[1].Field)
	assert.Equal(t, "What is your pincode?", valid.Fields[1].Message)

	fromNames, err := parseCodedRecover(`{"kind":"missing_field","fields":["phone_number","city"],"message":"Please share the missing details.","confidence":0.8}`)
	require.NoError(t, err)
	valid, ok = validateCodedRecover(fromNames, codedRecoverContext{Kind: "create"})
	require.True(t, ok)
	assert.Equal(t, []string{"customer_phone", "city"}, recoverAskFields(valid.Fields))
}

func TestBuildRecoverPrompt_NoSecrets(t *testing.T) {
	prompt := buildRecoverPrompt(codedRecoverContext{
		Kind: "create", Operation: "create_order", Resource: "order", Status: 400,
		Hint: sanitizeRecoverHint(`{"email":["required"]}`),
	})
	assert.Contains(t, prompt, "validation fields")
	assert.NotContains(t, prompt, "https://")
}

func TestTiqrEcommerce_CheckoutEmptyCartReturnsToCollections(t *testing.T) {
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{Language: "en", Route: codedRouteCheckout, Confidence: 0.95}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.Equal(t, "collection", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "checkout please", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "cart is empty")
	assert.Equal(t, models.SessionStatusActive, session.Status)
}

func TestTiqrEcommerce_CheckoutWithCartOpensDetails(t *testing.T) {
	useCodedIntent(t, func(string, codedIntentContext) (codedIntentResult, error) {
		return codedIntentResult{Language: "en", Route: codedRouteCheckout, Confidence: 0.95}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)

	session.SessionData["tiqr_cart"] = []any{map[string]any{"product_option": "9", "quantity": "1"}}
	session.SessionData["cart_count"] = float64(1)
	require.NoError(t, app.DB.Save(session).Error)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "I want to checkout", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "cart_review_1", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "Confirm items")
	assert.Contains(t, outgoingBlob(t, app, session), "Edit cart")
	assert.NotContains(t, outgoingBlob(t, app, session), "cart is empty")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Confirm items", tiqrConfirmItems, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "details", session.CurrentStep)
}

func TestTiqrEcommerce_ListProductsFailureRecovers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			http.Error(w, `{"detail":"boom https://internal/api"}`, http.StatusBadGateway)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo"})
		}
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)
	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)

	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "unable to fetch those products")
	assert.Contains(t, blob, "connecting you with a team member")
	assert.NotContains(t, blob, "https://internal")
	assert.NotContains(t, blob, "ticker api")
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_CreateOrderMissingEmailRetry(t *testing.T) {
	orderCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{
						"id": "101", "name": "Kunafa", "min_price": "40",
						"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
					},
				},
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/order/"):
			orderCalls++
			if orderCalls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"email":["This field is required."]}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ord-1", "display_uid": "TQ-1"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo"})
		}
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)
	useCodedRecover(t, func(ctx codedRecoverContext) (codedRecoverResult, error) {
		assert.Equal(t, "create", ctx.Kind)
		return codedRecoverResult{
			Kind: codedRecoverMissingField, Field: "customer_email",
			Message: "Could you reply with your email address?", Confidence: 0.95,
		}, nil
	})

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Checkout", tiqrCheckout, nil))
	reloadSession(t, app, session)
	require.Equal(t, "details", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "", "", map[string]any{
		"customer_name":    "Ada",
		"customer_phone":   "910000000000",
		"address_line_one": "1 Main",
		"city":             "Kochi",
		"state":            "KL",
		"country":          "India",
		"pincode":          "682001",
	}))
	reloadSession(t, app, session)
	assert.Contains(t, outgoingBlob(t, app, session), "Could you reply with your email address?")
	assert.Equal(t, 1, orderCalls)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "ada@example.com", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, 2, orderCalls)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "order is confirmed")
	assert.NotContains(t, blob, "ticker api")
	assert.NotContains(t, blob, "This field is required")
}

func TestTiqrEcommerce_CreateOrderAsksEveryMissingFieldBeforeRetry(t *testing.T) {
	orderCalls := 0
	recoverCalls := 0
	var placed map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{
						"id": "101", "name": "Kunafa", "min_price": "40",
						"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
					},
				},
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/order/"):
			orderCalls++
			if orderCalls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"pincode":["This field is required."],"email":["This field is required."]}`))
				return
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&placed))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ord-1", "display_uid": "TQ-1"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo"})
		}
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)
	useCodedRecover(t, func(ctx codedRecoverContext) (codedRecoverResult, error) {
		recoverCalls++
		assert.Equal(t, "create", ctx.Kind)
		assert.Contains(t, ctx.Hint, "email")
		assert.Contains(t, ctx.Hint, "pincode")
		return codedRecoverResult{
			Kind: codedRecoverMissingField, Field: "email",
			Message: "Could you reply with your email address?", Confidence: 0.95,
		}, nil
	})

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Checkout", tiqrCheckout, nil))
	reloadSession(t, app, session)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "", "", map[string]any{
		"customer_name":    "Ada",
		"customer_phone":   "910000000000",
		"address_line_one": "1 Main",
		"city":             "Kochi",
		"state":            "KL",
		"country":          "India",
	}))
	reloadSession(t, app, session)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Could you reply with your email address?")
	assert.NotContains(t, blob, "Could you share your pincode?")
	assert.Equal(t, 1, orderCalls)
	assert.Equal(t, 1, recoverCalls)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "ada@example.com", "", nil))
	reloadSession(t, app, session)
	blob = outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Could you share your pincode?")
	assert.Equal(t, "order_fix_pincode", session.CurrentStep)
	assert.Equal(t, 1, orderCalls)
	assert.Equal(t, 1, recoverCalls)
	assert.Equal(t, models.SessionStatusActive, session.Status)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "682020", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, 2, orderCalls)
	assert.Equal(t, 1, recoverCalls)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Equal(t, "ada@example.com", placed["email"])
	address, ok := placed["new_address"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "682020", address["pincode"])
	blob = outgoingBlob(t, app, session)
	assert.Contains(t, blob, "order is confirmed")
	assert.NotContains(t, blob, "ticker api")
	assert.NotContains(t, blob, "This field is required")
}

func TestTiqrEcommerce_CreateOrderRetryExhausted(t *testing.T) {
	prev := codedIntentSettings.OrderRetries
	codedIntentSettings.OrderRetries = 0
	t.Cleanup(func() { codedIntentSettings.OrderRetries = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count":   1,
				"results": []any{map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"}},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{map[string]any{
					"id": "101", "name": "Kunafa", "min_price": "40",
					"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
				}},
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/order/"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"email":["This field is required."]}`))
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo"})
		}
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)
	useCodedRecover(t, func(codedRecoverContext) (codedRecoverResult, error) {
		return codedRecoverResult{
			Kind: codedRecoverGiveUp, Message: "We could not place your order just now.", Confidence: 0.9,
		}, nil
	})

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedFlowByKey(tiqrEcommerceKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Checkout", tiqrCheckout, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "", "", map[string]any{
		"customer_name": "Ada", "customer_phone": "910000000000",
		"address_line_one": "1 Main", "city": "Kochi", "state": "KL", "country": "India", "pincode": "682001",
	}))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Placing order for the following items failed.")
	assert.Contains(t, blob, "Regular x 1")
	assert.Contains(t, blob, "Address")
	assert.Contains(t, blob, "Ada")
	assert.Contains(t, blob, "1 Main")
	assert.Contains(t, blob, "Kochi")
	assert.Contains(t, blob, "connecting you with a team member")
	assert.NotContains(t, blob, "Thank you for shopping")
	assert.NotContains(t, blob, "ticker api")
	assert.NotContains(t, blob, "https://")
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_PickupOnlyProceedsToCollections(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerceWithStore(t, products, nil, map[string]any{
		"id":             42,
		"name":           "Demo",
		"delivery_modes": []any{"PICKUP_FROM_STORE"},
	}, nil)
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "fulfillment_pickup_only", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "store pickup only")
	assert.Contains(t, blob, "Proceed")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Proceed", tiqrProceed, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, "PICKUP_FROM_STORE", session.SessionData["delivery_mode"])
}

func TestTiqrEcommerce_BothModesPickupSkipsLocation(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerceWithStore(t, products, nil, map[string]any{
		"id":   42,
		"name": "Demo",
		"delivery_modes": []any{
			"PICKUP_FROM_STORE",
			"DELIVERY_TO_LOCATION",
		},
	}, nil)
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "fulfillment_mode", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Store pickup", tiqrPickupMode, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, "PICKUP_FROM_STORE", session.SessionData["delivery_mode"])
}

func TestTiqrEcommerce_DeliveryInRangeContinuesToCollections(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerceWithStore(t, products, nil, map[string]any{
		"id":                       42,
		"name":                     "Demo",
		"address":                  "Vadakara",
		"latitude":                 "11.2",
		"longitude":                "75.8",
		"location_based_delivery":  true,
		"free_delivery_radius":     8,
		"delivery_radius":          16,
		"delivery_modes":           []any{"DELIVERY_TO_LOCATION"},
	}, nil)
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "delivery_location_1", session.CurrentStep)

	pin := `{"latitude":11.2,"longitude":75.8}`
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, pin, "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, "DELIVERY_TO_LOCATION", session.SessionData["delivery_mode"])
	assert.Equal(t, 11.2, session.SessionData["delivery_latitude"])
	assert.Equal(t, 75.8, session.SessionData["delivery_longitude"])
	assert.Equal(t, "free", session.SessionData["delivery_zone"])
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "delivery is free")
	assert.Contains(t, blob, "8 km")
	assert.Contains(t, blob, "16 km")
}

func TestTiqrEcommerce_DeliveryOutOfRangeOffersPickup(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerceWithStore(t, products, nil, map[string]any{
		"id":                      42,
		"name":                    "Demo",
		"address":                 "Vadakara, Kozhikode",
		"latitude":                "11.55",
		"longitude":               "75.63",
		"location_based_delivery": true,
		"free_delivery_radius":    8,
		"delivery_radius":         16,
		"delivery_modes": []any{
			"PICKUP_FROM_STORE",
			"DELIVERY_TO_LOCATION",
		},
	}, nil)
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Delivery", tiqrDeliveryMode, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "delivery_location_1", session.CurrentStep)

	pin := `{"latitude":1.0,"longitude":1.0}`
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, pin, "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "delivery_fallback_1", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "unable to deliver")
	assert.Contains(t, blob, "Vadakara")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Store pickup", tiqrPickupMode, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, "PICKUP_FROM_STORE", session.SessionData["delivery_mode"])
}

func TestTiqrEcommerce_DeliveryOnlyOutOfRangeAsksAgain(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerceWithStore(t, products, nil, map[string]any{
		"id":                      42,
		"name":                    "Demo",
		"latitude":                "11.55",
		"longitude":               "75.63",
		"location_based_delivery": true,
		"delivery_modes":          []any{"DELIVERY_TO_LOCATION"},
		"delivery_radius":         10,
	}, nil)
	flow := codedFlowByKey(tiqrEcommerceKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrBuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, `{"latitude":1,"longitude":2}`, "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "delivery_location_2", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "within our delivery radius")
	assert.NotContains(t, blob, "Store Pickup as your preferred option")
	assert.NotContains(t, blob, `"id":"pickup"`)
}
