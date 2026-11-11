package handlers

import (
	"testing"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
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

func TestStripConnectClosers(t *testing.T) {
	assert.Equal(t, "Placing order failed.",
		stripConnectClosers("Placing order failed.\n\n"+tiqrecommerce.HandoffConnect))
	assert.Equal(t, "I'm unable to handle voice messages right now.",
		stripConnectClosers(codedflow.VoiceUnsupportedMessage))
	assert.Equal(t, "", stripConnectClosers(codedflow.AgentHandoff))
	assert.Equal(t, "Cart details here",
		stripConnectClosers("Cart details here"))
}

func TestFormatEnquiryHandoffBody(t *testing.T) {
	inHours := formatEnquiryHandoffBody("Connecting you now.", 1042, false, "")
	assert.Equal(t, "Connecting you now.\n\nYour enquiry number is 1042.", inHours)

	outDefault := formatEnquiryHandoffBody(
		"Placing order failed.\n\n"+tiqrecommerce.HandoffConnect, 1042, true, "")
	assert.Equal(t,
		"Placing order failed.\n\nWe're currently outside business hours. An agent will help you during business hours.\n\nYour enquiry number is 1042.",
		outDefault,
	)
	assert.NotContains(t, outDefault, "connecting you with a team member")

	outStore := formatEnquiryHandoffBody(
		"Placing order failed.\n\n"+tiqrecommerce.HandoffConnect, 1042, true, "We're closed until Monday.")
	assert.Equal(t,
		"Placing order failed.\n\nWe're closed until Monday.\n\nYour enquiry number is 1042.",
		outStore,
	)

	outEmptyBody := formatEnquiryHandoffBody("", 0, true, "")
	assert.Equal(t, outsideHoursDefaultNotice, outEmptyBody)
	assert.NotContains(t, outEmptyBody, "connecting you")

	outEmptyWithNumber := formatEnquiryHandoffBody(codedflow.AgentHandoff, 99, true, "")
	assert.Equal(t,
		outsideHoursDefaultNotice+"\n\nYour enquiry number is 99.",
		outEmptyWithNumber,
	)

	outHoursNoNumber := formatEnquiryHandoffBody("Cart still here.", 0, true, "")
	assert.Equal(t,
		"Cart still here.\n\nWe're currently outside business hours. An agent will help you during business hours.",
		outHoursNoNumber,
	)

	assert.Equal(t, codedflow.AgentHandoff, formatEnquiryHandoffBody("", 0, false, ""))
}
