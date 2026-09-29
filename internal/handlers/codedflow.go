package handlers

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
)

// codedFlowDataKey marks a session that is inside a compiled-in flow.
// CurrentFlowID stays empty so this is not treated as a saved graph.
const codedFlowDataKey = "_coded_flow"

// CodedStep is one named step shown to admins. The function that runs it
// lives on the flow and is not part of the API payload.
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
	start       string
	fns         map[string]codedStepFn
}

type codedStepFn func(c *codedCtx) (codedJump, error)

// codedJump is how a step continues. Exactly one of yield, end, or next
// should be set. outcome is the button or validation result the step
// just consumed, used by the step itself before it picks next.
type codedJump struct {
	next    string
	yield   bool
	end     bool
	outcome string
}

func stay() codedJump            { return codedJump{yield: true} }
func finishCoded() codedJump     { return codedJump{end: true} }
func goTo(step string) codedJump { return codedJump{next: step} }

var (
	codedFlowList  []*CodedFlow
	codedFlowIndex = map[string]*CodedFlow{}
)

func registerCodedFlow(flow CodedFlow) {
	if flow.Key == "" || flow.start == "" || len(flow.fns) == 0 {
		panic("coded flow is incomplete")
	}
	if _, exists := codedFlowIndex[flow.Key]; exists {
		panic("duplicate coded flow " + flow.Key)
	}
	codedFlowList = append(codedFlowList, &flow)
	codedFlowIndex[flow.Key] = &flow
}

func codedFlowByKey(key string) *CodedFlow {
	return codedFlowIndex[strings.TrimSpace(key)]
}

func codedFlows() []*CodedFlow {
	return codedFlowList
}

// resumeCodedFlow continues a session that is already inside a coded flow.
// Returns true when the inbound was handled.
func (a *App) resumeCodedFlow(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	session *models.ChatbotSession,
	userInput, buttonID string,
	flowResponseData map[string]any,
) bool {
	key := codedFlowSessionKey(session)
	if key == "" {
		return false
	}
	flow := codedFlowByKey(key)
	if flow == nil {
		a.Log.Error("Active coded flow is not registered", "session", session.ID, "flow", key)
		finishCodedSession(session)
		if err := a.persistChatSession(session); err != nil {
			a.Log.Error("Failed to close unknown coded flow session", "error", err, "session", session.ID)
		}
		return true
	}
	if err := a.runCodedFlow(account, contact, session, flow, userInput, buttonID, flowResponseData); err != nil {
		a.Log.Error("Coded flow failed", "error", err, "session", session.ID, "flow", flow.Key)
	}
	return true
}

func codedFlowSessionKey(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return ""
	}
	key, _ := session.SessionData[codedFlowDataKey].(string)
	return strings.TrimSpace(key)
}

// codedCtx is one inbound message running through a coded flow.
// chat is shared for the whole run so a step that consumes the message
// hides it from the next step, matching the graph runner.
type codedCtx struct {
	app  *App
	chat *chatNodeCtx
}

func (c *codedCtx) session() *models.ChatbotSession {
	return c.chat.session
}

func (c *codedCtx) listLen(key string) int {
	if c.session().SessionData == nil {
		return 0
	}
	items, ok := anySlice(c.session().SessionData[key])
	if !ok {
		return 0
	}
	return len(items)
}

func (c *codedCtx) tiqr(id string, cfg map[string]any) (bool, error) {
	node := &ChatNode{ID: id, Type: ChatNodeTiqrStoreAPI, Label: id, Config: cfg}
	out, err := c.app.execChatTiqrStoreAPI(node, c.chat)
	if err != nil {
		return false, err
	}
	return out.outcome == "http:2xx", nil
}

func (c *codedCtx) buttons(id string, cfg map[string]any) (codedJump, error) {
	node := &ChatNode{ID: id, Type: ChatNodeButtons, Label: id, Config: cfg}
	out, err := c.app.execChatButtons(node, c.chat)
	if err != nil {
		return codedJump{}, err
	}
	if out.yield {
		return stay(), nil
	}
	return codedJump{outcome: out.outcome}, nil
}

func (c *codedCtx) prompt(id string, cfg map[string]any) (codedJump, error) {
	node := &ChatNode{ID: id, Type: ChatNodePrompt, Label: id, Config: cfg}
	out, err := c.app.execChatPrompt(node, c.chat)
	if err != nil {
		return codedJump{}, err
	}
	if out.yield {
		return stay(), nil
	}
	return codedJump{outcome: out.outcome}, nil
}

