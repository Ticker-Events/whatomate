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

	// WhatsApp plain-text body limit. Interactive button bodies are 1024.
	tiqrTextBodyMaxRunes = 4096
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

func cartSummaryTemplate() string {
	return "*Your cart:*\n{{cart_lines}}\n\n*Subtotal:* {{cart_subtotal}}"
}

func cartButtonDetailsTemplate(kind cartButtonKind) string {
	summary := cartSummaryTemplate()
	switch kind {
	case cartButtonAdded:
		return "*{{option_name}}* has been added to your cart.\n\n" +
			summary +
			"\n\nYou now have *{{cart_count}}* items in your cart."
	case cartButtonNext:
		return summary
	default:
		return summary
	}
}

func cartButtonQuestion(kind cartButtonKind) string {
	if kind == cartButtonReview {
		return tiqrCartReviewSuffix
	}
	return tiqrCartQuestion
}

func cartButtonCombinedTemplate(kind cartButtonKind) string {
	details := cartButtonDetailsTemplate(kind)
	question := cartButtonQuestion(kind)
	if details == "" {
		return question
	}
	return details + "\n\n" + question
}

// prepareCartButtonBody refreshes cart session fields and stores the full
// cart_lines. It returns the authored template for a single button message
// when the filled body fits; callers that need the split path should use
// askCartActionButtons instead.
func prepareCartButtonBody(c *Conv, kind cartButtonKind, buttonTitles []string) string {
	_ = buttonTitles
	refreshCartSessionFields(c)
	lines := tiqrCartLines(c)
	currency := sessionCurrencyCode(c.Session())
	if len(lines) > 0 {
		rendered := formatCartLinesForButton(lines, currency, true, 0)
		if c != nil && c.Session() != nil {
			if c.Session().SessionData == nil {
				c.Session().SessionData = models.JSONB{}
			}
			c.Session().SessionData["cart_lines"] = rendered
			delete(c.Session().SessionData, "hidden_count")
		}
	}
	return cartButtonCombinedTemplate(kind)
}

// askCartActionButtons sends either one button message (when the cart fits in
// 1024 characters) or the full cart as text plus a short button question.
func askCartActionButtons(
	c *Conv,
	name string,
	kind cartButtonKind,
	buttons []codedflow.Button,
	step codedflow.StepNote,
) (codedflow.Choice, bool) {
	body := prepareCartButtonBody(c, kind, cartButtonTitles(buttons))
	titles := cartButtonTitles(buttons)
	budget := interactiveBodyBudget(c, titles)
	if cartButtonBodyRunes(c, body) <= budget {
		return c.AskButtons(name, codedflow.ButtonPrompt{
			Body:    body,
			Buttons: buttons,
			Step:    step,
		})
	}
	details := cartButtonDetailsTemplate(kind)
	filled := fillCartButtonPlaceholders(authoredOrRaw(c, details), cartButtonPlaceholderData(c))
	if utf8.RuneCountInString(filled) <= tiqrTextBodyMaxRunes {
		// Template keeps placeholders so Say can translate, then processTemplate fills them.
		c.Say(details)
	} else {
		sayTextChunks(c, filled)
	}
	return c.AskButtons(name, codedflow.ButtonPrompt{
		Body:    cartButtonQuestion(kind),
		Buttons: buttons,
		Step:    step,
	})
}

func interactiveBodyBudget(c *Conv, buttonTitles []string) int {
	budget := codedflow.InteractiveBodyMaxRunes
	if c != nil && c.Conv != nil {
		if legend := c.OptionLegendSuffix(buttonTitles); legend != "" {
			budget -= utf8.RuneCountInString("\n\n" + legend)
		}
	}
	if budget < 64 {
		budget = 64
	}
	return budget
}

func authoredOrRaw(c *Conv, tmpl string) string {
	if c != nil && c.Conv != nil {
		return c.AuthoredText(tmpl)
	}
	return tmpl
}

// sayTextChunks sends one or more Say messages so no chunk exceeds the
// WhatsApp text body limit. Splits between newlines when possible.
func sayTextChunks(c *Conv, text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return
	}
	for _, chunk := range splitTextChunks(text, tiqrTextBodyMaxRunes) {
		c.Say(chunk)
	}
}

func splitTextChunks(text string, maxRunes int) []string {
	if maxRunes < 1 {
		maxRunes = tiqrTextBodyMaxRunes
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return []string{text}
	}
	lines := strings.Split(text, "\n")
	var chunks []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		chunks = append(chunks, strings.TrimRight(b.String(), "\n"))
		b.Reset()
	}
	for _, line := range lines {
		candidate := line
		if b.Len() > 0 {
			candidate = b.String() + "\n" + line
		}
		if utf8.RuneCountInString(candidate) <= maxRunes {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(line)
			continue
		}
		flush()
		if utf8.RuneCountInString(line) <= maxRunes {
			b.WriteString(line)
			continue
		}
		// A single line longer than the limit: hard-split by runes.
		runes := []rune(line)
		for len(runes) > 0 {
			n := maxRunes
			if n > len(runes) {
				n = len(runes)
			}
			chunks = append(chunks, string(runes[:n]))
			runes = runes[n:]
		}
	}
	flush()
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
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
	}
	if c == nil || c.Session() == nil || c.Session().SessionData == nil {
		return out
	}
	data := c.Session().SessionData
	out["cart_lines"] = asString(data["cart_lines"])
	out["cart_subtotal"] = asString(data["cart_subtotal"])
	out["cart_count"] = formatCartCount(data["cart_count"])
	out["option_name"] = asString(data["option_name"])
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
