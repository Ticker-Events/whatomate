package tiqrleads

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
)

const (
	midFlowUnsureMessage = "I'm not sure about your question."
	continueLeadMessage  = "Let's continue where we left off."
	sessionResumePartial = "leads_resume_partial"
)

// askLeadRouteButtons asks a button prompt, answering side questions without
// leaving the lead step.
func askLeadRouteButtons(
	c *codedflow.Conv,
	stepName string,
	prompt codedflow.ButtonPrompt,
) (codedflow.Route, bool) {
	if c.Stop {
		return codedflow.Route{}, false
	}
	if route, done, ok := replayLeadRoute(c); done {
		return route, ok
	}
	choices := choiceMapFromButtons(prompt.Buttons)
	if id := matchedButtonID(c, choices); id != "" {
		return acceptLeadChoice(c, stepName, id, choices[id])
	}
	if c.NoAnswerYet() {
		return sendLeadButtons(c, stepName, prompt), false
	}
	text := strings.TrimSpace(c.ChatCtx().UserInput())
	if id := matchChoiceTitle(text, choices); id != "" {
		return acceptLeadChoice(c, stepName, id, choices[id])
	}
	if handled := handleLeadSideQuestion(c, stepName, text, func() {
		_ = sendLeadButtons(c, stepName, prompt)
	}); handled {
		return codedflow.Route{}, false
	}
	if id, title, ok := matchChoiceViaIntent(c, text, prompt.Body, prompt.Step, choices); ok {
		return acceptLeadChoice(c, stepName, id, title)
	}
	return reaskAfterUnsure(c, stepName, func() {
		_ = sendLeadButtons(c, stepName, prompt)
	}), false
}

// askLeadRouteList asks a list prompt with the same side-question behavior.
func askLeadRouteList(
	c *codedflow.Conv,
	stepName string,
	items []any,
	prompt codedflow.ListPrompt,
) (codedflow.Route, bool) {
	if c.Stop {
		return codedflow.Route{}, false
	}
	if route, done, ok := replayLeadRoute(c); done {
		return route, ok
	}
	if prompt.ItemsKey != "" && items != nil {
		c.Session().SessionData[prompt.ItemsKey] = items
	}
	choices := choiceMapFromListItems(items, prompt.IDField, prompt.Title)
	if id := matchedButtonID(c, choices); id != "" {
		return acceptLeadChoice(c, stepName, id, choices[id])
	}
	if c.NoAnswerYet() {
		return sendLeadList(c, stepName, prompt), false
	}
	text := strings.TrimSpace(c.ChatCtx().UserInput())
	if id := matchChoiceTitle(text, choices); id != "" {
		return acceptLeadChoice(c, stepName, id, choices[id])
	}
	if handled := handleLeadSideQuestion(c, stepName, text, func() {
		_ = sendLeadList(c, stepName, prompt)
	}); handled {
		return codedflow.Route{}, false
	}
	if id, title, ok := matchChoiceViaIntent(c, text, prompt.Body, prompt.Step, choices); ok {
		return acceptLeadChoice(c, stepName, id, title)
	}
	return reaskAfterUnsure(c, stepName, func() {
		_ = sendLeadList(c, stepName, prompt)
	}), false
}

