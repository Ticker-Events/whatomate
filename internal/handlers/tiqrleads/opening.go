package tiqrleads

import (
	"strings"
	"unicode"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	sessionOpeningText     = "leads_opening_text"
	sessionOpeningKind     = "leads_opening_kind"
	sessionHelpMode        = "leads_help_mode"
	sessionHelpTurn        = "leads_help_turn"
	sessionPendingQuestion = "leads_pending_question"
	sessionOpenOutcome     = "leads_open_outcome"
	sessionOpenAnswer      = "leads_open_answer"

	openingKindGeneric  = "generic"
	openingKindQuestion = "question"

	helpModePostQualify = "post_qualify"
	helpModeReentry     = "reentry"

	helpOutcomeFAQ        = "faq"
	helpOutcomeStoreInfo  = "store_info"
	helpOutcomeStoreFacts = "store_facts"
	helpOutcomeRefuse     = "refuse"
	helpOutcomeMiss       = "miss"
	helpOutcomeStaff      = "staff"

	sessionOpeningHandled = "leads_opening_handled"

	howCanIHelpMessage   = "How can I help you?"
	anythingElseMessage  = "Is there anything else I can help with?"
	openingBridgeMessage = "I can also note a few details for the team."
)

// EnsureOpeningText stores unanswered pre-flow inbound text once on the session.
// prior is session messages already logged for this session (oldest first).
func EnsureOpeningText(session *models.ChatbotSession, current string, prior []string) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	if strings.TrimSpace(asString(session.SessionData[sessionOpeningText])) != "" {
		return
	}
	parts := make([]string, 0, len(prior)+1)
	seen := map[string]bool{}
	for _, line := range prior {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		parts = append(parts, line)
	}
	current = strings.TrimSpace(current)
	if current != "" && !seen[current] {
		parts = append(parts, current)
	}
	session.SessionData[sessionOpeningText] = strings.Join(parts, "\n")
}

