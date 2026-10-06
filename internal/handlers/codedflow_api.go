package handlers

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/audit"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"gorm.io/gorm"
)

const (
	codedSettingPickupFlowID   = "pickup_flow_id"
	codedSettingDeliveryFlowID = "delivery_flow_id"
)

// CodedFlowBindingResponse is one compiled-in flow plus the keywords
// assigned for a WhatsApp account. Steps are read-only.
type CodedFlowBindingResponse struct {
	Key             string                `json:"key"`
	Name            string                `json:"name"`
	Description     string                `json:"description"`
	Steps           []codedflow.CodedStep `json:"steps"`
	Keywords        []string              `json:"keywords"`
	IsEnabled       bool                  `json:"is_enabled"`
	WhatsAppAccount string                `json:"whatsapp_account"`
	// PickupFlowID / DeliveryFlowID are the effective Meta flow IDs
	// (stored override or built-in default) for tiqr_ecommerce.
	PickupFlowID   string `json:"pickup_flow_id,omitempty"`
	DeliveryFlowID string `json:"delivery_flow_id,omitempty"`
	// StoredPickupFlowID / StoredDeliveryFlowID are the raw overrides
	// (empty means use the built-in default).
	StoredPickupFlowID   string `json:"stored_pickup_flow_id,omitempty"`
	StoredDeliveryFlowID string `json:"stored_delivery_flow_id,omitempty"`
}

// ListCodedFlows returns every coded flow shipped with this build, merged
// with the keyword binding for the requested WhatsApp account.
func (a *App) ListCodedFlows(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}
	if !a.HasPermission(userID, models.ResourceFlowsChatbot, models.ActionRead, orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Permission denied", nil, "")
	}

	accountName := strings.TrimSpace(string(r.RequestCtx.QueryArgs().Peek("account")))
	if accountName == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "WhatsApp account is required", nil, "")
	}
	if !a.whatsAppAccountInOrg(orgID, accountName) {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "WhatsApp account not found", nil, "")
	}

	bindings, err := a.codedFlowBindings(orgID, accountName)
	if err != nil {
		a.Log.Error("Failed to list coded flow bindings", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to list coded flows", nil, "")
	}

	flows := codedflow.Flows()
	response := make([]CodedFlowBindingResponse, 0, len(flows))
	for _, flow := range flows {
		item := CodedFlowBindingResponse{
			Key:             flow.Key,
			Name:            flow.Name,
			Description:     flow.Description,
			Steps:           flow.Steps,
			Keywords:        []string{},
			WhatsAppAccount: accountName,
		}
		var binding *models.CodedFlowBinding
		if b, ok := bindings[flow.Key]; ok {
			item.Keywords = []string(b.Keywords)
			item.IsEnabled = b.IsEnabled
			binding = &b
		}
		if item.Keywords == nil {
			item.Keywords = []string{}
		}
		applyEcommerceFlowIDs(&item, flow.Key, binding)
		response = append(response, item)
	}

	return r.SendEnvelope(map[string]any{
		"flows": response,
	})
}

