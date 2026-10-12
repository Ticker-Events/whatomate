package whatsapp_test

import (
	"testing"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/stretchr/testify/assert"
)

func TestFlowActionPayload_OmitsEmptyData(t *testing.T) {
	t.Parallel()
	got := whatsapp.FlowActionPayload("DETAILS", nil)
	assert.Equal(t, map[string]any{"screen": "DETAILS"}, got)

	got = whatsapp.FlowActionPayload("DETAILS", map[string]string{})
	assert.Equal(t, map[string]any{"screen": "DETAILS"}, got)

	got = whatsapp.FlowActionPayload("DETAILS", map[string]string{"customer_phone": "  "})
	assert.Equal(t, map[string]any{"screen": "DETAILS"}, got)
}

func TestFlowActionPayload_IncludesData(t *testing.T) {
	t.Parallel()
	got := whatsapp.FlowActionPayload("DETAILS", map[string]string{
		"customer_phone": "910000000000",
		"city":           "Bengaluru",
		"empty":          "",
	})
	assert.Equal(t, "DETAILS", got["screen"])
	data, ok := got["data"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "910000000000", data["customer_phone"])
	assert.Equal(t, "Bengaluru", data["city"])
	assert.NotContains(t, data, "empty")
}
