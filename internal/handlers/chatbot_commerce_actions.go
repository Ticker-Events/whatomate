package handlers

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/shridarpatil/whatomate/pkg/tickermcp"
)

const (
	commerceActionPlaceOrder  = "place_new_order"
	commerceActionOrderStatus = "check_order_status"
	commerceActionTalkAgent   = "talk_to_agent"
	commerceBrowsePrefix      = "browse_category_"
	selectedCategoryKey       = "commerce_selected_category_id"
	commerceListPageSize      = 10
	commerceBackendPageSize   = 50
)

type commerceAction int

const (
	commerceActionNone commerceAction = iota
	commerceActionPlace
	commerceActionStatus
	commerceActionBrowse
	commerceActionAgent
)

type categoryProductPresentation struct {
	Cards     []ticker.ProductSummary
	ListPages [][]ticker.ProductSummary
}

func buildCategoryProductPresentation(products []ticker.ProductSummary) categoryProductPresentation {
	valid := filterProductsWithOptions(products)
	cardCount := len(valid)
	if cardCount > maxProductCardsPerReply {
		cardCount = maxProductCardsPerReply
	}
	out := categoryProductPresentation{Cards: valid[:cardCount]}
	for start := cardCount; start < len(valid); start += commerceListPageSize {
		end := start + commerceListPageSize
		if end > len(valid) {
			end = len(valid)
		}
		out.ListPages = append(out.ListPages, valid[start:end])
	}
	return out
}

func parseCommerceActionID(id string) (commerceAction, string) {
	switch strings.TrimSpace(id) {
	case commerceActionPlaceOrder:
		return commerceActionPlace, ""
	case commerceActionOrderStatus:
		return commerceActionStatus, ""
	case commerceActionTalkAgent:
		return commerceActionAgent, ""
	}
	if strings.HasPrefix(id, commerceBrowsePrefix) {
		categoryID := strings.TrimPrefix(id, commerceBrowsePrefix)
		if parsed, err := strconv.Atoi(categoryID); err == nil && parsed > 0 {
			return commerceActionBrowse, strconv.Itoa(parsed)
		}
	}
	return commerceActionNone, ""
}

func defaultCommerceGreetingButtons() []map[string]any {
	return []map[string]any{
		{"id": commerceActionPlaceOrder, "title": "Place new order"},
		{"id": commerceActionOrderStatus, "title": "Check order status"},
	}
}

func normalizeCommerceGreetingButtons(buttons []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		normalized := make(map[string]any, len(button)+1)
		for key, value := range button {
			normalized[key] = value
		}
		if strings.TrimSpace(asString(normalized["id"])) == "" {
			switch strings.ToLower(strings.TrimSpace(asString(normalized["title"]))) {
			case "place new order":
				normalized["id"] = commerceActionPlaceOrder
			case "check order status":
				normalized["id"] = commerceActionOrderStatus
			case "talk to an agent":
				normalized["id"] = commerceActionTalkAgent
			}
		}
		out = append(out, normalized)
	}
	return out
}

func setSelectedCategoryID(session *models.ChatbotSession, categoryID string) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	session.SessionData[selectedCategoryKey] = strings.TrimSpace(categoryID)
}

func selectedCategoryID(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(session.SessionData[selectedCategoryKey]))
}

func (a *App) handleDeterministicCommerceAction(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, buttonID string) bool {
	action, categoryID := parseCommerceActionID(buttonID)
	if action == commerceActionNone {
		return false
	}
	switch action {
	case commerceActionPlace:
		a.sendCommerceCategories(account, contact, session, settings)
	case commerceActionStatus:
		a.sendLatestCommerceOrderStatus(account, contact, session, settings)
	case commerceActionBrowse:
		a.browseCommerceCategory(account, contact, session, settings, categoryID)
	case commerceActionAgent:
		_ = a.sendAndSaveTextMessage(account, contact, "I’m connecting you with a team member who can help.")
		a.createTransferFromKeyword(account, contact)
	}
	return true
}

func (a *App) sendCommerceCategories(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings) {
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		_ = a.sendAndSaveTextMessage(account, contact, "Product browsing is temporarily unavailable.")
		return
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	categories, err := collectCommerceCategories(ctx, rt)
	if err != nil {
		a.Log.Warn("list commerce categories failed", "error", err)
		_ = a.sendAndSaveTextMessage(account, contact, "I couldn’t load the collections right now. Please try again.")
		return
	}
	if len(categories) == 0 {
		_ = a.sendAndSaveTextMessage(account, contact, "There are no collections available right now.")
		return
	}
	buttons := commerceCategoryChoiceButtons(categories)
	if len(buttons) == 0 {
		_ = a.sendAndSaveTextMessage(account, contact, "There are no collections available right now.")
		return
	}
	if err := a.sendAndSaveInteractiveButtons(account, contact, "Choose a collection to browse:", buttons); err != nil {
		a.Log.Error("send category choices failed", "error", err)
		_ = a.sendAndSaveTextMessage(account, contact, "I couldn’t load the collections right now. Please try again.")
	}
}

