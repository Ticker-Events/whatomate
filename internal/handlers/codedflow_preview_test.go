package handlers

// preview tests live here because they need *App

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func previewLive() *bool {
	live := false
	return &live
}

func previewMessageText(messages []codedflow.CodedPreviewMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(msg.Content)
		b.WriteByte('\n')
		if msg.Interactive != "" {
			b.WriteString(msg.Interactive)
			b.WriteByte('\n')
		}
		for _, button := range msg.Buttons {
			b.WriteString(button.Title)
			b.WriteByte('\n')
			b.WriteString(button.ID)
			b.WriteByte('\n')
			if button.URL != "" {
				b.WriteString(button.URL)
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

func countRows(t *testing.T, app *App, orgID uuid.UUID, accountName string) (messages, sessions, transfers int64) {
	t.Helper()
	require.NoError(t, app.DB.Model(&models.Message{}).Where("whats_app_account = ?", accountName).Count(&messages).Error)
	require.NoError(t, app.DB.Model(&models.ChatbotSession{}).Where("organization_id = ?", orgID).Count(&sessions).Error)
	require.NoError(t, app.DB.Model(&models.AgentTransfer{}).Where("whats_app_account = ?", accountName).Count(&transfers).Error)
	return messages, sessions, transfers
}

func TestPreviewCodedFlow_MenuDoesNotSend(t *testing.T) {
	useCodedIntent(t, nil, nil)
	useCodedTranslate(t, nil)
	srv := newStoreServer(t, twoProducts(nil), nil)
	useStoreREST(t, srv)
	app, org, account, _, _ := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	beforeMsg, beforeSessions, beforeTransfers := countRows(t, app, org.ID, account.Name)

	resp, err := app.previewCodedTurn(org.ID, uuid.New(), account.Name, tiqrecommerce.FlowKey, codedPreviewInput{Mock: previewLive()})
	require.NoError(t, err)
	assert.Equal(t, "waiting_input", resp.Status)
	assert.Equal(t, "intent", resp.Step)
	assert.Equal(t, "button", resp.Input)
	assert.Contains(t, previewMessageText(resp.Messages), "Buy products")
	assert.NotEmpty(t, resp.SessionID)
	require.NotEmpty(t, resp.Messages)
	assert.NotNil(t, resp.Messages[0].Context)
	assert.Contains(t, resp.Messages[0].Context, "store")
	assert.Contains(t, resp.Messages[0].Context, "collections")
	assert.NotNil(t, resp.Context)
	assert.Equal(t, "intent", resp.Context["_current_step"])
	assert.NotNil(t, resp.AICalls)
	require.Len(t, resp.APICalls, 2)
	assert.Equal(t, "get_store", resp.APICalls[0].Name)
	assert.Equal(t, "/service/buyer/store/42/", resp.APICalls[0].Path)
	assert.Contains(t, resp.APICalls[0].Curl, "curl -sS -X GET")
	assert.Contains(t, resp.APICalls[0].Curl, "/service/buyer/store/42/")
	assert.Equal(t, 200, resp.APICalls[0].HTTPStatus)
	require.NotNil(t, resp.APICalls[0].Response)
	storeResp, ok := resp.APICalls[0].Response.(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, storeResp["name"])
	assert.Equal(t, "list_collections", resp.APICalls[1].Name)
	assert.Contains(t, resp.APICalls[1].Path, "/service/buyer/store/42/category/")
	assert.Contains(t, resp.APICalls[1].Curl, "curl -sS -X GET")
	assert.Contains(t, resp.APICalls[1].Curl, resp.APICalls[1].Path)

	var stored models.ChatbotSession
	err = app.DB.First(&stored, "id = ?", resp.SessionID).Error
	assert.Error(t, err)

	afterMsg, afterSessions, afterTransfers := countRows(t, app, org.ID, account.Name)
	assert.Equal(t, beforeMsg, afterMsg)
	assert.Equal(t, beforeSessions, afterSessions)
	assert.Equal(t, beforeTransfers, afterTransfers)
}

func TestPreviewCodedFlow_ButtonAdvances(t *testing.T) {
	useCodedIntent(t, nil, nil)
	useCodedTranslate(t, nil)
	srv := newStoreServer(t, twoProducts(nil), nil)
	useStoreREST(t, srv)
	app, org, account, _, _ := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	userID := uuid.New()

	first, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrecommerce.FlowKey, codedPreviewInput{Mock: previewLive()})
	require.NoError(t, err)
	sessionID, err := uuid.Parse(first.SessionID)
	require.NoError(t, err)
	require.Len(t, first.APICalls, 2)

	next, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrecommerce.FlowKey, codedPreviewInput{
		SessionID: sessionID,
		Text:      "Buy products",
		ButtonID:  tiqrecommerce.BuyProducts,
		Mock:      previewLive(),
	})
	require.NoError(t, err)
	assert.Equal(t, "waiting_input", next.Status)
	assert.Equal(t, "collection", next.Step)
	assert.Equal(t, "button", next.Input)
	assert.Equal(t, "list", next.Messages[0].Interactive)
	assert.Contains(t, previewMessageText(next.Messages), "Sweets")
	assert.Empty(t, next.APICalls)
}

func TestCodedPreviewRedisRoundTripIgnoresProcessMemory(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	app := &App{Redis: rdb}

	orgID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()
	contactID := uuid.New()
	hold := &codedPreviewHold{
		orgID:       orgID,
		userID:      userID,
		flowKey:     tiqrecommerce.FlowKey,
		accountName: "shop",
		Mock:        true,
		contact: &models.Contact{
			BaseModel:      models.BaseModel{ID: contactID},
			OrganizationID: orgID,
			PhoneNumber:    codedPreviewPhone,
			ProfileName:    "Preview",
		},
		session: &models.ChatbotSession{
			BaseModel:       models.BaseModel{ID: sessionID},
			OrganizationID:  orgID,
			ContactID:       contactID,
			WhatsAppAccount: "shop",
			PhoneNumber:     codedPreviewPhone,
			Status:          models.SessionStatusActive,
			CurrentStep:     "collection",
			SessionData: models.JSONB{
				"collections": []map[string]any{{"id": "57", "name": "Sweets"}},
				"store":       map[string]any{"id": "42"},
			},
		},
	}
	require.NoError(t, app.saveCodedPreview(hold))

	codedPreviewStore.Lock()
	delete(codedPreviewStore.items, sessionID)
	codedPreviewStore.Unlock()

	loaded, err := app.loadCodedPreview(sessionID)
	require.NoError(t, err)
	require.True(t, previewHoldMatches(loaded, orgID, userID, tiqrecommerce.FlowKey, "shop"))
	assert.Equal(t, "collection", loaded.session.CurrentStep)
	assert.Equal(t, codedPreviewPhone, loaded.contact.PhoneNumber)
	items, ok := anySlice(loaded.session.SessionData["collections"])
	require.True(t, ok)
	require.Len(t, items, 1)
	rec, ok := asStringMap(items[0])
	require.True(t, ok)
	assert.Equal(t, "Sweets", asString(rec["name"]))
	assert.False(t, previewHoldMatches(loaded, orgID, uuid.New(), tiqrecommerce.FlowKey, "shop"))
}

func TestPreviewCodedFlow_SessionSurvivesEmptyProcessMemory(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	useCodedIntent(t, nil, nil)
	useCodedTranslate(t, nil)
	srv := newStoreServer(t, twoProducts(nil), nil)
	useStoreREST(t, srv)
	app, org, account, _, _ := newGraphTestFixtures(t)
	app.Redis = rdb
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	userID := uuid.New()

	first, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrecommerce.FlowKey, codedPreviewInput{Mock: previewLive()})
	require.NoError(t, err)
	sessionID, err := uuid.Parse(first.SessionID)
	require.NoError(t, err)
	require.True(t, mr.Exists(codedPreviewRedisKey(sessionID)))

	codedPreviewStore.Lock()
	delete(codedPreviewStore.items, sessionID)
	codedPreviewStore.Unlock()

	next, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrecommerce.FlowKey, codedPreviewInput{
		SessionID: sessionID,
		Text:      "Buy products",
		ButtonID:  tiqrecommerce.BuyProducts,
		Mock:      previewLive(),
	})
	require.NoError(t, err)
	assert.Equal(t, "waiting_input", next.Status)
	assert.Equal(t, "collection", next.Step)
}

