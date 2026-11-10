package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	commerceCatalogGenPrefix = "commerce:gen:"
	commerceCatalogKeyPrefix = "commerce:cat:"

	commerceScopeStore       = "store"
	commerceScopeCollections = "collections"
	commerceScopeFaqs        = "faqs"
	commerceScopeProducts    = "products"
)

var commerceCatalogCacheScopes = map[string]string{
	"get_store":            commerceScopeStore,
	"get_store_info":       commerceScopeStore,
	"list_collections":     commerceScopeCollections,
	"search_collections":   commerceScopeCollections,
	"list_faqs":            commerceScopeFaqs,
	"list_products":        commerceScopeProducts,
	"search_products":      commerceScopeProducts,
	"get_product":          commerceScopeProducts,
	"list_product_options": commerceScopeProducts,
}

func commerceCatalogScope(operation string) (string, bool) {
	scope, ok := commerceCatalogCacheScopes[strings.TrimSpace(operation)]
	return scope, ok
}

func (a *App) commerceCatalogCacheTTL() time.Duration {
	if a == nil || a.Config == nil {
		return 24 * time.Hour
	}
	return a.Config.CodedFlow.CatalogCacheDuration()
}

func (a *App) getCommerceCatalogCached(storeID, operation string, params map[string]string) (any, bool) {
	if a == nil || a.Redis == nil {
		return nil, false
	}
	scope, ok := commerceCatalogScope(operation)
	if !ok {
		return nil, false
	}
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, false
	}
	ctx := context.Background()
	gen := a.commerceCatalogGeneration(ctx, storeID, scope)
	key := commerceCatalogPayloadKey(storeID, scope, gen, operation, params)
	raw, err := a.Redis.Get(ctx, key).Bytes()
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func (a *App) setCommerceCatalogCached(storeID, operation string, params map[string]string, payload any) {
	if a == nil || a.Redis == nil || payload == nil {
		return
	}
	scope, ok := commerceCatalogScope(operation)
	if !ok {
		return
	}
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ctx := context.Background()
	gen := a.commerceCatalogGeneration(ctx, storeID, scope)
	key := commerceCatalogPayloadKey(storeID, scope, gen, operation, params)
	_ = a.Redis.Set(ctx, key, data, a.commerceCatalogCacheTTL()).Err()
}

func (a *App) invalidateCommerceCatalogScopes(storeID string, scopes []string) error {
	if a == nil || a.Redis == nil {
		return nil
	}
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return fmt.Errorf("store_id is required")
	}
	ctx := context.Background()
	seen := map[string]struct{}{}
	for _, scope := range scopes {
		scope = strings.TrimSpace(strings.ToLower(scope))
		if scope == "" {
			continue
		}
		if !validCommerceCatalogScope(scope) {
			return fmt.Errorf("unknown scope %q", scope)
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		if err := a.Redis.Incr(ctx, commerceCatalogGenKey(storeID, scope)).Err(); err != nil {
			return err
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("scopes are required")
	}
	return nil
}

func (a *App) commerceCatalogGeneration(ctx context.Context, storeID, scope string) int64 {
	val, err := a.Redis.Get(ctx, commerceCatalogGenKey(storeID, scope)).Result()
	if err != nil || strings.TrimSpace(val) == "" {
		return 0
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func validCommerceCatalogScope(scope string) bool {
	switch scope {
	case commerceScopeStore, commerceScopeCollections, commerceScopeFaqs, commerceScopeProducts:
		return true
	default:
		return false
	}
}

func commerceCatalogGenKey(storeID, scope string) string {
	return commerceCatalogGenPrefix + storeID + ":" + scope
}

func commerceCatalogPayloadKey(storeID, scope string, gen int64, operation string, params map[string]string) string {
	return fmt.Sprintf("%s%s:%s:%d:%s:%s",
		commerceCatalogKeyPrefix,
		storeID,
		scope,
		gen,
		strings.TrimSpace(operation),
		commerceCatalogParamsHash(params),
	)
}

func commerceCatalogParamsHash(params map[string]string) string {
	if len(params) == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:])
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(strings.TrimSpace(params[key]))
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
