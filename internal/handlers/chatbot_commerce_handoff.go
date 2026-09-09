package handlers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	draftrepo "github.com/shridarpatil/whatomate/internal/commerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/tickermcp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (a *App) appendDraftAttachments(session *models.ChatbotSession, attachments []AIAttachment) {
	if len(attachments) == 0 || commerceDraftID(session) == uuid.Nil {
		return
	}
	repo := draftrepo.NewDraftRepository(a.DB)
	draft, err := repo.Get(commerceDraftID(session), session.OrganizationID)
	if err != nil {
		a.Log.Warn("load commerce draft attachments failed", "error", err)
		return
	}
	seen := make(map[string]bool, len(draft.Attachments))
	for _, raw := range draft.Attachments {
		if item, ok := raw.(map[string]any); ok {
			seen[asString(item["message_id"])] = true
		}
	}
	for _, attachment := range attachmentsJSON(attachments) {
		item, _ := attachment.(map[string]any)
		if !seen[asString(item["message_id"])] {
			draft.Attachments = append(draft.Attachments, item)
		}
	}
	if err := repo.Save(draft, draft.Version); err != nil {
		a.Log.Warn("save commerce draft attachments failed", "error", err)
	}
}

func requiredCaptureMissing(fields []tickermcp.CaptureField, captured models.JSONB) []string {
	var missing []string
	for _, field := range fields {
		if !field.Required {
			continue
		}
		value, exists := captured[field.Key]
		if !exists || captureValueEmpty(value) {
			missing = append(missing, field.Key)
		}
	}
	sort.Strings(missing)
	return missing
}

func captureValueEmpty(value any) bool {
	switch value := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(value) == ""
	case []any:
		return len(value) == 0
	case models.JSONBArray:
		return len(value) == 0
	default:
		return false
	}
}

func (a *App) selectedCommerceCategory(session *models.ChatbotSession, settings *models.ChatbotSettings) (tickermcp.Category, error) {
	categoryID := selectedCategoryID(session)
	rt := a.newCommerceRuntime(settings, session)
	if categoryID == "" || rt == nil {
		return tickermcp.Category{}, errors.New("selected commerce collection is unavailable")
	}
	defer rt.Close()
	page, err := rt.Client.ListCategoryPage(context.Background(), rt.StoreID, categoryID, 1, 0)
	if err != nil {
		return tickermcp.Category{}, err
	}
	if len(page.Results) != 1 {
		return tickermcp.Category{}, errors.New("selected commerce collection was not found")
	}
	return page.Results[0], nil
}

// completeCommerceCapture performs schema completeness validation and hands
// after-capture collections to the existing assignment/SLA machinery.
func (a *App) completeCommerceCapture(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, st *checkoutState) bool {
	category, err := a.selectedCommerceCategory(session, settings)
	if err != nil {
		a.Log.Warn("validate commerce capture failed", "error", err)
		return false
	}
	missing := requiredCaptureMissing(category.RequiredCaptureFields, jsonMapFromSession(session, "commerce_captured_fields"))
	if len(missing) > 0 {
		a.Log.Warn("commerce capture incomplete", "missing", missing)
		return false
	}
	if category.HandoffPolicy != "after_capture" {
		return false
	}
	setCheckoutState(session, st)
	a.stageCommerceHandoffSessionData(session, category)
	if err := a.persistSessionData(session); err != nil {
		a.Log.Error("persist commerce capture before handoff failed", "error", err)
		return true
	}
	transfer, created, err := a.createCommerceTransfer(account, contact, session, settings, category)
	if err != nil {
		a.Log.Error("commerce handoff failed", "error", err)
		_ = a.sendAndSaveTextMessage(account, contact, "We saved your request, but could not connect an expert just now. Please try again shortly.")
		return true
	}
	if created {
		reply := strings.TrimSpace(category.HandoffMessage)
		if reply == "" {
			reply = "Thanks — your request is saved. A specialist will review it and continue with you here."
		}
		_ = a.sendAndSaveTextMessage(account, contact, reply)
	}
	a.Log.Info("commerce handoff active", "transfer_id", transfer.ID, "draft_id", transfer.CommerceDraftID)
	return true
}

func (a *App) stageCommerceHandoffSessionData(session *models.ChatbotSession, category tickermcp.Category) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	var draft models.CommerceDraft
	if err := a.DB.Where("id = ? AND organization_id = ?", commerceDraftID(session), session.OrganizationID).First(&draft).Error; err != nil {
		return
	}
	media := make([]any, 0, len(draft.Attachments))
	for _, raw := range draft.Attachments {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		messageID := asString(item["message_id"])
		media = append(media, map[string]any{
			"message_id": messageID, "type": asString(item["mime_type"]),
			"filename": asString(item["filename"]), "capture_key": asString(item["capture_key"]),
			"url": "/api/media/" + messageID,
		})
	}
	summary := commerceHandoffSummary(&draft, category)
	session.SessionData["commerce_media_references"] = media
	session.SessionData["commerce_handoff_summary"] = summary
	session.SessionData["commerce_handoff"] = map[string]any{
		"draft_id": draft.ID.String(), "captured_fields": jsonMapFromSession(session, "commerce_captured_fields"),
		"media_references": media, "summary": summary,
	}
}

