package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/shridarpatil/whatomate/pkg/tickermcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPickupOrderParams_MapsFlowContext(t *testing.T) {
	params := tiqrecommerce.PickupOrderParams(map[string]any{
		"customer_name":    "Aswin Divakar",
		"customer_email":   "buyer@example.com",
		"customer_phone":   "9846435358",
		"phone_number":     "919846435358",
		"address_line_one": "Infopark Rd",
		"address_line_two": "TCS",
		"city":             "Kakkanad",
		"state":            "Keralam",
		"country":          "India",
		"pincode":          "682042",
		"customer_notes":   "Note",
		"tiqr_cart": []any{map[string]any{
			"product_option": "1312",
			"quantity":       "2",
			"option_name":    "Kunafa",
		}},
	})

	assert.Equal(t, "buyer@example.com", params["email"])
	assert.Equal(t, "Note", params["notes"])
	assert.Equal(t, "PICKUP_FROM_STORE", params["delivery_mode"])
	assert.Equal(t, "9846435358", params["phone_number"])
	assert.JSONEq(t, `[{"product_option":"1312","quantity":"2"}]`, params["items"])
	assert.NotContains(t, params["items"], "option_name")
	assert.NotContains(t, params["items"], "Kunafa")
	assert.NotContains(t, params, "new_address")
	assert.JSONEq(t, `{
		"name": "Aswin Divakar",
		"email": "buyer@example.com",
		"phone": "9846435358",
		"phone_number": "9846435358",
		"notes": "Note"
	}`, params["buyer_meta_data"])
}

func TestCodedPaymentCTAContent(t *testing.T) {
	t.Parallel()

	body, paymentURL := codedflow.PaymentCTAForTest(map[string]any{
		"display_uid": "TQ-1",
		"amount":      40.0,
		"payment": map[string]any{
			"status": "initiated",
			"meta_data": map[string]any{
				"url_to_redirect": "https://pay.example/go",
			},
		},
	}, "INR")
	assert.Contains(t, body, "Order placed!")
	assert.Contains(t, body, "TQ-1")
	assert.Contains(t, body, "Total:")
	assert.Equal(t, "https://pay.example/go", paymentURL)
	assert.NotContains(t, body, "https://pay.example/go")
	assert.NotContains(t, body, "order is confirmed")

	body, paymentURL = codedflow.PaymentCTAForTest(map[string]any{
		"display_uid": "TQ-MOCK",
		"payment_url": "https://pay.example/mock",
	}, "INR")
	assert.Contains(t, body, "TQ-MOCK")
	assert.Equal(t, "https://pay.example/mock", paymentURL)
	assert.NotContains(t, body, "https://pay.example/mock")

	body, paymentURL = codedflow.PaymentCTAForTest(map[string]any{
		"display_uid": "TQ-NOPAY",
	}, "INR")
	assert.Contains(t, body, "TQ-NOPAY")
	assert.Empty(t, paymentURL)
}

func TestPickupOrderParams_DeliveryIncludesAddress(t *testing.T) {
	params := tiqrecommerce.PickupOrderParams(map[string]any{
		"customer_name":    "Aswin Divakar",
		"customer_email":   "buyer@example.com",
		"customer_phone":   "9846435358",
		"delivery_mode":    "DELIVERY_TO_LOCATION",
		"address_line_one": "Infopark Rd",
		"address_line_two": "TCS",
		"city":             "Kakkanad",
		"state":            "Keralam",
		"country":          "India",
		"pincode":          "682042",
		"customer_notes":   "Note",
		"tiqr_cart": []any{map[string]any{
			"product_option": "1312",
			"quantity":       "2",
		}},
	})

	assert.Equal(t, "DELIVERY_TO_LOCATION", params["delivery_mode"])
	assert.Equal(t, "9846435358", params["phone_number"])
	assert.JSONEq(t, `{
		"name": "Aswin Divakar",
		"address_line_1": "Infopark Rd",
		"address_line_2": "TCS",
		"city": "Kakkanad",
		"state": "Keralam",
		"country": "India",
		"pincode": "682042",
		"email": "buyer@example.com",
		"phone_number": "9846435358",
		"phone": "9846435358"
	}`, params["new_address"])
	assert.JSONEq(t, `{
		"name": "Aswin Divakar",
		"email": "buyer@example.com",
		"phone": "9846435358",
		"phone_number": "9846435358",
		"notes": "Note"
	}`, params["buyer_meta_data"])
}

func TestFormatFailedOrderHandoff(t *testing.T) {
	msg := tiqrecommerce.FormatFailedOrderHandoff(map[string]any{
		"customer_name":    "Aswin Divakar",
		"address_line_one": "Infopark Rd",
		"address_line_two": "TCS",
		"city":             "Kakkanad",
		"state":            "Keralam",
		"country":          "India",
		"pincode":          "682042",
		"tiqr_cart": []any{
			map[string]any{"option_name": "Kunafa", "product_option": "9", "quantity": "2"},
			map[string]any{"product_option": "10", "quantity": "1"},
		},
	})
	assert.Contains(t, msg, "Placing order for the following items failed.")
	assert.Contains(t, msg, "Kunafa x 2")
	assert.Contains(t, msg, "Option 10 x 1")
	assert.Contains(t, msg, "Address\nAswin Divakar\nInfopark Rd, TCS\nKakkanad, Keralam\nIndia 682042")
	assert.Contains(t, msg, tiqrecommerce.HandoffConnect)
}

func TestPickupOrderParams_UsesStoredDeliveryModeAndBuyerMeta(t *testing.T) {
	params := tiqrecommerce.PickupOrderParams(map[string]any{
		"customer_email":     "buyer@example.com",
		"customer_phone":     "9846435358",
		"delivery_mode":      "DELIVERY_TO_LOCATION",
		"delivery_latitude":  12.97,
		"delivery_longitude": 77.59,
		"tiqr_cart":          []any{map[string]any{"product_option": "1", "quantity": "1"}},
	})
	assert.Equal(t, "DELIVERY_TO_LOCATION", params["delivery_mode"])
	assert.JSONEq(t, `{
		"email": "buyer@example.com",
		"phone": "9846435358",
		"phone_number": "9846435358",
		"latitude": 12.97,
		"longitude": 77.59
	}`, params["buyer_meta_data"])

	var address map[string]string
	require.NoError(t, json.Unmarshal([]byte(params["new_address"]), &address))
	assert.Equal(t, "12.970000", address["latitude"])
	assert.Equal(t, "77.590000", address["longitude"])
}

func TestPickupOrderParams_RoundsDeliveryPinOntoAddress(t *testing.T) {
	params := tiqrecommerce.PickupOrderParams(map[string]any{
		"delivery_mode":      "DELIVERY_TO_LOCATION",
		"delivery_latitude":  11.5545985,
		"delivery_longitude": 75.6326679,
	})
	var address map[string]string
	require.NoError(t, json.Unmarshal([]byte(params["new_address"]), &address))
	assert.Equal(t, "11.554599", address["latitude"])
	assert.Equal(t, "75.632668", address["longitude"])
}

func TestPickupOrderParams_DoesNotUsePhoneAsEmail(t *testing.T) {
	params := tiqrecommerce.PickupOrderParams(map[string]any{
		"customer_email": "9846435358",
		"customer_phone": "9846435358",
		"phone_number":   "919846435358",
		"email":          "buyer@example.com",
	})
	assert.Equal(t, "buyer@example.com", params["email"])
	assert.Equal(t, "9846435358", params["phone_number"])
	assert.NotContains(t, params, "new_address")
}

func TestOrderItemsForAPI_MergesAndSkipsInvalidQty(t *testing.T) {
	t.Parallel()
	items := tiqrecommerce.OrderItemsForAPI([]any{
		map[string]any{"product_option": "9", "quantity": "2"},
		map[string]any{"product_option": "9", "quantity": "3"},
		map[string]any{"product_option": "8", "quantity": "0"},
		map[string]any{"product_option": "7", "quantity": "1"},
	})
	require.Len(t, items, 2)
	assert.Equal(t, "9", items[0]["product_option"])
	assert.Equal(t, "5", items[0]["quantity"])
	assert.Equal(t, "7", items[1]["product_option"])
	assert.Equal(t, "1", items[1]["quantity"])
}

func TestTiqrCartUpsertAndUnitCount(t *testing.T) {
	t.Parallel()
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"product_name": "Kunafa Cake",
		"option_name":  "Kunafa",
		"options": []any{
			map[string]any{"id": "9", "name": "Kunafa", "price": 40.0},
		},
	}}
	c := codedflow.NewConv(nil, newCodedChat(&chatNodeCtx{session: session}))

	tiqrecommerce.UpsertCartItem(tiqrecommerce.Wrap(c), "9", "2", nil)
	assert.Equal(t, 1, tiqrecommerce.CartLen(tiqrecommerce.Wrap(c)))
	assert.Equal(t, 2, tiqrecommerce.CartUnitCount(tiqrecommerce.Wrap(c)))
	assert.Equal(t, float64(2), session.SessionData["cart_count"])

	tiqrecommerce.UpsertCartItem(tiqrecommerce.Wrap(c), "9", "3", nil)
	assert.Equal(t, 1, tiqrecommerce.CartLen(tiqrecommerce.Wrap(c)))
	assert.Equal(t, 5, tiqrecommerce.CartUnitCount(tiqrecommerce.Wrap(c)))

	tiqrecommerce.UpsertCartItem(tiqrecommerce.Wrap(c), "", "1", nil) // empty option ignored
	assert.Equal(t, 1, tiqrecommerce.CartLen(tiqrecommerce.Wrap(c)))
	tiqrecommerce.UpsertCartItem(tiqrecommerce.Wrap(c), "8", "0", nil) // qty 0 ignored
	assert.Equal(t, 1, tiqrecommerce.CartLen(tiqrecommerce.Wrap(c)))

	summary := tiqrecommerce.FormatTiqrCartSummary(tiqrecommerce.Wrap(c))
	assert.Contains(t, summary, "1. *Kunafa Cake(Kunafa)* x5")
	assert.Contains(t, summary, "₹200.00")
	assert.NotContains(t, summary, "each")
	assert.Contains(t, summary, "*Subtotal:* ₹200.00")

	ok, name := tiqrecommerce.RemoveTiqrCartLine(tiqrecommerce.Wrap(c), "9")
	require.True(t, ok)
	assert.Equal(t, "Kunafa", name)
	assert.Equal(t, 0, tiqrecommerce.CartLen(tiqrecommerce.Wrap(c)))
	assert.Equal(t, 0, tiqrecommerce.CartUnitCount(tiqrecommerce.Wrap(c)))
}

func TestRequiredCaptureFieldsFromCollection(t *testing.T) {
	t.Parallel()
	fields := tiqrecommerce.RequiredCaptureFieldsFrom(map[string]any{
		"required_capture_fields": []any{
			map[string]any{"key": "writing", "label": "Cake writing", "type": "text", "required": true, "help_text": "Short message"},
			map[string]any{"key": "optional_note", "label": "Note", "type": "text", "required": false},
			map[string]any{"key": "", "label": "Broken", "type": "text", "required": true},
			map[string]any{"key": "flavor", "label": "Flavor", "type": "single_select", "required": "true", "options": []any{"Vanilla", "Chocolate"}},
		},
	})
	require.Len(t, fields, 2)
	assert.Equal(t, "writing", fields[0]["key"])
	assert.Equal(t, "Short message", fields[0]["help_text"])
	assert.Equal(t, "flavor", fields[1]["key"])
	assert.Equal(t, []any{"Vanilla", "Chocolate"}, fields[1]["options"])
}

func TestCurrentCollectionCaptureFieldsPrefersProductCategory(t *testing.T) {
	t.Parallel()
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"collection_id": "57",
		"product_id":    "101",
		"products": []any{map[string]any{
			"id":          "101",
			"category_id": "58",
		}},
		"collections": []any{
			map[string]any{
				"id": "57",
				"required_capture_fields": []any{
					map[string]any{"key": "writing", "label": "Cake writing", "type": "text", "required": true},
				},
			},
			map[string]any{
				"id": "58",
				"required_capture_fields": []any{
					map[string]any{"key": "message", "label": "Card message", "type": "text", "required": true},
				},
			},
		},
	}}
	c := codedflow.NewConv(nil, newCodedChat(&chatNodeCtx{session: session}))
	fields := tiqrecommerce.CurrentCollectionCaptureFields(tiqrecommerce.Wrap(c))
	require.Len(t, fields, 1)
	assert.Equal(t, "message", fields[0]["key"])

	delete(session.SessionData["products"].([]any)[0].(map[string]any), "category_id")
	fields = tiqrecommerce.CurrentCollectionCaptureFields(tiqrecommerce.Wrap(c))
	require.Len(t, fields, 1)
	assert.Equal(t, "writing", fields[0]["key"])
}

