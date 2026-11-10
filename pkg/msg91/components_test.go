package msg91

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/logf"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

func TestMapTemplateComponents(t *testing.T) {
	components := []map[string]any{
		{
			"type": "header",
			"parameters": []any{
				map[string]any{"type": "image", "image": map[string]any{"link": "https://cdn.example/a.jpg"}},
			},
		},
		{
			"type": "body",
			"parameters": []any{
				map[string]any{"type": "text", "text": "Alice"},
				map[string]any{"type": "text", "text": "42"},
			},
		},
		{
			"type":     "button",
			"sub_type": "url",
			"parameters": []any{
				map[string]any{"type": "text", "text": "otp123"},
			},
		},
	}

	got := mapTemplateComponents(components)
	require.Contains(t, got, "header_1")
	require.Contains(t, got, "body_1")
	require.Contains(t, got, "body_2")
	require.Contains(t, got, "button_1")

	header := got["header_1"].(map[string]any)
	assert.Equal(t, "image", header["type"])
	assert.Equal(t, "https://cdn.example/a.jpg", header["value"])

	body1 := got["body_1"].(map[string]any)
	assert.Equal(t, "text", body1["type"])
	assert.Equal(t, "Alice", body1["value"])

	btn := got["button_1"].(map[string]any)
	assert.Equal(t, "url", btn["subtype"])
	assert.Equal(t, "otp123", btn["value"])
}

func TestParseAPIError(t *testing.T) {
	err := parseAPIError(400, []byte(`{"status":"error","hasError":true,"errors":"bad authkey","message":""}`))
	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, 400, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "bad authkey")
}

func TestParseSendID(t *testing.T) {
	id, err := parseSendID([]byte(`{"status":"success","hasError":false,"data":"processing","request_id":"req-123"}`))
	require.NoError(t, err)
	assert.Equal(t, "req-123", id)
}

func TestValidateCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("authkey"))
		assert.Equal(t, "/api/v5/whatsapp/whatsapp-activation/", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"integrated_number": "919876543210"}},
		})
	}))
	defer srv.Close()

	client := NewWithBaseURL(logf.New(logf.Opts{}), srv.URL)
	result, err := client.ValidateCredentials(t.Context(), &whatsapp.Account{
		Provider:     ProviderMSG91,
		PhoneID:      "919876543210",
		MSG91AuthKey: "test-key",
	})
	require.NoError(t, err)
	assert.Equal(t, "919876543210", result.PhoneNumber)
}

func TestSendTextMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "secret", r.Header.Get("authkey"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "919876543210", body["integrated_number"])
		assert.Equal(t, "911234567890", body["recipient_number"])
		assert.Equal(t, "text", body["content_type"])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "success",
			"hasError":   false,
			"request_id": "out-1",
		})
	}))
	defer srv.Close()

	client := NewWithBaseURL(logf.New(logf.Opts{}), srv.URL)
	id, err := client.SendTextMessage(t.Context(), &whatsapp.Account{
		PhoneID:      "919876543210",
		MSG91AuthKey: "secret",
	}, whatsapp.Recipient{Phone: "911234567890"}, "hello")
	require.NoError(t, err)
	assert.Equal(t, "out-1", id)
}

func TestSendTemplateMessageMapsComponents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/bulk/")
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		payload := body["payload"].(map[string]any)
		tmpl := payload["template"].(map[string]any)
		entries := tmpl["to_and_components"].([]any)
		entry := entries[0].(map[string]any)
		comps := entry["components"].(map[string]any)
		require.Contains(t, comps, "body_1")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "hasError": false, "request_id": "tmpl-1"})
	}))
	defer srv.Close()

	client := NewWithBaseURL(logf.New(logf.Opts{}), srv.URL)
	id, err := client.SendTemplateMessage(t.Context(), &whatsapp.Account{
		PhoneID:      "919876543210",
		MSG91AuthKey: "secret",
	}, whatsapp.Recipient{Phone: "+911234567890"}, "welcome", "en", []map[string]any{
		{"type": "body", "parameters": []any{map[string]any{"type": "text", "text": "Sam"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "tmpl-1", id)
}
