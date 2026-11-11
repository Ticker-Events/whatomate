package ticker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientSearchProducts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/service/buyer/product/", r.URL.Path)
		assert.Equal(t, "5", r.URL.Query().Get("store_id"))
		assert.Equal(t, "tea", r.URL.Query().Get("search"))
		assert.Equal(t, "true", r.URL.Query().Get("include_custom_pricing"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{
					"id": 2, "name": "Green Tea", "min_price": 50, "options": []any{},
					"images": []any{
						map[string]any{"id": 1, "image": "https://cdn.example.com/tea.jpg"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := ticker.NewClient(srv.URL, srv.Client())
	products, err := c.SearchProducts(context.Background(), "5", "tea", 10)
	require.NoError(t, err)
	require.Len(t, products, 1)
	assert.Equal(t, "Green Tea", products[0].Name)
	assert.Equal(t, 0.5, products[0].MinPrice) // 50 paise → ₹0.50
	assert.Equal(t, "https://cdn.example.com/tea.jpg", products[0].ImageURL)
}

func TestExtractProductImageURL(t *testing.T) {
	assert.Equal(t, "", ticker.ExtractProductImageURL(nil))
	assert.Equal(t, "", ticker.ExtractProductImageURL(map[string]any{}))
	assert.Equal(t, "https://cdn.example.com/a.jpg", ticker.ExtractProductImageURL(map[string]any{
		"images": []any{
			map[string]any{"image": "https://cdn.example.com/a.jpg"},
		},
	}))
	assert.Equal(t, "https://cdn.example.com/original.jpg", ticker.ExtractProductImageURL(map[string]any{
		"images": []any{
			map[string]any{
				"image":        "https://cdn.example.com/display.webp",
				"url":          "https://cdn.example.com/display.webp",
				"original_url": "https://cdn.example.com/original.jpg",
			},
		},
	}))
}

func TestClientCreateOrderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"new_address":["required"]}`))
	}))
	defer srv.Close()

	c := ticker.NewClient(srv.URL, srv.Client())
	_, err := c.CreateOrder(context.Background(), ticker.CreateOrderRequest{
		Store: 1,
		Items: []ticker.OrderItem{{ProductOption: 1, Quantity: 1}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestClientListCategories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/service/buyer/store/5/category/", r.URL.Path)
		assert.Equal(t, "vegan", r.URL.Query().Get("tags"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count": 1,
			"results": []map[string]any{
				{"id": 9, "name": "Cakes"},
			},
		})
	}))
	defer srv.Close()

	c := ticker.NewClient(srv.URL, srv.Client())
	page, err := c.ListCategories(context.Background(), "5", ticker.ListCategoriesParams{
		Tags:  []string{"vegan"},
		Limit: 20,
	})
	require.NoError(t, err)
	cats, ok := page["categories"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, cats, 1)
	assert.Equal(t, "Cakes", cats[0]["name"])
}

func TestClientListProductsPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/service/buyer/product/", r.URL.Path)
		assert.Equal(t, "3", r.URL.Query().Get("category_id"))
		assert.Equal(t, "true", r.URL.Query().Get("include_custom_pricing"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count":   1,
			"results": []map[string]any{{"id": 2, "name": "Latte"}},
		})
	}))
	defer srv.Close()

	c := ticker.NewClient(srv.URL, srv.Client())
	page, err := c.ListProductsPage(context.Background(), "7", ticker.ListProductsParams{
		CategoryID: "3",
		Limit:      10,
	})
	require.NoError(t, err)
	products, ok := page["products"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, products, 1)
	assert.Equal(t, "Latte", products[0]["name"])
	assert.Equal(t, false, page["has_more"])
}

func TestClientFollowNext(t *testing.T) {
	var gotOffset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOffset = r.URL.Query().Get("offset")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count":   2,
			"results": []map[string]any{{"id": 8, "name": "Mocha", "min_price": 15000}},
		})
	}))
	defer srv.Close()

	c := ticker.NewClient(srv.URL, srv.Client())
	page, err := c.FollowNext(context.Background(), srv.URL+"/service/buyer/product/?limit=50&offset=50")
	require.NoError(t, err)
	assert.Equal(t, "50", gotOffset)
	assert.Equal(t, false, page["has_more"])
	results, ok := page["results"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, results, 1)
	assert.Equal(t, "Mocha", results[0]["name"])
	assert.Equal(t, 150.0, results[0]["min_price"])

	_, err = c.FollowNext(context.Background(), "https://evil.example/service/buyer/product/?offset=50")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the store api")
}

func TestClientGetProduct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/service/buyer/product/9/", r.URL.Path)
		assert.Equal(t, "5", r.URL.Query().Get("store_id"))
		assert.Equal(t, "true", r.URL.Query().Get("include_custom_pricing"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 9, "name": "Custom Cake", "min_price": 0, "custom_pricing": true,
		})
	}))
	defer srv.Close()

	c := ticker.NewClient(srv.URL, srv.Client())
	raw, err := c.GetProduct(context.Background(), "5", "9")
	require.NoError(t, err)
	assert.Equal(t, "Custom Cake", raw["name"])
	assert.Equal(t, true, raw["custom_pricing"])
}

func TestClientGetStoreAndFaqs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/service/buyer/store/8/":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 8, "name": "Demo"})
		case "/service/buyer/store/8/faq/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{"question": "Hours?", "answer": "9-5"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := ticker.NewClient(srv.URL, srv.Client())
	store, err := c.GetStore(context.Background(), "8")
	require.NoError(t, err)
	assert.Equal(t, "Demo", store["name"])

	faqs, err := c.ListFaqs(context.Background(), "8")
	require.NoError(t, err)
	list, ok := faqs.([]map[string]any)
	require.True(t, ok)
	require.Len(t, list, 1)
}

func TestClientPreviewCheckout(t *testing.T) {
	var gotPath string
	var gotBody ticker.CheckoutPreviewRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		assert.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []any{
				map[string]any{
					"product_option":  map[string]any{"id": 9, "name": "Regular"},
					"quantity":        2,
					"original_amount": 8000,
					"amount":          8000,
				},
			},
			"original_amount":         8000,
			"amount":                  12000,
			"original_shipping_fee":   4000,
			"shipping_fee":            4000,
			"discount_total":          0,
			"shipping_discount_total": 0,
			"applied_discounts":       []any{},
		})
	}))
	defer srv.Close()

	lat, lng := 11.2, 75.8
	c := ticker.NewClient(srv.URL, srv.Client())
	raw, err := c.PreviewCheckout(context.Background(), ticker.CheckoutPreviewRequest{
		Items:        []ticker.OrderItem{{ProductOption: 9, Quantity: 2}},
		DeliveryMode: "DELIVERY_TO_LOCATION",
		Latitude:     &lat,
		Longitude:    &lng,
	})
	require.NoError(t, err)
	assert.Equal(t, "/service/buyer/cart/checkout-preview/", gotPath)
	assert.Equal(t, "DELIVERY_TO_LOCATION", gotBody.DeliveryMode)
	require.Len(t, gotBody.Items, 1)
	assert.Equal(t, 9, gotBody.Items[0].ProductOption)
	assert.Equal(t, 2, gotBody.Items[0].Quantity)
	require.NotNil(t, gotBody.Latitude)
	require.NotNil(t, gotBody.Longitude)
	assert.Equal(t, 11.2, *gotBody.Latitude)
	assert.Equal(t, 75.8, *gotBody.Longitude)
	assert.Equal(t, 80.0, raw["original_amount"])
	assert.Equal(t, 120.0, raw["amount"])
	assert.Equal(t, 40.0, raw["original_shipping_fee"])
}
