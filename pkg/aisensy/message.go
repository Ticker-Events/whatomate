package aisensy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

// sendMessage posts a Meta-format message payload to AiSensy's /messages/ endpoint.
func (c *Client) sendMessage(ctx context.Context, account *whatsapp.Account, payload map[string]any) (string, error) {
	url := c.messagesURL(account)

	respBody, err := c.doRequest(ctx, http.MethodPost, url, payload, account)
	if err != nil {
		return "", err
	}

	var resp whatsapp.MetaAPIResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("failed to parse aisensy message response: %w", err)
	}
	if len(resp.Messages) == 0 {
		return "", fmt.Errorf("no message ID in aisensy response")
	}

	return resp.Messages[0].ID, nil
}

// SendTextMessage sends a plain text message via AiSensy.
func (c *Client) SendTextMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, text string, replyToMsgID ...string) (string, error) {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"type":              "text",
		"text": map[string]any{
			"preview_url": false,
			"body":        text,
		},
	}
	rcpt.SetOnPayload(payload)

	if len(replyToMsgID) > 0 && replyToMsgID[0] != "" {
		payload["context"] = map[string]any{"message_id": replyToMsgID[0]}
	}

	return c.sendMessage(ctx, account, payload)
}

// mediaRef builds an image/video/audio/document object. AiSensy requires a
// publicly accessible HTTPS URL (link); Meta-style media IDs are only used
// when the ref is not a URL.
func mediaRef(ref string) map[string]any {
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		return map[string]any{"link": ref}
	}
	return map[string]any{"id": ref}
}

// SendImageMessage sends an image message via AiSensy.
func (c *Client) SendImageMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, mediaID, caption string) (string, error) {
	img := mediaRef(mediaID)
	if caption != "" {
		img["caption"] = caption
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"type":              "image",
		"image":             img,
	}
	rcpt.SetOnPayload(payload)
	return c.sendMessage(ctx, account, payload)
}

// SendVideoMessage sends a video message via AiSensy.
func (c *Client) SendVideoMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, mediaID, caption string) (string, error) {
	vid := mediaRef(mediaID)
	if caption != "" {
		vid["caption"] = caption
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"type":              "video",
		"video":             vid,
	}
	rcpt.SetOnPayload(payload)
	return c.sendMessage(ctx, account, payload)
}

// SendAudioMessage sends an audio message via AiSensy.
func (c *Client) SendAudioMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, mediaID string) (string, error) {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"type":              "audio",
		"audio":             mediaRef(mediaID),
	}
	rcpt.SetOnPayload(payload)
	return c.sendMessage(ctx, account, payload)
}

// SendDocumentMessage sends a document message via AiSensy.
func (c *Client) SendDocumentMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, mediaID, filename, caption string) (string, error) {
	doc := mediaRef(mediaID)
	if filename != "" {
		doc["filename"] = filename
	}
	if caption != "" {
		doc["caption"] = caption
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"type":              "document",
		"document":          doc,
	}
	rcpt.SetOnPayload(payload)
	return c.sendMessage(ctx, account, payload)
}

// SendInteractiveButtons sends an interactive message with buttons or a list.
// If buttons ≤ 3, sends button format; if 4-10, sends list format.
func (c *Client) SendInteractiveButtons(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText string, buttons []whatsapp.Button) (string, error) {
	if len(buttons) == 0 {
		return "", fmt.Errorf("at least one button is required")
	}
	if len(buttons) > 10 {
		return "", fmt.Errorf("maximum 10 buttons allowed")
	}

	var interactive map[string]any
	if len(buttons) <= 3 {
		btnList := make([]map[string]any, 0, len(buttons))
		for _, btn := range buttons {
			title := btn.Title
			if len(title) > 20 {
				title = title[:20]
			}
			btnList = append(btnList, map[string]any{
				"type":  "reply",
				"reply": map[string]any{"id": btn.ID, "title": title},
			})
		}
		interactive = map[string]any{
			"type":   "button",
			"body":   map[string]any{"text": bodyText},
			"action": map[string]any{"buttons": btnList},
		}
	} else {
		rows := make([]map[string]any, 0, len(buttons))
		for _, btn := range buttons {
			title := btn.Title
			if len(title) > 24 {
				title = title[:24]
			}
			rows = append(rows, map[string]any{"id": btn.ID, "title": title})
		}
		interactive = map[string]any{
			"type": "list",
			"body": map[string]any{"text": bodyText},
			"action": map[string]any{
				"button": "Select an option",
				"sections": []map[string]any{
					{"title": "Options", "rows": rows},
				},
			},
		}
	}

	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"type":              "interactive",
		"interactive":       interactive,
	}
	rcpt.SetOnPayload(payload)
	return c.sendMessage(ctx, account, payload)
}

