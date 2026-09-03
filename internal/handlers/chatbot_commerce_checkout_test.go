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
					"qty":     1,
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

func TestCheckoutBuyerMeta_IncludesShippingAddress(t *testing.T) {
	t.Parallel()

	st := &checkoutState{
		Email:        "a@b.com",
		DeliveryMode: "DELIVERY_TO_LOCATION",
		HasLocation:  true,
		Latitude:     12.97,
		Longitude:    77.59,
		NewAddress: map[string]any{
			"name":           "Roopak",
			"phone":          "9876543210",
			"email":          "a@b.com",
			"address_line_1": "B2-803",
			"city":           "Bengaluru",
			"state":          "Karnataka",
			"country":        "India",
			"pincode":        "560100",
		},
	}
	meta := checkoutBuyerMeta(st, nil)
	assert.Equal(t, "a@b.com", meta["email"])
	assert.Equal(t, "Roopak", meta["name"])
	assert.Equal(t, 12.97, meta["latitude"])
	assert.Equal(t, 77.59, meta["longitude"])
	shipping, ok := meta["shipping_address"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "B2-803", shipping["address_line_1"])
	assert.Equal(t, "560100", shipping["pincode"])
	_, hasEmail := shipping["email"]
	assert.False(t, hasEmail, "write-only email should not be in display address")
	billing, ok := meta["billing_address"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Bengaluru", billing["city"])
}

func TestFormatOrderConfirmSummary_IncludesDeliveryFee(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{SessionData: models.JSONB{
		cartKey: map[string]any{
			"1": map[string]any{
				"qty": 1,
				"product": map[string]any{
					"option_name": "Tea",
					"price":       100.0,
				},
			},
		},
	}}
	st := &checkoutState{
		Email:            "a@b.com",
		DeliveryMode:     "DELIVERY_TO_LOCATION",
		DeliveryZone:     "paid",
		ShippingFeePaise: 5000,
		NewAddress: map[string]any{
			"address_line_1": "B2",
			"city":           "Bengaluru",
			"state":          "KA",
			"pincode":        "560100",
		},
	}
	msg := formatOrderConfirmSummary(session, st)
	assert.Contains(t, msg, "Delivery fee: ₹50.00")
	assert.Contains(t, msg, "Estimated total: ₹150.00")
}

func TestFormatOrderSuccessMessage_IncludesDeliveryFee(t *testing.T) {
	t.Parallel()

	msg := formatOrderSuccessMessage(map[string]any{
		"display_uid":  "ORD-1",
		"amount":       150.0,
		"shipping_fee": 50.0,
		"payment_url":  "https://pay.example",
	})
	assert.Contains(t, msg, "Delivery fee: ₹50.00")
	assert.Contains(t, msg, "Total: ₹150.00")
}

func TestCheckoutStateRoundTrip_LocationFields(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		BaseModel:   models.BaseModel{ID: uuid.New()},
		SessionData: models.JSONB{},
	}
	setCheckoutState(session, &checkoutState{
		Step:             "location",
		Email:            "a@b.com",
		DeliveryMode:     "DELIVERY_TO_LOCATION",
		Latitude:         12.9,
		Longitude:        77.6,
		HasLocation:      true,
		DeliveryZone:     "paid",
		ShippingFeePaise: 2500,
		NewAddress:       map[string]any{"city": "Bengaluru"},
	})
	st := getCheckoutState(session)
	require.NotNil(t, st)
	assert.Equal(t, "location", st.Step)
	assert.True(t, st.HasLocation)
	assert.Equal(t, 12.9, st.Latitude)
	assert.Equal(t, 77.6, st.Longitude)
	assert.Equal(t, "paid", st.DeliveryZone)
	assert.Equal(t, int64(2500), st.ShippingFeePaise)
}

func TestExtractQtyFromText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 2, extractQtyFromText("2"))
	assert.Equal(t, 2, extractQtyFromText("change, i want 2"))
	assert.Equal(t, 2, extractQtyFromText("I want 2 products"))
	assert.Equal(t, 0, extractQtyFromText("i want to update the cart"))
	assert.Equal(t, 0, parsePositiveInt("560100"))
}

func TestIsCheckoutEditExitIntent(t *testing.T) {
	t.Parallel()

	assert.True(t, isCheckoutEditExitIntent("i want to update the cart"))
	assert.True(t, isCheckoutEditExitIntent("change, i want 2"))
	assert.True(t, isCheckoutEditExitIntent("explore more"))
	assert.True(t, isCheckoutEditExitIntent("cancel checkout"))
	assert.False(t, isCheckoutEditExitIntent("anroopak@gmail.com"))
	assert.False(t, isCheckoutEditExitIntent("I want 2 products")) // qty phrase, not exit
}

