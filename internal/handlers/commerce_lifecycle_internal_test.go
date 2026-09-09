package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/config"
	appcrypto "github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/shridarpatil/whatomate/internal/middleware"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

func fixedLifecycleEnvelope(eventType string) commerceLifecycleEnvelope {
	return commerceLifecycleEnvelope{
		Version:       "1",
		EventID:       uuid.MustParse("93d45ef0-5a80-47bd-8fd1-719552cb87b8"),
		EventType:     eventType,
		OccurredAt:    time.Date(2026, 9, 9, 7, 30, 0, 123456000, time.UTC),
		StoreID:       42,
		OrderID:       "ord_123",
		CorrelationID: uuid.MustParse("62baf621-56bd-4807-9f32-0b143f7fd3f0"),
		CausationID:   nil,
		Capabilities: map[string]any{
			"contract_version": "1",
			"capabilities":     map[string]any{"lifecycle_events": true},
		},
		Conversation: &lifecycleConversation{PhoneNumber: "919999999999", WhatsAppAccount: "shop"},
		Data:         map[string]any{"status": "CONFIRMED", "previous_status": "PENDING"},
	}
}

func TestCanonicalLifecycleSignature(t *testing.T) {
	payload := fixedLifecycleEnvelope("order.confirmed")
	body, err := canonicalLifecycleJSON(payload)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"version":"1","event_id":"93d45ef0-5a80-47bd-8fd1-719552cb87b8",
		"event_type":"order.confirmed","occurred_at":"2026-09-09T07:30:00.123456Z",
		"store_id":42,"order_id":"ord_123",
		"correlation_id":"62baf621-56bd-4807-9f32-0b143f7fd3f0","causation_id":null,
		"capabilities":{"capabilities":{"lifecycle_events":true},"contract_version":"1"},
		"conversation":{"phone_number":"919999999999","whatsapp_account":"shop"},
		"data":{"previous_status":"PENDING","status":"CONFIRMED"}}`, string(body))

	const secret = "independent-lifecycle-secret"
	const timestamp = int64(1788939000)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	_, _ = mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	assert.True(t, validLifecycleSignature(secret, timestamp, body, signature))
	assert.False(t, validLifecycleSignature(secret, timestamp, append(body, ' '), signature), "body tampering must fail")
	assert.False(t, validLifecycleSignature("wrong", timestamp, body, signature))
	assert.False(t, validLifecycleSignature(secret, timestamp+1, body, signature))
}

func TestLifecycleEventMappings(t *testing.T) {
	expected := map[string][3]string{
		"payment.succeeded": {"", "succeeded", "confirmed"},
		"payment.failed":    {"", "failed", "pending_payment"},
		"order.confirmed":   {"confirmed", "", "confirmed"},
		"order.assigned":    {"assigned", "", "assigned"},
		"order.preparing":   {"preparing", "", "preparing"},
		"order.ready":       {"ready", "", "ready"},
		"order.dispatched":  {"dispatched", "", "dispatched"},
		"order.delivered":   {"delivered", "", "delivered"},
		"order.cancelled":   {"cancelled", "", "cancelled"},
		"order.completed":   {"completed", "", "completed"},
	}
	require.Len(t, lifecycleEventMessages, len(expected))
	for eventType, want := range expected {
		t.Run(eventType, func(t *testing.T) {
			require.NoError(t, validateLifecycleEnvelope(fixedLifecycleEnvelope(eventType)))
			order, payment, draft := lifecycleState(eventType)
			assert.Equal(t, want, [3]string{order, payment, draft})
			assert.NotEmpty(t, lifecycleEventMessages[eventType])
		})
	}
}

func TestLifecycleRegressionRules(t *testing.T) {
	assert.True(t, lifecycleWouldRegress(
		models.CommerceDraft{BackendOrderStatus: "dispatched"}, "order.preparing",
	))
	assert.True(t, lifecycleWouldRegress(
		models.CommerceDraft{BackendPaymentStatus: "succeeded"}, "payment.failed",
	))
	assert.True(t, lifecycleWouldRegress(
		models.CommerceDraft{BackendOrderStatus: "cancelled"}, "order.ready",
	))
	assert.False(t, lifecycleWouldRegress(
		models.CommerceDraft{BackendOrderStatus: "preparing"}, "order.ready",
	))
}

func TestLifecyclePositionOrdering(t *testing.T) {
	occurredAt := time.Now().UTC()
	lowID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	highID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	confirmed := &models.CommerceLifecycleEvent{
		EventID: lowID, EventType: "order.confirmed", OccurredAt: occurredAt,
	}
	preparing := &models.CommerceLifecycleEvent{
		EventID: highID, EventType: "order.preparing", OccurredAt: occurredAt,
	}
	assert.Negative(t, compareLifecyclePosition(confirmed, preparing))
	assert.Positive(t, compareLifecyclePosition(preparing, confirmed))

	sameTypeLaterID := &models.CommerceLifecycleEvent{
		EventID: highID, EventType: "order.confirmed", OccurredAt: occurredAt,
	}
	assert.Negative(t, compareLifecyclePosition(confirmed, sameTypeLaterID))

	firstSequence, secondSequence := int64(10), int64(11)
	confirmed.Sequence = &secondSequence
	preparing.Sequence = &firstSequence
	assert.Positive(t, compareLifecyclePosition(confirmed, preparing), "backend sequence must take precedence")
}

func TestLifecycleReceiverRequiresAPIKey(t *testing.T) {
	req := testutil.NewRequest(t)
	req.RequestCtx.Request.Header.SetMethod(fasthttp.MethodPost)
	req.RequestCtx.Request.Header.SetContentType("application/json")
	app := &App{}
	require.NoError(t, app.ReceiveCommerceLifecycleEvent(req))
	assert.Equal(t, fasthttp.StatusUnauthorized, testutil.GetResponseStatusCode(req))
	assert.Contains(t, string(testutil.GetResponseBody(req)), "API_KEY_REQUIRED")
}

func signedLifecycleRequest(t *testing.T, payload commerceLifecycleEnvelope, secret string, timestamp int64) *fastglue.Request {
	t.Helper()
	body, err := canonicalLifecycleJSON(payload)
	require.NoError(t, err)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	_, _ = mac.Write(body)
	req := testutil.NewRequest(t)
	req.RequestCtx.Request.Header.SetMethod(fasthttp.MethodPost)
	req.RequestCtx.Request.Header.SetContentType("application/json")
	req.RequestCtx.Request.Header.Set("Idempotency-Key", payload.EventID.String())
	req.RequestCtx.Request.Header.Set("X-Event-ID", payload.EventID.String())
	req.RequestCtx.Request.Header.Set("X-Correlation-ID", payload.CorrelationID.String())
	req.RequestCtx.Request.Header.Set("X-TiQR-Timestamp", fmt.Sprint(timestamp))
	req.RequestCtx.Request.Header.Set("X-TiQR-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.RequestCtx.Request.SetBody(body)
	return req
}

func lifecycleReceiverApp(t *testing.T, payload commerceLifecycleEnvelope, secret string) (*App, uuid.UUID, uuid.UUID) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	keyID := uuid.New()
	encrypted, err := appcrypto.Encrypt(secret, "test-encryption-key")
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.CommerceLifecycleConfig{
		OrganizationID: org.ID, StoreID: fmt.Sprint(payload.StoreID),
		WhatsAppAccount: payload.Conversation.WhatsAppAccount,
		APIKeyID:        keyID, SigningSecret: encrypted, Enabled: true,
	}).Error)
	return &App{DB: db, Config: &config.Config{App: config.AppConfig{EncryptionKey: "test-encryption-key"}}, Log: testutil.NopLogger()}, org.ID, keyID
}

func TestLifecycleReceiverRejectsSkewTamperAndHeaderMismatch(t *testing.T) {
	payload := fixedLifecycleEnvelope("order.confirmed")
	const secret = "receiver-secret"
	app, orgID, keyID := lifecycleReceiverApp(t, payload, secret)
	setContext := func(req *fastglue.Request) {
		req.RequestCtx.SetUserValue(middleware.ContextKeyOrganizationID, orgID)
		req.RequestCtx.SetUserValue(middleware.ContextKeyAPIKeyID, keyID)
	}

	t.Run("skew", func(t *testing.T) {
		req := signedLifecycleRequest(t, payload, secret, time.Now().Add(-10*time.Minute).Unix())
		setContext(req)
		require.NoError(t, app.ReceiveCommerceLifecycleEvent(req))
		assert.Equal(t, fasthttp.StatusUnauthorized, testutil.GetResponseStatusCode(req))
		assert.Contains(t, string(testutil.GetResponseBody(req)), "TIMESTAMP_OUT_OF_RANGE")
	})
	t.Run("tamper", func(t *testing.T) {
		req := signedLifecycleRequest(t, payload, secret, time.Now().Unix())
		req.RequestCtx.Request.SetBodyString(strings.ReplaceAll(string(req.RequestCtx.PostBody()), "order.confirmed", "order.preparing"))
		setContext(req)
		require.NoError(t, app.ReceiveCommerceLifecycleEvent(req))
		assert.Equal(t, fasthttp.StatusUnauthorized, testutil.GetResponseStatusCode(req))
		assert.Contains(t, string(testutil.GetResponseBody(req)), "INVALID_SIGNATURE")
	})
	t.Run("header mismatch", func(t *testing.T) {
		req := signedLifecycleRequest(t, payload, secret, time.Now().Unix())
		req.RequestCtx.Request.Header.Set("X-Event-ID", uuid.NewString())
		setContext(req)
		require.NoError(t, app.ReceiveCommerceLifecycleEvent(req))
		assert.Equal(t, fasthttp.StatusBadRequest, testutil.GetResponseStatusCode(req))
		assert.Contains(t, string(testutil.GetResponseBody(req)), "HEADER_MISMATCH")
	})
}

func TestLifecycleReceiverStoreAndAPIKeyIsolation(t *testing.T) {
	payload := fixedLifecycleEnvelope("order.confirmed")
	const secret = "isolation-secret"
	app, orgID, keyID := lifecycleReceiverApp(t, payload, secret)
	for name, mutate := range map[string]func(*fastglue.Request){
		"wrong api key": func(req *fastglue.Request) {
			req.RequestCtx.SetUserValue(middleware.ContextKeyAPIKeyID, uuid.New())
		},
		"wrong organization": func(req *fastglue.Request) {
			req.RequestCtx.SetUserValue(middleware.ContextKeyOrganizationID, uuid.New())
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := signedLifecycleRequest(t, payload, secret, time.Now().Unix())
			req.RequestCtx.SetUserValue(middleware.ContextKeyOrganizationID, orgID)
			req.RequestCtx.SetUserValue(middleware.ContextKeyAPIKeyID, keyID)
			mutate(req)
			require.NoError(t, app.ReceiveCommerceLifecycleEvent(req))
			assert.Equal(t, fasthttp.StatusUnauthorized, testutil.GetResponseStatusCode(req))
		})
	}
}

func TestLifecycleClaimReplayBeforeAndAfterFailure(t *testing.T) {
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	app := &App{DB: db}
	payload := fixedLifecycleEnvelope("order.confirmed")
	payload.EventID = uuid.New()
	payload.CorrelationID = uuid.New()
	config := models.CommerceLifecycleConfig{
		OrganizationID: org.ID, StoreID: "42", WhatsAppAccount: "shop",
	}
	canonicalBytes, err := canonicalLifecycleJSON(payload)
	require.NoError(t, err)
	var canonical models.JSONB
	require.NoError(t, json.Unmarshal(canonicalBytes, &canonical))

	event, duplicate, err := app.claimLifecycleEvent(org.ID, config, payload, canonical)
	require.NoError(t, err)
	assert.False(t, duplicate)
	_, _, err = app.claimLifecycleEvent(org.ID, config, payload, canonical)
	require.Error(t, err, "a concurrent replay must not run side effects")

	require.NoError(t, db.Model(event).Updates(map[string]any{
		"status": models.CommerceLifecycleStatusFailed, "processing_claimed": nil,
	}).Error)
	event, duplicate, err = app.claimLifecycleEvent(org.ID, config, payload, canonical)
	require.NoError(t, err)
	assert.False(t, duplicate, "failed events must be replayable")

	require.NoError(t, db.Model(event).Updates(map[string]any{
		"status": models.CommerceLifecycleStatusProcessed, "processing_claimed": nil,
	}).Error)
	_, duplicate, err = app.claimLifecycleEvent(org.ID, config, payload, canonical)
	require.NoError(t, err)
	assert.True(t, duplicate, "processed events must return idempotent success")
}

func TestLifecycleProcessingUpdatesStateAndSendsOnce(t *testing.T) {
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.lifecycle"}]}`))
	}))
	defer server.Close()

	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	account := testutil.CreateTestWhatsAppAccount(t, db, org.ID)
	contact := testutil.CreateTestContactWith(t, db, org.ID, testutil.WithContactAccount(account.Name))
	payload := fixedLifecycleEnvelope("order.preparing")
	payload.EventID = uuid.New()
	payload.CorrelationID = uuid.New()
	payload.StoreID = 9123
	payload.OrderID = "order-atomic"
	payload.OrderDisplayID = "ORDER-42"
	payload.Conversation = &lifecycleConversation{
		PhoneNumber: strings.TrimPrefix(contact.PhoneNumber, "+"), WhatsAppAccount: account.Name,
	}
	draft := &models.CommerceDraft{
		OrganizationID: org.ID, ContactID: contact.ID, WhatsAppAccount: account.Name,
		StoreID: "9123", Status: "confirmed", Version: 1, BackendOrderID: payload.OrderID,
		Cart: models.JSONB{}, Addons: models.JSONBArray{}, CapturedFields: models.JSONB{},
		Attachments: models.JSONBArray{}, Notes: models.JSONB{}, AddressSnapshot: models.JSONB{},
	}
	require.NoError(t, db.Create(draft).Error)
	event := &models.CommerceLifecycleEvent{
		EventID: payload.EventID, OrganizationID: org.ID, StoreID: draft.StoreID,
		WhatsAppAccount: account.Name, EventType: payload.EventType, Version: "1",
		OrderID: payload.OrderID, CorrelationID: payload.CorrelationID,
		OccurredAt: payload.OccurredAt, ReceivedAt: time.Now(),
		CanonicalPayload: models.JSONB{}, Status: models.CommerceLifecycleStatusProcessing,
	}
	require.NoError(t, db.Create(event).Error)
	app := &App{DB: db, Log: testutil.NopLogger(), WhatsApp: whatsapp.NewWithBaseURL(testutil.NopLogger(), server.URL)}

	require.NoError(t, app.processLifecycleEvent(event, payload))
	require.NoError(t, app.processLifecycleEvent(event, payload))
	assert.EqualValues(t, 1, sends.Load(), "event replay must reuse the lifecycle message")

	var persistedDraft models.CommerceDraft
	require.NoError(t, db.First(&persistedDraft, "id = ?", draft.ID).Error)
	assert.Equal(t, "preparing", persistedDraft.Status)
	assert.Equal(t, "preparing", persistedDraft.BackendOrderStatus)
	var persistedEvent models.CommerceLifecycleEvent
	require.NoError(t, db.First(&persistedEvent, "id = ?", event.ID).Error)
	assert.Equal(t, models.CommerceLifecycleStatusProcessed, persistedEvent.Status)
	require.NotNil(t, persistedEvent.MessageID)
	var messageCount int64
	require.NoError(t, db.Model(&models.Message{}).Where("lifecycle_event_id = ?", payload.EventID).Count(&messageCount).Error)
	assert.EqualValues(t, 1, messageCount)
}