// askLeadRouteText asks a free-text question classified onto Buttons (hybrid rules).
func askLeadRouteText(
	c *codedflow.Conv,
	stepName string,
	prompt codedflow.TextRoutePrompt,
) (codedflow.Route, bool) {
	if c.Stop {
		return codedflow.Route{}, false
	}
	if route, done, ok := replayLeadRoute(c); done {
		return route, ok
	}
	choices := choiceMapFromButtons(prompt.Buttons)
	if c.NoAnswerYet() {
		body := c.Text(prompt.Body)
		if _, err := c.App().ExecChatMessage(c.ChatCtx(), stepName, map[string]any{"message": body}); err != nil {
			c.Fail(err)
			return codedflow.Route{}, false
		}
		if c.ChatCtx().Capturing() {
			c.ChatCtx().Preview().ExpectText()
		}
		c.Wait(stepName)
		return codedflow.Route{}, false
	}
	text := strings.TrimSpace(c.ChatCtx().UserInput())
	if id := matchChoiceTitle(text, choices); id != "" {
		return acceptLeadChoice(c, stepName, id, choices[id])
	}
	if handled := handleLeadSideQuestion(c, stepName, text, func() {
		body := c.Text(prompt.Body)
		_, _ = c.App().ExecChatMessage(c.ChatCtx(), stepName, map[string]any{"message": body})
	}); handled {
		return codedflow.Route{}, false
	}
	if id, title, ok := matchChoiceViaIntent(c, text, prompt.Body, prompt.Step, choices); ok {
		return acceptLeadChoice(c, stepName, id, title)
	}
	return reaskAfterUnsure(c, stepName, func() {
		body := c.Text(prompt.Body)
		_, _ = c.App().ExecChatMessage(c.ChatCtx(), stepName, map[string]any{"message": body})
	}), false
}

// askLeadText collects free text, diverting only for FAQ / refuse / staff / obvious questions.
func askLeadText(c *codedflow.Conv, stepName, body string, step codedflow.StepNote) (string, bool) {
	if c.Stop {
		return "", false
	}
	if rec, done := c.DoneCall(); done {
		if !codedflow.CallOK(rec) {
			return "", false
		}
		return asString(c.Session().SessionData[stepName]), true
	}
	message := c.Text(body)
	if c.NoAnswerYet() {
		if _, err := c.App().ExecChatMessage(c.ChatCtx(), stepName, map[string]any{"message": message}); err != nil {
			c.Fail(err)
			return "", false
		}
		if c.ChatCtx().Capturing() {
			c.ChatCtx().Preview().ExpectText()
		}
		c.Wait(stepName)
		return "", false
	}
	text := strings.TrimSpace(c.ChatCtx().UserInput())
	if text == "" {
		return "", false
	}
	if shouldDivertFreeText(c, text) {
		if handled := handleLeadSideQuestion(c, stepName, text, func() {
			_, _ = c.App().ExecChatMessage(c.ChatCtx(), stepName, map[string]any{"message": message})
		}); handled {
			return "", false
		}
	}
	c.ChatCtx().SetConsumed(true)
	c.Session().SessionData[stepName] = text
	c.AppendCall(map[string]any{"name": stepName, "ok": true, "var": stepName, "value": text})
	return text, true
}

// askLeadNumber collects a number, diverting side questions the same way as free text.
func askLeadNumber(c *codedflow.Conv, stepName string, prompt codedflow.NumberPrompt) (string, bool) {
	if c.Stop {
		return "", false
	}
	if rec, done := c.DoneCall(); done {
		if !codedflow.CallOK(rec) {
			return "", false
		}
		return asString(c.Session().SessionData[stepName]), true
	}
	body := c.Text(prompt.Body)
	if c.NoAnswerYet() {
		if _, err := c.App().ExecChatMessage(c.ChatCtx(), stepName, map[string]any{"message": body}); err != nil {
			c.Fail(err)
			return "", false
		}
		if c.ChatCtx().Capturing() {
			c.ChatCtx().Preview().ExpectText()
		}
		c.Wait(stepName)
		return "", false
	}
	text := strings.TrimSpace(c.ChatCtx().UserInput())
	re, err := regexp.Compile(prompt.Pattern)
	if err != nil {
		c.Fail(err)
		return "", false
	}
	if re.MatchString(text) {
		c.ChatCtx().SetConsumed(true)
		c.Session().SessionData[stepName] = text
		c.AppendCall(map[string]any{"name": stepName, "ok": true, "var": stepName, "value": text})
		return text, true
	}
	if shouldDivertFreeText(c, text) || looksLikeQuestion(text) {
		if handled := handleLeadSideQuestion(c, stepName, text, func() {
			_, _ = c.App().ExecChatMessage(c.ChatCtx(), stepName, map[string]any{"message": body})
		}); handled {
			return "", false
		}
	}
	// Not a number and not a diverted question — re-prompt for a number.
	c.ChatCtx().SetConsumed(true)
	notice := c.Text("Please reply with a number.")
	_ = c.App().DeliverCodedText(c.ChatCtx(), stepName+"_retry", notice)
	_, _ = c.App().ExecChatMessage(c.ChatCtx(), stepName, map[string]any{"message": body})
	c.Wait(stepName)
	return "", false
}