func (a *App) createCommerceTransfer(account *models.WhatsAppAccount, contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings, category tickermcp.Category) (*models.AgentTransfer, bool, error) {
	if err := a.syncReferencedCommerceDraft(session); err != nil && !errors.Is(err, draftrepo.ErrDraftConflict) {
		return nil, false, err
	}
	draftID := commerceDraftID(session)
	if draftID == uuid.Nil {
		return nil, false, errors.New("commerce draft is required for handoff")
	}

	var transfer models.AgentTransfer
	created := false
	err := a.DB.Transaction(func(tx *gorm.DB) error {
		var draft models.CommerceDraft
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND organization_id = ?", draftID, account.OrganizationID).First(&draft).Error; err != nil {
			return err
		}
		var lockedContact models.Contact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND organization_id = ?", contact.ID, account.OrganizationID).
			First(&lockedContact).Error; err != nil {
			return err
		}
		contact = &lockedContact
		if draft.TransferID != nil {
			return tx.Where("id = ? AND organization_id = ?", *draft.TransferID, account.OrganizationID).First(&transfer).Error
		}
		if err := tx.Where("organization_id = ? AND contact_id = ? AND status = ?", account.OrganizationID, contact.ID, models.TransferStatusActive).
			First(&transfer).Error; err == nil {
			draft.TransferID = &transfer.ID
			return tx.Model(&draft).Update("transfer_id", transfer.ID).Error
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		metadata := commerceHandoffMetadata(&draft, category)
		var agentID *uuid.UUID
		if settings != nil && settings.AgentAssignment.AssignToSameAgent && contact.AssignedUserID != nil {
			var agent models.User
			if err := tx.Where("id = ? AND organization_id = ? AND is_available = ?", *contact.AssignedUserID, account.OrganizationID, true).First(&agent).Error; err == nil {
				agentID = contact.AssignedUserID
			}
		}
		transfer = models.AgentTransfer{
			BaseModel:       models.BaseModel{ID: uuid.New()},
			OrganizationID:  account.OrganizationID,
			ContactID:       contact.ID,
			WhatsAppAccount: account.Name,
			PhoneNumber:     contact.PhoneNumber,
			Status:          models.TransferStatusActive,
			Source:          models.TransferSourceCommerce,
			AgentID:         agentID,
			Notes:           commerceHandoffSummary(&draft, category),
			Metadata:        metadata,
			CommerceDraftID: &draft.ID,
			TransferredAt:   time.Now(),
		}
		if settings != nil {
			a.SetSLADeadlines(&transfer, settings)
		}
		if agentID != nil {
			a.UpdateSLAOnPickup(&transfer)
		}
		if err := tx.Create(&transfer).Error; err != nil {
			return err
		}
		draft.TransferID = &transfer.ID
		draft.Status = "transferred"
		if err := tx.Model(&draft).Updates(map[string]any{
			"transfer_id": transfer.ID, "status": draft.Status, "active_owner_key": nil,
			"version": gorm.Expr("version + 1"),
		}).Error; err != nil {
			return err
		}
		if err := applyCommerceTags(tx, contact, append(category.Tags, category.VisualTags...)); err != nil {
			return err
		}
		if err := tx.Model(&models.ChatbotSession{}).
			Where("organization_id = ? AND contact_id = ? AND status = ?", account.OrganizationID, contact.ID, models.SessionStatusActive).
			Updates(map[string]any{"status": models.SessionStatusCancelled, "completed_at": time.Now()}).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	a.syncContactChatbotPausedToFirestore(account.OrganizationID, contact.ID, &transfer.ID, true)
	a.syncCommerceHandoffToFirestore(account.OrganizationID, contact.ID, &transfer, contact)
	if created {
		a.broadcastTransferCreated(&transfer, contact)
	}
	return &transfer, created, nil
}

func commerceHandoffMetadata(draft *models.CommerceDraft, category tickermcp.Category) models.JSONB {
	media := make([]any, 0, len(draft.Attachments))
	for _, raw := range draft.Attachments {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		messageID := asString(item["message_id"])
		media = append(media, map[string]any{
			"message_id": messageID, "mime_type": asString(item["mime_type"]),
			"filename": asString(item["filename"]), "capture_key": asString(item["capture_key"]),
			"url": "/api/media/" + messageID,
		})
	}
	return models.JSONB{
		"kind": "commerce_handoff", "draft_id": draft.ID.String(), "store_id": draft.StoreID,
		"collection":      map[string]any{"id": category.ID, "name": category.Name},
		"captured_fields": draft.CapturedFields, "cart": draft.Cart, "addons": draft.Addons,
		"fulfillment_mode": draft.FulfillmentMode, "requested_fulfillment_at": draft.RequestedFulfillmentAt,
		"address": draft.AddressSnapshot, "media": media,
	}
}

func commerceHandoffSummary(draft *models.CommerceDraft, category tickermcp.Category) string {
	return fmt.Sprintf("Commerce handoff · %s · draft %s · %d captured fields · %d media",
		category.Name, draft.ID, len(draft.CapturedFields), len(draft.Attachments))
}

func applyCommerceTags(tx *gorm.DB, contact *models.Contact, configured []string) error {
	tags := make(map[string]bool)
	for _, raw := range append([]string{"commerce"}, configured...) {
		tag := strings.TrimSpace(raw)
		if tag == "" || len(tag) > 50 {
			continue
		}
		tags[tag] = true
	}
	for _, raw := range contact.Tags {
		if tag, ok := raw.(string); ok {
			tags[tag] = true
		}
	}
	names := make([]string, 0, len(tags))
	for tag := range tags {
		names = append(names, tag)
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.Tag{
			OrganizationID: contact.OrganizationID, Name: tag, Color: "purple",
		}).Error; err != nil {
			return err
		}
	}
	sort.Strings(names)
	values := make(models.JSONBArray, len(names))
	for i, name := range names {
		values[i] = name
	}
	contact.Tags = values
	return tx.Model(contact).Update("tags", values).Error
}
