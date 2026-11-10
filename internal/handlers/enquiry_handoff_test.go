package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnquiryAIJSON(t *testing.T) {
	title, summary, err := parseEnquiryAIJSON(`{"title":"Refund timing","summary":"Customer asked about refunds."}`)
	require.NoError(t, err)
	assert.Equal(t, "Refund timing", title)
	assert.Equal(t, "Customer asked about refunds.", summary)
}

func TestParseEnquiryAIJSONFenced(t *testing.T) {
	title, summary, err := parseEnquiryAIJSON("```json\n{\"title\":\"Hours\",\"summary\":\"Store hours question.\"}\n```")
	require.NoError(t, err)
	assert.Equal(t, "Hours", title)
	assert.Equal(t, "Store hours question.", summary)
}

func TestIsCodedEnquiryFlow(t *testing.T) {
	assert.True(t, isCodedEnquiryFlow("tiqr_leads"))
	assert.True(t, isCodedEnquiryFlow("tiqr_ecommerce"))
	assert.True(t, isCodedEnquiryFlow("tiqr_dining"))
	assert.False(t, isCodedEnquiryFlow("other"))
	assert.False(t, isCodedEnquiryFlow(""))
}

func TestEnquiryNumberFromPayload(t *testing.T) {
	assert.Equal(t, 42, enquiryNumberFromPayload(map[string]any{"number": float64(42)}))
	assert.Equal(t, 7, enquiryNumberFromPayload(map[string]any{"number": "7"}))
	assert.Equal(t, 0, enquiryNumberFromPayload(map[string]any{}))
}
