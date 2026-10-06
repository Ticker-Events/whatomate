package handlers

import (
	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
)

func tryEcommerceTransfer(a *App, chat codedflow.Chat, message string) (bool, error) {
	if chat == nil || chat.Session() == nil {
		return false, nil
	}
	if codedflow.SessionKey(chat.Session()) != tiqrecommerce.FlowKey {
		return false, nil
	}
	conv := codedflow.NewConv(a, chat)
	err := tiqrecommerce.TransferEcommerce(conv, message)
	return true, err
}

// resumeCodedFlow continues a session that is already inside a coded flow.
func (a *App) resumeCodedFlow(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	session *models.ChatbotSession,
	userInput, buttonID string,
	flowResponseData map[string]any,
) bool {
	key := codedflow.SessionKey(session)
	if key == "" {
		return false
	}
	flow := codedflow.ByKey(key)
	if flow == nil {
		a.Log.Error("Active coded flow is not registered", "session", session.ID, "flow", key)
		codedflow.FinishSession(session)
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

func (a *App) runCodedFlow(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	session *models.ChatbotSession,
	flow *codedflow.CodedFlow,
	userInput, buttonID string,
	flowResponseData map[string]any,
) error {
	return a.runCodedFlowPreview(account, contact, session, flow, userInput, buttonID, flowResponseData, nil)
}

func (a *App) runCodedFlowPreview(
	account *models.WhatsAppAccount,
	contact *models.Contact,
	session *models.ChatbotSession,
	flow *codedflow.CodedFlow,
	userInput, buttonID string,
	flowResponseData map[string]any,
	preview *codedflow.PreviewSink,
) error {
	chat := newCodedChat(&chatNodeCtx{
		account:          account,
		contact:          contact,
		session:          session,
		userInput:        userInput,
		inboundMedia:     tiqrecommerce.TakeInboundCaptureMedia(session),
		buttonID:         buttonID,
		flowResponseData: flowResponseData,
		preview:          preview,
	})
	return codedflow.RunPreview(a, session, flow, chat, preview)
}

func (a *App) matchCodedFlowTrigger(orgID uuid.UUID, accountName, messageText string) *codedflow.CodedFlow {
	var bindings []models.CodedFlowBinding
	err := a.DB.Where(
		"organization_id = ? AND whats_app_account = ? AND is_enabled = ?",
		orgID, accountName, true,
	).Find(&bindings).Error
	if err != nil {
		a.Log.Error("Failed to fetch coded flow bindings", "error", err)
		return nil
	}
	return codedflow.MatchTrigger(bindings, messageText)
}
