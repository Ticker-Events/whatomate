package handlers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/audit"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

const (
	chatbotFlowExportFormat        = "whatomate.chatbot_flow"
	chatbotFlowExportFormatVersion = 1
)

// ChatbotFlowExportPayload is the portable flow document inside an export envelope.
// Cross-reference IDs in graph node configs are preserved as-is.
type ChatbotFlowExportPayload struct {
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	TriggerKeywords   []string       `json:"trigger_keywords"`
	TriggerButtonID   string         `json:"trigger_button_id,omitempty"`
	InitialMessage    string         `json:"initial_message,omitempty"`
	CompletionMessage string         `json:"completion_message,omitempty"`
	OnCompleteAction  string         `json:"on_complete_action,omitempty"`
	CompletionConfig  map[string]any `json:"completion_config,omitempty"`
	TimeoutMessage    string         `json:"timeout_message,omitempty"`
	CancelKeywords    []string       `json:"cancel_keywords,omitempty"`
	PanelConfig       map[string]any `json:"panel_config,omitempty"`
	WhatsAppAccount   string         `json:"whatsapp_account,omitempty"`
	Graph             map[string]any `json:"graph,omitempty"`
}

// ChatbotFlowExportEnvelope wraps a flow for download / restore.
type ChatbotFlowExportEnvelope struct {
	Format        string                   `json:"format"`
	FormatVersion int                      `json:"format_version"`
	ExportedAt    string                   `json:"exported_at"`
	Flow          ChatbotFlowExportPayload `json:"flow"`
}

var unsafeFilenameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func chatbotFlowToExportPayload(flow *models.ChatbotFlow) ChatbotFlowExportPayload {
	payload := ChatbotFlowExportPayload{
		Name:              flow.Name,
		Description:       flow.Description,
		TriggerKeywords:   []string(flow.TriggerKeywords),
		TriggerButtonID:   flow.TriggerButtonID,
		InitialMessage:    flow.InitialMessage,
		CompletionMessage: flow.CompletionMessage,
		OnCompleteAction:  flow.OnCompleteAction,
		TimeoutMessage:    flow.TimeoutMessage,
		CancelKeywords:    []string(flow.CancelKeywords),
		WhatsAppAccount:   flow.WhatsAppAccount,
	}
	if flow.CompletionConfig != nil {
		payload.CompletionConfig = map[string]any(flow.CompletionConfig)
	}
	if flow.PanelConfig != nil {
		payload.PanelConfig = map[string]any(flow.PanelConfig)
	}
	if flow.Graph != nil {
		payload.Graph = map[string]any(flow.Graph)
	}
	if payload.TriggerKeywords == nil {
		payload.TriggerKeywords = []string{}
	}
	return payload
}

func chatbotFlowExportFilename(name string) string {
	base := strings.TrimSpace(name)
	if base == "" {
		base = "chatbot-flow"
	}
	base = unsafeFilenameChars.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-._")
	if base == "" {
		base = "chatbot-flow"
	}
	return base + ".json"
}

func validateChatbotFlowExportGraph(graph map[string]any) error {
	if graph == nil {
		return fmt.Errorf("graph is required")
	}
	_, err := parseChatGraph(models.JSONB(graph))
	if err != nil {
		return err
	}
	return nil
}

func (a *App) createChatbotFlowFromPayload(orgID, userID uuid.UUID, payload ChatbotFlowExportPayload, name string) (*models.ChatbotFlow, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if err := validateChatbotFlowExportGraph(payload.Graph); err != nil {
		return nil, err
	}

	flow := &models.ChatbotFlow{
		BaseModel:         models.BaseModel{ID: uuid.New()},
		OrganizationID:    orgID,
		WhatsAppAccount:   payload.WhatsAppAccount,
		Name:              name,
		Description:       payload.Description,
		TriggerKeywords:   models.StringArray(payload.TriggerKeywords),
		TriggerButtonID:   payload.TriggerButtonID,
		InitialMessage:    payload.InitialMessage,
		CompletionMessage: payload.CompletionMessage,
		OnCompleteAction:  payload.OnCompleteAction,
		CompletionConfig:  models.JSONB(payload.CompletionConfig),
		TimeoutMessage:    payload.TimeoutMessage,
		CancelKeywords:    models.StringArray(payload.CancelKeywords),
		PanelConfig:       models.JSONB(payload.PanelConfig),
		Graph:             models.JSONB(payload.Graph),
		IsEnabled:         false, // avoid keyword collisions with source flow
		CreatedByID:       &userID,
		UpdatedByID:       &userID,
	}

	if err := a.DB.Create(flow).Error; err != nil {
		return nil, err
	}
	return flow, nil
}

