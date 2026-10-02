package codedflow

import (
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

const (
	// ShowMoreID is the reserved WhatsApp row id for the next page of a list.
	ShowMoreID = "__show_more__"
	// ShowMoreAltID is the second carousel button when a card has two replies.
	ShowMoreAltID = "__show_more_b__"
	// ShowMoreTitle is the customer-facing label for the next page.
	ShowMoreTitle = "Show more"
	// ShowMoreDescription is the list-row description under Show more.
	ShowMoreDescription = "See the next items"

	// WhatsAppChoiceCap is the most rows or cards WhatsApp accepts in one message.
	WhatsAppChoiceCap = 10
	// ListPageSize is how many real items are shown when another page follows.
	// The last slot is Show more.
	ListPageSize = 9
)

// ListCursorKey is the session key for how far into a list the customer has paged.
func ListCursorKey(itemsVar string) string { return "_list_cursor_" + itemsVar }

// ListShownKey is how many real items the last page of this list displayed.
func ListShownKey(itemsVar string) string { return "_list_shown_" + itemsVar }

// ListPageKey is the session key for API page metadata of a list variable.
func ListPageKey(itemsVar string) string { return "_list_page_" + itemsVar }

// ListPage is the pagination state for one session list variable.
type ListPage struct {
	Count     int
	Limit     int
	Offset    int
	Next      string
	HasMore   bool
	Operation string
	APIType   string
	Params    map[string]string
}

// IsListOperation reports TiQR operations that return a page of rows.
func IsListOperation(operation string) bool {
	switch strings.TrimSpace(operation) {
	case "list_collections", "search_collections", "list_products", "search_products",
		"list_product_options", "list_faqs", "list_orders_by_phone":
		return true
	default:
		return false
	}
}

// ItemsVar reads a buttons config items variable, without {{ }} wrappers.
func ItemsVar(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	key := strings.TrimSpace(asString(cfg["items_var"]))
	key = strings.TrimSuffix(strings.TrimPrefix(key, "{{"), "}}")
	return strings.TrimSpace(key)
}

// IsShowMoreID reports the reserved next-page row ids.
func IsShowMoreID(id string) bool {
	switch strings.TrimSpace(id) {
	case ShowMoreID, ShowMoreAltID:
		return true
	default:
		return false
	}
}

// IsShowMoreRequest reports a tap or a typed "Show more" on the current page.
func IsShowMoreRequest(id, title string) bool {
	if IsShowMoreID(id) {
		return true
	}
	if strings.TrimSpace(id) != "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(title), ShowMoreTitle)
}

// IncludesShowMore reports whether the rows just built offer another page.
func IncludesShowMore(buttons []map[string]any) bool {
	for _, button := range buttons {
		if IsShowMoreID(fieldString(button, "id")) || IsShowMoreID(fieldString(button, "id_2")) {
			return true
		}
	}
	return false
}

// Cursor is the index of the first item on the page currently shown.
func Cursor(data models.JSONB, itemsVar string) int {
	if data == nil || itemsVar == "" {
		return 0
	}
	n := anyToInt(data[ListCursorKey(itemsVar)])
	if n < 0 {
		return 0
	}
	return n
}

// SetCursor stores the index of the first item on the next page to render.
func SetCursor(data models.JSONB, itemsVar string, cursor int) {
	if data == nil || itemsVar == "" {
		return
	}
	if cursor < 0 {
		cursor = 0
	}
	data[ListCursorKey(itemsVar)] = cursor
}

// Shown is how many real items were on the page last sent.
func Shown(data models.JSONB, itemsVar string) int {
	if data == nil || itemsVar == "" {
		return 0
	}
	return anyToInt(data[ListShownKey(itemsVar)])
}

// SetShown records how many real items the page about to be sent contains.
func SetShown(data models.JSONB, itemsVar string, n int) {
	if data == nil || itemsVar == "" {
		return
	}
	if n < 0 {
		n = 0
	}
	data[ListShownKey(itemsVar)] = n
}

// AdvanceCursor moves past the real items on the page the customer just saw.
func AdvanceCursor(data models.JSONB, itemsVar string) {
	step := Shown(data, itemsVar)
	if step <= 0 {
		step = ListPageSize
	}
	SetCursor(data, itemsVar, Cursor(data, itemsVar)+step)
}

// ClearListCursor forgets paging for a list whose rows were just replaced.
func ClearListCursor(data models.JSONB, itemsVar string) {
	if data == nil || itemsVar == "" {
		return
	}
	delete(data, ListCursorKey(itemsVar))
	delete(data, ListShownKey(itemsVar))
}

// ClearListPage forgets API pagination for a list whose rows were just replaced.
func ClearListPage(data models.JSONB, itemsVar string) {
	if data == nil || itemsVar == "" {
		return
	}
	delete(data, ListPageKey(itemsVar))
}

// ReadPage loads API pagination stored beside a list variable.
func ReadPage(data models.JSONB, itemsVar string) ListPage {
	if data == nil || itemsVar == "" {
		return ListPage{}
	}
	raw, ok := asStringMap(data[ListPageKey(itemsVar)])
	if !ok {
		return ListPage{}
	}
	page := ListPage{
		Count:     anyToInt(raw["count"]),
		Limit:     anyToInt(raw["limit"]),
		Offset:    anyToInt(raw["offset"]),
		Next:      asString(raw["next"]),
		Operation: asString(raw["operation"]),
		APIType:   asString(raw["api_type"]),
	}
	if hasMore, ok := raw["has_more"].(bool); ok {
		page.HasMore = hasMore
	}
	if params, ok := asStringMap(raw["params"]); ok {
		page.Params = map[string]string{}
		for key, value := range params {
			page.Params[key] = asString(value)
		}
	}
	return page
}

// WritePage stores API pagination beside a list variable.
func WritePage(data models.JSONB, itemsVar string, page ListPage) {
	if data == nil || itemsVar == "" {
		return
	}
	raw := map[string]any{
		"count":     page.Count,
		"limit":     page.Limit,
		"offset":    page.Offset,
		"next":      page.Next,
		"has_more":  page.HasMore,
		"operation": page.Operation,
		"api_type":  page.APIType,
	}
	if len(page.Params) > 0 {
		params := make(map[string]any, len(page.Params))
		for key, value := range page.Params {
			params[key] = value
		}
		raw["params"] = params
	}
	data[ListPageKey(itemsVar)] = raw
}

// HasListPage reports whether this variable already has API pagination state.
func HasListPage(data models.JSONB, itemsVar string) bool {
	page := ReadPage(data, itemsVar)
	return page.Operation != "" || page.Next != "" || page.HasMore || page.Count > 0
}

// CopyListPage copies pagination metadata from one list variable to another.
func CopyListPage(data models.JSONB, from, to string) {
	WritePage(data, to, ReadPage(data, from))
}

// PageFromPayload reads rows and pagination from a TiQR list response.
func PageFromPayload(payload map[string]any) ([]any, ListPage) {
	if payload == nil {
		return nil, ListPage{}
	}
	var items []any
	for _, key := range []string{"results", "products", "categories", "options", "orders", "faqs"} {
		if list, ok := anySlice(payload[key]); ok {
			items = list
			break
		}
	}
	page := ListPage{
		Count:  anyToInt(payload["count"]),
		Limit:  anyToInt(payload["limit"]),
		Offset: anyToInt(payload["offset"]),
		Next:   asString(payload["next"]),
	}
	if hasMore, ok := payload["has_more"].(bool); ok {
		page.HasMore = hasMore
	}
	if page.Count < len(items) {
		page.Count = len(items)
	}
	if page.Next != "" {
		page.HasMore = true
	}
	loadedThrough := page.Offset + len(items)
	if page.Count > loadedThrough {
		page.HasMore = true
	}
	return items, page
}

// WithRequest keeps the original list call on a follow-up page.
func (next ListPage) WithRequest(prev ListPage) ListPage {
	next.Operation = prev.Operation
	next.APIType = prev.APIType
	next.Params = cloneParams(prev.Params)
	if next.Limit == 0 {
		next.Limit = prev.Limit
	}
	if next.Count < prev.Count {
		next.Count = prev.Count
	}
	if next.Next != "" {
		next.HasMore = true
	}
	return next
}

// NeededCount is how many rows must be loaded before the current page can render.
func NeededCount(cursor, loaded int, page ListPage) int {
	if cursor < 0 {
		cursor = 0
	}
	total := page.Count
	if total < loaded {
		total = loaded
	}
	remain := total - cursor
	if remain < 0 {
		remain = 0
	}
	if remain > WhatsAppChoiceCap {
		need := cursor + ListPageSize
		if need > total {
			need = total
		}
		return need
	}
	want := cursor + remain
	if page.HasMore && loaded < total && want < total {
		want = total
	}
	if page.HasMore && loaded < total && want <= loaded {
		want = loaded + 1
	}
	if page.HasMore && cursor+ListPageSize > loaded && want <= loaded {
		want = loaded + 1
	}
	if want < cursor {
		return cursor
	}
	return want
}

// Window returns the rows for one WhatsApp list or carousel page.
// showMore is true when the 10th slot should be Show more.
func Window(items []any, cursor int, page ListPage) (out []any, showMore bool) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(items) {
		cursor = len(items)
	}
	total := page.Count
	if total < len(items) {
		total = len(items)
	}
	remain := total - cursor
	if remain < 0 {
		remain = 0
	}
	showMore = remain > WhatsAppChoiceCap || (page.HasMore && len(items) < total)
	if !showMore && page.HasMore && cursor < len(items) && cursor+WhatsAppChoiceCap > len(items) {
		showMore = true
	}
	size := remain
	if showMore {
		size = ListPageSize
	}
	available := len(items) - cursor
	if size > available {
		size = available
	}
	if size < 0 {
		size = 0
	}
	if size == 0 {
		return nil, showMore && page.HasMore
	}
	return items[cursor : cursor+size], showMore
}

