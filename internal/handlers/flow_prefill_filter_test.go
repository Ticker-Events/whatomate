package handlers

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestFilterFlowActionData_IntersectsSchema(t *testing.T) {
	t.Parallel()
	flowJSON := models.JSONB{
		"screens": []any{
			map[string]any{
				"id": "DETAILS",
				"data": map[string]any{
					"customer_phone": map[string]any{"type": "string", "__example__": "91"},
					"city":           map[string]any{"type": "string", "__example__": "City"},
				},
			},
		},
	}
	got := filterFlowActionData(flowJSON, nil, "DETAILS", map[string]string{
		"customer_phone": "910000000000",
		"city":           "Bengaluru",
		"pincode":        "560001",
	})
	assert.Equal(t, map[string]string{
		"customer_phone": "910000000000",
		"city":           "Bengaluru",
	}, got)
}

func TestFilterFlowActionData_NoSchemaOmitsData(t *testing.T) {
	t.Parallel()
	got := filterFlowActionData(nil, nil, "DETAILS", map[string]string{
		"customer_phone": "910000000000",
	})
	assert.Nil(t, got)
}
