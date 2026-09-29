package handlers_test

import (
	"encoding/json"
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func TestApp_ListCodedFlows(t *testing.T) {
	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	account := testutil.CreateTestWhatsAppAccount(t, app.DB, org.ID)
	perms := getChatbotFlowPermissions(t, app)
	role := testutil.CreateTestRole(t, app.DB, org.ID, "coded-flow-reader", perms)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("coded-flows")),
		testutil.WithRoleID(&role.ID),
	)

	req := testutil.NewGETRequest(t)
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetQueryParam(req, "account", account.Name)

	err := app.ListCodedFlows(req)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	var resp struct {
		Data struct {
			Flows []handlers.CodedFlowBindingResponse `json:"flows"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(req), &resp))
	require.NotEmpty(t, resp.Data.Flows)

	var pickup *handlers.CodedFlowBindingResponse
	for i := range resp.Data.Flows {
		if resp.Data.Flows[i].Key == "tiqr_ecommerce" {
			pickup = &resp.Data.Flows[i]
		}
	}
	require.NotNil(t, pickup)
	assert.Equal(t, "TiQR Ecommerce", pickup.Name)
	assert.Empty(t, pickup.Keywords)
	assert.False(t, pickup.IsEnabled)
	assert.NotEmpty(t, pickup.Steps)
}

func TestApp_UpdateCodedFlowBinding_RejectsUnknownKey(t *testing.T) {
	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	account := testutil.CreateTestWhatsAppAccount(t, app.DB, org.ID)
	perms := getChatbotFlowPermissions(t, app)
	role := testutil.CreateTestRole(t, app.DB, org.ID, "coded-flow-writer", perms)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("coded-unknown")),
		testutil.WithRoleID(&role.ID),
	)

	req := testutil.NewJSONRequest(t, map[string]any{
		"keywords":   []string{"shop"},
		"is_enabled": true,
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetQueryParam(req, "account", account.Name)
	testutil.SetPathParam(req, "key", "not_a_real_flow")

	err := app.UpdateCodedFlowBinding(req)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusNotFound, testutil.GetResponseStatusCode(req))
}

func TestApp_UpdateCodedFlowBinding_OrgIsolation(t *testing.T) {
	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	other := testutil.CreateTestOrganization(t, app.DB)
	account := testutil.CreateTestWhatsAppAccount(t, app.DB, org.ID)
	otherAccount := testutil.CreateTestWhatsAppAccount(t, app.DB, other.ID)

	perms := getChatbotFlowPermissions(t, app)
	role := testutil.CreateTestRole(t, app.DB, org.ID, "coded-flow-org", perms)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("coded-org")),
		testutil.WithRoleID(&role.ID),
	)
	otherRole := testutil.CreateTestRole(t, app.DB, other.ID, "coded-flow-org-2", perms)
	otherUser := testutil.CreateTestUser(t, app.DB, other.ID,
		testutil.WithEmail(testutil.UniqueEmail("coded-org-2")),
		testutil.WithRoleID(&otherRole.ID),
	)

	save := testutil.NewJSONRequest(t, map[string]any{
		"keywords":   []string{"shop", "shop"},
		"is_enabled": true,
	})
	testutil.SetAuthContext(save, org.ID, user.ID)
	testutil.SetQueryParam(save, "account", account.Name)
	testutil.SetPathParam(save, "key", "tiqr_ecommerce")
	require.NoError(t, app.UpdateCodedFlowBinding(save))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(save))

	var saved struct {
		Data handlers.CodedFlowBindingResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(save), &saved))
	assert.Equal(t, []string{"shop"}, saved.Data.Keywords)
	assert.True(t, saved.Data.IsEnabled)

	listOther := testutil.NewGETRequest(t)
	testutil.SetAuthContext(listOther, other.ID, otherUser.ID)
	testutil.SetQueryParam(listOther, "account", otherAccount.Name)
	require.NoError(t, app.ListCodedFlows(listOther))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(listOther))

	var listed struct {
		Data struct {
			Flows []handlers.CodedFlowBindingResponse `json:"flows"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(listOther), &listed))
	for _, flow := range listed.Data.Flows {
		if flow.Key == "tiqr_ecommerce" {
			assert.Empty(t, flow.Keywords)
			assert.False(t, flow.IsEnabled)
		}
	}

	var count int64
	require.NoError(t, app.DB.Model(&models.CodedFlowBinding{}).
		Where("organization_id = ? AND flow_key = ?", other.ID, "tiqr_ecommerce").
		Count(&count).Error)
	assert.Zero(t, count)
}

func TestApp_UpdateCodedFlowBinding_RequiresWrite(t *testing.T) {
	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	account := testutil.CreateTestWhatsAppAccount(t, app.DB, org.ID)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("coded-forbidden")),
	)

	req := testutil.NewJSONRequest(t, map[string]any{
		"keywords":   []string{"shop"},
		"is_enabled": true,
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetQueryParam(req, "account", account.Name)
	testutil.SetPathParam(req, "key", "tiqr_ecommerce")

	err := app.UpdateCodedFlowBinding(req)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusForbidden, testutil.GetResponseStatusCode(req))
}
