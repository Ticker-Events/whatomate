package aisensy

import (
	"context"
	"encoding/json"
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

func TestFormatCurl_JSONBodyAndAuth(t *testing.T) {
	t.Parallel()

	curl := formatCurl(
		http.MethodPost,
		"https://backend.aisensy.com/direct-apis/t1/wa_template",
		"jwt-token",
		"application/json",
		[]byte(`{"name":"promo"}`),
	)

	assert.Contains(t, curl, "curl --request 'POST'")
	assert.Contains(t, curl, "--url 'https://backend.aisensy.com/direct-apis/t1/wa_template'")
	assert.Contains(t, curl, "--header 'Authorization: Bearer jwt-token'")
	assert.Contains(t, curl, "--header 'Content-Type: application/json'")
	assert.Contains(t, curl, `--data '{"name":"promo"}'`)
}

func TestFormatCurl_EscapesSingleQuotes(t *testing.T) {
	t.Parallel()

	curl := formatCurl(http.MethodPost, "https://example.com", "tok", "application/json", []byte(`{"msg":"it's fine"}`))
	assert.Contains(t, curl, `'{"msg":"it'\''s fine"}'`)
}

func TestFormatCurl_TruncatesLargeBody(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("a", curlBodyLogLimit+50)
	curl := formatCurl(http.MethodPost, "https://example.com", "tok", "application/json", []byte(body))
	assert.Contains(t, curl, "...[truncated]")
	assert.Less(t, len(curl), curlBodyLogLimit+200)
}

func TestFormatCurl_OmitsMultipartBody(t *testing.T) {
	t.Parallel()

	curl := formatCurl(
		http.MethodPost,
		"https://example.com/flows/1/assets",
		"tok",
		"multipart/form-data; boundary=abc",
		[]byte("secret-file-bytes"),
	)
	assert.Contains(t, curl, "<multipart body omitted>")
	assert.NotContains(t, curl, "secret-file-bytes")
}

func TestDoRequest_LogsCurlOnAPIError(t *testing.T) {
	t.Parallel()

	token := fakeAiSensyJWT(t, time.Now().Add(time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/users/regenrate-token") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"users": []map[string]string{{"token": token}},
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad template"}`))
	}))
	t.Cleanup(server.Close)

	client := &Client{
		HTTPClient: server.Client(),
		Log:        testutil.NopLogger(),
		baseURL:    strings.TrimRight(server.URL, "/") + "/api",
	}
	account := &whatsapp.Account{
		AiSensyEmail:     "agent@example.com",
		AiSensyPassword:  "secret",
		AiSensyProjectID: "proj-1",
		AiSensyToken:     token,
	}

	failURL := client.baseURL + "/message-templates/"
	_, err := client.doRequest(context.Background(), http.MethodPost, failURL, map[string]any{"name": "x"}, account)
	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "bad template")

	curl := formatCurl(http.MethodPost, failURL, token, "application/json", []byte(`{"name":"x"}`))
	assert.Contains(t, curl, failURL)
	assert.Contains(t, curl, "Authorization: Bearer "+token)
}
