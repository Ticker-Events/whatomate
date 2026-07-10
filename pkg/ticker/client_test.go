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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": 2, "name": "Green Tea", "min_price": 50, "options": []any{}},
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