// UpdateCodedFlowBinding sets keywords and enabled for one compiled-in flow.
// Unknown keys are rejected. The flow definition cannot be created or deleted.
func (a *App) UpdateCodedFlowBinding(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}
	if !a.HasPermission(userID, models.ResourceFlowsChatbot, models.ActionWrite, orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Permission denied", nil, "")
	}

	key, _ := r.RequestCtx.UserValue("key").(string)
	key = strings.TrimSpace(key)
	flow := codedflow.ByKey(key)
	if flow == nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Coded flow not found", nil, "")
	}

	accountName := strings.TrimSpace(string(r.RequestCtx.QueryArgs().Peek("account")))
	if accountName == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "WhatsApp account is required", nil, "")
	}
	if !a.whatsAppAccountInOrg(orgID, accountName) {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "WhatsApp account not found", nil, "")
	}

	var req struct {
		Keywords       []string `json:"keywords"`
		IsEnabled      bool     `json:"is_enabled"`
		PickupFlowID   *string  `json:"pickup_flow_id"`
		DeliveryFlowID *string  `json:"delivery_flow_id"`
	}
	if err := json.Unmarshal(r.RequestCtx.PostBody(), &req); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid request body", nil, "")
	}
	keywords, err := normalizeCodedFlowKeywords(req.Keywords)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, err.Error(), nil, "")
	}

	var binding models.CodedFlowBinding
	findErr := a.DB.Where(
		"organization_id = ? AND whats_app_account = ? AND flow_key = ?",
		orgID, accountName, flow.Key,
	).First(&binding).Error

	var before *models.CodedFlowBinding
	switch {
	case findErr == nil:
		copy := binding
		before = &copy
		binding.Keywords = keywords
		binding.IsEnabled = req.IsEnabled
		merged, mergeErr := mergeEcommerceFlowSettings(flow.Key, binding.Settings, req.PickupFlowID, req.DeliveryFlowID)
		if mergeErr != nil {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, mergeErr.Error(), nil, "")
		}
		if flow.Key == tiqrecommerce.FlowKey {
			binding.Settings = merged
		}
		if err := a.DB.Save(&binding).Error; err != nil {
			a.Log.Error("Failed to update coded flow binding", "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update coded flow", nil, "")
		}
	case errors.Is(findErr, gorm.ErrRecordNotFound):
		settings, mergeErr := mergeEcommerceFlowSettings(flow.Key, nil, req.PickupFlowID, req.DeliveryFlowID)
		if mergeErr != nil {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, mergeErr.Error(), nil, "")
		}
		binding = models.CodedFlowBinding{
			BaseModel:       models.BaseModel{ID: uuid.New()},
			OrganizationID:  orgID,
			WhatsAppAccount: accountName,
			FlowKey:         flow.Key,
			Keywords:        keywords,
			IsEnabled:       req.IsEnabled,
			Settings:        settings,
		}
		if err := a.DB.Create(&binding).Error; err != nil {
			a.Log.Error("Failed to create coded flow binding", "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update coded flow", nil, "")
		}
	default:
		a.Log.Error("Failed to load coded flow binding", "error", findErr)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update coded flow", nil, "")
	}

	action := models.AuditActionUpdated
	if before == nil {
		action = models.AuditActionCreated
	}
	audit.LogAudit(a.DB, orgID, userID, audit.GetUserName(a.DB, userID),
		"coded_flow_binding", binding.ID, action, before, &binding)

	resp := CodedFlowBindingResponse{
		Key:             flow.Key,
		Name:            flow.Name,
		Description:     flow.Description,
		Steps:           flow.Steps,
		Keywords:        []string(binding.Keywords),
		IsEnabled:       binding.IsEnabled,
		WhatsAppAccount: accountName,
	}
	applyEcommerceFlowIDs(&resp, flow.Key, &binding)
	return r.SendEnvelope(resp)
}

func (a *App) codedFlowBindings(orgID uuid.UUID, accountName string) (map[string]models.CodedFlowBinding, error) {
	var rows []models.CodedFlowBinding
	err := a.DB.Where("organization_id = ? AND whats_app_account = ?", orgID, accountName).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]models.CodedFlowBinding, len(rows))
	for _, row := range rows {
		if codedflow.ByKey(row.FlowKey) == nil {
			continue
		}
		out[row.FlowKey] = row
	}
	return out, nil
}

func (a *App) whatsAppAccountInOrg(orgID uuid.UUID, name string) bool {
	var count int64
	err := a.DB.Model(&models.WhatsAppAccount{}).
		Where("organization_id = ? AND name = ?", orgID, name).
		Count(&count).Error
	return err == nil && count > 0
}

func normalizeCodedFlowKeywords(raw []string) (models.StringArray, error) {
	seen := map[string]struct{}{}
	out := make(models.StringArray, 0, len(raw))
	for _, keyword := range raw {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		if len(keyword) > 200 {
			return nil, errors.New("Keyword is too long")
		}
		key := strings.ToLower(keyword)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, keyword)
	}
	return out, nil
}