func TestPreviewCodedFlow_IncludesAIDetails(t *testing.T) {
	useCodedIntent(t, func(string, codedflow.IntentContext) (codedflow.IntentResult, error) {
		return codedflow.IntentResult{
			Language:   "en",
			Route:      codedflow.RouteChoice,
			ChoiceID:   tiqrecommerce.BuyProducts,
			Confidence: 0.93,
		}, nil
	}, nil)
	useCodedTranslate(t, nil)
	srv := newStoreServer(t, twoProducts(nil), nil)
	useStoreREST(t, srv)
	app, org, account, _, _ := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	userID := uuid.New()

	first, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrecommerce.FlowKey, codedPreviewInput{Mock: previewLive()})
	require.NoError(t, err)
	sessionID, err := uuid.Parse(first.SessionID)
	require.NoError(t, err)

	next, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrecommerce.FlowKey, codedPreviewInput{
		SessionID: sessionID,
		Text:      "I want to buy something",
		Mock:      previewLive(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, next.AICalls)
	assert.Equal(t, "intent", next.AICalls[0].Role)
	assert.Equal(t, codedflow.RouteChoice, next.AICalls[0].Route)
	assert.InDelta(t, 0.93, next.AICalls[0].Confidence, 0.001)
	require.NotNil(t, next.AICalls[0].Grounded)
	assert.True(t, *next.AICalls[0].Grounded)
	require.NotEmpty(t, next.Messages)
	foundAI := false
	for _, msg := range next.Messages {
		if len(msg.AI) > 0 {
			foundAI = true
			assert.Equal(t, "intent", msg.AI[0].Role)
			break
		}
	}
	assert.True(t, foundAI)
}