func TestCodedOrderNotesIncludesCaptureAnswers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Note", tiqrecommerce.CodedOrderNotes(map[string]any{"customer_notes": "Note"}))
	assert.Equal(t, "", tiqrecommerce.CodedOrderNotes(map[string]any{}))

	notes := tiqrecommerce.CodedOrderNotes(map[string]any{
		"customer_notes": "Leave at gate",
		"tiqr_cart": []any{map[string]any{
			"product_option": "9",
			"product_name":   "Normal Cake",
			"option_name":    "Vancho",
			"quantity":       "1",
			"capture_fields": map[string]any{"writing": "Happy Birthday Aswin", "delivery_date": "12/05/2026, 03 PM"},
			"capture_labels": map[string]any{"writing": "Writing on Cake", "delivery_date": "Delivery Date"},
			"capture_order":  []any{"writing", "delivery_date"},
		}},
	})
	assert.Equal(t, "Normal Cake(Vancho)\n- Writing on Cake: Happy Birthday Aswin\n- Delivery Date: 12/05/2026, 03 PM\n\nNote: Leave at gate", notes)

	notes = tiqrecommerce.CodedOrderNotes(map[string]any{
		"tiqr_cart": []any{
			map[string]any{
				"product_option": "9",
				"product_name":   "Normal Cake",
				"option_name":    "Regular",
				"capture_fields": map[string]any{"writing": "Happy birthday"},
				"capture_labels": map[string]any{"writing": "Writing on Cake"},
				"capture_order":  []any{"writing"},
			},
			map[string]any{
				"product_option": "10",
				"product_name":   "Normal Cake",
				"option_name":    "Large",
				"capture_fields": map[string]any{"writing": "Congrats"},
				"capture_labels": map[string]any{"writing": "Writing on Cake"},
				"capture_order":  []any{"writing"},
			},
		},
	})
	assert.Equal(t, "Normal Cake(Regular)\n- Writing on Cake: Happy birthday\n\nNormal Cake(Large)\n- Writing on Cake: Congrats", notes)

	notes = tiqrecommerce.CodedOrderNotes(map[string]any{
		"tiqr_cart": []any{map[string]any{
			"product_option": "9",
			"option_name":    "Vancho",
			"capture_fields": map[string]any{"writing": "Happy birthday"},
		}},
	})
	assert.Equal(t, "Vancho\n- writing: Happy birthday", notes)

	notes = tiqrecommerce.CodedOrderNotes(map[string]any{
		"customer_notes": "Leave at gate",
		tiqrecommerce.HandoffCartKey: map[string]any{
			"source": "tiqr_cart",
			"lines": []any{map[string]any{
				"product_option": "9",
				"product_name":   "Themed Cake",
				"option_name":    "Regular",
				"capture_fields": map[string]any{"writing": "Happy Birthday"},
				"capture_labels": map[string]any{"writing": "Cake writing"},
				"capture_order":  []any{"writing"},
			}},
		},
	})
	assert.Equal(t, "Themed Cake(Regular)\n- Cake writing: Happy Birthday\n\nNote: Leave at gate", notes)
}

func TestTiqrCartKeepsDistinctCaptureAnswers(t *testing.T) {
	t.Parallel()
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"option_name":             "Kunafa",
		"commerce_capture_labels": map[string]any{"writing": "Cake writing"},
		"options": []any{
			map[string]any{"id": "9", "name": "Kunafa", "price": 40.0},
		},
	}}
	c := codedflow.NewConv(nil, newCodedChat(&chatNodeCtx{session: session}))
	tiqrecommerce.UpsertCartItem(tiqrecommerce.Wrap(c), "9", "1", map[string]any{"writing": "Happy birthday"})
	tiqrecommerce.UpsertCartItem(tiqrecommerce.Wrap(c), "9", "1", map[string]any{"writing": "Congrats"})
	assert.Equal(t, 2, tiqrecommerce.CartLen(tiqrecommerce.Wrap(c)))
	summary := tiqrecommerce.FormatTiqrCartSummary(tiqrecommerce.Wrap(c))
	assert.Contains(t, summary, "Cake writing: Happy birthday")
	assert.Contains(t, summary, "Cake writing: Congrats")

	tiqrecommerce.UpsertCartItem(tiqrecommerce.Wrap(c), "9", "2", map[string]any{"writing": "Happy birthday"})
	assert.Equal(t, 2, tiqrecommerce.CartLen(tiqrecommerce.Wrap(c)))
	assert.Equal(t, 4, tiqrecommerce.CartUnitCount(tiqrecommerce.Wrap(c)))
}

func TestTiqrCartSetQty(t *testing.T) {
	t.Parallel()
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"tiqr_cart": []any{
			map[string]any{"product_option": "9", "quantity": "2", "option_name": "Kunafa", "price": 40.0},
		},
	}}
	c := codedflow.NewConv(nil, newCodedChat(&chatNodeCtx{session: session}))
	require.True(t, tiqrecommerce.SetTiqrCartLineQty(tiqrecommerce.Wrap(c), "9", 4))
	assert.Equal(t, 4, tiqrecommerce.CartUnitCount(tiqrecommerce.Wrap(c)))
	assert.False(t, tiqrecommerce.SetTiqrCartLineQty(tiqrecommerce.Wrap(c), "9", 0))
	assert.Equal(t, 4, tiqrecommerce.CartUnitCount(tiqrecommerce.Wrap(c)))
}

func TestParseTiqrCartEdit(t *testing.T) {
	t.Parallel()
	lines := []tiqrecommerce.TiqrCartLine{
		{OptionID: "9", Name: "Kunafa", Qty: 2},
		{OptionID: "8", Name: "Chocolate Large", Qty: 1},
	}

	got := tiqrecommerce.ParseTiqrCartEdit("remove item 1", lines)
	assert.Equal(t, "remove", got.Action)
	assert.Equal(t, 1, got.Index)
	assert.False(t, got.Incomplete)

	got = tiqrecommerce.ParseTiqrCartEdit("remove Kunafa", lines)
	assert.Equal(t, "remove", got.Action)
	assert.Equal(t, 1, got.Index)

	got = tiqrecommerce.ParseTiqrCartEdit("change item 2 to 1", lines)
	assert.Equal(t, "set_qty", got.Action)
	assert.Equal(t, 2, got.Index)
	assert.Equal(t, 1, got.Qty)

	got = tiqrecommerce.ParseTiqrCartEdit("reduce Chocolate to 1", lines)
	assert.Equal(t, "set_qty", got.Action)
	assert.Equal(t, 2, got.Index)
	assert.Equal(t, 1, got.Qty)

	got = tiqrecommerce.ParseTiqrCartEdit("Chocolate Large to 3", lines)
	assert.Equal(t, "set_qty", got.Action)
	assert.Equal(t, 2, got.Index)
	assert.Equal(t, 3, got.Qty)

	got = tiqrecommerce.ParseTiqrCartEdit("remove", lines)
	assert.Equal(t, "remove", got.Action)
	assert.True(t, got.Incomplete)

	got = tiqrecommerce.ParseTiqrCartEdit("Kunafa", lines)
	assert.Equal(t, 1, got.Index)
	assert.True(t, got.Incomplete)
	assert.Empty(t, got.Action)
}

func TestParseTiqrCartEdit_AmbiguousName(t *testing.T) {
	t.Parallel()
	lines := []tiqrecommerce.TiqrCartLine{
		{OptionID: "1", Name: "Chocolate Small", Qty: 1},
		{OptionID: "2", Name: "Chocolate Large", Qty: 1},
	}
	got := tiqrecommerce.ParseTiqrCartEdit("remove Chocolate", lines)
	assert.Equal(t, "remove", got.Action)
	assert.True(t, got.Ambiguous)
	assert.True(t, got.Incomplete)
}

func TestFormatTiqrCartSummaryNumbered(t *testing.T) {
	t.Parallel()
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"tiqr_cart": []any{
			map[string]any{
				"product_option": "9",
				"quantity":       "2",
				"product_name":   "Kunafa Cake",
				"option_name":    "Kunafa",
				"price":          40.0,
			},
			map[string]any{
				"product_option": "8",
				"quantity":       "1",
				"product_name":   "Chocolate Cake",
				"option_name":    "Chocolate",
				"price":          50.0,
			},
		},
	}}
	c := codedflow.NewConv(nil, newCodedChat(&chatNodeCtx{session: session}))
	summary := tiqrecommerce.FormatTiqrCartSummary(tiqrecommerce.Wrap(c))
	assert.Contains(t, summary, "1. *Kunafa Cake(Kunafa)* x2 — ₹80.00")
	assert.Contains(t, summary, "2. *Chocolate Cake(Chocolate)* x1 — ₹50.00")
	assert.Contains(t, summary, "*Subtotal:* ₹130.00")
	assert.NotContains(t, summary, "each")
	assert.Less(t, strings.Index(summary, "1. *Kunafa Cake(Kunafa)*"), strings.Index(summary, "2. *Chocolate Cake(Chocolate)*"))

	// Older cart lines without product_name keep option-only labels.
	session.SessionData["tiqr_cart"] = []any{
		map[string]any{"product_option": "9", "quantity": "2", "option_name": "Kunafa", "price": 40.0},
	}
	summary = tiqrecommerce.FormatTiqrCartSummary(tiqrecommerce.Wrap(c))
	assert.Contains(t, summary, "1. *Kunafa* x2 — ₹80.00")
	assert.NotContains(t, summary, "Kunafa Cake")
}

func useStoreREST(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := newTiqrStoreRESTClient
	newTiqrStoreRESTClient = func(baseURL string) *ticker.Client {
		return ticker.NewClient(baseURL, srv.Client())
	}
	t.Cleanup(func() { newTiqrStoreRESTClient = prev })
}

func enableTiqrEcommerce(t *testing.T, app *App, orgID uuid.UUID, accountName, keyword string, enabled bool) {
	t.Helper()
	binding := models.CodedFlowBinding{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  orgID,
		WhatsAppAccount: accountName,
		FlowKey:         tiqrecommerce.FlowKey,
		Keywords:        models.StringArray{keyword},
		IsEnabled:       enabled,
	}
	require.NoError(t, app.DB.Create(&binding).Error)
	if !enabled {
		require.NoError(t, app.DB.Model(&binding).Update("is_enabled", false).Error)
	}
}

func useCodedTranslate(t *testing.T, translate func(lang, text string) (string, error)) {
	t.Helper()
	prev := codedflow.TranslateCodedLine
	codedflow.TranslateCodedLine = func(_ codedflow.Host, _ *models.ChatbotSession, lang, text string) (string, error) {
		if translate == nil {
			t.Errorf("translated %q", text)
			return text, nil
		}
		return translate(lang, text)
	}
	t.Cleanup(func() { codedflow.TranslateCodedLine = prev })
}

func useCodedIntent(t *testing.T, intent func(string, codedflow.IntentContext) (codedflow.IntentResult, error), guide func(string) (string, error)) {
	t.Helper()
	prevIntent, prevGuide := codedflow.IdentifyCodedIntent, codedflow.GuideCodedIntent
	codedflow.IdentifyCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, message string, ctx codedflow.IntentContext) (codedflow.IntentResult, error) {
		if intent == nil {
			t.Fatal("intent identifier was called")
		}
		return intent(message, ctx)
	}
	codedflow.GuideCodedIntent = func(_ codedflow.Host, _ *models.ChatbotSession, message, _ string, _ codedflow.IntentContext) (string, error) {
		if guide == nil {
			t.Fatal("guide was called")
		}
		return guide(message)
	}
	t.Cleanup(func() {
		codedflow.IdentifyCodedIntent = prevIntent
		codedflow.GuideCodedIntent = prevGuide
	})
}

func useCodedRecover(t *testing.T, recover func(codedflow.RecoverContext) (codedflow.RecoverResult, error)) {
	t.Helper()
	prev := codedflow.RecoverCodedFailure
	codedflow.RecoverCodedFailure = func(_ codedflow.Host, _ *models.ChatbotSession, ctx codedflow.RecoverContext) (codedflow.RecoverResult, error) {
		if recover == nil {
			t.Fatal("recover was called")
		}
		return recover(ctx)
	}
	t.Cleanup(func() { codedflow.RecoverCodedFailure = prev })
}

