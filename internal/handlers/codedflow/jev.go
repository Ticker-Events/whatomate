package codedflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	typeSafeSystemOneURL  = "https://api.typesafe.ai/v1/systemone"
	aiGatewaySystemOneURL = "https://ai-gateway.vercel.sh/typesafe/v1/systemone"

	jevJudgeYes      = 0.8
	jevJudgeNo       = 0.2
	maxFAQCandidates = 8
)

type JevEndpoint struct {
	URL   string
	Key   string
	Model string
}

type jevChoiceAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

type jevNoulAnswer struct {
	Type string  `json:"type"`
	Noul float64 `json:"noul"`
}

type JevSystemOneResponse struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
}

type faqCandidate struct {
	ID       string
	Question string
}

func codedIntentProvider(settings *models.ChatbotSettings) models.IntentProvider {
	return codedFlowRoleProvider(settings, codedFlowRoleIntent)
}

func jevEndpointFor(settings *models.ChatbotSettings) (JevEndpoint, error) {
	if settings == nil {
		return JevEndpoint{}, fmt.Errorf("ai is not configured")
	}
	switch codedIntentProvider(settings) {
	case models.IntentProviderJev:
		key := strings.TrimSpace(settings.AI.TypeSafeAPIKey)
		if key == "" {
			return JevEndpoint{}, fmt.Errorf("typesafe api key is not configured")
		}
		return JevEndpoint{URL: typeSafeSystemOneURL, Key: key, Model: models.IntentTypeSafeModel}, nil
	case models.IntentProviderGateway:
		key := strings.TrimSpace(settings.AI.GatewayAPIKey)
		if key == "" {
			return JevEndpoint{}, fmt.Errorf("ai gateway api key is not configured")
		}
		model := strings.TrimSpace(settings.AI.GatewayModel)
		if model == "" {
			model = models.IntentGatewayModelJev
		}
		if !models.ValidIntentGatewayModel(model) {
			return JevEndpoint{}, fmt.Errorf("invalid ai gateway model: %s", model)
		}
		return JevEndpoint{URL: aiGatewaySystemOneURL, Key: key, Model: model}, nil
	default:
		return JevEndpoint{}, fmt.Errorf("intent provider is not jev or gateway")
	}
}

func identifyCodedIntentJev(a Host, settings *models.ChatbotSettings, message string, ctx codedIntentContext) (codedIntentResult, error) {
	endpoint, err := jevEndpointFor(settings)
	if err != nil {
		return codedIntentResult{}, err
	}
	state, questions := buildJevIntentRequest(message, ctx)
	raw, err := a.CallSystemOne(endpoint, state, questions)
	if err != nil {
		return codedIntentResult{}, err
	}
	result, err := composeJevIntent(raw, message, ctx)
	if err != nil {
		return result, err
	}
	if result.Route != codedRouteFAQ || result.Confidence <= 0 {
		return result, nil
	}
	if result.FaqID != "" {
		return result, nil
	}
	if id := exactFAQMatch(message, ctx.FAQs); id != "" {
		result.FaqID = id
		result.Reasoning += " faq=exact"
		return result, nil
	}
	if len(ctx.FAQs) == 0 {
		return result, nil
	}
	faqState, faqQuestions := buildJevFAQRequest(message, ctx.FAQs)
	faqRaw, err := a.CallSystemOne(endpoint, faqState, faqQuestions)
	if err != nil {
		return codedIntentResult{}, err
	}
	faqAns, ok := parseJevChoice(faqRaw.Answers["faq_id"])
	if !ok || !choiceConfidenceUsable(faqAns) || faqAns.Choice == "" || faqAns.Choice == "none" {
		result.Route = codedRouteUnclear
		result.FaqID = ""
		result.Confidence = 0
		result.Reasoning += " faq_id unavailable"
		return result, nil
	}
	if _, exists := ctx.FAQs[faqAns.Choice]; !exists {
		result.Route = codedRouteUnclear
		result.FaqID = ""
		result.Confidence = 0
		result.Reasoning += " faq_id ungrounded"
		return result, nil
	}
	result.FaqID = faqAns.Choice
	if faqAns.Confidence > 0 {
		result.Confidence = faqAns.Confidence
	}
	result.Reasoning += " faq=follow_up"
	return result, nil
}