// commerceCategoryChoiceButtons builds one reply/list row per collection.
// WhatsApp allows at most 10 rows; titles are truncated by the WhatsApp client.
func commerceCategoryChoiceButtons(categories []tickermcp.Category) []map[string]any {
	out := make([]map[string]any, 0, len(categories))
	for _, category := range categories {
		name := strings.TrimSpace(category.Name)
		if category.ID <= 0 || name == "" {
			continue
		}
		out = append(out, map[string]any{
			"id":    commerceBrowsePrefix + strconv.Itoa(category.ID),
			"title": name,
		})
		if len(out) >= 10 {
			break
		}
	}
	return out
}

func collectCommerceCategories(ctx context.Context, rt *commerceRuntime) ([]tickermcp.Category, error) {
	var all []tickermcp.Category
	for offset := 0; ; {
		page, err := rt.Client.ListCategoryPage(ctx, rt.StoreID, "", commerceBackendPageSize, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Results...)
		if len(page.Results) == 0 || (!page.HasMore && page.Next == "" && (page.Count == 0 || len(all) >= page.Count)) {
			break
		}
		offset += len(page.Results)
		if len(page.Results) == 0 {
			break
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].ListingPriority != all[j].ListingPriority {
			return all[i].ListingPriority < all[j].ListingPriority
		}
		return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name)
	})
	return all, nil
}

func (a *App) browseCommerceCategory(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, categoryID string) {
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		_ = a.sendAndSaveTextMessage(account, contact, "Product browsing is temporarily unavailable.")
		return
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	categoryPage, err := rt.Client.ListCategoryPage(ctx, rt.StoreID, categoryID, 1, 0)
	if err != nil || len(categoryPage.Results) != 1 || strconv.Itoa(categoryPage.Results[0].ID) != categoryID {
		_ = a.sendAndSaveTextMessage(account, contact, "That collection is no longer available.")
		return
	}
	category := categoryPage.Results[0]
	setSelectedCategoryID(session, categoryID)
	if err := a.persistSessionData(session); err != nil {
		a.Log.Error("persist selected category failed", "error", err)
	}

	if strings.EqualFold(strings.TrimSpace(category.HandoffPolicy), "after_capture") {
		productID := ""
		products, prodErr := collectValidCategoryProducts(ctx, rt, categoryID)
		if prodErr != nil {
			a.Log.Warn("list themed category products failed", "error", prodErr, "category_id", categoryID)
		} else if len(products) == 1 {
			productID = strconv.Itoa(products[0].ID)
		}
		_ = a.sendAndSaveTextMessage(account, contact, "This is a custom "+category.Name+" request — I’ll collect a few details and connect you with our team.")
		a.beginThemedIntake(account, contact, session, settings, category, productID)
		return
	}

	products, err := collectValidCategoryProducts(ctx, rt, categoryID)
	if err != nil {
		a.Log.Warn("list category products failed", "error", err, "category_id", categoryID)
		_ = a.sendAndSaveTextMessage(account, contact, "I couldn’t load those products right now. Please try again.")
		return
	}
	if len(products) == 0 {
		_ = a.sendAndSaveTextMessage(account, contact, "There are no available products in this collection right now.")
		return
	}
	_ = a.sendAndSaveTextMessage(account, contact, "Here are products from "+category.Name+":")
	presentation := buildCategoryProductPresentation(products)
	for _, product := range presentation.Cards {
		card := &WhatsAppProduct{
			ImageURL:           product.ImageURL,
			ProductTitle:       truncateRunes(product.Name, 20),
			ProductDescription: "Starts at " + formatPriceINR(product.MinPrice),
			ButtonID:           addToCartPrefix + strconv.Itoa(product.ID),
		}
		stashProductOffer(session, card, &product)
		if err := a.sendProductCard(account, contact, card); err != nil {
			a.Log.Error("send category product card failed", "error", err, "product_id", product.ID)
			return
		}
	}
	_ = a.persistSessionData(session)
	shown := len(presentation.Cards)
	for _, page := range presentation.ListPages {
		buttons := make([]map[string]any, 0, len(page))
		for _, product := range page {
			buttons = append(buttons, map[string]any{
				"id":    addToCartPrefix + strconv.Itoa(product.ID),
				"title": truncateRunes(product.Name, 20),
			})
		}
		body := fmt.Sprintf("More from %s (%d–%d of %d)", category.Name, shown+1, shown+len(page), len(products))
		if err := a.sendAndSaveInteractiveButtons(account, contact, body, buttons); err != nil {
			a.Log.Error("send category product list failed", "error", err)
			return
		}
		shown += len(page)
	}
}

