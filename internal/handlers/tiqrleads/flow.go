package tiqrleads

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	tiqrLeadsKey = "tiqr_leads"

	sessionEvalMode   = "leads_evaluation_mode"
	sessionCompletion = "leads_completion_action"
	sessionAnswers    = "leads_answers"
	sessionLeadSaved  = "leads_saved"
	sessionCriteria   = "lead_criteria"
	sessionThankYou   = "leads_thank_you"
	sessionHighMin    = "leads_high_min"
	sessionMediumMin  = "leads_medium_min"
	sessionLowMin     = "leads_low_min"
	sessionScore      = "leads_score"
	sessionCategory   = "leads_category"
	sessionQualified  = "leads_qualified"

	evalRules  = "rules"
	evalHybrid = "hybrid"

	completionRecord        = "record"
	completionHandoffHigh   = "handoff_high"
	completionAlwaysHandoff = "always_handoff"
	completionMidChat       = "mid_chat"
)

// FlowKey is the coded-flow registry key for TiQR Leads.
const FlowKey = tiqrLeadsKey

func init() {
	codedflow.Register(newTiqrLeadsFlow())
}

func newTiqrLeadsFlow() codedflow.CodedFlow {
	return codedflow.NewFlow(
		tiqrLeadsKey,
		"TiQR Leads",
		"Qualify WhatsApp leads against store-defined criteria, then record or hand off.",
		[]codedflow.CodedStep{
			{Name: "load_store", Label: "Load store"},
			{Name: "check_existing", Label: "Check existing lead"},
			{Name: "load_faqs", Label: "Load FAQs"},
			{Name: "load_criteria", Label: "Load criteria"},
			{Name: "ask", Label: "Ask qualification questions"},
			{Name: "save", Label: "Save lead"},
			{Name: "finish", Label: "Thank you or handoff"},
		},
		tiqrLeads,
	)
}

func tiqrLeads(c *codedflow.Conv) error {
	if _, ok := c.Store("store", "get_store", nil); !ok {
		return c.Transfer(codedflow.AgentHandoff)
	}

	// Contact-scoped: skip re-qualification when this phone already has a qualified lead.
	existing, ok := c.Store("existing_lead", "get_lead", map[string]string{
		"phone_number": c.Session().PhoneNumber,
	})
	if ok && leadIsQualified(existing) {
		writeClassificationFromLead(c, existing)
		c.Say("We've already recorded your details.")
		return c.End()
	}
	if c.Stop {
		return nil
	}

	_ = c.LoadFaqs("faqs", "list_faqs")
	if c.Stop {
		return nil
	}

	c.Once("leads_bind_settings", func() {
		eval, completion := c.App().LeadsFlowOptions(c.Session().OrganizationID, c.Session().WhatsAppAccount)
		if eval == "" {
			eval = evalRules
		}
		if completion == "" {
			completion = completionRecord
		}
		c.Session().SessionData[sessionEvalMode] = eval
		c.Session().SessionData[sessionCompletion] = completion
	})

	evalMode := asString(c.Session().SessionData[sessionEvalMode])
	if evalMode == "" {
		evalMode = evalRules
	}

	criteriaRaw, ok := c.Store("lead_criteria", "get_lead_criteria", map[string]string{"mode": evalMode})
	if !ok {
		c.Say("Lead qualification is not set up yet. Please try again later.")
		return c.End()
	}
	c.Once("leads_stash_criteria", func() {
		c.Session().SessionData[sessionCriteria] = criteriaRaw
		c.Session().SessionData[sessionThankYou] = asString(criteriaRaw["thank_you_text"])
		c.Session().SessionData[sessionHighMin] = criteriaRaw["high_min"]
		c.Session().SessionData[sessionMediumMin] = criteriaRaw["medium_min"]
		c.Session().SessionData[sessionLowMin] = criteriaRaw["low_min"]
		if c.Session().SessionData[sessionAnswers] == nil {
			c.Session().SessionData[sessionAnswers] = []any{}
		}
	})

	criteriaList, _ := anySlice(criteriaRaw["criteria"])
	if len(criteriaList) == 0 {
		c.Say("Lead qualification is not set up yet. Please try again later.")
		return c.End()
	}

	for i, item := range criteriaList {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		if err := askCriterion(c, i, row, evalMode); err != nil {
			return err
		}
		if c.Stop {
			return nil
		}
	}

	return finishQualified(c)
}

