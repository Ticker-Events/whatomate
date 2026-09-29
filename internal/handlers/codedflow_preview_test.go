package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func previewLive() *bool {
	live := false
	return &live
}

func previewMessageText(messages []CodedPreviewMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(msg.Content)
		b.WriteByte('\n')
		for _, button := range msg.Buttons {
			b.WriteString(button.Title)
			b.WriteByte('\n')
			b.WriteString(button.ID)
			b.WriteByte('\n')
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
	useCodedLanguage(t, nil, nil)
	useCodedDiversion(t, nil)
	srv := newStoreServer(t, twoProducts(nil), nil)
	useStoreREST(t, srv)
	app, org, account, _, _ := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	beforeMsg, beforeSessions, beforeTransfers := countRows(t, app, org.ID, account.Name)

	resp, err := app.previewCodedTurn(org.ID, uuid.New(), account.Name, tiqrEcommerceKey, codedPreviewInput{Mock: previewLive()})
	require.NoError(t, err)
	assert.Equal(t, "waiting_input", resp.Status)
	assert.Equal(t, "intent", resp.Step)
	assert.Equal(t, "button", resp.Input)
	assert.Contains(t, previewMessageText(resp.Messages), "Buy products")
	assert.NotEmpty(t, resp.SessionID)

	var stored models.ChatbotSession
	err = app.DB.First(&stored, "id = ?", resp.SessionID).Error
	assert.Error(t, err)

	afterMsg, afterSessions, afterTransfers := countRows(t, app, org.ID, account.Name)
	assert.Equal(t, beforeMsg, afterMsg)
	assert.Equal(t, beforeSessions, afterSessions)
	assert.Equal(t, beforeTransfers, afterTransfers)
}

func TestPreviewCodedFlow_ButtonAdvances(t *testing.T) {
	useCodedLanguage(t, nil, nil)
	useCodedDiversion(t, nil)
	srv := newStoreServer(t, twoProducts(nil), nil)
	useStoreREST(t, srv)
	app, org, account, _, _ := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})
	userID := uuid.New()

	first, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrEcommerceKey, codedPreviewInput{Mock: previewLive()})
	require.NoError(t, err)
	sessionID, err := uuid.Parse(first.SessionID)
	require.NoError(t, err)

	next, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrEcommerceKey, codedPreviewInput{
		SessionID: sessionID,
		Text:      "Buy products",
		ButtonID:  tiqrBuyProducts,
		Mock:      previewLive(),
	})
	require.NoError(t, err)
	assert.Equal(t, "waiting_input", next.Status)
	assert.Equal(t, "collection", next.Step)
	assert.Equal(t, "button", next.Input)
	assert.Equal(t, "list", next.Messages[0].Interactive)
	assert.Contains(t, previewMessageText(next.Messages), "Sweets")
}

func TestPreviewCodedFlow_MockAsksBeforeTiqrCalls(t *testing.T) {
	useCodedLanguage(t, nil, nil)
	useCodedDiversion(t, nil)
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

	first, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrEcommerceKey, codedPreviewInput{})
	require.NoError(t, err)
	assert.Equal(t, "needs_mock", first.Status)
	assert.Equal(t, "get_store", first.MockOperation)
	assert.Zero(t, hits)

	sessionID, err := uuid.Parse(first.SessionID)
	require.NoError(t, err)
	second, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrEcommerceKey, codedPreviewInput{
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
	useCodedLanguage(t, nil, nil)
	useCodedDiversion(t, nil)
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
	turn := func(in codedPreviewInput) CodedPreviewResponse {
		t.Helper()
		if sessionID != uuid.Nil {
			in.SessionID = sessionID
		}
		resp, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrEcommerceKey, in)
		require.NoError(t, err)
		id, err := uuid.Parse(resp.SessionID)
		require.NoError(t, err)
		sessionID = id
		return resp
	}
	supply := func(operation string, payload map[string]any) CodedPreviewResponse {
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

	turn(codedPreviewInput{Text: "Buy products", ButtonID: tiqrBuyProducts})
	asked = turn(codedPreviewInput{Text: "Sweets", ButtonID: "57"})
	require.Equal(t, "list_products", asked.MockOperation)
	supply("list_products", map[string]any{
		"results": twoProducts([]any{map[string]any{"id": "9", "name": "Regular", "price": "40"}}),
	})
	turn(codedPreviewInput{Text: "Kunafa", ButtonID: "101"})
	turn(codedPreviewInput{Text: "1"})
	turn(codedPreviewInput{Text: "Checkout", ButtonID: tiqrCheckout})
	asked = turn(codedPreviewInput{FlowResponse: map[string]any{"customer_name": "Preview Customer"}})
	require.Equal(t, "create_order", asked.MockOperation)
	done := turn(codedPreviewInput{Mocks: map[string]any{
		"create_order": map[string]any{"id": "preview-order"},
	}})
	assert.Equal(t, "completed", done.Status)
	assert.Contains(t, previewMessageText(done.Messages), "Your order is confirmed.")
	assert.Zero(t, orders)
}

func TestPreviewCodedFlow_LiveCreateOrderIsSent(t *testing.T) {
	useCodedLanguage(t, nil, nil)
	useCodedDiversion(t, nil)
	var orders int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/order"):
			orders++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "real-order"})
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
	turn := func(in codedPreviewInput) CodedPreviewResponse {
		t.Helper()
		resp, err := app.previewCodedTurn(org.ID, userID, account.Name, tiqrEcommerceKey, in)
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
	turn(codedPreviewInput{SessionID: sessionID, Text: "Buy products", ButtonID: tiqrBuyProducts})
	turn(codedPreviewInput{SessionID: sessionID, Text: "Sweets", ButtonID: "57"})
	turn(codedPreviewInput{SessionID: sessionID, Text: "Kunafa", ButtonID: "101"})
	turn(codedPreviewInput{SessionID: sessionID, Text: "1"})
	turn(codedPreviewInput{SessionID: sessionID, Text: "Checkout", ButtonID: tiqrCheckout})
	done := turn(codedPreviewInput{
		SessionID: sessionID,
		FlowResponse: map[string]any{
			"customer_name":    "Preview Customer",
			"customer_phone":   "910000000000",
			"customer_email":   "preview@example.com",
			"address_line_one": "1 Preview Street",
			"city":             "Bengaluru",
			"state":            "KA",
			"country":          "India",
			"pincode":          "560001",
		},
	})

	assert.Equal(t, "completed", done.Status)
	assert.Contains(t, previewMessageText(done.Messages), "Your order is confirmed.")
	assert.Equal(t, 1, orders)

	_, _, transfers := countRows(t, app, org.ID, account.Name)
	assert.Zero(t, transfers)
}
