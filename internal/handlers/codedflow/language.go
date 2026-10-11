package codedflow

import (
	"fmt"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

// Default open-detection language labels for Jev when no per-flow list is set.
var defaultJevLanguageCriteria = map[string]any{
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
}

func languagePromptInstruction(supported []string) string {
	if len(supported) == 0 {
		return "language is a short label for the language the customer is writing in. English is en. Any other language, including mixed forms such as Manglish, gets its own label. Do not translate the message."
	}
	parts := make([]string, 0, len(supported))
	for _, code := range supported {
		name := models.CodedFlowLanguageName(code)
		if name == "" {
			name = code
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", code, name))
	}
	return "language must be one of these codes only: " + strings.Join(parts, ", ") +
		". If the customer message is not clearly one of those languages, set language to en. Do not translate the message."
}

func jevLanguageCriteria(supported []string) map[string]any {
	if len(supported) == 0 {
		out := make(map[string]any, len(defaultJevLanguageCriteria))
		for k, v := range defaultJevLanguageCriteria {
			out[k] = v
		}
		return out
	}
	out := make(map[string]any, len(supported)+1)
	for _, code := range supported {
		switch code {
		case "manglish":
			out[code] = "Malayalam written in Latin letters, or mixed Malayalam and English"
		default:
			name := models.CodedFlowLanguageName(code)
			if name == "" {
				name = code
			}
			out[code] = name
		}
	}
	out["other"] = "Not one of the listed languages"
	return out
}

// normalizeDetectedLanguage maps an AI language label onto the allowed set.
// When supported is empty, empty labels become en and other values are kept.
// When supported is set, labels outside the set (including "other") become en.
func normalizeDetectedLanguage(lang string, supported []string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return "en"
	}
	if len(supported) == 0 {
		return lang
	}
	if lang == "other" {
		return "en"
	}
	for _, code := range supported {
		if strings.EqualFold(strings.TrimSpace(code), lang) {
			return strings.ToLower(strings.TrimSpace(code))
		}
	}
	return "en"
}
