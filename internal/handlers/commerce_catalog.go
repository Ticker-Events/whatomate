package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

const commerceCatalogPageSize = 10

type commerceCatalogSendRequest struct {
	Kind           string `json:"kind"`
	Page           int    `json:"page"`
	CollectionID   string `json:"collection_id"`
	CollectionName string `json:"collection_name"`
}

type commerceCatalogPageResponse struct {
	Count    int              `json:"count"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Results  []map[string]any `json:"results"`
}

// GetCommerceCatalogStatus reports whether TiQR Ecommerce is enabled for an account.
func (a *App) GetCommerceCatalogStatus(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}
	accountName, err := a.commerceCatalogAccount(r, orgID)
	if err != nil {
		return err
	}
	if !a.HasPermission(userID, models.ResourceChat, models.ActionRead, orgID) &&
		!a.HasPermission(userID, models.ResourceContacts, models.ActionRead, orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Permission denied", nil, "")
	}
	return r.SendEnvelope(map[string]any{
		"active": a.tiqrEcommerceEnabled(orgID, accountName),
	})
}

// ListCommerceCatalogCollections returns one page of store collections for the staff drawer.
func (a *App) ListCommerceCatalogCollections(r *fastglue.Request) error {
	orgID, userID, accountName, page, search, err := a.commerceCatalogListContext(r)
	if err != nil {
		return err
	}
	if !a.HasPermission(userID, models.ResourceChat, models.ActionRead, orgID) &&
		!a.HasPermission(userID, models.ResourceContacts, models.ActionRead, orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Permission denied", nil, "")
	}
	if !a.tiqrEcommerceEnabled(orgID, accountName) {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "TiQR Ecommerce is not enabled", nil, "")
	}
	operation := "list_collections"
	params := map[string]string{
		"limit":  strconv.Itoa(commerceCatalogPageSize),
		"offset": strconv.Itoa((page - 1) * commerceCatalogPageSize),
	}
	if search != "" {
		operation = "search_collections"
		params["search"] = search
	}
	payload, err := a.commerceCatalogStorePage(orgID, accountName, operation, params)
	if err != nil {
		a.Log.Error("Failed to list commerce catalog collections", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusBadGateway, "Failed to load collections", nil, "")
	}
	return r.SendEnvelope(commerceCatalogPageFromPayload(payload, page))
}

// ListCommerceCatalogProducts returns one page of products for a collection.
func (a *App) ListCommerceCatalogProducts(r *fastglue.Request) error {
	orgID, userID, accountName, page, _, err := a.commerceCatalogListContext(r)
	if err != nil {
		return err
	}
	if !a.HasPermission(userID, models.ResourceChat, models.ActionRead, orgID) &&
		!a.HasPermission(userID, models.ResourceContacts, models.ActionRead, orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Permission denied", nil, "")
	}
	if !a.tiqrEcommerceEnabled(orgID, accountName) {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "TiQR Ecommerce is not enabled", nil, "")
	}
	collectionID, _ := r.RequestCtx.UserValue("collection_id").(string)
	collectionID = strings.TrimSpace(collectionID)
	if collectionID == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "collection_id is required", nil, "")
	}
	payload, err := a.commerceCatalogStorePage(orgID, accountName, "list_products", map[string]string{
		"category_id": collectionID,
		"limit":       strconv.Itoa(commerceCatalogPageSize),
		"offset":      strconv.Itoa((page - 1) * commerceCatalogPageSize),
	})
	if err != nil {
		a.Log.Error("Failed to list commerce catalog products", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusBadGateway, "Failed to load products", nil, "")
	}
	return r.SendEnvelope(commerceCatalogPageFromPayload(payload, page))
}

// SendCommerceCatalog sends a collections list or product carousel/list to a contact.
// It does not write chatbot session data or change agent transfer state.
func (a *App) SendCommerceCatalog(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}
	contactID, err := parsePathUUID(r, "id", "contact")
	if err != nil {
		return nil
	}

	var req commerceCatalogSendRequest
	if err := json.Unmarshal(r.RequestCtx.PostBody(), &req); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid request body", nil, "")
	}
	req.Kind = strings.TrimSpace(strings.ToLower(req.Kind))
	req.CollectionID = strings.TrimSpace(req.CollectionID)
	req.CollectionName = strings.TrimSpace(req.CollectionName)
	if req.Page < 1 {
		req.Page = 1
	}

	var contact models.Contact
	query := a.DB.Where("id = ? AND organization_id = ?", contactID, orgID)
	if !a.HasPermission(userID, models.ResourceContacts, models.ActionRead, orgID) {
		query = query.Where("assigned_user_id = ?", userID)
	}
	if err := query.First(&contact).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Contact not found", nil, "")
	}

	account, err := a.resolveWhatsAppAccount(orgID, contact.WhatsAppAccount)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Failed to resolve WhatsApp account", nil, "")
	}
	if !a.tiqrEcommerceEnabled(orgID, account.Name) {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "TiQR Ecommerce is not enabled", nil, "")
	}

	opts := DefaultSendOptions()
	opts.SentByUserID = &userID
	opts.Async = false

	switch req.Kind {
	case "collections":
		if err := a.sendCommerceCatalogCollections(account, &contact, req.Page, opts); err != nil {
			return a.commerceCatalogSendError(r, err)
		}
	case "products":
		if req.CollectionID == "" {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "collection_id is required", nil, "")
		}
		if err := a.sendCommerceCatalogProducts(account, &contact, req.CollectionID, req.CollectionName, req.Page, opts); err != nil {
			return a.commerceCatalogSendError(r, err)
		}
	default:
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "kind must be collections or products", nil, "")
	}

	return r.SendEnvelope(map[string]any{"status": "ok"})
}

func (a *App) commerceCatalogSendError(r *fastglue.Request, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "out of range"),
		strings.Contains(msg, "no products"),
		strings.Contains(msg, "no collections"),
		strings.Contains(msg, "empty"):
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, msg, nil, "")
	default:
		a.Log.Error("Failed to send commerce catalog", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to send catalog", nil, "")
	}
}

func (a *App) sendCommerceCatalogCollections(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	page int,
	opts MessageSendOptions,
) error {
	payload, err := a.commerceCatalogStorePage(account.OrganizationID, account.Name, "list_collections", map[string]string{
		"limit":  strconv.Itoa(commerceCatalogPageSize),
		"offset": strconv.Itoa((page - 1) * commerceCatalogPageSize),
	})
	if err != nil {
		return err
	}
	items, count := commerceCatalogItems(payload)
	if err := commerceCatalogValidatePage(page, count, len(items)); err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("no collections on this page")
	}

	data := models.JSONB{"collections": items}
	cfg := map[string]any{
		"body":              "Choose a collection below and we'll show you the items in it.",
		"mode":              "list",
		"header":            "Our collections",
		"footer":            "Tap Browse to continue",
		"source":            "dynamic",
		"id_field":          "id",
		"items_var":         "collections",
		"list_button":       "Browse",
		"title_field":       "{{name}}",
		"section_title":     "Collections",
		"description_field": "{{description}}",
	}
	buttons, err := buttonsForNode(cfg, data)
	if err != nil {
		return err
	}
	if len(buttons) == 0 {
		return fmt.Errorf("no collections on this page")
	}
	return a.sendAgentInteractiveList(account, contact, cfg, buttons, opts)
}

func (a *App) sendCommerceCatalogProducts(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	collectionID, collectionName string,
	page int,
	opts MessageSendOptions,
) error {
	payload, err := a.commerceCatalogStorePage(account.OrganizationID, account.Name, "list_products", map[string]string{
		"category_id": collectionID,
		"limit":       strconv.Itoa(commerceCatalogPageSize),
		"offset":      strconv.Itoa((page - 1) * commerceCatalogPageSize),
	})
	if err != nil {
		return err
	}
	items, count := commerceCatalogItems(payload)
	if err := commerceCatalogValidatePage(page, count, len(items)); err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("no products on this page")
	}

	currency := "INR"
	if store, storeErr := a.commerceCatalogStorePage(account.OrganizationID, account.Name, "get_store", nil); storeErr == nil {
		if code := strings.TrimSpace(asString(store["currency"])); code != "" {
			currency = code
		}
	}
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	tiqrecommerce.StashSessionCurrency(session, currency)
	if collectionName == "" {
		collectionName = "this collection"
	}
	session.SessionData["collection_name"] = collectionName
	session.SessionData["collection_id"] = collectionID

	if len(items) == 1 {
		return a.sendAgentSingleProduct(account, contact, session, items, opts)
	}

	enriched := tiqrecommerce.EnrichProductsForCarousel(session, items)
	session.SessionData["products"] = enriched
	cfg := map[string]any{
		"body":               "Here is what's available in *{{collection_name}}*.\n\nTap *Add to cart* on the item you'd like.",
		"mode":               "carousel",
		"source":             "dynamic",
		"id_field":           "{{id}}",
		"items_var":          "products",
		"body_field":         "{{card_body}}",
		"media_field":        "{{images[0].original_url}}",
		"title_field":        "Add to cart",
		"fallback_media_url": tiqrecommerce.FallbackMedia,
	}
	body := processTemplate(stringFromConfig(cfg, "body"), session.SessionData)
	cards, err := carouselCardsForNode(cfg, session.SessionData)
	if err != nil {
		return err
	}
	for _, card := range cards {
		for _, key := range []string{"title", "title_2", "url", "body", "media_url", "id", "id_2"} {
			if s, ok := card[key].(string); ok && s != "" {
				card[key] = processTemplate(s, session.SessionData)
			}
		}
	}
	return a.sendAgentInteractiveCarousel(account, contact, body, cards, opts)
}

func (a *App) sendAgentSingleProduct(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	session *models.ChatbotSession,
	products []any,
	opts MessageSendOptions,
) error {
	session.SessionData["products"] = products
	cfg := map[string]any{
		"body":               "*{{products[0].name}}* ({{currency_symbol}}{{products[0].min_price}})\n\n{{products[0].description}}",
		"mode":               "reply",
		"source":             "dynamic",
		"dynamic_type":       "reply",
		"id_field":           "id",
		"items_var":          "products",
		"body_field":         "{{name}} ({{currency_symbol}}{{min_price}})",
		"title_field":        "Add to cart",
		"header_image":       "{{products[0].images[0].original_url}}",
		"fallback_media_url": tiqrecommerce.FallbackMedia,
	}
	body := processTemplate(stringFromConfig(cfg, "body"), session.SessionData)
	buttons, err := buttonsForNode(cfg, session.SessionData)
	if err != nil {
		return err
	}
	if len(buttons) == 0 {
		return fmt.Errorf("no products on this page")
	}
	for _, b := range buttons {
		for _, key := range []string{"title", "url", "phone_number", "description"} {
			if s, ok := b[key].(string); ok && s != "" {
				b[key] = processTemplate(s, session.SessionData)
			}
		}
	}
	headerImage := replyHeaderImage(cfg, session.SessionData)
	waButtons := whatsappReplyButtons(buttons)
	if len(waButtons) == 0 {
		return fmt.Errorf("no products on this page")
	}
	_, err = a.SendOutgoingMessage(context.Background(), OutgoingMessageRequest{
		Account:         account,
		Contact:         contact,
		Type:            models.MessageTypeInteractive,
		InteractiveType: "button",
		BodyText:        body,
		Buttons:         waButtons,
		HeaderImageURL:  headerImage,
	}, opts)
	return err
}

func (a *App) sendAgentInteractiveList(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	cfg map[string]any,
	buttons []map[string]any,
	opts MessageSendOptions,
) error {
	waButtons := make([]whatsapp.Button, 0, len(buttons))
	for i, btn := range buttons {
		if len(waButtons) >= 10 {
			break
		}
		title, _ := btn["title"].(string)
		if strings.TrimSpace(title) == "" {
			continue
		}
		id, _ := btn["id"].(string)
		if id == "" {
			id = fmt.Sprintf("row_%d", i+1)
		}
		desc, _ := btn["description"].(string)
		waButtons = append(waButtons, whatsapp.Button{
			ID:          id,
			Title:       truncateRunes(title, 24),
			Description: truncateRunes(desc, 72),
		})
	}
	if len(waButtons) == 0 {
		return fmt.Errorf("at least one list row is required")
	}
	_, err := a.SendOutgoingMessage(context.Background(), OutgoingMessageRequest{
		Account:         account,
		Contact:         contact,
		Type:            models.MessageTypeInteractive,
		InteractiveType: "list",
		ExplicitList:    true,
		BodyText:        stringFromConfig(cfg, "body"),
		Buttons:         waButtons,
		ButtonText:      truncateRunes(stringFromConfig(cfg, "list_button"), 20),
		HeaderText:      truncateRunes(stringFromConfig(cfg, "header"), 60),
		FooterText:      truncateRunes(stringFromConfig(cfg, "footer"), 60),
		SectionTitle:    truncateRunes(stringFromConfig(cfg, "section_title"), 24),
	}, opts)
	return err
}

func (a *App) sendAgentInteractiveCarousel(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	bodyText string,
	cards []map[string]any,
	opts MessageSendOptions,
) error {
	params := whatsapp.CarouselMessageParams{Cards: make([]whatsapp.CarouselCard, 0, len(cards))}
	for _, card := range cards {
		action := fieldString(card, "type")
		if action != "url" {
			action = "reply"
		}
		built := whatsapp.CarouselCard{
			MediaType: fieldString(card, "media_type"),
			MediaURL:  fieldString(card, "media_url"),
			Body:      fieldString(card, "body"),
			Action:    action,
			URL:       fieldString(card, "url"),
		}
		if title := fieldString(card, "title"); title != "" {
			built.Replies = append(built.Replies, whatsapp.CarouselQuickReply{
				ID:    fieldString(card, "id"),
				Title: title,
			})
		}
		if title := fieldString(card, "title_2"); title != "" {
			built.Replies = append(built.Replies, whatsapp.CarouselQuickReply{
				ID:    fieldString(card, "id_2"),
				Title: title,
			})
		}
		params.Cards = append(params.Cards, built)
	}
	_, err := a.SendOutgoingMessage(context.Background(), OutgoingMessageRequest{
		Account:         account,
		Contact:         contact,
		Type:            models.MessageTypeInteractive,
		InteractiveType: "carousel",
		BodyText:        bodyText,
		Carousel:        params,
	}, opts)
	return err
}

func (a *App) commerceCatalogListContext(r *fastglue.Request) (orgID, userID uuid.UUID, accountName string, page int, search string, err error) {
	orgID, userID, err = a.getOrgAndUserID(r)
	if err != nil {
		_ = r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
		return
	}
	accountName, err = a.commerceCatalogAccount(r, orgID)
	if err != nil {
		return
	}
	page = 1
	if raw := strings.TrimSpace(string(r.RequestCtx.QueryArgs().Peek("page"))); raw != "" {
		n, parseErr := strconv.Atoi(raw)
		if parseErr != nil || n < 1 {
			_ = r.SendErrorEnvelope(fasthttp.StatusBadRequest, "page must be a positive integer", nil, "")
			err = errEnvelopeSent
			return
		}
		page = n
	}
	search = strings.TrimSpace(string(r.RequestCtx.QueryArgs().Peek("search")))
	return
}

func (a *App) commerceCatalogAccount(r *fastglue.Request, orgID uuid.UUID) (string, error) {
	accountName := strings.TrimSpace(string(r.RequestCtx.QueryArgs().Peek("account")))
	if accountName == "" {
		_ = r.SendErrorEnvelope(fasthttp.StatusBadRequest, "WhatsApp account is required", nil, "")
		return "", errEnvelopeSent
	}
	if !a.whatsAppAccountInOrg(orgID, accountName) {
		_ = r.SendErrorEnvelope(fasthttp.StatusNotFound, "WhatsApp account not found", nil, "")
		return "", errEnvelopeSent
	}
	return accountName, nil
}

func (a *App) tiqrEcommerceEnabled(orgID uuid.UUID, accountName string) bool {
	var binding models.CodedFlowBinding
	err := a.DB.Where(
		"organization_id = ? AND whats_app_account = ? AND flow_key = ? AND is_enabled = ?",
		orgID, accountName, tiqrecommerce.FlowKey, true,
	).First(&binding).Error
	return err == nil
}

func (a *App) commerceCatalogStorePage(
	orgID uuid.UUID,
	accountName, operation string,
	params map[string]string,
) (map[string]any, error) {
	settings, err := a.getChatbotSettingsCached(orgID, accountName)
	if err != nil || settings == nil || !commerceRESTConfigured(settings.AI) {
		return nil, fmt.Errorf("commerce settings unavailable")
	}
	storeID := strings.TrimSpace(settings.AI.CommerceStoreID)
	client := newTiqrStoreRESTClient(settings.AI.CommerceRESTURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := invokeTiqrStoreRESTOperation(ctx, client, operation, storeID, "", params)
	if err != nil {
		return nil, err
	}
	payload := tiqrStoreResultMap(operation, raw)
	aliasBuyerListResults(payload)
	return payload, nil
}

func commerceCatalogItems(payload map[string]any) ([]any, int) {
	items, page := pageItemsAndCount(payload)
	return items, page
}

func pageItemsAndCount(payload map[string]any) ([]any, int) {
	if payload == nil {
		return nil, 0
	}
	var items []any
	for _, key := range []string{"results", "products", "categories"} {
		if list, ok := anySlice(payload[key]); ok {
			items = list
			break
		}
	}
	count := anyToInt(payload["count"])
	if count < len(items) {
		count = len(items)
	}
	return items, count
}

func commerceCatalogPageFromPayload(payload map[string]any, page int) commerceCatalogPageResponse {
	items, count := pageItemsAndCount(payload)
	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if row, ok := asStringMap(item); ok {
			results = append(results, row)
		}
	}
	return commerceCatalogPageResponse{
		Count:    count,
		Page:     page,
		PageSize: commerceCatalogPageSize,
		Results:  results,
	}
}

func commerceCatalogValidatePage(page, count, loaded int) error {
	if page < 1 {
		return fmt.Errorf("page out of range")
	}
	if count == 0 {
		if page == 1 {
			return nil
		}
		return fmt.Errorf("page out of range")
	}
	maxPage := (count + commerceCatalogPageSize - 1) / commerceCatalogPageSize
	if page > maxPage {
		return fmt.Errorf("page out of range")
	}
	if loaded == 0 && page > 1 {
		return fmt.Errorf("page out of range")
	}
	return nil
}