func askCriterion(c *codedflow.Conv, index int, row map[string]any, evalMode string) error {
	id := asString(row["id"])
	name := asString(row["name"])
	question := asString(row["question"])
	weight := asInt(row["weight"])
	stepName := fmt.Sprintf("lead_q_%s", id)
	if id == "" {
		stepName = fmt.Sprintf("lead_q_%d", index)
	}

	if evalMode == evalHybrid {
		rules, _ := anySlice(row["rules"])
		buttons := make([]codedflow.Button, 0, len(rules))
		scoreByID := map[string]int{}
		labelByID := map[string]string{}
		for _, ruleItem := range rules {
			rule, ok := asStringMap(ruleItem)
			if !ok {
				continue
			}
			ruleID := asString(rule["id"])
			condition := asString(rule["condition"])
			if ruleID == "" || condition == "" {
				continue
			}
			buttons = append(buttons, codedflow.Button{ID: ruleID, Title: truncate(condition, 24)})
			scoreByID[ruleID] = asInt(rule["score"])
			labelByID[ruleID] = condition
		}
		route, ok := c.AskRouteText(stepName, codedflow.TextRoutePrompt{
			Body:    question,
			Buttons: buttons,
			Step: codedflow.StepNote{
				Doing:  "Qualify the lead on " + name,
				Expect: "An answer that matches one of the conditions",
			},
		}, codedflow.RouteOptions{AllowFAQ: true})
		if !ok {
			return nil
		}
		if route.Kind != codedflow.RouteChoice {
			return c.Transfer(codedflow.AgentHandoff)
		}
		stashAnswer(c, stepName, name, question, labelByID[route.ID], scoreByID[route.ID], weight)
		return nil
	}

	choices, _ := anySlice(row["choices"])
	buttons := make([]codedflow.Button, 0, len(choices))
	scoreByID := map[string]int{}
	labelByID := map[string]string{}
	for _, choiceItem := range choices {
		choice, ok := asStringMap(choiceItem)
		if !ok {
			continue
		}
		choiceID := asString(choice["id"])
		label := asString(choice["label"])
		if choiceID == "" || label == "" {
			continue
		}
		buttons = append(buttons, codedflow.Button{ID: choiceID, Title: label})
		scoreByID[choiceID] = asInt(choice["score"])
		labelByID[choiceID] = label
	}
	titleMax := 24
	if len(buttons) <= 3 {
		titleMax = 20
	}
	for i := range buttons {
		buttons[i].Title = truncate(buttons[i].Title, titleMax)
	}
	note := codedflow.StepNote{
		Doing:  "Qualify the lead on " + name,
		Expect: "One of the offered choices",
	}
	var route codedflow.Route
	var ok bool
	if len(buttons) <= 3 {
		route, ok = c.AskRouteButtons(stepName, codedflow.ButtonPrompt{
			Body:    question,
			Buttons: buttons,
			Step:    note,
		}, codedflow.RouteOptions{AllowFAQ: true})
	} else {
		items := make([]any, 0, len(buttons))
		for _, button := range buttons {
			items = append(items, map[string]any{
				"id":    button.ID,
				"title": button.Title,
			})
		}
		route, ok = c.AskRouteList(stepName, items, codedflow.ListPrompt{
			Body:     question,
			Button:   "Choose",
			Header:   truncate(name, 60),
			Section:  "Options",
			ItemsKey: stepName + "_items",
			IDField:  "id",
			Title:    "title",
			Step:     note,
		}, codedflow.RouteOptions{AllowFAQ: true})
	}
	if !ok {
		return nil
	}
	if route.Kind != codedflow.RouteChoice {
		return c.Transfer(codedflow.AgentHandoff)
	}
	stashAnswer(c, stepName, name, question, labelByID[route.ID], scoreByID[route.ID], weight)
	return nil
}

func stashAnswer(c *codedflow.Conv, stepName, name, question, answer string, score, weight int) {
	c.Once("answer_"+stepName, func() {
		rows := answersFromSessionData(c.Session().SessionData)
		rows = append(rows, map[string]any{
			"criterion_name": name,
			"question":       question,
			"answer":         answer,
			"score":          score,
			"weight":         weight,
			"position":       len(rows),
		})
		c.Session().SessionData[sessionAnswers] = rows
	})
}

