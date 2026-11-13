package tiqrecommercev2

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

// agentRunner runs one ADK turn. Tests can replace it.
var agentRunner = runADKTurn

func runAgentTurn(c *codedflow.Conv) error {
	if c == nil || c.App() == nil || c.ChatCtx() == nil {
		return fmt.Errorf("tiqr ecommerce v2 is not configured")
	}
	account := c.ChatCtx().Account()
	if account == nil {
		return c.Transfer(notReadyLine)
	}
	settings, err := c.App().GetChatbotSettingsCached(account.OrganizationID, account.Name)
	if err != nil || settings == nil || settings.AI.Provider != models.AIProviderGoogle || strings.TrimSpace(settings.AI.APIKey) == "" {
		return c.Transfer(notReadyLine)
	}

	rt := newRuntime(c)
	defer rt.persist()

	userText := strings.TrimSpace(c.ChatCtx().UserInput())
	buttonID := strings.TrimSpace(c.ChatCtx().ButtonID())
	if pin, ok := codedflow.ParseLocationPin(userText); ok {
		userText = fmt.Sprintf("location pin latitude=%f longitude=%f", pin.Latitude, pin.Longitude)
	}
	if buttonID != "" {
		userText = strings.TrimSpace(userText + "\n[button_id=" + buttonID + "]")
	}
	if data := c.ChatCtx().FlowResponseData(); len(data) > 0 {
		userText = strings.TrimSpace(userText + "\n[whatsapp_flow_submitted]")
		rt.State.AwaitingDetails = false
		for k, v := range data {
			if s := strings.TrimSpace(asString(v)); s != "" {
				if c.Session().SessionData == nil {
					c.Session().SessionData = models.JSONB{}
				}
				c.Session().SessionData[k] = s
			}
		}
	}

	// Keyword-only first turn: offer saved welcome when they have not said what they want.
	if userText == "" && buttonID == "" && len(rt.State.Transcript) == 0 {
		welcome := ""
		if settings := bindingSettings(account.OrganizationID, account.Name); settings != nil {
			welcome, _ = settings["welcome_message"].(string)
			welcome = strings.TrimSpace(welcome)
		}
		store, _ := rt.callTiqr("store", "get_store", nil)
		if store != nil {
			c.Session().SessionData["store"] = store
		}
		body := strings.TrimSpace(welcome)
		if body == "" && store != nil {
			name := strings.TrimSpace(asString(store["name"]))
			if name != "" {
				body = "Welcome to " + name + "."
			}
		}
		if sum := fulfillmentSummary(store); sum != "" {
			if body != "" {
				body += "\n\n" + sum
			} else {
				body = sum
			}
		}
		if body == "" {
			body = "What would you like to do?"
		} else if !strings.Contains(strings.ToLower(body), "what would you like") {
			body += "\n\nWhat would you like to do?"
		}
		buttons := []map[string]any{
			{"id": fixedBuyProducts, "title": "Buy products"},
			{"id": fixedCheckOrderStatus, "title": "Check order status"},
			{"id": fixedTalkToStaff, "title": "Talk to staff"},
		}
		_, _ = c.App().ExecChatButtons(c.ChatCtx(), "v2_welcome", map[string]any{
			"body":    body,
			"buttons": buttons,
		})
		rt.State.appendTranscript("assistant", body)
		c.Session().CurrentStep = "v2_wait"
		rt.persist()
		return nil
	}

	if userText != "" {
		rt.State.appendTranscript("user", userText)
		langs := c.App().CodedFlowSupportedLanguages(c.Session())
		lang := detectLanguageHint(userText, langs)
		if lang != "" {
			rt.State.CustomerLanguage = lang
			c.Session().SessionData["customer_language"] = lang
		}
	}

	modelID := ModelFromSettings(bindingSettings(account.OrganizationID, account.Name))

	ctx := context.Background()
	if err := agentRunner(ctx, rt, settings.AI.APIKey, modelID, userText); err != nil {
		c.App().LogWarn("tiqr ecommerce v2 agent turn failed", "error", err)
		if !rt.Replied {
			_ = c.App().DeliverCodedText(c.ChatCtx(), "v2_retry", retryLine)
			rt.State.appendTranscript("assistant", retryLine)
			c.Session().CurrentStep = "v2_wait"
		}
	}
	rt.persist()
	return nil
}

