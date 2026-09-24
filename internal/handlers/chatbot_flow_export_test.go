package handlers_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func sampleChatFlowGraph(gotoFlowID, teamID string) map[string]any {
	return map[string]any{
		"version":    2,
		"entry_node": "start",
		"nodes": []any{
			map[string]any{
				"id":    "start",
				"type":  "start",
				"label": "Start",
				"position": map[string]any{
					"x": 0.0,
					"y": 0.0,
				},
				"config": map[string]any{},
			},
			map[string]any{
				"id":    "msg",
				"type":  "message",
				"label": "Welcome",
				"position": map[string]any{
					"x": 200.0,
					"y": 0.0,
				},
				"config": map[string]any{
					"message": "Hello",
				},
			},
			map[string]any{
				"id":    "jump",
				"type":  "goto_flow",
				"label": "Jump",
				"position": map[string]any{
					"x": 400.0,
					"y": 0.0,
				},
				"config": map[string]any{
					"flow_id": gotoFlowID,
				},
			},
			map[string]any{
				"id":    "handoff",
				"type":  "transfer",
				"label": "Transfer",
				"position": map[string]any{
					"x": 600.0,
					"y": 0.0,
				},
				"config": map[string]any{
					"team_id": teamID,
					"body":    "Connecting…",
				},
			},
		},
		"edges": []any{
			map[string]any{"from": "start", "to": "msg", "condition": "default"},
			map[string]any{"from": "msg", "to": "jump", "condition": "default"},
			map[string]any{"from": "jump", "to": "handoff", "condition": "default"},
		},
	}
}

func createTestChatbotFlowWithGraph(t *testing.T, app *handlers.App, orgID uuid.UUID, name string, graph map[string]any) *models.ChatbotFlow {
	t.Helper()

	flow := &models.ChatbotFlow{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  orgID,
		WhatsAppAccount: "primary",
		Name:            name,
		Description:     "Test flow with graph",
		TriggerKeywords: models.StringArray{"hello"},
		IsEnabled:       true,
		Graph:           models.JSONB(graph),
	}
	require.NoError(t, app.DB.Create(flow).Error)
	return flow
}

func TestApp_ExportChatbotFlow(t *testing.T) {
	t.Parallel()

	t.Run("success strips ids and returns envelope", func(t *testing.T) {
		app := newTestApp(t)
		org := testutil.CreateTestOrganization(t, app.DB)
		perms := getChatbotFlowPermissions(t, app)
		role := testutil.CreateTestRole(t, app.DB, org.ID, "flow-admin", perms)
		user := testutil.CreateTestUser(t, app.DB, org.ID,
			testutil.WithEmail(testutil.UniqueEmail("export-flow")),
			testutil.WithRoleID(&role.ID),
		)

		gotoID := uuid.New().String()
		teamID := uuid.New().String()
		flow := createTestChatbotFlowWithGraph(t, app, org.ID, "Support Flow", sampleChatFlowGraph(gotoID, teamID))

		req := testutil.NewGETRequest(t)
		testutil.SetAuthContext(req, org.ID, user.ID)
		testutil.SetPathParam(req, "id", flow.ID.String())

		err := app.ExportChatbotFlow(req)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))
		assert.Contains(t, string(req.RequestCtx.Response.Header.Peek("Content-Disposition")), "Support-Flow.json")

		var envelope handlers.ChatbotFlowExportEnvelope
		require.NoError(t, json.Unmarshal(testutil.GetResponseBody(req), &envelope))
		assert.Equal(t, "whatomate.chatbot_flow", envelope.Format)
		assert.Equal(t, 1, envelope.FormatVersion)
		assert.NotEmpty(t, envelope.ExportedAt)
		assert.Equal(t, "Support Flow", envelope.Flow.Name)
		assert.Equal(t, "primary", envelope.Flow.WhatsAppAccount)
		assert.Equal(t, []string{"hello"}, envelope.Flow.TriggerKeywords)
		require.NotNil(t, envelope.Flow.Graph)
		assert.EqualValues(t, 2, envelope.Flow.Graph["version"])

		// Envelope must not leak org / flow identity fields
		raw := string(testutil.GetResponseBody(req))
		assert.NotContains(t, raw, `"organization_id"`)
		assert.NotContains(t, raw, flow.ID.String())
		assert.NotContains(t, raw, `"enabled"`)
		assert.NotContains(t, raw, `"is_enabled"`)

		// Cross-ref IDs preserved in graph
		nodes, ok := envelope.Flow.Graph["nodes"].([]any)
		require.True(t, ok)
		foundGoto, foundTeam := false, false
		for _, n := range nodes {
			node := n.(map[string]any)
			cfg, _ := node["config"].(map[string]any)
			if node["type"] == "goto_flow" {
				assert.Equal(t, gotoID, cfg["flow_id"])
				foundGoto = true
			}
			if node["type"] == "transfer" {
				assert.Equal(t, teamID, cfg["team_id"])
				foundTeam = true
			}
		}
		assert.True(t, foundGoto)
		assert.True(t, foundTeam)
	})

	t.Run("not found", func(t *testing.T) {
		app := newTestApp(t)
		org := testutil.CreateTestOrganization(t, app.DB)
		perms := getChatbotFlowPermissions(t, app)
		role := testutil.CreateTestRole(t, app.DB, org.ID, "flow-admin", perms)
		user := testutil.CreateTestUser(t, app.DB, org.ID,
			testutil.WithEmail(testutil.UniqueEmail("export-flow-nf")),
			testutil.WithRoleID(&role.ID),
		)

		req := testutil.NewGETRequest(t)
		testutil.SetAuthContext(req, org.ID, user.ID)
		testutil.SetPathParam(req, "id", uuid.New().String())

		err := app.ExportChatbotFlow(req)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusNotFound, testutil.GetResponseStatusCode(req))
	})
}

