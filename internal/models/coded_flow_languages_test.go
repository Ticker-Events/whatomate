package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCodedFlowLanguages(t *testing.T) {
	got, err := NormalizeCodedFlowLanguages([]string{" en ", "HI", "en", "manglish", "ar"})
	require.NoError(t, err)
	assert.Equal(t, []string{"en", "hi", "manglish", "ar"}, got)

	empty, err := NormalizeCodedFlowLanguages(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	_, err = NormalizeCodedFlowLanguages([]string{"en", "zz"})
	require.Error(t, err)
}

func TestValidCodedFlowLanguage(t *testing.T) {
	assert.True(t, ValidCodedFlowLanguage("en"))
	assert.True(t, ValidCodedFlowLanguage("ar"))
	assert.True(t, ValidCodedFlowLanguage("manglish"))
	assert.True(t, ValidCodedFlowLanguage("sat"))
	assert.False(t, ValidCodedFlowLanguage("es"))
	assert.False(t, ValidCodedFlowLanguage("other"))
}
