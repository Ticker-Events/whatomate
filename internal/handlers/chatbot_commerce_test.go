package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/logf"
)

func testApp() *App {
	return &App{
		Log: logf.New(logf.Opts{Level: logf.ErrorLevel}),
	}
}

type stubCommerceBackend struct {
	searchFn        func(ctx context.Context, storeID, search string, limit int) ([]ticker.ProductSummary, error)
	getFn           func(ctx context.Context, productID string) (map[string]any, error)
	storeFn         func(ctx context.Context, storeID string) (map[string]any, error)
	categoriesFn    func(ctx context.Context, storeID string) ([]map[string]any, error)
	orderFn         func(ctx context.Context, orderUUID string) (map[string]any, error)
	lookupStatusFn  func(ctx context.Context, storeID, phoneNumber, orderID string) (map[string]any, error)
	createFn        func(ctx context.Context, body ticker.CreateOrderRequest) (map[string]any, error)
	checkDeliveryFn func(ctx context.Context, storeID string, latitude, longitude float64) (map[string]any, error)
	lastCreate      ticker.CreateOrderRequest
}

func (s *stubCommerceBackend) SearchProducts(ctx context.Context, storeID, search string, limit int) ([]ticker.ProductSummary, error) {
	if s.searchFn != nil {
		return s.searchFn(ctx, storeID, search, limit)
	}
	return nil, fmt.Errorf("search not stubbed")
}

func (s *stubCommerceBackend) GetProduct(ctx context.Context, productID string) (map[string]any, error) {
	if s.getFn != nil {
		return s.getFn(ctx, productID)
	}
	return nil, fmt.Errorf("get_product not stubbed")
}

func (s *stubCommerceBackend) GetStore(ctx context.Context, storeID string) (map[string]any, error) {
	if s.storeFn != nil {
		return s.storeFn(ctx, storeID)
	}
	return nil, fmt.Errorf("get_store not stubbed")
}

func (s *stubCommerceBackend) ListCategories(ctx context.Context, storeID string) ([]map[string]any, error) {
	if s.categoriesFn != nil {
		return s.categoriesFn(ctx, storeID)
	}
	return nil, fmt.Errorf("list_categories not stubbed")
}

func (s *stubCommerceBackend) GetOrder(ctx context.Context, orderUUID string) (map[string]any, error) {
	if s.orderFn != nil {
		return s.orderFn(ctx, orderUUID)
	}
	return nil, fmt.Errorf("get_order not stubbed")
}

func (s *stubCommerceBackend) LookupOrderStatus(ctx context.Context, storeID, phoneNumber, orderID string) (map[string]any, error) {
	if s.lookupStatusFn != nil {
		return s.lookupStatusFn(ctx, storeID, phoneNumber, orderID)
	}
	return nil, fmt.Errorf("lookup_order_status not stubbed")
}

func (s *stubCommerceBackend) CreateOrder(ctx context.Context, body ticker.CreateOrderRequest) (map[string]any, error) {
	s.lastCreate = body
	if s.createFn != nil {
		return s.createFn(ctx, body)
	}
	return nil, fmt.Errorf("create_order not stubbed")
}

func (s *stubCommerceBackend) CheckDeliveryEligibility(ctx context.Context, storeID string, latitude, longitude float64) (map[string]any, error) {
	if s.checkDeliveryFn != nil {
		return s.checkDeliveryFn(ctx, storeID, latitude, longitude)
	}
	return map[string]any{
		"deliverable":        true,
		"zone":               "unconfigured",
		"shipping_fee_paise": 0,
		"message":            "",
	}, nil
}

func TestCommerceConfigured(t *testing.T) {
	assert.False(t, commerceConfigured(models.AIConfig{CommerceEnabled: true}))
	assert.False(t, commerceConfigured(models.AIConfig{
		CommerceEnabled: true,
		CommerceMCPURL:  "http://127.0.0.1:8100/mcp",
	}))
	assert.True(t, commerceConfigured(models.AIConfig{
		CommerceEnabled: true,
		CommerceMCPURL:  "http://127.0.0.1:8100/mcp",
		CommerceStoreID: "42",
	}))
}