func TestPreviewCodedFlow_MockAsksBeforeTiqrCalls(t *testing.T) {
	useCodedIntent(t, nil, nil)
	useCodedTranslate(t, nil)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)

	app, org, account, _, _ := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	userID := uuid.New()

	first, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrecommerce.FlowKey, codedPreviewInput{})
	require.NoError(t, err)
	assert.Equal(t, "needs_mock", first.Status)
	assert.Equal(t, "get_store", first.MockOperation)
	assert.Zero(t, hits)

	sessionID, err := uuid.Parse(first.SessionID)
	require.NoError(t, err)
	second, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrecommerce.FlowKey, codedPreviewInput{
		SessionID: sessionID,
		Mocks: map[string]any{
			"get_store": map[string]any{"id": 42, "name": "Demo"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "needs_mock", second.Status)
	assert.Equal(t, "list_collections", second.MockOperation)
	assert.Zero(t, hits)
}

func TestPreviewCodedFlow_MockCreateOrderIsNotSent(t *testing.T) {
	useCodedIntent(t, nil, nil)
	useCodedTranslate(t, nil)
	var orders int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/order") {
			orders++
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)

	app, org, account, _, _ := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	userID := uuid.New()
	var sessionID uuid.UUID
	turn := func(in codedPreviewInput) codedflow.CodedPreviewResponse {
		t.Helper()
		if sessionID != uuid.Nil {
			in.SessionID = sessionID
		}
		resp, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrecommerce.FlowKey, in)
		require.NoError(t, err)
		id, err := uuid.Parse(resp.SessionID)
		require.NoError(t, err)
		sessionID = id
		return resp
	}
	supply := func(operation string, payload map[string]any) codedflow.CodedPreviewResponse {
		t.Helper()
		resp := turn(codedPreviewInput{Mocks: map[string]any{operation: payload}})
		require.NotEqual(t, operation, resp.MockOperation, previewMessageText(resp.Messages))
		return resp
	}

	asked := turn(codedPreviewInput{})
	require.Equal(t, "get_store", asked.MockOperation)
	supply("get_store", map[string]any{"id": 42, "name": "Demo"})
	menu := supply("list_collections", map[string]any{
		"results": []any{map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"}},
	})
	require.Equal(t, "waiting_input", menu.Status)

	turn(codedPreviewInput{Text: "Buy products", ButtonID: tiqrecommerce.BuyProducts})
	asked = turn(codedPreviewInput{Text: "Sweets", ButtonID: "57"})
	require.Equal(t, "list_products", asked.MockOperation)
	supply("list_products", map[string]any{
		"results": twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}}),
	})
	turn(codedPreviewInput{Text: "Kunafa", ButtonID: "101"})
	turn(codedPreviewInput{Text: "1"})
	turn(codedPreviewInput{Text: "Checkout", ButtonID: tiqrecommerce.Checkout})
	turn(codedPreviewInput{Text: "Confirm items", ButtonID: tiqrecommerce.ConfirmItems})
	asked = turn(codedPreviewInput{FlowResponse: map[string]any{"customer_name": "Preview Customer"}})
	require.Equal(t, "create_order", asked.MockOperation)
	done := turn(codedPreviewInput{Mocks: map[string]any{
		"create_order": map[string]any{
			"id":          "preview-order",
			"display_uid": "TQ-PREVIEW",
			"amount":      40.0,
			"payment_url": "https://pay.example/preview",
		},
	}})
	assert.Equal(t, "completed", done.Status)
	blob := previewMessageText(done.Messages)
	assert.Contains(t, blob, "Order placed!")
	assert.Contains(t, blob, "TQ-PREVIEW")
	assert.Contains(t, blob, "cta_url")
	assert.Contains(t, blob, "Pay now")
	assert.Contains(t, blob, "https://pay.example/preview")
	assert.NotContains(t, blob, "Pay here:")
	assert.NotContains(t, blob, "order is confirmed")
	assert.Zero(t, orders)
}

