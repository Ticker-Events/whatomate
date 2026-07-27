package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

const (
	addToCartPrefix         = "add_to_cart_"
	addOptionPrefix         = "add_option_"
	productOffersKey        = "product_offers"
	cartKey                 = "cart"
	maxProductCardsPerReply = 5
)

// productFormatInstructions is appended to the AI system prompt so models
// emit structured product cards instead of plain-text recommendations.
const productFormatInstructions = `## WhatsApp Product Cards

Whenever you need to display a specific product to the user, you MUST NOT output standard text for that product. Instead, format that specific product recommendation as a structured JSON object inside a ` + "```whatsapp_product" + ` code block.

CRITICAL RULES FOR THE JSON OBJECT:
1. "image_url": MUST be the exact HTTPS image_url from search_products / get_product tool results (images[].image). NEVER invent, guess, slugify, or placeholder a URL. If the tool result has no image, omit image_url or use "".
2. "product_title": Keep it short (under 20 characters). Use the real product name from tools.
3. "product_description": MUST include "Starts at ₹X.XX" using min_price from tool results, plus a brief detail. Do not list individual option prices on the product card.
4. "button_id": This must follow the strict format: "add_to_cart_[PRODUCT_ID]" using the numeric product id from tools.
5. Only emit cards for products that have at least one option in tool results.

JSON STRUCTURE TO OUTPUT:
` + "```whatsapp_product" + `
{
  "image_url": "https://...",
  "product_title": "PRODUCT_NAME",
  "product_description": "Starts at ₹X.XX — DETAILS",
  "button_id": "add_to_cart_ID"
}
` + "```" + `

If a user asks for multiple products, output at most 5 distinct ` + "```whatsapp_product" + ` blocks (the top / best matches only), sequentially. Do not list more than 5 products. Continue to use normal plain text for standard conversational greetings, cart summaries, or general questions.`

var (
	whatsappProductFenceRE = regexp.MustCompile("(?s)```whatsapp_product\\s*\\n(.*?)```")
	addToCartButtonIDRE    = regexp.MustCompile(`^add_to_cart_.+`)
	addOptionButtonIDRE    = regexp.MustCompile(`^add_option_\d+`)
	nonSizeAttributeRE     = regexp.MustCompile(`(?i)\b(color|colour|material|flavor|flavour|scent|fragrance|style|pattern|finish)\b`)
	sizeOptionNameRE       = regexp.MustCompile(`(?i)^(xx?s|xxl|xxxl|2xl|3xl|4xl|5xl|xs|s|m|l|xl|\d{1,2}(\s*/\s*\d{1,2})?|\d{1,2}\s*(cm|inch|in|")?|one\s*size|free\s*size|os|size\s*[\d\w]+)$`)
)

// AIResponseSegmentType identifies a parsed piece of an AI reply.
type AIResponseSegmentType int

const (
	AISegmentText AIResponseSegmentType = iota
	AISegmentProduct
)

// WhatsAppProduct is the JSON payload inside a whatsapp_product fence.
type WhatsAppProduct struct {
	ImageURL           string `json:"image_url"`
	ProductTitle       string `json:"product_title"`
	ProductDescription string `json:"product_description"`
	ButtonID           string `json:"button_id"`
}

// ProductID returns the unique id suffix from button_id (after add_to_cart_).
func (p WhatsAppProduct) ProductID() string {
	return strings.TrimPrefix(p.ButtonID, addToCartPrefix)
}

// AIResponseSegment is one ordered piece of a parsed AI reply.
type AIResponseSegment struct {
	Type    AIResponseSegmentType
	Text    string
	Product *WhatsAppProduct
}

