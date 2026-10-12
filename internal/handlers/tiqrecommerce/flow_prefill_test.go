package tiqrecommerce

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFallbackSupportEmail(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "support+919846435358@tiqr.events", fallbackSupportEmail("919846435358"))
	assert.Equal(t, "support+919846435358@tiqr.events", fallbackSupportEmail("+91 98464-35358"))
	assert.Equal(t, "", fallbackSupportEmail(""))
	assert.Equal(t, "", fallbackSupportEmail("abc"))
}

func TestContextEmail_FallbackWhenMissing(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "support+910000000000@tiqr.events", contextEmail(map[string]any{
		"customer_phone": "910000000000",
	}))
	assert.Equal(t, "ada@example.com", contextEmail(map[string]any{
		"customer_email": "ada@example.com",
		"customer_phone": "910000000000",
	}))
	assert.Equal(t, "buyer@example.com", contextEmail(map[string]any{
		"customer_email": "9846435358",
		"customer_phone": "9846435358",
		"email":          "buyer@example.com",
	}))
}

func TestPickupOrderParams_FallbackEmail(t *testing.T) {
	t.Parallel()
	params := PickupOrderParams(map[string]any{
		"customer_phone": "910000000000",
		"delivery_mode":  tiqrModePickup,
	})
	assert.Equal(t, "support+910000000000@tiqr.events", params["email"])
	assert.Equal(t, "910000000000", params["phone_number"])
}
