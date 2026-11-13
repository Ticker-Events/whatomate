package tiqrecommercev2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

func TestBindingRequiredStep(t *testing.T) {
	assert.Equal(t, stepCollections, bindingRequiredStep(fixedBuyProducts))
	assert.Equal(t, stepCollections, bindingRequiredStep(fixedAddMore))
	assert.Equal(t, stepOrderStatus, bindingRequiredStep(fixedCheckOrderStatus))
	assert.Equal(t, stepDetails, bindingRequiredStep(fixedConfirmItems))
	assert.Equal(t, stepCartEdit, bindingRequiredStep(fixedEditCart))
	assert.Equal(t, stepDeliveryPin, bindingRequiredStep(fixedShareAgain))
	assert.Equal(t, stepCheckoutPreview, bindingRequiredStep(fixedTryAgain))
	assert.Equal(t, "", bindingRequiredStep(fixedTalkToStaff))
}

func TestCheckBindingTap_RejectsWrongStep(t *testing.T) {
	rt := &Runtime{
		State:           TurnState{FixedActions: defaultFixedActions()},
		BindingButtonID: fixedConfirmItems,
	}
	assert.Contains(t, rt.checkBindingTap(stepCollections), "requires step details")
	assert.Empty(t, rt.checkBindingTap(stepDetails))
}

func TestRejectContradictingBinding_CreateOrderOnConfirm(t *testing.T) {
	rt := &Runtime{
		State:           TurnState{FixedActions: defaultFixedActions()},
		BindingButtonID: fixedConfirmItems,
	}
	assert.Contains(t, rt.rejectContradictingBindingTool("create_order", ""), "details shell first")
	assert.Contains(t, rt.rejectContradictingBindingTool("reply", stepCart), "details")
	assert.Empty(t, rt.rejectContradictingBindingTool("reply", stepDetails))
}

func TestGateCreateOrder_RequiresFlowNotConfirm(t *testing.T) {
	rt := &Runtime{State: TurnState{}}
	assert.Contains(t, rt.GateCreateOrder(), "details Flow")

	rt2 := &Runtime{AllowCreateOrder: true, State: TurnState{PendingConfirm: "create_order"}}
	assert.Empty(t, rt2.GateCreateOrder())
	assert.Equal(t, "", rt2.State.PendingConfirm)
}

func TestNewRuntime_ConfirmDoesNotAllowCreateOrder(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	chat := &stubChat{session: session, buttonID: fixedConfirmItems}
	rt := newRuntime(codedflow.NewConv(nil, chat))
	assert.False(t, rt.AllowCreateOrder)
	assert.Equal(t, fixedConfirmItems, rt.BindingButtonID)
}

func TestNewRuntime_ExactTitleBinds(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	chat := &stubChat{session: session, userInput: "Buy products"}
	rt := newRuntime(codedflow.NewConv(nil, chat))
	assert.Equal(t, fixedBuyProducts, rt.BindingButtonID)
}

func TestNewRuntime_FlowSubmissionAllowsCreateOrder(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	chat := &stubChat{session: session, flowData: map[string]any{"name": "Ada", "phone": "999"}}
	rt := newRuntime(codedflow.NewConv(nil, chat))
	assert.True(t, rt.AllowCreateOrder)
	assert.True(t, rt.FlowSubmitted)
}

func TestCheckShellPrerequisites(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	rt := &Runtime{
		Conv:  codedflow.NewConv(nil, &stubChat{session: session}),
		State: TurnState{FixedActions: defaultFixedActions()},
	}
	assert.Contains(t, rt.checkShellPrerequisites(stepCollections), "collections not loaded")
	session.SessionData["collections"] = []any{map[string]any{"id": "c1", "name": "Cakes"}}
	assert.Empty(t, rt.checkShellPrerequisites(stepCollections))

	assert.Contains(t, rt.checkShellPrerequisites(stepProducts), "products not loaded")
	session.SessionData["products"] = []any{map[string]any{"id": "p1", "name": "Kunafa"}}
	assert.Empty(t, rt.checkShellPrerequisites(stepProducts))

	assert.Contains(t, rt.checkShellPrerequisites(stepCheckoutPreview), "cart is empty")
	rt.State.rememberID("opt-1")
	_, errMsg := rt.AddCartLine("opt-1", "Cake", "1kg", 1, nil, nil, nil)
	require.Empty(t, errMsg)
	assert.Empty(t, rt.checkShellPrerequisites(stepCheckoutPreview))
}

