package aisensy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMediaURL_RefreshesRejectedTokenOnce(t *testing.T) {
	t.Parallel()

	stale := fakeAiSensyJWT(t, time.Now().Add(time.Hour))
	fresh := fakeAiSensyJWT(t, time.Now().Add(2*time.Hour))
	var tokenRegens atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/users/regenrate-token"):
			tokenRegens.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"users": []map[string]string{{"token": fresh}},
			})
		case strings.HasSuffix(r.URL.Path, "/get-media/"):
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode get-media body: %v", err)
			}
			if body["id"] != "media-1" {
				t.Errorf("media id = %q", body["id"])
			}
			if r.Header.Get("Authorization") != "Bearer "+fresh {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"Invalid Token!"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[1,2,3,4]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client := &Client{
		HTTPClient: server.Client(),
		Log:        testutil.NopLogger(),
		baseURL:    strings.TrimRight(server.URL, "/"),
	}
	account := &whatsapp.Account{
		AiSensyEmail:     "agent@example.com",
		AiSensyPassword:  "secret",
		AiSensyProjectID: "proj-1",
		AiSensyToken:     stale,
	}

	mediaURL, err := client.GetMediaURL(context.Background(), "media-1", account)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(mediaURL, mediaCachePrefix))
	assert.Equal(t, fresh, account.AiSensyToken)
	assert.Equal(t, int32(1), tokenRegens.Load())

	data, err := client.DownloadMedia(context.Background(), mediaURL, "")
	require.NoError(t, err)
	assert.Equal(t, []byte{1, 2, 3, 4}, data)

	_, err = client.GetMediaURL(context.Background(), "media-1", account)
	require.NoError(t, err)
	assert.Equal(t, int32(1), tokenRegens.Load())
}