func TestDurablePendingMessageFailsClosedWithoutRedispatch(t *testing.T) {
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sends.Add(1)
		_, _ = w.Write([]byte(`{"messages":[{"id":"unexpected"}]}`))
	}))
	defer server.Close()
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	account := testutil.CreateTestWhatsAppAccount(t, db, org.ID)
	contact := testutil.CreateTestContactWith(t, db, org.ID, testutil.WithContactAccount(account.Name))
	key := "lifecycle:" + uuid.NewString()
	eventID := uuid.New()
	require.NoError(t, db.Create(&models.Message{
		OrganizationID: org.ID, WhatsAppAccount: account.Name, ContactID: contact.ID,
		Direction: models.DirectionOutgoing, MessageType: models.MessageTypeText,
		Status: models.MessageStatusPending, LifecycleEventID: &eventID, DurableSendKey: &key,
	}).Error)
	app := &App{DB: db, Log: testutil.NopLogger(), WhatsApp: whatsapp.NewWithBaseURL(testutil.NopLogger(), server.URL)}

	_, err := app.SendOutgoingMessage(context.Background(), OutgoingMessageRequest{
		Account: account, Contact: contact, Type: models.MessageTypeText, Content: "status",
		LifecycleEventID: &eventID, DurableSendKey: key,
	}, SLASendOptions())
	require.Error(t, err)
	assert.Zero(t, sends.Load())
}