func shouldDivertFreeText(c *codedflow.Conv, text string) bool {
	if looksLikeQuestion(text) {
		return true
	}
	outcome := classifyHelpQuestion(c, text)
	return outcome.Kind == helpOutcomeFAQ ||
		outcome.Kind == helpOutcomeRefuse ||
		outcome.Kind == helpOutcomeStaff
}

func looksLikeQuestion(text string) bool {
	t := strings.TrimSpace(strings.ToLower(text))
	if t == "" {
		return false
	}
	if strings.Contains(t, "?") {
		return true
	}
	prefixes := []string{
		"what ", "what's ", "whats ", "when ", "where ", "who ", "why ", "how ",
		"can ", "could ", "would ", "will ", "do you", "does ", "is there",
		"are you", "are there", "tell me", "explain ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

// handleLeadSideQuestion answers FAQ/refuse or transfers for staff. For miss it
// sends an unsure line. Always re-asks the lead prompt except on staff transfer.
// Returns true when the inbound was consumed as a side question.
func handleLeadSideQuestion(c *codedflow.Conv, stepName, text string, reask func()) bool {
	outcome := classifyHelpQuestion(c, text)
	switch outcome.Kind {
	case helpOutcomeFAQ:
		c.ChatCtx().SetConsumed(true)
		deliverHelpLine(c, stepName+"_faq", outcome.Answer)
		reask()
		c.Wait(stepName)
		return true
	case helpOutcomeRefuse:
		c.ChatCtx().SetConsumed(true)
		deliverHelpLine(c, stepName+"_refuse", codedflow.RefuseMessage)
		reask()
		c.Wait(stepName)
		return true
	case helpOutcomeStaff:
		c.ChatCtx().SetConsumed(true)
		_ = c.Transfer(codedflow.AgentHandoff)
		return true
	case helpOutcomeMiss:
		if !looksLikeQuestion(text) {
			// Caller decides whether this is still an answer (free text) or unsure (choices).
			return false
		}
		c.ChatCtx().SetConsumed(true)
		deliverHelpLine(c, stepName+"_unsure", midFlowUnsureMessage)
		reask()
		c.Wait(stepName)
		return true
	default:
		return false
	}
}

func reaskAfterUnsure(c *codedflow.Conv, stepName string, reask func()) codedflow.Route {
	c.ChatCtx().SetConsumed(true)
	deliverHelpLine(c, stepName+"_unsure", midFlowUnsureMessage)
	reask()
	c.Wait(stepName)
	return codedflow.Route{}
}

func replayLeadRoute(c *codedflow.Conv) (codedflow.Route, bool, bool) {
	rec, done := c.DoneCall()
	if !done {
		return codedflow.Route{}, false, false
	}
	if !codedflow.CallOK(rec) {
		return codedflow.Route{}, true, false
	}
	kind := asString(rec["route"])
	if kind == "" {
		kind = codedflow.RouteChoice
	}
	return codedflow.Route{
		Kind:  kind,
		ID:    asString(rec["id"]),
		Title: asString(rec["title"]),
	}, true, true
}

func acceptLeadChoice(c *codedflow.Conv, stepName, id, title string) (codedflow.Route, bool) {
	c.ChatCtx().SetConsumed(true)
	route := codedflow.Route{Kind: codedflow.RouteChoice, ID: id, Title: title}
	c.AppendCall(map[string]any{
		"name":  stepName,
		"ok":    true,
		"route": route.Kind,
		"id":    route.ID,
		"title": route.Title,
	})
	return route, true
}

func choiceMapFromButtons(buttons []codedflow.Button) map[string]string {
	out := make(map[string]string, len(buttons))
	for _, button := range buttons {
		id := strings.TrimSpace(button.ID)
		if id == "" {
			continue
		}
		out[id] = button.Title
	}
	return out
}

func choiceMapFromListItems(items []any, idField, titleField string) map[string]string {
	if idField == "" {
		idField = "id"
	}
	if titleField == "" {
		titleField = "title"
	}
	out := map[string]string{}
	for _, item := range items {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		id := strings.TrimSpace(asString(row[idField]))
		if id == "" {
			continue
		}
		out[id] = asString(row[titleField])
	}
	return out
}

func matchedButtonID(c *codedflow.Conv, choices map[string]string) string {
	if c.ChatCtx() == nil {
		return ""
	}
	id := strings.TrimSpace(c.ChatCtx().ButtonID())
	if id == "" {
		return ""
	}
	if _, ok := choices[id]; ok {
		return id
	}
	return ""
}

func matchChoiceTitle(text string, choices map[string]string) string {
	norm := normalizeLeadText(text)
	if norm == "" {
		return ""
	}
	for id, title := range choices {
		if normalizeLeadText(title) == norm {
			return id
		}
	}
	return ""
}

func matchChoiceViaIntent(
	c *codedflow.Conv,
	message, question string,
	step codedflow.StepNote,
	choices map[string]string,
) (string, string, bool) {
	if len(choices) == 0 || strings.TrimSpace(message) == "" {
		return "", "", false
	}
	ctx := codedflow.IntentContext{
		AllowFAQ:    false,
		AllowRefuse: false,
		Question:    question,
		Doing:       step.Doing,
		Expect:      step.Expect,
		ChoiceIDs:   choices,
		FAQs:        map[string]string{},
	}
	if c.App() != nil && c.Session() != nil {
		ctx.SupportedLanguages = c.App().CodedFlowSupportedLanguages(c.Session())
	}
	raw, err := c.ClassifyIntent(message, ctx)
	if err != nil {
		return "", "", false
	}
	result, grounded := codedflow.ValidateCodedIntent(raw, ctx)
	if !grounded || result.Confidence < codedflow.IntentThreshold() {
		return "", "", false
	}
	if result.Route != codedflow.RouteChoice {
		return "", "", false
	}
	title, ok := choices[result.ChoiceID]
	if !ok {
		return "", "", false
	}
	return result.ChoiceID, title, true
}

func sendLeadButtons(c *codedflow.Conv, stepName string, prompt codedflow.ButtonPrompt) codedflow.Route {
	buttons := make([]any, 0, len(prompt.Buttons))
	for _, button := range prompt.Buttons {
		buttons = append(buttons, map[string]any{
			"id":    button.ID,
			"type":  "reply",
			"title": truncate(button.Title, 20),
		})
	}
	cfg := map[string]any{
		"body":    c.Text(prompt.Body),
		"mode":    "reply",
		"source":  "static",
		"buttons": buttons,
	}
	if strings.TrimSpace(prompt.Step.Doing) != "" {
		cfg["step_doing"] = prompt.Step.Doing
	}
	if strings.TrimSpace(prompt.Step.Expect) != "" {
		cfg["step_expect"] = prompt.Step.Expect
	}
	out, err := c.App().ExecChatButtons(c.ChatCtx(), stepName, cfg)
	if err != nil {
		c.Fail(err)
		return codedflow.Route{}
	}
	if out.Yield {
		c.Wait(stepName)
	}
	return codedflow.Route{}
}

func sendLeadList(c *codedflow.Conv, stepName string, prompt codedflow.ListPrompt) codedflow.Route {
	cfg := map[string]any{
		"body":          c.Text(prompt.Body),
		"mode":          "list",
		"header":        truncate(prompt.Header, 60),
		"footer":        truncate(prompt.Footer, 60),
		"source":        "dynamic",
		"id_field":      prompt.IDField,
		"items_var":     prompt.ItemsKey,
		"list_button":   truncate(prompt.Button, 20),
		"title_field":   prompt.Title,
		"section_title": truncate(prompt.Section, 24),
	}
	if strings.TrimSpace(prompt.Step.Doing) != "" {
		cfg["step_doing"] = prompt.Step.Doing
	}
	if strings.TrimSpace(prompt.Step.Expect) != "" {
		cfg["step_expect"] = prompt.Step.Expect
	}
	out, err := c.App().ExecChatButtons(c.ChatCtx(), stepName, cfg)
	if err != nil {
		c.Fail(err)
		return codedflow.Route{}
	}
	if out.Yield {
		c.Wait(stepName)
	}
	return codedflow.Route{}
}

func normalizeLeadText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func stepAlreadyAnswered(c *codedflow.Conv, kind, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || c == nil || c.Session() == nil {
		return false
	}
	for _, item := range answersFromSessionData(c.Session().SessionData) {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		if asString(row["kind"]) != kind {
			continue
		}
		if strings.EqualFold(asString(row["criterion_name"]), name) {
			return true
		}
	}
	return false
}

func leadExists(lead map[string]any) bool {
	return lead != nil && asString(lead["id"]) != ""
}

func seedPartialLeadAnswers(c *codedflow.Conv, lead map[string]any) {
	c.Once("leads_seed_partial", func() {
		if answers, ok := anySlice(lead["answers"]); ok && len(answers) > 0 {
			c.Session().SessionData[sessionAnswers] = answers
		}
		c.Session().SessionData[sessionResumePartial] = "1"
		c.Session().SessionData[sessionScore] = asInt(lead["score"])
		c.Session().SessionData[sessionCategory] = asString(lead["category"])
		c.Session().SessionData[sessionQualified] = false
		if name := strings.TrimSpace(asString(lead["name"])); name != "" {
			c.Session().SessionData["lead_f_name"] = name
		}
	})
}

func criterionAnswerName(row map[string]any) string {
	name := strings.TrimSpace(asString(row["name"]))
	if name != "" {
		return name
	}
	return strings.TrimSpace(asString(row["label"]))
}

func fieldAnswerName(row map[string]any) string {
	label := strings.TrimSpace(asString(row["label"]))
	if label != "" {
		return label
	}
	return strings.TrimSpace(asString(row["key"]))
}

// ensureLeadSideQuestionOnImage handles a text side-question while waiting for a photo.
func ensureLeadSideQuestionOnImage(c *codedflow.Conv, stepName, body string) bool {
	if c.NoAnswerYet() {
		return false
	}
	text := strings.TrimSpace(c.ChatCtx().UserInput())
	if text == "" {
		return false
	}
	media := codedflow.InboundMedia{}
	if c.ChatCtx() != nil {
		media = c.ChatCtx().InboundMedia()
	}
	if strings.TrimSpace(media.MediaURL) != "" || strings.TrimSpace(media.MessageID) != "" {
		return false
	}
	prompt := body
	if !strings.Contains(strings.ToLower(prompt), "photo") {
		prompt += "\nPlease send a photo."
	}
	reask := func() {
		_ = c.App().DeliverCodedText(c.ChatCtx(), stepName, c.Text(prompt))
	}
	if handleLeadSideQuestion(c, stepName, text, reask) {
		return true
	}
	if !looksLikeQuestion(text) {
		return false
	}
	c.ChatCtx().SetConsumed(true)
	deliverHelpLine(c, stepName+"_unsure", midFlowUnsureMessage)
	reask()
	c.Wait(stepName)
	return true
}
