package handlers

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterProductsWithOptions(t *testing.T) {
	t.Parallel()

	in := []ticker.ProductSummary{
		{ID: 1, Name: "With opts", Options: []ticker.ProductOption{{ID: 1, Name: "A"}}},
		{ID: 2, Name: "No opts"},
	}
	out := filterProductsWithOptions(in)
	require.Len(t, out, 1)
	assert.Equal(t, 1, out[0].ID)
}

func TestCheckoutStateRoundTrip(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		BaseModel:   models.BaseModel{ID: uuid.New()},
		SessionData: models.JSONB{},
	}
	setCheckoutState(session, &checkoutState{
		Step:         "email",
		Email:        "",
		DeliveryMode: "",
		NewAddress:   map[string]any{},
	})
	st := getCheckoutState(session)
	require.NotNil(t, st)
	assert.Equal(t, "email", st.Step)

	clearCheckoutState(session)
	assert.Nil(t, getCheckoutState(session))
}

func TestPlaceCommerceOrder(t *testing.T) {
	t.Parallel()

	stub := &stubCommerceBackend{
		createFn: func(ctx context.Context, body ticker.CreateOrderRequest) (map[string]any, error) {
			return map[string]any{
				"display_uid": "ORD-1",
				"amount":      5000,
			}, nil
		},
	}
	rt := &commerceRuntime{Client: stub, StoreID: "1", PhoneNumber: "+911"}
	result, err := placeCommerceOrder(context.Background(), rt, createOrderArgs{
		Confirmed:    true,
		Items:        []map[string]any{{"product_option": 10, "quantity": 2}},
		Email:        "a@b.com",
		DeliveryMode: "PICKUP_FROM_STORE",
	})
	require.NoError(t, err)
	assert.Equal(t, "ORD-1", result["display_uid"])
	assert.Equal(t, 10, stub.lastCreate.Items[0].ProductOption)
	assert.Equal(t, 2, stub.lastCreate.Items[0].Quantity)
}

func TestFormatCheckoutCartSummary(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{SessionData: models.JSONB{
		cartKey: map[string]any{
			"1": map[string]any{
				"qty": 2,
				"product": map[string]any{
					"option_name": "Large",
					"price":       100.0,
				},
			},
		},
	}}
	summary := formatCheckoutCartSummary(session)
	assert.Contains(t, summary, "Large x2")
	assert.Contains(t, summary, "₹200.00")
}

func TestCartIsEmpty(t *testing.T) {
	t.Parallel()

	assert.True(t, cartIsEmpty(nil))
	assert.True(t, cartIsEmpty(&models.ChatbotSession{SessionData: models.JSONB{}}))
	assert.False(t, cartIsEmpty(&models.ChatbotSession{SessionData: models.JSONB{
		cartKey: map[string]any{"1": map[string]any{"qty": 1}},
	}}))
}

func TestHandleCartQuantityReplyLogic(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			cartKey: map[string]any{
				"7": map[string]any{
					"qty": 1,
					"product": map[string]any{"option_name": "Medium"},
				},
			},
			cartPendingOptionKey: "7",
		},
	}
	pendingID := getCartPendingOptionID(session)
	require.Equal(t, "7", pendingID)
	qty := parsePositiveInt("4")
	require.Equal(t, 4, qty)
	require.True(t, setCartLineQty(session, pendingID, qty))
	clearCartPendingOption(session)
	assert.Equal(t, 4, anyToInt(normalizeCartMap(session.SessionData[cartKey])["7"]["qty"]))
	assert.Empty(t, getCartPendingOptionID(session))
}

func TestParseDeliveryAddressText_CommaSeparated(t *testing.T) {
	t.Parallel()

	st := &checkoutState{
		Email: "a@b.com",
		NewAddress: map[string]any{
			"name":  "Roopak",
			"phone": "9999999999",
		},
	}
	addr := parseDeliveryAddressText("B2-803, SNN Raj Greenbay, Bengaluru, Karnataka, 560100", nil, st)
	assert.Equal(t, "560100", addr["pincode"])
	assert.Equal(t, "Bengaluru", addr["city"])
	assert.Equal(t, "Karnataka", addr["state"])
	assert.Contains(t, asString(addr["address_line_1"]), "B2-803")
	assert.Equal(t, "a@b.com", addr["email"])
	assert.Equal(t, "India", addr["country"])
}

func TestParseDeliveryAddressText_Labeled(t *testing.T) {
	t.Parallel()

	text := "Name: Roopak\nPhone: 9876543210\nAddress: B2-803, SNN Raj Greenbay\nCity: Bangalore\nState: Karnataka\nPincode: 560100\nCountry: India"
	addr := parseDeliveryAddressText(text, nil, &checkoutState{Email: "x@y.com", NewAddress: map[string]any{}})
	assert.Equal(t, "Roopak", addr["name"])
	assert.Equal(t, "9876543210", addr["phone"])
	assert.Equal(t, "B2-803, SNN Raj Greenbay", addr["address_line_1"])
	assert.Equal(t, "Bangalore", addr["city"])
	assert.Equal(t, "Karnataka", addr["state"])
	assert.Equal(t, "560100", addr["pincode"])
	assert.Equal(t, "India", addr["country"])
}