func TestCreateOrderRequiresConfirmation(t *testing.T) {
	app := testApp()
	rt := &commerceRuntime{
		Client:  &stubCommerceBackend{},
		StoreID: "1",
	}
	out := app.executeCommerceTool(rt, "create_order", `{"confirmed":false,"items":[{"product_option":1,"quantity":1}],"email":"a@b.com","delivery_mode":"PICKUP_FROM_STORE"}`)
	assert.Contains(t, out, "confirmed=true")
}

func TestCreateOrderInjectsStoreID(t *testing.T) {
	stub := &stubCommerceBackend{
		createFn: func(ctx context.Context, body ticker.CreateOrderRequest) (map[string]any, error) {
			return map[string]any{
				"uuid":        "ord-uuid-1",
				"display_uid": "ST-260710-000001",
				"status":      "PENDING_PAYMENT",
				"amount":      10000, // paise → ₹100.00
				"payment": map[string]any{
					"status": "initiated",
					"meta_data": map[string]any{
						"url_to_redirect": "https://pay.example/x",
					},
				},
			}, nil
		},
	}
	app := testApp()
	rt := &commerceRuntime{
		Client:      stub,
		StoreID:     "99",
		PhoneNumber: "+919999999999",
	}
	out := app.executeCommerceTool(rt, "create_order", `{
		"confirmed": true,
		"items": [{"product_option": 10, "quantity": 2}],
		"email": "buyer@example.com",
		"delivery_mode": "PICKUP_FROM_STORE"
	}`)
	require.NotContains(t, out, `"error"`)
	assert.Equal(t, 99, stub.lastCreate.Store)
	assert.Equal(t, "buyer@example.com", stub.lastCreate.Email)
	assert.Equal(t, "+919999999999", stub.lastCreate.PhoneNumber)
	assert.Contains(t, out, "ST-260710-000001")
	assert.Contains(t, out, `"amount":100`)
	assert.Contains(t, out, "https://pay.example/x")
}

func TestSearchProductsTool(t *testing.T) {
	stub := &stubCommerceBackend{
		searchFn: func(ctx context.Context, storeID, search string, limit int) ([]ticker.ProductSummary, error) {
			assert.Equal(t, "7", storeID)
			assert.Equal(t, "coffee", search)
			assert.Equal(t, 10, limit)
			price := 1.99
			return []ticker.ProductSummary{
				{
					ID:          1,
					Name:        "Coffee Beans",
					Description: "Arabica",
					MinPrice:    price,
					Options: []ticker.ProductOption{
						{ID: 11, Name: "250g", Price: price, StockStatus: "in_stock"},
					},
				},
				{
					ID:       2,
					Name:     "Empty Product",
					MinPrice: 5,
				},
			}, nil
		},
	}
	app := testApp()
	rt := &commerceRuntime{
		Client:  stub,
		StoreID: "7",
	}
	out := app.executeCommerceTool(rt, "search_products", `{"query":"coffee","limit":10}`)
	require.NotContains(t, out, `"error"`)
	assert.Contains(t, out, `"products"`)
	assert.Contains(t, out, "Coffee Beans")
	assert.NotContains(t, out, "Empty Product")
	assert.Contains(t, out, `"id":11`)
	assert.Contains(t, out, `"price":1.99`)
	assert.Contains(t, out, `"currency":"INR"`)
}

func TestGoogleFunctionResponseWrapsArray(t *testing.T) {
	m := googleFunctionResponse(`[{"id":1}]`)
	assert.Contains(t, m, "result")
	m2 := googleFunctionResponse(`{"products":[]}`)
	_, ok := m2["products"]
	assert.True(t, ok)
	m3 := googleFunctionResponse(`{"error":"boom"}`)
	assert.Equal(t, "boom", m3["error"])
}

func TestLooksLikeCommerceCatalogQuery(t *testing.T) {
	assert.True(t, looksLikeCommerceCatalogQuery("what all products do you have?"))
	assert.True(t, looksLikeCommerceCatalogQuery("Show me the price of tea"))
	assert.False(t, looksLikeCommerceCatalogQuery("Hello"))
	assert.False(t, looksLikeCommerceCatalogQuery("thanks"))
}

