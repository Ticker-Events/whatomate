package aisensy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// callsURL returns the calls endpoint for the given phone ID.
func (c *Client) callsURL() string {
	return fmt.Sprintf("%s/calls/", c.baseURL)
}

// InitiateCall places an outgoing WhatsApp call via AiSensy.
func (c *Client) InitiateCall(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, sdpOffer string) (string, error) {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"action":            "connect",
		"session": map[string]string{
			"sdp_type": "offer",
			"sdp":      sdpOffer,
		},
	}
	rcpt.SetOnPayload(payload)

	respBody, err := c.doRequest(ctx, http.MethodPost, c.callsURL(), payload, account)
	if err != nil {
		return "", fmt.Errorf("failed to initiate call via aisensy: %w", err)
	}

	var resp struct {
		Calls []struct {
			ID string `json:"id"`
		} `json:"calls"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil || len(resp.Calls) == 0 {
		return "", fmt.Errorf("failed to parse call_id from aisensy response: %s", string(respBody))
	}

	c.Log.Info("Outgoing call initiated via AiSensy", "phone", rcpt.Phone, "call_id", resp.Calls[0].ID)
	return resp.Calls[0].ID, nil
}

// TerminateCall terminates an active call via AiSensy.
func (c *Client) TerminateCall(ctx context.Context, account *whatsapp.Account, callID string) error {
	payload := map[string]string{
		"messaging_product": "whatsapp",
		"call_id":           callID,
		"action":            "terminate",
	}

	_, err := c.doRequest(ctx, http.MethodPost, c.callsURL(), payload, account)
	if err != nil {
		return fmt.Errorf("failed to terminate call via aisensy: %w", err)
	}

	c.Log.Info("Call terminated via AiSensy", "call_id", callID)
	return nil
}

// PreAcceptCall sends the SDP answer as a pre-accept signal via AiSensy.
func (c *Client) PreAcceptCall(ctx context.Context, account *whatsapp.Account, callID, sdpAnswer string) error {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"call_id":           callID,
		"action":            "pre_accept",
		"session": map[string]string{
			"sdp_type": "answer",
			"sdp":      sdpAnswer,
		},
	}

	_, err := c.doRequest(ctx, http.MethodPost, c.callsURL(), payload, account)
	if err != nil {
		return fmt.Errorf("failed to pre-accept call via aisensy: %w", err)
	}

	c.Log.Info("Call pre-accepted via AiSensy", "call_id", callID)
	return nil
}

// AcceptCall accepts an incoming call via AiSensy.
func (c *Client) AcceptCall(ctx context.Context, account *whatsapp.Account, callID, sdpAnswer string) error {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"call_id":           callID,
		"action":            "accept",
		"session": map[string]string{
			"sdp_type": "answer",
			"sdp":      sdpAnswer,
		},
	}

	_, err := c.doRequest(ctx, http.MethodPost, c.callsURL(), payload, account)
	if err != nil {
		return fmt.Errorf("failed to accept call via aisensy: %w", err)
	}

	c.Log.Info("Call accepted via AiSensy", "call_id", callID)
	return nil
}

// RejectCall rejects an incoming call via AiSensy.
func (c *Client) RejectCall(ctx context.Context, account *whatsapp.Account, callID string) error {
	payload := map[string]string{
		"messaging_product": "whatsapp",
		"call_id":           callID,
		"action":            "reject",
	}

	_, err := c.doRequest(ctx, http.MethodPost, c.callsURL(), payload, account)
	if err != nil {
		return fmt.Errorf("failed to reject call via aisensy: %w", err)
	}

	c.Log.Info("Call rejected via AiSensy", "call_id", callID)
	return nil
}

// SendCallPermissionRequest sends a call_permission_request interactive message via AiSensy.
func (c *Client) SendCallPermissionRequest(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText string) (string, error) {
	if bodyText == "" {
		bodyText = "We'd like to call you to assist with your query."
	}

	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"type":              "interactive",
		"interactive": map[string]any{
			"type": "call_permission_request",
			"action": map[string]string{
				"name": "call_permission_request",
			},
			"body": map[string]string{
				"text": bodyText,
			},
		},
	}
	rcpt.SetOnPayload(payload)

	respBody, err := c.doRequest(ctx, http.MethodPost, c.messagesURL(account), payload, account)
	if err != nil {
		return "", fmt.Errorf("failed to send call permission request via aisensy: %w", err)
	}

	var resp struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(respBody, &resp); err == nil && len(resp.Messages) > 0 {
		return resp.Messages[0].ID, nil
	}

	return "", nil
}

// GetCallPermission checks the current call permission state for a user via AiSensy.
// A 404 from AiSensy means no permission record exists yet (user hasn't been asked),
// which is treated as "no_permission" rather than an error.
func (c *Client) GetCallPermission(ctx context.Context, account *whatsapp.Account, userPhone string) (string, error) {
	apiURL := fmt.Sprintf("%s/call-permissions/?user_wa_id=%s", c.baseURL, userPhone)

	token, err := c.getToken(ctx, account)
	if err != nil {
		return "", fmt.Errorf("failed to get call permission via aisensy: %w", err)
	}

	respBody, statusCode, err := c.rawRequest(ctx, http.MethodGet, apiURL, nil, token)
	if err != nil {
		return "", fmt.Errorf("failed to get call permission via aisensy: %w", err)
	}

	// 404 means AiSensy has no record for this user — they haven't been asked yet.
	if statusCode == http.StatusNotFound {
		return "no_permission", nil
	}

	if statusCode < 200 || statusCode >= 300 {
		return "", fmt.Errorf("failed to get call permission via aisensy: %w", parseAPIError(statusCode, respBody))
	}

	var resp struct {
		Permission struct {
			Status string `json:"status"`
		} `json:"permission"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("failed to parse call permission response: %w", err)
	}

	return resp.Permission.Status, nil
}