func TestIsCheckoutQtyPhrase(t *testing.T) {
	t.Parallel()

	assert.True(t, isCheckoutQtyPhrase("I want 2 products"))
	assert.True(t, isCheckoutQtyPhrase("make it 3"))
	assert.False(t, isCheckoutQtyPhrase("hello there"))
	assert.False(t, isCheckoutQtyPhrase("2")) // bare int handled separately
}

func TestSoleCartOptionID(t *testing.T) {
	t.Parallel()

	single := &models.ChatbotSession{SessionData: models.JSONB{
		cartKey: map[string]any{
			"7": map[string]any{"qty": 1, "product": map[string]any{"option_name": "A"}},
		},
	}}
	id, ok := soleCartOptionID(single)
	require.True(t, ok)
	assert.Equal(t, 7, id)

	multi := &models.ChatbotSession{SessionData: models.JSONB{
		cartKey: map[string]any{
			"7": map[string]any{"qty": 1},
			"8": map[string]any{"qty": 2},
		},
	}}
	_, ok = soleCartOptionID(multi)
	assert.False(t, ok)
}

func TestCheckoutQtyUpdate_LeavesEmailStep(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{SessionData: models.JSONB{
		cartKey: map[string]any{
			"7": map[string]any{
				"qty":     1,
				"product": map[string]any{"option_name": "Linea Pearl Chain", "price": 1.0},
			},
		},
	}}
	setCheckoutState(session, &checkoutState{Step: "email", NewAddress: map[string]any{}})

	require.True(t, setCartLineQty(session, "7", 2))
	st := getCheckoutState(session)
	require.NotNil(t, st)
	assert.Equal(t, "email", st.Step)
	assert.Equal(t, 2, anyToInt(normalizeCartMap(session.SessionData[cartKey])["7"]["qty"]))
}

func TestCheckoutEditExit_ClearsCheckoutKeepsCart(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{SessionData: models.JSONB{
		cartKey: map[string]any{
			"7": map[string]any{"qty": 1, "product": map[string]any{"option_name": "A"}},
		},
	}}
	setCheckoutState(session, &checkoutState{Step: "email", Email: "a@b.com", NewAddress: map[string]any{}})
	require.NotNil(t, getCheckoutState(session))

	clearCheckoutState(session)
	optID, ok := soleCartOptionID(session)
	require.True(t, ok)
	setCartPendingOption(session, optID)

	assert.Nil(t, getCheckoutState(session))
	assert.Equal(t, "7", getCartPendingOptionID(session))
	assert.False(t, cartIsEmpty(session))
}

func TestConfirmStepHelpers_DoNotTreatRandomAsYes(t *testing.T) {
	t.Parallel()

	assert.False(t, isCheckoutConfirmYes("I want 2 products"))
	assert.False(t, isCheckoutConfirmYes("maybe later"))
	assert.True(t, isCheckoutConfirmYes("yes"))
	assert.True(t, isCheckoutConfirmYes("confirm"))
	assert.True(t, isCheckoutCancelText("cancel"))
}

func TestMultiLineCart_BareNumberDoesNotUpdate(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{SessionData: models.JSONB{
		cartKey: map[string]any{
			"7": map[string]any{"qty": 1},
			"8": map[string]any{"qty": 1},
		},
	}}
	_, ok := soleCartOptionID(session)
	assert.False(t, ok)
	// Without a sole line, qty update must not guess which line to change.
	assert.Equal(t, 1, anyToInt(normalizeCartMap(session.SessionData[cartKey])["7"]["qty"]))
	assert.Equal(t, 1, anyToInt(normalizeCartMap(session.SessionData[cartKey])["8"]["qty"]))
}

func TestCheckoutButtonIDs_IncludeExplore(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "checkout_explore", checkoutExploreButtonID)
	assert.True(t, IsCheckoutButton(checkoutExploreButtonID))
	assert.True(t, IsCheckoutButton(checkoutButtonID))
}

