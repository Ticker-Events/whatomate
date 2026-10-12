package codedflow

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommitArchiveTurnCapturesMessageAIAndAPI(t *testing.T) {
	session := &models.ChatbotSession{
		ContactID:      uuid.New(),
		OrganizationID: uuid.New(),
		Status:         models.SessionStatusActive,
		CurrentStep:    "buy",
		StartedAt:      time.Now().UTC(),
		SessionData:    models.JSONB{"_coded_flow": "tiqr_ecommerce"},
	}
	session.ID = uuid.New()
	sink := &PreviewSink{Session: session}
	sink.Text("buy", "Hello")
	sink.NoteAI(CodedPreviewAICall{Role: "intent", Response: "buy"})
	sink.NoteAPI(CodedPreviewAPICall{Name: "list_products", Path: "/products", HTTPStatus: 200})

	archive := CommitArchiveTurn(session, "tiqr_ecommerce", sink, map[string]any{
		"text": "hi",
	})
	require.NotNil(t, archive)
	require.Len(t, archive.Turns, 1)
	turn := archive.Turns[0]
	assert.Equal(t, "hi", turn.Inbound["text"])
	require.NotEmpty(t, turn.Messages)
	assert.Equal(t, "Hello", turn.Messages[0].Content)
	require.Len(t, turn.AICalls, 1)
	assert.Equal(t, "intent", turn.AICalls[0].Role)
	require.Len(t, turn.APICalls, 1)
	assert.Equal(t, "list_products", turn.APICalls[0].Name)
	assert.Equal(t, 1, archive.Version)
	assert.Equal(t, "tiqr_ecommerce", archive.FlowKey)

	loaded := LoadSessionArchive(session)
	require.NotNil(t, loaded)
	require.Len(t, loaded.Turns, 1)
}

func TestSanitizeArchiveContextDropsPrivateKeys(t *testing.T) {
	out := SanitizeArchiveContext(map[string]any{
		"cart":                 []any{},
		"agent_instruction":    "secret",
		"api_token":            "x",
		ArchiveKey:             map[string]any{"version": 1},
		"commerce_handoff":     map[string]any{"summary": "ok"},
	})
	assert.Contains(t, out, "cart")
	assert.Contains(t, out, "commerce_handoff")
	assert.NotContains(t, out, "agent_instruction")
	assert.NotContains(t, out, "api_token")
	assert.NotContains(t, out, ArchiveKey)
}

func TestTrimArchiveToBudgetDropsOldestTurns(t *testing.T) {
	archive := &SessionArchive{Version: 1, Turns: nil}
	for i := 0; i < 40; i++ {
		payload := make([]byte, 8_000)
		for j := range payload {
			payload[j] = 'a'
		}
		archive.Turns = append(archive.Turns, SessionArchiveTurn{
			At: time.Now().UTC().Format(time.RFC3339),
			Messages: []CodedPreviewMessage{{
				Type:    "bot",
				Content: string(payload),
			}},
		})
	}
	trimArchiveToBudget(archive)
	b, err := json.Marshal(archive)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(b), archiveByteBudget+50_000)
	assert.GreaterOrEqual(t, len(archive.Turns), 1)
	assert.Less(t, len(archive.Turns), 40)
}
