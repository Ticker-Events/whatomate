package codedflow

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shridarpatil/whatomate/internal/models"
)

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// AsString exports asString for sibling packages.
func AsString(v any) string { return asString(v) }

func asStringMap(item any) (map[string]any, bool) {
	switch v := item.(type) {
	case map[string]any:
		return v, true
	case models.JSONB:
		return map[string]any(v), true
	default:
		return nil, false
	}
}

// AsStringMap exports asStringMap for sibling packages.
func AsStringMap(item any) (map[string]any, bool) { return asStringMap(item) }

func anySlice(raw any) ([]any, bool) {
	switch v := raw.(type) {
	case []any:
		return v, true
	case models.JSONBArray:
		return []any(v), true
	case []map[string]any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out, true
	default:
		return nil, false
	}
}

// AnySlice exports anySlice for sibling packages.
func AnySlice(raw any) ([]any, bool) { return anySlice(raw) }

func fieldString(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	return asString(obj[key])
}

// FieldString exports fieldString for sibling packages.
func FieldString(obj map[string]any, key string) string { return fieldString(obj, key) }

func anyToFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// AnyToFloat64 exports anyToFloat64 for sibling packages.
func AnyToFloat64(v any) (float64, bool) { return anyToFloat64(v) }

func anyToInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0
		}
		return int(i)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0
		}
		return i
	default:
		return 0
	}
}

// AnyToInt exports anyToInt for sibling packages.
func AnyToInt(v any) int { return anyToInt(v) }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// FirstNonEmpty exports firstNonEmpty for sibling packages.
func FirstNonEmpty(values ...string) string { return firstNonEmpty(values...) }

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

// TruncateRunes exports truncateRunes for sibling packages.
func TruncateRunes(s string, max int) string { return truncateRunes(s, max) }

func stringFromConfig(cfg map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := cfg[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
