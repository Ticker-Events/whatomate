package codedflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDetectedLanguage_Open(t *testing.T) {
	assert.Equal(t, "en", normalizeDetectedLanguage("", nil))
	assert.Equal(t, "hi", normalizeDetectedLanguage("hi", nil))
	assert.Equal(t, "es", normalizeDetectedLanguage("es", []string{}))
	assert.Equal(t, "other", normalizeDetectedLanguage("other", nil))
}

func TestNormalizeDetectedLanguage_Restricted(t *testing.T) {
	supported := []string{"en", "hi", "manglish"}
	assert.Equal(t, "hi", normalizeDetectedLanguage("hi", supported))
	assert.Equal(t, "manglish", normalizeDetectedLanguage("Manglish", supported))
	assert.Equal(t, "en", normalizeDetectedLanguage("es", supported))
	assert.Equal(t, "en", normalizeDetectedLanguage("other", supported))
	assert.Equal(t, "en", normalizeDetectedLanguage("", supported))
	assert.Equal(t, "en", normalizeDetectedLanguage("ar", supported))
}

func TestValidateCodedIntent_RestrictsLanguage(t *testing.T) {
	ctx := IntentContext{
		SupportedLanguages: []string{"en", "hi"},
	}
	raw := IntentResult{Language: "es", Route: codedRouteUnclear}
	got, ok := ValidateCodedIntent(raw, ctx)
	require.True(t, ok)
	assert.Equal(t, "en", got.Language)

	rawOpen := IntentResult{Language: "es", Route: codedRouteUnclear}
	gotOpen, ok := ValidateCodedIntent(rawOpen, IntentContext{})
	require.True(t, ok)
	assert.Equal(t, "es", gotOpen.Language)
}

func TestJevLanguageCriteria_Restricted(t *testing.T) {
	criteria := jevLanguageCriteria([]string{"en", "ml", "manglish"})
	assert.Equal(t, "English", criteria["en"])
	assert.Equal(t, "Malayalam", criteria["ml"])
	assert.Equal(t, "Malayalam written in Latin letters, or mixed Malayalam and English", criteria["manglish"])
	assert.Equal(t, "Not one of the listed languages", criteria["other"])
	_, hasES := criteria["es"]
	assert.False(t, hasES)
	_, hasAR := criteria["ar"]
	assert.False(t, hasAR)
}

func TestJevLanguageCriteria_Open(t *testing.T) {
	criteria := jevLanguageCriteria(nil)
	assert.Equal(t, "Spanish", criteria["es"])
	assert.Equal(t, "Arabic", criteria["ar"])
	assert.Equal(t, "Any other language or mixed form", criteria["other"])
}

func TestLanguagePromptInstruction_Restricted(t *testing.T) {
	got := languagePromptInstruction([]string{"en", "hi"})
	assert.Contains(t, got, "en (English)")
	assert.Contains(t, got, "hi (Hindi)")
	assert.Contains(t, got, "set language to en")
}

func TestBuildJevIntentRequest_UsesSupportedLanguages(t *testing.T) {
	_, questions := buildJevIntentRequest("namaskaram", IntentContext{
		SupportedLanguages: []string{"en", "ml"},
	})
	lang, ok := questions["language"].(map[string]any)
	require.True(t, ok)
	criteria, ok := lang["criteria"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, criteria, "en")
	assert.Contains(t, criteria, "ml")
	assert.Contains(t, criteria, "other")
	_, hasES := criteria["es"]
	assert.False(t, hasES)
}
