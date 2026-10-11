package codedflow

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeShownProducts_KeepsHistoryByID(t *testing.T) {
	data := models.JSONB{}
	MergeShownProducts(data, []any{
		map[string]any{"id": "101", "name": "Kunafa"},
		map[string]any{"id": "102", "name": "Canape"},
	})
	MergeShownProducts(data, []any{
		map[string]any{"id": "201", "name": "Other"},
		map[string]any{"id": "101", "name": "Kunafa Updated"},
	})
	items, ok := anySlice(data[shownProductsKey])
	require.True(t, ok)
	require.Len(t, items, 3)
	assert.Equal(t, "Kunafa Updated", fieldString(asStringMapMust(items[0]), "name"))
	assert.Equal(t, "102", fieldString(asStringMapMust(items[1]), "id"))
	assert.Equal(t, "201", fieldString(asStringMapMust(items[2]), "id"))
}

func TestQuotedChoices_CarouselAndList(t *testing.T) {
	carousel := &QuotedMessage{InteractiveData: map[string]any{
		"type": "carousel",
		"cards": []any{
			map[string]any{
				"body": "Kunafa (₹120)",
				"buttons": []any{
					map[string]any{"id": "101", "title": "Add to cart"},
				},
			},
		},
	}}
	got := QuotedChoices(carousel)
	require.Len(t, got, 1)
	assert.Equal(t, "101", got[0].ID)
	assert.Equal(t, "Add to cart", got[0].Title)
	assert.Equal(t, "Kunafa (₹120)", got[0].Body)

	list := &QuotedMessage{InteractiveData: map[string]any{
		"type": "list",
		"rows": []any{
			map[string]any{"id": "57", "title": "Sweets", "description": "Desserts"},
		},
	}}
	got = QuotedChoices(list)
	require.Len(t, got, 1)
	assert.Equal(t, "57", got[0].ID)
	assert.Equal(t, "Sweets", got[0].Title)
}

func TestValidateQuotedReplyIntent_SelectProductAndCurrent(t *testing.T) {
	ctx := IntentContext{
		QuotedChoices: map[string]string{"101": "Kunafa — Add to cart"},
		Collections:   map[string]string{"57": "Sweets"},
	}
	got, ok := ValidateQuotedReplyIntent(IntentResult{
		Route: RouteSelectProduct, ChoiceID: "101", Confidence: 0.9,
	}, ctx)
	assert.True(t, ok)
	assert.Equal(t, RouteSelectProduct, got.Route)

	_, ok = ValidateQuotedReplyIntent(IntentResult{
		Route: RouteSelectProduct, ChoiceID: "999", Confidence: 0.9,
	}, ctx)
	assert.False(t, ok)

	got, ok = ValidateQuotedReplyIntent(IntentResult{
		Route: RouteCurrent, Confidence: 0.9,
	}, ctx)
	assert.True(t, ok)
	assert.Equal(t, RouteCurrent, got.Route)
}

func TestStashAndTakeQuotedInbound(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	StashQuotedInbound(session, &QuotedMessage{
		ID:               "msg-1",
		Content:          "Pick one",
		IsLatestOutgoing: false,
		InteractiveData:  map[string]any{"type": "button"},
	})
	got := TakeQuotedInbound(session)
	require.NotNil(t, got)
	assert.Equal(t, "msg-1", got.ID)
	assert.Equal(t, "Pick one", got.Content)
	assert.Nil(t, TakeQuotedInbound(session))
}

func TestShownProductByID(t *testing.T) {
	data := models.JSONB{
		shownProductsKey: []any{
			map[string]any{"id": "101", "name": "Kunafa"},
		},
		"products": []any{
			map[string]any{"id": "201", "name": "Other"},
		},
	}
	assert.Equal(t, "Kunafa", fieldString(ShownProductByID(data, "101"), "name"))
	assert.Equal(t, "Other", fieldString(ShownProductByID(data, "201"), "name"))
	assert.Nil(t, ShownProductByID(data, "999"))
}
