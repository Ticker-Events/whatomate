package codedflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchLocalIntent_CollectionPhrases(t *testing.T) {
	ctx := IntentContext{
		ChoiceIDs:   map[string]string{"1118": "Ada Tacos (1) — Add to cart"},
		Collections: map[string]string{"66": "Fried", "64": "Cakes"},
	}
	for _, msg := range []string{"Fried", "buy fried", "Buy fried collection", "I want fried"} {
		got, ok := matchLocalIntent(msg, ctx)
		require.True(t, ok, msg)
		assert.Equal(t, RouteCollection, got.Route, msg)
		assert.Equal(t, "66", got.CollectionID, msg)
		assert.Equal(t, 1.0, got.Confidence, msg)
	}

	_, ok := matchLocalIntent("Kunafa", ctx)
	assert.False(t, ok)

	_, ok = matchLocalIntent("Add to cart", ctx)
	assert.False(t, ok)
	got, ok := matchLocalIntent("Ada Tacos (1) — Add to cart", ctx)
	require.True(t, ok)
	assert.Equal(t, RouteChoice, got.Route)
	assert.Equal(t, "1118", got.ChoiceID)
}

func TestBuildIntentPrompt_AlwaysListsLoadedCollections(t *testing.T) {
	prompt := BuildIntentPrompt("Buy fried", IntentContext{
		AllowCatalog: false,
		ChoiceIDs:    map[string]string{"66": "Fried — Browse", "1118": "Ada Tacos — Add to cart"},
		Collections:  map[string]string{"66": "Fried", "64": "Cakes"},
	})
	assert.Contains(t, prompt, "Fried (id 66)")
	assert.Contains(t, prompt, "Cakes (id 64)")
	assert.Contains(t, prompt, `"buy …"`)
	assert.Contains(t, prompt, "almost always naming a collection or a product")
	assert.NotContains(t, prompt, "Collections already loaded, use only these ids. If this list is empty, do not use the collection route:\n(none)")
}

func TestBuildGuidePrompt_ListsLoadedCollections(t *testing.T) {
	prompt := buildGuidePrompt("Fried", "en", IntentContext{
		Collections: map[string]string{"66": "Fried", "64": "Cakes"},
	})
	assert.Contains(t, prompt, "Fried")
	assert.Contains(t, prompt, "Cakes")
	assert.Contains(t, prompt, "treat it as that collection")
}

func TestValidateCodedIntent_CollectionWithoutAllowCatalog(t *testing.T) {
	ctx := IntentContext{Collections: map[string]string{"66": "Fried"}}
	got, ok := ValidateCodedIntent(IntentResult{Route: RouteCollection, CollectionID: "66", Confidence: 0.9}, ctx)
	require.True(t, ok)
	assert.Equal(t, "66", got.CollectionID)

	_, ok = ValidateCodedIntent(IntentResult{Route: RouteProduct, ProductQuery: "fried", Confidence: 0.9}, ctx)
	assert.False(t, ok)
}

func TestBuildQuotedReplyIntentPrompt_ListsLoadedCollections(t *testing.T) {
	prompt := BuildQuotedReplyIntentPrompt("Fried", IntentContext{
		QuotedContent: "Choose a collection",
		QuotedKind:    "list",
		QuotedChoices: map[string]string{"64": "Cakes"},
		Collections:   map[string]string{"66": "Fried", "64": "Cakes"},
	})
	assert.Contains(t, prompt, "Fried (id 66)")
	assert.Contains(t, prompt, "almost always naming a collection or a product")
}
