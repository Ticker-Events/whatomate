package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	typeSafeSystemOneURL  = "https://api.typesafe.ai/v1/systemone"
	aiGatewaySystemOneURL = "https://ai-gateway.vercel.sh/typesafe/v1/systemone"

	jevJudgeYes = 0.8
	jevJudgeNo  = 0.2
	jevMaxSpans = 200
)

type jevEndpoint struct {
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

type jevSystemOneResponse struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
}

func codedIntentProvider(settings *models.ChatbotSettings) models.IntentProvider {
	return codedFlowRoleProvider(settings, codedFlowRoleIntent)
}

func jevEndpointFor(settings *models.ChatbotSettings) (jevEndpoint, error) {
	if settings == nil {
		return jevEndpoint{}, fmt.Errorf("ai is not configured")
	}
	switch codedIntentProvider(settings) {
	case models.IntentProviderJev:
		key := strings.TrimSpace(settings.AI.TypeSafeAPIKey)
		if key == "" {
			return jevEndpoint{}, fmt.Errorf("typesafe api key is not configured")
		}
		return jevEndpoint{URL: typeSafeSystemOneURL, Key: key, Model: models.IntentTypeSafeModel}, nil
	case models.IntentProviderGateway:
		key := strings.TrimSpace(settings.AI.GatewayAPIKey)
		if key == "" {
			return jevEndpoint{}, fmt.Errorf("ai gateway api key is not configured")
		}
		model := strings.TrimSpace(settings.AI.GatewayModel)
		if model == "" {
			model = models.IntentGatewayModelJev
		}
		if !models.ValidIntentGatewayModel(model) {
			return jevEndpoint{}, fmt.Errorf("invalid ai gateway model: %s", model)
		}
		return jevEndpoint{URL: aiGatewaySystemOneURL, Key: key, Model: model}, nil
	default:
		return jevEndpoint{}, fmt.Errorf("intent provider is not jev or gateway")
	}
}

func identifyCodedIntentJev(a *App, settings *models.ChatbotSettings, message string, ctx codedIntentContext) (codedIntentResult, error) {
	endpoint, err := jevEndpointFor(settings)
	if err != nil {
		return codedIntentResult{}, err
	}
	state, questions := buildJevIntentRequest(message, ctx)
	raw, err := a.callSystemOne(endpoint, state, questions)
	if err != nil {
		return codedIntentResult{}, err
	}
	return composeJevIntent(raw, message, ctx)
}

