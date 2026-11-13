package tiqrecommercev2

import (
	"fmt"
	"strings"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

// Runtime is the per-turn tool context. Tools mutate session state through it.
type Runtime struct {
	Conv   *codedflow.Conv
	State  TurnState
	Rounds int

	AllowCreateOrder bool
	AllowTransfer    bool

	Replied  bool
	Ended    bool
	Transfer bool
	LastErr  string
}

func newRuntime(c *codedflow.Conv) *Runtime {
	session := c.Session()
	st := loadState(session)
	userText := ""
	buttonID := ""
	if chat := c.ChatCtx(); chat != nil {
		userText = chat.UserInput()
		buttonID = chat.ButtonID()
	}
	rt := &Runtime{
		Conv:             c,
		State:            st,
		AllowCreateOrder: LooksLikeCheckoutConfirm(userText, buttonID) || st.PendingConfirm == "create_order",
		AllowTransfer:    LooksLikeStaffRequest(userText) || buttonID == fixedTalkToStaff || st.PendingConfirm == "transfer",
	}
	return rt
}

func (rt *Runtime) session() *models.ChatbotSession {
	if rt == nil || rt.Conv == nil {
		return nil
	}
	return rt.Conv.Session()
}

func (rt *Runtime) persist() {
	if rt == nil {
		return
	}
	saveState(rt.session(), rt.State)
}

func (rt *Runtime) cart() []CartLine {
	s := rt.session()
	if s == nil || s.SessionData == nil {
		return nil
	}
	return cartLinesFromSession(s.SessionData[sessionCartKey])
}

func (rt *Runtime) setCart(lines []CartLine) {
	s := rt.session()
	if s == nil {
		return
	}
	if s.SessionData == nil {
		s.SessionData = models.JSONB{}
	}
	s.SessionData[sessionCartKey] = cartLinesToSession(lines)
	s.SessionData["cart_count"] = len(lines)
}

func (rt *Runtime) bumpRound() bool {
	rt.Rounds++
	return rt.Rounds <= maxToolRounds
}

func (rt *Runtime) rejectUngrounded(id string) string {
	return fmt.Sprintf("rejected: id %q is not grounded for this session", id)
}

// AddCartLine appends or merges by line id (option + capture).
func (rt *Runtime) AddCartLine(optionID, productName, optionName string, qty int, capture, labels map[string]string, order []string) (CartLine, string) {
	optionID = strings.TrimSpace(optionID)
	if optionID == "" {
		return CartLine{}, "product_option is required"
	}
	if !rt.State.isGrounded(optionID) {
		return CartLine{}, rt.rejectUngrounded(optionID)
	}
	if qty < 1 {
		return CartLine{}, "quantity must be 1 or more"
	}
	for _, v := range capture {
		if IsReservedFieldPhrase(v) {
			return CartLine{}, "that reply is not a capture answer; treat it as checkout or talk to staff"
		}
	}
	lineID := LineIDFor(optionID, capture)
	lines := rt.cart()
	for i := range lines {
		if lines[i].LineID != lineID {
			continue
		}
		lines[i].Quantity += qty
		rt.setCart(lines)
		rt.State.rememberID(lineID)
		return lines[i], ""
	}
	line := CartLine{
		LineID:        lineID,
		ProductOption: optionID,
		Quantity:      qty,
		ProductName:   productName,
		OptionName:    optionName,
		CaptureFields: capture,
		CaptureLabels: labels,
		CaptureOrder:  order,
	}
	lines = append(lines, line)
	rt.setCart(lines)
	rt.State.rememberID(lineID)
	return line, ""
}

func (rt *Runtime) UpdateCartLine(lineID string, qty int) string {
	if IsReservedFieldPhrase(lineID) {
		return "that is not a cart line"
	}
	if !rt.State.isGrounded(lineID) && findLineInCart(rt.cart(), lineID) == nil {
		// Allow update by line id present in cart even if not in grounded set yet.
		if _, ok := findCartLine(rt.cart(), lineID); !ok {
			return rt.rejectUngrounded(lineID)
		}
	}
	lines, ok := updateCartLine(rt.cart(), lineID, qty)
	if !ok {
		return "cart line not found"
	}
	rt.setCart(lines)
	return ""
}

func (rt *Runtime) RemoveCartLine(lineID string) (string, string) {
	lines, name, ok := removeCartLine(rt.cart(), lineID)
	if !ok {
		return "", "cart line not found"
	}
	rt.setCart(lines)
	return name, ""
}

func findLineInCart(lines []CartLine, lineID string) *CartLine {
	for i := range lines {
		if lines[i].LineID == lineID {
			return &lines[i]
		}
	}
	return nil
}

// SetDeliveryPin applies pin eligibility with a 3-attempt out-of-range cap.
func (rt *Runtime) SetDeliveryPin(lat, lng float64, store map[string]any, pickupAvailable bool) (map[string]any, string) {
	el := evaluateStoreDelivery(store, lat, lng)
	s := rt.session()
	if s != nil {
		if s.SessionData == nil {
			s.SessionData = models.JSONB{}
		}
		s.SessionData["delivery_latitude"] = lat
		s.SessionData["delivery_longitude"] = lng
		s.SessionData["delivery_mode"] = modeDelivery
		s.SessionData["delivery_zone"] = el.Zone
		if el.HasDistance {
			s.SessionData["delivery_distance_km"] = el.DistanceKm
		}
	}
	out := map[string]any{
		"deliverable": el.Deliverable,
		"zone":        el.Zone,
		"distance_km": el.DistanceKm,
		"message":     "We can deliver to that location.",
	}
	if el.Deliverable {
		rt.State.PinAttempts = 0
		rt.State.AwaitingLocation = false
		return out, ""
	}
	rt.State.PinAttempts++
	out["message"] = "Sorry, we're currently unable to deliver to this location."
	out["pin_attempts"] = rt.State.PinAttempts
	if rt.State.PinAttempts >= maxPinAttempts {
		if pickupAvailable {
			out["next"] = "offer_pickup_or_transfer"
			out["message"] = out["message"].(string) + " You can switch to store pickup, or talk to staff."
		} else {
			out["next"] = "transfer"
			out["message"] = out["message"].(string) + " Please talk to staff for help."
		}
	} else {
		out["next"] = "ask_pin_again"
		rt.State.AwaitingLocation = true
	}
	return out, ""
}

// GateCreateOrder refuses unless the customer already confirmed this turn or earlier.
func (rt *Runtime) GateCreateOrder() string {
	if rt.AllowCreateOrder {
		rt.State.PendingConfirm = ""
		return ""
	}
	rt.State.PendingConfirm = "create_order"
	return "refused: ask the customer to confirm the priced order, then call create_order after they confirm"
}

// GateTransfer refuses unless the customer asked for staff this turn or earlier.
func (rt *Runtime) GateTransfer() string {
	if rt.AllowTransfer {
		rt.State.PendingConfirm = ""
		return ""
	}
	rt.State.PendingConfirm = "transfer"
	return "refused: ask the customer if they want to talk to staff, then call transfer_to_staff after they agree"
}

func (rt *Runtime) callTiqr(name, operation string, params map[string]string) (map[string]any, bool) {
	if rt.Conv == nil || rt.Conv.App() == nil {
		return nil, false
	}
	raw := make(map[string]any, len(params))
	for k, v := range params {
		raw[k] = v
	}
	out, err := rt.Conv.App().ExecChatTiqrStoreAPI(rt.Conv.ChatCtx(), name, map[string]any{
		"api_type":  "rest",
		"operation": operation,
		"params":    raw,
	})
	if err != nil {
		rt.LastErr = err.Error()
		return nil, false
	}
	if out.Outcome != "http:2xx" {
		return nil, false
	}
	payload := rt.Conv.ChatCtx().LastTiqr()
	return payload, payload != nil
}

func (rt *Runtime) callTiqrMCP(name, operation string, params map[string]string) (map[string]any, bool) {
	if rt.Conv == nil || rt.Conv.App() == nil {
		return nil, false
	}
	raw := make(map[string]any, len(params))
	for k, v := range params {
		raw[k] = v
	}
	out, err := rt.Conv.App().ExecChatTiqrStoreAPI(rt.Conv.ChatCtx(), name, map[string]any{
		"api_type":  "mcp",
		"operation": operation,
		"params":    raw,
	})
	if err != nil {
		rt.LastErr = err.Error()
		return nil, false
	}
	if out.Outcome != "http:2xx" {
		return nil, false
	}
	payload := rt.Conv.ChatCtx().LastTiqr()
	return payload, payload != nil
}

func fulfillmentSummary(store map[string]any) string {
	modes, _ := anySlice(store["delivery_modes"])
	hasPickup, hasDelivery := false, false
	for _, m := range modes {
		switch strings.TrimSpace(asString(m)) {
		case modePickup:
			hasPickup = true
		case modeDelivery:
			hasDelivery = true
		}
	}
	switch {
	case hasPickup && hasDelivery:
		return "We offer store pickup and delivery."
	case hasPickup:
		return "We offer store pickup."
	case hasDelivery:
		return "We offer delivery."
	default:
		return ""
	}
}

func storeHasPickup(store map[string]any) bool {
	modes, _ := anySlice(store["delivery_modes"])
	for _, m := range modes {
		if strings.TrimSpace(asString(m)) == modePickup {
			return true
		}
	}
	return false
}
