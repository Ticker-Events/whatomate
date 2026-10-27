package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/shridarpatil/whatomate/internal/handlers"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func TestTokenRefreshProcessor_SuccessClearsFailure(t *testing.T) {
	t.Parallel()

	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	app.Config.WhatsApp.AppID = "app-id"
	app.Config.WhatsApp.AppSecret = "app-secret"
	app.Config.WhatsApp.TokenRefreshDaysBefore = 10

	expiresSoon := time.Now().UTC().Add(24 * time.Hour)
	enc, err := crypto.Encrypt("old-token", app.Config.App.EncryptionKey)
	require.NoError(t, err)

	account := models.WhatsAppAccount{
		OrganizationID:     org.ID,
		Name:               "Meta Acc",
		PhoneID:            "phone-refresh-ok",
		BusinessID:         "biz-1",
		AccessToken:        enc,
		Provider:           "meta",
		Status:             "active",
		APIVersion:         "v21.0",
		TokenExpiresAt:     &expiresSoon,
		TokenRefreshFailed: true,
		TokenRefreshError:  "previous failure",
	}
	require.NoError(t, app.DB.Create(&account).Error)

	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/oauth/access_token") {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "new-token",
				"expires_in":   5184000,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer metaServer.Close()
	app.WhatsApp = whatsapp.NewWithBaseURL(app.Log, metaServer.URL)

	handlers.NewTokenRefreshProcessor(app, time.Hour).RunOnce(context.Background())

	var updated models.WhatsAppAccount
	require.NoError(t, app.DB.First(&updated, "id = ?", account.ID).Error)
	assert.False(t, updated.TokenRefreshFailed)
	assert.Empty(t, updated.TokenRefreshError)
	require.NotNil(t, updated.TokenExpiresAt)
	assert.True(t, updated.TokenExpiresAt.After(time.Now().Add(30*24*time.Hour)))
	assert.NotEqual(t, enc, updated.AccessToken)
}

func TestTokenRefreshProcessor_FailureSetsFlag(t *testing.T) {
	t.Parallel()

	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	app.Config.WhatsApp.AppID = "app-id"
	app.Config.WhatsApp.AppSecret = "app-secret"
	app.Config.WhatsApp.TokenRefreshDaysBefore = 10

	expiresSoon := time.Now().UTC().Add(24 * time.Hour)
	enc, err := crypto.Encrypt("old-token", app.Config.App.EncryptionKey)
	require.NoError(t, err)

	account := models.WhatsAppAccount{
		OrganizationID: org.ID,
		Name:           "Meta Fail",
		PhoneID:        "phone-refresh-fail",
		BusinessID:     "biz-2",
		AccessToken:    enc,
		Provider:       "meta",
		Status:         "active",
		APIVersion:     "v21.0",
		TokenExpiresAt: &expiresSoon,
	}
	require.NoError(t, app.DB.Create(&account).Error)

	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Error validating access token",
				"code":    190,
			},
		})
	}))
	defer metaServer.Close()
	app.WhatsApp = whatsapp.NewWithBaseURL(app.Log, metaServer.URL)

	handlers.NewTokenRefreshProcessor(app, time.Hour).RunOnce(context.Background())

	var updated models.WhatsAppAccount
	require.NoError(t, app.DB.First(&updated, "id = ?", account.ID).Error)
	assert.True(t, updated.TokenRefreshFailed)
	assert.Contains(t, updated.TokenRefreshError, "Error validating access token")
	assert.Equal(t, enc, updated.AccessToken)
}

func TestTokenRefreshProcessor_SkipsAiSensy(t *testing.T) {
	t.Parallel()

	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	app.Config.WhatsApp.AppID = "app-id"
	app.Config.WhatsApp.AppSecret = "app-secret"
	app.Config.WhatsApp.TokenRefreshDaysBefore = 10

	expiresSoon := time.Now().UTC().Add(24 * time.Hour)
	account := models.WhatsAppAccount{
		OrganizationID: org.ID,
		Name:           "AiSensy Acc",
		PhoneID:        "phone-aisensy",
		BusinessID:     "biz-ai",
		AccessToken:    "unused",
		Provider:       "aisensy",
		Status:         "active",
		TokenExpiresAt: &expiresSoon,
	}
	require.NoError(t, app.DB.Create(&account).Error)

	called := false
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer metaServer.Close()
	app.WhatsApp = whatsapp.NewWithBaseURL(app.Log, metaServer.URL)

	handlers.NewTokenRefreshProcessor(app, time.Hour).RunOnce(context.Background())
	assert.False(t, called)
}

func TestApp_ListTokenAlerts(t *testing.T) {
	t.Parallel()

	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	user := createAdminUser(t, app, org.ID)

	expired := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(30 * 24 * time.Hour)

	require.NoError(t, app.DB.Create(&models.WhatsAppAccount{
		OrganizationID:     org.ID,
		Name:               "Failed Refresh",
		PhoneID:            "p-fail",
		BusinessID:         "b1",
		AccessToken:        "tok",
		Provider:           "meta",
		Status:             "active",
		TokenExpiresAt:     &future,
		TokenRefreshFailed: true,
		TokenRefreshError:  "boom",
	}).Error)
	require.NoError(t, app.DB.Create(&models.WhatsAppAccount{
		OrganizationID: org.ID,
		Name:           "Expired",
		PhoneID:        "p-exp",
		BusinessID:     "b2",
		AccessToken:    "tok",
		Provider:       "meta",
		Status:         "active",
		TokenExpiresAt: &expired,
	}).Error)
	require.NoError(t, app.DB.Create(&models.WhatsAppAccount{
		OrganizationID: org.ID,
		Name:           "Healthy",
		PhoneID:        "p-ok",
		BusinessID:     "b3",
		AccessToken:    "tok",
		Provider:       "meta",
		Status:         "active",
		TokenExpiresAt: &future,
	}).Error)
	require.NoError(t, app.DB.Create(&models.WhatsAppAccount{
		OrganizationID:     org.ID,
		Name:               "AiSensy Failed",
		PhoneID:            "p-ai",
		BusinessID:         "b4",
		AccessToken:        "tok",
		Provider:           "aisensy",
		Status:             "active",
		TokenRefreshFailed: true,
	}).Error)

	req := testutil.NewGETRequest(t)
	testutil.SetAuthContext(req, org.ID, user.ID)
	err := app.ListTokenAlerts(req)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	var resp struct {
		Data struct {
			Alerts []struct {
				Name               string `json:"name"`
				TokenRefreshFailed bool   `json:"token_refresh_failed"`
			} `json:"alerts"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(req), &resp))
	require.Len(t, resp.Data.Alerts, 2)
	names := []string{resp.Data.Alerts[0].Name, resp.Data.Alerts[1].Name}
	assert.Contains(t, names, "Failed Refresh")
	assert.Contains(t, names, "Expired")
}
