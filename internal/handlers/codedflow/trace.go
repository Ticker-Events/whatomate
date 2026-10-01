package codedflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	CodedFlowTraceComponent = "coded_flow"
	codedFlowTraceMaxRunes  = 8000
)

// ParseTruthyEnv parses an env-style truthy string. Empty uses defaultWhenEmpty.
func ParseTruthyEnv(raw string, defaultWhenEmpty bool) bool {
	return parseTruthyEnv(raw, defaultWhenEmpty)
}

func parseTruthyEnv(raw string, defaultWhenEmpty bool) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultWhenEmpty
	}
	switch strings.ToLower(raw) {
	case "0", "false", "no", "off", "disable", "disabled":
		return false
	default:
		return true
	}
}

// CodedFlowTraceEnv reports WHATOMATE_CODEDFLOW_TRACE (default enabled when unset).
func CodedFlowTraceEnv() (enabled bool, set bool) {
	v, ok := os.LookupEnv("WHATOMATE_CODEDFLOW_TRACE")
	if !ok {
		return true, false
	}
	return parseTruthyEnv(v, true), true
}

func MustJSON(v any) string { return mustJSON(v) }

func mustJSON(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func TruncateTrace(s string) string { return truncateTrace(s) }

func truncateTrace(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= codedFlowTraceMaxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:codedFlowTraceMaxRunes]) + "…[truncated]"
}

// FormatHTTPCurl builds a curl string for trace logs.
func FormatHTTPCurl(method, rawURL string, headers map[string]string, body string) string {
	return formatHTTPCurl(method, rawURL, headers, body)
}

func formatHTTPCurl(method, rawURL string, headers map[string]string, body string) string {
	var b strings.Builder
	b.WriteString("curl -sS -X ")
	b.WriteString(strings.ToUpper(strings.TrimSpace(method)))
	b.WriteString(" ")
	b.WriteString(shellQuote(rawURL))
	for key, value := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		b.WriteString(" \\\n  -H ")
		b.WriteString(shellQuote(key + ": " + value))
	}
	if body != "" {
		b.WriteString(" \\\n  --data-raw ")
		b.WriteString(shellQuote(body))
	}
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