func finishQualified(c *codedflow.Conv) error {
	writeClassificationFromSession(c, true)
	if err := SaveLeadFromSession(c, true, asString(c.Session().SessionData[sessionCompletion])); err != nil {
		c.App().LogWarn("tiqr leads save failed", "error", err)
	}
	completion := asString(c.Session().SessionData[sessionCompletion])
	category := asString(c.Session().SessionData[sessionCategory])
	thankYou := asString(c.Session().SessionData[sessionThankYou])
	if thankYou == "" {
		thankYou = "Thanks for your answers. We'll be in touch soon."
	}
	switch completion {
	case completionAlwaysHandoff:
		return c.Transfer(codedflow.AgentHandoff)
	case completionHandoffHigh:
		if category == "high" {
			return c.Transfer(codedflow.AgentHandoff)
		}
		c.Say(thankYou)
		return c.End()
	default:
		c.Say(thankYou)
		return c.End()
	}
}

func leadIsQualified(lead map[string]any) bool {
	if lead == nil || asString(lead["id"]) == "" {
		return false
	}
	switch v := lead["qualified"].(type) {
	case bool:
		return v
	default:
		return stringsEqualFoldTrue(asString(lead["qualified"]))
	}
}

func stringsEqualFoldTrue(s string) bool {
	return s == "true" || s == "True" || s == "1"
}

func writeClassificationFromSession(c *codedflow.Conv, qualified bool) {
	if c == nil || c.ChatCtx() == nil {
		return
	}
	session := c.Session()
	if session == nil || session.SessionData == nil {
		return
	}
	score, category := scoreFromSessionData(session.SessionData)
	session.SessionData[sessionScore] = score
	session.SessionData[sessionCategory] = category
	session.SessionData[sessionQualified] = qualified
}

func writeClassificationFromLead(c *codedflow.Conv, lead map[string]any) {
	if c == nil || c.ChatCtx() == nil || lead == nil {
		return
	}
	session := c.Session()
	if session == nil || session.SessionData == nil {
		return
	}
	session.SessionData[sessionScore] = asInt(lead["score"])
	session.SessionData[sessionCategory] = asString(lead["category"])
	session.SessionData[sessionQualified] = true
	if answers, ok := anySlice(lead["answers"]); ok {
		session.SessionData[sessionAnswers] = answers
	}
}

// SaveLeadFromSession posts create_lead once for this session.
func SaveLeadFromSession(c *codedflow.Conv, qualified bool, completionAction string) error {
	if c == nil || c.Session() == nil {
		return nil
	}
	if asString(c.Session().SessionData[sessionLeadSaved]) == "1" {
		return nil
	}
	if completionAction == "" {
		completionAction = completionMidChat
	}
	answers := answersFromSessionData(c.Session().SessionData)
	payloadAnswers, _ := json.Marshal(answers)
	criteriaSnap, _ := json.Marshal(c.Session().SessionData[sessionCriteria])
	params := map[string]string{
		"session_id":        c.Session().ID.String(),
		"phone_number":      c.Session().PhoneNumber,
		"name":              asString(c.Session().SessionData["contact_name"]),
		"evaluation_mode":   asString(c.Session().SessionData[sessionEvalMode]),
		"completion_action": completionAction,
		"qualified":         strconv.FormatBool(qualified),
		"answers_json":      string(payloadAnswers),
		"criteria_snapshot": string(criteriaSnap),
	}
	if params["evaluation_mode"] == "" {
		params["evaluation_mode"] = evalRules
	}
	_, ok := c.Store("lead", "create_lead", params)
	if !ok {
		return fmt.Errorf("create_lead failed")
	}
	c.Session().SessionData[sessionLeadSaved] = "1"
	return nil
}

// IsLeadsSession reports whether the session is inside TiQR Leads.
func IsLeadsSession(session *models.ChatbotSession) bool {
	return codedflow.SessionKey(session) == FlowKey
}

// PrepareHandoff saves a partial lead (when not already saved) and returns the
// agent handoff body. Completion modes that already called SaveLeadFromSession
// keep the qualified lead; mid-chat handoffs create a partial one.
// Preview capture writes classification only — create_lead stays mockable via Store.
func PrepareHandoff(c *codedflow.Conv, message string) string {
	writeClassificationFromSession(c, false)
	if c != nil && c.ChatCtx() != nil && c.ChatCtx().Capturing() {
		if summary := buildHandoffSummary(c.Session()); summary != "" {
			return summary
		}
		return message
	}
	_ = SaveLeadFromSession(c, false, completionMidChat)
	if summary := buildHandoffSummary(c.Session()); summary != "" {
		return summary
	}
	return message
}