func TestEffectiveAIHistoryLimitCommerceFloor(t *testing.T) {
	assert.Equal(t, 20, effectiveAIHistoryLimit(&models.ChatbotSettings{
		AI: models.AIConfig{
			HistoryLimit:    4,
			CommerceEnabled: true,
			CommerceMCPURL:  "http://127.0.0.1:8100/mcp",
			CommerceStoreID: "21",
		},
	}))
	assert.Equal(t, 4, effectiveAIHistoryLimit(&models.ChatbotSettings{
		AI: models.AIConfig{HistoryLimit: 4},
	}))
	assert.Equal(t, 30, effectiveAIHistoryLimit(&models.ChatbotSettings{
		AI: models.AIConfig{
			HistoryLimit:    30,
			CommerceEnabled: true,
			CommerceMCPURL:  "http://127.0.0.1:8100/mcp",
			CommerceStoreID: "21",
		},
	}))
}

func TestAppendGeminiTurnMergesSameRole(t *testing.T) {
	var contents []map[string]any
	contents = appendGeminiTurn(contents, "user", "hi")
	contents = appendGeminiTurn(contents, "user", "again")
	require.Len(t, contents, 1)
	assert.Equal(t, "user", contents[0]["role"])
	assert.Contains(t, geminiPartsText(contents[0]["parts"]), "hi")
	assert.Contains(t, geminiPartsText(contents[0]["parts"]), "again")

	contents = appendGeminiTurn(contents, "model", "hello")
	require.Len(t, contents, 2)
	assert.Equal(t, "model", contents[1]["role"])
}

func TestUnknownTool(t *testing.T) {
	app := testApp()
	rt := &commerceRuntime{
		Client:  &stubCommerceBackend{},
		StoreID: "1",
	}
	out := app.executeCommerceTool(rt, "delete_everything", `{}`)
	assert.Contains(t, out, "unknown tool")
}

func TestBuildCommerceSystemPrompt(t *testing.T) {
	p := buildCommerceSystemPrompt("Be friendly", "FAQ: hours 9-5")
	assert.Contains(t, p, "Be friendly")
	assert.Contains(t, p, "sales assistant")
	assert.Contains(t, p, "FAQ: hours 9-5")
	assert.Contains(t, p, "confirmed=true")
	assert.Contains(t, p, "display_uid")
	assert.Contains(t, p, "get_order_status")
	assert.Contains(t, p, "Never ask for or mention internal uuid")
	assert.Contains(t, p, "₹")
	assert.Contains(t, p, "get_store.address")
	assert.Contains(t, p, "out_of_range")
}

func TestCompactOrderCreateResultUsesDisplayUIDAndPaymentURL(t *testing.T) {
	out := compactOrderCreateResult(map[string]any{
		"id":          1,
		"uuid":        "secret-uuid",
		"display_uid": "AB-260710-000042",
		"status":      "PENDING_PAYMENT",
		"amount":      25050, // paise
		"payment": map[string]any{
			"status": "initiated",
			"meta_data": map[string]any{
				"url_to_redirect": "https://pay.example/go",
			},
		},
	})
	assert.Equal(t, "AB-260710-000042", out["display_uid"])
	assert.Equal(t, 250.5, out["amount"])
	assert.Equal(t, "https://pay.example/go", out["payment_url"])
	assert.Equal(t, "INR", out["currency"])
	assert.NotContains(t, out, "uuid")
}

func TestGetOrderStatusLatestWithoutOrderID(t *testing.T) {
	stub := &stubCommerceBackend{
		lookupStatusFn: func(ctx context.Context, storeID, phoneNumber, orderID string) (map[string]any, error) {
			assert.Equal(t, "42", storeID)
			assert.Equal(t, "919876543210", phoneNumber)
			assert.Empty(t, orderID)
			return map[string]any{
				"display_uid": "ST-260710-000001",
				"status":      "CONFIRMED",
				"amount":      10000,
			}, nil
		},
	}
	app := testApp()
	rt := &commerceRuntime{Client: stub, StoreID: "42", PhoneNumber: "919876543210"}
	out := app.executeCommerceTool(rt, "get_order_status", `{}`)
	assert.Contains(t, out, `"display_uid":"ST-260710-000001"`)
	assert.Contains(t, out, `"status":"CONFIRMED"`)
}

