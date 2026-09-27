package whatsapp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListMessageInteractive(t *testing.T) {
	t.Parallel()

	longTitle := strings.Repeat("A", 30)
	longHeader := strings.Repeat("H", 70)
	rows := make([]ListRow, 0, 12)
	rows = append(rows, ListRow{ID: "", Title: "", Description: "skip"})
	for i := 0; i < 11; i++ {
		rows = append(rows, ListRow{
			ID:          "id",
			Title:       longTitle,
			Description: strings.Repeat("D", 80),
		})
	}

	interactive, err := ListMessageInteractive("  Which option?  ", ListMessageParams{
		Header:       longHeader,
		Footer:       "footer",
		ButtonText:   strings.Repeat("B", 25),
		SectionTitle: "",
		Rows:         rows,
	})
	require.NoError(t, err)

	assert.Equal(t, "list", interactive["type"])
	assert.Equal(t, "Which option?", interactive["body"].(map[string]any)["text"])
	assert.Equal(t, strings.Repeat("H", 60), interactive["header"].(map[string]any)["text"])
	assert.Equal(t, "footer", interactive["footer"].(map[string]any)["text"])

	action := interactive["action"].(map[string]any)
	assert.Equal(t, strings.Repeat("B", 20), action["button"])
	sections := action["sections"].([]map[string]any)
	require.Len(t, sections, 1)
	assert.Equal(t, "Options", sections[0]["title"])
	gotRows := sections[0]["rows"].([]map[string]any)
	require.Len(t, gotRows, 10)
	assert.Equal(t, strings.Repeat("A", 24), gotRows[0]["title"])
	assert.Equal(t, strings.Repeat("D", 72), gotRows[0]["description"])
	assert.Equal(t, "id", gotRows[0]["id"])
}

func TestListMessageInteractiveRequiresBodyAndRow(t *testing.T) {
	t.Parallel()

	_, err := ListMessageInteractive("  ", ListMessageParams{})
	require.Error(t, err)

	_, err = ListMessageInteractive("Hello", ListMessageParams{
		Rows: []ListRow{{Title: "  "}},
	})
	require.Error(t, err)
}
