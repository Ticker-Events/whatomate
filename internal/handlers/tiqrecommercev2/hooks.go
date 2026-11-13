package tiqrecommercev2

import (
	"strings"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
)

// Hooks connects this package to handlers DB lookups without an import cycle.
type Hooks struct {
	BindingSettings func(orgID uuid.UUID, accountName, flowKey string) models.JSONB
}

var hooks Hooks

// Configure sets handler-side hooks. Safe to call from handlers init.
func Configure(h Hooks) {
	hooks = h
}

func bindingSettings(orgID uuid.UUID, accountName string) models.JSONB {
	if hooks.BindingSettings == nil {
		return nil
	}
	return hooks.BindingSettings(orgID, accountName, FlowKey)
}

func metaFlowIDFromBinding(orgID uuid.UUID, accountName string, delivery bool) string {
	settings := bindingSettings(orgID, accountName)
	if settings == nil {
		return ""
	}
	key := "pickup_flow_id"
	if delivery {
		key = "delivery_flow_id"
	}
	id, _ := settings[key].(string)
	return strings.TrimSpace(id)
}