func TestGetOrderStatusWithOrderID(t *testing.T) {
	stub := &stubCommerceBackend{
		lookupStatusFn: func(ctx context.Context, storeID, phoneNumber, orderID string) (map[string]any, error) {
			assert.Equal(t, "ST-260710-000099", orderID)
			return map[string]any{
				"display_uid": "ST-260710-000099",
				"status":      "IN_TRANSIT",
				"amount":      5000,
			}, nil
		},
	}
	app := testApp()
	rt := &commerceRuntime{Client: stub, StoreID: "42", PhoneNumber: "919876543210"}
	out := app.executeCommerceTool(rt, "get_order_status", `{"order_id":"ST-260710-000099"}`)
	assert.Contains(t, out, `"display_uid":"ST-260710-000099"`)
}

func TestGetOrderStatusRequiresPhone(t *testing.T) {
	app := testApp()
	rt := &commerceRuntime{Client: &stubCommerceBackend{}, StoreID: "42"}
	out := app.executeCommerceTool(rt, "get_order_status", `{}`)
	assert.Contains(t, out, "customer phone is required")
}

func TestGetOrderStatusUnauthorizedError(t *testing.T) {
	stub := &stubCommerceBackend{
		lookupStatusFn: func(ctx context.Context, storeID, phoneNumber, orderID string) (map[string]any, error) {
			return nil, fmt.Errorf("Order not found")
		},
	}
	app := testApp()
	rt := &commerceRuntime{Client: stub, StoreID: "42", PhoneNumber: "919876543210"}
	out := app.executeCommerceTool(rt, "get_order_status", `{"order_id":"ST-000"}`)
	assert.Contains(t, out, "Order not found")
}

