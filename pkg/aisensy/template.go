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

	payload := map[string]any{
		"name":       template.Name,
		"category":   template.Category,
		"language":   template.Language,
		"components": buildTemplateComponents(template),
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
// components array for the AiSensy API.
func buildTemplateComponents(t *whatsapp.TemplateSubmission) []map[string]any {
	var components []map[string]any

	if t.HeaderType != "" && t.HeaderType != "NONE" {
		header := map[string]any{"type": "HEADER", "format": t.HeaderType}
		if t.HeaderType == "TEXT" && t.HeaderContent != "" {
			header["text"] = t.HeaderContent
		}
		components = append(components, header)
	}

	if t.BodyContent != "" {
		body := map[string]any{"type": "BODY", "text": t.BodyContent}
		components = append(components, body)
	}

	if t.FooterContent != "" {
		components = append(components, map[string]any{
			"type": "FOOTER",
			"text": t.FooterContent,
		})
	}

	if len(t.Buttons) > 0 {
		buttons := make([]any, 0, len(t.Buttons))
		for _, btn := range t.Buttons {
			buttons = append(buttons, btn)
		}
		components = append(components, map[string]any{
			"type":    "BUTTONS",
			"buttons": buttons,
		})
	}

	return components
}
