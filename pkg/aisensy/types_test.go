package aisensy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAPIError_NestedMetaDetails(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"error": {
			"message": "(#131009) Parameter value is not valid",
			"type": "OAuthException",
			"code": 131009,
			"error_data": {
				"messaging_product": "whatsapp",
				"details": "Interactive Message type, 'address_message' not supported. Supported types ['button', 'list']"
			}
		}
	}`)
	err := parseAPIError(400, body)
	require.Error(t, err)
	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, 400, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "(#131009) Parameter value is not valid")
	assert.Contains(t, apiErr.Message, "address_message' not supported")
}