func TestLifecycleProcessingRecordsDelayedEventAsStale(t *testing.T) {
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	account := testutil.CreateTestWhatsAppAccount(t, db, org.ID)
	contact := testutil.CreateTestContactWith(t, db, org.ID, testutil.WithContactAccount(account.Name))
	newerAt := time.Now().UTC()
	newerID := uuid.New()
	draft := &models.CommerceDraft{
		OrganizationID: org.ID, ContactID: contact.ID, WhatsAppAccount: account.Name,
		StoreID: "stale-store", Status: "preparing", Version: 1, BackendOrderID: "stale-order",
		LastLifecycleAt: &newerAt, LastLifecycleEventID: &newerID,
		Cart: models.JSONB{}, Addons: models.JSONBArray{}, CapturedFields: models.JSONB{},
		Attachments: models.JSONBArray{}, Notes: models.JSONB{}, AddressSnapshot: models.JSONB{},
	}
	require.NoError(t, db.Create(draft).Error)
	payload := fixedLifecycleEnvelope("order.confirmed")
	payload.EventID = uuid.New()
	payload.OrderID = draft.BackendOrderID
	payload.StoreID = 1
	payload.OccurredAt = newerAt.Add(-time.Minute)
	event := &models.CommerceLifecycleEvent{
		EventID: payload.EventID, OrganizationID: org.ID, StoreID: draft.StoreID,
		WhatsAppAccount: account.Name, EventType: payload.EventType, Version: "1",
		OrderID: payload.OrderID, CorrelationID: payload.CorrelationID,
		OccurredAt: payload.OccurredAt, ReceivedAt: time.Now(),
		CanonicalPayload: models.JSONB{}, Status: models.CommerceLifecycleStatusProcessing,
	}
	require.NoError(t, db.Create(event).Error)
	app := &App{DB: db, Log: testutil.NopLogger()}

	require.NoError(t, app.processLifecycleEvent(event, payload))
	var persisted models.CommerceLifecycleEvent
	require.NoError(t, db.First(&persisted, "id = ?", event.ID).Error)
	assert.Equal(t, models.CommerceLifecycleStatusStale, persisted.Status)
	var messages int64
	require.NoError(t, db.Model(&models.Message{}).Where("lifecycle_event_id = ?", event.EventID).Count(&messages).Error)
	assert.Zero(t, messages)
}

