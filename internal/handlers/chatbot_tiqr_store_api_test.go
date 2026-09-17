package handlers

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTiqrInvoker struct {
	lastName string
	lastArgs map[string]any
	result   any
	err      error
}

func (s *stubTiqrInvoker) CallTool(_ context.Context, name string, args map[string]any) (any, error) {
	s.lastName = name
	s.lastArgs = args
	return s.result, s.err
}

func (s *stubTiqrInvoker) Close() error { return nil }

func withTiqrInvoker(t *testing.T, stub *stubTiqrInvoker) {
	t.Helper()
	prev := newTiqrStoreInvoker
	newTiqrStoreInvoker = func(_, _ string) tiqrStoreInvoker { return stub }
	t.Cleanup(func() { newTiqrStoreInvoker = prev })
}

func newTiqrStoreFlow(t *testing.T, app *App, org *models.Organization, account *models.WhatsAppAccount, cfg map[string]any) *models.ChatbotFlow {
	t.Helper()
	flow := &models.ChatbotFlow{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  org.ID,
		WhatsAppAccount: account.Name,
		Name:            "tiqr-store-api-flow",
		IsEnabled:       true,
		Graph: models.JSONB{
			"version":    2,
			"entry_node": "store",
			"nodes": []any{
				map[string]any{"id": "store", "type": "tiqr_store_api", "label": "store api", "config": cfg},
				map[string]any{"id": "ok", "type": "message", "label": "success", "config": map[string]any{"message": "ok"}},
				map[string]any{"id": "bad", "type": "message", "label": "error", "config": map[string]any{"message": "boom"}},
				map[string]any{"id": "end", "type": "end", "label": "done"},
			},
			"edges": []any{
				map[string]any{"from": "store", "to": "ok", "condition": "http:2xx"},
				map[string]any{"from": "store", "to": "bad", "condition": "http:non2xx"},
				map[string]any{"from": "ok", "to": "end", "condition": "default"},
				map[string]any{"from": "bad", "to": "end", "condition": "default"},
			},
		},
	}
	require.NoError(t, app.DB.Create(flow).Error)
	return flow
}

func TestRunChatGraph_TiqrStoreAPI_MapsResponseOn2xx(t *testing.T) {
	stub := &stubTiqrInvoker{
		result: map[string]any{
			"products": []any{map[string]any{"name": "Chocolate Cake"}},
		},
	}
	withTiqrInvoker(t, stub)

	app, org, account, contact, session := newGraphTestFixtures(t)
	session.SessionData["query"] = "cake"
	require.NoError(t, app.DB.Save(session).Error)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceEnabled: true,
		CommerceMCPURL:  "http://mcp.test/mcp",
		CommerceStoreID: "42",
	})
	flow := newTiqrStoreFlow(t, app, org, account, map[string]any{
		"operation": "search_products",
		"params":    map[string]any{"search": "{{query}}", "limit": "10"},
		"response_mapping": map[string]any{
			"product_name": "products[0].name",
		},
	})

	require.NoError(t, app.runChatGraph(account, contact, session, flow, "start", "", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)
	assert.Equal(t, models.SessionStatusCompleted, session.Status)
	assert.Equal(t, "Chocolate Cake", session.SessionData["product_name"])
	assert.Equal(t, "42", session.SessionData["store_id"])
	assert.Equal(t, "list_products", stub.lastName)
	assert.Equal(t, 42, stub.lastArgs["store_id"])
	assert.Equal(t, "cake", stub.lastArgs["search"])
	assert.Equal(t, 10, stub.lastArgs["limit"])

	path := chatGraphPath(t, session)
	require.GreaterOrEqual(t, len(path), 2)
	assert.Equal(t, "http:2xx", path[0]["outcome"])
	assert.Equal(t, "ok", path[1]["node"])
}

func TestRunChatGraph_TiqrStoreAPI_MissingConfigRoutesNon2xx(t *testing.T) {
	app, org, account, contact, session := newGraphTestFixtures(t)
	flow := newTiqrStoreFlow(t, app, org, account, map[string]any{
		"operation": "list_products",
	})

	require.NoError(t, app.runChatGraph(account, contact, session, flow, "start", "", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)
	path := chatGraphPath(t, session)
	require.GreaterOrEqual(t, len(path), 2)
	assert.Equal(t, "http:non2xx", path[0]["outcome"])
	assert.Equal(t, "bad", path[1]["node"])
}

func TestRunChatGraph_TiqrStoreAPI_ToolErrorRoutesNon2xx(t *testing.T) {
	stub := &stubTiqrInvoker{err: fmt.Errorf("mcp down")}
	withTiqrInvoker(t, stub)

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceEnabled: true,
		CommerceMCPURL:  "http://mcp.test/mcp",
		CommerceStoreID: "21",
	})
	flow := newTiqrStoreFlow(t, app, org, account, map[string]any{
		"operation": "list_collections",
	})

	require.NoError(t, app.runChatGraph(account, contact, session, flow, "start", "", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)
	path := chatGraphPath(t, session)
	assert.Equal(t, "http:non2xx", path[0]["outcome"])
	assert.Equal(t, "bad", path[1]["node"])
}

func TestRunChatGraph_TiqrStoreAPI_CreateOrderInjectsStoreAndPhone(t *testing.T) {
	stub := &stubTiqrInvoker{
		result: map[string]any{"display_uid": "ST-1", "uuid": "ord-1"},
	}
	withTiqrInvoker(t, stub)

	app, org, account, contact, session := newGraphTestFixtures(t)
	createChatbotSettings(t, app, org.ID, account.Name, models.AIConfig{
		CommerceEnabled: true,
		CommerceMCPURL:  "http://mcp.test/mcp",
		CommerceStoreID: "99",
	})
	flow := newTiqrStoreFlow(t, app, org, account, map[string]any{
		"operation": "create_order",
		"params": map[string]any{
			"items":         `[{"product_option": 7, "quantity": 1}]`,
			"email":         "buyer@example.com",
			"delivery_mode": "PICKUP_FROM_STORE",
		},
		"response_mapping": map[string]any{"order_number": "display_uid"},
	})

	require.NoError(t, app.runChatGraph(account, contact, session, flow, "start", "", nil))
	require.NoError(t, app.DB.First(session, session.ID).Error)
	assert.Equal(t, "ST-1", session.SessionData["order_number"])
	assert.Equal(t, "create_order", stub.lastName)
	order, ok := stub.lastArgs["order"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 99, order["store"])
	assert.Equal(t, session.PhoneNumber, order["phone_number"])
	assert.Equal(t, "buyer@example.com", order["email"])
}

func TestBuildTiqrStoreToolArgs_SearchCollectionsRequiresQuery(t *testing.T) {
	_, _, err := buildTiqrStoreToolArgs("search_collections", 1, "", map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "search")

	tool, args, err := buildTiqrStoreToolArgs("search_collections", 1, "", map[string]string{"search": "cakes"})
	require.NoError(t, err)
	assert.Equal(t, "list_categories", tool)
	assert.Equal(t, "cakes", args["search"])
}

func TestBuildTiqrStoreToolArgs_UnknownOperation(t *testing.T) {
	_, _, err := buildTiqrStoreToolArgs("get_cart", 1, "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}
