package handlers

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/ticker"
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
  "product_description": "Starts at ₹12 — ceramic classic",
  "button_id": "add_to_cart_MUG1"
}
` + "```\n" + `
And another:
` + "```whatsapp_product\n" + `{
  "image_url": "https://cdn.example.com/b.jpg",
  "product_title": "Red Mug",
  "product_description": "Starts at ₹14 — bold finish",
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
  "product_title": "",
  "product_description": "Y",
  "button_id": "add_to_cart_1"
}
` + "```\n" + "Outro"

	segs := parseAIResponseSegments(raw)
	require.Len(t, segs, 2)
	assert.Equal(t, "Intro", segs[0].Text)
	assert.Equal(t, "Outro", segs[1].Text)
}

func TestParseAIResponseSegments_EmptyImageURLAllowed(t *testing.T) {
	t.Parallel()

	raw := "```whatsapp_product\n" + `{
  "image_url": "",
  "product_title": "Hat",
  "product_description": "Starts at ₹9",
  "button_id": "add_to_cart_HAT1"
}
` + "```"

	segs := parseAIResponseSegments(raw)
	require.Len(t, segs, 1)
	require.Equal(t, AISegmentProduct, segs[0].Type)
	require.NotNil(t, segs[0].Product)
	assert.Equal(t, "", segs[0].Product.ImageURL)
	assert.Equal(t, "Hat", segs[0].Product.ProductTitle)
}

func TestParseAIResponseSegments_BadButtonIDSkipped(t *testing.T) {
	t.Parallel()

	raw := "```whatsapp_product\n" + `{
  "image_url": "https://x.com/a.jpg",
  "product_title": "Hat",
  "product_description": "Starts at ₹9",
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
  "product_description": "Starts at ₹9",
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

func TestAddOptionToCart_FirstAddAndQtyIncrement(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		BaseModel:   models.BaseModel{ID: uuid.New()},
		SessionData: models.JSONB{},
	}
	meta := map[string]any{
		"product_id":   1,
		"product_name": "Runner",
		"option_id":    42,
		"option_name":  "Large",
		"price":        99.0,
	}

	added, title, qty := addOptionToCart(session, 42, meta)
	require.True(t, added)
	assert.Equal(t, "Large", title)
	assert.Equal(t, 1, qty)

	cart := normalizeCartMap(session.SessionData[cartKey])
	require.Len(t, cart, 1)
	line := cart["42"]
	require.NotNil(t, line)
	assert.Equal(t, 1, anyToInt(line["qty"]))

	added, _, qty = addOptionToCart(session, 42, meta)
	require.True(t, added)
	assert.Equal(t, 2, qty)
	cart = normalizeCartMap(session.SessionData[cartKey])
	assert.Equal(t, 2, anyToInt(cart["42"]["qty"]))
}

func TestNormalizeCartMap_DropsLegacyArray(t *testing.T) {
	t.Parallel()

	legacy := []any{
		map[string]any{"product_id": "A", "title": "Alpha", "qty": 2},
	}
	cart := normalizeCartMap(legacy)
	assert.Empty(t, cart)
}

func TestIsAddToCartButton(t *testing.T) {
	t.Parallel()

	assert.True(t, IsAddToCartButton("add_to_cart_SKU1"))
	assert.False(t, IsAddToCartButton("add_to_cart_"))
	assert.False(t, IsAddToCartButton("btn_1"))
	assert.False(t, IsAddToCartButton(""))
}

func TestIsAddOptionButton(t *testing.T) {
	t.Parallel()

	assert.True(t, IsAddOptionButton("add_option_42"))
	assert.False(t, IsAddOptionButton("add_option_"))
	assert.False(t, IsAddOptionButton("add_to_cart_1"))
	assert.Equal(t, 42, OptionIDFromButton("add_option_42"))
}

func TestIsCheckoutButton(t *testing.T) {
	t.Parallel()

	assert.True(t, IsCheckoutButton("checkout"))
	assert.True(t, IsCheckoutButton("checkout_explore"))
	assert.True(t, IsCheckoutButton("checkout_confirm"))
	assert.False(t, IsCheckoutButton("add_to_cart_1"))
}

func TestFormatCartSummary(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			cartKey: map[string]any{
				"10": map[string]any{
					"qty": 2,
					"product": map[string]any{
						"option_id":   10,
						"option_name": "Alpha",
						"price":       50.0,
					},
				},
				"20": map[string]any{
					"qty": 1,
					"product": map[string]any{
						"option_id":   20,
						"option_name": "Beta",
						"price":       25.0,
					},
				},
			},
		},
	}
	summary := formatCartSummary(session)
	assert.Contains(t, summary, "## Current Cart")
	assert.Contains(t, summary, "Alpha (product_option id: 10) x2")
	assert.Contains(t, summary, "Beta (product_option id: 20) x1")

	assert.Empty(t, formatCartSummary(&models.ChatbotSession{SessionData: models.JSONB{}}))
}

func TestAugmentAIContextWithProducts(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			cartKey: map[string]any{
				"10": map[string]any{
					"qty": 1,
					"product": map[string]any{
						"option_id":   10,
						"option_name": "Alpha",
						"price":       10.0,
					},
				},
			},
		},
	}
	out := augmentAIContextWithProducts("## Context Information\n\nfoo", session)
	assert.Contains(t, out, "## Context Information")
	assert.Contains(t, out, "WhatsApp Product Cards")
	assert.Contains(t, out, "```whatsapp_product")
	assert.Contains(t, out, "at most 5")
	assert.Contains(t, out, "## Current Cart")
	assert.Contains(t, out, "Alpha")
}