func TestLifecycleClaimAllowsOrderedEventsAtSameTimestamp(t *testing.T) {
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	account := testutil.CreateTestWhatsAppAccount(t, db, org.ID)
	contact := testutil.CreateTestContactWith(t, db, org.ID, testutil.WithContactAccount(account.Name))
	occurredAt := time.Now().UTC()
	confirmedID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	preparingID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	draft := &models.CommerceDraft{
		OrganizationID: org.ID, ContactID: contact.ID, WhatsAppAccount: account.Name,
		StoreID: "same-time-store", Status: "confirmed", Version: 1, BackendOrderID: "same-time-order",
		LastLifecycleAt: &occurredAt, LastLifecycleEventID: &confirmedID,
		Cart: models.JSONB{}, Addons: models.JSONBArray{}, CapturedFields: models.JSONB{},
		Attachments: models.JSONBArray{}, Notes: models.JSONB{}, AddressSnapshot: models.JSONB{},
	}
	require.NoError(t, db.Create(draft).Error)
	require.NoError(t, db.Create(&models.CommerceLifecycleEvent{
		EventID: confirmedID, OrganizationID: org.ID, StoreID: draft.StoreID,
		WhatsAppAccount: draft.WhatsAppAccount, EventType: "order.confirmed", Version: "1",
		OrderID: draft.BackendOrderID, CorrelationID: uuid.New(), OccurredAt: occurredAt,
		ReceivedAt: time.Now(), CanonicalPayload: models.JSONB{},
		Status: models.CommerceLifecycleStatusProcessed,
	}).Error)
	next := &models.CommerceLifecycleEvent{
		EventID: preparingID, OrganizationID: org.ID, StoreID: draft.StoreID,
		WhatsAppAccount: draft.WhatsAppAccount, EventType: "order.preparing", Version: "1",
		OrderID: draft.BackendOrderID, CorrelationID: uuid.New(), OccurredAt: occurredAt,
		ReceivedAt: time.Now(), CanonicalPayload: models.JSONB{},
		Status: models.CommerceLifecycleStatusProcessing,
	}
	require.NoError(t, db.Create(next).Error)

	stale, err := (&App{DB: db}).claimLifecyclePosition(next, draft)
	require.NoError(t, err)
	assert.False(t, stale)
}

