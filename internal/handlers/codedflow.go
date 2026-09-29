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

func registerCodedFlow(flow CodedFlow) {
	if flow.Key == "" || flow.run == nil {
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
	return strings.TrimSpace(asString(session.SessionData[codedFlowDataKey]))
}

// runCodedFlow replays the flow function from the top. Finished calls
// return their saved result. The trigger text is not treated as an answer.
func (a *App) runCodedFlow(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	session *models.ChatbotSession,
	flow *CodedFlow,
	userInput, buttonID string,
	flowResponseData map[string]any,
) error {
	return a.runCodedFlowPreview(account, contact, session, flow, userInput, buttonID, flowResponseData, nil)
}

// runCodedFlowPreview is runCodedFlow with outbound messages captured
// instead of sent. A nil sink is a normal live run.
func (a *App) runCodedFlowPreview(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	session *models.ChatbotSession,
	flow *CodedFlow,
	userInput, buttonID string,
	flowResponseData map[string]any,
	preview *codedPreviewSink,
) error {
	if flow == nil || flow.run == nil {
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
	session.CurrentFlowID = nil

	chat := &chatNodeCtx{
		account:          account,
		contact:          contact,
		session:          session,
		userInput:        userInput,
		buttonID:         buttonID,
		flowResponseData: flowResponseData,
		preview:          preview,
	}
	conv := &Conv{app: a, chat: chat}
	conv.ensureLanguage(userInput)
	if session.CurrentStep == "" {
		chat.userInput = ""
		chat.buttonID = ""
		chat.flowResponseData = nil
	}

	err := flow.run(conv)
	if err == nil {
		err = conv.err
	}
	if chat.preview == nil {
		if perr := a.persistChatSession(session); perr != nil && err == nil {
			err = perr
		}
	}
	return err
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
