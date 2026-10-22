package handlers

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func commerceCatalogChatRole(t *testing.T, app *App, orgID uuid.UUID) *models.CustomRole {
	t.Helper()
	return testutil.CreateTestRoleWithKeys(t, app.DB, orgID, "commerce-catalog", []string{
		"chat:read",
		"chat:write",
		"contacts:read",
	})
}

func TestGetCommerceCatalogStatus_ActiveWhenEnabled(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	role := commerceCatalogChatRole(t, app, org.ID)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("catalog-status")),
		testutil.WithRoleID(&role.ID),
	)
	enableTiqrEcommerce(t, app, org.ID, account.Name, "shop", true)

	req := testutil.NewGETRequest(t)
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetQueryParam(req, "account", account.Name)

	require.NoError(t, app.GetCommerceCatalogStatus(req))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	var resp struct {
		Data struct {
			Active bool `json:"active"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(req), &resp))
	assert.True(t, resp.Data.Active)
}

func TestGetCommerceCatalogStatus_InactiveWhenDisabled(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	role := commerceCatalogChatRole(t, app, org.ID)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("catalog-inactive")),
		testutil.WithRoleID(&role.ID),
	)

	req := testutil.NewGETRequest(t)
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetQueryParam(req, "account", account.Name)

	require.NoError(t, app.GetCommerceCatalogStatus(req))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	var resp struct {
		Data struct {
			Active bool `json:"active"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(req), &resp))
	assert.False(t, resp.Data.Active)
}

func TestListCommerceCatalogCollections_ReturnsPage(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	role := commerceCatalogChatRole(t, app, org.ID)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("catalog-list")),
		testutil.WithRoleID(&role.ID),
	)
	enableTiqrEcommerce(t, app, org.ID, account.Name, "shop", true)

	collections := make([]any, 0, 12)
	for i := 1; i <= 12; i++ {
		collections = append(collections, map[string]any{
			"id": i, "name": fmt.Sprintf("Collection %d", i), "description": "Desc",
		})
	}
	srv := newStoreServerWith(t, nil, nil, map[string]any{
		"id":               42,
		"name":             "Demo",
		"currency":         "INR",
		"test_collections": collections,
	})
	useStoreREST(t, srv)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})

	req := testutil.NewGETRequest(t)
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetQueryParam(req, "account", account.Name)
	testutil.SetQueryParam(req, "page", "1")

	require.NoError(t, app.ListCommerceCatalogCollections(req))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	var resp struct {
		Data commerceCatalogPageResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(req), &resp))
	assert.Equal(t, 12, resp.Data.Count)
	assert.Equal(t, 1, resp.Data.Page)
	assert.Equal(t, 10, resp.Data.PageSize)
	assert.Len(t, resp.Data.Results, 10)
}