func TestLifecycleOrderingBlocksRetryableFailureButAllowsDeadLetter(t *testing.T) {
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	account := testutil.CreateTestWhatsAppAccount(t, db, org.ID)
	contact := testutil.CreateTestContactWith(t, db, org.ID, testutil.WithContactAccount(account.Name))
	draft := &models.CommerceDraft{
		OrganizationID: org.ID, ContactID: contact.ID, WhatsAppAccount: account.Name,
		StoreID: "ordered-store", Status: "confirmed", Version: 1, BackendOrderID: "ordered-order",
		Cart: models.JSONB{}, Addons: models.JSONBArray{}, CapturedFields: models.JSONB{},
		Attachments: models.JSONBArray{}, Notes: models.JSONB{}, AddressSnapshot: models.JSONB{},
	}
	require.NoError(t, db.Create(draft).Error)
	earlier := &models.CommerceLifecycleEvent{
		EventID: uuid.New(), OrganizationID: org.ID, StoreID: draft.StoreID,
		WhatsAppAccount: account.Name, EventType: "order.confirmed", Version: "1",
		OrderID: draft.BackendOrderID, CorrelationID: uuid.New(),
		OccurredAt: time.Now().Add(-time.Minute), ReceivedAt: time.Now(),
		CanonicalPayload: models.JSONB{}, Status: models.CommerceLifecycleStatusFailed,
	}
	later := &models.CommerceLifecycleEvent{
		EventID: uuid.New(), OrganizationID: org.ID, StoreID: draft.StoreID,
		WhatsAppAccount: account.Name, EventType: "order.preparing", Version: "1",
		OrderID: draft.BackendOrderID, CorrelationID: uuid.New(),
		OccurredAt: time.Now(), ReceivedAt: time.Now(),
		CanonicalPayload: models.JSONB{}, Status: models.CommerceLifecycleStatusProcessing,
	}
	require.NoError(t, db.Create(earlier).Error)
	require.NoError(t, db.Create(later).Error)
	app := &App{DB: db}

	_, err := app.claimLifecyclePosition(later, draft)
	require.ErrorContains(t, err, "earlier retryable lifecycle event")

	require.NoError(t, db.Model(earlier).Update("status", models.CommerceLifecycleStatusDeadLetter).Error)
	stale, err := app.claimLifecyclePosition(later, draft)
	require.NoError(t, err)
	assert.False(t, stale)
}