// parseAIResponseSegments splits model output into plain-text and product segments.
func parseAIResponseSegments(raw string) []AIResponseSegment {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	matches := whatsappProductFenceRE.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil
		}
		return []AIResponseSegment{{Type: AISegmentText, Text: trimmed}}
	}

	var segments []AIResponseSegment
	last := 0
	for _, loc := range matches {
		fullStart, fullEnd := loc[0], loc[1]
		jsonStart, jsonEnd := loc[2], loc[3]

		if fullStart > last {
			if text := strings.TrimSpace(raw[last:fullStart]); text != "" {
				segments = append(segments, AIResponseSegment{Type: AISegmentText, Text: text})
			}
		}

		product, ok := parseWhatsAppProductJSON(raw[jsonStart:jsonEnd])
		if ok {
			segments = append(segments, AIResponseSegment{Type: AISegmentProduct, Product: product})
		}

		last = fullEnd
	}

	if last < len(raw) {
		if text := strings.TrimSpace(raw[last:]); text != "" {
			segments = append(segments, AIResponseSegment{Type: AISegmentText, Text: text})
		}
	}

	return segments
}

func parseWhatsAppProductJSON(raw string) (*WhatsAppProduct, bool) {
	var p WhatsAppProduct
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &p); err != nil {
		return nil, false
	}
	p.ImageURL = strings.TrimSpace(p.ImageURL)
	p.ProductTitle = strings.TrimSpace(p.ProductTitle)
	p.ProductDescription = strings.TrimSpace(p.ProductDescription)
	p.ButtonID = strings.TrimSpace(p.ButtonID)

	if p.ProductTitle == "" || p.ProductDescription == "" {
		return nil, false
	}
	if !addToCartButtonIDRE.MatchString(p.ButtonID) {
		return nil, false
	}
	if p.ProductID() == "" {
		return nil, false
	}

	p.ProductTitle = truncateRunes(p.ProductTitle, 20)
	return &p, true
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

func formatPriceINR(price float64) string {
	return fmt.Sprintf("₹%.2f", price)
}

// IsAddToCartButton reports whether a WhatsApp interactive button id is an add-to-cart action.
func IsAddToCartButton(buttonID string) bool {
	return strings.HasPrefix(buttonID, addToCartPrefix) && len(buttonID) > len(addToCartPrefix)
}

// IsAddOptionButton reports option-picker button ids.
func IsAddOptionButton(buttonID string) bool {
	return addOptionButtonIDRE.MatchString(buttonID)
}

// OptionIDFromButton parses add_option_{id}.
func OptionIDFromButton(buttonID string) int {
	if !IsAddOptionButton(buttonID) {
		return 0
	}
	id, _ := strconv.Atoi(strings.TrimPrefix(buttonID, addOptionPrefix))
	return id
}

func optionsVaryOnlyBySize(p ticker.ProductSummary) bool {
	if len(p.Options) < 2 {
		return false
	}
	for _, opt := range p.Options {
		if nonSizeAttributeRE.MatchString(opt.Name) {
			return false
		}
		if !isSizeOptionName(opt.Name) {
			return false
		}
	}
	return true
}

func isSizeOptionName(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return false
	}
	if sizeOptionNameRE.MatchString(n) {
		return true
	}
	return strings.Contains(strings.ToLower(n), "size")
}

func normalizeCartMap(raw any) map[string]map[string]any {
	if raw == nil {
		return map[string]map[string]any{}
	}
	if legacy, ok := raw.([]any); ok {
		// Legacy product_id array carts cannot be ordered; drop on read.
		_ = legacy
		return map[string]map[string]any{}
	}
	if legacy, ok := raw.([]map[string]any); ok {
		_ = legacy
		return map[string]map[string]any{}
	}
	src, ok := raw.(map[string]any)
	if !ok {
		return map[string]map[string]any{}
	}
	out := make(map[string]map[string]any, len(src))
	for k, v := range src {
		line, ok := v.(map[string]any)
		if !ok {
			continue
		}
		out[k] = line
	}
	return out
}

func cartMetaFromProductSummary(product ticker.ProductSummary, opt ticker.ProductOption) map[string]any {
	return map[string]any{
		"product_id":   product.ID,
		"product_name": product.Name,
		"option_id":    opt.ID,
		"option_name":  opt.Name,
		"price":        opt.Price,
		"image_url":    product.ImageURL,
	}
}

func cartLineOptionName(meta map[string]any) string {
	if meta == nil {
		return "item"
	}
	if name, _ := meta["option_name"].(string); name != "" {
		return name
	}
	if name, _ := meta["product_name"].(string); name != "" {
		return name
	}
	return "item"
}

