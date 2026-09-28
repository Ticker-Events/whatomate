package aisensy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_UpdateFlowJSON_SendsMultipartFile(t *testing.T) {
	t.Parallel()

	flowJSON := &whatsapp.FlowJSON{
		Version: "3.0",
		Screens: []any{
			map[string]any{
				"id":    "WELCOME",
				"title": "Welcome",
			},
		},
	}
	wantFile, err := json.Marshal(flowJSON)
	require.NoError(t, err)

	var wantAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/flows/flow-123/assets/")
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		assert.NotEqual(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, wantAuth, r.Header.Get("Authorization"))

		require.NoError(t, r.ParseMultipartForm(1<<20))
		assert.Equal(t, "flow.json", r.FormValue("name"))
		assert.Equal(t, "FLOW_JSON", r.FormValue("asset_type"))

		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()
		assert.Equal(t, "flow.json", header.Filename)
		assert.Equal(t, "application/json", header.Header.Get("Content-Type"))

		got, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.JSONEq(t, string(wantFile), string(got))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}))
	t.Cleanup(server.Close)

	client, account := newTemplateTestClient(t, server)
	wantAuth = "Bearer " + account.AiSensyToken
	err = client.UpdateFlowJSON(context.Background(), account, "flow-123", flowJSON)
	require.NoError(t, err)
}

func TestClient_UpdateFlowJSON_ValidationError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":           false,
			"validation_errors": "Invalid screen layout",
		})
	}))
	t.Cleanup(server.Close)

	client, account := newTemplateTestClient(t, server)
	err := client.UpdateFlowJSON(context.Background(), account, "flow-123", &whatsapp.FlowJSON{Version: "3.0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation errors")
}
