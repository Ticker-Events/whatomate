package aisensy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

const (
	templateListFields = "id,name,language,category,status,components,quality_score,quality_rating"
	templatePageLimit  = "100"
	templateMaxPages   = 50
)

type templateListPage struct {
	Data   []whatsapp.MetaTemplate `json:"data"`
	Paging struct {
		Cursors struct {
			After string `json:"after"`
		} `json:"cursors"`
		Next string `json:"next"`
	} `json:"paging"`
}

// FetchTemplates fetches all templates for the account via AiSensy.
// Prefer the Meta-compatible /api/message-templates list (cursor pagination).
// Fall back to /get-templates when that endpoint is unavailable. Both paths
// follow paging.next / cursors.after so sync is not truncated at Meta's
// default ~25-item page size.
func (c *Client) FetchTemplates(ctx context.Context, account *whatsapp.Account) ([]whatsapp.MetaTemplate, error) {
	directRoot := strings.TrimSuffix(c.baseURL, "/api")
	primary := c.messageTemplatesListURL("")
	templates, err := c.fetchTemplatePages(ctx, account, primary, c.baseURL+"/message-templates/")
	if err == nil {
		return templates, nil
	}

	c.Log.Warn("AiSensy message-templates list failed; falling back to get-templates",
		"error", err)

	fallback := fmt.Sprintf("%s/get-templates?limit=%s", directRoot, templatePageLimit)
	templates, fallbackErr := c.fetchTemplatePages(ctx, account, fallback, directRoot+"/get-templates")
	if fallbackErr != nil {
		return nil, fmt.Errorf("failed to fetch templates via aisensy: message-templates: %v; get-templates: %w", err, fallbackErr)
	}
	return templates, nil
}

func (c *Client) messageTemplatesListURL(after string) string {
	values := url.Values{}
	values.Set("fields", templateListFields)
	values.Set("limit", templatePageLimit)
	if after != "" {
		values.Set("after", after)
	}
	return c.baseURL + "/message-templates/?" + values.Encode()
}

func (c *Client) fetchTemplatePages(
	ctx context.Context,
	account *whatsapp.Account,
	startURL string,
	listPathPrefix string,
) ([]whatsapp.MetaTemplate, error) {
	all := make([]whatsapp.MetaTemplate, 0)
	nextURL := startURL
	pageCount := 0

	for nextURL != "" && pageCount < templateMaxPages {
		respBody, err := c.doRequest(ctx, http.MethodGet, nextURL, nil, account)
		if err != nil {
			return nil, err
		}

		page, err := parseTemplateListPage(respBody)
		if err != nil {
			return nil, err
		}

		all = append(all, page.Data...)
		nextURL = c.nextTemplatePageURL(page, listPathPrefix)
		pageCount++
	}

	if nextURL != "" {
		return nil, fmt.Errorf("template fetch exceeded max pages (%d); aborting incomplete sync", templateMaxPages)
	}

	c.Log.Info("Fetched templates from AiSensy", "count", len(all), "pages", pageCount, "endpoint", listPathPrefix)
	return all, nil
}

func parseTemplateListPage(respBody []byte) (templateListPage, error) {
	var page templateListPage
	if err := json.Unmarshal(respBody, &page); err == nil && page.Data != nil {
		return page, nil
	}

	// Bare array response (no paging).
	var templates []whatsapp.MetaTemplate
	if err := json.Unmarshal(respBody, &templates); err != nil {
		return templateListPage{}, fmt.Errorf("failed to parse templates response: %w", err)
	}
	return templateListPage{Data: templates}, nil
}

// nextTemplatePageURL resolves the next page URL. AiSensy may proxy Meta's
// paging.next (graph.facebook.com); rewrite those onto the AiSensy list path
// while preserving the after cursor.
func (c *Client) nextTemplatePageURL(page templateListPage, listPathPrefix string) string {
	after := page.Paging.Cursors.After
	if page.Paging.Next != "" {
		if rewritten := c.rewriteTemplatePagingURL(page.Paging.Next, listPathPrefix); rewritten != "" {
			return rewritten
		}
	}
	if after == "" {
		return ""
	}
	return c.buildTemplatePageURL(listPathPrefix, after)
}

func (c *Client) rewriteTemplatePagingURL(next, listPathPrefix string) string {
	if strings.HasPrefix(next, c.baseURL) || strings.Contains(next, "aisensy.com") {
		return next
	}

	parsed, err := url.Parse(next)
	if err != nil {
		return next
	}
	after := parsed.Query().Get("after")
	if after == "" {
		// Absolute non-AiSensy URL without an after cursor cannot be replayed.
		return next
	}
	return c.buildTemplatePageURL(listPathPrefix, after)
}

func (c *Client) buildTemplatePageURL(listPathPrefix, after string) string {
	if strings.Contains(listPathPrefix, "message-templates") {
		return c.messageTemplatesListURL(after)
	}

	values := url.Values{}
	values.Set("limit", templatePageLimit)
	if after != "" {
		values.Set("after", after)
	}
	return listPathPrefix + "?" + values.Encode()
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
