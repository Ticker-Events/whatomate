package handlers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

const msg91WebhookSecretHeader = "X-MSG91-Secret"

// MSG91WebhookPayload is the flexible inbound/outbound webhook shape from MSG91
// WhatsApp Webhook (New). Field names vary between camelCase and snake_case.
type MSG91WebhookPayload struct {
	CompanyID         string          `json:"companyId"`
	RequestedAt       string          `json:"requestedAt"`
	CustomerNumber    string          `json:"customerNumber"`
	CustomerName      string          `json:"customerName"`
	EventName         string          `json:"eventName"`
	Direction         json.RawMessage `json:"direction"`
	Content           string          `json:"content"`
	ContentType       string          `json:"contentType"`
	RequestID         string          `json:"requestId"`
	UUID              string          `json:"uuid"`
	Reason            string          `json:"reason"`
	Button            string          `json:"button"`
	IntegratedNumber  string          `json:"integratedNumber"`
	TemplateName      string          `json:"templateName"`
	TemplateLanguage  string          `json:"templateLanguage"`
	Messages          string          `json:"messages"`
	Text              string          `json:"text"`
	Latitude          string          `json:"latitude"`
	Longitude         string          `json:"longitude"`
	Filename          string          `json:"filename"`
	Reaction          string          `json:"reaction"`
	ReplyMsgID        string          `json:"replyMsgId"`
	TS                string          `json:"ts"`
	Contacts          string          `json:"contacts"`

	// snake_case aliases seen in older webhook docs
	CustomerNumberSnake   string `json:"customer_number"`
	EventNameSnake        string `json:"event_name"`
	IntegratedNumberSnake string `json:"integrated_number"`
	RequestIDSnake        string `json:"request_id"`
	MessageUUID           string `json:"message_uuid"`
	StatusSnake           string `json:"status"`
	DirectionSnake        string `json:"direction_label"`
}

// MSG91WebhookHandler processes MSG91 WhatsApp webhooks (inbound + delivery reports).
func (a *App) MSG91WebhookHandler(r *fastglue.Request) error {
	body := r.RequestCtx.PostBody()

	var payload MSG91WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		a.Log.Error("Failed to parse MSG91 webhook payload", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid payload", nil, "")
	}

	integrated := msg91FirstNonEmpty(payload.IntegratedNumber, payload.IntegratedNumberSnake)
	if integrated == "" {
		// Some inbound samples omit integratedNumber; try nested messages for "to".
		a.Log.Debug("MSG91 webhook missing integratedNumber; attempting nested parse")
	}

	account, err := a.resolveMSG91Account(integrated)
	if err != nil || account == nil {
		a.Log.Warn("MSG91 webhook account not found", "integrated_number", integrated, "error", err)
		// Return 200 to avoid MSG91 auto-pause on 4xx when misconfigured numbers arrive.
		r.RequestCtx.SetStatusCode(fasthttp.StatusOK)
		return nil
	}

	// Per-account webhook secret (configured as X-MSG91-Secret in MSG91 dashboard).
	if expected := strings.TrimSpace(account.MSG91WebhookSecret); expected != "" {
		got := string(r.RequestCtx.Request.Header.Peek(msg91WebhookSecretHeader))
		if got == "" {
			got = string(r.RequestCtx.Request.Header.Peek("x-msg91-secret"))
		}
		if got != expected {
			a.Log.Warn("MSG91 webhook rejected: invalid or missing secret header",
				"phone_id", account.PhoneID)
			return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Invalid webhook secret", nil, "")
		}
	}

	customer := msg91FirstNonEmpty(payload.CustomerNumber, payload.CustomerNumberSnake)
	eventName := strings.ToLower(msg91FirstNonEmpty(payload.EventName, payload.EventNameSnake, payload.StatusSnake))
	uuid := msg91FirstNonEmpty(payload.UUID, payload.MessageUUID)
	requestID := msg91FirstNonEmpty(payload.RequestID, payload.RequestIDSnake)
	direction := parseMSG91Direction(payload.Direction, payload.DirectionSnake)

	a.Log.Info("MSG91 webhook received",
		"integrated_number", account.PhoneID,
		"event", eventName,
		"direction", direction,
		"uuid", uuid,
		"request_id", requestID,
		"customer", customer,
	)

	// Inbound messages: direction 0 / "inbound"
	if direction == "inbound" || direction == "0" {
		go a.processMSG91Inbound(account.PhoneID, payload, customer, uuid)
		r.RequestCtx.SetStatusCode(fasthttp.StatusOK)
		return nil
	}

	// Outbound delivery reports / status events
	if eventName != "" || uuid != "" || requestID != "" {
		go a.processMSG91Status(account.PhoneID, payload, eventName, uuid, requestID)
	}

	r.RequestCtx.SetStatusCode(fasthttp.StatusOK)
	return nil
}