func runADKTurn(ctx context.Context, rt *Runtime, apiKey, modelID, userText string) error {
	model, err := gemini.NewModel(ctx, modelID, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return err
	}
	tools, err := buildTools(rt)
	if err != nil {
		return err
	}
	instruction := buildInstruction(rt)
	a, err := llmagent.New(llmagent.Config{
		Name:        "tiqr_ecommerce_v2",
		Model:       model,
		Description: "WhatsApp ecommerce shopping agent for one store.",
		Instruction: instruction,
		Tools:       tools,
	})
	if err != nil {
		return err
	}
	sessSvc := session.InMemoryService()
	r, err := runner.New(runner.Config{
		AppName:           "tiqr_ecommerce_v2",
		Agent:             a,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		return err
	}
	userID := "wa"
	sessionID := "turn"
	if s := rt.session(); s != nil {
		sessionID = s.ID.String()
		if s.ContactID != (uuid.UUID{}) {
			userID = s.ContactID.String()
		}
	}
	msg := genai.NewContentFromText(userText, genai.RoleUser)
	if strings.TrimSpace(userText) == "" {
		msg = genai.NewContentFromText("(customer sent an empty message; ask how you can help)", genai.RoleUser)
	}
	var runErr error
	for ev, err := range r.Run(ctx, userID, sessionID, msg, agent.RunConfig{}) {
		if err != nil {
			runErr = err
			break
		}
		if ev == nil {
			continue
		}
		// Persist a short model text into transcript when no reply tool ran yet.
		if !rt.Replied && ev.LLMResponse.Content != nil {
			for _, p := range ev.LLMResponse.Content.Parts {
				if p != nil && strings.TrimSpace(p.Text) != "" {
					rt.State.appendTranscript("model", p.Text)
				}
			}
		}
		if rt.Replied || rt.Ended || rt.Transfer {
			break
		}
		if rt.Rounds >= maxToolRounds {
			if !rt.Replied {
				_ = rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), "v2_retry", retryLine)
				rt.State.appendTranscript("assistant", retryLine)
				rt.Replied = true
				rt.session().CurrentStep = "v2_wait"
			}
			break
		}
	}
	if !rt.Replied && !rt.Ended && !rt.Transfer {
		// Model finished without reply tool — send last model text or retry line.
		last := ""
		for i := len(rt.State.Transcript) - 1; i >= 0; i-- {
			if rt.State.Transcript[i].Role == "model" {
				last = rt.State.Transcript[i].Text
				break
			}
		}
		if last == "" {
			last = retryLine
		}
		_ = rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), "v2_reply", last)
		rt.State.appendTranscript("assistant", last)
		rt.Replied = true
		rt.session().CurrentStep = "v2_wait"
	}
	return runErr
}

func buildInstruction(rt *Runtime) string {
	var b strings.Builder
	b.WriteString(`You are the store's WhatsApp shopping assistant. You control the conversation by calling tools.
Rules:
- Never invent product, collection, option, addon, or order ids. Only use ids returned by tools (grounded).
- Call reply exactly once per customer turn to send WhatsApp text/buttons/location/pay.
- Fixed action ids always allowed: buy_products, check_order_status, talk_to_agent, add_more, checkout, edit_cart, confirm_items.
- create_order and transfer_to_staff are gated: if refused, ask the customer, then call again after they confirm.
- Checkout preview is the only price quote. Never invent fees on the delivery pin message.
- Search results are search results, not a collection name.
- Reserved phrases (talk to staff, checkout, …) are never capture/add-on/edit answers.
- Order status: summarize without a pay link, then end_session.
- Include collection ai_instructions in your reasoning when present for the active collection.
- Keep WhatsApp replies short.
`)
	if rt.State.CustomerLanguage != "" && rt.State.CustomerLanguage != "en" {
		fmt.Fprintf(&b, "Reply in language code %s using that language's native script.\n", rt.State.CustomerLanguage)
	}
	if rt.State.AIInstructions != "" {
		fmt.Fprintf(&b, "Active collection instructions: %s\n", rt.State.AIInstructions)
	}
	b.WriteString("\nSession cart:\n")
	b.WriteString(formatCartSummary(rt.cart()))
	b.WriteString("\n\nRecent transcript:\n")
	start := 0
	if len(rt.State.Transcript) > 12 {
		start = len(rt.State.Transcript) - 12
	}
	for _, t := range rt.State.Transcript[start:] {
		fmt.Fprintf(&b, "%s: %s\n", t.Role, t.Text)
	}
	if rt.AllowCreateOrder {
		b.WriteString("\nCustomer already confirmed placing the order this turn — create_order is allowed.\n")
	}
	if rt.AllowTransfer {
		b.WriteString("\nCustomer already asked for staff this turn — transfer_to_staff is allowed.\n")
	}
	return b.String()
}

func detectLanguageHint(text string, allowed []string) string {
	// Lightweight sticky update: if store lists languages, prefer matching script heuristics.
	// Full detection is left to the model; we only store a hint when clearly constrained.
	_ = text
	if len(allowed) == 1 {
		return allowed[0]
	}
	return ""
}
