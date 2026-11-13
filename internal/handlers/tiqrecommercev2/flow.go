// Package tiqrecommercev2 is TiQR Ecommerce V2: an ADK agent that shops via tools.
package tiqrecommercev2

import (
	"strings"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	FlowKey = "tiqr_ecommerce_v2"

	DefaultModel = "gemini-2.5-flash"

	SettingModel = "model"

	maxToolRounds     = 8
	maxPinAttempts    = 3
	sessionStateKey   = "_v2_state"
	sessionCartKey    = "tiqr_cart"
	CartSource        = "tiqr_ecommerce_v2"

	fixedBuyProducts      = "buy_products"
	fixedCheckOrderStatus = "check_order_status"
	fixedTalkToStaff      = "talk_to_agent"
	fixedAddMore          = "add_more"
	fixedCheckout         = "checkout"
	fixedEditCart         = "edit_cart"
	fixedConfirmItems     = "confirm_items"

	modePickup   = "PICKUP_FROM_STORE"
	modeDelivery = "DELIVERY_TO_LOCATION"

	notReadyLine = "This shopping assistant is not ready yet. I'm connecting you with a team member who can help."
	retryLine    = "I need a moment — please send that again."
)

func init() {
	codedflow.Register(newFlow())
}

func newFlow() codedflow.CodedFlow {
	return codedflow.NewFlow(
		FlowKey,
		"TiQR Ecommerce V2",
		"AI-controlled shopping: browse, cart, checkout, order status, or talk to staff. "+
			"Uses Google ADK with grounded store tools. Model is configurable per account.",
		[]codedflow.CodedStep{
			{Name: "agent", Label: "AI shopping agent"},
			{Name: "tools", Label: "Catalog, cart, fulfillment, order"},
			{Name: "handoff", Label: "Staff or early handoff"},
		},
		runFlow,
	)
}

func runFlow(c *codedflow.Conv) error {
	return runAgentTurn(c)
}

// IsV2Session reports whether the WhatsApp session is inside this flow.
func IsV2Session(session *models.ChatbotSession) bool {
	return codedflow.SessionKey(session) == FlowKey
}

// ModelFromSettings returns the Gemini model id from binding settings.
func ModelFromSettings(settings models.JSONB) string {
	if settings == nil {
		return DefaultModel
	}
	if m, ok := settings[SettingModel].(string); ok {
		m = strings.TrimSpace(m)
		if m != "" {
			return m
		}
	}
	return DefaultModel
}

// NormalizeModelID accepts a Gemini model id or blank (default).
func NormalizeModelID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == '/' {
			continue
		}
		return "", errInvalidModel
	}
	if len(raw) > 120 {
		return "", errInvalidModel
	}
	return raw, nil
}

var errInvalidModel = errString("Model must be a Gemini model id (letters, digits, -, _, ., /), or blank for the default")

type errString string

func (e errString) Error() string { return string(e) }
