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
		"id":                        float64(7),
		"name":                      "Demo Store",
		"description":               "Handmade goods",
		"address":                   "Vadakara, Kozhikode",
		"country":                   "IN",
		"delivery_modes":            []any{"PICKUP_FROM_STORE"},
		"delivery_radius":           float64(16),
		"free_delivery_radius":      float64(8),
		"location_based_delivery":   true,
		"commerce_contract_version": "1",
		"capabilities": map[string]any{
			"fulfillment_slots": false,
		},
		"logo":        "https://example.com/logo.png",
		"cover_image": "https://example.com/cover.png",
	})
	assert.Equal(t, map[string]any{
		"id":                        float64(7),
		"name":                      "Demo Store",
		"description":               "Handmade goods",
		"address":                   "Vadakara, Kozhikode",
		"country":                   "IN",
		"delivery_modes":            []any{"PICKUP_FROM_STORE"},
		"delivery_radius":           float64(16),
		"free_delivery_radius":      float64(8),
		"location_based_delivery":   true,
		"commerce_contract_version": "1",
		"capabilities": map[string]any{
			"fulfillment_slots": false,
		},
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
		"image":            "https://example.com/cat.png",
		"handoff_policy":   "none",
	}, out)
	assert.NotContains(t, out, "tags")
}

func TestAsObjectPageEnvelopePreservesMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
	}{
		{name: "products envelope", key: "products"},
		{name: "categories envelope", key: "categories"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, meta, err := asObjectPage(map[string]any{
				"count":    float64(23),
				"limit":    float64(10),
				"offset":   float64(10),
				"has_more": true,
				tc.key:     []any{map[string]any{"id": float64(3), "name": "Cake"}},
			}, tc.key)
			require.NoError(t, err)
			require.Len(t, items, 1)
			assert.Equal(t, 23, meta.Count)
			assert.Equal(t, 10, meta.Limit)
			assert.Equal(t, 10, meta.Offset)
			assert.True(t, meta.HasMore)
		})
	}
}

func TestListArgsIncludeCategoryAndPagination(t *testing.T) {
	productArgs, err := productListArgs(7, " cake ", "12", 10, 20)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"store_id":    7,
		"search":      "cake",
		"category_id": 12,
		"limit":       10,
		"offset":      20,
	}, productArgs)

	categoryArgs, err := categoryListArgs(7, "12", 1, 0, " cakes ", []string{"dessert"}, "and")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"store_id":    7,
		"category_id": 12,
		"limit":       1,
		"offset":      0,
		"search":      "cakes",
		"tags":        []string{"dessert"},
		"tags_op":     "and",
	}, categoryArgs)
}

func TestDecodeCategoryValidatesConfig(t *testing.T) {
	category := decodeCategory(map[string]any{
		"id":              float64(12),
		"name":            "Custom cakes",
		"ai_instructions": "Ask for a reference.",
		"handoff_policy":  "invalid",
		"required_capture_fields": []any{
			map[string]any{"key": "writing", "label": "Cake writing", "type": "text", "required": true},
			map[string]any{"key": "", "label": "Broken", "type": "text"},
		},
	})
	assert.Equal(t, "none", category.HandoffPolicy)
	require.Len(t, category.RequiredCaptureFields, 1)
	assert.Equal(t, "writing", category.RequiredCaptureFields[0].Key)
}

func TestDecodeCategoryImageFormsPreserveFields(t *testing.T) {
	base := map[string]any{
		"id":               float64(12),
		"name":             "Custom cakes",
		"description":      "Made to order",
		"listing_priority": float64(-2),
		"ai_instructions":  "Ask for a reference.",
		"handoff_policy":   "after_capture",
		"handoff_message":  "A baker will help next.",
		"required_capture_fields": []any{
			map[string]any{"key": "writing", "label": "Cake writing", "type": "text", "required": true},
		},
	}

	t.Run("legacy string", func(t *testing.T) {
		input := cloneMap(base)
		input["image"] = " https://example.com/legacy.jpg "
		category := decodeCategory(input)
		assert.Equal(t, "https://example.com/legacy.jpg", category.Image)
		assert.Equal(t, "Custom cakes", category.Name)
		assert.Equal(t, -2, category.ListingPriority)
		assert.Equal(t, "Ask for a reference.", category.AIInstructions)
		assert.Equal(t, "after_capture", category.HandoffPolicy)
		require.Len(t, category.RequiredCaptureFields, 1)
	})

	t.Run("serializer object prefers original_url", func(t *testing.T) {
		input := cloneMap(base)
		input["image"] = map[string]any{
			"id":           float64(99),
			"image":        "https://example.com/image.webp",
			"url":          "https://example.com/url.webp",
			"original_url": "https://example.com/original.jpg",
		}
		category := decodeCategory(input)
		assert.Equal(t, "https://example.com/original.jpg", category.Image)
		assert.Equal(t, "Made to order", category.Description)
		assert.Equal(t, "A baker will help next.", category.HandoffMessage)
		assert.Equal(t, "Ask for a reference.", category.AIInstructions)
	})

	t.Run("serializer object falls back to url then image", func(t *testing.T) {
		assert.Equal(t, "https://example.com/original.jpg", categoryImageURL(map[string]any{
			"url":          "https://example.com/url.webp",
			"original_url": "https://example.com/original.jpg",
		}))
		assert.Equal(t, "https://example.com/url.jpg", categoryImageURL(map[string]any{
			"url": "https://example.com/url.jpg",
		}))
		assert.Equal(t, "https://example.com/original.jpg", categoryImageURL(map[string]any{
			"original_url": "https://example.com/original.jpg",
		}))
	})
}

func TestCloneMapDoesNotShareNestedValues(t *testing.T) {
	original := map[string]any{
		"name":  "Demo",
		"modes": []any{"PICKUP_FROM_STORE"},
		"meta":  map[string]any{"country": "IN"},
	}
	cloned := cloneMap(original)

	cloned["modes"].([]any)[0] = "DELIVERY_TO_LOCATION"
	cloned["meta"].(map[string]any)["country"] = "US"

	assert.Equal(t, "PICKUP_FROM_STORE", original["modes"].([]any)[0])
	assert.Equal(t, "IN", original["meta"].(map[string]any)["country"])
}

func TestReadOnlyToolsAreSafeToRetry(t *testing.T) {
	assert.True(t, isReadOnlyTool("get_store"))
	assert.True(t, isReadOnlyTool("list_products"))
	assert.False(t, isReadOnlyTool("create_order"))
	assert.False(t, isReadOnlyTool("update_order"))
}
