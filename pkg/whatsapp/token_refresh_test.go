package whatsapp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_RefreshLongLivedToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/oauth/access_token")
		assert.Equal(t, "fb_exchange_token", r.URL.Query().Get("grant_type"))
		assert.Equal(t, "app-id", r.URL.Query().Get("client_id"))
		assert.Equal(t, "app-secret", r.URL.Query().Get("client_secret"))
		assert.Equal(t, "old-token", r.URL.Query().Get("fb_exchange_token"))

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-token",
			"token_type":   "bearer",
			"expires_in":   5184000,
		})
	}))
	defer server.Close()

	client := whatsapp.NewWithBaseURL(testutil.NopLogger(), server.URL)
	token, expiresIn, err := client.RefreshLongLivedToken(
		testutil.TestContext(t), "old-token", "app-id", "app-secret", "v21.0")
	require.NoError(t, err)
	assert.Equal(t, "new-token", token)
	assert.Equal(t, 5184000, expiresIn)
}

func TestClient_RefreshLongLivedToken_Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Error validating access token",
				"code":    190,
			},
		})
	}))
	defer server.Close()

	client := whatsapp.NewWithBaseURL(testutil.NopLogger(), server.URL)
	_, _, err := client.RefreshLongLivedToken(
		testutil.TestContext(t), "bad", "app-id", "app-secret", "v21.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Error validating access token")
}

func TestExpiresAtHelpers(t *testing.T) {
	t.Parallel()

	assert.Nil(t, whatsapp.ExpiresAtFromUnix(0))
	assert.Nil(t, whatsapp.ExpiresAtFromExpiresIn(0, time.Now()))

	now := time.Unix(1_700_000_000, 0).UTC()
	got := whatsapp.ExpiresAtFromExpiresIn(3600, now)
	require.NotNil(t, got)
	assert.Equal(t, now.Add(time.Hour), *got)

	fromUnix := whatsapp.ExpiresAtFromUnix(1_700_000_000)
	require.NotNil(t, fromUnix)
	assert.Equal(t, time.Unix(1_700_000_000, 0).UTC(), *fromUnix)
}
