package tiqrecommercev2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

func TestLineIDFor_DifferentCapturesStaySeparate(t *testing.T) {
	a := LineIDFor("99", map[string]string{"message": "Happy birthday"})
	b := LineIDFor("99", map[string]string{"message": "Congrats"})
	c := LineIDFor("99", map[string]string{"message": "Happy birthday"})
	assert.NotEqual(t, a, b)
	assert.Equal(t, a, c)
}

func TestOrderItemsForAPI_KeepsDistinctLines(t *testing.T) {
	lines := []CartLine{
		{LineID: LineIDFor("1", map[string]string{"m": "a"}), ProductOption: "1", Quantity: 1, CaptureFields: map[string]string{"m": "a"}},
		{LineID: LineIDFor("1", map[string]string{"m": "b"}), ProductOption: "1", Quantity: 2, CaptureFields: map[string]string{"m": "b"}},
	}
	items := OrderItemsForAPI(lines)
	require.Len(t, items, 2)
	assert.Equal(t, 1, items[0]["quantity"])
	assert.Equal(t, 2, items[1]["quantity"])
}

func TestRuntime_CartLineIdentityAndGrounding(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	rt := &Runtime{
		Conv:  codedflow.NewConv(nil, &stubChat{session: session}),
		State: TurnState{FixedActions: defaultFixedActions()},
	}
	rt.State.rememberID("opt-1")

	line, errMsg := rt.AddCartLine("opt-1", "Cake", "1kg", 1, map[string]string{"msg": "Hi"}, map[string]string{"msg": "Message"}, []string{"msg"})
	require.Empty(t, errMsg)
	require.NotEmpty(t, line.LineID)

	_, errMsg = rt.AddCartLine("invented", "X", "Y", 1, nil, nil, nil)
	assert.Contains(t, errMsg, "not grounded")

	line2, errMsg := rt.AddCartLine("opt-1", "Cake", "1kg", 1, map[string]string{"msg": "Bye"}, map[string]string{"msg": "Message"}, []string{"msg"})
	require.Empty(t, errMsg)
	assert.NotEqual(t, line.LineID, line2.LineID)
	assert.Len(t, rt.cart(), 2)

	require.Empty(t, rt.UpdateCartLine(line.LineID, 3))
	updated, ok := findCartLine(rt.cart(), line.LineID)
	require.True(t, ok)
	assert.Equal(t, 3, updated.Quantity)

	name, errMsg := rt.RemoveCartLine(line2.LineID)
	require.Empty(t, errMsg)
	assert.NotEmpty(t, name)
	assert.Len(t, rt.cart(), 1)
}

func TestRuntime_ReservedCaptureRejected(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	rt := &Runtime{
		Conv:  codedflow.NewConv(nil, &stubChat{session: session}),
		State: TurnState{FixedActions: defaultFixedActions()},
	}
	rt.State.rememberID("opt-1")
	_, errMsg := rt.AddCartLine("opt-1", "Cake", "1kg", 1, map[string]string{"msg": "talk to staff"}, nil, nil)
	assert.Contains(t, errMsg, "not a capture answer")
}

func TestGates_CreateOrderAndTransfer(t *testing.T) {
	rt := &Runtime{State: TurnState{}}
	assert.Contains(t, rt.GateCreateOrder(), "refused")
	assert.Equal(t, "create_order", rt.State.PendingConfirm)

	rt.AllowCreateOrder = true
	assert.Empty(t, rt.GateCreateOrder())

	rt2 := &Runtime{State: TurnState{}}
	assert.Contains(t, rt2.GateTransfer(), "refused")
	rt2.AllowTransfer = true
	assert.Empty(t, rt2.GateTransfer())
}

func TestSetDeliveryPin_AttemptCap(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	rt := &Runtime{
		Conv:  codedflow.NewConv(nil, &stubChat{session: session}),
		State: TurnState{},
	}
	store := map[string]any{
		"location_based_delivery": true,
		"latitude":                10.0,
		"longitude":               76.0,
		"delivery_radius":         1.0,
		"free_delivery_radius":    0.5,
		"delivery_modes":          []any{modePickup, modeDelivery},
	}
	for i := 1; i <= maxPinAttempts; i++ {
		data, errMsg := rt.SetDeliveryPin(0, 0, store, true)
		require.Empty(t, errMsg)
		assert.Equal(t, false, data["deliverable"])
		assert.Equal(t, i, rt.State.PinAttempts)
		if i < maxPinAttempts {
			assert.Equal(t, "ask_pin_again", data["next"])
		} else {
			assert.Equal(t, "offer_pickup_or_transfer", data["next"])
		}
	}
}

func TestLooksLikeStaffAndCheckout(t *testing.T) {
	assert.True(t, LooksLikeStaffRequest("please talk to staff"))
	assert.True(t, LooksLikeCheckoutConfirm("confirm items", ""))
	assert.True(t, LooksLikeCheckoutConfirm("", fixedConfirmItems))
	assert.True(t, IsReservedFieldPhrase("checkout"))
}

func TestModelFromSettings(t *testing.T) {
	assert.Equal(t, DefaultModel, ModelFromSettings(nil))
	assert.Equal(t, "gemini-2.0-flash", ModelFromSettings(models.JSONB{SettingModel: "gemini-2.0-flash"}))
	_, err := NormalizeModelID("bad model!")
	assert.Error(t, err)
}

func TestFlowRegistered(t *testing.T) {
	flow := codedflow.ByKey(FlowKey)
	require.NotNil(t, flow)
	assert.Equal(t, "TiQR Ecommerce V2", flow.Name)
}

type stubChat struct {
	session   *models.ChatbotSession
	buttonID  string
	userInput string
	flowData  map[string]any
	consumed  bool
}

func (c *stubChat) Session() *models.ChatbotSession             { return c.session }
func (c *stubChat) Account() *models.WhatsAppAccount           { return nil }
func (c *stubChat) Contact() *models.Contact                   { return nil }
func (c *stubChat) UserInput() string                          { return c.userInput }
func (c *stubChat) SetUserInput(v string)                      { c.userInput = v }
func (c *stubChat) InboundMedia() codedflow.InboundMedia       { return codedflow.InboundMedia{} }
func (c *stubChat) ButtonID() string                           { return c.buttonID }
func (c *stubChat) SetButtonID(v string)                       { c.buttonID = v }
func (c *stubChat) FlowResponseData() map[string]any           { return c.flowData }
func (c *stubChat) SetFlowResponseData(v map[string]any)       { c.flowData = v }
func (c *stubChat) QuotedMessage() *codedflow.QuotedMessage    { return nil }
func (c *stubChat) Consumed() bool                             { return c.consumed }
func (c *stubChat) SetConsumed(v bool)                         { c.consumed = v }
func (c *stubChat) Capturing() bool                            { return false }
func (c *stubChat) LastTiqr() map[string]any                   { return nil }
func (c *stubChat) SetLastTiqr(map[string]any)                 {}
func (c *stubChat) LastTiqrErr() string                        { return "" }
func (c *stubChat) SetLastTiqrErr(string)                      {}
func (c *stubChat) LastTiqrStatus() int                        { return 0 }
func (c *stubChat) SetLastTiqrStatus(int)                      {}
func (c *stubChat) Preview() *codedflow.PreviewSink            { return nil }
func (c *stubChat) SetPreview(*codedflow.PreviewSink)          {}
func (c *stubChat) Archive() *codedflow.PreviewSink            { return nil }
func (c *stubChat) SetArchive(*codedflow.PreviewSink)          {}
func (c *stubChat) Native() any                                { return nil }