func TestPreviewCodedFlow_LiveCreateOrderIsSent(t *testing.T) {
	useCodedIntent(t, nil, nil)
	useCodedTranslate(t, nil)
	var orders int
	var orderBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/order"):
			orders++
			require.NoError(t, json.NewDecoder(r.Body).Decode(&orderBody))
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "real-order", "display_uid": "TQ-LIVE"})
		case strings.Contains(r.URL.Path, "/category/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []any{
					map[string]any{"id": "57", "name": "Sweets", "description": "Desserts"},
				},
			})
		case strings.Contains(r.URL.Path, "/product/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count":   2,
				"results": twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}}),
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "Demo"})
		}
	}))
	t.Cleanup(srv.Close)
	useStoreREST(t, srv)

	app, org, account, _, _ := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	userID := uuid.New()
	turn := func(in codedPreviewInput) codedflow.CodedPreviewResponse {
		t.Helper()
		resp, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrecommerce.FlowKey, in)
		require.NoError(t, err)
		require.NotEqual(t, "error", resp.Status, previewMessageText(resp.Messages))
		if in.SessionID == uuid.Nil {
			id, err := uuid.Parse(resp.SessionID)
			require.NoError(t, err)
			in.SessionID = id
		}
		return resp
	}

	started := turn(codedPreviewInput{Mock: previewLive()})
	sessionID, err := uuid.Parse(started.SessionID)
	require.NoError(t, err)
	turn(codedPreviewInput{SessionID: sessionID, Text: "Buy products", ButtonID: tiqrecommerce.BuyProducts})
	turn(codedPreviewInput{SessionID: sessionID, Text: "Sweets", ButtonID: "57"})
	turn(codedPreviewInput{SessionID: sessionID, Text: "Kunafa", ButtonID: "101"})
	turn(codedPreviewInput{SessionID: sessionID, Text: "1"})
	turn(codedPreviewInput{SessionID: sessionID, Text: "Checkout", ButtonID: tiqrecommerce.Checkout})
	turn(codedPreviewInput{SessionID: sessionID, Text: "Confirm items", ButtonID: tiqrecommerce.ConfirmItems})
	done := turn(codedPreviewInput{
		SessionID: sessionID,
		FlowResponse: map[string]any{
			"customer_name":  "Preview Customer",
			"customer_phone": "910000000000",
			"customer_email": "preview@example.com",
			"customer_notes": "Pickup note",
		},
	})

	assert.Equal(t, "completed", done.Status)
	blob := previewMessageText(done.Messages)
	assert.Contains(t, blob, "Order placed!")
	assert.Contains(t, blob, "TQ-LIVE")
	assert.NotContains(t, blob, "order is confirmed")
	assert.Equal(t, 1, orders)
	assert.Equal(t, "preview@example.com", orderBody["email"])
	assert.Equal(t, "910000000000", orderBody["phone_number"])
	assert.Equal(t, "Pickup note", orderBody["notes"])
	assert.Equal(t, "PICKUP_FROM_STORE", orderBody["delivery_mode"])
	_, hasAddress := orderBody["new_address"]
	assert.False(t, hasAddress)
	meta, ok := orderBody["buyer_meta_data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Preview Customer", meta["name"])
	assert.Equal(t, "preview@example.com", meta["email"])
	assert.Equal(t, "910000000000", meta["phone"])
	assert.Equal(t, "910000000000", meta["phone_number"])
	assert.Equal(t, "Pickup note", meta["notes"])

	_, _, transfers := countRows(t, app, org.ID, account.Name)
	assert.Zero(t, transfers)
}
