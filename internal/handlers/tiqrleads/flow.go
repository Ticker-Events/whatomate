package tiqrleads

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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
	sessionWelcome    = "leads_welcome"
	sessionHighMin    = "leads_high_min"
	sessionMediumMin  = "leads_medium_min"
	sessionLowMin     = "leads_low_min"
	sessionScore           = "leads_score"
	sessionCategory        = "leads_category"
	sessionQualified       = "leads_qualified"
	sessionHandoffSummary  = "leads_handoff_summary"

	evalRules  = "rules"
	evalHybrid = "hybrid"

	completionRecord        = "record"
	completionHandoffHigh   = "handoff_high"
	completionAlwaysHandoff = "always_handoff"
	completionMidChat       = "mid_chat"
	completionSessionExpired = "session_expired"

	answerKindCriterion = "criterion"
	answerKindField     = "field"
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

	// Contact-scoped: already-qualified leads skip re-ask and go to FAQ help.
	existing, ok := c.Store("existing_lead", "get_lead", map[string]string{
		"phone_number": c.Session().PhoneNumber,
	})
	if ok && leadIsQualified(existing) {
		writeClassificationFromLead(c, existing)
		_ = c.LoadFaqs("faqs", "list_faqs")
		if c.Stop {
			return nil
		}
		return runHelpPath(c, true)
	}
	if c.Stop {
		return nil
	}
	if ok && leadExists(existing) && !leadIsQualified(existing) {
		seedPartialLeadAnswers(c, existing)
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
		c.Session().SessionData[sessionWelcome] = asString(criteriaRaw["welcome_text"])
		c.Session().SessionData[sessionHighMin] = criteriaRaw["high_min"]
		c.Session().SessionData[sessionMediumMin] = criteriaRaw["medium_min"]
		c.Session().SessionData[sessionLowMin] = criteriaRaw["low_min"]
		if c.Session().SessionData[sessionAnswers] == nil {
			c.Session().SessionData[sessionAnswers] = []any{}
		}
	})

	steps := stepsFromCriteriaPayload(criteriaRaw)
	if len(steps) == 0 {
		c.Say("Lead qualification is not set up yet. Please try again later.")
		return c.End()
	}

	stashDefaultNameAnswer(c)
	welcomeWithName(c)

	for i, item := range steps {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		kind := strings.TrimSpace(asString(row["kind"]))
		if kind == answerKindField || (kind == "" && asString(row["key"]) != "" && asString(row["type"]) != "") {
			stepName := leadFieldStepName(i, row)
			if stepAlreadyAnswered(c, answerKindField, fieldAnswerName(row)) {
				drainLeadStepReplay(c, stepName)
				continue
			}
			if err := askCaptureField(c, i, row); err != nil {
				return err
			}
		} else {
			stepName := leadCriterionStepName(i, row)
			if stepAlreadyAnswered(c, answerKindCriterion, criterionAnswerName(row)) {
				drainLeadStepReplay(c, stepName)
				continue
			}
			if err := askCriterion(c, i, row, evalMode); err != nil {
				return err
			}
		}
		if c.Stop {
			return nil
		}
	}

	return finishQualified(c)
}

func stepsFromCriteriaPayload(criteriaRaw map[string]any) []any {
	if steps, ok := anySlice(criteriaRaw["steps"]); ok && len(steps) > 0 {
		return steps
	}
	criteriaList, _ := anySlice(criteriaRaw["criteria"])
	out := make([]any, 0, len(criteriaList))
	for _, item := range criteriaList {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		if asString(row["kind"]) == "" {
			row["kind"] = answerKindCriterion
		}
		out = append(out, row)
	}
	return out
}

func leadCriterionStepName(index int, row map[string]any) string {
	id := asString(row["id"])
	if id == "" {
		return fmt.Sprintf("lead_q_%d", index)
	}
	return fmt.Sprintf("lead_q_%s", id)
}

func leadFieldStepName(index int, field map[string]any) string {
	key := strings.TrimSpace(asString(field["key"]))
	if key == "" {
		return fmt.Sprintf("lead_f_%d", index)
	}
	return fmt.Sprintf("lead_f_%s", key)
}

func askCriterion(c *codedflow.Conv, index int, row map[string]any, evalMode string) error {
	name := asString(row["name"])
	question := asString(row["question"])
	weight := asInt(row["weight"])
	stepName := leadCriterionStepName(index, row)

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
		route, ok := askLeadRouteText(c, stepName, codedflow.TextRoutePrompt{
			Body:    question,
			Buttons: buttons,
			Step: codedflow.StepNote{
				Doing:  "Qualify the lead on " + name,
				Expect: "An answer that matches one of the conditions",
			},
		})
		if !ok {
			return nil
		}
		stashCriterionAnswer(c, stepName, name, question, labelByID[route.ID], scoreByID[route.ID], weight)
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
		route, ok = askLeadRouteButtons(c, stepName, codedflow.ButtonPrompt{
			Body:    question,
			Buttons: buttons,
			Step:    note,
		})
	} else {
		items := make([]any, 0, len(buttons))
		for _, button := range buttons {
			items = append(items, map[string]any{
				"id":    button.ID,
				"title": button.Title,
			})
		}
		route, ok = askLeadRouteList(c, stepName, items, codedflow.ListPrompt{
			Body:     question,
			Button:   "Choose",
			Header:   truncate(name, 60),
			Section:  "Options",
			ItemsKey: stepName + "_items",
			IDField:  "id",
			Title:    "title",
			Step:     note,
		})
	}
	if !ok {
		return nil
	}
	stashCriterionAnswer(c, stepName, name, question, labelByID[route.ID], scoreByID[route.ID], weight)
	return nil
}

