package handlers

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
)

// prepareListWindow loads further API pages when the WhatsApp page about to
// be sent needs rows that are not in the session yet.
func (a *App) prepareListWindow(ctx *chatNodeCtx, cfg map[string]any) error {
	if ctx == nil || ctx.session == nil {
		return nil
	}
	mode := stringFromConfig(cfg, "mode")
	if mode != "list" && mode != "carousel" {
		return nil
	}
	key := codedflow.ItemsVar(cfg)
	if key == "" {
		return nil
	}
	if ctx.session.SessionData == nil {
		ctx.session.SessionData = models.JSONB{}
	}
	chat := newCodedChat(ctx)
	for i := 0; i < 20; i++ {
		data := ctx.session.SessionData
		items, _ := anySlice(data[key])
		page := codedflow.ReadPage(data, key)
		if page.Operation == "" || !page.HasMore {
			return nil
		}
		if len(items) >= codedflow.NeededCount(codedflow.Cursor(data, key), len(items), page) {
			return nil
		}
		if err := a.ExtendListPage(chat, key); err != nil {
			if errors.Is(err, codedflow.ErrPreviewNeedsMock) {
				return err
			}
			a.Log.Warn("list page fetch failed", "items", key, "operation", page.Operation, "error", err)
			page = codedflow.ReadPage(data, key)
			page.HasMore = false
			page.Next = ""
			codedflow.WritePage(data, key, page)
			return nil
		}
	}
	return nil
}

// ExtendListPage calls the list operation's next page and appends the rows.
func (a *App) ExtendListPage(chat codedflow.Chat, itemsKey string) error {
	ctx := unwrapChat(chat)
	if ctx == nil || ctx.session == nil || ctx.session.SessionData == nil {
		return fmt.Errorf("list page has no session")
	}
	data := ctx.session.SessionData
	page := codedflow.ReadPage(data, itemsKey)
	if page.Operation == "" || !page.HasMore {
		page.HasMore = false
		codedflow.WritePage(data, itemsKey, page)
		return nil
	}
	items, _ := anySlice(data[itemsKey])
	params := map[string]any{}
	for key, value := range page.Params {
		params[key] = value
	}
	if page.Limit > 0 {
		params["limit"] = strconv.Itoa(page.Limit)
	}
	params["offset"] = strconv.Itoa(len(items))
	if page.APIType != "mcp" && page.Next != "" {
		params["next_url"] = page.Next
	}
	apiType := page.APIType
	if apiType == "" {
		apiType = "rest"
	}
	out, err := a.execChatTiqrStoreAPI(&ChatNode{
		ID:   itemsKey + "_next",
		Type: ChatNodeTiqrStoreAPI,
		Config: map[string]any{
			"api_type":  apiType,
			"operation": page.Operation,
			"params":    params,
		},
	}, ctx)
	if err != nil {
		return err
	}
	if out.outcome != "http:2xx" || ctx.lastTiqr == nil {
		page.HasMore = false
		page.Next = ""
		codedflow.WritePage(data, itemsKey, page)
		return fmt.Errorf("next list page failed")
	}
	more, nextPage := codedflow.PageFromPayload(ctx.lastTiqr)
	if page.Operation == "list_orders_by_phone" {
		more = tiqrecommerce.ReshapeOrdersForList(more)
	}
	nextPage = nextPage.WithRequest(page)
	added := codedflow.MergeListItems(data, itemsKey, more, nextPage)
	if added == 0 {
		return fmt.Errorf("next list page returned no new rows")
	}
	return nil
}