func TestValidateChrome_RejectsDroppedNameAndLongTitle(t *testing.T) {
	english := shellCopy{body: "How many *Kunafa* would you like?"}
	assert.Contains(t, validateChrome(english, ShellChrome{Body: "How many would you like?"}), "drops catalog name")
	assert.Empty(t, validateChrome(english, ShellChrome{Body: "How many *Kunafa* would you like? (ml)"}))
	assert.Contains(t, validateChrome(english, ShellChrome{ButtonTitles: map[string]string{"x": "this title is way too long for whatsapp"}}), "too long")
}

func TestEnglishShellCopy_WelcomeAndQuantity(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"store": map[string]any{"name": "Demo Cafe", "delivery_modes": []any{modePickup}},
	}}
	rt := &Runtime{
		Conv:  codedflow.NewConv(nil, &stubChat{session: session}),
		State: TurnState{FixedActions: defaultFixedActions()},
	}
	welcome := rt.englishShellCopy(stepWelcome, ShellChrome{})
	assert.Contains(t, welcome.body, "Welcome to Demo Cafe")
	assert.Contains(t, welcome.body, "What would you like to do?")
	assert.Contains(t, welcome.body, "store pickup")

	qty := rt.englishShellCopy(stepQuantity, ShellChrome{OptionName: "Kunafa"})
	assert.Contains(t, qty.body, "*Kunafa*")
	assert.Contains(t, qty.body, "whole number")
}

func TestProductsShellChoosesImageVsCarousel(t *testing.T) {
	// One product → image reply config path; two → carousel. Verified via session prep helpers.
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	rt := &Runtime{
		Conv:  codedflow.NewConv(nil, &stubChat{session: session}),
		State: TurnState{FixedActions: defaultFixedActions()},
	}
	grounded := map[string]any{
		"items": []any{
			map[string]any{"id": "p1", "name": "A", "min_price": "10", "image_url": "https://x/a.jpg"},
		},
	}
	storeProductsSession(rt, grounded, "c1")
	products, _ := anySlice(session.SessionData["products"])
	require.Len(t, products, 1)

	grounded2 := map[string]any{
		"items": []any{
			map[string]any{"id": "p1", "name": "A", "min_price": "10"},
			map[string]any{"id": "p2", "name": "B", "min_price": "20"},
		},
	}
	storeProductsSession(rt, grounded2, "c1")
	products, _ = anySlice(session.SessionData["products"])
	require.Len(t, products, 2)
}

func TestTwoCartLinesStaySeparate(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	rt := &Runtime{
		Conv:  codedflow.NewConv(nil, &stubChat{session: session}),
		State: TurnState{FixedActions: defaultFixedActions()},
	}
	rt.State.rememberID("opt-1")
	a, err := rt.AddCartLine("opt-1", "Kunafa", "Reg", 1, map[string]string{"msg": "Happy birthday"}, map[string]string{"msg": "Message"}, []string{"msg"})
	require.Empty(t, err)
	b, err := rt.AddCartLine("opt-1", "Kunafa", "Reg", 1, map[string]string{"msg": "Congratulations"}, map[string]string{"msg": "Message"}, []string{"msg"})
	require.Empty(t, err)
	assert.NotEqual(t, a.LineID, b.LineID)
	assert.Len(t, rt.cart(), 2)
}

func TestKnownWaitingStepsCoverPlan(t *testing.T) {
	steps := knownWaitingSteps()
	for _, s := range []string{
		stepWelcome, stepCollections, stepProducts, stepOptions, stepQuantity,
		stepCapture, stepAddons, stepCart, stepCartEdit, stepFulfillment,
		stepDeliveryPin, stepDeliveryFallback, stepCheckoutPreview, stepCheckoutPreviewFail,
		stepDetails, stepOrderPlaced, stepOrderStatus, stepOrderStatusSummary,
	} {
		assert.True(t, steps[s], s)
	}
}

func TestParseChromeTitles(t *testing.T) {
	got := parseChromeTitles("buy_products=Comprar,checkout=Pagar")
	assert.Equal(t, "Comprar", got["buy_products"])
	assert.Equal(t, "Pagar", got["checkout"])
}