func TestCommerceReminderClaimIsDraftScopedAndExclusive(t *testing.T) {
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	account := testutil.CreateTestWhatsAppAccount(t, db, org.ID)
	contact := testutil.CreateTestContactWith(t, db, org.ID, testutil.WithContactAccount(account.Name))
	old := time.Now().Add(-2 * time.Hour)
	draft := &models.CommerceDraft{
		OrganizationID: org.ID, ContactID: contact.ID, WhatsAppAccount: account.Name,
		StoreID: "42", Status: "pending_payment", Version: 1,
		Cart: models.JSONB{}, Addons: models.JSONBArray{}, CapturedFields: models.JSONB{},
		Attachments: models.JSONBArray{}, Notes: models.JSONB{}, AddressSnapshot: models.JSONB{},
	}
	require.NoError(t, db.Create(draft).Error)
	require.NoError(t, db.Model(draft).UpdateColumn("updated_at", old).Error)
	settings := models.ChatbotSettings{
		OrganizationID: org.ID,
		AI:             models.AIConfig{CommerceStoreID: "42"},
		ClientInactivity: models.ClientInactivityConfig{
			PendingPaymentReminderMinutes: 30,
		},
	}
	processor := &SLAProcessor{app: &App{DB: db, Log: testutil.NopLogger()}}

	claimed, token, err := processor.claimCommerceReminder(settings, time.Now(), "pending_payment")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.NotEmpty(t, token)

	second, _, err := processor.claimCommerceReminder(settings, time.Now(), "pending_payment")
	require.NoError(t, err)
	assert.Nil(t, second, "an active draft claim must exclude concurrent workers")

	processor.releaseCommerceReminderClaim(draft.ID, token, "transient")
	replayed, _, err := processor.claimCommerceReminder(settings, time.Now(), "pending_payment")
	require.NoError(t, err)
	assert.NotNil(t, replayed, "failed claims must remain replayable")
}

