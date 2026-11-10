package handlers

import (
	"encoding/json"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

type commerceCatalogInvalidateRequest struct {
	StoreID string   `json:"store_id"`
	Scopes  []string `json:"scopes"`
}

// InvalidateCommerceCatalog bumps Redis generation keys for the given store
// scopes so the next catalog read misses. Auth is the org API key; the org
// must already have that CommerceStoreID configured.
func (a *App) InvalidateCommerceCatalog(r *fastglue.Request) error {
	orgID, err := a.getOrgID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}

	var req commerceCatalogInvalidateRequest
	if err := json.Unmarshal(r.RequestCtx.PostBody(), &req); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid request body", nil, "")
	}
	storeID := strings.TrimSpace(req.StoreID)
	if storeID == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "store_id is required", nil, "")
	}
	if len(req.Scopes) == 0 {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "scopes are required", nil, "")
	}

	var settings models.ChatbotSettings
	err = a.DB.Where(
		"organization_id = ? AND ai_commerce_store_id = ?",
		orgID, storeID,
	).First(&settings).Error
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Store is not linked to this organization", nil, "")
	}

	if err := a.invalidateCommerceCatalogScopes(storeID, req.Scopes); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, err.Error(), nil, "")
	}
	return r.SendEnvelope(map[string]any{
		"store_id": storeID,
		"scopes":   req.Scopes,
		"ok":       true,
	})
}