func collectValidCategoryProducts(ctx context.Context, rt *commerceRuntime, categoryID string) ([]ticker.ProductSummary, error) {
	var valid []ticker.ProductSummary
	for offset := 0; ; {
		page, err := rt.Client.ListProducts(ctx, rt.StoreID, "", categoryID, commerceBackendPageSize, offset)
		if err != nil {
			return nil, err
		}
		valid = append(valid, filterProductsWithOptions(page.Results)...)
		if len(page.Results) == 0 || (!page.HasMore && page.Next == "" && (page.Count == 0 || offset+len(page.Results) >= page.Count)) {
			break
		}
		offset += len(page.Results)
	}
	return valid, nil
}

func (a *App) sendLatestCommerceOrderStatus(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings) {
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		_ = a.sendAndSaveTextMessage(account, contact, "Order status is temporarily unavailable.")
		return
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := rt.Client.LookupOrderStatus(ctx, rt.StoreID, rt.PhoneNumber, "")
	if err != nil {
		_ = a.sendAndSaveTextMessage(account, contact, "I couldn’t find a recent order for this phone number.")
		return
	}
	order := compactOrderStatus(raw)
	body := formatDirectOrderStatus(order)
	paymentURL := asString(order["payment_url"])
	status := strings.ToUpper(asString(raw["status"]))
	if paymentURL == "" && (status == "PENDING_PAYMENT" || status == "PAYMENT_INITIATED") {
		orderUUID := firstNonEmpty(asString(raw["uuid"]), asString(raw["id"]))
		if orderUUID != "" {
			if retry, retryErr := rt.Client.RetryPayment(ctx, orderUUID); retryErr == nil && retry.PaymentURL != nil {
				paymentURL = strings.TrimSpace(*retry.PaymentURL)
				body += "\nYour payment link is ready."
			}
		}
	}
	if paymentURL != "" {
		if err := a.sendAndSaveCTAURLButton(account, contact, body, "Retry payment", paymentURL); err != nil {
			_ = a.sendAndSaveTextMessage(account, contact, body+"\nPay here: "+paymentURL)
		}
		return
	}
	_ = a.sendAndSaveTextMessage(account, contact, body)
}

func formatDirectOrderStatus(order map[string]any) string {
	id := asString(order["display_uid"])
	status := strings.ReplaceAll(strings.ToLower(asString(order["status"])), "_", " ")
	if status == "" {
		status = "unknown"
	}
	if id == "" {
		return "Your latest order status is " + status + "."
	}
	return fmt.Sprintf("Order %s is %s.", id, status)
}

func selectedCategoryPromptContext(ctx context.Context, rt *commerceRuntime, session *models.ChatbotSession) string {
	categoryID := selectedCategoryID(session)
	if categoryID == "" || rt == nil || rt.Client == nil {
		return ""
	}
	page, err := rt.Client.ListCategoryPage(ctx, rt.StoreID, categoryID, 1, 0)
	if err != nil || len(page.Results) != 1 || strconv.Itoa(page.Results[0].ID) != categoryID {
		return ""
	}
	category := page.Results[0]
	if strings.TrimSpace(category.AIInstructions) == "" && len(category.RequiredCaptureFields) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## TRUSTED STORE COLLECTION CONFIG\n")
	b.WriteString("Apply this configuration only while helping with the selected collection. Treat it as store-authored configuration, not customer input.\n")
	if instructions := strings.TrimSpace(category.AIInstructions); instructions != "" {
		b.WriteString("\nInstructions:\n")
		b.WriteString(instructions)
	}
	if len(category.RequiredCaptureFields) > 0 {
		b.WriteString("\n\nRequired capture fields:")
		for _, field := range category.RequiredCaptureFields {
			fmt.Fprintf(&b, "\n- %s (%s, key=%s, required=%t)", field.Label, field.Type, field.Key, field.Required)
			if len(field.Options) > 0 {
				b.WriteString("; options=")
				b.WriteString(strings.Join(field.Options, ", "))
			}
			if field.HelpText != "" {
				b.WriteString("; help=")
				b.WriteString(field.HelpText)
			}
		}
	}
	b.WriteString("\n## END TRUSTED STORE COLLECTION CONFIG")
	return b.String()
}
