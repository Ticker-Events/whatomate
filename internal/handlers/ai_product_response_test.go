package handlers

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAIResponseSegments_PlainTextOnly(t *testing.T) {
	t.Parallel()

	segs := parseAIResponseSegments("  Hello there!  ")
	require.Len(t, segs, 1)
	assert.Equal(t, AISegmentText, segs[0].Type)
	assert.Equal(t, "Hello there!", segs[0].Text)
}

func TestParseAIResponseSegments_MixedTextAndProducts(t *testing.T) {
	t.Parallel()

	raw := `Here are some picks:
` + "```whatsapp_product\n" + `{
  "image_url": "https://cdn.example.com/a.jpg",
  "product_title": "Blue Mug",
  "product_description": "$12 — ceramic classic",
  "button_id": "add_to_cart_MUG1"
}
` + "```\n" + `
And another:
` + "```whatsapp_product\n" + `{
  "image_url": "https://cdn.example.com/b.jpg",
  "product_title": "Red Mug",
  "product_description": "$14 — bold finish",
  "button_id": "add_to_cart_MUG2"
}
` + "```\n" + `
Enjoy!`

	segs := parseAIResponseSegments(raw)
	require.Len(t, segs, 5)

	assert.Equal(t, AISegmentText, segs[0].Type)
	assert.Contains(t, segs[0].Text, "Here are some picks")

	require.Equal(t, AISegmentProduct, segs[1].Type)
	require.NotNil(t, segs[1].Product)
	assert.Equal(t, "Blue Mug", segs[1].Product.ProductTitle)
	assert.Equal(t, "MUG1", segs[1].Product.ProductID())

	assert.Equal(t, AISegmentText, segs[2].Type)
	assert.Contains(t, segs[2].Text, "And another")

	require.Equal(t, AISegmentProduct, segs[3].Type)
	assert.Equal(t, "add_to_cart_MUG2", segs[3].Product.ButtonID)

	assert.Equal(t, AISegmentText, segs[4].Type)
	assert.Equal(t, "Enjoy!", segs[4].Text)
}

func TestParseAIResponseSegments_InvalidBlocksSkipped(t *testing.T) {
	t.Parallel()

	raw := "Intro\n" + "```whatsapp_product\n" + `{
  "image_url": "",
  "product_title": "X",
  "product_description": "Y",
  "button_id": "add_to_cart_1"
}
` + "```\n" + "Outro"

	segs := parseAIResponseSegments(raw)
	require.Len(t, segs, 2)
	assert.Equal(t, "Intro", segs[0].Text)
	assert.Equal(t, "Outro", segs[1].Text)
}

func TestParseAIResponseSegments_BadButtonIDSkipped(t *testing.T) {
	t.Parallel()

	raw := "```whatsapp_product\n" + `{
  "image_url": "https://x.com/a.jpg",
  "product_title": "Hat",
  "product_description": "$9",
  "button_id": "buy_HAT1"
}
` + "```"

	segs := parseAIResponseSegments(raw)
	assert.Empty(t, segs)
}

func TestParseAIResponseSegments_TitleTruncated(t *testing.T) {
	t.Parallel()

	longTitle := "This Title Is Way Too Long For WhatsApp"
	raw := "```whatsapp_product\n" + `{
  "image_url": "https://x.com/a.jpg",
  "product_title": "` + longTitle + `",
  "product_description": "$9",
  "button_id": "add_to_cart_HAT1"
}
` + "```"

	segs := parseAIResponseSegments(raw)
	require.Len(t, segs, 1)
	require.NotNil(t, segs[0].Product)
	assert.Equal(t, 20, len([]rune(segs[0].Product.ProductTitle)))
	assert.True(t, strings.HasPrefix(longTitle, segs[0].Product.ProductTitle))
}

func TestParseAIResponseSegments_InvalidJSONSkipped(t *testing.T) {
	t.Parallel()

	raw := "Before\n" + "```whatsapp_product\n" + `{not-json` + "\n```\n" + "After"
	segs := parseAIResponseSegments(raw)
	require.Len(t, segs, 2)
	assert.Equal(t, "Before", segs[0].Text)
	assert.Equal(t, "After", segs[1].Text)
}

func TestAddProductToCart_FirstAddAndQtyIncrement(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		BaseModel:   models.BaseModel{ID: uuid.New()},
		SessionData: models.JSONB{},
	}
	product := &WhatsAppProduct{
		ImageURL:           "https://cdn.example.com/shoe.jpg",
		ProductTitle:       "Runner",
		ProductDescription: "$99 — light",
		ButtonID:           "add_to_cart_SHOE1",
	}
	stashProductOffer(session, product)

	added, title := addProductToCart(session, "add_to_cart_SHOE1")
	require.True(t, added)
	assert.Equal(t, "Runner", title)

	cart := normalizeCart(session.SessionData[cartKey])
	require.Len(t, cart, 1)
	assert.Equal(t, "SHOE1", cart[0]["product_id"])
	assert.Equal(t, 1, anyToInt(cart[0]["qty"]))

	added, _ = addProductToCart(session, "add_to_cart_SHOE1")
	require.True(t, added)
	cart = normalizeCart(session.SessionData[cartKey])
	require.Len(t, cart, 1)
	assert.Equal(t, 2, anyToInt(cart[0]["qty"]))
}

func TestAddProductToCart_MissingOffer(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	added, title := addProductToCart(session, "add_to_cart_UNKNOWN")
	assert.False(t, added)
	assert.Empty(t, title)
}

func TestIsAddToCartButton(t *testing.T) {
	t.Parallel()

	assert.True(t, IsAddToCartButton("add_to_cart_SKU1"))
	assert.False(t, IsAddToCartButton("add_to_cart_"))
	assert.False(t, IsAddToCartButton("btn_1"))
	assert.False(t, IsAddToCartButton(""))
}

func TestFormatCartSummary(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			cartKey: []any{
				map[string]any{"product_id": "A", "title": "Alpha", "qty": 2},
				map[string]any{"product_id": "B", "title": "Beta", "qty": 1},
			},
		},
	}
	summary := formatCartSummary(session)
	assert.Contains(t, summary, "## Current Cart")
	assert.Contains(t, summary, "Alpha (id: A) x2")
	assert.Contains(t, summary, "Beta (id: B) x1")

	assert.Empty(t, formatCartSummary(&models.ChatbotSession{SessionData: models.JSONB{}}))
}

func TestAugmentAIContextWithProducts(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			cartKey: []any{
				map[string]any{"product_id": "A", "title": "Alpha", "qty": 1},
			},
		},
	}
	out := augmentAIContextWithProducts("## Context Information\n\nfoo", session)
	assert.Contains(t, out, "## Context Information")
	assert.Contains(t, out, "WhatsApp Product Cards")
	assert.Contains(t, out, "```whatsapp_product")
	assert.Contains(t, out, "## Current Cart")
	assert.Contains(t, out, "Alpha")
}
