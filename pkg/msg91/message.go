package msg91

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

func (c *Client) sendOutbound(ctx context.Context, account *whatsapp.Account, payload map[string]any) (string, error) {
	authKey, _ := accountFromWA(account)
	respBody, err := c.doRequest(ctx, http.MethodPost, outboundPath, payload, authKey)
	if err != nil {
		return "", err
	}
	return parseSendID(respBody)
}

func (c *Client) baseOutbound(account *whatsapp.Account, rcpt whatsapp.Recipient, contentType string) (map[string]any, error) {
	_, integrated := accountFromWA(account)
	if integrated == "" {
		return nil, fmt.Errorf("msg91 integrated_number (phone_id) is required")
	}
	if rcpt.Phone == "" {
		return nil, fmt.Errorf("recipient phone number is required")
	}
	return map[string]any{
		"integrated_number": normalizePhone(integrated),
		"recipient_number":  normalizePhone(rcpt.Phone),
		"content_type":      contentType,
	}, nil
}

// SendTextMessage sends a session text message via MSG91.
func (c *Client) SendTextMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, text string, replyToMsgID ...string) (string, error) {
	payload, err := c.baseOutbound(account, rcpt, "text")
	if err != nil {
		return "", err
	}
	payload["text"] = map[string]any{"body": text}
	if len(replyToMsgID) > 0 && replyToMsgID[0] != "" {
		payload["context"] = map[string]any{"message_id": replyToMsgID[0]}
	}
	return c.sendOutbound(ctx, account, payload)
}

func mediaObject(ref, caption, filename string) map[string]any {
	obj := map[string]any{}
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		obj["link"] = ref
	} else {
		obj["id"] = ref
	}
	if caption != "" {
		obj["caption"] = caption
	}
	if filename != "" {
		obj["filename"] = filename
	}
	return obj
}

// SendImageMessage sends an image session message via MSG91.
func (c *Client) SendImageMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, mediaID, caption string) (string, error) {
	payload, err := c.baseOutbound(account, rcpt, "image")
	if err != nil {
		return "", err
	}
	payload["image"] = mediaObject(mediaID, caption, "")
	return c.sendOutbound(ctx, account, payload)
}

// SendVideoMessage sends a video session message via MSG91.
func (c *Client) SendVideoMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, mediaID, caption string) (string, error) {
	payload, err := c.baseOutbound(account, rcpt, "video")
	if err != nil {
		return "", err
	}
	payload["video"] = mediaObject(mediaID, caption, "")
	return c.sendOutbound(ctx, account, payload)
}

// SendAudioMessage sends an audio session message via MSG91.
func (c *Client) SendAudioMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, mediaID string) (string, error) {
	payload, err := c.baseOutbound(account, rcpt, "audio")
	if err != nil {
		return "", err
	}
	payload["audio"] = mediaObject(mediaID, "", "")
	return c.sendOutbound(ctx, account, payload)
}

// SendDocumentMessage sends a document session message via MSG91.
func (c *Client) SendDocumentMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, mediaID, filename, caption string) (string, error) {
	payload, err := c.baseOutbound(account, rcpt, "document")
	if err != nil {
		return "", err
	}
	payload["document"] = mediaObject(mediaID, caption, filename)
	return c.sendOutbound(ctx, account, payload)
}

