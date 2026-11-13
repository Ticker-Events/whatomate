package firestore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionArchiveTurnDoc_IncludesIndex(t *testing.T) {
	docID, payload, err := sessionArchiveTurnDoc(3, map[string]any{
		"step": "quantity",
		"messages": []any{
			map[string]any{"type": "text", "content": "How many?"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "0003", docID)
	assert.Equal(t, 3, payload["index"])
	assert.Equal(t, "quantity", payload["step"])
	require.Len(t, payload["messages"], 1)
}

func TestSessionArchiveTurnDoc_RejectsNegativeIndex(t *testing.T) {
	_, _, err := sessionArchiveTurnDoc(-1, map[string]any{"step": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid index")
}

func TestSessionArchiveMetaData_IncludesOrgAndTurnCount(t *testing.T) {
	data := sessionArchiveMetaData(SessionArchiveDoc{
		Version:        1,
		FlowKey:        "tiqr_ecommerce_v2",
		SessionID:      "sess-1",
		ContactID:      "contact-1",
		OrganizationID: "org-1",
		StartedAt:      "2026-01-01T00:00:00Z",
		Status:         "active",
		TurnCount:      4,
	})
	assert.Equal(t, 4, data["turn_count"])
	assert.Equal(t, "org-1", data["organizationId"])
	assert.Equal(t, "org-1", data["organization_id"])
	assert.Equal(t, "tiqr_ecommerce_v2", data["flow_key"])
}
