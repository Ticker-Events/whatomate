package tiqrecommerce

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCartButtonTemplate_AddedKeepsQuestion(t *testing.T) {
	tmpl := cartButtonCombinedTemplate(cartButtonAdded)
	assert.Contains(t, tmpl, "has been added to your cart")
	assert.Contains(t, tmpl, "{{cart_lines}}")
	assert.Contains(t, tmpl, "{{cart_subtotal}}")
	assert.Contains(t, tmpl, "{{cart_count}}")
	assert.Contains(t, tmpl, tiqrCartQuestion)
	assert.NotContains(t, tmpl, "check out when you're ready")
	assert.NotContains(t, tmpl, "more items.")
}

func TestCartButtonTemplate_NextOmitsAddedLine(t *testing.T) {
	tmpl := cartButtonCombinedTemplate(cartButtonNext)
	assert.NotContains(t, tmpl, "has been added")
	assert.NotContains(t, tmpl, "You now have")
	assert.NotContains(t, tmpl, "more items.")
	assert.Contains(t, tmpl, tiqrCartQuestion)
}

func TestCartButtonDetails_HaveNoQuestion(t *testing.T) {
	assert.NotContains(t, cartButtonDetailsTemplate(cartButtonAdded), tiqrCartQuestion)
	assert.Equal(t, tiqrCartQuestion, cartButtonQuestion(cartButtonNext))
	assert.Equal(t, tiqrCartReviewSuffix, cartButtonQuestion(cartButtonReview))
}

func TestSplitTextChunks_KeepsLinesWhole(t *testing.T) {
	lines := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		lines = append(lines, strings.Repeat("A", 300))
	}
	text := strings.Join(lines, "\n")
	chunks := splitTextChunks(text, 1000)
	require.Greater(t, len(chunks), 1)
	joined := strings.Join(chunks, "\n")
	assert.Equal(t, text, joined)
	for _, chunk := range chunks {
		assert.LessOrEqual(t, utf8.RuneCountInString(chunk), 1000)
		assert.NotContains(t, chunk, "…and")
	}
}

func TestFormatCartLinesForButton_KeepsCapture(t *testing.T) {
	lines := []TiqrCartLine{
		{
			OptionID: "1", Name: "Cake", Qty: 1, Price: 10,
			Capture: map[string]any{"note": "Happy birthday"},
			Labels:  map[string]string{"note": "Message"},
		},
	}
	got := formatCartLinesForButton(lines, "INR", true, 0)
	assert.Contains(t, got, "Cake")
	assert.Contains(t, got, "Message: Happy birthday")
}
