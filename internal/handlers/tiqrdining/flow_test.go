package tiqrdining

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlowRegistered(t *testing.T) {
	flow := codedflow.ByKey(FlowKey)
	require.NotNil(t, flow)
	assert.Equal(t, "TiQR Dining", flow.Name)
	assert.Equal(t, tiqrDiningKey, flow.Key)
}

func TestBookingConfirmationSeated(t *testing.T) {
	msg := bookingConfirmationMessage(map[string]any{
		"status":       "seated",
		"guest_name":   "Asha",
		"token_number": 3,
		"tables": []any{
			map[string]any{"title": "Window"},
		},
	})
	assert.Contains(t, msg, "Asha")
	assert.Contains(t, msg, "Window")
	assert.Contains(t, msg, "#3")
}

func TestBookingConfirmationWaiting(t *testing.T) {
	msg := bookingConfirmationMessage(map[string]any{
		"status":               "waiting",
		"guest_name":           "Ravi",
		"token_number":         7,
		"average_wait_minutes": 15,
		"estimated_seat_at":    "2026-11-01T14:30:00Z",
	})
	assert.Contains(t, msg, "Ravi")
	assert.Contains(t, msg, "#7")
	assert.Contains(t, msg, "15 minutes")
}

func TestLooksLikeDiningCommand(t *testing.T) {
	assert.True(t, looksLikeDiningCommand("cancel my booking"))
	assert.True(t, looksLikeDiningCommand("Talk to staff"))
	assert.False(t, looksLikeDiningCommand("Aswin"))
}

func TestCancelConfirmationMessage(t *testing.T) {
	assert.Contains(t, cancelConfirmationMessage(map[string]any{"token_number": 2}), "#2")
}
