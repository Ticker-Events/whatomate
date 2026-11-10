package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommerceCatalogCache_HitMissAndScopeInvalidation(t *testing.T) {
	mr := miniredis.RunT(t)
	app := &App{
		Redis:  redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		Config: &config.Config{CodedFlow: config.CodedFlowConfig{CatalogCacheTTL: "1h"}},
	}

	params := map[string]string{"limit": "20"}
	payload := map[string]any{"results": []any{map[string]any{"id": "1", "name": "Cakes"}}}
	app.setCommerceCatalogCached("42", "list_collections", params, payload)

	hit, ok := app.getCommerceCatalogCached("42", "list_collections", params)
	require.True(t, ok)
	assert.Equal(t, payload, hit)

	// create_order is never cached.
	app.setCommerceCatalogCached("42", "create_order", nil, map[string]any{"id": "x"})
	_, ok = app.getCommerceCatalogCached("42", "create_order", nil)
	assert.False(t, ok)

	productParams := map[string]string{"category_id": "1"}
	app.setCommerceCatalogCached("42", "list_products", productParams, map[string]any{"results": []any{}})
	_, ok = app.getCommerceCatalogCached("42", "list_products", productParams)
	require.True(t, ok)

	require.NoError(t, app.invalidateCommerceCatalogScopes("42", []string{"products"}))

	_, ok = app.getCommerceCatalogCached("42", "list_products", productParams)
	assert.False(t, ok, "products generation bump must miss")
	_, ok = app.getCommerceCatalogCached("42", "list_collections", params)
	assert.True(t, ok, "collections cache stays after products invalidate")
}

func TestCommerceCatalogCache_ConfigurableTTL(t *testing.T) {
	assert.Equal(t, time.Hour, config.CodedFlowConfig{CatalogCacheTTL: "1h"}.CatalogCacheDuration())
	assert.Equal(t, 24*time.Hour, config.CodedFlowConfig{}.CatalogCacheDuration())
	assert.Equal(t, 24*time.Hour, config.CodedFlowConfig{CatalogCacheTTL: "nope"}.CatalogCacheDuration())
}

func TestInvokeTiqrStoreRESTOperation_WelcomeBypassesCache(t *testing.T) {
	// Welcome uses invokeTiqrStoreRESTOperation directly. Prove the raw helper
	// never reads Redis even when a payload is already cached for that key.
	mr := miniredis.RunT(t)
	app := &App{
		Redis:  redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		Config: &config.Config{},
	}
	params := map[string]string{"limit": "20"}
	app.setCommerceCatalogCached("42", "list_collections", params, map[string]any{
		"results": []any{map[string]any{"id": "stale"}},
	})

	server := ticker.NewClient("http://example.invalid", nil)
	_, err := invokeTiqrStoreRESTOperation(context.Background(), server, "list_collections", "42", "", params)
	require.Error(t, err, "direct invoke must call the network, not Redis")
}

func TestCommerceCatalogCache_SecondListCollectionsSkipsREST(t *testing.T) {
	// Mirrors the cache wrap in commerceCatalogStorePage / execChatTiqrStoreAPI
	// without needing a DB-backed settings row.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/service/buyer/store/42/category/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []map[string]any{
					{"id": 1, "name": "Cakes"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/service/buyer/order/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"display_uid": "ORD-1",
				"uuid":        "ord-1",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/service/buyer/order/by-phone/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count":   1,
				"results": []map[string]any{{"display_uid": "ORD-1"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	mr := miniredis.RunT(t)
	app := &App{
		Redis:  redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		Config: &config.Config{},
	}
	client := ticker.NewClient(srv.URL, srv.Client())
	params := map[string]string{"limit": "20"}

	readCollections := func() (any, error) {
		if hit, ok := app.getCommerceCatalogCached("42", "list_collections", params); ok {
			return hit, nil
		}
		raw, err := invokeTiqrStoreRESTOperation(context.Background(), client, "list_collections", "42", "", params)
		if err == nil {
			app.setCommerceCatalogCached("42", "list_collections", params, raw)
		}
		return raw, err
	}

	first, err := readCollections()
	require.NoError(t, err)
	require.Equal(t, int32(1), hits.Load())

	second, err := readCollections()
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load(), "second list_collections must reuse Redis")
	firstJSON, err := json.Marshal(first)
	require.NoError(t, err)
	secondJSON, err := json.Marshal(second)
	require.NoError(t, err)
	assert.JSONEq(t, string(firstJSON), string(secondJSON))

	_, err = invokeTiqrStoreRESTOperation(context.Background(), client, "create_order", "42", "919800000001", map[string]string{
		"items":         `[{"product_option":1,"quantity":1}]`,
		"email":         "buyer@example.com",
		"delivery_mode": "PICKUP_FROM_STORE",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(2), hits.Load(), "create_order always hits TiQR")

	_, err = invokeTiqrStoreRESTOperation(context.Background(), client, "list_orders_by_phone", "42", "919800000001", nil)
	require.NoError(t, err)
	assert.Equal(t, int32(3), hits.Load(), "order lookup always hits TiQR")
}