func outgoingBlob(t *testing.T, app *App, session *models.ChatbotSession) string {
	t.Helper()
	var b strings.Builder
	var logs []models.ChatbotSessionMessage
	require.NoError(t, app.DB.Where("session_id = ?", session.ID).Find(&logs).Error)
	for _, msg := range logs {
		b.WriteString(msg.Message)
		b.WriteByte('\n')
	}
	var msgs []models.Message
	require.NoError(t, app.DB.Where("contact_id = ?", session.ContactID).Find(&msgs).Error)
	for _, msg := range msgs {
		b.WriteString(msg.Content)
		b.WriteByte('\n')
		if msg.InteractiveData != nil {
			raw, err := json.Marshal(msg.InteractiveData)
			require.NoError(t, err)
			b.Write(raw)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func reloadSession(t *testing.T, app *App, session *models.ChatbotSession) {
	t.Helper()
	require.NoError(t, app.DB.First(session, session.ID).Error)
}

type storeCounts struct {
	store       int
	collections int
	products    int
	searches    int
	lastSearch  string
}

func newStoreServer(t *testing.T, products []any, counts *storeCounts) *httptest.Server {
	t.Helper()
	return newStoreServerWith(t, products, counts, map[string]any{"id": 42, "name": "Demo"})
}

func newStoreServerWith(t *testing.T, products []any, counts *storeCounts, store map[string]any) *httptest.Server {
	t.Helper()
	if store == nil {
		store = map[string]any{"id": 42, "name": "Demo"}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			if counts != nil {
				counts.collections++
			}
			results := []any{
				map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				map[string]any{"id": "58", "name": "Cakes", "description": "Celebration cakes"},
			}
			if extra, ok := store["test_collections"].([]any); ok && len(extra) > 0 {
				results = extra
			}
			_ = json.NewEncoder(w).Encode(pageStoreResults(r, results, testPageSize(store)))
		case strings.Contains(r.URL.Path, "/product/"):
			if counts != nil {
				counts.products++
				if q := r.URL.Query().Get("search"); q != "" {
					counts.searches++
					counts.lastSearch = q
				}
			}
			// Detail: /service/buyer/product/{id}/ — return one product object.
			path := strings.TrimSuffix(r.URL.Path, "/")
			parts := strings.Split(path, "/")
			if len(parts) > 0 {
				last := parts[len(parts)-1]
				if last != "product" && r.URL.Query().Get("search") == "" &&
					!strings.Contains(r.URL.RawQuery, "category_id") &&
					r.Method == http.MethodGet {
					if _, err := strconv.Atoi(last); err == nil {
						for _, product := range products {
							item, ok := product.(map[string]any)
							if !ok {
								continue
							}
							if fieldString(item, "id") == last || asString(item["id"]) == last {
								_ = json.NewEncoder(w).Encode(item)
								return
							}
						}
						if len(products) == 1 {
							_ = json.NewEncoder(w).Encode(products[0])
							return
						}
						http.NotFound(w, r)
						return
					}
				}
			}
			_ = json.NewEncoder(w).Encode(pageStoreResults(r, products, testPageSize(store)))
		default:
			if counts != nil {
				counts.store++
			}
			payload := map[string]any{}
			for key, value := range store {
				if key == "test_collections" || key == "test_orders" || key == "test_page_size" {
					continue
				}
				payload[key] = value
			}
			_ = json.NewEncoder(w).Encode(payload)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testPageSize(store map[string]any) int {
	switch n := store["test_page_size"].(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// pageStoreResults returns one REST page. pageSize caps the response the way
// a store API limit does, and sets next when count is larger than the page.
func pageStoreResults(r *http.Request, results []any, pageSize int) map[string]any {
	if pageSize <= 0 {
		return map[string]any{"count": len(results), "results": results}
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			offset = n
		}
	}
	if offset > len(results) {
		offset = len(results)
	}
	end := offset + pageSize
	if end > len(results) {
		end = len(results)
	}
	out := map[string]any{
		"count":   len(results),
		"results": results[offset:end],
	}
	if end < len(results) {
		q := r.URL.Query()
		q.Set("limit", strconv.Itoa(pageSize))
		q.Set("offset", strconv.Itoa(end))
		out["next"] = "http://" + r.Host + r.URL.Path
		if encoded := q.Encode(); encoded != "" {
			out["next"] = out["next"].(string) + "?" + encoded
		}
	}
	return out
}

func lastListRowTitles(t *testing.T, app *App, session *models.ChatbotSession) []string {
	t.Helper()
	var msgs []models.Message
	require.NoError(t, app.DB.Where("contact_id = ?", session.ContactID).Order("created_at asc").Find(&msgs).Error)
	var titles []string
	for _, msg := range msgs {
		if msg.InteractiveData == nil {
			continue
		}
		raw, err := json.Marshal(msg.InteractiveData)
		require.NoError(t, err)
		var parsed struct {
			Type string `json:"type"`
			Rows []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"rows"`
		}
		require.NoError(t, json.Unmarshal(raw, &parsed))
		if parsed.Type != "list" || len(parsed.Rows) == 0 {
			continue
		}
		titles = make([]string, 0, len(parsed.Rows))
		for _, row := range parsed.Rows {
			titles = append(titles, row.Title)
		}
	}
	return titles
}

func lastCarouselButtonIDs(t *testing.T, app *App, session *models.ChatbotSession) []string {
	t.Helper()
	var msgs []models.Message
	require.NoError(t, app.DB.Where("contact_id = ?", session.ContactID).Order("created_at asc").Find(&msgs).Error)
	var ids []string
	for _, msg := range msgs {
		if msg.InteractiveData == nil {
			continue
		}
		raw, err := json.Marshal(msg.InteractiveData)
		require.NoError(t, err)
		var parsed struct {
			Type  string `json:"type"`
			Cards []struct {
				Buttons []struct {
					ID string `json:"id"`
				} `json:"buttons"`
			} `json:"cards"`
		}
		require.NoError(t, json.Unmarshal(raw, &parsed))
		if parsed.Type != "carousel" || len(parsed.Cards) == 0 {
			continue
		}
		ids = make([]string, 0, len(parsed.Cards))
		for _, card := range parsed.Cards {
			if len(card.Buttons) == 0 {
				ids = append(ids, "")
				continue
			}
			ids = append(ids, card.Buttons[0].ID)
		}
	}
	return ids
}

func startEcommerce(t *testing.T, products []any, counts *storeCounts) (*App, *models.WhatsAppAccount, *models.Contact, *models.ChatbotSession) {
	t.Helper()
	return startEcommerceWithStore(t, products, counts, map[string]any{"id": 42, "name": "Demo"}, nil)
}

func startEcommerceWithStore(
	t *testing.T,
	products []any,
	counts *storeCounts,
	store map[string]any,
	extraAI *models.AIConfig,
) (*App, *models.WhatsAppAccount, *models.Contact, *models.ChatbotSession) {
	t.Helper()
	srv := newStoreServerWith(t, products, counts, store)
	useStoreREST(t, srv)
	app, org, account, contact, session := newGraphTestFixtures(t)
	ai := models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	}
	if extraAI != nil {
		if extraAI.CommerceEnabled {
			ai.CommerceEnabled = true
		}
		if extraAI.CommerceMCPURL != "" {
			ai.CommerceMCPURL = extraAI.CommerceMCPURL
		}
		if extraAI.CommerceMCPAPIKey != "" {
			ai.CommerceMCPAPIKey = extraAI.CommerceMCPAPIKey
		}
		if extraAI.CommerceRESTURL != "" {
			ai.CommerceRESTURL = extraAI.CommerceRESTURL
		}
		if extraAI.CommerceStoreID != "" {
			ai.CommerceStoreID = extraAI.CommerceStoreID
		}
	}
	createChatbotSettings(t, app, org.ID, account.Name, ai)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NotNil(t, flow)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.Equal(t, "intent", session.CurrentStep)
	return app, account, contact, session
}

func twoProducts(options []any) []any {
	return []any{
		map[string]any{
			"id": "101", "name": "Kunafa", "min_price": "120", "options": options,
		},
		map[string]any{
			"id": "102", "name": "Canape", "min_price": "80",
			"options": []any{map[string]any{"id": "1", "name": "Default", "price": "10"}},
		},
	}
}

func TestSingleProductCTA_Config(t *testing.T) {
	prompt := tiqrecommerce.SingleProductCTA()
	assert.Contains(t, prompt.Body, "{{products[0].name}}")
	assert.Contains(t, prompt.Body, "{{products[0].description}}")
	assert.Equal(t, "{{products[0].images[0].original_url}}", prompt.HeaderImage)
	assert.Equal(t, "Add to cart", prompt.Title)
	assert.Equal(t, "product_selected", prompt.StoreAs)
	assert.Equal(t, "{{name}} ({{currency_symbol}}{{min_price}})", prompt.BodyField)
	assert.Equal(t, tiqrecommerce.FallbackMedia, prompt.FallbackMedia)
	assert.Equal(t, "name", prompt.Select["product_name"])
	assert.Equal(t, "id", prompt.Select["product_id"])

	c := codedflow.NewConv(nil, newCodedChat(&chatNodeCtx{session: &models.ChatbotSession{
		SessionData: models.JSONB{codedflow.CustomerLanguageKey: "en"},
	}}))
	cfg := c.ImageButtonConfig(prompt)
	assert.Equal(t, "reply", cfg["mode"])
	assert.Equal(t, "dynamic", cfg["source"])
	assert.Equal(t, "products", cfg["items_var"])
	assert.Equal(t, prompt.HeaderImage, cfg["header_image"])
	assert.Equal(t, tiqrecommerce.FallbackMedia, cfg["fallback_media_url"])
	assert.Equal(t, "Add to cart", cfg["title_field"])
	assert.Equal(t, prompt.BodyField, cfg["body_field"])
}

func TestTiqrEcommerce_SingleProductImageCTA(t *testing.T) {
	product := []any{
		map[string]any{
			"id":          "101",
			"name":        "Kunafa",
			"min_price":   "120",
			"description": "Crispy shredded pastry",
			"images": []any{
				map[string]any{"original_url": "https://cdn.example.com/kunafa.jpg"},
			},
			"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
		},
	}
	app, account, contact, session := startEcommerce(t, product, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.Equal(t, "product", session.CurrentStep)

	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Kunafa")
	assert.Contains(t, blob, "120")
	assert.Contains(t, blob, "Crispy shredded pastry")
	assert.Contains(t, blob, "Add to cart")
	assert.Contains(t, blob, "https://cdn.example.com/kunafa.jpg")
	assert.NotContains(t, blob, `"type":"carousel"`)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add to cart", "101", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "quantity", session.CurrentStep)
	assert.Equal(t, "101", session.SessionData["product_id"])
	assert.Equal(t, "9", session.SessionData["option_id"])
	_ = contact
}

func TestMatchCodedFlowTrigger_ContainsAndIgnoresEmpty(t *testing.T) {
	app, org, account, _, _ := newGraphTestFixtures(t)
	enableTiqrEcommerce(t, app, org.ID, account.Name, "shop", true)

	flow := app.matchCodedFlowTrigger(org.ID, account.Name, "I want to Shop")
	require.NotNil(t, flow)
	assert.Equal(t, tiqrecommerce.FlowKey, flow.Key)
	assert.Equal(t, "TiQR Ecommerce", flow.Name)

	assert.Nil(t, app.matchCodedFlowTrigger(org.ID, account.Name, "hello"))

	empty := models.CodedFlowBinding{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  org.ID,
		WhatsAppAccount: account.Name + "-other",
		FlowKey:         tiqrecommerce.FlowKey,
		Keywords:        models.StringArray{""},
		IsEnabled:       true,
	}
	require.NoError(t, app.DB.Create(&empty).Error)
	assert.Nil(t, app.matchCodedFlowTrigger(org.ID, account.Name+"-other", "anything"))
}

func TestMatchCodedFlowTrigger_DisabledDoesNotMatch(t *testing.T) {
	app, org, account, _, _ := newGraphTestFixtures(t)
	enableTiqrEcommerce(t, app, org.ID, account.Name, "shop", false)
	assert.Nil(t, app.matchCodedFlowTrigger(org.ID, account.Name, "shop"))
}

func TestTiqrEcommerce_KeywordDoesNotSelectMenu(t *testing.T) {
	var counts storeCounts
	app, account, _, session := startEcommerce(t, twoProducts(nil), &counts)
	assert.Equal(t, models.SessionStatusActive, session.Status)
	assert.Equal(t, tiqrecommerce.FlowKey, session.SessionData[codedflow.DataKey])
	assert.Nil(t, session.SessionData["collection_id"])
	_, ok := session.SessionData["collections"]
	assert.True(t, ok)
	assert.Equal(t, 1, counts.store)
	assert.Equal(t, 1, counts.collections)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Welcome to Demo")
	assert.Contains(t, blob, "buy_products")
	assert.Contains(t, blob, "check_order_status")
	assert.Contains(t, blob, "talk_to_agent")
	_ = account
}

func TestTiqrEcommerce_MissingCommerceEnds(t *testing.T) {
	app, _, account, contact, session := newGraphTestFixtures(t)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NotNil(t, flow)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)

	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Empty(t, session.CurrentStep)
	assert.Nil(t, session.SessionData[codedflow.DataKey])
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "business information")
	assert.Contains(t, blob, "connecting you with a team member")
	assert.NotContains(t, strings.ToLower(blob), "fetch the store")
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_OptionBranches(t *testing.T) {
	cases := []struct {
		name     string
		options  []any
		wantStep string
	}{
		{name: "none", options: []any{}, wantStep: "next"},
		{
			name:     "one",
			options:  []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
			wantStep: "quantity",
		},
		{
			name: "many",
			options: []any{
				map[string]any{"id": "9", "name": "Regular", "price": "40"},
				map[string]any{"id": "10", "name": "Large", "price": "60"},
			},
			wantStep: "option",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, account, contact, session := startEcommerce(t, twoProducts(tc.options), nil)
			flow := codedflow.ByKey(tiqrecommerce.FlowKey)
			require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
			reloadSession(t, app, session)
			require.Equal(t, "collection", session.CurrentStep)

			require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
			reloadSession(t, app, session)
			require.Equal(t, "product", session.CurrentStep)

			require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
			reloadSession(t, app, session)
			assert.Equal(t, tc.wantStep, session.CurrentStep)
			if tc.name == "one" {
				assert.Equal(t, "9", session.SessionData["option_id"])
				assert.Equal(t, "Regular", session.SessionData["option_name"])
			}
			if tc.name == "none" {
				assert.Contains(t, outgoingBlob(t, app, session), "isn't available")
			}
		})
	}
}

// Regression: ticker product/option payloads decode ids as float64.
// Browse → pick product → pick option → quantity must still resolve.
func TestTiqrEcommerce_NumericProductAndOptionIDs(t *testing.T) {
	products := []any{
		map[string]any{
			"id": float64(2970), "name": "Normal Cakes", "min_price": float64(1),
			"options": []any{
				map[string]any{"id": float64(3319), "name": "Vancho", "price": float64(1)},
				map[string]any{"id": float64(3320), "name": "Chocolate", "price": float64(1)},
			},
		},
	}
	store := map[string]any{
		"id":   42,
		"name": "Demo",
		"test_collections": []any{
			map[string]any{"id": float64(71), "name": "Normal Cakes", "description": "Cakes"},
		},
	}
	app, account, contact, session := startEcommerceWithStore(t, products, nil, store, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Normal Cakes", "71", nil))
	reloadSession(t, app, session)
	require.Equal(t, "product", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add to cart", "2970", nil))
	reloadSession(t, app, session)
	require.Equal(t, "option", session.CurrentStep)
	assert.Equal(t, "2970", asString(session.SessionData["product_id"]))

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Vancho", "3319", nil))
	reloadSession(t, app, session)
	require.Equal(t, "quantity", session.CurrentStep)
	assert.Equal(t, "3319", asString(session.SessionData["option_id"]))
	assert.Equal(t, "Vancho", asString(session.SessionData["option_name"]))

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	cart, ok := session.SessionData["tiqr_cart"].([]any)
	require.True(t, ok)
	require.Len(t, cart, 1)
	line, ok := cart[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "3319", asString(line["product_option"]))
	assert.Equal(t, "Vancho", asString(line["option_name"]))
	_ = contact
}

func TestTiqrEcommerce_AppendsCartAndAddMoreSkipsCollections(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{Language: "en", Route: codedflow.RouteUnclear, Confidence: 0.4}, nil
	}, func(string) (string, error) {
		return "Send a whole number.", nil
	})
	var counts storeCounts
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerce(t, products, &counts)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.Equal(t, "quantity", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "nope", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "quantity", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "Send a whole number.")
	assert.Equal(t, models.SessionStatusActive, session.Status)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "2", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "next", session.CurrentStep)
	assert.Equal(t, float64(2), session.SessionData["cart_count"])

	cart, ok := session.SessionData["tiqr_cart"].([]any)
	require.True(t, ok)
	require.Len(t, cart, 1)
	item, ok := cart[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "9", item["product_option"])
	assert.Equal(t, "2", item["quantity"])
	assert.Contains(t, asString(session.SessionData["cart_summary"]), "Your cart")
	assert.Contains(t, outgoingBlob(t, app, session), "Edit cart")
	assert.Contains(t, outgoingBlob(t, app, session), "Checkout")

	before := counts.collections
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add more items", tiqrecommerce.AddMore, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, before, counts.collections)
	assert.Equal(t, 1, counts.store)
}

func TestTiqrEcommerce_AsksCollectionCaptureBeforeCart(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	store := map[string]any{
		"id":   42,
		"name": "Demo",
		"test_collections": []any{
			map[string]any{
				"id":          "57",
				"name":        "Sweets",
				"description": "Desserts",
				"required_capture_fields": []any{
					map[string]any{
						"key": "writing", "label": "Cake writing", "type": "text", "required": true,
						"help_text": "Short message for the cake",
					},
					map[string]any{
						"key": "flavor", "label": "Flavor", "type": "single_select", "required": true,
						"options": []any{"Vanilla", "Chocolate"},
					},
					map[string]any{"key": "optional_note", "label": "Note", "type": "text", "required": false},
				},
			},
			map[string]any{"id": "58", "name": "Cakes", "description": "Celebration cakes"},
		},
	}
	app, account, contact, session := startEcommerceWithStore(t, products, nil, store, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.Equal(t, "quantity", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_0_writing", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Cake writing")
	assert.Contains(t, blob, "Short message for the cake")
	_, hasCart := session.SessionData["tiqr_cart"]
	assert.False(t, hasCart)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Happy birthday", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_1_flavor", session.CurrentStep)
	blob = outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Flavor")
	assert.Contains(t, blob, "Vanilla")
	assert.Contains(t, blob, "Chocolate")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Strawberry", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_1_flavor", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "Please provide a valid value.")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Chocolate", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "next", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "added to your cart")
	assert.Contains(t, asString(session.SessionData["cart_summary"]), "Cake writing: Happy birthday")
	assert.Contains(t, asString(session.SessionData["cart_summary"]), "Flavor: Chocolate")

	cart, ok := session.SessionData["tiqr_cart"].([]any)
	require.True(t, ok)
	require.Len(t, cart, 1)
	item, ok := cart[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "9", item["product_option"])
	assert.Equal(t, "1", item["quantity"])
	assert.Equal(t, "Kunafa", item["product_name"])
	assert.Equal(t, "Regular", item["option_name"])
	fields, ok := item["capture_fields"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Happy birthday", fields["writing"])
	assert.Equal(t, "Chocolate", fields["flavor"])
	assert.NotContains(t, fields, "optional_note")
	order, ok := item["capture_order"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"writing", "flavor"}, order)

	captured, ok := session.SessionData["commerce_captured_fields"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Happy birthday", captured["writing"])
	assert.Equal(t, "Chocolate", captured["flavor"])

	session.SessionData["customer_notes"] = "Leave at gate"
	assert.Equal(t,
		"Kunafa(Regular)\n- Cake writing: Happy birthday\n- Flavor: Chocolate\n\nNote: Leave at gate",
		tiqrecommerce.PickupOrderParams(session.SessionData)["notes"],
	)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Add more items", tiqrecommerce.AddMore, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_0_writing", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "Cake writing")
}

func TestTiqrEcommerce_EarlyHandoffSkipsProductsAndOrders(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	store := map[string]any{
		"id":   42,
		"name": "Demo",
		"test_collections": []any{
			map[string]any{
				"id":              "57",
				"name":            "Custom Cakes",
				"description":     "Made to order",
				"handoff_policy":  "after_capture",
				"handoff_message": "A baker will review this and continue with you here.",
				"required_capture_fields": []any{
					map[string]any{
						"key": "writing", "label": "Cake writing", "type": "text", "required": true,
					},
				},
			},
			map[string]any{"id": "58", "name": "Cakes", "description": "Celebration cakes"},
		},
	}
	app, account, contact, session := startEcommerceWithStore(t, products, nil, store, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Custom Cakes", "57", nil))
	reloadSession(t, app, session)

	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "custom Custom Cakes request")
	assert.Contains(t, blob, "Cake writing")
	assert.NotContains(t, blob, "Add to cart")
	assert.NotEqual(t, "product", session.CurrentStep)
	assert.NotEqual(t, "quantity", session.CurrentStep)
	_, hasCart := session.SessionData["tiqr_cart"]
	assert.False(t, hasCart)
	_ = contact
}

// Regression: ticker list_collections returns numeric ids (JSON float64).
// collectionByID must still resolve handoff_policy after_capture.
func TestTiqrEcommerce_EarlyHandoffWithNumericCollectionID(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	store := map[string]any{
		"id":   42,
		"name": "Demo",
		"test_collections": []any{
			map[string]any{
				"id":              float64(70),
				"name":            "Themed Cakes",
				"description":     "Custom themed",
				"handoff_policy":  "after_capture",
				"handoff_message": "We will assign to our agent now. Please wait.",
				"required_capture_fields": []any{
					map[string]any{
						"key": "writing_on_cake", "label": "Writing on cake", "type": "text", "required": true,
					},
				},
			},
			map[string]any{"id": float64(71), "name": "Normal Cakes", "description": "Standard"},
		},
	}
	app, account, contact, session := startEcommerceWithStore(t, products, nil, store, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Themed Cakes", "70", nil))
	reloadSession(t, app, session)

	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "custom Themed Cakes request")
	assert.Contains(t, blob, "Writing on cake")
	assert.NotContains(t, blob, "Add to cart")
	assert.NotEqual(t, "product", session.CurrentStep)
	_, hasCart := session.SessionData["tiqr_cart"]
	assert.False(t, hasCart)
	_ = contact
}

func TestTiqrEcommerce_EarlyHandoffCompletes(t *testing.T) {
	product := []any{
		map[string]any{
			"id": "101", "name": "Themed Cake", "min_price": "500",
			"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "500"}},
		},
	}
	store := map[string]any{
		"id":   42,
		"name": "Demo",
		"test_collections": []any{
			map[string]any{
				"id":              "57",
				"name":            "Custom Cakes",
				"description":     "Made to order",
				"handoff_policy":  "after_capture",
				"handoff_message": "A baker will review this and continue with you here.",
				"required_capture_fields": []any{
					map[string]any{
						"key": "writing", "label": "Cake writing", "type": "text", "required": true,
					},
				},
			},
		},
	}
	app, account, contact, session := startEcommerceWithStore(t, product, nil, store, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Custom Cakes", "57", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_0_writing", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Happy Birthday", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "early_handoff_details", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, tiqrecommerce.TiqrEcommercePickupFlowID)
	assert.Contains(t, blob, "name, email, and phone number")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "", "", map[string]any{
		"customer_name":  "Ada",
		"customer_email": "ada@example.com",
		"customer_phone": "910000000000",
		"customer_notes": "Please call on arrival",
	}))
	reloadSession(t, app, session)

	assert.Equal(t, models.SessionStatusCancelled, session.Status)
	assert.Empty(t, session.CurrentStep)
	assert.Nil(t, session.SessionData[codedflow.DataKey])
	blob = outgoingBlob(t, app, session)
	assert.Contains(t, blob, "A baker will review this")
	_, hasCart := session.SessionData["tiqr_cart"]
	assert.False(t, hasCart)

	var transfers []models.AgentTransfer
	require.NoError(t, app.DB.Where("contact_id = ? AND source = ?", contact.ID, models.TransferSourceCommerce).Find(&transfers).Error)
	require.Len(t, transfers, 1)
	assert.Equal(t, models.TransferStatusActive, transfers[0].Status)
	require.NotNil(t, transfers[0].CommerceDraftID)

	var draft models.CommerceDraft
	require.NoError(t, app.DB.First(&draft, "id = ?", *transfers[0].CommerceDraftID).Error)
	assert.Equal(t, "transferred", draft.Status)
	assert.Equal(t, "Happy Birthday", draft.CapturedFields["writing"])
	assert.Equal(t, "Ada", draft.AddressSnapshot["name"])
	assert.Equal(t, "ada@example.com", draft.AddressSnapshot["email"])
	assert.Equal(t, "Please call on arrival", draft.Notes["customer_notes"])
	orderNotes := asString(draft.Notes["order_notes"])
	assert.Contains(t, orderNotes, "Cake writing")
	assert.Contains(t, orderNotes, "Happy Birthday")
	assert.Contains(t, orderNotes, "Themed Cake")
	assert.Contains(t, orderNotes, "Regular")
	assert.Equal(t, orderNotes, tiqrecommerce.CommerceHandoffNotesText(&draft))
	lines, ok := draft.Cart["lines"].([]any)
	require.True(t, ok)
	require.Len(t, lines, 1)
	line, ok := lines[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "9", fieldString(line, "product_option"))
	assert.Equal(t, "Regular", fieldString(line, "option_name"))
	assert.Equal(t, "Themed Cake", fieldString(line, "product_name"))
}

func TestTiqrEcommerce_UsesConfiguredPickupFlowID(t *testing.T) {
	product := []any{
		map[string]any{
			"id": "101", "name": "Themed Cake", "min_price": "500",
			"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "500"}},
		},
	}
	store := map[string]any{
		"id":   42,
		"name": "Demo",
		"test_collections": []any{
			map[string]any{
				"id":              "57",
				"name":            "Custom Cakes",
				"description":     "Made to order",
				"handoff_policy":  "after_capture",
				"handoff_message": "A baker will review this.",
				"required_capture_fields": []any{
					map[string]any{
						"key": "writing", "label": "Cake writing", "type": "text", "required": true,
					},
				},
			},
		},
	}
	const customPickupFlowID = "777666555444333"
	app, account, contact, session := startEcommerceWithStore(t, product, nil, store, nil)
	require.NoError(t, app.DB.Where(
		"organization_id = ? AND whats_app_account = ? AND flow_key = ?",
		account.OrganizationID, account.Name, tiqrecommerce.FlowKey,
	).Delete(&models.CodedFlowBinding{}).Error)
	require.NoError(t, app.DB.Create(&models.CodedFlowBinding{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  account.OrganizationID,
		WhatsAppAccount: account.Name,
		FlowKey:         tiqrecommerce.FlowKey,
		Keywords:        models.StringArray{"shop"},
		IsEnabled:       true,
		Settings: models.JSONB{
			"pickup_flow_id": customPickupFlowID,
		},
	}).Error)

	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Custom Cakes", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Happy Birthday", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "early_handoff_details", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, customPickupFlowID)
	assert.NotContains(t, blob, tiqrecommerce.TiqrEcommercePickupFlowID)
}

func TestEarlyHandoffProductOptionLineUsesChosenOption(t *testing.T) {
	line, ok := tiqrecommerce.EarlyHandoffProductOptionLine(map[string]any{
		"early_handoff_product_id": "101",
		"option_id":                "12",
		"early_handoff_products": []any{
			map[string]any{
				"id":   "101",
				"name": "Themed Cake",
				"options": []any{
					map[string]any{"id": "9", "name": "1 KG"},
					map[string]any{"id": "12", "name": "2 KG"},
				},
			},
		},
	})
	require.True(t, ok)
	assert.Equal(t, "12", line["product_option"])
	assert.Equal(t, "2 KG", line["option_name"])
	assert.Equal(t, "Themed Cake", line["product_name"])
}

func TestMigrateEarlyHandoffSessionKeys(t *testing.T) {
	session := &models.ChatbotSession{
		CurrentStep: "themed_addons_free",
		SessionData: models.JSONB{
			"themed_product_id": "101",
			"themed_timezone":   "Asia/Kolkata",
			checkoutSessionKey: map[string]any{
				"flow": "themed",
				"step": "addons",
			},
			codedflow.CallsKey: []any{
				map[string]any{"name": "themed_select_category", "ok": true},
				map[string]any{"name": "themed_addons_free", "ok": true, "var": "themed_addons_free", "value": "Skip"},
			},
		},
	}
	tiqrecommerce.MigrateEarlyHandoffSessionKeys(session)
	assert.Empty(t, session.CurrentStep)
	assert.Equal(t, "101", session.SessionData["early_handoff_product_id"])
	assert.Equal(t, "Asia/Kolkata", session.SessionData["early_handoff_timezone"])
	_, hasOld := session.SessionData["themed_product_id"]
	assert.False(t, hasOld)
	st := getCheckoutState(session)
	require.NotNil(t, st)
	assert.Equal(t, checkoutFlowEarlyHandoff, st.Flow)
	records, ok := anySlice(session.SessionData[codedflow.CallsKey])
	require.True(t, ok)
	require.Len(t, records, 2)
	first, ok := asStringMap(records[0])
	require.True(t, ok)
	assert.Equal(t, "early_handoff_select_category", first["name"])
	second, ok := asStringMap(records[1])
	require.True(t, ok)
	assert.Equal(t, "early_handoff_addons_free", second["name"])
	assert.Equal(t, "early_handoff_addons_free", second["var"])
}

func TestMigrateEarlyHandoffSessionKeysFromAfterCapture(t *testing.T) {
	session := &models.ChatbotSession{
		CurrentStep: "after_capture_addons_free",
		SessionData: models.JSONB{
			"after_capture_product_id": "101",
			"after_capture_timezone":   "Asia/Kolkata",
			checkoutSessionKey: map[string]any{
				"flow": "after_capture",
				"step": "addons",
			},
			codedflow.CallsKey: []any{
				map[string]any{"name": "after_capture_select_category", "ok": true},
				map[string]any{"name": "after_capture_addons_free", "ok": true, "var": "after_capture_addons_free", "value": "Skip"},
			},
		},
	}
	tiqrecommerce.MigrateEarlyHandoffSessionKeys(session)
	assert.Empty(t, session.CurrentStep)
	assert.Equal(t, "101", session.SessionData["early_handoff_product_id"])
	assert.Equal(t, "Asia/Kolkata", session.SessionData["early_handoff_timezone"])
	_, hasOld := session.SessionData["after_capture_product_id"]
	assert.False(t, hasOld)
	st := getCheckoutState(session)
	require.NotNil(t, st)
	assert.Equal(t, checkoutFlowEarlyHandoff, st.Flow)
	records, ok := anySlice(session.SessionData[codedflow.CallsKey])
	require.True(t, ok)
	require.Len(t, records, 2)
	first, ok := asStringMap(records[0])
	require.True(t, ok)
	assert.Equal(t, "early_handoff_select_category", first["name"])
	second, ok := asStringMap(records[1])
	require.True(t, ok)
	assert.Equal(t, "early_handoff_addons_free", second["name"])
	assert.Equal(t, "early_handoff_addons_free", second["var"])
}

func TestTiqrEcommerce_EarlyHandoffManyFieldsThenSkip(t *testing.T) {
	product := []any{
		map[string]any{
			"id": "101", "name": "Themed Cake", "min_price": "500",
			"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "500"}},
		},
	}
	store := map[string]any{
		"id":   42,
		"name": "Demo",
		"test_collections": []any{
			map[string]any{
				"id":             "57",
				"name":           "Custom Cakes",
				"description":    "Made to order",
				"handoff_policy": "after_capture",
				"required_capture_fields": []any{
					map[string]any{"key": "writing_on_cake", "label": "Writing on cake", "type": "text", "required": true},
					map[string]any{"key": "delivery_date", "label": "Delivery date", "type": "text", "required": true},
					map[string]any{"key": "number_of_layers", "label": "Number of layers", "type": "text", "required": true},
				},
			},
		},
	}
	app, account, contact, session := startEcommerceWithStore(t, product, nil, store, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Custom Cakes", "57", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_0_writing_on_cake", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Happye", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_1_delivery_date", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "October 02, 04 pm", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_2_number_of_layers", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "2", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "early_handoff_details", session.CurrentStep)
	captured, ok := session.SessionData["commerce_captured_fields"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Happye", captured["writing_on_cake"])
	assert.Equal(t, "October 02, 04 pm", captured["delivery_date"])
	assert.Equal(t, "2", captured["number_of_layers"])
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, tiqrecommerce.TiqrEcommercePickupFlowID)
	assert.NotContains(t, blob, "When would you like")
	captured, ok = session.SessionData["commerce_captured_fields"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Happye", captured["writing_on_cake"])
	assert.Equal(t, "October 02, 04 pm", captured["delivery_date"])
	assert.Equal(t, "2", captured["number_of_layers"])
	_ = contact
}

func TestTiqrEcommerce_EarlyHandoffNamedProductSkipsQuantity(t *testing.T) {
	products := []any{
		map[string]any{
			"id": "101", "name": "Themed Cake", "min_price": "500",
			"category_id": "57",
			"options":     []any{map[string]any{"id": "9", "name": "Regular", "price": "500"}},
		},
		map[string]any{
			"id": "102", "name": "Other Cake", "min_price": "400",
			"category_id": "57",
			"options":     []any{map[string]any{"id": "10", "name": "Default", "price": "400"}},
		},
	}
	store := map[string]any{
		"id":   42,
		"name": "Demo",
		"test_collections": []any{
			map[string]any{
				"id":              "57",
				"name":            "Custom Cakes",
				"description":     "Made to order",
				"handoff_policy":  "after_capture",
				"handoff_message": "A baker will help next.",
				"required_capture_fields": []any{
					map[string]any{
						"key": "writing", "label": "Cake writing", "type": "text", "required": true,
					},
				},
			},
		},
	}
	useCodedIntent(t, func(_ string, ctx codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Language:     "en",
			Route:        codedflow.RouteProduct,
			ProductQuery: "Themed Cake",
			Confidence:   0.95,
		}, nil
	}, nil)
	app, account, contact, session := startEcommerceWithStore(t, products, nil, store, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "I want Themed Cake", "", nil))
	reloadSession(t, app, session)
	// Product search shows carousel; pick the product.
	require.Equal(t, "product", session.CurrentStep)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Themed Cake", "101", nil))
	reloadSession(t, app, session)

	assert.NotEqual(t, "quantity", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "custom Custom Cakes request")
	assert.Contains(t, blob, "Cake writing")
	_, hasCart := session.SessionData["tiqr_cart"]
	assert.False(t, hasCart)
	_ = contact
}

func TestTiqrEcommerce_GuideUnclearStays(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{Language: "en", Route: codedflow.RouteUnclear, Confidence: 0.4}, nil
	}, func(string) (string, error) {
		return "Would you like to buy something, check an order, or talk to staff?", nil
	})
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "what are your hours?", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, models.SessionStatusActive, session.Status)
	assert.Nil(t, session.SessionData["collection_id"])
	assert.Contains(t, outgoingBlob(t, app, session), "Would you like to buy something")
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Zero(t, transfers)
}

func TestTiqrEcommerce_HandoffTransfers(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{Language: "en", Route: codedflow.RouteHandoff, Confidence: 0.95}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "I want to talk to a person", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCancelled, session.Status)
	assert.Nil(t, session.SessionData[codedflow.DataKey])
	assert.Nil(t, session.SessionData["collection_id"])

	var transfers []models.AgentTransfer
	require.NoError(t, app.DB.Where("contact_id = ?", contact.ID).Find(&transfers).Error)
	require.Len(t, transfers, 1)
	assert.Equal(t, models.TransferSourceCommerce, transfers[0].Source)
	require.NotNil(t, transfers[0].CommerceDraftID)

	cart, ok := transfers[0].Metadata["cart"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, tiqrecommerce.CartSource, cart["source"])
	lines, ok := cart["lines"].([]any)
	require.True(t, ok)
	assert.Empty(t, lines)

	var draft models.CommerceDraft
	require.NoError(t, app.DB.First(&draft, "id = ?", *transfers[0].CommerceDraftID).Error)
	assert.Equal(t, "transferred", draft.Status)
	assert.Equal(t, tiqrecommerce.CartSource, draft.Cart["source"])
}

func TestTiqrEcommerce_HandoffSnapshotsCartAndAddress(t *testing.T) {
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	session.SessionData["tiqr_cart"] = []any{map[string]any{
		"product_option": "1312",
		"quantity":       "2",
		"product_name":   "Kunafa Cake",
		"option_name":    "Kunafa",
		"price":          40.0,
		"capture_fields": map[string]any{"writing": "Happy Birthday"},
		"capture_labels": map[string]any{"writing": "Cake writing"},
	}}
	session.SessionData["commerce_captured_fields"] = map[string]any{"writing": "Happy Birthday"}
	session.SessionData["delivery_mode"] = tiqrecommerce.ModeDelivery
	session.SessionData["delivery_latitude"] = 10.015
	session.SessionData["delivery_longitude"] = 76.341
	session.SessionData["customer_name"] = "Ada"
	session.SessionData["customer_email"] = "ada@example.com"
	session.SessionData["customer_phone"] = "910000000000"
	session.SessionData["address_line_one"] = "Infopark Rd"
	session.SessionData["city"] = "Kochi"
	session.SessionData["state"] = "Kerala"
	session.SessionData["country"] = "India"
	session.SessionData["pincode"] = "682042"
	session.SessionData["customer_notes"] = "Leave at gate"
	session.SessionData["commerce_addons"] = []any{
		map[string]any{"addon": 9, "quantity": 2, "name": "Candles"},
	}
	session.SessionData["commerce_notes"] = map[string]any{"addon_requests": "Gold candles"}
	session.SessionData["collection_id"] = "57"
	session.SessionData["collection_name"] = "Cakes"
	session.SessionData["collections"] = []any{map[string]any{
		"id": "57", "name": "Cakes", "description": "Sweet",
	}}
	require.NoError(t, app.DB.Save(session).Error)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "", tiqrecommerce.TalkToAgent, nil))
	reloadSession(t, app, session)

	assert.Equal(t, models.SessionStatusCancelled, session.Status)

	var transfers []models.AgentTransfer
	require.NoError(t, app.DB.Where("contact_id = ? AND source = ?", contact.ID, models.TransferSourceCommerce).Find(&transfers).Error)
	require.Len(t, transfers, 1)
	require.NotNil(t, transfers[0].CommerceDraftID)

	meta := transfers[0].Metadata
	cart, ok := meta["cart"].(map[string]any)
	require.True(t, ok)
	lines, ok := cart["lines"].([]any)
	require.True(t, ok)
	require.Len(t, lines, 1)
	line, ok := lines[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1312", asString(line["product_option"]))
	assert.Equal(t, "2", fmt.Sprint(line["quantity"]))
	assert.Equal(t, "Kunafa Cake", asString(line["product_name"]))
	capture, ok := line["capture_fields"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Happy Birthday", asString(capture["writing"]))

	addons, ok := meta["addons"].([]any)
	require.True(t, ok)
	require.Len(t, addons, 1)
	addon, ok := addons[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Candles", asString(addon["name"]))

	handoff, ok := session.SessionData["commerce_handoff"].(map[string]any)
	require.True(t, ok)
	handoffCart, ok := handoff["cart"].(map[string]any)
	require.True(t, ok)
	handoffLines, ok := handoffCart["lines"].([]any)
	require.True(t, ok)
	require.Len(t, handoffLines, 1)
	assert.Equal(t, "Gold candles", asString(handoff["addon_requests"]))

	address, ok := meta["address"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Ada", asString(address["name"]))
	assert.Equal(t, "Infopark Rd", asString(address["address_line_1"]))
	assert.Equal(t, "682042", asString(address["pincode"]))

	contactMeta, ok := meta["contact"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Ada", asString(contactMeta["name"]))
	assert.Equal(t, "ada@example.com", asString(contactMeta["email"]))

	fulfillment, ok := meta["fulfillment"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, tiqrecommerce.ModeDelivery, asString(fulfillment["delivery_mode"]))

	assert.Contains(t, asString(meta["notes"]), "Leave at gate")
	assert.Equal(t, "Happy Birthday", asString(meta["captured_fields"].(map[string]any)["writing"]))

	var draft models.CommerceDraft
	require.NoError(t, app.DB.First(&draft, "id = ?", *transfers[0].CommerceDraftID).Error)
	assert.Equal(t, "transferred", draft.Status)
	assert.Equal(t, tiqrecommerce.ModeDelivery, draft.FulfillmentMode)
	assert.Equal(t, "Ada", asString(draft.AddressSnapshot["name"]))
	draftLines, ok := draft.Cart["lines"].([]any)
	require.True(t, ok)
	require.Len(t, draftLines, 1)
}

func TestTiqrEcommerce_AIErrorTransfers(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{}, assert.AnError
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "hello", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_TalkToAgentSkipsAI(t *testing.T) {
	useCodedIntent(t, nil, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Talk to staff", tiqrecommerce.TalkToAgent, nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Contains(t, outgoingBlob(t, app, session), "connecting you with a team member")
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_IntentTitleOnlySelectsBuy(t *testing.T) {
	useCodedIntent(t, nil, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.NotContains(t, outgoingBlob(t, app, session), codedflow.AgentHandoff)
}

func TestTiqrEcommerce_IntentPaddedButtonIDSelectsBuy(t *testing.T) {
	useCodedIntent(t, nil, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", " buy_products ", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.NotContains(t, outgoingBlob(t, app, session), codedflow.AgentHandoff)
}

func TestTiqrEcommerce_IntentTitleOnlyOrderStatus(t *testing.T) {
	useCodedIntent(t, nil, nil)
	withOrderStatusMCP(t, orderStatusStoreFixture()["test_orders"].([]any), orderStatusDetailByUID())
	app, account, contact, session := startEcommerceWithStore(t, twoProducts(nil), nil, map[string]any{"id": 42, "name": "Demo"}, orderStatusAI())
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Check order status", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "pick_order", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "ORD-1")
	assert.Contains(t, blob, "ORD-2")
	assert.NotContains(t, blob, codedflow.AgentHandoff)
}

func TestTiqrEcommerce_UnknownButtonRepromptsIntent(t *testing.T) {
	useCodedIntent(t, nil, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Nope", "not_a_menu_button", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "intent", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "What would you like to do?")
	assert.NotContains(t, blob, codedflow.AgentHandoff)
}

func orderStatusStoreFixture() map[string]any {
	return map[string]any{
		"id":   42,
		"name": "Demo",
		"test_orders": []any{
			map[string]any{
				"id": 1, "display_uid": "ORD-1", "created_at": "2026-10-01T10:00:00Z", "status": "CONFIRMED",
			},
			map[string]any{
				"id": 2, "display_uid": "ORD-2", "created_at": "2026-09-15T08:00:00Z", "status": "PENDING_PAYMENT",
			},
		},
	}
}

func orderStatusDetailByUID() map[string]any {
	return map[string]any{
		"ORD-1": map[string]any{
			"id": 1, "display_uid": "ORD-1", "status": "CONFIRMED",
			"total_amount": 25000, "delivery_mode": "DELIVERY_TO_LOCATION",
			"items": []any{
				map[string]any{
					"quantity": 1, "amount": 25000,
					"product_option_snapshot": map[string]any{
						"name": "Regular",
						"product": map[string]any{
							"name": "Kunafa",
						},
					},
				},
			},
			"address": map[string]any{
				"name": "Asha", "address_line_1": "12 MG Road",
				"city": "Bengaluru", "state": "KA", "pincode": "560001", "country": "India",
			},
		},
		"ORD-2": map[string]any{
			"id": 2, "display_uid": "ORD-2", "status": "PENDING_PAYMENT",
			"total_amount": 5000, "delivery_mode": "PICKUP_FROM_STORE",
			"items": []any{
				map[string]any{
					"quantity": 1, "amount": 5000,
					"product_option_snapshot": map[string]any{
						"name": "Default",
						"product": map[string]any{
							"name": "Tea",
						},
					},
				},
			},
			"address": map[string]any{"address_line_1": "hidden for pickup"},
		},
	}
}

type orderStatusLookupStub struct {
	lastName string
	lastArgs map[string]any
	orders   []any
	byUID    map[string]any
}

func (s *orderStatusLookupStub) CallTool(_ context.Context, name string, args map[string]any) (any, error) {
	s.lastName = name
	s.lastArgs = args
	switch name {
	case "list_orders_by_phone":
		orders := s.orders
		if orders == nil {
			orders = []any{}
		}
		return map[string]any{"orders": orders, "count": len(orders)}, nil
	case "lookup_order_status":
		uid := strings.TrimSpace(asString(args["order_display_id"]))
		if detail, ok := s.byUID[uid]; ok {
			return detail, nil
		}
		return nil, fmt.Errorf("order not found")
	default:
		return nil, fmt.Errorf("unexpected tool %s", name)
	}
}

func (s *orderStatusLookupStub) Close() error { return nil }

func withOrderStatusMCP(t *testing.T, orders []any, byUID map[string]any) *orderStatusLookupStub {
	t.Helper()
	stub := &orderStatusLookupStub{orders: orders, byUID: byUID}
	prev := newTiqrStoreInvoker
	newTiqrStoreInvoker = func(_, _ string) tiqrStoreInvoker { return stub }
	t.Cleanup(func() { newTiqrStoreInvoker = prev })
	return stub
}

func orderStatusAI() *models.AIConfig {
	return &models.AIConfig{
		CommerceEnabled: true,
		CommerceMCPURL:  "http://mcp.test/mcp",
	}
}

func TestTiqrEcommerce_OrderStatus(t *testing.T) {
	stub := withOrderStatusMCP(t, orderStatusStoreFixture()["test_orders"].([]any), orderStatusDetailByUID())
	app, account, contact, session := startEcommerceWithStore(t, twoProducts(nil), nil, map[string]any{"id": 42, "name": "Demo"}, orderStatusAI())
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Check order status", tiqrecommerce.CheckOrderStatus, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "pick_order", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "ORD-1")
	assert.Contains(t, blob, "ORD-2")
	assert.Contains(t, blob, "1 Oct 2026")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "ORD-1", "1", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Equal(t, "lookup_order_status", stub.lastName)
	assert.Equal(t, "ORD-1", stub.lastArgs["order_display_id"])
	blob = outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Order *ORD-1*")
	assert.Contains(t, blob, "confirmed")
	assert.Contains(t, blob, "Kunafa (Regular)")
	assert.Contains(t, blob, "₹250.00")
	assert.Contains(t, blob, "Fulfillment: Delivery")
	assert.Contains(t, blob, "12 MG Road")
}

func TestTiqrEcommerce_OrderStatusPickupOmitsAddress(t *testing.T) {
	withOrderStatusMCP(t, orderStatusStoreFixture()["test_orders"].([]any), orderStatusDetailByUID())
	app, account, contact, session := startEcommerceWithStore(t, twoProducts(nil), nil, map[string]any{"id": 42, "name": "Demo"}, orderStatusAI())
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Check order status", tiqrecommerce.CheckOrderStatus, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "ORD-2", "2", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Order *ORD-2*")
	assert.Contains(t, blob, "Fulfillment: Store pickup")
	assert.NotContains(t, blob, "hidden for pickup")
}

func TestTiqrEcommerce_OrderStatusMissing(t *testing.T) {
	withOrderStatusMCP(t, []any{}, orderStatusDetailByUID())
	app, account, contact, session := startEcommerceWithStore(t, twoProducts(nil), nil, map[string]any{"id": 42, "name": "Demo"}, orderStatusAI())
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Check order status", tiqrecommerce.CheckOrderStatus, nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Contains(t, outgoingBlob(t, app, session), "couldn't find a recent order")
}

func TestTiqrEcommerce_OrderStatusUnavailableWithoutMCP(t *testing.T) {
	app, account, contact, session := startEcommerceWithStore(t, twoProducts(nil), nil, map[string]any{"id": 42, "name": "Demo"}, &models.AIConfig{
		CommerceEnabled: false,
	})
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Check order status", tiqrecommerce.CheckOrderStatus, nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Contains(t, outgoingBlob(t, app, session), "couldn't look up your orders")
}

func TestTiqrEcommerce_PreferredLanguageFromIntent(t *testing.T) {
	var welcomeCalls int
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Language:   "hi",
			Route:      codedflow.RouteChoice,
			ChoiceID:   tiqrecommerce.BuyProducts,
			Confidence: 0.92,
		}, nil
	}, nil)
	useCodedTranslate(t, func(_, text string) (string, error) {
		if strings.Contains(text, "Welcome to") {
			welcomeCalls++
		}
		return "HI:" + text, nil
	})
	app, org, account, contact, session := newGraphTestFixtures(t)
	srv := newStoreServer(t, twoProducts(nil), nil)
	useStoreREST(t, srv)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "intent", session.CurrentStep)
	assert.Empty(t, session.SessionData[codedflow.CustomerLanguageKey])
	assert.Contains(t, outgoingBlob(t, app, session), "Welcome to Demo")
	assert.Zero(t, welcomeCalls)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "mujhe kharidna hai", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "hi", session.SessionData[codedflow.CustomerLanguageKey])
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "HI:Choose a collection")
}

func TestTiqrEcommerce_EnglishSkipsTranslation(t *testing.T) {
	useCodedIntent(t, nil, nil)
	useCodedTranslate(t, nil)
	app, _, _, session := startEcommerce(t, twoProducts(nil), nil)
	assert.Empty(t, session.SessionData[codedflow.CustomerLanguageKey])
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Welcome to Demo")
	assert.NotContains(t, blob, "HI:")
}

func TestTiqrEcommerce_ProductSearchFromMenu(t *testing.T) {
	var counts storeCounts
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Language:     "en",
			Route:        codedflow.RouteProduct,
			ProductQuery: "Themed Cake",
			Confidence:   0.91,
		}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), &counts)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "I want to purchase Themed Cake", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "product", session.CurrentStep)
	assert.Equal(t, 1, counts.searches)
	assert.Equal(t, "Themed Cake", counts.lastSearch)
	assert.Equal(t, "Themed Cake", session.SessionData["collection_name"])
	assert.Nil(t, session.SessionData["collection_id"])
}

func TestTiqrEcommerce_CollectionRouteFromMenu(t *testing.T) {
	var counts storeCounts
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Language:     "en",
			Route:        codedflow.RouteCollection,
			CollectionID: "58",
			Confidence:   0.9,
		}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), &counts)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "what cakes are available", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "product", session.CurrentStep)
	assert.Equal(t, "58", session.SessionData["collection_id"])
	assert.Equal(t, "Cakes", session.SessionData["collection_name"])
	assert.Equal(t, 1, counts.products)
	assert.Zero(t, counts.searches)
}

func TestTiqrEcommerce_InventedCollectionTransfers(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Language:     "en",
			Route:        codedflow.RouteCollection,
			CollectionID: "999",
			Confidence:   0.99,
		}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "show me muffins", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_GuideExhaustionTransfers(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{Language: "en", Route: codedflow.RouteUnclear, Confidence: 0.2}, nil
	}, func(string) (string, error) {
		return "Please choose buy, order status, or staff.", nil
	})
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	for i := 0; i < codedflow.MaxGuideTurns(); i++ {
		require.NoError(t, app.runCodedFlow(account, contact, session, flow, "hmm", "", nil))
		reloadSession(t, app, session)
		assert.Equal(t, "intent", session.CurrentStep)
		assert.Equal(t, models.SessionStatusActive, session.Status)
	}
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "still lost", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestValidateCodedIntent_RejectsInventedIDs(t *testing.T) {
	ctx := codedflow.IntentContext{
		AllowCatalog: true,
		ChoiceIDs:    map[string]string{tiqrecommerce.BuyProducts: "Buy products"},
		Collections:  map[string]string{"57": "Sweets"},
	}
	_, ok := codedflow.ValidateCodedIntent(codedflow.IntentResult{Route: codedflow.RouteCollection, CollectionID: "999", Confidence: 1}, ctx)
	assert.False(t, ok)
	_, ok = codedflow.ValidateCodedIntent(codedflow.IntentResult{Route: codedflow.RouteProduct, ProductQuery: "Cake", ChoiceID: "x", Confidence: 1}, ctx)
	assert.False(t, ok)
	_, ok = codedflow.ValidateCodedIntent(codedflow.IntentResult{Route: codedflow.RouteChoice, ChoiceID: tiqrecommerce.BuyProducts, Confidence: 1}, ctx)
	assert.True(t, ok)
	_, ok = codedflow.ValidateCodedIntent(codedflow.IntentResult{Route: codedflow.RouteHandoff, Confidence: 1}, ctx)
	assert.True(t, ok)
}

func TestValidateCodedIntent_AnswerRoute(t *testing.T) {
	ctx := codedflow.IntentContext{Pattern: `^[0-9]+$`}
	got, ok := codedflow.ValidateCodedIntent(codedflow.IntentResult{Route: codedflow.RouteAnswer, Answer: "2", Confidence: 0.9}, ctx)
	assert.True(t, ok)
	assert.Equal(t, "2", got.Answer)

	_, ok = codedflow.ValidateCodedIntent(codedflow.IntentResult{Route: codedflow.RouteAnswer, Answer: "2", ChoiceID: "x", Confidence: 0.9}, ctx)
	assert.False(t, ok)
	_, ok = codedflow.ValidateCodedIntent(codedflow.IntentResult{Route: codedflow.RouteAnswer, Answer: "two", Confidence: 0.9}, ctx)
	assert.False(t, ok)
	_, ok = codedflow.ValidateCodedIntent(codedflow.IntentResult{Route: codedflow.RouteAnswer, Answer: "2", Confidence: 0.9}, codedflow.IntentContext{})
	assert.False(t, ok)
}

func TestBuildIntentPrompt_IncludesStepContext(t *testing.T) {
	prompt := codedflow.BuildIntentPrompt("Kunafaa", codedflow.IntentContext{
		Question:  "Here is what's available in *Sweets*.",
		Doing:     "The customer is choosing one product from the carousel.",
		Expect:    "A product from the cards.",
		ChoiceIDs: map[string]string{"101": "Kunafa (₹40) — Add to cart"},
	})
	assert.Contains(t, prompt, "Here is what's available in *Sweets*.")
	assert.Contains(t, prompt, "The customer is choosing one product from the carousel.")
	assert.Contains(t, prompt, "A product from the cards.")
	assert.Contains(t, prompt, "Kunafa (₹40) — Add to cart")
	assert.Contains(t, prompt, "route answer:")
	assert.Contains(t, prompt, "At most 200 words")
	assert.Contains(t, prompt, `"reasoning":""`)
}

func TestLimitWords_CapsAtLimit(t *testing.T) {
	words := make([]string, 220)
	for i := range words {
		words[i] = "word"
	}
	got := codedflow.LimitWords(strings.Join(words, " "), 200)
	assert.Equal(t, 200, len(strings.Fields(got)))
	assert.Equal(t, "keep this", codedflow.LimitWords("  keep   this  ", 100))
}

func TestTiqrEcommerce_QuantityWordAccepted(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Language:   "en",
			Route:      codedflow.RouteAnswer,
			Answer:     "2",
			Confidence: 0.95,
		}, nil
	}, nil)
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerce(t, products, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.Equal(t, "quantity", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Two", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "next", session.CurrentStep)
	assert.Equal(t, "2", session.SessionData["quantity"])
	assert.NotContains(t, outgoingBlob(t, app, session), "Send a whole number.")
	cart, ok := session.SessionData["tiqr_cart"].([]any)
	require.True(t, ok)
	require.Len(t, cart, 1)
	item, ok := cart[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2", item["quantity"])
}

func TestTiqrEcommerce_CollectionTypoSelectsChoice(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Language:   "en",
			Route:      codedflow.RouteChoice,
			ChoiceID:   "57",
			Confidence: 0.9,
		}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.Equal(t, "collection", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweetss", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "57", session.SessionData["collection_id"])
	assert.Equal(t, "product", session.CurrentStep)
}

// A step that records a call must not hand that record to the next step in
// the same turn. Otherwise the collections lookup is replayed as the menu
// answer and an empty choice transfers the chat.
func TestCodedCallCursorSkipsCallsMadeThisRun(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	c := codedflow.NewConv(nil, newCodedChat(&chatNodeCtx{session: session}))

	c.AppendCall(map[string]any{"name": "store", "ok": true, "var": "store", "value": map[string]any{"name": "Demo"}})
	_, done := c.DoneCall()
	assert.False(t, done)

	c.SetSeq(0)
	rec, done := c.DoneCall()
	require.True(t, done)
	assert.Equal(t, "store", rec["name"])
}

func TestValidateCodedIntent_CheckoutRoute(t *testing.T) {
	got, ok := codedflow.ValidateCodedIntent(codedflow.IntentResult{Route: codedflow.RouteCheckout, Confidence: 0.9}, codedflow.IntentContext{})
	assert.True(t, ok)
	assert.Equal(t, codedflow.RouteCheckout, got.Route)
	_, ok = codedflow.ValidateCodedIntent(codedflow.IntentResult{Route: codedflow.RouteCheckout, ChoiceID: "x", Confidence: 0.9}, codedflow.IntentContext{})
	assert.False(t, ok)
}

func TestSanitizeRecoverHint_DropsURLsAndKeepsFields(t *testing.T) {
	hint := codedflow.SanitizeRecoverHint(`{"email":["This field is required."],"detail":"see https://example.com/docs?token=abc"}`)
	assert.Contains(t, hint, "email")
	assert.NotContains(t, hint, "https://")
	assert.NotContains(t, hint, "token=")
}

func TestValidateCodedRecover_MissingField(t *testing.T) {
	ctx := codedflow.RecoverContext{Kind: "create"}
	got, ok := codedflow.ValidateCodedRecover(codedflow.RecoverResult{
		Kind: codedflow.RecoverMissingField, Field: "email", Message: "Please share your email.", Confidence: 0.9,
	}, ctx)
	assert.True(t, ok)
	assert.Equal(t, "customer_email", got.Field)

	_, ok = codedflow.ValidateCodedRecover(codedflow.RecoverResult{
		Kind: codedflow.RecoverMissingField, Field: "email", Message: "Call https://api.example/x", Confidence: 0.9,
	}, ctx)
	assert.False(t, ok)

	_, ok = codedflow.ValidateCodedRecover(codedflow.RecoverResult{
		Kind: codedflow.RecoverMissingField, Field: "email", Message: "Please share your email.", Confidence: 0.9,
	}, codedflow.RecoverContext{Kind: "fetch"})
	assert.False(t, ok)
}

func TestCanonicalFieldsFromHint_OrdersEveryMissingField(t *testing.T) {
	hint := codedflow.SanitizeRecoverHint(`{"pincode":["required"],"new_address":{"city":["required"]},"email":["required"],"phone_number":["required"]}`)
	assert.Equal(t, []string{
		"customer_phone",
		"customer_email",
		"city",
		"pincode",
	}, codedflow.CanonicalFieldsFromHint(hint))
}

func TestExpandRecoverAsks_AsksEveryHintField(t *testing.T) {
	hint := codedflow.SanitizeRecoverHint(`{"pincode":["required"],"email":["required"]}`)
	got := codedflow.ExpandRecoverAsks(codedflow.RecoverResult{
		Kind:    codedflow.RecoverMissingField,
		Field:   "customer_email",
		Message: "Could you reply with your email address?",
		Fields: []codedflow.RecoverAsk{{
			Field: "customer_email", Message: "Could you reply with your email address?",
		}},
	}, hint)
	require.Equal(t, codedflow.RecoverMissingField, got.Kind)
	require.Len(t, got.Fields, 2)
	assert.Equal(t, "customer_email", got.Fields[0].Field)
	assert.Equal(t, "Could you reply with your email address?", got.Fields[0].Message)
	assert.Equal(t, "pincode", got.Fields[1].Field)
	assert.Equal(t, "Could you share your pincode?", got.Fields[1].Message)
}

func TestParseCodedRecover_AllFields(t *testing.T) {
	got, err := codedflow.ParseCodedRecover(`{"kind":"missing_field","field":"pincode","fields":[{"field":"pincode","message":"What is your pincode?"},{"field":"email","message":"What is your email?"}],"message":"I need a couple of details.","confidence":0.9,"reasoning":"both missing"}`)
	require.NoError(t, err)
	valid, ok := codedflow.ValidateCodedRecover(got, codedflow.RecoverContext{Kind: "create"})
	require.True(t, ok)
	require.Len(t, valid.Fields, 2)
	assert.Equal(t, "customer_email", valid.Fields[0].Field)
	assert.Equal(t, "What is your email?", valid.Fields[0].Message)
	assert.Equal(t, "pincode", valid.Fields[1].Field)
	assert.Equal(t, "What is your pincode?", valid.Fields[1].Message)

	fromNames, err := codedflow.ParseCodedRecover(`{"kind":"missing_field","fields":["phone_number","city"],"message":"Please share the missing details.","confidence":0.8}`)
	require.NoError(t, err)
	valid, ok = codedflow.ValidateCodedRecover(fromNames, codedflow.RecoverContext{Kind: "create"})
	require.True(t, ok)
	assert.Equal(t, []string{"customer_phone", "city"}, codedflow.RecoverAskFields(valid.Fields))
}

func TestBuildRecoverPrompt_NoSecrets(t *testing.T) {
	prompt := codedflow.BuildRecoverPrompt(codedflow.RecoverContext{
		Kind: "create", Operation: "create_order", Resource: "order", Status: 400,
		Hint: codedflow.SanitizeRecoverHint(`{"email":["required"]}`),
	})
	assert.Contains(t, prompt, "validation fields")
	assert.NotContains(t, prompt, "https://")
}

func TestTiqrEcommerce_CheckoutEmptyCartReturnsToCollections(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{Language: "en", Route: codedflow.RouteCheckout, Confidence: 0.95}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.Equal(t, "collection", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "checkout please", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "cart is empty")
	assert.Equal(t, models.SessionStatusActive, session.Status)
}

func TestTiqrEcommerce_CheckoutWithCartOpensDetails(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{Language: "en", Route: codedflow.RouteCheckout, Confidence: 0.95}, nil
	}, nil)
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)

	session.SessionData["tiqr_cart"] = []any{map[string]any{"product_option": "9", "quantity": "1"}}
	session.SessionData["cart_count"] = float64(1)
	require.NoError(t, app.DB.Save(session).Error)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "I want to checkout", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "cart_review_1", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "Confirm items")
	assert.Contains(t, outgoingBlob(t, app, session), "Edit cart")
	assert.NotContains(t, outgoingBlob(t, app, session), "cart is empty")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Confirm items", tiqrecommerce.ConfirmItems, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "details", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, tiqrecommerce.TiqrEcommercePickupFlowID)
	assert.Contains(t, blob, "name, email, and phone number")
	assert.NotContains(t, blob, "and address")
}

func TestTiqrEcommerce_ListProductsFailureRecovers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			http.Error(w, `{"detail":"boom https://internal/api"}`, http.StatusBadGateway)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo"})
		}
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)
	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)

	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "unable to fetch those products")
	assert.Contains(t, blob, "connecting you with a team member")
	assert.NotContains(t, blob, "https://internal")
	assert.NotContains(t, blob, "ticker api")
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_CreateOrderMissingEmailRetry(t *testing.T) {
	orderCalls := 0
	var placed map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{
						"id": "101", "name": "Kunafa", "min_price": "40",
						"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
					},
				},
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/order/"):
			orderCalls++
			if orderCalls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"email":["This field is required."]}`))
				return
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&placed))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ord-1", "display_uid": "TQ-1"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo"})
		}
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)
	useCodedRecover(t, func(ctx codedflow.RecoverContext) (codedflow.RecoverResult, error) {
		assert.Equal(t, "create", ctx.Kind)
		return codedflow.RecoverResult{
			Kind: codedflow.RecoverMissingField, Field: "customer_email",
			Message: "Could you reply with your email address?", Confidence: 0.95,
		}, nil
	})

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Checkout", tiqrecommerce.Checkout, nil))
	reloadSession(t, app, session)
	require.Equal(t, "cart_review_1", session.CurrentStep)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Confirm items", tiqrecommerce.ConfirmItems, nil))
	reloadSession(t, app, session)
	require.Equal(t, "details", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), tiqrecommerce.TiqrEcommercePickupFlowID)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "", "", map[string]any{
		"customer_name":  "Ada",
		"customer_phone": "910000000000",
		"customer_notes": "Leave at counter",
	}))
	reloadSession(t, app, session)
	assert.Contains(t, outgoingBlob(t, app, session), "Could you reply with your email address?")
	assert.Equal(t, 1, orderCalls)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "ada@example.com", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, 2, orderCalls)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Equal(t, "ada@example.com", placed["email"])
	assert.Equal(t, "910000000000", placed["phone_number"])
	assert.Equal(t, "Leave at counter", placed["notes"])
	assert.Equal(t, "PICKUP_FROM_STORE", placed["delivery_mode"])
	_, hasAddress := placed["new_address"]
	assert.False(t, hasAddress)
	meta, ok := placed["buyer_meta_data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Ada", meta["name"])
	assert.Equal(t, "ada@example.com", meta["email"])
	assert.Equal(t, "910000000000", meta["phone"])
	assert.Equal(t, "910000000000", meta["phone_number"])
	assert.Equal(t, "Leave at counter", meta["notes"])
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Order placed!")
	assert.Contains(t, blob, "TQ-1")
	assert.NotContains(t, blob, "order is confirmed")
	assert.NotContains(t, blob, "ticker api")
	assert.NotContains(t, blob, "This field is required")
}