// UnansweredInboundTexts returns trailing inbound messages with no later outbound.
func UnansweredInboundTexts(messages []models.ChatbotSessionMessage) []string {
	if len(messages) == 0 {
		return nil
	}
	lastOut := -1
	for i, msg := range messages {
		if msg.Direction == models.DirectionOutgoing {
			lastOut = i
		}
	}
	out := make([]string, 0)
	for i := lastOut + 1; i < len(messages); i++ {
		msg := messages[i]
		if msg.Direction != models.DirectionIncoming {
			continue
		}
		text := strings.TrimSpace(msg.Message)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func classifyOpeningKind(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return openingKindGeneric
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if !isGenericOpeningLine(line) {
			return openingKindQuestion
		}
	}
	return openingKindGeneric
}

func normalizePhrase(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return -1
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func isGenericOpeningLine(line string) bool {
	line = normalizePhrase(line)
	if line == "" {
		return true
	}
	generics := map[string]bool{
		"hi": true, "hii": true, "hiii": true, "hello": true, "hey": true, "heyy": true,
		"good morning": true, "good afternoon": true, "good evening": true, "good night": true,
		"gm": true, "gn": true, "morning": true, "evening": true,
		"ok": true, "okay": true, "k": true, "kk": true, "yes": true, "y": true, "yeah": true, "yep": true,
		"thanks": true, "thank you": true, "thankyou": true, "thx": true, "ty": true,
		"namaste": true, "vanakkam": true, "hola": true, "hola hi": true,
		"hai": true, "helo": true, "hellow": true,
	}
	return generics[line]
}

func isDeclineHelp(text string) bool {
	text = normalizePhrase(text)
	if text == "" {
		return false
	}
	declines := map[string]bool{
		"no": true, "nope": true, "nah": true, "n": true,
		"thats all": true, "that is all": true,
		"nothing": true, "nothing else": true, "no thanks": true, "no thank you": true,
		"thanks": true, "thank you": true, "thankyou": true, "thx": true, "ty": true,
		"all good": true, "im good": true, "done": true,
	}
	return declines[text]
}

type helpLookupResult struct {
	Kind   string // faq, store_info, refuse, miss, staff
	Answer string
}

func faqsFromSession(data models.JSONB) map[string]string {
	out := map[string]string{}
	if data == nil {
		return out
	}
	items, _ := anySlice(data["faqs"])
	for _, item := range items {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		id := strings.TrimSpace(asString(row["id"]))
		q := strings.TrimSpace(asString(row["question"]))
		if id == "" || q == "" {
			continue
		}
		out[id] = q
	}
	return out
}

func faqAnswerFromSession(data models.JSONB, id string) string {
	id = strings.TrimSpace(id)
	if data == nil || id == "" {
		return ""
	}
	items, _ := anySlice(data["faqs"])
	for _, item := range items {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		if asString(row["id"]) == id {
			return strings.TrimSpace(asString(row["answer"]))
		}
	}
	return ""
}

func exactFAQID(message string, faqs map[string]string) string {
	norm := normalizeHelpText(message)
	if norm == "" {
		return ""
	}
	for id, question := range faqs {
		if normalizeHelpText(question) == norm {
			return id
		}
	}
	return ""
}

func normalizeHelpText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return -1
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// classifyHelpQuestion maps free text to a grounded FAQ, store-info answer,
// refuse, staff handoff, or miss. It does not send messages or transfer.
// Intent AI is always noted via Conv helpers. Store information is only tried
// after FAQ matching fails.
func classifyHelpQuestion(c *codedflow.Conv, message string) helpLookupResult {
	message = strings.TrimSpace(message)
	if message == "" {
		return helpLookupResult{Kind: helpOutcomeMiss}
	}
	faqs := faqsFromSession(c.Session().SessionData)
	if id := exactFAQID(message, faqs); id != "" {
		if answer := faqAnswerFromSession(c.Session().SessionData, id); answer != "" {
			c.NoteLocalIntent(codedflow.IntentResult{
				Route:      codedflow.RouteFAQ,
				FaqID:      id,
				Confidence: 1,
				Reasoning:  "exact FAQ question match",
			})
			return helpLookupResult{Kind: helpOutcomeFAQ, Answer: answer}
		}
	}

	ctx := codedflow.IntentContext{
		AllowFAQ:           true,
		AllowRefuse:        true,
		Question:           "(help after lead qualification)",
		Doing:              "Answer a customer question from the store FAQ first, then store information",
		Expect:             "A store FAQ match, refuse for off-topic or unsafe asks, or handoff when they ask for a person",
		ChoiceIDs:          map[string]string{},
		Collections:        map[string]string{},
		FAQs:               faqs,
		SupportedLanguages: nil,
	}
	if c.App() != nil && c.Session() != nil {
		ctx.SupportedLanguages = c.App().CodedFlowSupportedLanguages(c.Session())
	}

	raw, err := c.ClassifyIntent(message, ctx)
	if err != nil {
		return tryGroundedStoreAnswer(c, message)
	}
	result, grounded := codedflow.ValidateCodedIntent(raw, ctx)
	if !grounded {
		return tryGroundedStoreAnswer(c, message)
	}
	if result.Confidence < codedflow.IntentThreshold() && result.Route != codedflow.RouteRefuse {
		return tryGroundedStoreAnswer(c, message)
	}
	switch result.Route {
	case codedflow.RouteFAQ:
		answer := faqAnswerFromSession(c.Session().SessionData, result.FaqID)
		if answer == "" {
			return tryGroundedStoreAnswer(c, message)
		}
		return helpLookupResult{Kind: helpOutcomeFAQ, Answer: answer}
	case codedflow.RouteRefuse:
		return helpLookupResult{Kind: helpOutcomeRefuse}
	case codedflow.RouteHandoff:
		if codedflow.WantsPerson(message) {
			return helpLookupResult{Kind: helpOutcomeStaff}
		}
		return tryGroundedStoreAnswer(c, message)
	default:
		return tryGroundedStoreAnswer(c, message)
	}
}

// tryGroundedStoreAnswer checks structured store fields first, then policy text.
func tryGroundedStoreAnswer(c *codedflow.Conv, message string) helpLookupResult {
	if facts := tryStoreFactsHelp(c, message); facts.Kind == helpOutcomeStoreFacts {
		return facts
	}
	if answer, ok := c.TryStoreInfoAnswer(message); ok {
		return helpLookupResult{Kind: helpOutcomeStoreInfo, Answer: answer}
	}
	return helpLookupResult{Kind: helpOutcomeMiss}
}
