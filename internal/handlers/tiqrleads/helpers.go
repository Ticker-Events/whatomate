package tiqrleads

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case float64:
		if t == math.Trunc(t) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return asString(float64(t))
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case jsonNumber:
		return string(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

type jsonNumber string

func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		n, _ := strconv.Atoi(asString(v))
		return n
	}
}

func asStringMap(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case models.JSONB:
		return map[string]any(t), true
	default:
		return nil, false
	}
}

func anySlice(v any) ([]any, bool) {
	switch t := v.(type) {
	case []any:
		return t, true
	case []map[string]any:
		out := make([]any, len(t))
		for i, row := range t {
			out[i] = row
		}
		return out, true
	default:
		return nil, false
	}
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || s == "" {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func answersFromSessionData(data models.JSONB) []any {
	if data == nil {
		return nil
	}
	rows, _ := anySlice(data[sessionAnswers])
	return rows
}

func scoreFromSessionData(data models.JSONB) (int, string) {
	answers := answersFromSessionData(data)
	total := 0.0
	for _, item := range answers {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		if asString(row["kind"]) == answerKindField {
			continue
		}
		total += float64(asInt(row["score"])) * float64(asInt(row["weight"])) / 100.0
	}
	score := int(math.Round(total))
	high := asInt(data[sessionHighMin])
	medium := asInt(data[sessionMediumMin])
	low := asInt(data[sessionLowMin])
	if high <= 0 {
		high = 75
	}
	if medium <= 0 {
		medium = 50
	}
	if low <= 0 {
		low = 25
	}
	switch {
	case score >= high:
		return score, "high"
	case score >= medium:
		return score, "medium"
	case score >= low:
		return score, "low"
	default:
		return score, "unqualified"
	}
}

func buildHandoffSummary(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return "Lead handoff"
	}
	data := session.SessionData
	score, category := scoreFromSessionData(data)
	answers := answersFromSessionData(data)
	stepCount := 0
	if snap, ok := asStringMap(data[sessionCriteria]); ok {
		if rows, ok := anySlice(snap["steps"]); ok && len(rows) > 0 {
			stepCount = len(rows)
		} else if rows, ok := anySlice(snap["criteria"]); ok {
			stepCount = len(rows)
		}
	}
	qualified := stepCount > 0 && len(answers) >= stepCount
	var b strings.Builder
	b.WriteString("Lead qualification summary\n")
	b.WriteString("Name: ")
	b.WriteString(asString(data["contact_name"]))
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("Phone: %s\n", session.PhoneNumber))
	b.WriteString(fmt.Sprintf("Score: %d (%s)\n", score, category))
	b.WriteString(fmt.Sprintf("Qualified: %t\n", qualified))
	if qualified {
		b.WriteString("Status: complete\n")
	} else {
		b.WriteString("Status: partial\n")
	}
	for _, item := range answers {
		row, ok := asStringMap(item)
		if !ok {
			continue
		}
		if asString(row["kind"]) == answerKindField {
			b.WriteString(fmt.Sprintf("- %s: %s\n",
				asString(row["criterion_name"]),
				asString(row["answer"]),
			))
			continue
		}
		b.WriteString(fmt.Sprintf("- %s: %s (score %d, weight %d)\n",
			asString(row["criterion_name"]),
			asString(row["answer"]),
			asInt(row["score"]),
			asInt(row["weight"]),
		))
	}
	return strings.TrimSpace(b.String())
}
