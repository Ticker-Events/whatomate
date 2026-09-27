package whatsapp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCarouselMessageInteractiveQuickReply(t *testing.T) {
	t.Parallel()

	cards := make([]CarouselCard, 0, 11)
	cards = append(cards, CarouselCard{MediaURL: "", Replies: []CarouselQuickReply{{ID: "skip", Title: "Skip"}}})
	for i := 0; i < 11; i++ {
		cards = append(cards, CarouselCard{
			MediaType: "image",
			MediaURL:  "https://example.com/a.jpg",
			Body:      "one\ntwo\nthree\nfour " + strings.Repeat("B", 200),
			Action:    "reply",
			Replies: []CarouselQuickReply{{
				ID:    strings.Repeat("i", 300),
				Title: strings.Repeat("T", 30),
			}},
		})
	}

	interactive, err := CarouselMessageInteractive("  "+strings.Repeat("M", 1100)+"  ", CarouselMessageParams{Cards: cards})
	require.NoError(t, err)
	assert.Equal(t, "carousel", interactive["type"])
	assert.Equal(t, strings.Repeat("M", 1024), interactive["body"].(map[string]any)["text"])

	got := interactive["action"].(map[string]any)["cards"].([]map[string]any)
	require.Len(t, got, 10)
	assert.Equal(t, 0, got[0]["card_index"])
	assert.Equal(t, "cta_url", got[0]["type"])
	header := got[0]["header"].(map[string]any)
	assert.Equal(t, "image", header["type"])
	assert.Equal(t, "https://example.com/a.jpg", header["image"].(map[string]any)["link"])
	body := got[0]["body"].(map[string]any)["text"].(string)
	assert.LessOrEqual(t, len([]rune(body)), 160)
	assert.Equal(t, 2, strings.Count(body, "\n"))
	buttons := got[0]["action"].(map[string]any)["buttons"].([]map[string]any)
	require.Len(t, buttons, 1)
	reply := buttons[0]["quick_reply"].(map[string]any)
	assert.Equal(t, strings.Repeat("T", 20), reply["title"])
	assert.Equal(t, strings.Repeat("i", 256), reply["id"])
}

func TestCarouselMessageInteractiveURL(t *testing.T) {
	t.Parallel()

	interactive, err := CarouselMessageInteractive("Arrivals", CarouselMessageParams{Cards: []CarouselCard{
		{MediaType: "video", MediaURL: "https://example.com/a.mp4", Body: "Aloe", Action: "url", URL: "https://shop.example/a", Replies: []CarouselQuickReply{{Title: "Buy now"}}},
		{MediaType: "video", MediaURL: "https://example.com/b.mp4", Body: "Fern", Action: "url", URL: "https://shop.example/b", Replies: []CarouselQuickReply{{Title: "Buy now"}}},
	}})
	require.NoError(t, err)
	got := interactive["action"].(map[string]any)["cards"].([]map[string]any)
	require.Len(t, got, 2)
	header := got[0]["header"].(map[string]any)
	assert.Equal(t, "video", header["type"])
	assert.Equal(t, "https://example.com/a.mp4", header["video"].(map[string]any)["link"])
	action := got[1]["action"].(map[string]any)
	assert.Equal(t, "cta_url", action["name"])
	params := action["parameters"].(map[string]any)
	assert.Equal(t, "Buy now", params["display_text"])
	assert.Equal(t, "https://shop.example/b", params["url"])
}

func TestCarouselMessageInteractiveRequiresTwoMatchingCards(t *testing.T) {
	t.Parallel()

	_, err := CarouselMessageInteractive("  ", CarouselMessageParams{})
	require.Error(t, err)

	_, err = CarouselMessageInteractive("Hello", CarouselMessageParams{Cards: []CarouselCard{
		{MediaURL: "https://example.com/a.jpg", Action: "reply", Replies: []CarouselQuickReply{{ID: "a", Title: "A"}}},
	}})
	require.Error(t, err)

	_, err = CarouselMessageInteractive("Hello", CarouselMessageParams{Cards: []CarouselCard{
		{MediaURL: "https://example.com/a.jpg", Action: "reply", Replies: []CarouselQuickReply{{ID: "a", Title: "A"}}},
		{MediaURL: "https://example.com/b.jpg", Action: "reply", Replies: []CarouselQuickReply{{ID: "b", Title: "B"}, {ID: "c", Title: "C"}}},
	}})
	require.Error(t, err)
}