func TestCommerceReminderSuppressesTerminalDrafts(t *testing.T) {
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	account := testutil.CreateTestWhatsAppAccount(t, db, org.ID)
	contact := testutil.CreateTestContactWith(t, db, org.ID, testutil.WithContactAccount(account.Name))
	for _, status := range []string{"completed", "cancelled"} {
		draft := &models.CommerceDraft{
			OrganizationID: org.ID, ContactID: contact.ID, WhatsAppAccount: account.Name,
			StoreID: "terminal-" + status, Status: status, Version: 1,
			Cart: models.JSONB{}, Addons: models.JSONBArray{}, CapturedFields: models.JSONB{},
			Attachments: models.JSONBArray{}, Notes: models.JSONB{}, AddressSnapshot: models.JSONB{},
		}
		require.NoError(t, db.Create(draft).Error)
	}
	processor := &SLAProcessor{app: &App{DB: db, Log: testutil.NopLogger()}}
	for _, status := range []string{"completed", "cancelled"} {
		settings := models.ChatbotSettings{
			OrganizationID: org.ID, AI: models.AIConfig{CommerceStoreID: "terminal-" + status},
			ClientInactivity: models.ClientInactivityConfig{AbandonedDraftReminderMinutes: 1},
		}
		claimed, _, err := processor.claimCommerceReminder(settings, time.Now().Add(24*time.Hour), "abandoned_draft")
		require.NoError(t, err)
		assert.Nil(t, claimed)
	}
}

