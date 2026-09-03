package tickermcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeEndpoint(t *testing.T) {
	assert.Equal(t, "http://127.0.0.1:8100/mcp", normalizeEndpoint("http://127.0.0.1:8100"))
	assert.Equal(t, "http://127.0.0.1:8100/mcp", normalizeEndpoint("http://127.0.0.1:8100/"))
	assert.Equal(t, "http://127.0.0.1:8100/mcp", normalizeEndpoint("http://127.0.0.1:8100/mcp"))
	assert.Equal(t, "http://127.0.0.1:8100/mcp", normalizeEndpoint("http://127.0.0.1:8100/mcp/"))
}

func TestParseToolResultMultipleTextObjects(t *testing.T) {
	raw, err := parseToolResult(&mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: `{"id":1,"name":"A"}`},
			&mcp.TextContent{Text: `{"id":2,"name":"B"}`},
		},
	})
	require.NoError(t, err)
	list, err := asObjectList(raw)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "A", list[0]["name"])
	assert.Equal(t, "B", list[1]["name"])
}

func TestParseToolResultStructuredError(t *testing.T) {
	_, err := parseToolResult(&mcp.CallToolResult{
		StructuredContent: map[string]any{
			"error":   "Invalid or missing MCP API key",
			"details": nil,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid or missing MCP API key")
}

func TestParseToolResultIsError(t *testing.T) {
	// IsError path is handled in callTool; parse still works on content.
	raw, err := parseToolResult(&mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: `{"ok":true}`}},
	})
	require.NoError(t, err)
	m := raw.(map[string]any)
	assert.Equal(t, true, m["ok"])
}

func TestAsObjectListWrapped(t *testing.T) {
	list, err := asObjectList(map[string]any{
		"results": []any{map[string]any{"id": float64(3)}},
	})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.EqualValues(t, 3, list[0]["id"])
}

func TestAsObjectListSingleProductObject(t *testing.T) {
	list, err := asObjectList(map[string]any{
		"id":   float64(42),
		"name": "Vedika Jhumka",
	})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Vedika Jhumka", list[0]["name"])
}

func TestAsObjectListEmpty(t *testing.T) {
	list, err := asObjectList(nil)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestNormalizeJSONRawMessage(t *testing.T) {
	v := normalizeJSON(json.RawMessage(`{"a":1}`))
	m, ok := v.(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 1, m["a"])
}

func TestCompactStore(t *testing.T) {
	out := CompactStore(map[string]any{
		"id":                      float64(7),
		"name":                    "Demo Store",
		"description":             "Handmade goods",
		"address":                 "Vadakara, Kozhikode",
		"delivery_modes":          []any{"PICKUP_FROM_STORE"},
		"delivery_radius":         float64(16),
		"free_delivery_radius":    float64(8),
		"location_based_delivery": true,
		"logo":                    "https://example.com/logo.png",
		"cover_image":             "https://example.com/cover.png",
	})
	assert.Equal(t, map[string]any{
		"id":                      float64(7),
		"name":                    "Demo Store",
		"description":             "Handmade goods",
		"address":                 "Vadakara, Kozhikode",
		"delivery_modes":          []any{"PICKUP_FROM_STORE"},
		"delivery_radius":         float64(16),
		"free_delivery_radius":    float64(8),
		"location_based_delivery": true,
	}, out)
	assert.NotContains(t, out, "logo")
}

func TestCompactCategory(t *testing.T) {
	out := CompactCategory(map[string]any{
		"id":               float64(3),
		"name":             "Earrings",
		"description":      "Studs and jhumkas",
		"listing_priority": float64(2),
		"image":            "https://example.com/cat.png",
		"tags":             []any{"jewelry"},
	})
	assert.Equal(t, map[string]any{
		"id":               float64(3),
		"name":             "Earrings",
		"description":      "Studs and jhumkas",
		"listing_priority": float64(2),
	}, out)
	assert.NotContains(t, out, "image")
}
