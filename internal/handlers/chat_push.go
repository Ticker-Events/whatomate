package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
)

type chatIncomingPushPayload struct {
	OrganizationID string `json:"organization_id"`
	ContactID      string `json:"contact_id"`
	ContactName    string `json:"contact_name"`
	Preview        string `json:"preview"`
	AssignedUserID string `json:"assigned_user_id,omitempty"`
}

// notifyChatIncomingPush asks ticker-events to FCM-notify store staff about a
// staff-facing inbound chat message. No-op when ticker URL/pushkey are unset.
func (a *App) notifyChatIncomingPush(account *models.WhatsAppAccount, contact *models.Contact, preview string) {
	if a == nil || a.Config == nil || account == nil || contact == nil {
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(a.Config.Ticker.URL), "/")
	pushKey := strings.TrimSpace(a.Config.Ticker.PushKey)
	if baseURL == "" || pushKey == "" {
		return
	}

	name := strings.TrimSpace(contact.ProfileName)
	if name == "" {
		name = strings.TrimSpace(contact.PhoneNumber)
	}
	bodyPreview := strings.TrimSpace(preview)
	if bodyPreview == "" {
		bodyPreview = strings.TrimSpace(contact.LastMessagePreview)
	}

	payload := chatIncomingPushPayload{
		OrganizationID: account.OrganizationID.String(),
		ContactID:      contact.ID.String(),
		ContactName:    name,
		Preview:        bodyPreview,
	}
	if contact.AssignedUserID != nil && *contact.AssignedUserID != uuid.Nil {
		payload.AssignedUserID = contact.AssignedUserID.String()
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		a.sendChatIncomingPush(ctx, baseURL, pushKey, payload)
	}()
}

func (a *App) sendChatIncomingPush(ctx context.Context, baseURL, pushKey string, payload chatIncomingPushPayload) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		a.Log.Error("chat push: marshal failed", "error", err)
		return
	}

	url := baseURL + "/conversation/chat-incoming-push/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		a.Log.Error("chat push: build request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", pushKey)
	req.Header.Set("User-Agent", "Whatomate-ChatPush/1.0")

	client := a.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		a.Log.Warn("chat push: request failed", "error", err, "contact_id", payload.ContactID)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		a.Log.Warn("chat push: non-2xx response",
			"status", resp.StatusCode,
			"contact_id", payload.ContactID,
			"organization_id", payload.OrganizationID,
		)
		return
	}
	a.Log.Debug("chat push: notified ticker-events",
		"contact_id", payload.ContactID,
		"organization_id", payload.OrganizationID,
	)
}
