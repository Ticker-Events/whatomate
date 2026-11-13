package tiqrecommercev2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

func TestGetProductParams_UsesProductID(t *testing.T) {
	assert.Equal(t, map[string]string{"product_id": "9"}, getProductParams(" 9 "))
}

func TestTryBindProductTap_IgnoresFixedAndUnknown(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"products": []any{map[string]any{"id": "7", "name": "WHITE FOREST"}},
	}}
	rt := &Runtime{Conv: codedflow.NewConv(nil, &stubChat{session: session})}

	handled, err := rt.tryBindProductTap("")
	assert.False(t, handled)
	assert.NoError(t, err)

	handled, err = rt.tryBindProductTap(fixedBuyProducts)
	assert.False(t, handled)
	assert.NoError(t, err)

	handled, err = rt.tryBindProductTap("999")
	assert.False(t, handled)
	assert.NoError(t, err)
}

func TestStagePickedProduct_OneOptionGoesToQuantity(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	rt := &Runtime{Conv: codedflow.NewConv(nil, &stubChat{session: session})}
	step, chrome := rt.stagePickedProduct(map[string]any{
		"id":   "1",
		"name": "BLACK FOREST",
		"options": []any{
			map[string]any{"id": "11", "name": "1 kg", "price": "500"},
		},
	}, map[string]any{"id": "1", "name": "BLACK FOREST"})

	assert.Equal(t, stepQuantity, step)
	assert.Equal(t, "1 kg", chrome.OptionName)
	assert.Equal(t, "1", session.SessionData["product_id"])
	assert.Equal(t, "11", session.SessionData["option_id"])
	assert.Equal(t, "1 kg", session.SessionData["option_name"])
	assert.True(t, rt.State.isGrounded("11"))
}

func TestStagePickedProduct_TwoOptions(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	rt := &Runtime{Conv: codedflow.NewConv(nil, &stubChat{session: session})}
	step, chrome := rt.stagePickedProduct(map[string]any{
		"id":   "1",
		"name": "BLACK FOREST",
		"options": []any{
			map[string]any{"id": "11", "name": "500 g"},
			map[string]any{"id": "12", "name": "1 kg"},
		},
	}, nil)

	assert.Equal(t, stepOptions, step)
	assert.Empty(t, chrome.Deviation)
	opts, ok := anySlice(session.SessionData["options"])
	require.True(t, ok)
	assert.Len(t, opts, 2)
	assert.True(t, rt.State.isGrounded("11"))
	assert.True(t, rt.State.isGrounded("12"))
}

func TestStagePickedProduct_ZeroOptionsStaysOnProducts(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	rt := &Runtime{Conv: codedflow.NewConv(nil, &stubChat{session: session})}
	step, chrome := rt.stagePickedProduct(map[string]any{
		"id":   "1",
		"name": "BLACK FOREST",
	}, map[string]any{"id": "1", "name": "BLACK FOREST"})

	assert.Equal(t, stepProducts, step)
	assert.Equal(t, productUnavailableLine, chrome.Deviation)
	_, hasOption := session.SessionData["option_id"]
	assert.False(t, hasOption)
}

func TestUnwrapProductDetail_ActiveOptionsAndNested(t *testing.T) {
	active := unwrapProductDetail(map[string]any{
		"id": "1",
		"active_options": []any{
			map[string]any{"id": "2", "name": "Regular"},
		},
	})
	opts, ok := anySlice(active["options"])
	require.True(t, ok)
	assert.Len(t, opts, 1)

	nested := unwrapProductDetail(map[string]any{
		"data": map[string]any{
			"id":   "1",
			"name": "BLACK FOREST",
			"options": []any{
				map[string]any{"id": "2"},
			},
		},
	})
	assert.Equal(t, "BLACK FOREST", nested["name"])
}