func cartLinePrice(meta map[string]any) float64 {
	if meta == nil {
		return 0
	}
	switch p := meta["price"].(type) {
	case float64:
		return p
	case int:
		return float64(p)
	default:
		return 0
	}
}

// addOptionToCart upserts a cart line keyed by product_option_id.
// Returns the option display name and the line qty after the update.
func addOptionToCart(session *models.ChatbotSession, optionID int, meta map[string]any) (added bool, optionName string, qty int) {
	if session == nil || optionID <= 0 || meta == nil {
		return false, "", 0
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	key := strconv.Itoa(optionID)
	cart := normalizeCartMap(session.SessionData[cartKey])
	line, ok := cart[key]
	if ok {
		qty = anyToInt(line["qty"])
		if qty < 1 {
			qty = 1
		}
		qty++
		line["qty"] = qty
	} else {
		qty = 1
		line = map[string]any{
			"qty":     qty,
			"product": meta,
		}
	}
	cart[key] = line
	session.SessionData[cartKey] = cartMapToAny(cart)
	optionName = cartLineOptionName(meta)
	return true, optionName, qty
}

func setCartLineQty(session *models.ChatbotSession, optionIDKey string, qty int) bool {
	if session == nil || qty < 1 {
		return false
	}
	cart := normalizeCartMap(session.SessionData[cartKey])
	line, ok := cart[optionIDKey]
	if !ok {
		return false
	}
	line["qty"] = qty
	cart[optionIDKey] = line
	session.SessionData[cartKey] = cartMapToAny(cart)
	return true
}

func cartOptionName(session *models.ChatbotSession, optionIDKey string) string {
	cart := normalizeCartMap(session.SessionData[cartKey])
	if line, ok := cart[optionIDKey]; ok {
		if meta, ok := line["product"].(map[string]any); ok {
			return cartLineOptionName(meta)
		}
	}
	return "item"
}

func cartMapToAny(cart map[string]map[string]any) map[string]any {
	out := make(map[string]any, len(cart))
	for k, v := range cart {
		out[k] = v
	}
	return out
}

func anyToInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

// formatCartSummary builds a compact cart section for the AI system prompt.
func formatCartSummary(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return ""
	}
	cart := normalizeCartMap(session.SessionData[cartKey])
	if len(cart) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Current Cart\n")
	for optKey, line := range cart {
		meta, _ := line["product"].(map[string]any)
		name := cartLineOptionName(meta)
		optID := anyToInt(optKey)
		if optID <= 0 {
			optID = anyToInt(meta["option_id"])
		}
		qty := anyToInt(line["qty"])
		if qty < 1 {
			qty = 1
		}
		price := cartLinePrice(meta)
		fmt.Fprintf(&b, "- %s (product_option id: %d) x%d — %s each\n", name, optID, qty, formatPriceINR(price))
	}
	return strings.TrimSpace(b.String())
}

func stashProductOffer(session *models.ChatbotSession, product *WhatsAppProduct, summary *ticker.ProductSummary) {
	if session == nil || product == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	offers, _ := session.SessionData[productOffersKey].(map[string]any)
	if offers == nil {
		offers = map[string]any{}
	}
	offer := map[string]any{
		"title":       product.ProductTitle,
		"description": product.ProductDescription,
		"image_url":   product.ImageURL,
	}
	if summary != nil {
		offer["min_price"] = summary.MinPrice
		opts := make([]any, 0, len(summary.Options))
		for _, o := range summary.Options {
			opts = append(opts, map[string]any{
				"id":    o.ID,
				"name":  o.Name,
				"price": o.Price,
			})
		}
		offer["options"] = opts
	}
	offers[product.ProductID()] = offer
	session.SessionData[productOffersKey] = offers
}

func enrichProductDescription(product *WhatsAppProduct, summary ticker.ProductSummary) {
	if product == nil || summary.MinPrice <= 0 {
		return
	}
	startsAt := "Starts at " + formatPriceINR(summary.MinPrice)
	if strings.Contains(strings.ToLower(product.ProductDescription), "starts at") {
		return
	}
	product.ProductDescription = startsAt + " — " + strings.TrimSpace(product.ProductDescription)
}