func TestTiqrEcommerce_CreateOrderAsksEveryMissingFieldBeforeRetry(t *testing.T) {
	orderCalls := 0
	recoverCalls := 0
	var placed map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{
						"id": "101", "name": "Kunafa", "min_price": "40",
						"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
					},
				},
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/order/"):
			orderCalls++
			if orderCalls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"pincode":["This field is required."],"email":["This field is required."]}`))
				return
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&placed))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ord-1", "display_uid": "TQ-1"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo"})
		}
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)
	useCodedRecover(t, func(ctx codedflow.RecoverContext) (codedflow.RecoverResult, error) {
		recoverCalls++
		assert.Equal(t, "create", ctx.Kind)
		assert.Contains(t, ctx.Hint, "email")
		assert.Contains(t, ctx.Hint, "pincode")
		return codedflow.RecoverResult{
			Kind: codedflow.RecoverMissingField, Field: "email",
			Message: "Could you reply with your email address?", Confidence: 0.95,
		}, nil
	})

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Checkout", tiqrecommerce.Checkout, nil))
	reloadSession(t, app, session)
	require.Equal(t, "cart_review_1", session.CurrentStep)

	session.SessionData["delivery_mode"] = tiqrecommerce.ModeDelivery
	require.NoError(t, app.DB.Save(session).Error)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Confirm items", tiqrecommerce.ConfirmItems, nil))
	reloadSession(t, app, session)
	require.Equal(t, "details", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), tiqrecommerce.EcommerceFlowID)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "", "", map[string]any{
		"customer_name":    "Ada",
		"customer_phone":   "910000000000",
		"address_line_one": "1 Main",
		"city":             "Kochi",
		"state":            "KL",
		"country":          "India",
	}))
	reloadSession(t, app, session)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Could you reply with your email address?")
	assert.NotContains(t, blob, "Could you share your pincode?")
	assert.Equal(t, 1, orderCalls)
	assert.Equal(t, 1, recoverCalls)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "ada@example.com", "", nil))
	reloadSession(t, app, session)
	blob = outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Could you share your pincode?")
	assert.Equal(t, "order_fix_pincode", session.CurrentStep)
	assert.Equal(t, 1, orderCalls)
	assert.Equal(t, 1, recoverCalls)
	assert.Equal(t, models.SessionStatusActive, session.Status)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "682020", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, 2, orderCalls)
	assert.Equal(t, 1, recoverCalls)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Equal(t, "ada@example.com", placed["email"])
	address, ok := placed["new_address"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "682020", address["pincode"])
	blob = outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Order placed!")
	assert.Contains(t, blob, "TQ-1")
	assert.NotContains(t, blob, "order is confirmed")
	assert.NotContains(t, blob, "ticker api")
	assert.NotContains(t, blob, "This field is required")
}

func TestTiqrEcommerce_CreateOrderSendsPayNowCTA(t *testing.T) {
	var placed map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{
						"id": "101", "name": "Kunafa", "min_price": "40",
						"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
					},
				},
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/order/"):
			require.NoError(t, json.NewDecoder(r.Body).Decode(&placed))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "ord-pay",
				"display_uid": "TQ-PAY",
				"amount":      4000,
				"status":      "PENDING_PAYMENT",
				"payment": map[string]any{
					"status": "initiated",
					"meta_data": map[string]any{
						"url_to_redirect": "https://pay.example/go",
					},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo", "currency": "INR"})
		}
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Checkout", tiqrecommerce.Checkout, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Confirm items", tiqrecommerce.ConfirmItems, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "", "", map[string]any{
		"customer_name":  "Ada",
		"customer_phone": "910000000000",
		"customer_email": "ada@example.com",
	}))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Equal(t, "ada@example.com", placed["email"])

	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Order placed!")
	assert.Contains(t, blob, "TQ-PAY")
	assert.Contains(t, blob, "Pay now")
	assert.Contains(t, blob, "https://pay.example/go")
	assert.Contains(t, blob, `"type":"cta_url"`)
	assert.NotContains(t, blob, "Pay here:")
	assert.NotContains(t, blob, "order is confirmed")
}

func TestTiqrEcommerce_CreateOrderRetryExhausted(t *testing.T) {
	prev := codedflow.IntentSettings.OrderRetries
	codedflow.IntentSettings.OrderRetries = 0
	t.Cleanup(func() { codedflow.IntentSettings.OrderRetries = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/category/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count":   1,
				"results": []any{map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"}},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{map[string]any{
					"id": "101", "name": "Kunafa", "min_price": "40",
					"options": []any{map[string]any{"id": "9", "name": "Regular", "price": "40"}},
				}},
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/order/"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"email":["This field is required."]}`))
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo"})
		}
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)
	useCodedRecover(t, func(codedflow.RecoverContext) (codedflow.RecoverResult, error) {
		return codedflow.RecoverResult{
			Kind: codedflow.RecoverGiveUp, Message: "We could not place your order just now.", Confidence: 0.9,
		}, nil
	})

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "shop", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Checkout", tiqrecommerce.Checkout, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Confirm items", tiqrecommerce.ConfirmItems, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "", "", map[string]any{
		"customer_name": "Ada", "customer_phone": "910000000000",
		"address_line_one": "1 Main", "city": "Kochi", "state": "KL", "country": "India", "pincode": "682001",
	}))
	reloadSession(t, app, session)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "Placing order for the following items failed.")
	assert.Contains(t, blob, "Regular x 1")
	assert.Contains(t, blob, "Address")
	assert.Contains(t, blob, "Ada")
	assert.Contains(t, blob, "1 Main")
	assert.Contains(t, blob, "Kochi")
	assert.Contains(t, blob, "connecting you with a team member")
	assert.NotContains(t, blob, "Thank you for shopping")
	assert.NotContains(t, blob, "ticker api")
	assert.NotContains(t, blob, "https://")
	var transfers int64
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("contact_id = ?", contact.ID).Count(&transfers).Error)
	assert.Equal(t, int64(1), transfers)
}