func TestCommerceReminderSuppressedDuringActiveTransfer(t *testing.T) {
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	account := testutil.CreateTestWhatsAppAccount(t, db, org.ID)
	contact := testutil.CreateTestContactWith(t, db, org.ID, testutil.WithContactAccount(account.Name))
	draft := &models.CommerceDraft{
		OrganizationID: org.ID, ContactID: contact.ID, WhatsAppAccount: account.Name,
		StoreID: "active-transfer-store", Status: "pending_payment", Version: 1,
		Cart: models.JSONB{}, Addons: models.JSONBArray{}, CapturedFields: models.JSONB{},
		Attachments: models.JSONBArray{}, Notes: models.JSONB{}, AddressSnapshot: models.JSONB{},
	}
	require.NoError(t, db.Create(draft).Error)
	require.NoError(t, db.Model(draft).UpdateColumn("updated_at", time.Now().Add(-2*time.Hour)).Error)
	require.NoError(t, db.Create(&models.AgentTransfer{
		OrganizationID: org.ID, ContactID: contact.ID, WhatsAppAccount: account.Name,
		PhoneNumber: contact.PhoneNumber, Status: models.TransferStatusActive, TransferredAt: time.Now(),
	}).Error)
	settings := models.ChatbotSettings{
		OrganizationID: org.ID,
		AI:             models.AIConfig{CommerceStoreID: draft.StoreID},
		ClientInactivity: models.ClientInactivityConfig{
			PendingPaymentReminderEnabled: true,
			PendingPaymentReminderMinutes: 1,
		},
	}
	processor := &SLAProcessor{app: &App{DB: db, Log: testutil.NopLogger()}}
	processor.processCommerceReminderKind(settings, time.Now(), "pending_payment")

	var persisted models.CommerceDraft
	require.NoError(t, db.First(&persisted, "id = ?", draft.ID).Error)
	assert.Nil(t, persisted.PendingPaymentReminderAt)
	assert.Empty(t, persisted.ReminderClaimToken)
	assert.Equal(t, 0, persisted.ReminderAttempts)
}

func TestCommerceReminderSkipsTransferredDraftAndClaimsNext(t *testing.T) {
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	account := testutil.CreateTestWhatsAppAccount(t, db, org.ID)
	blockedContact := testutil.CreateTestContactWith(t, db, org.ID, testutil.WithContactAccount(account.Name))
	nextContact := testutil.CreateTestContactWith(t, db, org.ID, testutil.WithContactAccount(account.Name))
	old := time.Now().Add(-2 * time.Hour)
	makeDraft := func(contactID uuid.UUID) *models.CommerceDraft {
		draft := &models.CommerceDraft{
			OrganizationID: org.ID, ContactID: contactID, WhatsAppAccount: account.Name,
			StoreID: "skip-transfer-store", Status: "pending_payment", Version: 1,
			Cart: models.JSONB{}, Addons: models.JSONBArray{}, CapturedFields: models.JSONB{},
			Attachments: models.JSONBArray{}, Notes: models.JSONB{}, AddressSnapshot: models.JSONB{},
		}
		require.NoError(t, db.Create(draft).Error)
		require.NoError(t, db.Model(draft).UpdateColumn("updated_at", old).Error)
		return draft
	}
	blocked := makeDraft(blockedContact.ID)
	next := makeDraft(nextContact.ID)
	require.NoError(t, db.Create(&models.AgentTransfer{
		OrganizationID: org.ID, ContactID: blockedContact.ID, WhatsAppAccount: account.Name,
		PhoneNumber: blockedContact.PhoneNumber, Status: models.TransferStatusActive, TransferredAt: time.Now(),
	}).Error)
	settings := models.ChatbotSettings{
		OrganizationID: org.ID, AI: models.AIConfig{CommerceStoreID: blocked.StoreID},
		ClientInactivity: models.ClientInactivityConfig{PendingPaymentReminderMinutes: 1},
	}
	processor := &SLAProcessor{app: &App{DB: db, Log: testutil.NopLogger()}}

	claimed, _, err := processor.claimCommerceReminder(settings, time.Now(), "pending_payment")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, next.ID, claimed.ID)
}