func buildJevIntentRequest(message string, ctx codedIntentContext) (map[string]any, map[string]any) {
	choices := make([]map[string]string, 0, len(ctx.ChoiceIDs))
	for id, title := range ctx.ChoiceIDs {
		choices = append(choices, map[string]string{"id": id, "title": title})
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i]["id"] < choices[j]["id"] })

	collections := make([]map[string]string, 0)
	if ctx.AllowCatalog {
		for id, name := range ctx.Collections {
			collections = append(collections, map[string]string{"id": id, "name": name})
		}
		sort.Slice(collections, func(i, j int) bool { return collections[i]["id"] < collections[j]["id"] })
	}

	state := map[string]any{
		"message":     message,
		"question":    ctx.Question,
		"doing":       ctx.Doing,
		"expect":      ctx.Expect,
		"pattern":     ctx.Pattern,
		"choices":     choices,
		"collections": collections,
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
	if ctx.AllowCatalog {
		routeCriteria["collection"] = map[string]any{
			"what":    "They want the items in one loaded collection, or ask what is available in that collection.",
			"not_for": "A single product name to search.",
		}
		routeCriteria["product"] = map[string]any{
			"what":    "They name a product or ask to buy a specific item.",
			"not_for": "The Buy products button, or browsing a whole collection.",
		}
	}
	if strings.TrimSpace(ctx.Pattern) != "" {
		routeCriteria["answer"] = map[string]any{
			"what":    "Their reply is a valid answer to the current question but is not one of the choice ids.",
			"not_for": "A button id, collection name, or product name.",
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
			"criteria": map[string]any{
				"en":       "English",
				"hi":       "Hindi",
				"ml":       "Malayalam",
				"ta":       "Tamil",
				"te":       "Telugu",
				"kn":       "Kannada",
				"ar":       "Arabic",
				"es":       "Spanish",
				"manglish": "Malayalam written in Latin letters, or mixed Malayalam and English",
				"other":    "Any other language or mixed form",
			},
		},
	}

	if len(ctx.ChoiceIDs) > 0 {
		choiceCriteria := map[string]any{
			"none": "None of the shown choices match.",
		}
		for id, title := range ctx.ChoiceIDs {
			choiceCriteria[id] = map[string]any{
				"what":  title,
				"title": title,
			}
		}
		questions["choice_id"] = map[string]any{
			"type": "choice",
			"instructions": map[string]any{
				"question": "Which shown choice id matches `message`?",
				"choices":  "`choices`",
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

	if ctx.AllowCatalog && len(ctx.Collections) > 0 {
		collectionCriteria := map[string]any{
			"none": "None of the loaded collections match.",
		}
		for id, name := range ctx.Collections {
			collectionCriteria[id] = map[string]any{
				"what": name,
				"name": name,
			}
		}
		questions["collection_id"] = map[string]any{
			"type": "choice",
			"instructions": map[string]any{
				"question":    "Which loaded collection id matches `message`?",
				"collections": "`collections`",
			},
			"criteria": collectionCriteria,
		}
		questions["names_collection"] = map[string]any{
			"type": "noul",
			"instructions": map[string]any{
				"question":    "Does `message` refer to one loaded collection as a group to browse?",
				"collections": "`collections`",
			},
			"criteria": map[string]any{
				"true":  "They want that collection's items as a group.",
				"false": "They are not asking for a whole collection.",
			},
		}
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

	if ctx.AllowCatalog {
		spanCriteria := map[string]any{"none": "No product search span is present."}
		for _, span := range productSpanCandidates(message) {
			spanCriteria[span] = span
		}
		questions["product_span"] = map[string]any{
			"type": "choice",
			"instructions": map[string]any{
				"question": "Which span from `message` is the product search string?",
			},
			"criteria": spanCriteria,
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

func productSpanCandidates(message string) []string {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	words := strings.Fields(message)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(words)*2+1)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		if len(out) >= jevMaxSpans {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(message)
	for i := range words {
		add(words[i])
		if i+1 < len(words) {
			add(words[i] + " " + words[i+1])
		}
	}
	return out
}

func (a *App) callSystemOne(endpoint jevEndpoint, state map[string]any, questions map[string]any) (jevSystemOneResponse, error) {
	payload, err := json.Marshal(map[string]any{
		"model":     endpoint.Model,
		"state":     state,
		"questions": questions,
	})
	if err != nil {
		return jevSystemOneResponse{}, fmt.Errorf("failed to marshal typesafe payload: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		req, err := http.NewRequest(http.MethodPost, endpoint.URL, bytes.NewReader(payload))
		if err != nil {
			return jevSystemOneResponse{}, fmt.Errorf("failed to create typesafe request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+endpoint.Key)

		client := a.HTTPClient
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("typesafe request failed: %w", err)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("failed to read typesafe response: %w", readErr)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 {
			lastErr = fmt.Errorf("typesafe temporary error: %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return jevSystemOneResponse{}, fmt.Errorf("typesafe error %d: %s", resp.StatusCode, truncateRunes(string(body), 200))
		}
		var parsed jevSystemOneResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return jevSystemOneResponse{}, fmt.Errorf("failed to parse typesafe response: %w", err)
		}
		return parsed, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("typesafe request failed")
	}
	return jevSystemOneResponse{}, lastErr
}

func composeJevIntent(raw jevSystemOneResponse, message string, ctx codedIntentContext) (codedIntentResult, error) {
	routeAns, routeOK := parseJevChoice(raw.Answers["route"])
	langAns, _ := parseJevChoice(raw.Answers["language"])
	choiceAns, _ := parseJevChoice(raw.Answers["choice_id"])
	collectionAns, _ := parseJevChoice(raw.Answers["collection_id"])
	spanAns, _ := parseJevChoice(raw.Answers["product_span"])
	namesCollection, hasNamesCollection := parseJevNoul(raw.Answers["names_collection"])
	namesProduct, hasNamesProduct := parseJevNoul(raw.Answers["names_product"])
	asksPerson, hasAsksPerson := parseJevNoul(raw.Answers["asks_for_person"])
	statesQty, hasStatesQty := parseJevNoul(raw.Answers["states_quantity"])

	result := codedIntentResult{
		Language: "en",
		Route:    codedRouteUnclear,
	}
	if langAns.Choice != "" {
		result.Language = langAns.Choice
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
		query := strings.TrimSpace(spanAns.Choice)
		if query == "" || query == "none" || !choiceConfidenceUsable(spanAns) {
			query = strings.TrimSpace(message)
		}
		result.ProductQuery = query
	case codedRouteAnswer:
		result.Answer = normalizeCodedAnswer(message, ctx.Pattern)
	}

	result.Reasoning = fmt.Sprintf("route=%s confidence=%.2f", result.Route, result.Confidence)
	if judgeNote != "" {
		result.Reasoning += " judge=" + judgeNote
	}
	return result, nil
}

func isTalkToStaffChoice(id string, ctx codedIntentContext) bool {
	if id == tiqrTalkToAgent {
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