func askCaptureField(c *codedflow.Conv, index int, field map[string]any) error {
	key := strings.TrimSpace(asString(field["key"]))
	label := strings.TrimSpace(asString(field["label"]))
	if label == "" {
		label = key
	}
	fieldType := strings.TrimSpace(asString(field["type"]))
	help := strings.TrimSpace(asString(field["help_text"]))
	stepName := leadFieldStepName(index, field)
	body := label
	if help != "" {
		body += "\n" + help
	}
	note := codedflow.StepNote{
		Doing:  "Collect lead field " + label,
		Expect: "A free-text reply",
	}

	switch fieldType {
	case "number":
		value, ok := askLeadNumber(c, stepName, codedflow.NumberPrompt{
			Body:    body,
			Pattern: `^-?\d+(\.\d+)?$`,
			Step: codedflow.StepNote{
				Doing:  "Collect lead field " + label,
				Expect: "A number",
			},
		})
		if !ok {
			return nil
		}
		stashFieldAnswer(c, stepName, label, body, value)
		return nil
	case "single_select":
		options := stringOptions(field["options"])
		buttons := make([]codedflow.Button, 0, len(options))
		for i, option := range options {
			buttons = append(buttons, codedflow.Button{
				ID:    fmt.Sprintf("opt_%d", i),
				Title: truncate(option, 24),
			})
		}
		if len(buttons) == 0 {
			text, ok := askLeadText(c, stepName, body, note)
			if !ok {
				return nil
			}
			stashFieldAnswer(c, stepName, label, body, text)
			return nil
		}
		titleMax := 24
		if len(buttons) <= 3 {
			titleMax = 20
		}
		for i := range buttons {
			buttons[i].Title = truncate(buttons[i].Title, titleMax)
		}
		choiceNote := codedflow.StepNote{
			Doing:  "Collect lead field " + label,
			Expect: "One of the offered choices",
		}
		var route codedflow.Route
		var ok bool
		if len(buttons) <= 3 {
			route, ok = askLeadRouteButtons(c, stepName, codedflow.ButtonPrompt{
				Body:    body,
				Buttons: buttons,
				Step:    choiceNote,
			})
		} else {
			items := make([]any, 0, len(buttons))
			for _, button := range buttons {
				items = append(items, map[string]any{
					"id":    button.ID,
					"title": button.Title,
				})
			}
			route, ok = askLeadRouteList(c, stepName, items, codedflow.ListPrompt{
				Body:     body,
				Button:   "Choose",
				Header:   truncate(label, 60),
				Section:  "Options",
				ItemsKey: stepName + "_items",
				IDField:  "id",
				Title:    "title",
				Step:     choiceNote,
			})
		}
		if !ok {
			return nil
		}
		chosen := route.ID
		for i, button := range buttons {
			if button.ID == route.ID {
				chosen = options[i]
				break
			}
		}
		stashFieldAnswer(c, stepName, label, body, chosen)
		return nil
	case "multi_select":
		options := stringOptions(field["options"])
		prompt := body
		if len(options) > 0 {
			prompt += "\nOptions: " + strings.Join(options, ", ")
		}
		prompt += "\nReply with one or more options."
		text, ok := askLeadText(c, stepName, prompt, codedflow.StepNote{
			Doing:  "Collect lead field " + label,
			Expect: "One or more of the listed options",
		})
		if !ok {
			return nil
		}
		stashFieldAnswer(c, stepName, label, prompt, text)
		return nil
	case "image":
		return askCaptureImage(c, stepName, label, body)
	default:
		text, ok := askLeadText(c, stepName, body, note)
		if !ok {
			return nil
		}
		stashFieldAnswer(c, stepName, label, body, text)
		return nil
	}
}

