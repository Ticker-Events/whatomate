package firestore

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCredentialsJSON_FixesEscapedPEMNewlines(t *testing.T) {
	raw := `{"type":"service_account","private_key":"-----BEGIN PRIVATE KEY-----\\nLINE1\\nLINE2\\n-----END PRIVATE KEY-----\\n"}`

	out, err := normalizeCredentialsJSON(raw)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(out, &parsed))

	pk, ok := parsed["private_key"].(string)
	require.True(t, ok)
	assert.Contains(t, pk, "\n")
	assert.NotContains(t, pk, `\n`)
}

func TestNormalizeCredentialsJSON_EmptyReturnsNil(t *testing.T) {
	out, err := normalizeCredentialsJSON("  ")
	require.NoError(t, err)
	assert.Nil(t, out)
}
