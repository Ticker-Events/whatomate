package codedflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAsString_CoercesJSONNumberTypes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "70", AsString(float64(70)))
	assert.Equal(t, "70", AsString(70))
	assert.Equal(t, "70", AsString(int64(70)))
	assert.Equal(t, "70", AsString(json.Number("70")))
	assert.Equal(t, "70", AsString("70"))
	assert.Equal(t, "after_capture", AsString("after_capture"))
	assert.Equal(t, "", AsString(nil))
	assert.Equal(t, "70", FieldString(map[string]any{"id": float64(70)}, "id"))
	assert.Equal(t, "3319", FieldString(map[string]any{"id": float64(3319)}, "id"))
	assert.Equal(t, "1.5", AsString(float64(1.5)))
}

func TestFieldString_MatchesNumericIDsLikeGraphRunner(t *testing.T) {
	t.Parallel()

	collections := []any{
		map[string]any{"id": float64(70), "handoff_policy": "after_capture"},
		map[string]any{"id": float64(71), "handoff_policy": "none"},
	}
	var found map[string]any
	for _, entry := range collections {
		item := entry.(map[string]any)
		if FieldString(item, "id") == "70" {
			found = item
			break
		}
	}
	assert.NotNil(t, found)
	assert.Equal(t, "after_capture", AsString(found["handoff_policy"]))
}
