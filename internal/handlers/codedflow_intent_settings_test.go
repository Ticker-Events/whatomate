package handlers_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func TestApp_UpdateChatbotSettings_IntentJev(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	app.Config.App.EncryptionKey = "this-is-a-32-character-test-key-XX"
	org := testutil.CreateTestOrganization(t, app.DB)
	user := testutil.CreateTestUser(t, app.DB, org.ID)

	req := testutil.NewJSONRequest(t, map[string]any{
		"ai_intent_provider":  "jev",
		"ai_typesafe_api_key": "typesafe-secret",
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	err := app.UpdateChatbotSettings(req)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	getReq := testutil.NewGETRequest(t)
	testutil.SetAuthContext(getReq, org.ID, user.ID)
	err = app.GetChatbotSettings(getReq)
	require.NoError(t, err)

	var getResp struct {
		Data struct {
			Settings handlers.ChatbotSettingsResponse `json:"settings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(getReq), &getResp))
	assert.Equal(t, models.IntentProviderJev, getResp.Data.Settings.AIIntentProvider)
	assert.True(t, getResp.Data.Settings.AITypeSafeAPIKeySet)
	assert.NotContains(t, string(testutil.GetResponseBody(getReq)), "typesafe-secret")

	var stored models.ChatbotSettings
	require.NoError(t, app.DB.Where("organization_id = ?", org.ID).First(&stored).Error)
	assert.NotEqual(t, "typesafe-secret", stored.AI.TypeSafeAPIKey)
	assert.True(t, strings.HasPrefix(stored.AI.TypeSafeAPIKey, "enc:") || stored.AI.TypeSafeAPIKey != "")
}

func TestApp_UpdateChatbotSettings_IntentJevRequiresKey(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	user := testutil.CreateTestUser(t, app.DB, org.ID)

	req := testutil.NewJSONRequest(t, map[string]any{
		"ai_intent_provider": "jev",
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	err := app.UpdateChatbotSettings(req)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusBadRequest, testutil.GetResponseStatusCode(req))
}

func TestApp_UpdateChatbotSettings_IntentGateway(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	app.Config.App.EncryptionKey = "this-is-a-32-character-test-key-XX"
	org := testutil.CreateTestOrganization(t, app.DB)
	user := testutil.CreateTestUser(t, app.DB, org.ID)

	bad := testutil.NewJSONRequest(t, map[string]any{
		"ai_intent_provider": "gateway",
		"ai_gateway_api_key": "gw-secret",
		"ai_gateway_model":   "openai/gpt-6-astra",
	})
	testutil.SetAuthContext(bad, org.ID, user.ID)
	require.NoError(t, app.UpdateChatbotSettings(bad))
	assert.Equal(t, fasthttp.StatusBadRequest, testutil.GetResponseStatusCode(bad))

	req := testutil.NewJSONRequest(t, map[string]any{
		"ai_intent_provider": "gateway",
		"ai_gateway_api_key": "gw-secret",
		"ai_gateway_model":   models.IntentGatewayModelJev,
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	require.NoError(t, app.UpdateChatbotSettings(req))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	getReq := testutil.NewGETRequest(t)
	testutil.SetAuthContext(getReq, org.ID, user.ID)
	require.NoError(t, app.GetChatbotSettings(getReq))
	var getResp struct {
		Data struct {
			Settings handlers.ChatbotSettingsResponse `json:"settings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(getReq), &getResp))
	assert.Equal(t, models.IntentProviderGateway, getResp.Data.Settings.AIIntentProvider)
	assert.True(t, getResp.Data.Settings.AIGatewayAPIKeySet)
	assert.Equal(t, models.IntentGatewayModelJev, getResp.Data.Settings.AIGatewayModel)
	assert.NotContains(t, string(testutil.GetResponseBody(getReq)), "gw-secret")
}

func TestApp_UpdateChatbotSettings_CodedFlowRoleProviders(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	app.Config.App.EncryptionKey = "this-is-a-32-character-test-key-XX"
	org := testutil.CreateTestOrganization(t, app.DB)
	user := testutil.CreateTestUser(t, app.DB, org.ID)

	req := testutil.NewJSONRequest(t, map[string]any{
		"ai_intent_provider":    "gateway",
		"ai_translate_provider": "gateway",
		"ai_guide_provider":     "generic",
		"ai_recover_provider":   "gateway",
		"ai_gateway_api_key":    "gw-secret",
		"ai_gateway_model":      models.IntentGatewayModelGemini25Flash,
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	require.NoError(t, app.UpdateChatbotSettings(req))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	getReq := testutil.NewGETRequest(t)
	testutil.SetAuthContext(getReq, org.ID, user.ID)
	require.NoError(t, app.GetChatbotSettings(getReq))
	var getResp struct {
		Data struct {
			Settings handlers.ChatbotSettingsResponse `json:"settings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(getReq), &getResp))
	assert.Equal(t, models.IntentProviderGateway, getResp.Data.Settings.AIIntentProvider)
	assert.Equal(t, models.IntentProviderGateway, getResp.Data.Settings.AITranslateProvider)
	assert.Equal(t, models.IntentProviderGeneric, getResp.Data.Settings.AIGuideProvider)
	assert.Equal(t, models.IntentProviderGateway, getResp.Data.Settings.AIRecoverProvider)
	assert.Equal(t, models.IntentGatewayModelGemini25Flash, getResp.Data.Settings.AIGatewayModel)
}