func augmentAIContextWithProducts(contextData string, session *models.ChatbotSession) string {
	parts := make([]string, 0, 3)
	if contextData != "" {
		parts = append(parts, contextData)
	}
	parts = append(parts, productFormatInstructions)
	if cart := formatCartSummary(session); cart != "" {
		parts = append(parts, cart)
	}
	return strings.Join(parts, "\n\n")
}

func limitAIResponseProductCards(segments []AIResponseSegment, max int) []AIResponseSegment {
	if max < 0 {
		max = 0
	}
	out := make([]AIResponseSegment, 0, len(segments))
	products := 0
	for _, seg := range segments {
		if seg.Type == AISegmentProduct {
			if products >= max {
				continue
			}
			products++
		}
		out = append(out, seg)
	}
	return out
}

// sendAIResponse parses an AI reply and sends text / product cards in order.
func (a *App) sendAIResponse(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, raw string) error {
	fenceCount := len(whatsappProductFenceRE.FindAllString(raw, -1))
	segments := parseAIResponseSegments(raw)
	if len(segments) == 0 {
		if fenceCount > 0 {
			a.Log.Warn("All whatsapp_product blocks were invalid; nothing to send")
		}
		return nil
	}

	productCount := 0
	for _, seg := range segments {
		if seg.Type == AISegmentProduct {
			productCount++
		}
	}
	if fenceCount > productCount {
		a.Log.Warn("Skipped invalid whatsapp_product blocks", "skipped", fenceCount-productCount)
	}

	if productCount > maxProductCardsPerReply {
		a.Log.Info("Limiting product cards to top-N",
			"total", productCount,
			"limit", maxProductCardsPerReply,
		)
		segments = limitAIResponseProductCards(segments, maxProductCardsPerReply)
	}

	ctx := context.Background()
	sessionDirty := false
	for _, seg := range segments {
		switch seg.Type {
		case AISegmentText:
			if err := a.sendAndSaveTextMessage(account, contact, seg.Text); err != nil {
				return err
			}
		case AISegmentProduct:
			if seg.Product == nil {
				continue
			}
			summary, ok := a.lookupCommerceProductSummary(ctx, account, session, seg.Product.ProductID())
			if !ok || len(summary.Options) == 0 {
				a.Log.Warn("Skipping product card with zero options", "product_id", seg.Product.ProductID())
				continue
			}
			enrichProductDescription(seg.Product, summary)
			seg.Product.ImageURL = a.resolveProductCardImage(ctx, account, session, seg.Product, summary.ImageURL)
			stashProductOffer(session, seg.Product, &summary)
			sessionDirty = true
			if err := a.sendProductCard(account, contact, seg.Product); err != nil {
				a.Log.Error("Failed to send product card", "error", err, "button_id", seg.Product.ButtonID)
				return err
			}
		}
	}

	if sessionDirty && session != nil {
		if err := a.persistSessionData(session); err != nil {
			a.Log.Error("Failed to persist product offers", "error", err, "session", session.ID)
		}
	}
	return nil
}

func (a *App) sendProductCard(account *models.WhatsAppAccount, contact *models.Contact, product *WhatsAppProduct) error {
	body := product.ProductTitle + "\n" + product.ProductDescription
	ctx := context.Background()
	_, err := a.SendOutgoingMessage(ctx, OutgoingMessageRequest{
		Account:         account,
		Contact:         contact,
		Type:            models.MessageTypeInteractive,
		InteractiveType: "button",
		BodyText:        body,
		HeaderImageURL:  product.ImageURL,
		Buttons: []whatsapp.Button{
			{ID: product.ButtonID, Title: "Add to Cart"},
		},
	}, ChatbotSendOptions())
	return err
}

func (a *App) lookupCommerceProductSummary(ctx context.Context, account *models.WhatsAppAccount, session *models.ChatbotSession, productID string) (ticker.ProductSummary, bool) {
	if productID == "" || account == nil {
		return ticker.ProductSummary{}, false
	}
	var settings models.ChatbotSettings
	if err := a.DB.Where("organization_id = ?", account.OrganizationID).First(&settings).Error; err != nil {
		return ticker.ProductSummary{}, false
	}
	rt := a.newCommerceRuntime(&settings, session)
	if rt == nil || rt.Client == nil {
		return ticker.ProductSummary{}, false
	}
	raw, err := rt.Client.GetProduct(ctx, productID)
	if err != nil {
		a.Log.Debug("commerce get_product failed", "product_id", productID, "error", err)
		return ticker.ProductSummary{}, false
	}
	return ticker.CompactProduct(raw), true
}