// MergeListItems appends a follow-up page onto the list already in the session.
// It returns how many new rows were added. A page of duplicates ends pagination.
func MergeListItems(data models.JSONB, key string, more []any, next ListPage) int {
	if data == nil || key == "" {
		return 0
	}
	current, _ := anySlice(data[key])
	merged := appendUniqueItems(current, more)
	added := len(merged) - len(current)
	data[key] = merged
	if added == 0 {
		next.HasMore = false
		next.Next = ""
	} else if next.Count > len(merged) || next.Next != "" {
		next.HasMore = true
	}
	WritePage(data, key, next)
	SyncListCall(data, key, merged)
	return added
}

// SyncListCall updates the latest saved call for this list so the next turn
// restores the rows loaded so far, including pages fetched after the first call.
func SyncListCall(data models.JSONB, key string, items []any) {
	if data == nil || key == "" {
		return
	}
	records, ok := anySlice(data[CallsKey])
	if !ok {
		return
	}
	for i := len(records) - 1; i >= 0; i-- {
		rec, ok := asStringMap(records[i])
		if !ok || asString(rec["var"]) != key || !callOK(rec) {
			continue
		}
		rec["value"] = items
		return
	}
}

func appendUniqueItems(dst, more []any) []any {
	seen := map[string]struct{}{}
	for _, item := range dst {
		if id := itemKey(item); id != "" {
			seen[id] = struct{}{}
		}
	}
	out := append([]any{}, dst...)
	for _, item := range more {
		id := itemKey(item)
		if id != "" {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
		}
		out = append(out, item)
	}
	return out
}

func itemKey(item any) string {
	row, ok := asStringMap(item)
	if !ok {
		return ""
	}
	return asString(row["id"])
}

func cloneParams(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