func (c *codedCtx) whatsappFlow(id string, cfg map[string]any) (codedJump, error) {
	node := &ChatNode{ID: id, Type: ChatNodeWhatsAppFlow, Label: id, Config: cfg}
	out, err := c.app.execChatWhatsAppFlow(node, c.chat)
	if err != nil {
		return codedJump{}, err
	}
	if out.yield {
		return stay(), nil
	}
	return codedJump{outcome: out.outcome}, nil
}

func (c *codedCtx) say(id, message string) error {
	node := &ChatNode{ID: id, Type: ChatNodeMessage, Label: id, Config: map[string]any{"message": message}}
	_, err := c.app.execChatMessage(node, c.chat)
	return err
}

func (c *codedCtx) set(id string, rows []any) error {
	node := &ChatNode{ID: id, Type: ChatNodeSetVariable, Label: id, Config: map[string]any{"set": rows}}
	_, err := c.app.execChatSetVariable(node, c.chat)
	return err
}

// runCodedFlow walks named steps until one waits for the user or the
// flow finishes. An empty CurrentStep means this inbound started the flow,
// so the trigger text is not treated as an answer.
func (a *App) runCodedFlow(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	session *models.ChatbotSession,
	flow *CodedFlow,
	userInput, buttonID string,
	flowResponseData map[string]any,
) error {
	if flow == nil {
		return fmt.Errorf("coded flow is nil")
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	session.SessionData["phone_number"] = session.PhoneNumber
	if contact != nil {
		session.SessionData["contact_name"] = contact.ProfileName
	}
	session.SessionData[codedFlowDataKey] = flow.Key

	chat := &chatNodeCtx{
		account:          account,
		contact:          contact,
		session:          session,
		userInput:        userInput,
		buttonID:         buttonID,
		flowResponseData: flowResponseData,
	}
	if session.CurrentStep == "" {
		session.CurrentStep = flow.start
		chat.userInput = ""
		chat.buttonID = ""
		chat.flowResponseData = nil
	}

	ctx := &codedCtx{app: a, chat: chat}
	for range maxChatGraphIterations {
		fn := flow.fns[session.CurrentStep]
		if fn == nil {
			_ = a.persistChatSession(session)
			return fmt.Errorf("coded flow %q step %q not found", flow.Key, session.CurrentStep)
		}
		stepName := session.CurrentStep
		jump, err := fn(ctx)
		if err != nil {
			_ = a.persistChatSession(session)
			return err
		}
		appendCodedPath(session, stepName, jump)
		if jump.yield {
			return a.persistChatSession(session)
		}
		if jump.end {
			finishCodedSession(session)
			return a.persistChatSession(session)
		}
		if jump.next == "" {
			_ = a.persistChatSession(session)
			return fmt.Errorf("coded flow %q step %q has no next step", flow.Key, stepName)
		}
		session.CurrentStep = jump.next
	}
	_ = a.persistChatSession(session)
	return errChatGraphRunaway
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

func appendCodedPath(session *models.ChatbotSession, step string, jump codedJump) {
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	outcome := jump.outcome
	switch {
	case jump.yield:
		outcome = "yield"
	case jump.end:
		outcome = "end"
	case outcome == "" && jump.next != "":
		outcome = jump.next
	}
	entry := map[string]any{
		"node":    step,
		"type":    "coded",
		"outcome": outcome,
	}
	path, _ := session.SessionData["__path__"].([]any)
	session.SessionData["__path__"] = append(path, entry)
}

// matchCodedFlowTrigger returns the enabled coded flow whose keyword is
// contained in the message. Match is case-insensitive, same as visual flows.
// Empty keywords never match. A coded flow wins when a visual flow uses
// the same keyword.
func (a *App) matchCodedFlowTrigger(orgID uuid.UUID, accountName, messageText string) *CodedFlow {
	var bindings []models.CodedFlowBinding
	err := a.DB.Where(
		"organization_id = ? AND whats_app_account = ? AND is_enabled = ?",
		orgID, accountName, true,
	).Find(&bindings).Error
	if err != nil {
		a.Log.Error("Failed to fetch coded flow bindings", "error", err)
		return nil
	}

	messageLower := strings.ToLower(messageText)
	for _, binding := range bindings {
		flow := codedFlowByKey(binding.FlowKey)
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