func askCaptureImage(c *codedflow.Conv, stepName, label, body string) error {
	name := stepName + "_img"
	prompt := body
	if !strings.Contains(strings.ToLower(prompt), "photo") {
		prompt += "\nPlease send a photo."
	}
	if rec, ok := consumeNamedCall(c, name); ok {
		if !codedflow.CallOK(rec) {
			return nil
		}
		stashFieldAnswer(c, stepName, label, body, asString(rec["value"]))
		return nil
	}
	if ensureLeadSideQuestionOnImage(c, name, body) {
		return nil
	}
	media := codedflow.InboundMedia{}
	if c.ChatCtx() != nil {
		media = c.ChatCtx().InboundMedia()
	}
	link := strings.TrimSpace(media.DisplayURL)
	if link == "" && strings.TrimSpace(media.MessageID) != "" {
		link = "/api/media/" + strings.TrimSpace(media.MessageID)
	}
	if strings.TrimSpace(media.MediaURL) != "" && link != "" {
		if c.ChatCtx() != nil {
			c.ChatCtx().SetConsumed(true)
		}
		c.AppendCall(map[string]any{"name": name, "ok": true, "value": link})
		stashFieldAnswer(c, stepName, label, body, link)
		return nil
	}
	if c.ChatCtx() != nil && !c.NoAnswerYet() {
		c.ChatCtx().SetConsumed(true)
		prompt = "Please send a photo.\n" + body
	}
	c.Say(prompt)
	c.Wait(name)
	return nil
}

func stringOptions(raw any) []string {
	items, ok := anySlice(raw)
	if !ok {
		if rows, ok := raw.([]string); ok {
			return rows
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(asString(item))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func stashCriterionAnswer(c *codedflow.Conv, stepName, name, question, answer string, score, weight int) {
	stashAnswer(c, stepName, answerKindCriterion, name, question, answer, score, weight)
}

func stashFieldAnswer(c *codedflow.Conv, stepName, name, question, answer string) {
	stashAnswer(c, stepName, answerKindField, name, question, answer, 0, 0)
}

func stashAnswer(c *codedflow.Conv, stepName, kind, name, question, answer string, score, weight int) {
	upsert := func() {
		upsertLeadAnswer(c, kind, name, question, answer, score, weight)
	}
	c.Once("answer_"+stepName, upsert)
	// Once may already have fired with an empty poisoned answer from a
	// stolen DoneCall. Still write a real answer when we have one.
	if strings.TrimSpace(answer) != "" {
		upsert()
	}
}

func upsertLeadAnswer(c *codedflow.Conv, kind, name, question, answer string, score, weight int) {
	if c == nil || c.Session() == nil {
		return
	}
	rows := answersFromSessionData(c.Session().SessionData)
	for i, item := range rows {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		if asString(row["kind"]) != kind {
			continue
		}
		if !strings.EqualFold(asString(row["criterion_name"]), name) {
			continue
		}
		row["question"] = question
		row["answer"] = answer
		row["score"] = score
		row["weight"] = weight
		row["position"] = i
		rows[i] = row
		c.Session().SessionData[sessionAnswers] = rows
		return
	}
	rows = append(rows, map[string]any{
		"kind":           kind,
		"criterion_name": name,
		"question":       question,
		"answer":         answer,
		"score":          score,
		"weight":         weight,
		"position":       len(rows),
	})
	c.Session().SessionData[sessionAnswers] = rows
}

func finishQualified(c *codedflow.Conv) error {
	writeClassificationFromSession(c, true)
	if err := SaveLeadFromSession(c, true, asString(c.Session().SessionData[sessionCompletion])); err != nil {
		c.App().LogWarn("tiqr leads save failed", "error", err)
	}
	thankYou := asString(c.Session().SessionData[sessionThankYou])
	if thankYou == "" {
		thankYou = "Thanks for your answers. We'll be in touch soon."
	}
	c.Once("leads_thank_you", func() {
		c.Say(thankYou)
	})
	return runHelpPath(c, false)
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
	name := strings.TrimSpace(asString(c.Session().SessionData["lead_f_name"]))
	if name == "" {
		name = asString(c.Session().SessionData["contact_name"])
	}
	params := map[string]string{
		"session_id":        c.Session().ID.String(),
		"phone_number":      c.Session().PhoneNumber,
		"name":              name,
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

// SessionHandoffSummaryKey is the session_data key for the staff-only lead
// handoff summary. The customer never receives this text.
const SessionHandoffSummaryKey = sessionHandoffSummary

// PrepareHandoff saves a partial lead (when not already saved) and stashes the
// staff-only handoff summary on the session. It returns the original customer
// transfer message unchanged — the summary is for store/agent notes only.
// Completion modes that already called SaveLeadFromSession keep the qualified
// lead; mid-chat handoffs create a partial one.
// Preview capture writes classification only — create_lead stays mockable via Store.
func PrepareHandoff(c *codedflow.Conv, message string) string {
	writeClassificationFromSession(c, false)
	if c != nil && c.ChatCtx() != nil && !c.ChatCtx().Capturing() {
		_ = SaveLeadFromSession(c, false, completionMidChat)
	}
	stashHandoffSummary(c)
	return message
}

func stashHandoffSummary(c *codedflow.Conv) {
	if c == nil || c.Session() == nil {
		return
	}
	if c.Session().SessionData == nil {
		c.Session().SessionData = models.JSONB{}
	}
	if summary := buildHandoffSummary(c.Session()); summary != "" {
		c.Session().SessionData[sessionHandoffSummary] = summary
	}
}