func (a *App) resolveProductCardImage(ctx context.Context, account *models.WhatsAppAccount, session *models.ChatbotSession, product *WhatsAppProduct, commerceImage string) string {
	provided := strings.TrimSpace(product.ImageURL)
	if provided == "" {
		provided = strings.TrimSpace(commerceImage)
	}
	source := "none"
	resolved := ""

	if isReachablePublicMediaURL(ctx, provided) {
		source = "provided"
		resolved = provided
	} else if img := commerceImage; img != "" && isReachablePublicMediaURL(ctx, img) {
		source = "commerce"
		resolved = img
	} else if img := a.lookupCommerceProductImage(ctx, account, session, product.ProductID()); img != "" {
		source = "commerce"
		resolved = img
	}

	if provided != "" && source != "provided" && source != "commerce" {
		a.Log.Warn("Product card image_url not fetchable; using fallback",
			"product_id", product.ProductID(),
			"provided_host", hostOfURL(provided),
			"source", source,
		)
	}
	return resolved
}

func (a *App) lookupCommerceProductImage(ctx context.Context, account *models.WhatsAppAccount, session *models.ChatbotSession, productID string) string {
	summary, ok := a.lookupCommerceProductSummary(ctx, account, session, productID)
	if !ok {
		return ""
	}
	return summary.ImageURL
}

func isReachablePublicMediaURL(ctx context.Context, mediaURL string) bool {
	if !strings.HasPrefix(mediaURL, "https://") && !strings.HasPrefix(mediaURL, "http://") {
		return false
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, mediaURL, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return true
		}
		if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusForbidden {
			return false
		}
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err = client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func hostOfURL(raw string) string {
	if raw == "" {
		return ""
	}
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		if j := strings.IndexAny(rest, "/?"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return ""
}

func (a *App) persistSessionData(session *models.ChatbotSession) error {
	session.LastActivityAt = time.Now()
	return a.DB.Model(session).Updates(map[string]any{
		"session_data":     session.SessionData,
		"last_activity_at": session.LastActivityAt,
	}).Error
}

func (a *App) handleAddToCartProductTap(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, buttonID string) {
	productID := strings.TrimPrefix(buttonID, addToCartPrefix)
	ctx := context.Background()
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		a.Log.Warn("add_to_cart without commerce runtime", "product_id", productID)
		return
	}
	raw, err := rt.Client.GetProduct(ctx, productID)
	if err != nil {
		a.Log.Warn("add_to_cart get_product failed", "product_id", productID, "error", err)
		return
	}
	product := ticker.CompactProduct(raw)
	if len(product.Options) == 0 {
		a.Log.Warn("add_to_cart product has zero options", "product_id", productID)
		return
	}
	if len(product.Options) == 1 {
		opt := product.Options[0]
		meta := cartMetaFromProductSummary(product, opt)
		a.completeAddToCart(account, contact, session, opt.ID, meta, opt.Name)
		return
	}
	if err := a.sendOptionPicker(account, contact, session, product); err != nil {
		a.Log.Error("Failed to send option picker", "error", err, "product_id", productID)
	}
}

func (a *App) handleAddOptionTap(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, buttonID string) {
	optionID := OptionIDFromButton(buttonID)
	if optionID <= 0 {
		return
	}
	ctx := context.Background()
	rt := a.newCommerceRuntime(settings, session)
	if rt == nil {
		return
	}
	// Find product containing this option via cached offers or a broad search is expensive;
	// stash pending product id on picker — store last picker product in session.
	productID := getPendingPickerProductID(session)
	if productID == "" {
		a.Log.Warn("add_option without pending product context", "option_id", optionID)
		return
	}
	raw, err := rt.Client.GetProduct(ctx, productID)
	if err != nil {
		a.Log.Warn("add_option get_product failed", "product_id", productID, "error", err)
		return
	}
	product := ticker.CompactProduct(raw)
	for _, opt := range product.Options {
		if opt.ID == optionID {
			meta := cartMetaFromProductSummary(product, opt)
			a.completeAddToCart(account, contact, session, opt.ID, meta, opt.Name)
			clearPendingPickerProduct(session)
			return
		}
	}
	a.Log.Warn("add_option id not found on product", "option_id", optionID, "product_id", productID)
}

const pendingPickerProductKey = "pending_picker_product_id"

func setPendingPickerProduct(session *models.ChatbotSession, productID string) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	session.SessionData[pendingPickerProductKey] = productID
}