func applyEcommerceFlowIDs(item *CodedFlowBindingResponse, flowKey string, binding *models.CodedFlowBinding) {
	if item == nil || flowKey != tiqrecommerce.FlowKey {
		return
	}
	storedPickup, storedDelivery := ecommerceFlowIDsFromSettings(nil)
	if binding != nil {
		storedPickup, storedDelivery = ecommerceFlowIDsFromSettings(binding.Settings)
	}
	item.StoredPickupFlowID = storedPickup
	item.StoredDeliveryFlowID = storedDelivery
	item.PickupFlowID = firstNonEmptyFlowID(storedPickup, tiqrecommerce.TiqrEcommercePickupFlowID)
	item.DeliveryFlowID = firstNonEmptyFlowID(storedDelivery, tiqrecommerce.EcommerceFlowID)
}

func ecommerceFlowIDsFromSettings(settings models.JSONB) (pickup, delivery string) {
	if settings == nil {
		return "", ""
	}
	pickup, _ = settings[codedSettingPickupFlowID].(string)
	delivery, _ = settings[codedSettingDeliveryFlowID].(string)
	return strings.TrimSpace(pickup), strings.TrimSpace(delivery)
}

func mergeEcommerceFlowSettings(flowKey string, existing models.JSONB, pickupID, deliveryID *string) (models.JSONB, error) {
	if flowKey != tiqrecommerce.FlowKey {
		if existing == nil {
			return models.JSONB{}, nil
		}
		return existing, nil
	}
	out := models.JSONB{}
	for k, v := range existing {
		out[k] = v
	}
	if pickupID != nil {
		normalized, err := normalizeMetaFlowID(*pickupID)
		if err != nil {
			return nil, errors.New("Pickup flow ID must be a Meta flow ID (digits only), or blank to use the default")
		}
		if normalized == "" {
			delete(out, codedSettingPickupFlowID)
		} else {
			out[codedSettingPickupFlowID] = normalized
		}
	}
	if deliveryID != nil {
		normalized, err := normalizeMetaFlowID(*deliveryID)
		if err != nil {
			return nil, errors.New("Delivery flow ID must be a Meta flow ID (digits only), or blank to use the default")
		}
		if normalized == "" {
			delete(out, codedSettingDeliveryFlowID)
		} else {
			out[codedSettingDeliveryFlowID] = normalized
		}
	}
	return out, nil
}

func normalizeMetaFlowID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", nil
	}
	if len(id) > 100 {
		return "", errors.New("flow ID is too long")
	}
	for _, r := range id {
		if !unicode.IsDigit(r) {
			return "", errors.New("flow ID must be numeric")
		}
	}
	return id, nil
}

func firstNonEmptyFlowID(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ResolveEcommerceMetaFlowID returns the Meta flow ID for pickup or delivery
// checkout forms for the given account, falling back to built-in defaults.
func (a *App) ResolveEcommerceMetaFlowID(orgID uuid.UUID, accountName string, delivery bool) string {
	fallback := tiqrecommerce.TiqrEcommercePickupFlowID
	key := codedSettingPickupFlowID
	if delivery {
		fallback = tiqrecommerce.EcommerceFlowID
		key = codedSettingDeliveryFlowID
	}
	if a == nil || a.DB == nil || accountName == "" {
		return fallback
	}
	var binding models.CodedFlowBinding
	err := a.DB.Where(
		"organization_id = ? AND whats_app_account = ? AND flow_key = ?",
		orgID, accountName, tiqrecommerce.FlowKey,
	).First(&binding).Error
	if err != nil || binding.Settings == nil {
		return fallback
	}
	if id, ok := binding.Settings[key].(string); ok {
		id = strings.TrimSpace(id)
		if id != "" {
			return id
		}
	}
	return fallback
}
