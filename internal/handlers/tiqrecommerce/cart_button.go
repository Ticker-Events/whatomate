package tiqrecommerce

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	tiqrCartQuestion = "Would you like to add anything else, edit your cart, or place your order?"

	tiqrCartReviewSuffix = "Please confirm these items before we continue checkout.\n\nYou can edit quantities or remove items first."

	tiqrCartMoreOne = "…and 1 more item."
	tiqrCartMoreN   = "…and {{hidden_count}} more items."
)

type cartButtonKind int

const (
	cartButtonAdded cartButtonKind = iota
	cartButtonNext
	cartButtonReview
)

var cartNextButtons = []codedflow.Button{
	{ID: tiqrAddMore, Title: "Add more items"},
	{ID: tiqrEditCart, Title: "Edit cart"},
	{ID: tiqrCheckout, Title: "Checkout"},
}

var cartReviewButtons = []codedflow.Button{
	{ID: tiqrConfirmItems, Title: "Confirm items"},
	{ID: tiqrEditCart, Title: "Edit cart"},
	{ID: tiqrAddMore, Title: "Add more"},
}

func cartButtonTitles(buttons []codedflow.Button) []string {
	out := make([]string, 0, len(buttons))
	for _, b := range buttons {
		out = append(out, b.Title)
	}
	return out
}

func cartSummaryTemplate(hidden int) string {
	var b strings.Builder
	b.WriteString("*Your cart:*\n{{cart_lines}}\n\n")
	if hidden == 1 {
		b.WriteString(tiqrCartMoreOne)
		b.WriteString("\n\n")
	} else if hidden > 1 {
		b.WriteString(tiqrCartMoreN)
		b.WriteString("\n\n")
	}
	b.WriteString("*Subtotal:* {{cart_subtotal}}")
	return b.String()
}

func cartButtonTemplate(kind cartButtonKind, hidden int) string {
	summary := cartSummaryTemplate(hidden)
	switch kind {
	case cartButtonAdded:
		return "*{{option_name}}* has been added to your cart.\n\n" +
			summary +
			"\n\nYou now have *{{cart_count}}* items in your cart.\n\n" +
			tiqrCartQuestion
	case cartButtonNext:
		return summary + "\n\n" + tiqrCartQuestion
	default:
		return summary + "\n\n" + tiqrCartReviewSuffix
	}
}

// prepareCartButtonBody refreshes cart session fields, fits cart_lines into the
// WhatsApp interactive body budget (leaving room for the option legend), and
// returns the authored template for AskButtons.
func prepareCartButtonBody(c *Conv, kind cartButtonKind, buttonTitles []string) string {
	refreshCartSessionFields(c)
	lines := tiqrCartLines(c)
	if len(lines) == 0 {
		return cartButtonTemplate(kind, 0)
	}
	currency := sessionCurrencyCode(c.Session())
	budget := codedflow.InteractiveBodyMaxRunes
	if c != nil && c.Conv != nil {
		if legend := c.OptionLegendSuffix(buttonTitles); legend != "" {
			budget -= utf8.RuneCountInString("\n\n" + legend)
		}
	}
	if budget < 64 {
		budget = 64
	}

	includeCapture := true
	start := 0
	nameMax := 0
	var lastTmpl string
	for {
		hidden := start
		visible := lines[start:]
		rendered := formatCartLinesForButton(visible, currency, includeCapture, nameMax)
		if c != nil && c.Session() != nil {
			if c.Session().SessionData == nil {
				c.Session().SessionData = models.JSONB{}
			}
			c.Session().SessionData["cart_lines"] = rendered
			if hidden > 0 {
				c.Session().SessionData["hidden_count"] = strconv.Itoa(hidden)
			} else {
				delete(c.Session().SessionData, "hidden_count")
			}
		}
		tmpl := cartButtonTemplate(kind, hidden)
		lastTmpl = tmpl
		if cartButtonBodyRunes(c, tmpl) <= budget {
			return tmpl
		}
		if includeCapture && cartLinesHaveCapture(visible) {
			includeCapture = false
			continue
		}
		if start < len(lines)-1 {
			start++
			includeCapture = false
			continue
		}
		if nameMax == 0 {
			nameMax = 48
			continue
		}
		if nameMax > 8 {
			nameMax = nameMax * 3 / 4
			if nameMax < 8 {
				nameMax = 8
			}
			continue
		}
		return lastTmpl
	}
}

