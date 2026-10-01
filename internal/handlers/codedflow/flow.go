package codedflow

import (
	"fmt"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

// DataKey marks a session that is inside a compiled-in flow.
// CurrentFlowID stays empty so this is not treated as a saved graph.
const DataKey = "_coded_flow"

// codedFlowDataKey is the historic unexported alias.
const codedFlowDataKey = DataKey

// CodedStep is one line of the outline shown to admins. It is not executed.
type CodedStep struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// CodedFlow is a conversation defined in Go. Admins assign keywords; they
// do not edit the steps.
type CodedFlow struct {
	Key         string
	Name        string
	Description string
	Steps       []CodedStep
	run         func(*Conv) error
}

var (
	codedFlowList  []*CodedFlow
	codedFlowIndex = map[string]*CodedFlow{}
)

// Register adds a compiled-in coded flow. Panics on incomplete or duplicate keys.
func Register(flow CodedFlow) {
	if flow.Key == "" || flow.run == nil {
		panic("coded flow is incomplete")
	}
	if _, exists := codedFlowIndex[flow.Key]; exists {
		panic("duplicate coded flow " + flow.Key)
	}
	codedFlowList = append(codedFlowList, &flow)
	codedFlowIndex[flow.Key] = &flow
}

// ByKey returns the registered flow for key, or nil.
func ByKey(key string) *CodedFlow {
	return codedFlowIndex[strings.TrimSpace(key)]
}

// Flows returns every registered coded flow.
func Flows() []*CodedFlow {
	return codedFlowList
}

// SessionKey returns the active coded-flow key on the session, or empty.
func SessionKey(session *models.ChatbotSession) string {
	return codedFlowSessionKey(session)
}

func codedFlowSessionKey(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return ""
	}
	return strings.TrimSpace(asString(session.SessionData[codedFlowDataKey]))
}

// Run replays the flow function from the top. Finished calls return their saved result.
func Run(
	h Host,
	session *models.ChatbotSession,
	flow *CodedFlow,
	chat Chat,
) error {
	return RunPreview(h, session, flow, chat, nil)
}

// RunPreview is Run with outbound messages captured instead of sent. A nil sink is a normal live run.
func RunPreview(
	h Host,
	session *models.ChatbotSession,
	flow *CodedFlow,
	chat Chat,
	preview *PreviewSink,
) error {
	if flow == nil || flow.run == nil {
		return fmt.Errorf("coded flow is nil")
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	session.SessionData["phone_number"] = session.PhoneNumber
	if contact := chat.Contact(); contact != nil {
		session.SessionData["contact_name"] = contact.ProfileName
	}
	session.SessionData[codedFlowDataKey] = flow.Key
	session.CurrentFlowID = nil

	chat.SetPreview(preview)

	conv := &Conv{app: h, chat: chat}
	if session.CurrentStep == "" {
		chat.SetUserInput("")
		chat.SetButtonID("")
		chat.SetFlowResponseData(nil)
	} else if preview == nil {
		h.LogCodedFlowInbound(chat)
	}

	if preview == nil {
		h.LogCodedFlowContext(session, "turn_start")
	}

	err := flow.run(conv)
	if err == nil {
		err = conv.err
	}
	if preview == nil {
		h.LogCodedFlowContext(session, "turn_end")
	}
	if chat.Preview() == nil {
		if perr := h.PersistChatSession(session); perr != nil && err == nil {
			err = perr
		}
	}
	return err
}

// FinishSession clears coded-flow markers and marks the session completed.
func FinishSession(session *models.ChatbotSession) {
	finishCodedSession(session)
}

func finishCodedSession(session *models.ChatbotSession) {
	if session.SessionData != nil {
		delete(session.SessionData, codedFlowDataKey)
	}
	session.CurrentStep = ""
	session.StepRetries = 0
	session.CurrentFlowID = nil
	session.Status = models.SessionStatusCompleted
}

// MatchTrigger finds an enabled coded-flow binding whose keyword is contained
// in messageText. bindings must already be loaded by the caller (handlers).
func MatchTrigger(bindings []models.CodedFlowBinding, messageText string) *CodedFlow {
	messageLower := strings.ToLower(messageText)
	for _, binding := range bindings {
		flow := ByKey(binding.FlowKey)
		if flow == nil {
			continue
		}
		for _, keyword := range binding.Keywords {
			keyword = strings.TrimSpace(keyword)
			if keyword == "" {
				continue
			}
			if strings.Contains(messageLower, strings.ToLower(keyword)) {
				return flow
			}
		}
	}
	return nil
}

// SetRun sets the unexported run function (used by flow constructors in sibling packages).
func (f *CodedFlow) SetRun(fn func(*Conv) error) {
	f.run = fn
}

// NewFlow builds a CodedFlow with an unexported run function.
func NewFlow(key, name, description string, steps []CodedStep, run func(*Conv) error) CodedFlow {
	return CodedFlow{Key: key, Name: name, Description: description, Steps: steps, run: run}
}

// NewConv builds a Conv for tests that supply Host and Chat directly.
func NewConv(h Host, chat Chat) *Conv {
	return &Conv{app: h, chat: chat}
}