func buildJevIntentRequest(message string, ctx codedIntentContext) (map[string]any, map[string]any) {
	choices := make([]map[string]string, 0, len(ctx.ChoiceIDs))
	for id, title := range ctx.ChoiceIDs {
		choices = append(choices, map[string]string{"id": id, "title": title})
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i]["id"] < choices[j]["id"] })

	collections := make([]map[string]string, 0, len(ctx.Collections))
	for id, name := range ctx.Collections {
		collections = append(collections, map[string]string{"id": id, "name": name})
	}
	sort.Slice(collections, func(i, j int) bool { return collections[i]["id"] < collections[j]["id"] })

	state := map[string]any{
		"message":  message,
		"question": ctx.Question,
		"doing":    ctx.Doing,
		"expect":   ctx.Expect,
		"pattern":  ctx.Pattern,
		"choices":  choices,
	}
	if len(collections) > 0 {
		state["collections"] = collections
	}

	routeCriteria := map[string]any{
		"choice": map[string]any{
			"what":    "They picked one of the shown choices, including a close misspelling of a shown title.",
			"not_for": "Naming a product to search, naming a collection as a group, asking for checkout, or asking for a person when Talk to staff is not a shown choice.",
		},
		"checkout": map[string]any{
			"what":    "They want to check out, place the order, pay, or finish shopping with what is already in the cart.",
			"not_for": "Checking an existing order status.",
		},
		"handoff": map[string]any{
			"what":    "They ask for a human, agent, or staff, or they seem stuck and no shown choice matches.",
			"not_for": "Picking Talk to staff when that id is in the shown choices.",
		},
		"unclear": map[string]any{
			"what": "More than one shopping route fits, or none of them fit, and they are not asking for a person.",
		},
	}
	includeCollectionRoute := len(collections) > 0
	if includeCollectionRoute {
		routeCriteria["collection"] = map[string]any{
			"what":    "They want the items in one loaded collection, or ask what is available in that collection. Phrases like buy, I want, I need, or show me usually name a collection or a product.",
			"not_for": "A single product name to search.",
		}
	}
	if ctx.AllowCatalog {
		routeCriteria["product"] = map[string]any{
			"what":    "They name a product or ask to buy a specific item. Phrases like buy, I want, I need, or show me usually name a collection or a product.",
			"not_for": "The Buy products button, or browsing a whole collection.",
		}
	}
	if strings.TrimSpace(ctx.Pattern) != "" {
		routeCriteria["answer"] = map[string]any{
			"what":    "Their reply is a valid answer to the current question but is not one of the choice ids.",
			"not_for": "A button id, collection name, or product name.",
		}
	}
	if ctx.AllowFAQ && len(ctx.FAQs) > 0 {
		routeCriteria["faq"] = map[string]any{
			"what":    "They ask a store policy or FAQ question that the store already answers, including a paraphrase.",
			"not_for": "Buying, browsing a collection, naming a product, checkout, order status, or asking for a person.",
		}
	}

	questions := map[string]any{
		"route": map[string]any{
			"type": "choice",
			"instructions": map[string]any{
				"question": "Which route matches `message` for this step?",
				"doing":    "`doing`",
				"expect":   "`expect`",
				"focus":    "Pick one route. A close misspelling of a shown title still counts as that choice.",
			},
			"criteria": routeCriteria,
		},
		"language": map[string]any{
			"type": "choice",
			"instructions": map[string]any{
				"question": "Which label best describes the language of `message`?",
			},
			"criteria": jevLanguageCriteria(ctx.SupportedLanguages),
		},
	}

	if len(ctx.ChoiceIDs) > 0 {
		choiceCriteria := map[string]any{
			"none": "None of the shown choices match.",
		}
		for id, title := range ctx.ChoiceIDs {
			choiceCriteria[id] = title
		}
		questions["choice_id"] = map[string]any{
			"type": "choice",
			"instructions": map[string]any{
				"question": "Which shown choice id matches `message`?",
			},
			"criteria": choiceCriteria,
		}
		questions["asks_for_person"] = map[string]any{
			"type": "noul",
			"instructions": map[string]any{
				"question": "Is the customer asking for a human, agent, or staff?",
			},
			"criteria": map[string]any{
				"true":  "They want to talk to a person.",
				"false": "They are shopping or answering the current question.",
			},
		}
	}

	if includeCollectionRoute {
		collectionCriteria := map[string]any{
			"none": "None of the loaded collections match.",
		}
		for id, name := range ctx.Collections {
			collectionCriteria[id] = name
		}
		questions["collection_id"] = map[string]any{
			"type": "choice",
			"instructions": map[string]any{
				"question": "Which loaded collection id matches `message`?",
			},
			"criteria": collectionCriteria,
		}
		questions["names_collection"] = map[string]any{
			"type": "noul",
			"instructions": map[string]any{
				"question": "Does `message` refer to one loaded collection as a group to browse?",
			},
			"criteria": map[string]any{
				"true":  "They want that collection's items as a group.",
				"false": "They are not asking for a whole collection.",
			},
		}
	}

	if ctx.AllowCatalog {
		questions["names_product"] = map[string]any{
			"type": "noul",
			"instructions": map[string]any{
				"question": "Does `message` name a specific product or item to search for?",
			},
			"criteria": map[string]any{
				"true":  "They name a product to find or buy.",
				"false": "They are not naming a specific product.",
			},
		}
	}

	if strings.TrimSpace(ctx.Pattern) != "" {
		questions["states_quantity"] = map[string]any{
			"type": "noul",
			"instructions": map[string]any{
				"question": "Is `message` a count of units, written as digits or as a number word?",
				"pattern":  "`pattern`",
			},
			"criteria": map[string]any{
				"true":  "The reply states how many units, for example 2 or two.",
				"false": "The reply is not a quantity.",
			},
		}
	}

	return state, questions
}