func cartButtonBodyRunes(c *Conv, tmpl string) int {
	body := tmpl
	if c != nil && c.Conv != nil {
		body = c.AuthoredText(tmpl)
	}
	return utf8.RuneCountInString(fillCartButtonPlaceholders(body, cartButtonPlaceholderData(c)))
}

func cartButtonPlaceholderData(c *Conv) map[string]string {
	out := map[string]string{
		"cart_lines":    "",
		"cart_subtotal": "",
		"cart_count":    "",
		"option_name":   "",
		"hidden_count":  "",
	}
	if c == nil || c.Session() == nil || c.Session().SessionData == nil {
		return out
	}
	data := c.Session().SessionData
	out["cart_lines"] = asString(data["cart_lines"])
	out["cart_subtotal"] = asString(data["cart_subtotal"])
	out["cart_count"] = formatCartCount(data["cart_count"])
	out["option_name"] = asString(data["option_name"])
	out["hidden_count"] = asString(data["hidden_count"])
	return out
}

func formatCartCount(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case int:
		return strconv.Itoa(n)
	case int64:
		return strconv.FormatInt(n, 10)
	case string:
		return n
	default:
		return asString(v)
	}
}

func fillCartButtonPlaceholders(tmpl string, data map[string]string) string {
	out := tmpl
	for key, value := range data {
		out = strings.ReplaceAll(out, "{{"+key+"}}", value)
	}
	return out
}

func cartLinesHaveCapture(lines []TiqrCartLine) bool {
	for _, line := range lines {
		if formatLineCapture(line) != "" {
			return true
		}
	}
	return false
}

func formatCartLinesForButton(lines []TiqrCartLine, currency string, includeCapture bool, nameMax int) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for i, line := range lines {
		qty := line.Qty
		if qty < 1 {
			qty = 1
		}
		lineTotal := line.Price * float64(qty)
		name := tiqrCartLineDisplayName(line)
		if nameMax > 0 {
			name = truncateCartLineName(name, nameMax)
		}
		fmt.Fprintf(&b, "%d. *%s* x%d — %s\n", i+1, name, qty, formatMoney(lineTotal, currency))
		if includeCapture {
			if extra := formatLineCapture(line); extra != "" {
				b.WriteString(extra)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func truncateCartLineName(name string, max int) string {
	name = strings.TrimSpace(name)
	if max <= 0 {
		return name
	}
	r := []rune(name)
	if len(r) <= max {
		return name
	}
	if max == 1 {
		return string(r[:1])
	}
	return string(r[:max-1]) + "…"
}

// FitCartLinesForButton is exported for unit tests of the shrinking rules.
// fixedOverhead is the rune count of the button body outside {{cart_lines}}
// and the optional “…and N more items.” line.
func FitCartLinesForButton(lines []TiqrCartLine, currency string, fixedOverhead int, maxRunes int) (cartLines string, hidden int, includeCapture bool) {
	if maxRunes < 1 {
		maxRunes = codedflow.InteractiveBodyMaxRunes
	}
	includeCapture = true
	start := 0
	nameMax := 0
	var lastLines string
	var lastHidden int
	var lastCapture bool
	for {
		hidden = start
		visible := lines[start:]
		rendered := formatCartLinesForButton(visible, currency, includeCapture, nameMax)
		more := ""
		if hidden == 1 {
			more = "\n\n" + tiqrCartMoreOne
		} else if hidden > 1 {
			more = "\n\n" + strings.ReplaceAll(tiqrCartMoreN, "{{hidden_count}}", strconv.Itoa(hidden))
		}
		total := fixedOverhead + utf8.RuneCountInString(rendered) + utf8.RuneCountInString(more)
		lastLines = rendered
		lastHidden = hidden
		lastCapture = includeCapture
		if total <= maxRunes {
			return rendered, hidden, includeCapture
		}
		if includeCapture && cartLinesHaveCapture(visible) {
			includeCapture = false
			continue
		}
		if start < len(lines)-1 {
			start++
			includeCapture = false
			continue
		}
		if nameMax == 0 {
			nameMax = 48
			continue
		}
		if nameMax > 8 {
			nameMax = nameMax * 3 / 4
			if nameMax < 8 {
				nameMax = 8
			}
			continue
		}
		return lastLines, lastHidden, lastCapture
	}
}
