package tiqrecommerce

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFitCartLinesForButton_DropsCaptureBeforeHidingRows(t *testing.T) {
	lines := []TiqrCartLine{
		{
			OptionID: "1", Name: "Old Cake", Qty: 1, Price: 10,
			Capture: map[string]any{"note": strings.Repeat("x", 200)},
			Labels:  map[string]string{"note": "Note"},
		},
		{
			OptionID: "2", Name: "New Cake", Qty: 1, Price: 10,
			Capture: map[string]any{"note": strings.Repeat("y", 200)},
			Labels:  map[string]string{"note": "Note"},
		},
	}
	full := formatCartLinesForButton(lines, "INR", true, 0)
	overhead := 100
	// Room for both name lines without capture, but not with capture.
	max := overhead + utf8.RuneCountInString(formatCartLinesForButton(lines, "INR", false, 0)) + 10
	require.Greater(t, overhead+utf8.RuneCountInString(full), max)

	got, hidden, includeCapture := FitCartLinesForButton(lines, "INR", overhead, max)
	assert.Equal(t, 0, hidden)
	assert.False(t, includeCapture)
	assert.Contains(t, got, "Old Cake")
	assert.Contains(t, got, "New Cake")
	assert.NotContains(t, got, "Note:")
	assert.LessOrEqual(t, overhead+utf8.RuneCountInString(got), max)
}

func TestFitCartLinesForButton_HidesOldestKeepsNewest(t *testing.T) {
	lines := make([]TiqrCartLine, 0, 8)
	for i := 0; i < 8; i++ {
		lines = append(lines, TiqrCartLine{
			OptionID: string(rune('a' + i)),
			Name:     "Item " + strings.Repeat("N", 40) + string(rune('A'+i)),
			Qty:      1,
			Price:    10,
		})
	}
	overhead := 200
	max := 450
	got, hidden, _ := FitCartLinesForButton(lines, "INR", overhead, max)
	require.Greater(t, hidden, 0)
	assert.Contains(t, got, "Item "+strings.Repeat("N", 40)+"H")
	assert.NotContains(t, got, "Item "+strings.Repeat("N", 40)+"A")
	more := "\n\n…and " + strconv.Itoa(hidden) + " more items."
	if hidden == 1 {
		more = "\n\n…and 1 more item."
	}
	assert.LessOrEqual(t, overhead+utf8.RuneCountInString(got)+utf8.RuneCountInString(more), max)
}

func TestFitCartLinesForButton_SingularMoreItem(t *testing.T) {
	lines := []TiqrCartLine{
		{OptionID: "1", Name: strings.Repeat("A", 80), Qty: 1, Price: 10},
		{OptionID: "2", Name: strings.Repeat("B", 80), Qty: 1, Price: 10},
	}
	overhead := 100
	oneLine := formatCartLinesForButton(lines[1:], "INR", true, 0)
	max := overhead + utf8.RuneCountInString(oneLine) + utf8.RuneCountInString("\n\n…and 1 more item.") + 5
	got, hidden, _ := FitCartLinesForButton(lines, "INR", overhead, max)
	assert.Equal(t, 1, hidden)
	assert.Contains(t, got, strings.Repeat("B", 80))
	assert.NotContains(t, got, strings.Repeat("A", 80))
	tmpl := cartButtonTemplate(cartButtonNext, hidden)
	assert.Contains(t, tmpl, "…and 1 more item.")
	assert.NotContains(t, tmpl, "more items.")
}

func TestCartButtonTemplate_AddedKeepsQuestion(t *testing.T) {
	tmpl := cartButtonTemplate(cartButtonAdded, 0)
	assert.Contains(t, tmpl, "has been added to your cart")
	assert.Contains(t, tmpl, "{{cart_lines}}")
	assert.Contains(t, tmpl, "{{cart_subtotal}}")
	assert.Contains(t, tmpl, "{{cart_count}}")
	assert.Contains(t, tmpl, tiqrCartQuestion)
	assert.NotContains(t, tmpl, "check out when you're ready")
}

func TestCartButtonTemplate_NextOmitsAddedLine(t *testing.T) {
	tmpl := cartButtonTemplate(cartButtonNext, 2)
	assert.NotContains(t, tmpl, "has been added")
	assert.NotContains(t, tmpl, "You now have")
	assert.Contains(t, tmpl, "…and {{hidden_count}} more items.")
	assert.Contains(t, tmpl, tiqrCartQuestion)
}