func buildJevFAQRequest(message string, faqs map[string]string) (map[string]any, map[string]any) {
	ranked := rankFAQs(message, faqs, maxFAQCandidates)
	items := make([]map[string]string, 0, len(ranked))
	faqCriteria := map[string]any{
		"none": "None of the loaded FAQs match.",
	}
	for _, item := range ranked {
		items = append(items, map[string]string{"id": item.ID, "question": item.Question})
		faqCriteria[item.ID] = item.Question
	}
	state := map[string]any{
		"message": message,
		"faqs":    items,
	}
	questions := map[string]any{
		"faq_id": map[string]any{
			"type": "choice",
			"instructions": map[string]any{
				"question": "Which loaded FAQ id answers `message`?",
			},
			"criteria": faqCriteria,
		},
	}
	return state, questions
}

func exactFAQMatch(message string, faqs map[string]string) string {
	norm := normalizeIntentText(message)
	if norm == "" {
		return ""
	}
	for id, question := range faqs {
		if normalizeIntentText(question) == norm {
			return id
		}
	}
	return ""
}

func rankFAQs(message string, faqs map[string]string, limit int) []faqCandidate {
	if limit <= 0 || len(faqs) == 0 {
		return nil
	}
	msgTokens := intentTokenSet(message)
	type scored struct {
		faqCandidate
		score int
	}
	ranked := make([]scored, 0, len(faqs))
	for id, question := range faqs {
		overlap := 0
		for tok := range intentTokenSet(question) {
			if _, ok := msgTokens[tok]; ok {
				overlap++
			}
		}
		ranked = append(ranked, scored{faqCandidate: faqCandidate{ID: id, Question: question}, score: overlap})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].ID < ranked[j].ID
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]faqCandidate, len(ranked))
	for i, item := range ranked {
		out[i] = item.faqCandidate
	}
	return out
}

func intentTokenSet(s string) map[string]struct{} {
	norm := normalizeIntentText(s)
	out := map[string]struct{}{}
	for _, tok := range strings.Fields(norm) {
		if tok == "" {
			continue
		}
		out[tok] = struct{}{}
	}
	return out
}

var shoppingQueryPrefixes = []string{
	"i want to buy",
	"i want",
	"i need",
	"show me",
	"do you have",
	"looking for",
	"can i get",
	"please send",
	"buy",
}

func productQueryFromMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range shoppingQueryPrefixes {
		if strings.HasPrefix(lower, prefix) {
			rest := strings.TrimSpace(trimmed[len(prefix):])
			rest = strings.TrimLeft(rest, " ,.-:")
			if rest != "" {
				return rest
			}
			return trimmed
		}
	}
	return trimmed
}

