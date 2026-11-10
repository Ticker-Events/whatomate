package msg91

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// SubmitTemplate creates a WhatsApp template via MSG91 client-panel API.
func (c *Client) SubmitTemplate(ctx context.Context, account *whatsapp.Account, template *whatsapp.TemplateSubmission) (string, error) {
	authKey, integrated := accountFromWA(account)
	if integrated == "" {
		return "", fmt.Errorf("msg91 integrated_number (phone_id) is required")
	}
	if template == nil {
		return "", fmt.Errorf("template is required")
	}

	components := buildCreateTemplateComponents(template)
	payload := map[string]any{
		"integrated_number": normalizePhone(integrated),
		"template_name":     template.Name,
		"language":          template.Language,
		"category":          template.Category,
		"components":        components,
	}

	respBody, err := c.doRequest(ctx, http.MethodPost, clientTemplatePath, payload, authKey)
	if err != nil {
		return "", fmt.Errorf("failed to submit template via msg91: %w", err)
	}

	var result struct {
		Data struct {
			TemplateID string `json:"template_id"`
			Message    string `json:"message"`
		} `json:"data"`
	}
	_ = json.Unmarshal(respBody, &result)
	if result.Data.TemplateID != "" {
		return result.Data.TemplateID, nil
	}
	if template.MetaTemplateID != "" {
		return template.MetaTemplateID, nil
	}
	// Creation is async; return name as a stable handle when id is absent.
	return template.Name, nil
}

func buildCreateTemplateComponents(template *whatsapp.TemplateSubmission) []map[string]any {
	components := make([]map[string]any, 0, 4)

	if template.HeaderType != "" && !strings.EqualFold(template.HeaderType, "NONE") {
		header := map[string]any{
			"type":   "HEADER",
			"format": strings.ToUpper(template.HeaderType),
		}
		if strings.EqualFold(template.HeaderType, "TEXT") {
			header["text"] = template.HeaderContent
		}
		components = append(components, header)
	}

	if template.BodyContent != "" {
		components = append(components, map[string]any{
			"type": "BODY",
			"text": template.BodyContent,
		})
	}
	if template.FooterContent != "" {
		components = append(components, map[string]any{
			"type": "FOOTER",
			"text": template.FooterContent,
		})
	}
	if len(template.Buttons) > 0 {
		components = append(components, map[string]any{
			"type":    "BUTTONS",
			"buttons": template.Buttons,
		})
	}
	return components
}

// FetchTemplates lists templates for the integrated number.
func (c *Client) FetchTemplates(ctx context.Context, account *whatsapp.Account) ([]whatsapp.MetaTemplate, error) {
	authKey, integrated := accountFromWA(account)
	if integrated == "" {
		return nil, fmt.Errorf("msg91 integrated_number (phone_id) is required")
	}

	path := getTemplatesPath + url.PathEscape(normalizePhone(integrated))
	values := url.Values{}
	values.Set("pagination", "true")
	values.Set("page_size", "100")
	values.Set("page_num", "1")
	path = path + "?" + values.Encode()

	respBody, err := c.doRequest(ctx, http.MethodGet, path, nil, authKey)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch templates via msg91: %w", err)
	}

	templates := parseTemplateList(respBody)
	return templates, nil
}

func parseTemplateList(body []byte) []whatsapp.MetaTemplate {
	out := make([]whatsapp.MetaTemplate, 0)

	// Try common envelopes: {data:[...]}, {data:{data:[...]}}, or bare array.
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) == nil && len(envelope.Data) > 0 {
		if parsed := decodeTemplateSlice(envelope.Data); len(parsed) > 0 {
			return parsed
		}
		var nested struct {
			Data []json.RawMessage `json:"data"`
		}
		if json.Unmarshal(envelope.Data, &nested) == nil && len(nested.Data) > 0 {
			for _, raw := range nested.Data {
				if t, ok := decodeOneTemplate(raw); ok {
					out = append(out, t)
				}
			}
			return out
		}
	}
	if parsed := decodeTemplateSlice(body); len(parsed) > 0 {
		return parsed
	}
	return out
}

func decodeTemplateSlice(raw json.RawMessage) []whatsapp.MetaTemplate {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	out := make([]whatsapp.MetaTemplate, 0, len(arr))
	for _, item := range arr {
		if t, ok := decodeOneTemplate(item); ok {
			out = append(out, t)
		}
	}
	return out
}

func decodeOneTemplate(raw json.RawMessage) (whatsapp.MetaTemplate, bool) {
	var t whatsapp.MetaTemplate
	if json.Unmarshal(raw, &t) == nil && (t.Name != "" || t.ID != "") {
		return t, true
	}
	// Loose MSG91 field names
	var loose struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Template string `json:"template_name"`
		Language string `json:"language"`
		Lang     string `json:"template_language"`
		Category string `json:"category"`
		Status   string `json:"status"`
		TStatus  string `json:"template_status"`
	}
	if json.Unmarshal(raw, &loose) != nil {
		return whatsapp.MetaTemplate{}, false
	}
	name := loose.Name
	if name == "" {
		name = loose.Template
	}
	if name == "" && loose.ID == "" {
		return whatsapp.MetaTemplate{}, false
	}
	lang := loose.Language
	if lang == "" {
		lang = loose.Lang
	}
	status := loose.Status
	if status == "" {
		status = loose.TStatus
	}
	return whatsapp.MetaTemplate{
		ID:       loose.ID,
		Name:     name,
		Language: lang,
		Category: loose.Category,
		Status:   status,
	}, true
}

// DeleteTemplate deletes a template by name via MSG91.
func (c *Client) DeleteTemplate(ctx context.Context, account *whatsapp.Account, templateName string) error {
	authKey, integrated := accountFromWA(account)
	if integrated == "" {
		return fmt.Errorf("msg91 integrated_number (phone_id) is required")
	}
	if templateName == "" {
		return fmt.Errorf("template name is required")
	}
	values := url.Values{}
	values.Set("integrated_number", normalizePhone(integrated))
	values.Set("template_name", templateName)
	path := clientTemplatePath + "?" + values.Encode()
	_, err := c.doRequest(ctx, http.MethodDelete, path, nil, authKey)
	return err
}
