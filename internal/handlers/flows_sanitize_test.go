package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeActionPayload_ExistingWins(t *testing.T) {
	t.Parallel()
	auto := map[string]any{
		"customer_phone": "${form.customer_phone}",
		"pincode":        "${data.pincode}",
	}
	existing := map[string]any{
		"phone_number":   "${form.customer_phone}",
		"customer_phone": "${form.customer_phone}",
	}
	got := mergeActionPayload(auto, existing)
	assert.Equal(t, "${form.customer_phone}", got["phone_number"])
	assert.Equal(t, "${form.customer_phone}", got["customer_phone"])
	assert.Equal(t, "${data.pincode}", got["pincode"])
}

func TestSanitizeComponentsWithPayload_NavigateMergesDataKeysAndExisting(t *testing.T) {
	t.Parallel()
	children := []any{
		map[string]any{
			"type":  "TextInput",
			"name":  "customer_phone",
			"label": "Phone",
		},
		map[string]any{
			"type":  "Footer",
			"label": "Continue",
			"on-click-action": map[string]any{
				"name": "navigate",
				"next": map[string]any{"type": "screen", "name": "SCREEN_A"},
				"payload": map[string]any{
					"phone_number": "${form.customer_phone}",
				},
			},
		},
	}
	out := sanitizeComponentsWithPayload(
		children,
		[]string{"customer_phone", "pincode"},
		nil,
		[]string{"customer_phone", "pincode", "city"},
	)
	require.Len(t, out, 2)
	footer, ok := out[1].(map[string]any)
	require.True(t, ok)
	action, ok := footer["on-click-action"].(map[string]any)
	require.True(t, ok)
	payload, ok := action["payload"].(map[string]any)
	require.True(t, ok)

	assert.Equal(t, "${form.customer_phone}", payload["customer_phone"])
	assert.Equal(t, "${data.pincode}", payload["pincode"])
	assert.Equal(t, "${data.city}", payload["city"])
	assert.Equal(t, "${form.customer_phone}", payload["phone_number"], "existing custom binding must win")
}

func TestSanitizeScreensForMeta_PreservesDataExampleAndNavigatePrefill(t *testing.T) {
	t.Parallel()
	screens := []any{
		map[string]any{
			"id":    "SCREEN_B",
			"title": "Customer Details",
			"data": map[string]any{
				"customer_phone": map[string]any{"type": "string", "__example__": "9198"},
				"pincode":        map[string]any{"type": "string", "__example__": "560001"},
			},
			"layout": map[string]any{
				"type": "SingleColumnLayout",
				"children": []any{
					map[string]any{
						"type":        "TextInput",
						"name":        "customer_phone",
						"label":       "Phone",
						"init-value":  "${data.customer_phone}",
						"input-type":  "text",
						"required":    true,
					},
					map[string]any{
						"type":  "Footer",
						"label": "Continue",
						"on-click-action": map[string]any{
							"name": "navigate",
							"next": map[string]any{"type": "screen", "name": "SCREEN_A"},
							"payload": map[string]any{
								"phone_number": "${form.customer_phone}",
							},
						},
					},
				},
			},
		},
		map[string]any{
			"id":    "SCREEN_A",
			"title": "Address",
			"data": map[string]any{
				"pincode": map[string]any{"type": "string", "__example__": "keep-me"},
			},
			"layout": map[string]any{
				"type": "SingleColumnLayout",
				"children": []any{
					map[string]any{
						"type":  "TextInput",
						"name":  "pincode",
						"label": "Pin",
					},
					map[string]any{
						"type":  "Footer",
						"label": "Done",
						"on-click-action": map[string]any{
							"name":    "complete",
							"payload": map[string]any{},
						},
					},
				},
			},
		},
	}

	out := sanitizeScreensForMeta(screens)
	require.Len(t, out, 2)

	screenB, ok := out[0].(map[string]any)
	require.True(t, ok)
	dataB, ok := screenB["data"].(map[string]any)
	require.True(t, ok)
	phoneSchema, ok := dataB["customer_phone"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "9198", phoneSchema["__example__"])

	layoutB, ok := screenB["layout"].(map[string]any)
	require.True(t, ok)
	childrenB, ok := layoutB["children"].([]any)
	require.True(t, ok)
	footerB, ok := childrenB[1].(map[string]any)
	require.True(t, ok)
	actionB, ok := footerB["on-click-action"].(map[string]any)
	require.True(t, ok)
	payloadB, ok := actionB["payload"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "${data.pincode}", payloadB["pincode"])
	assert.Equal(t, "${form.customer_phone}", payloadB["phone_number"])

	screenA, ok := out[1].(map[string]any)
	require.True(t, ok)
	dataA, ok := screenA["data"].(map[string]any)
	require.True(t, ok)
	pinSchema, ok := dataA["pincode"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "keep-me", pinSchema["__example__"], "existing __example__ must not be wiped")
	_, hasPhone := dataA["customer_phone"]
	assert.True(t, hasPhone, "previous screen form fields are added to later screen data")
}
