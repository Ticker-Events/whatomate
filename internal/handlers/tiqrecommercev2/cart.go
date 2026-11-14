package tiqrecommercev2

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CartLine is one session-cart row: product option plus capture answers.
type CartLine struct {
	LineID        string            `json:"line_id"`
	ProductOption string            `json:"product_option"`
	Quantity      int               `json:"quantity"`
	ProductName   string            `json:"product_name,omitempty"`
	OptionName    string            `json:"option_name,omitempty"`
	CaptureFields map[string]string `json:"capture_fields,omitempty"`
	CaptureLabels map[string]string `json:"capture_labels,omitempty"`
	CaptureOrder  []string          `json:"capture_order,omitempty"`
}

// LineIDFor builds a stable id from option + capture answers so two cakes with
// different messages stay separate lines.
func LineIDFor(optionID string, capture map[string]string) string {
	optionID = strings.TrimSpace(optionID)
	fp := captureFingerprint(capture)
	sum := sha256.Sum256([]byte(optionID + "|" + fp))
	return "line_" + hex.EncodeToString(sum[:8])
}

func captureFingerprint(capture map[string]string) string {
	if len(capture) == 0 {
		return ""
	}
	keys := make([]string, 0, len(capture))
	for k := range capture {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strings.TrimSpace(capture[k]))
	}
	return strings.Join(parts, "&")
}

func cartLinesFromSession(raw any) []CartLine {
	items, ok := anySlice(raw)
	if !ok {
		return nil
	}
	out := make([]CartLine, 0, len(items))
	for _, entry := range items {
		m, ok := asStringMap(entry)
		if !ok {
			continue
		}
		line := cartLineFromMap(m)
		if line.ProductOption == "" {
			continue
		}
		if line.LineID == "" {
			line.LineID = LineIDFor(line.ProductOption, line.CaptureFields)
		}
		out = append(out, line)
	}
	return out
}

func cartLineFromMap(m map[string]any) CartLine {
	line := CartLine{
		LineID:        strings.TrimSpace(asString(m["line_id"])),
		ProductOption: strings.TrimSpace(asString(m["product_option"])),
		ProductName:   strings.TrimSpace(asString(m["product_name"])),
		OptionName:    strings.TrimSpace(asString(m["option_name"])),
		Quantity:      anyToInt(m["quantity"]),
	}
	if line.Quantity < 1 {
		line.Quantity = 1
	}
	if caps, ok := asStringMap(m["capture_fields"]); ok {
		line.CaptureFields = stringMapValues(caps)
	}
	if labels, ok := asStringMap(m["capture_labels"]); ok {
		line.CaptureLabels = stringMapValues(labels)
	}
	if order, ok := anySlice(m["capture_order"]); ok {
		for _, v := range order {
			if s := strings.TrimSpace(asString(v)); s != "" {
				line.CaptureOrder = append(line.CaptureOrder, s)
			}
		}
	}
	return line
}

func cartLinesToSession(lines []CartLine) []any {
	out := make([]any, 0, len(lines))
	for _, line := range lines {
		item := map[string]any{
			"line_id":        line.LineID,
			"product_option": line.ProductOption,
			"quantity":       strconv.Itoa(line.Quantity),
		}
		if line.ProductName != "" {
			item["product_name"] = line.ProductName
		}
		if line.OptionName != "" {
			item["option_name"] = line.OptionName
		}
		if len(line.CaptureFields) > 0 {
			caps := make(map[string]any, len(line.CaptureFields))
			for k, v := range line.CaptureFields {
				caps[k] = v
			}
			item["capture_fields"] = caps
		}
		if len(line.CaptureLabels) > 0 {
			labs := make(map[string]any, len(line.CaptureLabels))
			for k, v := range line.CaptureLabels {
				labs[k] = v
			}
			item["capture_labels"] = labs
		}
		if len(line.CaptureOrder) > 0 {
			order := make([]any, len(line.CaptureOrder))
			for i, k := range line.CaptureOrder {
				order[i] = k
			}
			item["capture_order"] = order
		}
		out = append(out, item)
	}
	return out
}

func updateCartLine(lines []CartLine, lineID string, qty int) ([]CartLine, bool) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" || qty < 1 {
		return lines, false
	}
	for i := range lines {
		if lines[i].LineID != lineID {
			continue
		}
		lines[i].Quantity = qty
		return lines, true
	}
	return lines, false
}

func removeCartLine(lines []CartLine, lineID string) ([]CartLine, string, bool) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return lines, "", false
	}
	next := make([]CartLine, 0, len(lines))
	name := ""
	found := false
	for _, line := range lines {
		if line.LineID == lineID {
			found = true
			name = line.OptionName
			if name == "" {
				name = line.ProductName
			}
			if name == "" {
				name = line.LineID
			}
			continue
		}
		next = append(next, line)
	}
	return next, name, found
}

func findCartLine(lines []CartLine, lineID string) (CartLine, bool) {
	lineID = strings.TrimSpace(lineID)
	for _, line := range lines {
		if line.LineID == lineID {
			return line, true
		}
	}
	return CartLine{}, false
}

func formatCartSummary(lines []CartLine) string {
	if len(lines) == 0 {
		return "Your cart is empty."
	}
	var b strings.Builder
	b.WriteString("*Your cart:*\n")
	for i, line := range lines {
		name := line.OptionName
		if name == "" {
			name = line.ProductName
		}
		if name == "" {
			name = "Item"
		}
		fmt.Fprintf(&b, "%d. %s x%d (%s)\n", i+1, name, line.Quantity, line.LineID)
		for _, key := range line.CaptureOrder {
			label := line.CaptureLabels[key]
			if label == "" {
				label = key
			}
			fmt.Fprintf(&b, "   - %s: %s\n", label, line.CaptureFields[key])
		}
		if len(line.CaptureOrder) == 0 {
			for key, val := range line.CaptureFields {
				label := line.CaptureLabels[key]
				if label == "" {
					label = key
				}
				fmt.Fprintf(&b, "   - %s: %s\n", label, val)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func stringMapValues(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = strings.TrimSpace(asString(v))
	}
	return out
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

func asStringMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func anySlice(v any) ([]any, bool) {
	switch t := v.(type) {
	case []any:
		return t, true
	case []map[string]any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = t[i]
		}
		return out, true
	default:
		return nil, false
	}
}

func anyToInt(v any) int {
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
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		return 0
	}
}
