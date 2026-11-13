package tiqrecommercev2

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
)

type archiveChat struct {
	stubChat
	archive *codedflow.PreviewSink
	preview *codedflow.PreviewSink
}

func (c *archiveChat) Archive() *codedflow.PreviewSink { return c.archive }
func (c *archiveChat) Preview() *codedflow.PreviewSink { return c.preview }
func (c *archiveChat) Capturing() bool                 { return c.preview != nil }

func TestNoteTool_WritesArchiveAICall(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	archive := &codedflow.PreviewSink{Session: session}
	chat := &archiveChat{
		stubChat: stubChat{session: session},
		archive:  archive,
	}
	rt := &Runtime{
		Conv:  codedflow.NewConv(nil, chat),
		State: TurnState{WaitingStep: stepCollections, FixedActions: defaultFixedActions()},
	}
	res := rt.noteTool("list_collections", nil, okResult([]any{map[string]any{"id": "c1"}}, "loaded"))
	assert.True(t, res.OK)
	require.Len(t, archive.AICalls, 1)
	assert.Equal(t, "adk_tool", archive.AICalls[0].Role)
	assert.Contains(t, archive.AICalls[0].Response, "list_collections ok")
	assert.Equal(t, true, archive.AICalls[0].Parsed["ok"])
	assert.Equal(t, stepCollections, archive.AICalls[0].Parsed["waiting_step"])
}

func TestNoteTool_RecordsFailure(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	archive := &codedflow.PreviewSink{Session: session}
	chat := &archiveChat{
		stubChat: stubChat{session: session},
		archive:  archive,
	}
	rt := &Runtime{Conv: codedflow.NewConv(nil, chat), State: TurnState{}}
	_ = rt.noteTool("reply", map[string]any{"step": "cart"}, errResult("binding tap requires step details"))
	require.Len(t, archive.AICalls, 1)
	assert.False(t, archive.AICalls[0].Parsed["ok"].(bool))
	assert.Contains(t, archive.AICalls[0].Error, "binding tap")
}

func TestMarkNeedsMock_SetsPreviewFlag(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	preview := &codedflow.PreviewSink{Mock: true, Session: session}
	chat := &archiveChat{
		stubChat: stubChat{session: session},
		preview:  preview,
	}
	rt := &Runtime{
		Conv:  codedflow.NewConv(nil, chat),
		State: TurnState{},
	}
	rt.markNeedsMock("get_store")
	assert.True(t, rt.NeedsMock)
	assert.Equal(t, "get_store", rt.MockOperation)
	assert.Equal(t, "get_store", preview.NeedsMock)
	assert.True(t, errors.Is(rt.previewNeedsMockErr(), codedflow.ErrPreviewNeedsMock))
}

func TestCallTiqr_StopsWhenAlreadyNeedsMock(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	rt := &Runtime{
		Conv:      codedflow.NewConv(nil, &stubChat{session: session}),
		NeedsMock: true,
	}
	payload, ok := rt.callTiqr("store", "get_store", nil)
	assert.False(t, ok)
	assert.Nil(t, payload)
}

func TestCapturing_PreviewOrderPayload(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	preview := &codedflow.PreviewSink{Session: session}
	archive := &codedflow.PreviewSink{Session: session}
	chat := &archiveChat{
		stubChat: stubChat{session: session},
		preview:  preview,
		archive:  archive,
	}
	rt := &Runtime{
		Conv:  codedflow.NewConv(nil, chat),
		State: TurnState{FixedActions: defaultFixedActions()},
	}
	assert.True(t, rt.capturing())
	payload := map[string]any{"display_uid": "PREVIEW-ORDER", "preview": true}
	session.SessionData["order"] = payload
	noted := rt.noteTool("create_order", nil, okResult(payload, "preview-only order"))
	assert.True(t, noted.OK)
	require.Len(t, archive.AICalls, 1)
	assert.Equal(t, "adk_tool", archive.AICalls[0].Role)
}

func TestNoteTool_AfterNeedsMock(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{}}
	archive := &codedflow.PreviewSink{Session: session}
	chat := &archiveChat{stubChat: stubChat{session: session}, archive: archive}
	rt := &Runtime{Conv: codedflow.NewConv(nil, chat), NeedsMock: true, State: TurnState{}}
	res := rt.noteTool("get_store", nil, errResult("preview needs mock; turn stopped"))
	assert.False(t, res.OK)
	require.NotEmpty(t, archive.AICalls)
	assert.Contains(t, archive.AICalls[0].Response, "preview needs mock")
}

func TestWaitingStepFromSession(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{
		sessionStateKey: TurnState{WaitingStep: stepCart},
	}}
	assert.Equal(t, stepCart, WaitingStepFromSession(session))
	assert.Equal(t, "", WaitingStepFromSession(nil))
}