func composeJevIntent(raw JevSystemOneResponse, message string, ctx codedIntentContext) (codedIntentResult, error) {
	routeAns, routeOK := parseJevChoice(raw.Answers["route"])
	langAns, _ := parseJevChoice(raw.Answers["language"])
	choiceAns, _ := parseJevChoice(raw.Answers["choice_id"])
	collectionAns, _ := parseJevChoice(raw.Answers["collection_id"])
	faqAns, _ := parseJevChoice(raw.Answers["faq_id"])
	namesCollection, hasNamesCollection := parseJevNoul(raw.Answers["names_collection"])
	namesProduct, hasNamesProduct := parseJevNoul(raw.Answers["names_product"])
	asksPerson, hasAsksPerson := parseJevNoul(raw.Answers["asks_for_person"])
	statesQty, hasStatesQty := parseJevNoul(raw.Answers["states_quantity"])

	result := codedIntentResult{
		Language: "en",
		Route:    codedRouteUnclear,
	}
	if langAns.Choice != "" && choiceConfidenceUsable(langAns) {
		result.Language = normalizeDetectedLanguage(langAns.Choice, ctx.SupportedLanguages)
	} else {
		result.Language = normalizeDetectedLanguage("", ctx.SupportedLanguages)
	}
	if !routeOK || !choiceConfidenceUsable(routeAns) {
		result.Reasoning = "route confidence unavailable"
		return result, nil
	}

	result.Route = routeAns.Choice
	result.Confidence = routeAns.Confidence
	judgeNote := ""

	if hasNamesCollection && hasNamesProduct {
		colYes := namesCollection >= jevJudgeYes
		prodYes := namesProduct >= jevJudgeYes
		colNo := namesCollection <= jevJudgeNo
		prodNo := namesProduct <= jevJudgeNo
		switch {
		case colYes && prodYes:
			result.Route = codedRouteUnclear
			result.Confidence = 0
			judgeNote = "names_collection and names_product both high"
		case colYes && prodNo:
			result.Route = codedRouteCollection
			result.Confidence = namesCollection
			judgeNote = "names_collection"
		case prodYes && colNo:
			result.Route = codedRouteProduct
			result.Confidence = namesProduct
			judgeNote = "names_product"
		case (namesCollection > jevJudgeNo && namesCollection < jevJudgeYes) ||
			(namesProduct > jevJudgeNo && namesProduct < jevJudgeYes):
			if result.Route == codedRouteCollection || result.Route == codedRouteProduct {
				result.Route = codedRouteUnclear
				result.Confidence = 0
				judgeNote = "catalog judge uncertain"
			}
		}
	} else if hasNamesProduct && !hasNamesCollection {
		switch {
		case namesProduct >= jevJudgeYes:
			result.Route = codedRouteProduct
			result.Confidence = namesProduct
			judgeNote = "names_product"
		case namesProduct > jevJudgeNo && namesProduct < jevJudgeYes:
			if result.Route == codedRouteProduct {
				result.Route = codedRouteUnclear
				result.Confidence = 0
				judgeNote = "names_product uncertain"
			}
		}
	}

	if hasAsksPerson {
		switch {
		case asksPerson >= jevJudgeYes:
			if choiceAns.Choice != "" && choiceAns.Choice != "none" && choiceConfidenceUsable(choiceAns) {
				if _, ok := ctx.ChoiceIDs[choiceAns.Choice]; ok && isTalkToStaffChoice(choiceAns.Choice, ctx) {
					result.Route = codedRouteChoice
					result.ChoiceID = choiceAns.Choice
					result.Confidence = choiceAns.Confidence
					judgeNote = "asks_for_person matched talk_to_staff choice"
				} else if result.Route != codedRouteChoice {
					result.Route = codedRouteHandoff
					result.ChoiceID = ""
					result.Confidence = asksPerson
					judgeNote = "asks_for_person"
				}
			} else if result.Route != codedRouteChoice {
				result.Route = codedRouteHandoff
				result.ChoiceID = ""
				result.Confidence = asksPerson
				judgeNote = "asks_for_person"
			}
		case asksPerson > jevJudgeNo && asksPerson < jevJudgeYes:
			if result.Route == codedRouteHandoff {
				result.Route = codedRouteUnclear
				result.Confidence = 0
				judgeNote = "asks_for_person uncertain"
			}
		}
	}

	if hasStatesQty {
		switch {
		case statesQty >= jevJudgeYes:
			result.Route = codedRouteAnswer
			result.Confidence = statesQty
			judgeNote = "states_quantity"
		case statesQty > jevJudgeNo && statesQty < jevJudgeYes:
			if result.Route == codedRouteAnswer {
				result.Route = codedRouteUnclear
				result.Confidence = 0
				judgeNote = "states_quantity uncertain"
			}
		}
	}

	switch result.Route {
	case codedRouteChoice:
		if result.ChoiceID == "" {
			if choiceAns.Choice != "" && choiceAns.Choice != "none" && choiceConfidenceUsable(choiceAns) {
				if _, ok := ctx.ChoiceIDs[choiceAns.Choice]; ok {
					result.ChoiceID = choiceAns.Choice
					if choiceAns.Confidence > 0 {
						result.Confidence = choiceAns.Confidence
					}
				}
			}
		}
	case codedRouteCollection:
		if collectionAns.Choice != "" && collectionAns.Choice != "none" && choiceConfidenceUsable(collectionAns) {
			if _, ok := ctx.Collections[collectionAns.Choice]; ok {
				result.CollectionID = collectionAns.Choice
				if collectionAns.Confidence > 0 {
					result.Confidence = collectionAns.Confidence
				}
			}
		}
	case codedRouteProduct:
		result.ProductQuery = productQueryFromMessage(message)
	case codedRouteAnswer:
		result.Answer = normalizeCodedAnswer(message, ctx.Pattern)
	case codedRouteFAQ:
		if faqAns.Choice != "" && faqAns.Choice != "none" && choiceConfidenceUsable(faqAns) {
			if _, ok := ctx.FAQs[faqAns.Choice]; ok {
				result.FaqID = faqAns.Choice
				if faqAns.Confidence > 0 {
					result.Confidence = faqAns.Confidence
				}
			}
		}
	}

	result.Reasoning = fmt.Sprintf("route=%s confidence=%.2f", result.Route, result.Confidence)
	if judgeNote != "" {
		result.Reasoning += " judge=" + judgeNote
	}
	return result, nil
}

