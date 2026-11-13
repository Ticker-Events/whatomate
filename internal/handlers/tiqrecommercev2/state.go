package tiqrecommercev2

import (
	"encoding/json"
	"strings"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

// TurnState is persisted on the WhatsApp session between agent turns.
type TurnState struct {
	GroundedIDs       []string          `json:"grounded_ids,omitempty"`
	PinAttempts       int               `json:"pin_attempts,omitempty"`
	StoreLoadRetries  int               `json:"store_load_retries,omitempty"`
	PendingConfirm    string            `json:"pending_confirm,omitempty"` // create_order | transfer
	Transcript        []TranscriptTurn  `json:"transcript,omitempty"`
	CustomerLanguage  string            `json:"customer_language,omitempty"`
	AwaitingDetails   bool              `json:"awaiting_details,omitempty"`
	AwaitingLocation  bool              `json:"awaiting_location,omitempty"`
	OrderStatusActive bool              `json:"order_status_active,omitempty"`
	SearchLabel       string            `json:"search_label,omitempty"`
	ActiveCollection  string            `json:"active_collection,omitempty"`
	AIInstructions    string            `json:"ai_instructions,omitempty"`
	FixedActions      map[string]string `json:"fixed_actions,omitempty"`
	WaitingStep       string            `json:"waiting_step,omitempty"`
}

type TranscriptTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// WaitingStepFromSession returns the V2 waiting step stored on session state.
func WaitingStepFromSession(session *models.ChatbotSession) string {
	st := loadState(session)
	return strings.TrimSpace(st.WaitingStep)
}

func loadState(session *models.ChatbotSession) TurnState {
	st := TurnState{
		FixedActions: defaultFixedActions(),
	}
	if session == nil || session.SessionData == nil {
		return st
	}
	raw, ok := session.SessionData[sessionStateKey]
	if !ok || raw == nil {
		return st
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	if st.FixedActions == nil {
		st.FixedActions = defaultFixedActions()
	}
	return st
}

func saveState(session *models.ChatbotSession, st TurnState) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	if len(st.Transcript) > 40 {
		st.Transcript = st.Transcript[len(st.Transcript)-40:]
	}
	session.SessionData[sessionStateKey] = st
}

func defaultFixedActions() map[string]string {
	return map[string]string{
		fixedBuyProducts:      "Buy products",
		fixedCheckOrderStatus: "Check order status",
		fixedTalkToStaff:      "Talk to staff",
		fixedAddMore:          "Add more items",
		fixedCheckout:         "Checkout",
		fixedEditCart:         "Edit cart",
		fixedConfirmItems:     "Confirm items",
		fixedPickup:           "Store pickup",
		fixedDelivery:         "Delivery",
		fixedShareAgain:       "Share again",
		fixedTryAgain:         "Try again",
		codedflow.ShowMoreID:  codedflow.ShowMoreTitle,
	}
}

func (st *TurnState) rememberID(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	for _, existing := range st.GroundedIDs {
		if existing == id {
			return
		}
	}
	st.GroundedIDs = append(st.GroundedIDs, id)
}

func (st *TurnState) rememberIDs(ids ...string) {
	for _, id := range ids {
		st.rememberID(id)
	}
}

func (st *TurnState) isGrounded(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if _, ok := st.FixedActions[id]; ok {
		return true
	}
	for _, existing := range st.GroundedIDs {
		if existing == id {
			return true
		}
	}
	return false
}

func (st *TurnState) appendTranscript(role, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	st.Transcript = append(st.Transcript, TranscriptTurn{Role: role, Text: text})
}

// ReservedFieldPhrases must never be stored as capture, cart-edit, add-on, or recovery values.
func ReservedFieldPhrases() []string {
	return []string{
		"talk to staff",
		"talk to agent",
		"talk to a person",
		"checkout",
		"check out",
		"check-out",
		"place order",
		"place my order",
		"ready to checkout",
		"ready to check out",
	}
}

func IsReservedFieldPhrase(text string) bool {
	n := strings.ToLower(strings.TrimSpace(text))
	for _, p := range ReservedFieldPhrases() {
		if n == p {
			return true
		}
	}
	return false
}

func LooksLikeStaffRequest(text string) bool {
	n := strings.ToLower(strings.TrimSpace(text))
	if n == "" {
		return false
	}
	if n == fixedTalkToStaff || n == "talk_to_agent" {
		return true
	}
	needles := []string{"talk to staff", "talk to agent", "human", "real person", "customer service"}
	for _, n2 := range needles {
		if strings.Contains(n, n2) {
			return true
		}
	}
	return false
}

func LooksLikeCheckoutConfirm(text, buttonID string) bool {
	id := strings.ToLower(strings.TrimSpace(buttonID))
	if id == fixedConfirmItems || id == fixedCheckout || id == "confirm_items" || id == "checkout" {
		return true
	}
	n := strings.ToLower(strings.TrimSpace(text))
	switch n {
	case "confirm", "confirm items", "yes", "place order", "place my order", "checkout", "check out", "pay":
		return true
	default:
		return false
	}
}

func LooksLikeIntentionalRequest(text string) bool {
	n := strings.ToLower(strings.TrimSpace(text))
	if n == "" {
		return false
	}
	if LooksLikeStaffRequest(n) || LooksLikeCheckoutConfirm(n, "") {
		return true
	}
	prefixes := []string{"buy ", "i want ", "i need ", "show me ", "order status", "my order", "track "}
	for _, p := range prefixes {
		if strings.HasPrefix(n, p) || strings.Contains(n, p) {
			return true
		}
	}
	// Keyword-only trigger: short and matches nothing shopping-like
	return len([]rune(n)) > 24
}