func TestStubCreateOrderJSONRoundTrip(t *testing.T) {
	// Ensure create args still marshal cleanly for MCP nesting.
	body := ticker.CreateOrderRequest{
		Store:        1,
		Items:        []ticker.OrderItem{{ProductOption: 2, Quantity: 1}},
		Email:        "a@b.com",
		DeliveryMode: "PICKUP_FROM_STORE",
	}
	b, err := json.Marshal(map[string]any{"order": body})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"store":1`)
}

func TestToolGetStoreAndListCategories(t *testing.T) {
	stub := &stubCommerceBackend{
		storeFn: func(ctx context.Context, storeID string) (map[string]any, error) {
			assert.Equal(t, "21", storeID)
			return map[string]any{"id": 21, "name": "Demo", "description": "Handmade"}, nil
		},
		categoriesFn: func(ctx context.Context, storeID string) ([]map[string]any, error) {
			assert.Equal(t, "21", storeID)
			return []map[string]any{{"id": 1, "name": "Earrings"}}, nil
		},
	}
	app := testApp()
	rt := &commerceRuntime{Client: stub, StoreID: "21"}

	storeOut := app.executeCommerceTool(rt, "get_store", `{}`)
	assert.Contains(t, storeOut, `"name":"Demo"`)

	catOut := app.executeCommerceTool(rt, "list_categories", `{}`)
	assert.Contains(t, catOut, `"Earrings"`)
	assert.Contains(t, catOut, `"count":1`)
}

func TestCommerceWelcomeFresh(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	assert.False(t, commerceWelcomeFresh(models.AIConfig{}))
	assert.True(t, commerceWelcomeFresh(models.AIConfig{
		CommerceWelcomeMessage:     "Hi!",
		CommerceWelcomeGeneratedAt: &now,
	}))
	assert.False(t, commerceWelcomeFresh(models.AIConfig{
		CommerceWelcomeMessage:     "Hi!",
		CommerceWelcomeGeneratedAt: &old,
	}))
	assert.True(t, commerceWelcomeStale(models.AIConfig{
		CommerceWelcomeMessage:     "Hi!",
		CommerceWelcomeGeneratedAt: &old,
	}))
}

func TestStripWhatsAppProductFences(t *testing.T) {
	raw := "Welcome!\n```whatsapp_product\n{\"title\":\"x\"}\n```\nAsk away."
	assert.Equal(t, "Welcome!\n\nAsk away.", stripWhatsAppProductFences(raw))
}

func TestFormatCommerceWelcomeCategoryList(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		assert.Equal(t, "", formatCommerceWelcomeCategoryList(nil))
		assert.Equal(t, "", formatCommerceWelcomeCategoryList([]map[string]any{}))
		assert.Equal(t, "", formatCommerceWelcomeCategoryList([]map[string]any{
			{"id": 1, "name": "  "},
			nil,
		}))
	})

	t.Run("sorts by listing_priority then name", func(t *testing.T) {
		got := formatCommerceWelcomeCategoryList([]map[string]any{
			{"name": "Zulu", "listing_priority": float64(0)},
			{"name": "Alpha", "listing_priority": float64(0)},
			{"name": "Featured", "listing_priority": float64(-1)},
			{"name": "Last", "listing_priority": 5},
		})
		assert.Equal(t, "• Featured\n• Alpha\n• Zulu\n• Last", got)
	})

	t.Run("max ten with more categories", func(t *testing.T) {
		cats := make([]map[string]any, 0, 12)
		for i := 1; i <= 12; i++ {
			cats = append(cats, map[string]any{
				"name":             fmt.Sprintf("Cat %02d", i),
				"listing_priority": i,
			})
		}
		got := formatCommerceWelcomeCategoryList(cats)
		lines := strings.Split(got, "\n")
		require.Len(t, lines, 11) // 10 bullets + more
		assert.Equal(t, "• Cat 01", lines[0])
		assert.Equal(t, "• Cat 10", lines[9])
		assert.Equal(t, commerceWelcomeMoreLabel, lines[10])
		assert.NotContains(t, got, "Cat 11")
	})

	t.Run("exactly ten has no more line", func(t *testing.T) {
		cats := make([]map[string]any, 0, 10)
		for i := 1; i <= 10; i++ {
			cats = append(cats, map[string]any{"name": fmt.Sprintf("C%d", i)})
		}
		got := formatCommerceWelcomeCategoryList(cats)
		assert.NotContains(t, got, commerceWelcomeMoreLabel)
		assert.Equal(t, 10, strings.Count(got, "• "))
	})
}

func TestCommerceSystemAddendumOutOfRangeGuidance(t *testing.T) {
	assert.Contains(t, commerceSystemAddendum, "get_store.address")
	assert.Contains(t, commerceSystemAddendum, "free_delivery_radius")
	assert.Contains(t, commerceSystemAddendum, "delivery_radius")
	assert.Contains(t, commerceSystemAddendum, "out_of_range")
	assert.Contains(t, commerceSystemAddendum, "Store Pickup")
}

func TestFormatOutOfRangeDeliveryMessage(t *testing.T) {
	t.Run("full store fields", func(t *testing.T) {
		got := formatOutOfRangeDeliveryMessage(map[string]any{
			"address":              "Vadakara, Kozhikode",
			"free_delivery_radius": 8,
			"delivery_radius":      16,
		})
		assert.Equal(t, strings.TrimSpace(`
Sorry, we're currently unable to deliver to this location.

Our store is located in Vadakara, Kozhikode. We offer:

- Free delivery within 8 km of our store
- Delivery up to 16 km for an additional delivery fee

Please provide a location within our delivery radius, or choose Store Pickup as your preferred option.

Thank you for your understanding.
`), got)
	})

	t.Run("omits free line when radius is zero", func(t *testing.T) {
		got := formatOutOfRangeDeliveryMessage(map[string]any{
			"address":              "MG Road",
			"free_delivery_radius": 0,
			"delivery_radius":      float64(12),
		})
		assert.Contains(t, got, "Our store is located in MG Road.")
		assert.Contains(t, got, "Delivery up to 12 km")
		assert.NotContains(t, got, "Free delivery")
	})

	t.Run("omits city when address is missing", func(t *testing.T) {
		got := formatOutOfRangeDeliveryMessage(map[string]any{
			"free_delivery_radius": 5,
			"delivery_radius":      10,
		})
		assert.NotContains(t, got, "Our store is located in")
		assert.Contains(t, got, "We offer:")
		assert.Contains(t, got, "Free delivery within 5 km")
		assert.Contains(t, got, "Store Pickup")
	})

	t.Run("nil store still offers pickup", func(t *testing.T) {
		got := formatOutOfRangeDeliveryMessage(nil)
		assert.Contains(t, got, "unable to deliver")
		assert.Contains(t, got, "Store Pickup")
		assert.NotContains(t, got, "We offer:")
		assert.NotContains(t, got, "located in")
	})
}