func isTalkToStaffChoice(id string, ctx codedIntentContext) bool {
	if id == "talk_to_agent" {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(ctx.ChoiceIDs[id]))
	return strings.Contains(title, "talk to") || strings.Contains(title, "staff") || strings.Contains(title, "agent")
}

func choiceConfidenceUsable(ans jevChoiceAnswer) bool {
	if len(ans.Probabilities) == 0 && ans.Confidence == 0 && ans.Choice != "" {
		// Language-model fallback sentinel: confidence unavailable.
		return false
	}
	return ans.Choice != ""
}

func parseJevChoice(raw json.RawMessage) (jevChoiceAnswer, bool) {
	if len(raw) == 0 {
		return jevChoiceAnswer{}, false
	}
	var ans jevChoiceAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return jevChoiceAnswer{}, false
	}
	ans.Choice = strings.TrimSpace(ans.Choice)
	return ans, ans.Choice != "" || ans.Type == "choice"
}

func parseJevNoul(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var ans jevNoulAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return 0, false
	}
	return ans.Noul, true
}

var codedNumberWords = map[string]string{
	"zero": "0", "one": "1", "two": "2", "three": "3", "four": "4",
	"five": "5", "six": "6", "seven": "7", "eight": "8", "nine": "9",
	"ten": "10", "eleven": "11", "twelve": "12", "thirteen": "13", "fourteen": "14",
	"fifteen": "15", "sixteen": "16", "seventeen": "17", "eighteen": "18", "nineteen": "19",
	"twenty": "20",
}

func normalizeCodedAnswer(message, pattern string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	if matchCodedPattern(pattern, message) {
		return message
	}
	lower := strings.ToLower(message)
	for word, digits := range codedNumberWords {
		if lower == word || strings.Contains(lower, word) {
			if matchCodedPattern(pattern, digits) {
				return digits
			}
		}
	}
	var b strings.Builder
	for _, r := range message {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if digits != "" && matchCodedPattern(pattern, digits) {
		return digits
	}
	return message
}