func TestSendCommerceCatalog_Collections_SendsListWithoutTouchingSession(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	role := commerceCatalogChatRole(t, app, org.ID)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("catalog-send-col")),
		testutil.WithRoleID(&role.ID),
	)
	contact := testutil.CreateTestContactWith(t, app.DB, org.ID, testutil.WithContactAccount(account.Name))
	enableTiqrEcommerce(t, app, org.ID, account.Name, "shop", true)

	session := &models.ChatbotSession{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  org.ID,
		ContactID:       contact.ID,
		WhatsAppAccount: account.Name,
		PhoneNumber:     contact.PhoneNumber,
		Status:          models.SessionStatusActive,
		SessionData:     models.JSONB{"marker": "keep-me"},
	}
	require.NoError(t, app.DB.Create(session).Error)

	srv := newStoreServerWith(t, nil, nil, map[string]any{
		"id":       42,
		"name":     "Demo",
		"currency": "INR",
	})
	useStoreREST(t, srv)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})

	req := testutil.NewJSONRequest(t, map[string]any{
		"kind": "collections",
		"page": 1,
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetPathParam(req, "id", contact.ID.String())

	require.NoError(t, app.SendCommerceCatalog(req))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	var messages []models.Message
	require.NoError(t, app.DB.Where("contact_id = ?", contact.ID).Find(&messages).Error)
	require.Len(t, messages, 1)
	assert.Equal(t, models.MessageTypeInteractive, messages[0].MessageType)
	assert.Equal(t, user.ID, *messages[0].SentByUserID)

	reloadSession(t, app, session)
	assert.Equal(t, "keep-me", session.SessionData["marker"])
	assert.Nil(t, session.SessionData["collections"])
}

func TestSendCommerceCatalog_Products_SendsCarousel(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	role := commerceCatalogChatRole(t, app, org.ID)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("catalog-send-prod")),
		testutil.WithRoleID(&role.ID),
	)
	contact := testutil.CreateTestContactWith(t, app.DB, org.ID, testutil.WithContactAccount(account.Name))
	enableTiqrEcommerce(t, app, org.ID, account.Name, "shop", true)

	products := []any{
		map[string]any{
			"id": "101", "name": "Kunafa", "min_price": "120", "description": "Sweet",
			"images": []any{map[string]any{"original_url": "https://cdn.example.com/a.jpg"}},
			"options": []any{map[string]any{"id": "1", "name": "Default", "price": "120"}},
		},
		map[string]any{
			"id": "102", "name": "Canape", "min_price": "80", "description": "Savory",
			"images": []any{map[string]any{"original_url": "https://cdn.example.com/b.jpg"}},
			"options": []any{map[string]any{"id": "2", "name": "Default", "price": "80"}},
		},
	}
	srv := newStoreServerWith(t, products, nil, map[string]any{
		"id":       42,
		"name":     "Demo",
		"currency": "INR",
	})
	useStoreREST(t, srv)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})

	req := testutil.NewJSONRequest(t, map[string]any{
		"kind":            "products",
		"collection_id":   "57",
		"collection_name": "Sweets",
		"page":            1,
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetPathParam(req, "id", contact.ID.String())

	require.NoError(t, app.SendCommerceCatalog(req))
	assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	var messages []models.Message
	require.NoError(t, app.DB.Where("contact_id = ?", contact.ID).Find(&messages).Error)
	require.Len(t, messages, 1)
	assert.Equal(t, models.MessageTypeInteractive, messages[0].MessageType)
	assert.Contains(t, messages[0].Content, "Sweets")
}

func TestSendCommerceCatalog_RejectsWhenFlowDisabled(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	role := commerceCatalogChatRole(t, app, org.ID)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("catalog-disabled")),
		testutil.WithRoleID(&role.ID),
	)
	contact := testutil.CreateTestContactWith(t, app.DB, org.ID, testutil.WithContactAccount(account.Name))

	req := testutil.NewJSONRequest(t, map[string]any{
		"kind": "collections",
		"page": 1,
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetPathParam(req, "id", contact.ID.String())

	require.NoError(t, app.SendCommerceCatalog(req))
	assert.Equal(t, fasthttp.StatusBadRequest, testutil.GetResponseStatusCode(req))
}

func TestSendCommerceCatalog_RejectsEmptyProductsPage(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	role := commerceCatalogChatRole(t, app, org.ID)
	user := testutil.CreateTestUser(t, app.DB, org.ID,
		testutil.WithEmail(testutil.UniqueEmail("catalog-empty")),
		testutil.WithRoleID(&role.ID),
	)
	contact := testutil.CreateTestContactWith(t, app.DB, org.ID, testutil.WithContactAccount(account.Name))
	enableTiqrEcommerce(t, app, org.ID, account.Name, "shop", true)

	srv := newStoreServerWith(t, []any{}, nil, map[string]any{
		"id":       42,
		"name":     "Demo",
		"currency": "INR",
	})
	useStoreREST(t, srv)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceRESTURL: srv.URL,
		CommerceStoreID: "42",
	})

	req := testutil.NewJSONRequest(t, map[string]any{
		"kind":          "products",
		"collection_id": "57",
		"page":          1,
	})
	testutil.SetAuthContext(req, org.ID, user.ID)
	testutil.SetPathParam(req, "id", contact.ID.String())

	require.NoError(t, app.SendCommerceCatalog(req))
	assert.Equal(t, fasthttp.StatusBadRequest, testutil.GetResponseStatusCode(req))
}

func TestCommerceCatalogValidatePage(t *testing.T) {
	assert.NoError(t, commerceCatalogValidatePage(1, 0, 0))
	assert.Error(t, commerceCatalogValidatePage(2, 0, 0))
	assert.NoError(t, commerceCatalogValidatePage(1, 12, 10))
	assert.NoError(t, commerceCatalogValidatePage(2, 12, 2))
	assert.Error(t, commerceCatalogValidatePage(3, 12, 0))
}