func (a *App) resolveMSG91Account(integrated string) (*models.WhatsAppAccount, error) {
	candidates := phoneIDCandidates(integrated)
	for _, id := range candidates {
		if id == "" {
			continue
		}
		acc, err := a.getWhatsAppAccountCached(id)
		if err == nil && acc != nil && acc.IsMSG91() {
			return acc, nil
		}
	}
	// Fallback DB scan for provider=msg91 when integrated number missing or mismatched formatting.
	if integrated == "" {
		return nil, fmt.Errorf("integrated number missing")
	}
	var account models.WhatsAppAccount
	norm := normalizeMSG91Phone(integrated)
	if err := a.DB.Where("provider = ? AND REPLACE(REPLACE(phone_id, '+', ''), ' ', '') = ?", "msg91", norm).
		First(&account).Error; err != nil {
		return nil, err
	}
	a.decryptAccountSecrets(&account)
	return &account, nil
}

func phoneIDCandidates(integrated string) []string {
	if integrated == "" {
		return nil
	}
	norm := normalizeMSG91Phone(integrated)
	return []string{integrated, norm, "+" + norm}
}

func normalizeMSG91Phone(n string) string {
	n = strings.TrimSpace(n)
	n = strings.TrimPrefix(n, "+")
	n = strings.ReplaceAll(n, " ", "")
	n = strings.ReplaceAll(n, "-", "")
	return n
}

func parseMSG91Direction(raw json.RawMessage, snake string) string {
	if len(raw) > 0 {
		var asString string
		if json.Unmarshal(raw, &asString) == nil {
			return strings.ToLower(strings.TrimSpace(asString))
		}
		var asNum float64
		if json.Unmarshal(raw, &asNum) == nil {
			return strconv.FormatInt(int64(asNum), 10)
		}
	}
	return strings.ToLower(strings.TrimSpace(snake))
}

func (a *App) processMSG91Inbound(phoneNumberID string, payload MSG91WebhookPayload, customer, uuid string) {
	defer func() {
		if r := recover(); r != nil {
			a.Log.Error("Panic recovered in processMSG91Inbound", "panic", r)
		}
	}()

	msg, profileName := buildMSG91IncomingMessage(payload, customer, uuid)
	if msg.ID == "" {
		a.Log.Warn("MSG91 inbound missing message id; skipping", "customer", customer)
		return
	}
	a.processIncomingMessage(phoneNumberID, msg, profileName)
}

func buildMSG91IncomingMessage(payload MSG91WebhookPayload, customer, uuid string) (IncomingTextMessage, string) {
	profileName := strings.TrimSpace(payload.CustomerName)
	msg := IncomingTextMessage{
		From:      normalizeMSG91Phone(customer),
		ID:        stripMSG91UUIDSuffix(uuid),
		Timestamp: msg91Timestamp(payload),
		Type:      "text",
	}

	// Prefer nested Meta-shaped messages array when present (stringified JSON).
	if strings.TrimSpace(payload.Messages) != "" {
		var nested []IncomingTextMessage
		if err := json.Unmarshal([]byte(payload.Messages), &nested); err == nil && len(nested) > 0 {
			n := nested[0]
			if n.From == "" {
				n.From = msg.From
			}
			if n.ID == "" {
				n.ID = msg.ID
			}
			if n.Timestamp == "" {
				n.Timestamp = msg.Timestamp
			}
			return n, profileName
		}
	}

	contentType := strings.ToLower(strings.TrimSpace(payload.ContentType))
	textBody := msg91FirstNonEmpty(payload.Text, extractMSG91TextContent(payload.Content))

	switch {
	case payload.Button != "":
		var btn struct {
			Payload string `json:"payload"`
			Text    string `json:"text"`
		}
		_ = json.Unmarshal([]byte(payload.Button), &btn)
		msg.Type = "button"
		msg.Button = &struct {
			Text    string `json:"text"`
			Payload string `json:"payload"`
		}{Text: btn.Text, Payload: btn.Payload}
	case payload.Latitude != "" && payload.Longitude != "":
		lat, _ := strconv.ParseFloat(payload.Latitude, 64)
		lng, _ := strconv.ParseFloat(payload.Longitude, 64)
		msg.Type = "location"
		msg.Location = &struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name,omitempty"`
			Address   string  `json:"address,omitempty"`
		}{Latitude: lat, Longitude: lng}
	case payload.Reaction != "":
		msg.Type = "reaction"
		msg.Reaction = &struct {
			MessageID string `json:"message_id"`
			Emoji     string `json:"emoji"`
		}{MessageID: payload.ReplyMsgID, Emoji: payload.Reaction}
	case contentType == "image" || contentType == "video" || contentType == "audio" || contentType == "document":
		msg.Type = contentType
		mediaID := msg91FirstNonEmpty(extractMSG91MediaLink(payload.Content), payload.Content)
		switch contentType {
		case "image":
			msg.Image = &struct {
				ID       string `json:"id"`
				MimeType string `json:"mime_type"`
				SHA256   string `json:"sha256"`
				Caption  string `json:"caption,omitempty"`
			}{ID: mediaID}
		case "video":
			msg.Video = &struct {
				ID       string `json:"id"`
				MimeType string `json:"mime_type"`
				SHA256   string `json:"sha256"`
				Caption  string `json:"caption,omitempty"`
			}{ID: mediaID}
		case "audio":
			msg.Audio = &struct {
				ID       string `json:"id"`
				MimeType string `json:"mime_type"`
			}{ID: mediaID}
		case "document":
			msg.Document = &struct {
				ID       string `json:"id"`
				MimeType string `json:"mime_type"`
				SHA256   string `json:"sha256"`
				Filename string `json:"filename,omitempty"`
				Caption  string `json:"caption,omitempty"`
			}{ID: mediaID, Filename: payload.Filename}
		}
	default:
		msg.Type = "text"
		msg.Text = &struct {
			Body string `json:"body"`
		}{Body: textBody}
	}

	if payload.ReplyMsgID != "" && msg.Context == nil {
		msg.Context = &struct {
			From string `json:"from"`
			ID   string `json:"id"`
		}{ID: payload.ReplyMsgID}
	}

	return msg, profileName
}