// SendCTAURLButton sends an interactive CTA URL button message via AiSensy.
func (c *Client) SendCTAURLButton(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText, buttonText, url string) (string, error) {
	if len(buttonText) > 20 {
		buttonText = buttonText[:20]
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"type":              "interactive",
		"interactive": map[string]any{
			"type": "cta_url",
			"body": map[string]any{"text": bodyText},
			"action": map[string]any{
				"name": "cta_url",
				"parameters": map[string]any{
					"display_text": buttonText,
					"url":          url,
				},
			},
		},
	}
	rcpt.SetOnPayload(payload)
	return c.sendMessage(ctx, account, payload)
}

// SendVoiceCallButton sends an interactive voice_call button message via AiSensy.
func (c *Client) SendVoiceCallButton(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText, displayText string, ttlMinutes int, p string) (string, error) {
	if len(displayText) > 20 {
		displayText = displayText[:20]
	}
	parameters := map[string]any{"display_text": displayText}
	if ttlMinutes > 0 {
		parameters["ttl_minutes"] = ttlMinutes
	}
	if p != "" {
		parameters["payload"] = p
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"type":              "interactive",
		"interactive": map[string]any{
			"type": "voice_call",
			"body": map[string]any{"text": bodyText},
			"action": map[string]any{
				"name":       "voice_call",
				"parameters": parameters,
			},
		},
	}
	rcpt.SetOnPayload(payload)
	return c.sendMessage(ctx, account, payload)
}

// SendTemplateMessage sends a template message via AiSensy.
func (c *Client) SendTemplateMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, templateName, languageCode string, components []map[string]any) (string, error) {
	tmpl := map[string]any{
		"name":     templateName,
		"language": map[string]any{"code": languageCode},
	}
	if len(components) > 0 {
		tmpl["components"] = components
	}

	payload := map[string]any{
		"messaging_product": "whatsapp",
		"type":              "template",
		"template":          tmpl,
	}
	rcpt.SetOnPayload(payload)
	return c.sendMessage(ctx, account, payload)
}

// SendFlowMessage sends a WhatsApp Flow interactive message via AiSensy.
func (c *Client) SendFlowMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, flowID, headerText, bodyText, ctaText, flowToken, firstScreen string) (string, error) {
	if ctaText == "" {
		ctaText = "Open"
	}
	if len(ctaText) > 20 {
		ctaText = ctaText[:20]
	}
	if flowToken == "" {
		flowToken = fmt.Sprintf("flow_%s", account.AiSensyProjectID)
	}
	if firstScreen == "" {
		firstScreen = "FIRST_SCREEN"
	}

	interactive := map[string]any{
		"type": "flow",
		"body": map[string]any{"text": bodyText},
		"action": map[string]any{
			"name": "flow",
			"parameters": map[string]any{
				"flow_message_version": "3",
				"flow_token":           flowToken,
				"flow_id":              flowID,
				"flow_cta":             ctaText,
				"flow_action":          "navigate",
				"flow_action_payload":  map[string]any{"screen": firstScreen},
			},
		},
	}
	if headerText != "" {
		interactive["header"] = map[string]any{"type": "text", "text": headerText}
	}

	payload := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"type":              "interactive",
		"interactive":       interactive,
	}
	rcpt.SetOnPayload(payload)
	return c.sendMessage(ctx, account, payload)
}

// MarkMessageRead marks a message as read via AiSensy.
func (c *Client) MarkMessageRead(ctx context.Context, account *whatsapp.Account, messageID string) error {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        messageID,
	}

	url := c.messagesURL(account)
	_, err := c.doRequest(ctx, http.MethodPost, url, payload, account)
	if err != nil {
		c.Log.Error("Failed to mark message as read via AiSensy", "error", err, "message_id", messageID)
	}
	return err
}
