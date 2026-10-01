package handlers

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/audit"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"gorm.io/gorm"
)

// CodedFlowBindingResponse is one compiled-in flow plus the keywords
// assigned for a WhatsApp account. Steps are read-only.
type CodedFlowBindingResponse struct {
	Key             string      `json:"key"`
	Name            string      `json:"name"`
	Description     string      `json:"description"`
	Steps           []codedflow.CodedStep `json:"steps"`
	Keywords        []string    `json:"keywords"`
	IsEnabled       bool        `json:"is_enabled"`
	WhatsAppAccount string      `json:"whatsapp_account"`
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
		if binding, ok := bindings[flow.Key]; ok {
			item.Keywords = []string(binding.Keywords)
			item.IsEnabled = binding.IsEnabled
		}
		if item.Keywords == nil {
			item.Keywords = []string{}
		}
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
		Keywords  []string `json:"keywords"`
		IsEnabled bool     `json:"is_enabled"`
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
		if err := a.DB.Save(&binding).Error; err != nil {
			a.Log.Error("Failed to update coded flow binding", "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update coded flow", nil, "")
		}
	case errors.Is(findErr, gorm.ErrRecordNotFound):
		binding = models.CodedFlowBinding{
			BaseModel:       models.BaseModel{ID: uuid.New()},
			OrganizationID:  orgID,
			WhatsAppAccount: accountName,
			FlowKey:         flow.Key,
			Keywords:        keywords,
			IsEnabled:       req.IsEnabled,
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

	return r.SendEnvelope(CodedFlowBindingResponse{
		Key:             flow.Key,
		Name:            flow.Name,
		Description:     flow.Description,
		Steps:           flow.Steps,
		Keywords:        []string(binding.Keywords),
		IsEnabled:       binding.IsEnabled,
		WhatsAppAccount: accountName,
	})
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
