package aisensy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeAiSensyJWT(t *testing.T, exp time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))
	return header + "." + payload + ".sig"
}

func newTemplateTestClient(t *testing.T, server *httptest.Server) (*Client, *whatsapp.Account) {
	t.Helper()
	client := &Client{
		HTTPClient: server.Client(),
		Log:        testutil.NopLogger(),
		baseURL:    strings.TrimRight(server.URL, "/") + "/api",
	}
	account := &whatsapp.Account{
		AiSensyProjectID: "proj-1",
		AiSensyToken:     fakeAiSensyJWT(t, time.Now().Add(time.Hour)),
	}
	return client, account
}

func TestClient_FetchTemplates_PaginatesMessageTemplates(t *testing.T) {
	t.Parallel()

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/api/message-templates/")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "1", "name": "page_one", "language": "en", "status": "APPROVED"},
				},
				"paging": map[string]any{
					"cursors": map[string]any{"after": "CURSOR"},
					"next":    "https://graph.facebook.com/v18.0/WABA/message_templates?after=CURSOR&limit=100",
				},
			})
			return
		}
		assert.Equal(t, "CURSOR", r.URL.Query().Get("after"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "2", "name": "page_two", "language": "en", "status": "APPROVED"},
			},
		})
	}))
	t.Cleanup(server.Close)

	client, account := newTemplateTestClient(t, server)
	templates, err := client.FetchTemplates(context.Background(), account)
	require.NoError(t, err)
	require.Len(t, templates, 2)
	assert.Equal(t, 2, hits)
	assert.Equal(t, "page_one", templates[0].Name)
	assert.Equal(t, "page_two", templates[1].Name)
}

func TestClient_FetchTemplates_FallsBackToGetTemplates(t *testing.T) {
	t.Parallel()

	var messageTemplatesHits, getTemplatesHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/message-templates/"):
			messageTemplatesHits++
			http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/get-templates"):
			getTemplatesHits++
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("after") == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": []map[string]any{
						{"id": "1", "name": "legacy_one", "language": "en", "status": "APPROVED"},
					},
					"paging": map[string]any{
						"cursors": map[string]any{"after": "NEXT"},
					},
				})
				return
			}
			assert.Equal(t, "NEXT", r.URL.Query().Get("after"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "2", "name": "legacy_two", "language": "en", "status": "APPROVED"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client, account := newTemplateTestClient(t, server)
	templates, err := client.FetchTemplates(context.Background(), account)
	require.NoError(t, err)
	require.Len(t, templates, 2)
	assert.Equal(t, 1, messageTemplatesHits)
	assert.Equal(t, 2, getTemplatesHits)
	assert.Equal(t, "legacy_one", templates[0].Name)
	assert.Equal(t, "legacy_two", templates[1].Name)
}
