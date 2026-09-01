package aisensy

import (
	"testing"

	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTemplateComponents_IncludesBodySampleText(t *testing.T) {
	t.Parallel()

	tmpl := &whatsapp.TemplateSubmission{
		Name:        "order_ready",
		Language:    "en",
		Category:    "UTILITY",
		BodyContent: "Hello {{1}}! Your order {{2}} is ready.",
		SampleValues: []any{
			map[string]any{"component": "body", "index": 1, "value": "John"},
			map[string]any{"component": "body", "index": 2, "value": "ORD-123"},
		},
	}

	components, err := buildTemplateComponents(tmpl)
	require.NoError(t, err)

	var bodyComp map[string]any
	for _, comp := range components {
		if comp["type"] == "BODY" {
			bodyComp = comp
			break
		}
	}
	require.NotNil(t, bodyComp)
	assert.Equal(t, "Hello {{1}}! Your order {{2}} is ready.", bodyComp["text"])

	example, ok := bodyComp["example"].(map[string]any)
	require.True(t, ok, "BODY must include example sample text")
	bodyText, ok := example["body_text"].([][]string)
	require.True(t, ok)
	require.Len(t, bodyText, 1)
	assert.Equal(t, []string{"John", "ORD-123"}, bodyText[0])
}

func TestBuildTemplateComponents_IncludesHeaderSampleText(t *testing.T) {
	t.Parallel()

	tmpl := &whatsapp.TemplateSubmission{
		Name:          "promo",
		Language:      "en",
		Category:      "MARKETING",
		HeaderType:    "TEXT",
		HeaderContent: "Hi {{1}}",
		BodyContent:   "Check out our deals.",
		SampleValues: []any{
			map[string]any{"component": "header", "index": 1, "value": "Alex"},
		},
	}

	components, err := buildTemplateComponents(tmpl)
	require.NoError(t, err)

	var headerComp map[string]any
	for _, comp := range components {
		if comp["type"] == "HEADER" {
			headerComp = comp
			break
		}
	}
	require.NotNil(t, headerComp)
	example, ok := headerComp["example"].(map[string]any)
	require.True(t, ok, "HEADER must include example sample text")
	assert.Equal(t, []string{"Alex"}, example["header_text"])
}

func TestBuildTemplateComponents_MissingSamplesReturnsError(t *testing.T) {
	t.Parallel()

	tmpl := &whatsapp.TemplateSubmission{
		Name:         "test",
		Language:     "en",
		Category:     "UTILITY",
		BodyContent:  "Hello {{1}}!",
		SampleValues: []any{},
	}

	_, err := buildTemplateComponents(tmpl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sample values are required")
}

func TestBuildTemplateComponents_NamedParams(t *testing.T) {
	t.Parallel()

	tmpl := &whatsapp.TemplateSubmission{
		Name:            "named_order",
		Language:        "en",
		Category:        "UTILITY",
		ParameterFormat: "named",
		BodyContent:     "Hello {{customer_name}}! Order {{order_id}} ready.",
		SampleValues: []any{
			map[string]any{"component": "body", "param_name": "customer_name", "value": "John"},
			map[string]any{"component": "body", "param_name": "order_id", "value": "ORD-1"},
		},
	}

	components, err := buildTemplateComponents(tmpl)
	require.NoError(t, err)

	var bodyComp map[string]any
	for _, comp := range components {
		if comp["type"] == "BODY" {
			bodyComp = comp
			break
		}
	}
	require.NotNil(t, bodyComp)
	example, ok := bodyComp["example"].(map[string]any)
	require.True(t, ok)
	named, ok := example["body_text_named_params"].([]map[string]string)
	require.True(t, ok)
	require.Len(t, named, 2)
	assert.Equal(t, "customer_name", named[0]["param_name"])
	assert.Equal(t, "John", named[0]["example"])
}
