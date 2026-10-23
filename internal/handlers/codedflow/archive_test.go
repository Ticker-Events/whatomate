package codedflow

import (
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

	committed := CommitArchiveTurn(session, "tiqr_ecommerce", sink, map[string]any{
		"text": "hi",
	})
	require.NotNil(t, committed)
	assert.Equal(t, 0, committed.Index)
	turn := committed.Turn
	assert.Equal(t, "hi", turn.Inbound["text"])
	require.NotEmpty(t, turn.Messages)
	assert.Equal(t, "Hello", turn.Messages[0].Content)
	require.Len(t, turn.AICalls, 1)
	assert.Equal(t, "intent", turn.AICalls[0].Role)
	require.Len(t, turn.APICalls, 1)
	assert.Equal(t, "list_products", turn.APICalls[0].Name)
	require.NotNil(t, committed.Meta)
	assert.Equal(t, 1, committed.Meta.Version)
	assert.Equal(t, "tiqr_ecommerce", committed.Meta.FlowKey)
	assert.Equal(t, 1, committed.Meta.TurnCount)

	loaded := LoadSessionArchive(session)
	require.NotNil(t, loaded)
	assert.Equal(t, 1, loaded.TurnCount)
	assert.Empty(t, loaded.Turns)
	raw, ok := session.SessionData[ArchiveKey].(map[string]any)
	require.True(t, ok)
	_, hasTurns := raw["turns"]
	assert.False(t, hasTurns)
}

func TestCommitArchiveTurnIncrementsIndexWithoutStoringTurns(t *testing.T) {
	session := &models.ChatbotSession{
		ContactID:      uuid.New(),
		OrganizationID: uuid.New(),
		Status:         models.SessionStatusActive,
		CurrentStep:    "buy",
		StartedAt:      time.Now().UTC(),
		SessionData:    models.JSONB{"_coded_flow": "tiqr_ecommerce"},
	}
	session.ID = uuid.New()

	for i := 0; i < 3; i++ {
		sink := &PreviewSink{Session: session}
		sink.Text("buy", "msg")
		committed := CommitArchiveTurn(session, "tiqr_ecommerce", sink, map[string]any{
			"text": "hi",
		})
		require.NotNil(t, committed)
		assert.Equal(t, i, committed.Index)
		assert.Equal(t, i+1, committed.Meta.TurnCount)
	}

	loaded := LoadSessionArchive(session)
	require.NotNil(t, loaded)
	assert.Equal(t, 3, loaded.TurnCount)
	assert.Empty(t, loaded.Turns)
	assert.True(t, HasSessionArchive(loaded))
}

func TestSanitizeArchiveContextDropsPrivateKeys(t *testing.T) {
	out := SanitizeArchiveContext(map[string]any{
		"cart":              []any{},
		"agent_instruction": "secret",
		"api_token":         "x",
		ArchiveKey:          map[string]any{"version": 1},
		"commerce_handoff":  map[string]any{"summary": "ok"},
	})
	assert.Contains(t, out, "cart")
	assert.Contains(t, out, "commerce_handoff")
	assert.NotContains(t, out, "agent_instruction")
	assert.NotContains(t, out, "api_token")
	assert.NotContains(t, out, ArchiveKey)
}

func TestHasSessionArchiveFromTurnCountAndLegacyTurns(t *testing.T) {
	assert.False(t, HasSessionArchive(nil))
	assert.False(t, HasSessionArchive(&SessionArchive{Version: 1}))
	assert.True(t, HasSessionArchive(&SessionArchive{TurnCount: 2}))
	assert.True(t, HasSessionArchive(&SessionArchive{
		Turns: []SessionArchiveTurn{{At: time.Now().UTC().Format(time.RFC3339)}},
	}))
}

func TestLoadSessionArchiveLegacyTurnsSetsTurnCount(t *testing.T) {
	session := &models.ChatbotSession{
		SessionData: models.JSONB{
			ArchiveKey: map[string]any{
				"version":  1,
				"flow_key": "tiqr_ecommerce",
				"turns": []any{
					map[string]any{"at": "2020-01-01T00:00:00Z", "messages": []any{}},
					map[string]any{"at": "2020-01-01T00:01:00Z", "messages": []any{}},
				},
			},
		},
	}
	loaded := LoadSessionArchive(session)
	require.NotNil(t, loaded)
	assert.Equal(t, 2, loaded.TurnCount)
	assert.Len(t, loaded.Turns, 2)
	assert.True(t, HasSessionArchive(loaded))
}

func TestTurnsFromMapsIgnoresIndex(t *testing.T) {
	turns, err := TurnsFromMaps([]map[string]any{
		{
			"index": 0,
			"at":    "2020-01-01T00:00:00Z",
			"step":  "buy",
			"messages": []any{
				map[string]any{"type": "text", "content": "hi"},
			},
			"ai_calls": []any{
				map[string]any{"role": "intent", "route": "buy", "reasoning": "shop"},
			},
			"api_calls": []any{
				map[string]any{"name": "list", "path": "/x", "curl": "curl"},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, turns, 1)
	assert.Equal(t, "buy", turns[0].Step)
	require.Len(t, turns[0].Messages, 1)
	assert.Equal(t, "hi", turns[0].Messages[0].Content)
	require.Len(t, turns[0].AICalls, 1)
	assert.Equal(t, "shop", turns[0].AICalls[0].Reasoning)
	require.Len(t, turns[0].APICalls, 1)
	assert.Equal(t, "list", turns[0].APICalls[0].Name)
}