func TestTiqrEcommerce_PickupOnlyProceedsToCollections(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerceWithStore(t, products, nil, map[string]any{
		"id":             42,
		"name":           "Demo",
		"delivery_modes": []any{"PICKUP_FROM_STORE"},
	}, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "fulfillment_pickup_only", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "store pickup only")
	assert.Contains(t, blob, "Proceed")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Proceed", tiqrecommerce.Proceed, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, "PICKUP_FROM_STORE", session.SessionData["delivery_mode"])
}

func TestTiqrEcommerce_BothModesPickupSkipsLocation(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerceWithStore(t, products, nil, map[string]any{
		"id":   42,
		"name": "Demo",
		"delivery_modes": []any{
			"PICKUP_FROM_STORE",
			"DELIVERY_TO_LOCATION",
		},
	}, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "fulfillment_mode", session.CurrentStep)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Store pickup", tiqrecommerce.PickupMode, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, "PICKUP_FROM_STORE", session.SessionData["delivery_mode"])
}

func TestTiqrEcommerce_DeliveryInRangeContinuesToCollections(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerceWithStore(t, products, nil, map[string]any{
		"id":                      42,
		"name":                    "Demo",
		"address":                 "Vadakara",
		"latitude":                "11.2",
		"longitude":               "75.8",
		"location_based_delivery": true,
		"free_delivery_radius":    8,
		"delivery_radius":         16,
		"delivery_modes":          []any{"DELIVERY_TO_LOCATION"},
	}, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "delivery_location_1", session.CurrentStep)

	pin := `{"latitude":11.2,"longitude":75.8}`
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, pin, "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, "DELIVERY_TO_LOCATION", session.SessionData["delivery_mode"])
	assert.Equal(t, 11.2, session.SessionData["delivery_latitude"])
	assert.Equal(t, 75.8, session.SessionData["delivery_longitude"])
	assert.Equal(t, "free", session.SessionData["delivery_zone"])
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "delivery is free")
	assert.Contains(t, blob, "8 km")
	assert.Contains(t, blob, "16 km")
}

