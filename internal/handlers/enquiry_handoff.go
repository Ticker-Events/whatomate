package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrdining"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrleads"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
)

func isCodedEnquiryFlow(flowKey string) bool {
	switch strings.TrimSpace(flowKey) {
	case tiqrleads.FlowKey, tiqrecommerce.FlowKey, tiqrdining.FlowKey:
		return true
	default:
		return false
	}
}

// willCreateAgentTransfer mirrors createTransferToQueue / createTransferToTeam
// gates so we do not allocate an enquiry when no transfer will be created.
func (a *App) willCreateAgentTransfer(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	settings *models.ChatbotSettings,
) bool {
	if account == nil || contact == nil {
		return false
	}
	if a.hasActiveAgentTransfer(account.OrganizationID, contact.ID) {
		return false
	}
	if settings != nil && settings.BusinessHours.Enabled && len(settings.BusinessHours.Hours) > 0 {
		if !a.isWithinBusinessHours(settings.BusinessHours.Hours) {
			return false
		}
	}
	return true
}

// PrepareEnquiryHandoff creates a flow enquiry when a coded handoff will reach
// the agent queue, appends the enquiry number to body, and kicks off AI fill.
// On any failure it returns the original body unchanged.
func (a *App) PrepareEnquiryHandoff(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	session *models.ChatbotSession,
	settings *models.ChatbotSettings,
	body string,
) string {
	if a == nil || account == nil || contact == nil || session == nil {
		return body
	}
	flowKey := codedflow.SessionKey(session)
	if !isCodedEnquiryFlow(flowKey) {
		return body
	}
	if !a.willCreateAgentTransfer(account, contact, settings) {
		return body
	}
	if settings == nil || !commerceRESTConfigured(settings.AI) {
		a.Log.Warn("enquiry handoff skipped: commerce REST not configured",
			"session", session.ID, "flow", flowKey)
		return body
	}

	storeID := strings.TrimSpace(settings.AI.CommerceStoreID)
	client := newTiqrStoreRESTClient(settings.AI.CommerceRESTURL)
	excerpt := a.buildEnquiryTranscriptExcerpt(session)
	contactName := strings.TrimSpace(contact.ProfileName)
	if contactName == "" && session.SessionData != nil {
		contactName = strings.TrimSpace(asString(session.SessionData["contact_name"]))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	raw, err := client.CreateEnquiry(ctx, storeID, ticker.CreateEnquiryRequest{
		SessionID:         session.ID.String(),
		PhoneNumber:       strings.TrimSpace(contact.PhoneNumber),
		ContactName:       contactName,
		FlowKey:           flowKey,
		TranscriptExcerpt: excerpt,
	})
	if err != nil {
		a.Log.Error("enquiry create failed; continuing handoff without number",
			"error", err, "session", session.ID, "flow", flowKey)
		return body
	}

	number := enquiryNumberFromPayload(raw)
	enquiryID := enquiryIDFromPayload(raw)
	if number <= 0 {
		a.Log.Warn("enquiry create returned no number",
			"session", session.ID, "payload", raw)
		return body
	}

	out := strings.TrimSpace(body)
	if out == "" {
		out = codedflow.AgentHandoff
	}
	out = out + "\n\nYour enquiry number is " + strconv.Itoa(number) + "."

	if enquiryID > 0 {
		go a.fillEnquirySummary(settings, session, storeID, enquiryID, excerpt, contactName, flowKey)
	}
	return out
}

func enquiryNumberFromPayload(raw map[string]any) int {
	if raw == nil {
		return 0
	}
	switch v := raw["number"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

func enquiryIDFromPayload(raw map[string]any) int {
	if raw == nil {
		return 0
	}
	switch v := raw["id"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

func (a *App) buildEnquiryTranscriptExcerpt(session *models.ChatbotSession) string {
	if session == nil {
		return ""
	}
	var parts []string
	if session.SessionData != nil {
		for _, key := range []string{
			"leads_handoff_summary",
			"commerce_handoff_summary",
		} {
			if note := strings.TrimSpace(asString(session.SessionData[key])); note != "" {
				parts = append(parts, note)
			}
		}
		if handoff, ok := session.SessionData["commerce_handoff"].(map[string]any); ok {
			if note := strings.TrimSpace(asString(handoff["summary"])); note != "" {
				parts = append(parts, note)
			}
		}
	}
	for _, msg := range a.getSessionHistory(session.ID, 20) {
		text := strings.TrimSpace(msg.Message)
		if text == "" {
			continue
		}
		role := "customer"
		if msg.Direction == models.DirectionOutgoing {
			role = "bot"
		}
		parts = append(parts, role+": "+text)
	}
	excerpt := strings.Join(parts, "\n")
	if len(excerpt) > 6000 {
		excerpt = excerpt[len(excerpt)-6000:]
	}
	return excerpt
}

func (a *App) fillEnquirySummary(
	settings *models.ChatbotSettings,
	session *models.ChatbotSession,
	storeID string,
	enquiryID int,
	excerpt, contactName, flowKey string,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			a.Log.Error("enquiry AI fill panicked", "error", recovered, "enquiry_id", enquiryID)
		}
	}()
	if settings == nil || session == nil || enquiryID <= 0 {
		return
	}
	title, summary, err := a.generateEnquiryTitleSummary(settings, session, excerpt, contactName, flowKey)
	if err != nil {
		a.Log.Warn("enquiry AI summary failed", "error", err, "enquiry_id", enquiryID)
		return
	}
	client := newTiqrStoreRESTClient(settings.AI.CommerceRESTURL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := client.AiFillEnquiry(ctx, storeID, enquiryID, ticker.AiFillEnquiryRequest{
		Title:             title,
		Summary:           summary,
		TranscriptExcerpt: excerpt,
	}); err != nil {
		a.Log.Warn("enquiry AI fill patch failed", "error", err, "enquiry_id", enquiryID)
	}
}

func (a *App) generateEnquiryTitleSummary(
	settings *models.ChatbotSettings,
	session *models.ChatbotSession,
	excerpt, contactName, flowKey string,
) (string, string, error) {
	prompt := fmt.Sprintf(`Summarise a WhatsApp chat handed to a human agent.

Return ONLY a JSON object with keys "title" and "summary".
- title: one short line (max 80 characters) naming what the customer needs.
- summary: 2-4 short sentences for staff — what they asked, what the bot already answered, what is still unresolved.

Contact: %s
Flow: %s

Transcript / notes:
%s
`, strings.TrimSpace(contactName), strings.TrimSpace(flowKey), strings.TrimSpace(excerpt))

	text, err := a.CompleteCodedRoleText(session, "enquiry_summary", prompt)
	if err != nil || strings.TrimSpace(text) == "" {
		if settings == nil || strings.TrimSpace(settings.AI.APIKey) == "" {
			return "", "", fmt.Errorf("ai unavailable: %w", err)
		}
		text, err = a.GenerateOpenAIResponse(settings, session, prompt, "")
		if err != nil {
			return "", "", err
		}
	}
	title, summary, parseErr := parseEnquiryAIJSON(text)
	if parseErr != nil {
		return "", "", parseErr
	}
	return title, summary, nil
}

func parseEnquiryAIJSON(text string) (string, string, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(strings.TrimSpace(text), "```")
		text = strings.TrimSpace(text)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		start := strings.Index(text, "{")
		end := strings.LastIndex(text, "}")
		if start < 0 || end <= start {
			return "", "", fmt.Errorf("invalid enquiry AI json")
		}
		if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
			return "", "", err
		}
	}
	title := strings.TrimSpace(asString(raw["title"]))
	summary := strings.TrimSpace(asString(raw["summary"]))
	if title == "" {
		return "", "", fmt.Errorf("enquiry AI missing title")
	}
	if len(title) > 255 {
		title = title[:255]
	}
	return title, summary, nil
}
