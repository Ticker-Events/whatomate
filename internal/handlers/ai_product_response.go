package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

const (
	addToCartPrefix     = "add_to_cart_"
	productOffersKey    = "product_offers"
	cartKey             = "cart"
	addToCartAckMessage = "Added to cart."
)

// productFormatInstructions is appended to the AI system prompt so models
// emit structured product cards instead of plain-text recommendations.
const productFormatInstructions = `## WhatsApp Product Cards

Whenever you need to display a specific product to the user, you MUST NOT output standard text for that product. Instead, format that specific product recommendation as a structured JSON object inside a ` + "```whatsapp_product" + ` code block.

CRITICAL RULES FOR THE JSON OBJECT:
1. "image_url": Provide a valid, placeholder, or inferred image URL for the product.
2. "product_title": Keep it short (under 20 characters).
3. "product_description": Include the price and a brief, catchy detail.
4. "button_id": This must follow the strict format: "add_to_cart_[UNIQUE_PRODUCT_ID]".

JSON STRUCTURE TO OUTPUT:
` + "```whatsapp_product" + `
{
  "image_url": "URL_HERE",
  "product_title": "PRODUCT_NAME",
  "product_description": "PRICE_AND_DETAILS",
  "button_id": "add_to_cart_ID"
}
` + "```" + `

If a user asks for multiple products, output multiple distinct ` + "```whatsapp_product" + ` blocks sequentially. Continue to use normal plain text for standard conversational greetings, cart summaries, or general questions.`

var (
	whatsappProductFenceRE = regexp.MustCompile("(?s)```whatsapp_product\\s*\\n(.*?)```")
	addToCartButtonIDRE    = regexp.MustCompile(`^add_to_cart_.+`)
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
// Invalid product blocks are skipped (caller may log); surrounding text is kept.
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
		// loc: full match start/end, then capture group start/end
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

	if p.ImageURL == "" || p.ProductTitle == "" || p.ProductDescription == "" {
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

// IsAddToCartButton reports whether a WhatsApp interactive button id is an add-to-cart action.
func IsAddToCartButton(buttonID string) bool {
	return strings.HasPrefix(buttonID, addToCartPrefix) && len(buttonID) > len(addToCartPrefix)
}

// stashProductOffer stores offer metadata so a later add_to_cart tap can rebuild the cart line.
func stashProductOffer(session *models.ChatbotSession, product *WhatsAppProduct) {
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
	offers[product.ProductID()] = map[string]any{
		"title":       product.ProductTitle,
		"description": product.ProductDescription,
		"image_url":   product.ImageURL,
	}
	session.SessionData[productOffersKey] = offers
}

// addProductToCart merges a product into SessionData["cart"], incrementing qty for duplicates.
// Returns false when the offer cannot be resolved.
func addProductToCart(session *models.ChatbotSession, buttonID string) (added bool, title string) {
	if session == nil || !IsAddToCartButton(buttonID) {
		return false, ""
	}
	productID := strings.TrimPrefix(buttonID, addToCartPrefix)
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}

	offers, _ := session.SessionData[productOffersKey].(map[string]any)
	offerRaw, ok := offers[productID]
	if !ok {
		return false, ""
	}
	offer, ok := offerRaw.(map[string]any)
	if !ok {
		return false, ""
	}

	title, _ = offer["title"].(string)
	description, _ := offer["description"].(string)
	imageURL, _ := offer["image_url"].(string)

	cart := normalizeCart(session.SessionData[cartKey])
	for i, item := range cart {
		if id, _ := item["product_id"].(string); id == productID {
			qty := anyToInt(item["qty"])
			if qty < 1 {
				qty = 1
			}
			item["qty"] = qty + 1
			cart[i] = item
			session.SessionData[cartKey] = cartToAnySlice(cart)
			return true, title
		}
	}

	cart = append(cart, map[string]any{
		"product_id":  productID,
		"title":       title,
		"description": description,
		"image_url":   imageURL,
		"qty":         1,
	})
	session.SessionData[cartKey] = cartToAnySlice(cart)
	return true, title
}

func normalizeCart(raw any) []map[string]any {
	switch v := raw.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func cartToAnySlice(cart []map[string]any) []any {
	out := make([]any, len(cart))
	for i, item := range cart {
		out[i] = item
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
	default:
		return 0
	}
}

// formatCartSummary builds a compact cart section for the AI system prompt.
func formatCartSummary(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return ""
	}
	cart := normalizeCart(session.SessionData[cartKey])
	if len(cart) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Current Cart\n")
	for _, item := range cart {
		title, _ := item["title"].(string)
		productID, _ := item["product_id"].(string)
		qty := anyToInt(item["qty"])
		if qty < 1 {
			qty = 1
		}
		if title == "" {
			title = productID
		}
		fmt.Fprintf(&b, "- %s (id: %s) x%d\n", title, productID, qty)
	}
	return strings.TrimSpace(b.String())
}

// augmentAIContextWithProducts appends product-card instructions and cart summary.
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
			stashProductOffer(session, seg.Product)
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

// sendProductCard sends one interactive product message (image header + Add to Cart).
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

// persistSessionData writes SessionData (and last activity) without changing step/status.
func (a *App) persistSessionData(session *models.ChatbotSession) error {
	session.LastActivityAt = time.Now()
	return a.DB.Model(session).Updates(map[string]any{
		"session_data":     session.SessionData,
		"last_activity_at": session.LastActivityAt,
	}).Error
}

// handleAddToCartTap updates the session cart and acknowledges the user.
// Returns true when the button was an add_to_cart action (caller should stop routing).
func (a *App) handleAddToCartTap(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, buttonID string) bool {
	if !IsAddToCartButton(buttonID) {
		return false
	}

	added, title := addProductToCart(session, buttonID)
	if added {
		if err := a.persistSessionData(session); err != nil {
			a.Log.Error("Failed to persist cart", "error", err, "session", session.ID)
		}
		ack := addToCartAckMessage
		if title != "" {
			ack = fmt.Sprintf("Added %s to cart.", title)
		}
		if err := a.sendAndSaveTextMessage(account, contact, ack); err != nil {
			a.Log.Error("Failed to send add-to-cart ack", "error", err, "contact", contact.PhoneNumber)
		}
		a.logSessionMessage(session.ID, models.DirectionOutgoing, ack, "add_to_cart")
	} else {
		a.Log.Warn("add_to_cart tap with unknown product offer", "button_id", buttonID, "session", session.ID)
		if err := a.sendAndSaveTextMessage(account, contact, "Sorry, that product is no longer available."); err != nil {
			a.Log.Error("Failed to send add-to-cart miss ack", "error", err, "contact", contact.PhoneNumber)
		}
	}
	return true
}
