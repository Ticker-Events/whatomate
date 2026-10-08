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
	assert.Equal(t, "1484028330223507", pickup.PickupFlowID)
	assert.Equal(t, "1557965846018132", pickup.DeliveryFlowID)
	assert.Empty(t, pickup.StoredPickupFlowID)
	assert.Empty(t, pickup.StoredDeliveryFlowID)
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
		"is_default": true,
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
	assert.True(t, saved.Data.IsDefault)

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

func TestApp_UpdateCodedFlowBinding_EcommerceFlowIDs(t *testing.T) {
	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	account := testutil.CreateTestWhatsAppAccount(t, app.DB, org.ID)
	perms := getChatbotFlowPermissions(t, app)
	role := testutil.CreateTestRole(t, app.DB, org.ID, "coded-flow-ids", perms)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("coded-flow-ids")),
		testutil.WithRoleID(&role.ID),
	)

	list := testutil.NewGETRequest(t)
	testutil.SetAuthContext(list, org.ID, user.ID)
	testutil.SetQueryParam(list, "account", account.Name)
	require.NoError(t, app.ListCodedFlows(list))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(list))

	var listed struct {
		Data struct {
			Flows []handlers.CodedFlowBindingResponse `json:"flows"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(list), &listed))
	var ecommerce *handlers.CodedFlowBindingResponse
	for i := range listed.Data.Flows {
		if listed.Data.Flows[i].Key == "tiqr_ecommerce" {
			ecommerce = &listed.Data.Flows[i]
		}
	}
	require.NotNil(t, ecommerce)
	assert.Equal(t, "1484028330223507", ecommerce.PickupFlowID)
	assert.Equal(t, "1557965846018132", ecommerce.DeliveryFlowID)
	assert.Empty(t, ecommerce.StoredPickupFlowID)
	assert.Empty(t, ecommerce.StoredDeliveryFlowID)

	save := testutil.NewJSONRequest(t, map[string]any{
		"keywords":         []string{"shop"},
		"is_enabled":       true,
		"pickup_flow_id":   "111222333444555",
		"delivery_flow_id": "999888777666555",
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
	assert.Equal(t, "111222333444555", saved.Data.PickupFlowID)
	assert.Equal(t, "999888777666555", saved.Data.DeliveryFlowID)
	assert.Equal(t, "111222333444555", saved.Data.StoredPickupFlowID)
	assert.Equal(t, "999888777666555", saved.Data.StoredDeliveryFlowID)

	assert.Equal(t, "111222333444555", app.ResolveEcommerceMetaFlowID(org.ID, account.Name, false))
	assert.Equal(t, "999888777666555", app.ResolveEcommerceMetaFlowID(org.ID, account.Name, true))

	clear := testutil.NewJSONRequest(t, map[string]any{
		"keywords":         []string{"shop"},
		"is_enabled":       true,
		"pickup_flow_id":   "",
		"delivery_flow_id": "",
	})
	testutil.SetAuthContext(clear, org.ID, user.ID)
	testutil.SetQueryParam(clear, "account", account.Name)
	testutil.SetPathParam(clear, "key", "tiqr_ecommerce")
	require.NoError(t, app.UpdateCodedFlowBinding(clear))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(clear))

	var cleared struct {
		Data handlers.CodedFlowBindingResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(clear), &cleared))
	assert.Equal(t, "1484028330223507", cleared.Data.PickupFlowID)
	assert.Equal(t, "1557965846018132", cleared.Data.DeliveryFlowID)
	assert.Empty(t, cleared.Data.StoredPickupFlowID)
	assert.Empty(t, cleared.Data.StoredDeliveryFlowID)
}

func TestApp_UpdateCodedFlowBinding_RejectsInvalidFlowID(t *testing.T) {
	app := newTestApp(t)
	org := testutil.CreateTestOrganization(t, app.DB)
	account := testutil.CreateTestWhatsAppAccount(t, app.DB, org.ID)
	perms := getChatbotFlowPermissions(t, app)
	role := testutil.CreateTestRole(t, app.DB, org.ID, "coded-flow-bad-id", perms)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("coded-bad-id")),
		testutil.WithRoleID(&role.ID),
	)

	req := testutil.NewJSONRequest(t, map[string]any{
		"keywords":       []string{"shop"},
		"is_enabled":     true,
		"pickup_flow_id": "not-a-meta-id",
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetQueryParam(req, "account", account.Name)
	testutil.SetPathParam(req, "key", "tiqr_ecommerce")

	err := app.UpdateCodedFlowBinding(req)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusBadRequest, testutil.GetResponseStatusCode(req))
}