// SendInteractiveButtons sends reply buttons (≤3) or a list (4–10) via MSG91.
func (c *Client) SendInteractiveButtons(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText string, buttons []whatsapp.Button, headerImageURL ...string) (string, error) {
	if len(buttons) == 0 {
		return "", fmt.Errorf("at least one button is required")
	}
	if len(buttons) > 10 {
		return "", fmt.Errorf("maximum 10 buttons allowed")
	}

	payload, err := c.baseOutbound(account, rcpt, "interactive")
	if err != nil {
		return "", err
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
	if len(headerImageURL) > 0 && headerImageURL[0] != "" {
		interactive["header"] = map[string]any{
			"type":  "image",
			"image": map[string]any{"link": headerImageURL[0]},
		}
	}
	payload["interactive"] = interactive
	return c.sendOutbound(ctx, account, payload)
}

// SendLocationRequest sends a location request interactive message via MSG91.
func (c *Client) SendLocationRequest(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText string) (string, error) {
	if strings.TrimSpace(bodyText) == "" {
		return "", fmt.Errorf("body text is required")
	}
	payload, err := c.baseOutbound(account, rcpt, "interactive")
	if err != nil {
		return "", err
	}
	payload["interactive"] = map[string]any{
		"type":   "location_request_message",
		"body":   map[string]any{"text": bodyText},
		"action": map[string]any{"name": "send_location"},
	}
	return c.sendOutbound(ctx, account, payload)
}

// SendInteractiveList sends an interactive list message via MSG91.
func (c *Client) SendInteractiveList(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText string, params whatsapp.ListMessageParams) (string, error) {
	interactive, err := whatsapp.ListMessageInteractive(bodyText, params)
	if err != nil {
		return "", err
	}
	payload, err := c.baseOutbound(account, rcpt, "interactive")
	if err != nil {
		return "", err
	}
	payload["interactive"] = interactive
	return c.sendOutbound(ctx, account, payload)
}

// SendAddressMessage is not supported by MSG91 WhatsApp APIs.
func (c *Client) SendAddressMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText string, params whatsapp.AddressMessageParams) (string, error) {
	return "", ErrNotSupported
}

// SendInteractiveCarousel is not supported by MSG91 WhatsApp APIs.
func (c *Client) SendInteractiveCarousel(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText string, params whatsapp.CarouselMessageParams) (string, error) {
	return "", ErrNotSupported
}

// SendCTAURLButton is not exposed as a dedicated MSG91 endpoint.
func (c *Client) SendCTAURLButton(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText, buttonText, url string) (string, error) {
	return "", ErrNotSupported
}

// SendVoiceCallButton is not exposed as a dedicated MSG91 session endpoint.
func (c *Client) SendVoiceCallButton(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, bodyText, displayText string, ttlMinutes int, payload string) (string, error) {
	return "", ErrNotSupported
}

// SendTemplateMessage sends a WhatsApp template via MSG91 bulk outbound API.
func (c *Client) SendTemplateMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, templateName, languageCode string, components []map[string]any) (string, error) {
	authKey, integrated := accountFromWA(account)
	if integrated == "" {
		return "", fmt.Errorf("msg91 integrated_number (phone_id) is required")
	}
	if rcpt.Phone == "" {
		return "", fmt.Errorf("recipient phone number is required")
	}

	compMap := mapTemplateComponents(components)
	body := map[string]any{
		"integrated_number": normalizePhone(integrated),
		"content_type":      "template",
		"payload": map[string]any{
			"messaging_product": "whatsapp",
			"type":              "template",
			"template": map[string]any{
				"name": templateName,
				"language": map[string]any{
					"code":   languageCode,
					"policy": "deterministic",
				},
				"to_and_components": []map[string]any{
					{
						"to":         []string{normalizePhone(rcpt.Phone)},
						"components": compMap,
					},
				},
			},
		},
	}

	respBody, err := c.doRequest(ctx, http.MethodPost, outboundBulkPath, body, authKey)
	if err != nil {
		return "", err
	}
	return parseSendID(respBody)
}

// SendFlowMessage is not supported via MSG91 public WhatsApp send APIs.
func (c *Client) SendFlowMessage(ctx context.Context, account *whatsapp.Account, rcpt whatsapp.Recipient, flowID, headerText, bodyText, ctaText, flowToken, firstScreen string, data map[string]string) (string, error) {
	return "", ErrNotSupported
}

// MarkMessageRead sends a typing indicator for the inbound message (closest MSG91 equivalent).
func (c *Client) MarkMessageRead(ctx context.Context, account *whatsapp.Account, messageID string) error {
	authKey, _ := accountFromWA(account)
	if messageID == "" {
		return fmt.Errorf("message id is required")
	}
	_, err := c.doRequest(ctx, http.MethodPost, typingPath, map[string]any{"message_id": messageID}, authKey)
	return err
}