func TestTiqrEcommerce_DeliveryOutOfRangeOffersPickup(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerceWithStore(t, products, nil, map[string]any{
		"id":                      42,
		"name":                    "Demo",
		"address":                 "Vadakara, Kozhikode",
		"latitude":                "11.55",
		"longitude":               "75.63",
		"location_based_delivery": true,
		"free_delivery_radius":    8,
		"delivery_radius":         16,
		"delivery_modes": []any{
			"PICKUP_FROM_STORE",
			"DELIVERY_TO_LOCATION",
		},
	}, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Delivery", tiqrecommerce.DeliveryMode, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "delivery_location_1", session.CurrentStep)

	pin := `{"latitude":1.0,"longitude":1.0}`
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, pin, "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "delivery_fallback_1", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "unable to deliver")
	assert.Contains(t, blob, "Vadakara")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Store pickup", tiqrecommerce.PickupMode, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	assert.Equal(t, "PICKUP_FROM_STORE", session.SessionData["delivery_mode"])
}

func TestTiqrEcommerce_DeliveryOnlyOutOfRangeAsksAgain(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	app, account, contact, session := startEcommerceWithStore(t, products, nil, map[string]any{
		"id":                      42,
		"name":                    "Demo",
		"latitude":                "11.55",
		"longitude":               "75.63",
		"location_based_delivery": true,
		"delivery_modes":          []any{"DELIVERY_TO_LOCATION"},
		"delivery_radius":         10,
	}, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, `{"latitude":1,"longitude":2}`, "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "delivery_location_2", session.CurrentStep)
	blob := outgoingBlob(t, app, session)
	assert.Contains(t, blob, "within our delivery radius")
	assert.NotContains(t, blob, "Store Pickup as your preferred option")
	assert.NotContains(t, blob, `"id":"pickup"`)
}

func TestTiqrEcommerce_CollectionListPagesWithShowMore(t *testing.T) {
	collections := make([]any, 12)
	for i := 0; i < len(collections); i++ {
		collections[i] = map[string]any{
			"id":          strconv.Itoa(i + 1),
			"name":        fmt.Sprintf("Collection %02d", i+1),
			"description": "Group",
		}
	}
	app, account, contact, session := startEcommerceWithStore(t, twoProducts(nil), nil, map[string]any{
		"id":               42,
		"name":             "Demo",
		"test_collections": collections,
	}, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.Equal(t, "collection", session.CurrentStep)

	titles := lastListRowTitles(t, app, session)
	require.Len(t, titles, 10)
	assert.Equal(t, "Collection 01", titles[0])
	assert.Equal(t, "Collection 09", titles[8])
	assert.Equal(t, codedflow.ShowMoreTitle, titles[9])
	assert.NotContains(t, titles, "Collection 10")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, codedflow.ShowMoreTitle, codedflow.ShowMoreID, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "collection", session.CurrentStep)
	titles = lastListRowTitles(t, app, session)
	require.Len(t, titles, 3)
	assert.Equal(t, []string{"Collection 10", "Collection 11", "Collection 12"}, titles)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Collection 12", "12", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "product", session.CurrentStep)
	assert.Equal(t, "12", session.SessionData["collection_id"])
}

func TestTiqrEcommerce_CollectionListFollowsNextPage(t *testing.T) {
	collections := make([]any, 15)
	for i := 0; i < len(collections); i++ {
		collections[i] = map[string]any{
			"id":          strconv.Itoa(i + 1),
			"name":        fmt.Sprintf("Shelf %02d", i+1),
			"description": "Group",
		}
	}
	counts := &storeCounts{}
	app, account, contact, session := startEcommerceWithStore(t, twoProducts(nil), counts, map[string]any{
		"id":               42,
		"name":             "Demo",
		"test_collections": collections,
		"test_page_size":   10,
	}, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	assert.Equal(t, 1, counts.collections)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	assert.Equal(t, 1, counts.collections)
	titles := lastListRowTitles(t, app, session)
	require.Len(t, titles, 10)
	assert.Equal(t, codedflow.ShowMoreTitle, titles[9])
	assert.NotContains(t, titles, "Shelf 10")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, codedflow.ShowMoreTitle, codedflow.ShowMoreID, nil))
	reloadSession(t, app, session)
	assert.Equal(t, 2, counts.collections)
	titles = lastListRowTitles(t, app, session)
	require.Len(t, titles, 6)
	assert.Equal(t, "Shelf 10", titles[0])
	assert.Equal(t, "Shelf 15", titles[5])
	assert.NotContains(t, titles, codedflow.ShowMoreTitle)
}

func TestTiqrEcommerce_ProductCarouselPagesWithShowMore(t *testing.T) {
	products := make([]any, 12)
	for i := 0; i < len(products); i++ {
		products[i] = map[string]any{
			"id":        strconv.Itoa(200 + i),
			"name":      fmt.Sprintf("Item %02d", i+1),
			"min_price": "10",
			"options":   []any{map[string]any{"id": "1", "name": "Default", "price": "10"}},
		}
	}
	app, account, contact, session := startEcommerce(t, products, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.Equal(t, "product", session.CurrentStep)

	ids := lastCarouselButtonIDs(t, app, session)
	require.Len(t, ids, 10)
	assert.Equal(t, "200", ids[0])
	assert.Equal(t, "208", ids[8])
	assert.Equal(t, codedflow.ShowMoreID, ids[9])

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, codedflow.ShowMoreTitle, codedflow.ShowMoreID, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "product", session.CurrentStep)
	ids = lastCarouselButtonIDs(t, app, session)
	require.Len(t, ids, 3)
	assert.Equal(t, []string{"209", "210", "211"}, ids)
}

func TestTiqrEcommerce_OptionListPagesWithShowMore(t *testing.T) {
	options := make([]any, 12)
	for i := 0; i < len(options); i++ {
		options[i] = map[string]any{
			"id":    strconv.Itoa(i + 1),
			"name":  fmt.Sprintf("Size %02d", i+1),
			"price": "10",
		}
	}
	app, account, contact, session := startEcommerce(t, twoProducts(options), nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.Equal(t, "option", session.CurrentStep)

	titles := lastListRowTitles(t, app, session)
	require.Len(t, titles, 10)
	assert.Contains(t, titles[0], "Size 01")
	assert.Contains(t, titles[8], "Size 09")
	assert.Equal(t, codedflow.ShowMoreTitle, titles[9])

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, codedflow.ShowMoreTitle, codedflow.ShowMoreID, nil))
	reloadSession(t, app, session)
	assert.Equal(t, "option", session.CurrentStep)
	titles = lastListRowTitles(t, app, session)
	require.Len(t, titles, 3)
	assert.Contains(t, titles[0], "Size 10")
	assert.Contains(t, titles[2], "Size 12")

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Size 12", "12", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "quantity", session.CurrentStep)
	assert.Equal(t, "12", session.SessionData["option_id"])
}