func (a *App) processMSG91Status(phoneNumberID string, payload MSG91WebhookPayload, eventName, uuid, requestID string) {
	defer func() {
		if r := recover(); r != nil {
			a.Log.Error("Panic recovered in processMSG91Status", "panic", r)
		}
	}()

	statusValue := mapMSG91EventToStatus(eventName)
	if statusValue == "" {
		a.Log.Debug("Ignoring MSG91 webhook event", "event", eventName)
		return
	}

	messageID := stripMSG91UUIDSuffix(uuid)
	if messageID == "" {
		messageID = requestID
	}
	if messageID == "" {
		return
	}

	// Prefer matching on wamid; if not found and requestID differs, try requestID.
	status := WebhookStatus{
		ID:          messageID,
		Status:      statusValue,
		Timestamp:   msg91Timestamp(payload),
		RecipientID: msg91FirstNonEmpty(payload.CustomerNumber, payload.CustomerNumberSnake),
	}
	if payload.Reason != "" && statusValue == string(models.MessageStatusFailed) {
		status.Errors = []WebhookStatusError{{
			Title:   "MSG91 delivery failure",
			Message: payload.Reason,
		}}
		status.Errors[0].ErrorData.Details = payload.Reason
	}

	a.processStatusUpdate(phoneNumberID, status)

	// If we stored request_id as WhatsAppMessageID on send, also try updating by requestID
	// when uuid was present but didn't match any row.
	if requestID != "" && uuid != "" && stripMSG91UUIDSuffix(uuid) != requestID {
		a.processStatusUpdate(phoneNumberID, WebhookStatus{
			ID:        requestID,
			Status:    statusValue,
			Timestamp: status.Timestamp,
			Errors:    status.Errors,
		})
		// Promote stored request_id to wamid when we learn it.
		a.DB.Model(&models.Message{}).
			Where("whats_app_message_id = ?", requestID).
			Update("whats_app_message_id", stripMSG91UUIDSuffix(uuid))
	}
}

func mapMSG91EventToStatus(event string) string {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "sent", "submitted", "send":
		return string(models.MessageStatusSent)
	case "delivered", "delivery":
		return string(models.MessageStatusDelivered)
	case "read":
		return string(models.MessageStatusRead)
	case "failed", "fail", "failure", "undelivered":
		return string(models.MessageStatusFailed)
	default:
		return ""
	}
}

func msg91Timestamp(payload MSG91WebhookPayload) string {
	if payload.TS != "" {
		if t, err := time.Parse(time.RFC3339, payload.TS); err == nil {
			return strconv.FormatInt(t.Unix(), 10)
		}
	}
	if payload.RequestedAt != "" {
		if t, err := time.Parse(time.RFC3339, payload.RequestedAt); err == nil {
			return strconv.FormatInt(t.Unix(), 10)
		}
	}
	return strconv.FormatInt(time.Now().Unix(), 10)
}

func stripMSG91UUIDSuffix(uuid string) string {
	// Some MSG91 samples append "_hello" style suffixes to wamid.
	uuid = strings.TrimSpace(uuid)
	if i := strings.Index(uuid, "_"); i > 0 && strings.HasPrefix(uuid, "wamid.") {
		return uuid[:i]
	}
	return uuid
}

func extractMSG91TextContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var asObj map[string]any
	if json.Unmarshal([]byte(content), &asObj) == nil {
		if t, ok := asObj["text"].(string); ok {
			return t
		}
		if t, ok := asObj["body"].(string); ok {
			return t
		}
	}
	return content
}

func extractMSG91MediaLink(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var asObj map[string]any
	if json.Unmarshal([]byte(content), &asObj) != nil {
		if strings.HasPrefix(content, "http://") || strings.HasPrefix(content, "https://") {
			return content
		}
		return ""
	}
	for _, key := range []string{"link", "url", "id"} {
		if v, ok := asObj[key].(string); ok && v != "" {
			return v
		}
	}
	for _, mediaKey := range []string{"image", "video", "audio", "document"} {
		if m, ok := asObj[mediaKey].(map[string]any); ok {
			if link, _ := m["link"].(string); link != "" {
				return link
			}
			if id, _ := m["id"].(string); id != "" {
				return id
			}
		}
	}
	return ""
}

func msg91FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
