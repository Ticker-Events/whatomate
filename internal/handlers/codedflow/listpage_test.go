package codedflow

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindow_ShowMoreWhenCountExceedsTen(t *testing.T) {
	items := make([]any, 11)
	for i := range items {
		items[i] = map[string]any{"id": i + 1}
	}
	page := ListPage{Count: 11}

	first, more := Window(items, 0, page)
	require.Len(t, first, ListPageSize)
	assert.True(t, more)
	assert.Equal(t, 1, anyToInt(asStringMapMust(first[0])["id"]))

	second, more := Window(items, ListPageSize, page)
	require.Len(t, second, 2)
	assert.False(t, more)
}

func TestWindow_TenItemsHaveNoShowMore(t *testing.T) {
	items := make([]any, 10)
	page, more := Window(items, 0, ListPage{Count: 10})
	require.Len(t, page, 10)
	assert.False(t, more)
}

func TestWindow_UnloadedTailStillShowsMore(t *testing.T) {
	items := make([]any, 50)
	page := ListPage{Count: 100, HasMore: true, Next: "https://store.example/next"}
	window, more := Window(items, 0, page)
	require.Len(t, window, ListPageSize)
	assert.True(t, more)

	need := NeededCount(45, len(items), page)
	assert.Equal(t, 54, need)
}

func TestNeededCount_FetchesLastPartialPage(t *testing.T) {
	page := ListPage{Count: 15, HasMore: true, Next: "https://store.example/next"}
	assert.Equal(t, 15, NeededCount(9, 10, page))
}

func TestPageFromPayload_CountAheadOfResults(t *testing.T) {
	items, page := PageFromPayload(map[string]any{
		"count":   float64(100),
		"limit":   float64(50),
		"offset":  float64(0),
		"next":    "https://store.example/products?offset=50",
		"results": []any{map[string]any{"id": "1"}},
	})
	require.Len(t, items, 1)
	assert.Equal(t, 100, page.Count)
	assert.Equal(t, 50, page.Limit)
	assert.True(t, page.HasMore)
	assert.Equal(t, "https://store.example/products?offset=50", page.Next)
}

func TestMergeListItems_AppendsAndStopsOnDuplicatePage(t *testing.T) {
	data := models.JSONB{
		"products": []any{map[string]any{"id": "1"}, map[string]any{"id": "2"}},
	}
	WritePage(data, "products", ListPage{Count: 4, HasMore: true, Operation: "list_products"})
	added := MergeListItems(data, "products", []any{
		map[string]any{"id": "2"},
		map[string]any{"id": "3"},
	}, ListPage{Count: 4, HasMore: true, Next: "https://store.example/next"}.WithRequest(ReadPage(data, "products")))
	assert.Equal(t, 1, added)
	items, ok := anySlice(data["products"])
	require.True(t, ok)
	require.Len(t, items, 3)
	assert.True(t, ReadPage(data, "products").HasMore)

	added = MergeListItems(data, "products", []any{map[string]any{"id": "3"}}, ListPage{Count: 3}.WithRequest(ReadPage(data, "products")))
	assert.Equal(t, 0, added)
	assert.False(t, ReadPage(data, "products").HasMore)
}

func TestReadPage_RoundTripsNumbersAsFloat(t *testing.T) {
	data := models.JSONB{}
	WritePage(data, "collections", ListPage{
		Count:     12,
		Limit:     20,
		Next:      "https://store.example/categories?offset=20",
		HasMore:   true,
		Operation: "list_collections",
		APIType:   "rest",
		Params:    map[string]string{"limit": "20"},
	})
	raw, ok := asStringMap(data[ListPageKey("collections")])
	require.True(t, ok)
	raw["count"] = float64(12)
	raw["limit"] = float64(20)
	page := ReadPage(data, "collections")
	assert.Equal(t, 12, page.Count)
	assert.Equal(t, 20, page.Limit)
	assert.Equal(t, "list_collections", page.Operation)
	assert.Equal(t, "20", page.Params["limit"])
}

func TestAdvanceCursor_UsesShownCount(t *testing.T) {
	data := models.JSONB{}
	SetShown(data, "options", 9)
	AdvanceCursor(data, "options")
	assert.Equal(t, 9, Cursor(data, "options"))
	SetShown(data, "options", 3)
	AdvanceCursor(data, "options")
	assert.Equal(t, 12, Cursor(data, "options"))
}

func asStringMapMust(item any) map[string]any {
	row, ok := asStringMap(item)
	if !ok {
		return map[string]any{}
	}
	return row
}