func TestCodedOrderNotesIncludesImageLink(t *testing.T) {
	t.Parallel()
	notes := tiqrecommerce.CodedOrderNotes(map[string]any{
		"commerce_captured_fields": map[string]any{
			"reference_image": []any{map[string]any{"url": "/api/media/msg-1"}},
		},
		"commerce_capture_labels": map[string]any{"reference_image": "Reference photo"},
	})
	assert.Contains(t, notes, "Reference photo")
	assert.Contains(t, notes, "/api/media/msg-1")
}

func TestSessionExpectsImageCapture(t *testing.T) {
	t.Parallel()
	session := &models.ChatbotSession{
		CurrentStep: "capture_0_reference_image",
		SessionData: models.JSONB{
			"commerce_capture_pending": map[string]any{
				"step": "capture_0_reference_image",
				"key":  "reference_image",
				"type": "image",
			},
		},
	}
	assert.True(t, tiqrecommerce.SessionExpectsCaptureAttachment(session))
	session.CurrentStep = "quantity"
	assert.False(t, tiqrecommerce.SessionExpectsCaptureAttachment(session))
}

func TestHandoffContextIncludesImageLink(t *testing.T) {
	t.Parallel()
	link := "/api/media/msg-9"
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"commerce_notes": map[string]any{
			"order_notes":  "Kunafa\n- Reference photo: " + link,
			"media_shared": "Reference photo: " + link,
		},
	}}
	assert.Contains(t, tiqrecommerce.SessionHandoffNotes(session), link)

	media := tiqrecommerce.MergeMediaReferences(nil, models.JSONB{
		"reference_image": []any{map[string]any{
			"message_id": "msg-9", "url": link, "capture_key": "reference_image", "filename": "ref.jpg",
		}},
	})
	require.Len(t, media, 1)
	item, ok := media[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, link, item["url"])

	summary := commerceHandoffSummary(&models.CommerceDraft{
		Notes: models.JSONB{"media_shared": "Reference photo: " + link},
	}, tickermcp.Category{Name: "Sweets"})
	assert.Contains(t, summary, link)
}

