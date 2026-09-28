package aisensy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"

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
// Meta rejects application/json on this endpoint and requires the definition
// as a multipart file field named "file".
func (c *Client) UpdateFlowJSON(ctx context.Context, account *whatsapp.Account, flowID string, flowJSON *whatsapp.FlowJSON) error {
	url := fmt.Sprintf("%s/flows/%s/assets/", c.baseURL, flowID)

	body, contentType, err := encodeFlowJSONUpload(flowJSON)
	if err != nil {
		return err
	}

	respBody, err := c.postFlowAsset(ctx, url, body, contentType, account)
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

func encodeFlowJSONUpload(flowJSON *whatsapp.FlowJSON) ([]byte, string, error) {
	jsonBytes, err := json.Marshal(flowJSON)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal flow JSON: %w", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="flow.json"`)
	h.Set("Content-Type", "application/json")
	part, err := writer.CreatePart(h)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(jsonBytes); err != nil {
		return nil, "", fmt.Errorf("failed to write flow JSON: %w", err)
	}
	if err := writer.WriteField("name", "flow.json"); err != nil {
		return nil, "", fmt.Errorf("failed to write name field: %w", err)
	}
	if err := writer.WriteField("asset_type", "FLOW_JSON"); err != nil {
		return nil, "", fmt.Errorf("failed to write asset_type field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return buf.Bytes(), writer.FormDataContentType(), nil
}

// postFlowAsset uploads the flow JSON file. On 401 it refreshes the token once
// and retries, matching doRequest.
func (c *Client) postFlowAsset(ctx context.Context, url string, body []byte, contentType string, account *whatsapp.Account) ([]byte, error) {
	token, err := c.getToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("failed to get aisensy token: %w", err)
	}

	respBody, statusCode, err := c.sendFlowAsset(ctx, url, body, contentType, token)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusUnauthorized {
		_, _, projectID, _ := accountFromWA(account)
		c.invalidateToken(projectID)

		token, err = c.GenerateToken(ctx, account.AiSensyEmail, account.AiSensyPassword, account.AiSensyProjectID)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh aisensy token: %w", err)
		}
		c.cacheAndPersistToken(ctx, account, account.AiSensyProjectID, token)

		respBody, statusCode, err = c.sendFlowAsset(ctx, url, body, contentType, token)
		if err != nil {
			return nil, err
		}
	}

	if statusCode < 200 || statusCode >= 300 {
		return nil, parseAPIError(statusCode, respBody)
	}

	return respBody, nil
}

func (c *Client) sendFlowAsset(ctx context.Context, url string, body []byte, contentType, token string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
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
