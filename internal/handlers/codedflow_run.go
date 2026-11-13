package handlers

import (
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommercev2"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrleads"
	"github.com/shridarpatil/whatomate/internal/models"
	"gorm.io/gorm"
)

func configureEcommerceV2(a *App) {
	if a == nil {
		return
	}
	tiqrecommercev2.Configure(tiqrecommercev2.Hooks{
		BindingSettings: func(orgID uuid.UUID, accountName, flowKey string) models.JSONB {
			var binding models.CodedFlowBinding
			err := a.DB.Where(
				"organization_id = ? AND whats_app_account = ? AND flow_key = ?",
				orgID, accountName, flowKey,
			).First(&binding).Error
			if err != nil || binding.Settings == nil {
				return nil
			}
			return binding.Settings
		},
	})
}

func tryEcommerceTransfer(a *App, chat codedflow.Chat, message string) (bool, error) {
	configureEcommerceV2(a)
	if chat == nil || chat.Session() == nil {
		return false, nil
	}
	key := codedflow.SessionKey(chat.Session())
	if key != tiqrecommerce.FlowKey && key != tiqrecommercev2.FlowKey {
		return false, nil
	}
	conv := codedflow.NewConv(a, chat)
	err := tiqrecommerce.TransferEcommerce(conv, message)
	return true, err
}

func tryLeadsTransfer(a *App, chat codedflow.Chat, message string) (bool, string, error) {
	if chat == nil || chat.Session() == nil {
		return false, "", nil
	}
	if !tiqrleads.IsLeadsSession(chat.Session()) {
		return false, "", nil
	}
	conv := codedflow.NewConv(a, chat)
	body := tiqrleads.PrepareHandoff(conv, message)
	return true, body, nil
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
	configureEcommerceV2(a)
	var archive *codedflow.PreviewSink
	if preview == nil {
		archive = &codedflow.PreviewSink{Session: session}
	}
	inboundMedia := tiqrecommerce.TakeInboundCaptureMedia(session)
	if inboundMedia.DisplayURL == "" && strings.TrimSpace(inboundMedia.MediaURL) != "" {
		inboundMedia.DisplayURL = displayMediaURL(a.S3Client, inboundMedia.MediaURL, inboundMedia.MessageID)
	}
	if flow != nil && flow.Key == tiqrleads.FlowKey && strings.TrimSpace(session.CurrentStep) == "" {
		var prior []string
		if a.DB != nil {
			prior = tiqrleads.UnansweredInboundTexts(a.getSessionHistory(session.ID, 50))
		}
		tiqrleads.EnsureOpeningText(session, userInput, prior)
	}
	chatCtx := &chatNodeCtx{
		account:          account,
		contact:          contact,
		session:          session,
		userInput:        userInput,
		inboundMedia:     inboundMedia,
		buttonID:         buttonID,
		flowResponseData: flowResponseData,
		quotedMessage:    codedflow.TakeQuotedInbound(session),
		preview:          preview,
		archive:          archive,
	}
	chat := newCodedChat(chatCtx)
	inbound := codedflow.ArchiveInboundFromChat(chat)
	err := codedflow.RunPreview(a, session, flow, chat, preview)
	if archive != nil {
		committed := codedflow.CommitArchiveTurn(session, flow.Key, archive, inbound)
		if committed != nil && (a.Firestore == nil || !a.Firestore.Enabled()) {
			codedflow.AppendLocalArchiveTurn(session, committed.Turn)
		}
		if perr := a.persistSessionData(session); perr != nil && err == nil {
			err = perr
		}
		if committed != nil {
			if werr := a.writeSessionArchiveTurnToFirestore(committed); werr != nil {
				a.Log.Error("session archive turn not stored in Firestore",
					"error", werr, "session_id", session.ID, "turn_index", committed.Index)
			}
		}
		if codedflow.SessionEnded(session) {
			a.flushSessionArchiveToFirestore(session)
		}
	}
	return err
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

func (a *App) matchDefaultCodedFlow(orgID uuid.UUID, accountName string) *codedflow.CodedFlow {
	var binding models.CodedFlowBinding
	err := a.DB.Where(
		"organization_id = ? AND whats_app_account = ? AND is_enabled = ? AND is_default = ?",
		orgID, accountName, true, true,
	).First(&binding).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			a.Log.Error("Failed to fetch default coded flow", "error", err)
		}
		return nil
	}
	return codedflow.ByKey(binding.FlowKey)
}
