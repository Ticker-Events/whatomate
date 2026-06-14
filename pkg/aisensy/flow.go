package aisensy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// CreateFlow creates a new WhatsApp Flow via AiSensy.
func (c *Client) CreateFlow(ctx context.Context, account *whatsapp.Account, name string, categories []string) (string, error) {
	url := fmt.Sprintf("%s/flows/", c.baseURL)

	payload := map[string]any{
		"name":       name,
		"categories": categories,
	}

	respBody, err := c.doRequest(ctx, http.MethodPost, url, payload, account)
	if err != nil {
		return "", fmt.Errorf("failed to create flow via aisensy: %w", err)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse create flow response: %w", err)
	}

	c.Log.Info("Flow created via AiSensy", "flow_id", result.ID, "name", name)
	return result.ID, nil
}

// UpdateFlowJSON updates the JSON definition of an existing flow via AiSensy.
func (c *Client) UpdateFlowJSON(ctx context.Context, account *whatsapp.Account, flowID string, flowJSON *whatsapp.FlowJSON) error {
	url := fmt.Sprintf("%s/flows/%s/assets/", c.baseURL, flowID)

	payload := map[string]any{
		"asset_type": "FLOW_JSON",
		"name":       "flow.json",
		"data":       flowJSON,
	}

	respBody, err := c.doRequest(ctx, http.MethodPost, url, payload, account)
	if err != nil {
		return fmt.Errorf("failed to update flow JSON via aisensy: %w", err)
	}

	var result struct {
		Success          bool `json:"success"`
		ValidationErrors any  `json:"validation_errors,omitempty"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse update flow response: %w", err)
	}

	if !result.Success {
		if result.ValidationErrors != nil {
			return fmt.Errorf("flow validation errors: %v", result.ValidationErrors)
		}
		return fmt.Errorf("failed to update flow JSON via aisensy")
	}

	c.Log.Info("Flow JSON updated via AiSensy", "flow_id", flowID)
	return nil
}

// PublishFlow publishes a draft flow via AiSensy.
func (c *Client) PublishFlow(ctx context.Context, account *whatsapp.Account, flowID string) error {
	url := fmt.Sprintf("%s/flows/%s/publish/", c.baseURL, flowID)

	respBody, err := c.doRequest(ctx, http.MethodPost, url, nil, account)
	if err != nil {
		return fmt.Errorf("failed to publish flow via aisensy: %w", err)
	}

	var result struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse publish flow response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("failed to publish flow via aisensy")
	}

	c.Log.Info("Flow published via AiSensy", "flow_id", flowID)
	return nil
}

// DeprecateFlow deprecates a published flow via AiSensy.
func (c *Client) DeprecateFlow(ctx context.Context, account *whatsapp.Account, flowID string) error {
	url := fmt.Sprintf("%s/flows/%s/deprecate/", c.baseURL, flowID)

	respBody, err := c.doRequest(ctx, http.MethodPost, url, nil, account)
	if err != nil {
		return fmt.Errorf("failed to deprecate flow via aisensy: %w", err)
	}

	var result struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse deprecate flow response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("failed to deprecate flow via aisensy")
	}

	c.Log.Info("Flow deprecated via AiSensy", "flow_id", flowID)
	return nil
}

// DeleteFlow deletes a flow via AiSensy.
func (c *Client) DeleteFlow(ctx context.Context, account *whatsapp.Account, flowID string) error {
	url := fmt.Sprintf("%s/flows/%s/", c.baseURL, flowID)

	_, err := c.doRequest(ctx, http.MethodDelete, url, nil, account)
	if err != nil {
		return fmt.Errorf("failed to delete flow via aisensy: %w", err)
	}

	c.Log.Info("Flow deleted via AiSensy", "flow_id", flowID)
	return nil
}

// GetFlow fetches metadata for a single flow via AiSensy.
func (c *Client) GetFlow(ctx context.Context, account *whatsapp.Account, flowID string) (*whatsapp.FlowGetResponse, error) {
	url := fmt.Sprintf("%s/flows/%s/", c.baseURL, flowID)

	respBody, err := c.doRequest(ctx, http.MethodGet, url, nil, account)
	if err != nil {
		return nil, fmt.Errorf("failed to get flow via aisensy: %w", err)
	}

	var result whatsapp.FlowGetResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse get flow response: %w", err)
	}

	return &result, nil
}

// GetFlowAssets fetches the JSON assets for a flow via AiSensy.
func (c *Client) GetFlowAssets(ctx context.Context, account *whatsapp.Account, flowID string) (*whatsapp.FlowJSON, error) {
	url := fmt.Sprintf("%s/flows/%s/assets/", c.baseURL, flowID)

	respBody, err := c.doRequest(ctx, http.MethodGet, url, nil, account)
	if err != nil {
		return nil, fmt.Errorf("failed to get flow assets via aisensy: %w", err)
	}

	var result struct {
		Data []struct {
			Name      string          `json:"name"`
			AssetType string          `json:"asset_type"`
			Data      json.RawMessage `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse flow assets response: %w", err)
	}

	for _, asset := range result.Data {
		if asset.AssetType == "FLOW_JSON" {
			var flowJSON whatsapp.FlowJSON
			if err := json.Unmarshal(asset.Data, &flowJSON); err != nil {
				return nil, fmt.Errorf("failed to parse flow JSON asset: %w", err)
			}
			return &flowJSON, nil
		}
	}

	return nil, nil
}

// ListFlows lists all flows for the AiSensy project.
func (c *Client) ListFlows(ctx context.Context, account *whatsapp.Account) ([]whatsapp.FlowGetResponse, error) {
	url := fmt.Sprintf("%s/flows/", c.baseURL)

	respBody, err := c.doRequest(ctx, http.MethodGet, url, nil, account)
	if err != nil {
		return nil, fmt.Errorf("failed to list flows via aisensy: %w", err)
	}

	var result struct {
		Data []whatsapp.FlowGetResponse `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse list flows response: %w", err)
	}

	return result.Data, nil
}
