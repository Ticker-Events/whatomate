package tiqrecommercev2

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"google.golang.org/adk/agent"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
)

const adkToolNoteMaxRunes = 2000

// noteTool records one ADK tool result on the preview/archive sink.
func (rt *Runtime) noteTool(name string, args any, res toolResult) toolResult {
	if rt == nil || rt.Conv == nil {
		return res
	}
	parsed := map[string]any{
		"tool": name,
		"ok":   res.OK,
	}
	if args != nil {
		parsed["args"] = truncateAny(args)
	}
	if res.Error != "" {
		parsed["error"] = res.Error
	}
	if res.Message != "" {
		parsed["message"] = truncateString(res.Message, 400)
	}
	if res.Data != nil {
		parsed["data"] = truncateAny(res.Data)
	}
	if step := strings.TrimSpace(rt.State.WaitingStep); step != "" {
		parsed["waiting_step"] = step
	}
	summary := name
	if res.OK {
		summary += " ok"
		if res.Message != "" {
			summary += ": " + truncateString(res.Message, 120)
		}
	} else {
		summary += " error: " + truncateString(res.Error, 160)
	}
	rt.Conv.NotePreviewAI(codedflow.CodedPreviewAICall{
		Role:     "adk_tool",
		Response: summary,
		Parsed:   parsed,
		Error:    res.Error,
	})
	return res
}

func (rt *Runtime) noteReply(step string, errMsg string) {
	if rt == nil || rt.Conv == nil {
		return
	}
	ok := errMsg == ""
	parsed := map[string]any{
		"tool": "reply",
		"ok":   ok,
		"step": step,
	}
	summary := "reply step=" + step
	if ok {
		summary += " ok"
	} else {
		parsed["error"] = errMsg
		summary += " rejected: " + truncateString(errMsg, 160)
	}
	rt.Conv.NotePreviewAI(codedflow.CodedPreviewAICall{
		Role:     "adk_tool",
		Response: summary,
		Parsed:   parsed,
		Error:    errMsg,
		Route:    step,
	})
}

func (rt *Runtime) capturing() bool {
	if rt == nil || rt.Conv == nil || rt.Conv.ChatCtx() == nil {
		return false
	}
	return rt.Conv.ChatCtx().Capturing()
}

func (rt *Runtime) markNeedsMock(operation string) {
	rt.NeedsMock = true
	rt.MockOperation = strings.TrimSpace(operation)
	if rt.MockOperation == "" {
		rt.MockOperation = "tiqr"
	}
	if rt.Conv != nil && rt.Conv.ChatCtx() != nil {
		if p := rt.Conv.ChatCtx().Preview(); p != nil && p.NeedsMock == "" {
			p.NeedsMock = rt.MockOperation
		}
	}
}

func (rt *Runtime) previewNeedsMockErr() error {
	if rt == nil || !rt.NeedsMock {
		return nil
	}
	return codedflow.ErrPreviewNeedsMock
}

// instrument wraps an ADK tool handler so every result is archived and
// further tools no-op after a preview needs_mock stop.
func instrument[T any](rt *Runtime, name string, fn func(agent.ToolContext, T) (toolResult, error)) func(agent.ToolContext, T) (toolResult, error) {
	return func(ctx agent.ToolContext, in T) (toolResult, error) {
		if rt.NeedsMock {
			return rt.noteTool(name, in, errResult("preview needs mock; turn stopped")), nil
		}
		res, err := fn(ctx, in)
		if err != nil {
			return res, err
		}
		return rt.noteTool(name, in, res), nil
	}
}

func truncateAny(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	s := string(b)
	if utf8.RuneCountInString(s) <= adkToolNoteMaxRunes {
		var out any
		if json.Unmarshal(b, &out) == nil {
			return out
		}
		return s
	}
	return truncateString(s, adkToolNoteMaxRunes) + "…"
}

func truncateString(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