func TestApp_ImportChatbotFlow(t *testing.T) {
	t.Parallel()

	t.Run("success creates disabled flow with intact cross-refs", func(t *testing.T) {
		app := newTestApp(t)
		org := testutil.CreateTestOrganization(t, app.DB)
		perms := getChatbotFlowPermissions(t, app)
		role := testutil.CreateTestRole(t, app.DB, org.ID, "flow-admin", perms)
		user := testutil.CreateTestUser(t, app.DB, org.ID,
			testutil.WithEmail(testutil.UniqueEmail("import-flow")),
			testutil.WithRoleID(&role.ID),
		)

		gotoID := uuid.New().String()
		teamID := uuid.New().String()
		envelope := handlers.ChatbotFlowExportEnvelope{
			Format:        "whatomate.chatbot_flow",
			FormatVersion: 1,
			ExportedAt:    "2026-01-01T00:00:00Z",
			Flow: handlers.ChatbotFlowExportPayload{
				Name:            "Imported Flow",
				Description:     "From export",
				TriggerKeywords: []string{"import-me"},
				WhatsAppAccount: "primary",
				Graph:           sampleChatFlowGraph(gotoID, teamID),
			},
		}

		req := testutil.NewJSONRequest(t, envelope)
		testutil.SetAuthContext(req, org.ID, user.ID)

		err := app.ImportChatbotFlow(req)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

		var resp struct {
			Data struct {
				ID      string `json:"id"`
				Message string `json:"message"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(testutil.GetResponseBody(req), &resp))
		assert.Equal(t, "Flow imported successfully", resp.Data.Message)

		parsedID, err := uuid.Parse(resp.Data.ID)
		require.NoError(t, err)

		var flow models.ChatbotFlow
		require.NoError(t, app.DB.First(&flow, "id = ?", parsedID).Error)
		assert.Equal(t, "Imported Flow", flow.Name)
		assert.False(t, flow.IsEnabled)
		assert.Equal(t, org.ID, flow.OrganizationID)
		assert.Equal(t, "primary", flow.WhatsAppAccount)

		rawGraph, err := json.Marshal(flow.Graph)
		require.NoError(t, err)
		assert.Contains(t, string(rawGraph), gotoID)
		assert.Contains(t, string(rawGraph), teamID)
	})

	t.Run("rejects bad format", func(t *testing.T) {
		app := newTestApp(t)
		org := testutil.CreateTestOrganization(t, app.DB)
		perms := getChatbotFlowPermissions(t, app)
		role := testutil.CreateTestRole(t, app.DB, org.ID, "flow-admin", perms)
		user := testutil.CreateTestUser(t, app.DB, org.ID,
			testutil.WithEmail(testutil.UniqueEmail("import-bad-fmt")),
			testutil.WithRoleID(&role.ID),
		)

		req := testutil.NewJSONRequest(t, map[string]any{
			"format":         "other",
			"format_version": 1,
			"flow": map[string]any{
				"name":  "X",
				"graph": sampleChatFlowGraph(uuid.New().String(), uuid.New().String()),
			},
		})
		testutil.SetAuthContext(req, org.ID, user.ID)

		err := app.ImportChatbotFlow(req)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusBadRequest, testutil.GetResponseStatusCode(req))
	})

	t.Run("rejects bad format version", func(t *testing.T) {
		app := newTestApp(t)
		org := testutil.CreateTestOrganization(t, app.DB)
		perms := getChatbotFlowPermissions(t, app)
		role := testutil.CreateTestRole(t, app.DB, org.ID, "flow-admin", perms)
		user := testutil.CreateTestUser(t, app.DB, org.ID,
			testutil.WithEmail(testutil.UniqueEmail("import-bad-ver")),
			testutil.WithRoleID(&role.ID),
		)

		req := testutil.NewJSONRequest(t, map[string]any{
			"format":         "whatomate.chatbot_flow",
			"format_version": 99,
			"flow": map[string]any{
				"name":  "X",
				"graph": sampleChatFlowGraph(uuid.New().String(), uuid.New().String()),
			},
		})
		testutil.SetAuthContext(req, org.ID, user.ID)

		err := app.ImportChatbotFlow(req)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusBadRequest, testutil.GetResponseStatusCode(req))
	})

	t.Run("rejects invalid graph", func(t *testing.T) {
		app := newTestApp(t)
		org := testutil.CreateTestOrganization(t, app.DB)
		perms := getChatbotFlowPermissions(t, app)
		role := testutil.CreateTestRole(t, app.DB, org.ID, "flow-admin", perms)
		user := testutil.CreateTestUser(t, app.DB, org.ID,
			testutil.WithEmail(testutil.UniqueEmail("import-bad-graph")),
			testutil.WithRoleID(&role.ID),
		)

		req := testutil.NewJSONRequest(t, map[string]any{
			"format":         "whatomate.chatbot_flow",
			"format_version": 1,
			"flow": map[string]any{
				"name": "X",
				"graph": map[string]any{
					"version": 1,
				},
			},
		})
		testutil.SetAuthContext(req, org.ID, user.ID)

		err := app.ImportChatbotFlow(req)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusBadRequest, testutil.GetResponseStatusCode(req))
	})
}

func TestApp_DuplicateChatbotFlow(t *testing.T) {
	t.Parallel()

	t.Run("success copies graph and disables", func(t *testing.T) {
		app := newTestApp(t)
		org := testutil.CreateTestOrganization(t, app.DB)
		perms := getChatbotFlowPermissions(t, app)
		role := testutil.CreateTestRole(t, app.DB, org.ID, "flow-admin", perms)
		user := testutil.CreateTestUser(t, app.DB, org.ID,
			testutil.WithEmail(testutil.UniqueEmail("dup-flow")),
			testutil.WithRoleID(&role.ID),
		)

		gotoID := uuid.New().String()
		teamID := uuid.New().String()
		source := createTestChatbotFlowWithGraph(t, app, org.ID, "Original", sampleChatFlowGraph(gotoID, teamID))

		req := testutil.NewJSONRequest(t, nil)
		req.RequestCtx.Request.Header.SetMethod("POST")
		testutil.SetAuthContext(req, org.ID, user.ID)
		testutil.SetPathParam(req, "id", source.ID.String())

		err := app.DuplicateChatbotFlow(req)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

		var resp struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(testutil.GetResponseBody(req), &resp))
		parsedID, err := uuid.Parse(resp.Data.ID)
		require.NoError(t, err)
		assert.NotEqual(t, source.ID, parsedID)

		var copy models.ChatbotFlow
		require.NoError(t, app.DB.First(&copy, "id = ?", parsedID).Error)
		assert.Equal(t, "Original (Copy)", copy.Name)
		assert.False(t, copy.IsEnabled)
		assert.Equal(t, source.WhatsAppAccount, copy.WhatsAppAccount)

		rawGraph, err := json.Marshal(copy.Graph)
		require.NoError(t, err)
		assert.Contains(t, string(rawGraph), gotoID)
		assert.Contains(t, string(rawGraph), teamID)
	})
}