func getPendingPickerProductID(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return ""
	}
	id, _ := session.SessionData[pendingPickerProductKey].(string)
	return strings.TrimSpace(id)
}

func clearPendingPickerProduct(session *models.ChatbotSession) {
	if session != nil && session.SessionData != nil {
		delete(session.SessionData, pendingPickerProductKey)
	}
}

func (a *App) sendOptionPicker(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, product ticker.ProductSummary) error {
	setPendingPickerProduct(session, strconv.Itoa(product.ID))
	_ = a.persistSessionData(session)

	sizeOnly := optionsVaryOnlyBySize(product)
	var b strings.Builder
	fmt.Fprintf(&b, "Choose an option for %s:\n", product.Name)
	buttons := make([]map[string]any, 0, len(product.Options))
	for _, opt := range product.Options {
		fmt.Fprintf(&b, "%s — %s\n", opt.Name, formatPriceINR(opt.Price))
		buttons = append(buttons, map[string]any{
			"id":    fmt.Sprintf("%s%d", addOptionPrefix, opt.ID),
			"title": truncateRunes(opt.Name, 20),
		})
	}
	body := strings.TrimSpace(b.String())

	if sizeOnly || len(buttons) > 3 {
		return a.sendAndSaveInteractiveButtons(account, contact, body, buttons)
	}

	waButtons := make([]whatsapp.Button, 0, len(buttons))
	for _, btn := range buttons {
		waButtons = append(waButtons, whatsapp.Button{
			ID:    btn["id"].(string),
			Title: btn["title"].(string),
		})
	}
	ctx := context.Background()
	_, err := a.SendOutgoingMessage(ctx, OutgoingMessageRequest{
		Account:         account,
		Contact:         contact,
		Type:            models.MessageTypeInteractive,
		InteractiveType: "button",
		BodyText:        body,
		HeaderImageURL:  product.ImageURL,
		Buttons:         waButtons,
	}, ChatbotSendOptions())
	return err
}

func (a *App) completeAddToCart(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, optionID int, meta map[string]any, optionName string) {
	added, name, qty := addOptionToCart(session, optionID, meta)
	if !added {
		return
	}
	if optionName != "" {
		name = optionName
	}
	if err := a.persistSessionData(session); err != nil {
		a.Log.Error("Failed to persist cart", "error", err, "session", session.ID)
	}
	setCartPendingOption(session, optionID)
	_ = a.persistSessionData(session)

	ack := formatAddToCartAck(name, qty)
	if err := a.sendAndSaveTextMessage(account, contact, ack); err != nil {
		a.Log.Error("Failed to send add-to-cart ack", "error", err, "contact", contact.PhoneNumber)
	}
	a.logSessionMessage(session.ID, models.DirectionOutgoing, ack, "add_to_cart")
	a.sendCheckoutButtonPrompt(account, contact)
}

func formatAddToCartAck(optionName string, qty int) string {
	if qty < 1 {
		qty = 1
	}
	if optionName == "" {
		optionName = "item"
	}
	return fmt.Sprintf(
		"Added %d × %s to your cart.\n\nReply with a number to change the quantity, or keep browsing — tap Checkout when you're ready.",
		qty, optionName,
	)
}

func (a *App) sendCheckoutButtonPrompt(account *models.WhatsAppAccount, contact *models.Contact) {
	_ = a.sendAndSaveInteractiveButtons(account, contact, "Ready to place your order?", []map[string]any{
		{"id": checkoutButtonID, "title": "Checkout"},
	})
}