func TestTiqrEcommerce_ImageCaptureStoresLink(t *testing.T) {
	products := twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}})
	store := map[string]any{
		"id":   42,
		"name": "Demo",
		"test_collections": []any{
			map[string]any{
				"id":   "57",
				"name": "Sweets",
				"required_capture_fields": []any{
					map[string]any{
						"key": "reference_image", "label": "Reference photo", "type": "image", "required": true,
					},
				},
			},
		},
	}
	app, account, contact, session := startEcommerceWithStore(t, products, nil, store, nil)
	flow := codedflow.ByKey(tiqrecommerce.FlowKey)

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Buy products", tiqrecommerce.BuyProducts, nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Sweets", "57", nil))
	reloadSession(t, app, session)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "Kunafa", "101", nil))
	reloadSession(t, app, session)
	require.Equal(t, "quantity", session.CurrentStep)
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "1", "", nil))
	reloadSession(t, app, session)
	require.Equal(t, "capture_0_reference_image", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "Please send a photo.")
	assert.True(t, tiqrecommerce.SessionExpectsCaptureAttachment(session))

	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "[Image attached]", "", nil))
	reloadSession(t, app, session)
	assert.Equal(t, "capture_0_reference_image", session.CurrentStep)
	assert.Contains(t, outgoingBlob(t, app, session), "Please send the photo again.")

	messageID := uuid.New()
	tiqrecommerce.StashInboundCaptureMedia(session, messageID.String(), "images/ref.jpg", "image/jpeg", "ref.jpg")
	require.NoError(t, app.runCodedFlow(account, contact, session, flow, "[Image attached]", "", nil))
	reloadSession(t, app, session)
	assert.NotEqual(t, "capture_0_reference_image", session.CurrentStep)
	link := "/api/media/" + messageID.String()
	captured, _ := session.SessionData["commerce_captured_fields"].(map[string]any)
	require.NotNil(t, captured)
	assert.Contains(t, fmt.Sprint(captured["reference_image"]), link)
	notes, _ := session.SessionData["commerce_notes"].(map[string]any)
	require.NotNil(t, notes)
	assert.Contains(t, fmt.Sprint(notes["media_shared"]), link)
	assert.Contains(t, tiqrecommerce.CodedOrderNotes(session.SessionData), link)
	_, stillStashed := session.SessionData["_inbound_capture_media"]
	assert.False(t, stillStashed)
}

func TestCheckoutImageCaptureContinuesAndNotesLink(t *testing.T) {
	app, account, contact, session := startEcommerce(t, twoProducts(nil), nil)
	messageID := uuid.New()
	setCheckoutState(session, &checkoutState{
		Step:         "capture",
		Flow:         checkoutFlowPostCart,
		CaptureIndex: 0,
		CaptureFields: []map[string]any{
			{"key": "reference_image", "label": "Reference photo", "type": "image"},
			{"key": "writing", "label": "Cake writing", "type": "text"},
		},
		NewAddress: map[string]any{},
	})
	handled := app.handleCheckoutConversation(account, contact, session, &models.ChatbotSettings{}, "[Image attached]", "", &models.Message{
		BaseModel:     models.BaseModel{ID: messageID},
		MediaURL:      "images/ref.jpg",
		MediaMimeType: "image/jpeg",
	})
	require.True(t, handled)
	st := getCheckoutState(session)
	require.NotNil(t, st)
	assert.Equal(t, 1, st.CaptureIndex)
	link := "/api/media/" + messageID.String()
	assert.Contains(t, checkoutNotes(session), link)
	assert.Contains(t, outgoingBlob(t, app, session), "Cake writing")

	setCheckoutState(session, &checkoutState{
		Step:         "capture",
		Flow:         checkoutFlowPostCart,
		CaptureIndex: 0,
		CaptureFields: []map[string]any{
			{"key": "reference_image", "label": "Reference photo", "type": "image"},
		},
		NewAddress: map[string]any{},
	})
	handled = app.handleCheckoutConversation(account, contact, session, &models.ChatbotSettings{}, "[Image attached]", "", &models.Message{})
	require.True(t, handled)
	st = getCheckoutState(session)
	require.NotNil(t, st)
	assert.Equal(t, 0, st.CaptureIndex)
	retry := outgoingBlob(t, app, session)
	assert.Contains(t, retry, "Please send the photo again.")
	assert.Contains(t, retry, "Please send a photo.")
}
