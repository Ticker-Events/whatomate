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
		rt.AllowCreateOrder = true
		rt.FlowSubmitted = true
		for k, v := range data {
			if s := strings.TrimSpace(asString(v)); s != "" {
				if c.Session().SessionData == nil {
					c.Session().SessionData = models.JSONB{}
				}
				c.Session().SessionData[k] = s
			}
		}
	}

	// Keyword-only first turn: welcome shell with no model.
	if userText == "" && buttonID == "" && len(rt.State.Transcript) == 0 {
		store, _ := rt.callTiqr("store", "get_store", nil)
		if store != nil {
			if c.Session().SessionData == nil {
				c.Session().SessionData = models.JSONB{}
			}
			c.Session().SessionData["store"] = store
		}
		if err := rt.sendShell(stepWelcome, ShellChrome{}); err != nil {
			c.App().LogWarn("tiqr ecommerce v2 welcome shell failed", "error", err)
			_ = c.App().DeliverCodedText(c.ChatCtx(), "v2_welcome_fallback", retryLine)
		}
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
			_ = rt.resendCurrentShell()
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
			break
		}
	}
	if !rt.Replied && !rt.Ended && !rt.Transfer {
		// No accepted shell — resend the current step shell (never free model text).
		if err := rt.resendCurrentShell(); err != nil {
			_ = rt.Conv.App().DeliverCodedText(rt.Conv.ChatCtx(), "v2_retry", retryLine)
			rt.State.appendTranscript("assistant", retryLine)
			rt.Replied = true
			if s := rt.session(); s != nil {
				s.CurrentStep = "v2_wait"
			}
		}
	}
	return runErr
}

func buildInstruction(rt *Runtime) string {
	var b strings.Builder
	b.WriteString(`You are the store's WhatsApp shopping assistant. You control the conversation by calling tools.
Rules:
- Never invent product, collection, option, addon, or order ids. Only use ids returned by tools (grounded).
- Call reply exactly once per customer turn. reply.step must be a V1 waiting step name. Go builds the WhatsApp shell (buttons, list, carousel, image, location, Flow, or text question). Do not write free-form WhatsApp bodies in English.
- Waiting steps: welcome, collections, products, options, quantity, capture, addons, cart, cart_edit, fulfillment, delivery_pin, delivery_fallback, checkout_preview, checkout_preview_fail, details, order_placed, order_status, order_status_summary.
- Required order: option (when more than one) → quantity → capture fields → addons → cart. Checkout: fulfillment → delivery_pin (when delivery) → checkout_preview → details Flow → create_order. Do not skip required steps.
- A tap or exact title is binding. If the customer tapped buy_products, reply step=collections. confirm_items → details (not create_order). talk_to_agent → transfer_to_staff.
- Optional deviation_line: one short text bubble before the shell (FAQ answer, clarify, or jump explanation). If the step did not change, the shell is resent after that bubble.
- Fixed action ids: buy_products, check_order_status, talk_to_agent, add_more, checkout, edit_cart, confirm_items, pickup, delivery, share_again, try_again.
- create_order is allowed only after the details Flow is submitted. transfer_to_staff is gated on the customer asking for staff.
- Checkout preview is the only price quote. Never invent fees on the delivery pin message.
- Search results are search results, not a collection name.
- Reserved phrases (talk to staff, checkout, …) are never capture/add-on/edit answers.
- Order status: reply step=order_status_summary with summary_text, no pay link, then end_session.
- Include collection ai_instructions in your reasoning when present for the active collection.
- Keep deviation lines short.
`)
	if rt.State.WaitingStep != "" {
		fmt.Fprintf(&b, "Current waiting step: %s\n", rt.State.WaitingStep)
	}
	if rt.BindingButtonID != "" {
		fmt.Fprintf(&b, "Customer tapped button_id=%s — that action is binding.\n", rt.BindingButtonID)
	}
	if rt.State.CustomerLanguage != "" && rt.State.CustomerLanguage != "en" {
		fmt.Fprintf(&b, "Session language %s: you may pass chrome_body/chrome_titles translations; catalog names must stay unchanged. English chrome is used if translation is rejected.\n", rt.State.CustomerLanguage)
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
		b.WriteString("\nDetails Flow was submitted this turn — create_order is allowed.\n")
	}
	if rt.AllowTransfer {
		b.WriteString("\nCustomer already asked for staff this turn — transfer_to_staff is allowed.\n")
	}
	return b.String()
}

func detectLanguageHint(text string, allowed []string) string {
	_ = text
	if len(allowed) == 1 {
		return allowed[0]
	}
	return ""
}