func TestIsIndiaStoreCountry(t *testing.T) {
	t.Parallel()

	assert.True(t, isIndiaStoreCountry("IN"))
	assert.True(t, isIndiaStoreCountry("in"))
	assert.True(t, isIndiaStoreCountry("India"))
	assert.True(t, isIndiaStoreCountry(" india "))
	assert.False(t, isIndiaStoreCountry("US"))
	assert.False(t, isIndiaStoreCountry(""))
	assert.False(t, isIndiaStoreCountry("AE"))
}

func TestIsIndiaWhatsAppNumber(t *testing.T) {
	t.Parallel()

	assert.True(t, isIndiaWhatsAppNumber("919876543210"))
	assert.True(t, isIndiaWhatsAppNumber("+91 98765 43210"))
	assert.True(t, isIndiaWhatsAppNumber("+919876543210"))
	assert.False(t, isIndiaWhatsAppNumber(""))
	assert.False(t, isIndiaWhatsAppNumber("9876543210"))
	assert.False(t, isIndiaWhatsAppNumber("6581234567"))
	assert.False(t, isIndiaWhatsAppNumber("6512345678"))
	assert.False(t, isIndiaWhatsAppNumber("911234567890")) // 91 + landline-like 1…
}

func TestMapAddressMessageToNewAddress(t *testing.T) {
	t.Parallel()

	values := map[string]any{
		"name":          "CUSTOMER_NAME",
		"phone_number":  "+919876543210",
		"in_pin_code":   "400063",
		"house_number":  "B2",
		"floor_number":  "8",
		"building_name": "Cello Triumph",
		"address":       "IB Patel Rd",
		"landmark_area": "Goregaon",
		"city":          "Mumbai",
		"state":         "Maharashtra",
	}
	st := &checkoutState{Email: "a@b.com", NewAddress: map[string]any{}}
	addr := mapAddressMessageToNewAddress(values, nil, st)
	assert.Equal(t, "CUSTOMER_NAME", addr["name"])
	assert.Equal(t, "+919876543210", addr["phone"])
	assert.Equal(t, "400063", addr["pincode"])
	assert.Equal(t, "Mumbai", addr["city"])
	assert.Equal(t, "Maharashtra", addr["state"])
	assert.Equal(t, "Goregaon", addr["landmark"])
	assert.Equal(t, "a@b.com", addr["email"])
	assert.Equal(t, "India", addr["country"])
	assert.Contains(t, asString(addr["address_line_1"]), "IB Patel Rd")
	assert.Contains(t, asString(addr["address_line_1"]), "Cello Triumph")
	assert.True(t, deliveryAddressComplete(addr))
}

func TestExtractAddressMessageValues_Nested(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"saved_address_id": "address1",
		"values": map[string]any{
			"in_pin_code": "400063",
			"city":        "Mumbai",
		},
	}
	vals := extractAddressMessageValues(raw)
	assert.Equal(t, "400063", vals["in_pin_code"])
	assert.Equal(t, "Mumbai", vals["city"])
	assert.NotContains(t, vals, "saved_address_id")
}

func TestDeliveryAddressComplete(t *testing.T) {
	t.Parallel()

	assert.False(t, deliveryAddressComplete(map[string]any{"address_line_1": "Street"}))
	assert.False(t, deliveryAddressComplete(map[string]any{"pincode": "560100"}))
	assert.False(t, deliveryAddressComplete(map[string]any{"address_line_1": "Street", "pincode": "000000"}))
	assert.True(t, deliveryAddressComplete(map[string]any{"address_line_1": "Street", "pincode": "560100"}))
}

func TestHandleCheckoutAddressMessage_IgnoresOtherNFM(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		BaseModel:   models.BaseModel{ID: uuid.New()},
		SessionData: models.JSONB{},
	}
	setCheckoutState(session, &checkoutState{Step: "address", NewAddress: map[string]any{}})
	app := testApp()
	assert.False(t, app.handleCheckoutAddressMessage(nil, nil, session, nil, "some_flow", map[string]any{}))
	assert.Equal(t, "address", getCheckoutState(session).Step)
}

func TestHandleCheckoutAddressMessage_IgnoresWhenNotAddressStep(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		BaseModel:   models.BaseModel{ID: uuid.New()},
		SessionData: models.JSONB{},
	}
	setCheckoutState(session, &checkoutState{Step: "email", NewAddress: map[string]any{}})
	app := testApp()
	assert.False(t, app.handleCheckoutAddressMessage(nil, nil, session, nil, "address_message", map[string]any{
		"values": map[string]any{"in_pin_code": "560100", "address": "Street"},
	}))
	assert.Equal(t, "email", getCheckoutState(session).Step)
}