func TestToolCheckDeliveryEligibilityOutOfRangeUsesStoreCopy(t *testing.T) {
	stub := &stubCommerceBackend{
		checkDeliveryFn: func(ctx context.Context, storeID string, latitude, longitude float64) (map[string]any, error) {
			assert.Equal(t, "21", storeID)
			assert.Equal(t, 11.2, latitude)
			assert.Equal(t, 75.8, longitude)
			return map[string]any{
				"deliverable":        false,
				"zone":               "out_of_range",
				"message":            "Sorry, we can’t deliver to this location yet.",
				"shipping_fee_paise": 0,
			}, nil
		},
		storeFn: func(ctx context.Context, storeID string) (map[string]any, error) {
			assert.Equal(t, "21", storeID)
			return map[string]any{
				"address":              "Vadakara, Kozhikode",
				"free_delivery_radius": 8,
				"delivery_radius":      16,
			}, nil
		},
	}
	app := testApp()
	rt := &commerceRuntime{Client: stub, StoreID: "21"}
	out := app.executeCommerceTool(rt, "check_delivery_eligibility", `{"latitude":11.2,"longitude":75.8}`)
	assert.Contains(t, out, "Vadakara, Kozhikode")
	assert.Contains(t, out, "8 km")
	assert.Contains(t, out, "16 km")
	assert.Contains(t, out, "unable to deliver")
	assert.NotContains(t, out, "we can’t deliver to this location yet")
}

func TestToolCheckDeliveryEligibilityInRangeKeepsToolMessage(t *testing.T) {
	storeCalled := false
	stub := &stubCommerceBackend{
		checkDeliveryFn: func(ctx context.Context, storeID string, latitude, longitude float64) (map[string]any, error) {
			return map[string]any{
				"deliverable":        true,
				"zone":               "free",
				"message":            "Free delivery to this location.",
				"shipping_fee_paise": 0,
			}, nil
		},
		storeFn: func(ctx context.Context, storeID string) (map[string]any, error) {
			storeCalled = true
			return map[string]any{"address": "Should not be used"}, nil
		},
	}
	app := testApp()
	rt := &commerceRuntime{Client: stub, StoreID: "21"}
	out := app.executeCommerceTool(rt, "check_delivery_eligibility", `{"latitude":11.2,"longitude":75.8}`)
	assert.Contains(t, out, "Free delivery to this location.")
	assert.NotContains(t, out, "Should not be used")
	assert.False(t, storeCalled)
}

func TestToolCheckDeliveryEligibilityRequiresCoords(t *testing.T) {
	app := testApp()
	rt := &commerceRuntime{Client: &stubCommerceBackend{}, StoreID: "21"}
	out := app.executeCommerceTool(rt, "check_delivery_eligibility", `{}`)
	assert.Contains(t, out, "latitude and longitude are required")
}

func TestAppendCommerceWelcomeCategories(t *testing.T) {
	assert.Equal(t, "Hello there", appendCommerceWelcomeCategories("Hello there", nil))
	assert.Equal(t, "Hello there\n\n• Earrings", appendCommerceWelcomeCategories(
		"Hello there",
		[]map[string]any{{"name": "Earrings"}},
	))
}