// ExportChatbotFlow downloads a portable JSON envelope for a chatbot flow.
func (a *App) ExportChatbotFlow(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}

	if !a.HasPermission(userID, models.ResourceFlowsChatbot, models.ActionRead, orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Permission denied", nil, "")
	}

	id, err := parsePathUUID(r, "id", "flow")
	if err != nil {
		return nil
	}

	var flow models.ChatbotFlow
	if err := a.DB.Where("id = ? AND organization_id = ?", id, orgID).First(&flow).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Flow not found", nil, "")
	}

	envelope := ChatbotFlowExportEnvelope{
		Format:        chatbotFlowExportFormat,
		FormatVersion: chatbotFlowExportFormatVersion,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Flow:          chatbotFlowToExportPayload(&flow),
	}

	body, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		a.Log.Error("Failed to marshal flow export", "error", err, "flow_id", id)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to export flow", nil, "")
	}

	filename := chatbotFlowExportFilename(flow.Name)
	r.RequestCtx.Response.Header.SetContentType("application/json")
	r.RequestCtx.Response.Header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	r.RequestCtx.SetStatusCode(fasthttp.StatusOK)
	r.RequestCtx.SetBody(body)
	return nil
}

// ImportChatbotFlow creates a new chatbot flow from an export envelope.
// Cross-reference IDs in the graph are kept as-is. The new flow is disabled.
func (a *App) ImportChatbotFlow(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}

	if !a.HasPermission(userID, models.ResourceFlowsChatbot, models.ActionWrite, orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Permission denied", nil, "")
	}

	var envelope ChatbotFlowExportEnvelope
	if err := json.Unmarshal(r.RequestCtx.PostBody(), &envelope); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid request body", nil, "")
	}

	if envelope.Format != chatbotFlowExportFormat {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Unsupported export format", nil, "")
	}
	if envelope.FormatVersion != chatbotFlowExportFormatVersion {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Unsupported export format version", nil, "")
	}
	if envelope.Flow.Name == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Name is required", nil, "")
	}
	if err := validateChatbotFlowExportGraph(envelope.Flow.Graph); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid flow graph: "+err.Error(), nil, "")
	}

	flow, err := a.createChatbotFlowFromPayload(orgID, userID, envelope.Flow, envelope.Flow.Name)
	if err != nil {
		a.Log.Error("Failed to import flow", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to import flow", nil, "")
	}

	a.InvalidateChatbotFlowsCache(orgID)

	audit.LogAudit(a.DB, orgID, userID, audit.GetUserName(a.DB, userID),
		"chatbot_flow", flow.ID, models.AuditActionCreated, nil, flow)

	return r.SendEnvelope(map[string]any{
		"id":      flow.ID.String(),
		"message": "Flow imported successfully",
	})
}

// DuplicateChatbotFlow creates a same-org copy of a chatbot flow (disabled, name + " (Copy)").
func (a *App) DuplicateChatbotFlow(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}

	if !a.HasPermission(userID, models.ResourceFlowsChatbot, models.ActionWrite, orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Permission denied", nil, "")
	}

	id, err := parsePathUUID(r, "id", "flow")
	if err != nil {
		return nil
	}

	var source models.ChatbotFlow
	if err := a.DB.Where("id = ? AND organization_id = ?", id, orgID).First(&source).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Flow not found", nil, "")
	}

	payload := chatbotFlowToExportPayload(&source)
	if err := validateChatbotFlowExportGraph(payload.Graph); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Source flow has invalid graph: "+err.Error(), nil, "")
	}

	flow, err := a.createChatbotFlowFromPayload(orgID, userID, payload, source.Name+" (Copy)")
	if err != nil {
		a.Log.Error("Failed to duplicate flow", "error", err, "original_flow_id", id)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to duplicate flow", nil, "")
	}

	a.InvalidateChatbotFlowsCache(orgID)

	audit.LogAudit(a.DB, orgID, userID, audit.GetUserName(a.DB, userID),
		"chatbot_flow", flow.ID, models.AuditActionCreated, nil, flow)

	return r.SendEnvelope(map[string]any{
		"id":      flow.ID.String(),
		"message": "Flow duplicated successfully",
	})
}