func TestLimitAIResponseProductCards(t *testing.T) {
	t.Parallel()

	mkProduct := func(id string) AIResponseSegment {
		return AIResponseSegment{
			Type: AISegmentProduct,
			Product: &WhatsAppProduct{
				ProductTitle:       id,
				ProductDescription: "Starts at ₹10",
				ButtonID:           "add_to_cart_" + id,
			},
		}
	}

	segs := []AIResponseSegment{
		{Type: AISegmentText, Text: "Here are picks:"},
		mkProduct("1"),
		mkProduct("2"),
		mkProduct("3"),
		mkProduct("4"),
		mkProduct("5"),
		mkProduct("6"),
		mkProduct("7"),
		{Type: AISegmentText, Text: "Want more?"},
	}

	limited := limitAIResponseProductCards(segs, maxProductCardsPerReply)
	require.Len(t, limited, 7)

	productIDs := make([]string, 0, 5)
	for _, seg := range limited {
		if seg.Type == AISegmentProduct {
			productIDs = append(productIDs, seg.Product.ProductID())
		}
	}
	assert.Equal(t, []string{"1", "2", "3", "4", "5"}, productIDs)
}

func TestOptionsVaryOnlyBySize(t *testing.T) {
	t.Parallel()

	sizeProduct := ticker.ProductSummary{
		Options: []ticker.ProductOption{
			{ID: 1, Name: "S"},
			{ID: 2, Name: "M"},
			{ID: 3, Name: "L"},
		},
	}
	assert.True(t, optionsVaryOnlyBySize(sizeProduct))

	colorProduct := ticker.ProductSummary{
		Options: []ticker.ProductOption{
			{ID: 1, Name: "Red"},
			{ID: 2, Name: "Blue"},
		},
	}
	assert.False(t, optionsVaryOnlyBySize(colorProduct))

	mixedProduct := ticker.ProductSummary{
		Options: []ticker.ProductOption{
			{ID: 1, Name: "Red / M"},
			{ID: 2, Name: "Blue / L"},
		},
	}
	assert.False(t, optionsVaryOnlyBySize(mixedProduct))
}

func TestEnrichProductDescription(t *testing.T) {
	t.Parallel()

	p := &WhatsAppProduct{ProductDescription: "Great mug"}
	enrichProductDescription(p, ticker.ProductSummary{MinPrice: 199})
	assert.Contains(t, p.ProductDescription, "Starts at ₹199.00")
}

func TestSetCartLineQty(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{SessionData: models.JSONB{
		cartKey: map[string]any{
			"5": map[string]any{
				"qty": 1,
				"product": map[string]any{"option_name": "Small"},
			},
		},
	}}
	require.True(t, setCartLineQty(session, "5", 3))
	assert.Equal(t, 3, anyToInt(normalizeCartMap(session.SessionData[cartKey])["5"]["qty"]))
}

func TestCartOrderItems(t *testing.T) {
	t.Parallel()

	session := &models.ChatbotSession{SessionData: models.JSONB{
		cartKey: map[string]any{
			"10": map[string]any{"qty": 2, "product": map[string]any{"option_id": 10}},
			"20": map[string]any{"qty": 1, "product": map[string]any{"option_id": 20}},
		},
	}}
	items := cartOrderItems(session)
	require.Len(t, items, 2)
}

func TestParsePositiveInt(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 5, parsePositiveInt("5"))
	assert.Equal(t, 0, parsePositiveInt("abc"))
	assert.Equal(t, 0, parsePositiveInt(""))
}

func TestFormatAddToCartAck(t *testing.T) {
	t.Parallel()

	msg := formatAddToCartAck("Varalakshmi Jhumka", 1)
	assert.Contains(t, msg, "Added 1 × Varalakshmi Jhumka to your cart.")
	assert.Contains(t, msg, "Reply with a number to change the quantity")
	assert.Contains(t, msg, "Checkout")

	msg2 := formatAddToCartAck("Large", 3)
	assert.Contains(t, msg2, "Added 3 × Large to your cart.")
}
