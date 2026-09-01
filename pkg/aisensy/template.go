package aisensy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// SubmitTemplate creates or updates a template via AiSensy's template endpoint.
// The wa_template endpoint lives outside the /api subtree, mirroring the pattern
// used by /get-templates.
func (c *Client) SubmitTemplate(ctx context.Context, account *whatsapp.Account, template *whatsapp.TemplateSubmission) (string, error) {
	directRoot := strings.TrimSuffix(c.baseURL, "/api")
	url := fmt.Sprintf("%s/wa_template", directRoot)

	components, err := buildTemplateComponents(template)
	if err != nil {
		return "", err
	}

	payload := map[string]any{
		"name":       template.Name,
		"category":   template.Category,
		"language":   template.Language,
		"components": components,
	}
	// Match Meta Cloud API: named body/header params need parameter_format=NAMED.
	if strings.ToUpper(template.Category) != "AUTHENTICATION" {
		isNamedParams := template.ParameterFormat == "named" ||
			whatsapp.HasNamedParams(template.BodyContent) ||
			whatsapp.HasNamedParams(template.HeaderContent)
		if isNamedParams {
			payload["parameter_format"] = "NAMED"
		}
	}

	respBody, err := c.doRequest(ctx, http.MethodPost, url, payload, account)
	if err != nil {
		return "", fmt.Errorf("failed to submit template via aisensy: %w", err)
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse template response: %w", err)
	}

	if result.ID == "" && template.MetaTemplateID != "" {
		return template.MetaTemplateID, nil
	}
	return result.ID, nil
}

// FetchTemplates fetches all templates for the account directly from Meta via
// AiSensy's get-templates endpoint. This endpoint syncs the live template list
// from Meta, returning approved/pending/rejected statuses in real time.
func (c *Client) FetchTemplates(ctx context.Context, account *whatsapp.Account) ([]whatsapp.MetaTemplate, error) {
	// The get-templates endpoint lives one level above the /api subtree.
	directRoot := strings.TrimSuffix(c.baseURL, "/api")
	url := fmt.Sprintf("%s/get-templates", directRoot)

	respBody, err := c.doRequest(ctx, http.MethodGet, url, nil, account)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch templates via aisensy: %w", err)
	}

	// Try wrapped {"data": [...]} shape first (standard AiSensy convention).
	var wrapped struct {
		Data []whatsapp.MetaTemplate `json:"data"`
	}
	if err := json.Unmarshal(respBody, &wrapped); err == nil && wrapped.Data != nil {
		return wrapped.Data, nil
	}

	// Fall back to a bare array response.
	var templates []whatsapp.MetaTemplate
	if err := json.Unmarshal(respBody, &templates); err != nil {
		return nil, fmt.Errorf("failed to parse templates response: %w", err)
	}
	return templates, nil
}

// DeleteTemplate deletes a template by name via AiSensy.
func (c *Client) DeleteTemplate(ctx context.Context, account *whatsapp.Account, templateName string) error {
	url := fmt.Sprintf("%s/message-templates/?name=%s", c.baseURL, templateName)

	_, err := c.doRequest(ctx, http.MethodDelete, url, nil, account)
	if err != nil {
		return fmt.Errorf("failed to delete template via aisensy: %w", err)
	}

	return nil
}

// buildTemplateComponents converts a TemplateSubmission into a Meta-compatible
// components array for the AiSensy API, including example/sample text for variables.
func buildTemplateComponents(t *whatsapp.TemplateSubmission) ([]map[string]any, error) {
	if strings.ToUpper(t.Category) == "AUTHENTICATION" {
		return whatsapp.BuildAuthComponents(t), nil
	}
	return whatsapp.BuildStandardComponents(t)
}
