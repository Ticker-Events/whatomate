package firestore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/internal/utils"
)

const (
	collectionMessages        = "messages"
	collectionContacts        = "contacts"
	collectionSessionArchives = "sessionArchives"
	collectionArchiveTurns    = "turns"
)

// SyncMessage writes or replaces a message document and updates the contact summary.
func (c *Client) SyncMessage(ctx context.Context, orgID uuid.UUID, msg *models.Message, contact *models.Contact, maskPhone bool) error {
	if !c.Enabled() || msg == nil || contact == nil {
		return nil
	}

	msgData := buildMessageData(orgID, msg, contact, maskPhone)
	_, err := c.firestore.Collection(collectionMessages).Doc(msg.ID.String()).Set(ctx, msgData)
	if err != nil {
		return fmt.Errorf("firestore sync message %s: %w", msg.ID, err)
	}

	return c.syncContactFromMessage(ctx, orgID, msg, contact, msgData, maskPhone)
}

// UpdateMessageStatus upserts delivery status on a message document.
func (c *Client) UpdateMessageStatus(ctx context.Context, messageID uuid.UUID, status models.MessageStatus, errorMessage string) error {
	if !c.Enabled() {
		return nil
	}

	data := map[string]any{
		"status":    string(status),
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
	}
	if errorMessage != "" {
		data["errorMessage"] = errorMessage
	}

	_, err := c.firestore.Collection(collectionMessages).Doc(messageID.String()).Set(ctx, data, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("firestore update message status %s: %w", messageID, err)
	}
	return nil
}

// ResetContactUnread sets unreadCount to zero on a contact document.
func (c *Client) ResetContactUnread(ctx context.Context, orgID, contactID uuid.UUID) error {
	if !c.Enabled() {
		return nil
	}

	_, err := c.firestore.Collection(collectionContacts).Doc(contactID.String()).Set(ctx, map[string]any{
		"organizationId": orgID.String(),
		"unreadCount":    0,
	}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("firestore reset contact unread %s: %w", contactID, err)
	}
	return nil
}

// SyncContactChatbotPaused merges chatbot pause state onto a contact document.
// When paused, activeTransferId is the agent transfer that is holding the bot.
// When unpaused, activeTransferId is deleted so the inbox does not keep a stale id.
func (c *Client) SyncContactChatbotPaused(ctx context.Context, orgID, contactID uuid.UUID, transferID *uuid.UUID, paused bool) error {
	if !c.Enabled() {
		return nil
	}

	data := map[string]any{
		"organizationId": orgID.String(),
	}
	applyChatbotPausedFields(data, paused, transferID)

	_, err := c.firestore.Collection(collectionContacts).Doc(contactID.String()).Set(ctx, data, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("firestore sync contact chatbot paused %s: %w", contactID, err)
	}
	return nil
}

// SyncCommerceHandoff exposes only display-safe handoff fields. Agent-transfer
// notes remain private in Postgres and are deliberately not mirrored.
func (c *Client) SyncCommerceHandoff(ctx context.Context, orgID, contactID, transferID uuid.UUID, summary string, tags []string) error {
	if !c.Enabled() {
		return nil
	}
	data := map[string]any{
		"organizationId":         orgID.String(),
		"chatbotPaused":          true,
		"activeTransferId":       transferID.String(),
		"commerceHandoffSummary": summary,
		"tags":                   tags,
	}
	_, err := c.firestore.Collection(collectionContacts).Doc(contactID.String()).Set(ctx, data, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("firestore sync commerce handoff %s: %w", contactID, err)
	}
	return nil
}

// SessionArchiveDoc is the parent metadata for a coded-flow session archive.
type SessionArchiveDoc struct {
	Version        int    `json:"version"`
	FlowKey        string `json:"flow_key"`
	SessionID      string `json:"session_id"`
	ContactID      string `json:"contact_id"`
	OrganizationID string `json:"organization_id"`
	StartedAt      string `json:"started_at"`
	EndedAt        string `json:"ended_at,omitempty"`
	Status         string `json:"status"`
	TurnCount      int    `json:"turn_count"`
}

// SyncSessionArchive writes archive metadata plus one document per turn under
// contacts/{contactId}/sessionArchives/{sessionId}/turns/{index}.
func (c *Client) SyncSessionArchive(ctx context.Context, meta SessionArchiveDoc, turns []map[string]any) error {
	if !c.Enabled() {
		return nil
	}
	contactID := strings.TrimSpace(meta.ContactID)
	sessionID := strings.TrimSpace(meta.SessionID)
	if contactID == "" || sessionID == "" {
		return fmt.Errorf("firestore sync session archive: missing contact or session id")
	}
	parent := c.firestore.Collection(collectionContacts).Doc(contactID).
		Collection(collectionSessionArchives).Doc(sessionID)

	data := map[string]any{
		"version":         meta.Version,
		"flow_key":        meta.FlowKey,
		"session_id":      meta.SessionID,
		"contact_id":      meta.ContactID,
		"organization_id": meta.OrganizationID,
		"started_at":      meta.StartedAt,
		"status":          meta.Status,
		"turn_count":      meta.TurnCount,
		"updated_at":      time.Now().UTC().Format(time.RFC3339),
	}
	if meta.EndedAt != "" {
		data["ended_at"] = meta.EndedAt
	}
	if _, err := parent.Set(ctx, data); err != nil {
		return fmt.Errorf("firestore sync session archive %s: %w", sessionID, err)
	}
	for i, turn := range turns {
		docID := fmt.Sprintf("%04d", i)
		payload := map[string]any{}
		for key, value := range turn {
			payload[key] = value
		}
		payload["index"] = i
		if _, err := parent.Collection(collectionArchiveTurns).Doc(docID).Set(ctx, payload); err != nil {
			return fmt.Errorf("firestore sync session archive turn %s/%s: %w", sessionID, docID, err)
		}
	}
	return nil
}

func (c *Client) syncContactFromMessage(ctx context.Context, orgID uuid.UUID, msg *models.Message, contact *models.Contact, msgData map[string]any, maskPhone bool) error {
	profileName := contact.ProfileName
	phoneNumber := contact.PhoneNumber
	if maskPhone {
		profileName = utils.MaskIfPhoneNumber(profileName)
		phoneNumber = utils.MaskPhoneNumber(phoneNumber)
	}

	contactData := map[string]any{
		"organizationId":  orgID.String(),
		"phoneNumber":     phoneNumber,
		"profileName":     profileName,
		"whatsappAccount": contact.WhatsAppAccount,
		"lastMessageInfo": msgData,
		"tags":            stringTags(contact.Tags),
	}

	applyAssignedUserField(contactData, contact)
	if contact.LastMessageAt != nil {
		contactData["lastMessageAt"] = contact.LastMessageAt.UTC().Format(time.RFC3339)
	} else {
		contactData["lastMessageAt"] = msg.CreatedAt.UTC().Format(time.RFC3339)
	}
	if contact.LastInboundAt != nil {
		contactData["lastInboundAt"] = contact.LastInboundAt.UTC().Format(time.RFC3339)
	} else if msg.Direction == models.DirectionIncoming {
		contactData["lastInboundAt"] = msg.CreatedAt.UTC().Format(time.RFC3339)
	}

	applyConversationWaitFields(contactData, contact)

	// Increment unread only for incoming messages that are not already read.
	if msg.Direction == models.DirectionIncoming && msg.Status != models.MessageStatusRead {
		contactData["unreadCount"] = firestore.Increment(1)
	}

	_, err := c.firestore.Collection(collectionContacts).Doc(contact.ID.String()).Set(ctx, contactData, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("firestore sync contact %s: %w", contact.ID, err)
	}
	return nil
}

// SyncContact merges contact summary fields without a new message.
// Used after outbound last-message updates and manual close/reopen.
func (c *Client) SyncContact(ctx context.Context, orgID uuid.UUID, contact *models.Contact, maskPhone bool) error {
	if !c.Enabled() || contact == nil {
		return nil
	}

	profileName := contact.ProfileName
	phoneNumber := contact.PhoneNumber
	if maskPhone {
		profileName = utils.MaskIfPhoneNumber(profileName)
		phoneNumber = utils.MaskPhoneNumber(phoneNumber)
	}

	contactData := map[string]any{
		"organizationId":  orgID.String(),
		"phoneNumber":     phoneNumber,
		"profileName":     profileName,
		"whatsappAccount": contact.WhatsAppAccount,
		"tags":            stringTags(contact.Tags),
	}
	applyAssignedUserField(contactData, contact)
	if contact.LastMessageAt != nil {
		contactData["lastMessageAt"] = contact.LastMessageAt.UTC().Format(time.RFC3339)
	}
	if contact.LastInboundAt != nil {
		contactData["lastInboundAt"] = contact.LastInboundAt.UTC().Format(time.RFC3339)
	}
	applyConversationWaitFields(contactData, contact)

	_, err := c.firestore.Collection(collectionContacts).Doc(contact.ID.String()).Set(ctx, contactData, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("firestore sync contact %s: %w", contact.ID, err)
	}
	return nil
}

func stringTags(values models.JSONBArray) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if tag, ok := value.(string); ok {
			out = append(out, tag)
		}
	}
	return out
}

// applyConversationWaitFields writes first-response SLA fields in camelCase so
// they match the rest of the contact document. A nil clock is deleted so SLA
// heat does not keep a stale timestamp after an outbound reply.
func applyConversationWaitFields(data map[string]any, contact *models.Contact) {
	if contact == nil {
		return
	}

	data["isClosed"] = contact.IsClosed
	if contact.AwaitingReplySince != nil {
		data["awaitingReplySince"] = contact.AwaitingReplySince.UTC().Format(time.RFC3339)
		return
	}
	data["awaitingReplySince"] = firestore.Delete
}

func applyAssignedUserField(data map[string]any, contact *models.Contact) {
	if contact == nil || data == nil {
		return
	}
	if contact.AssignedUserID != nil {
		data["assignedUserId"] = contact.AssignedUserID.String()
		return
	}
	data["assignedUserId"] = firestore.Delete
}

// applyChatbotPausedFields writes chatbot pause state in camelCase so it matches
// the rest of the contact document. A nil transfer id is deleted when unpaused.
func applyChatbotPausedFields(data map[string]any, paused bool, transferID *uuid.UUID) {
	if data == nil {
		return
	}

	data["chatbotPaused"] = paused
	if paused && transferID != nil {
		data["activeTransferId"] = transferID.String()
		return
	}
	data["activeTransferId"] = firestore.Delete
}

func buildMessageData(orgID uuid.UUID, msg *models.Message, contact *models.Contact, maskPhone bool) map[string]any {
	profileName := contact.ProfileName
	if maskPhone {
		profileName = utils.MaskIfPhoneNumber(profileName)
	}

	data := map[string]any{
		"organizationId": orgID.String(),
		"contactId":      contact.ID.String(),
		"profileName":    profileName,
		"direction":      string(msg.Direction),
		"messageType":    string(msg.MessageType),
		"content":        map[string]string{"body": msg.Content},
		"status":         string(msg.Status),
		"wamid":          msg.WhatsAppMessageID,
		"createdAt":      msg.CreatedAt.UTC().Format(time.RFC3339),
		"updatedAt":      msg.UpdatedAt.UTC().Format(time.RFC3339),
		"isReply":        msg.IsReply,
	}

	applyAssignedUserField(data, contact)
	if msg.MediaURL != "" {
		data["mediaUrl"] = msg.MediaURL
	}
	if msg.MediaMimeType != "" {
		data["mediaMimeType"] = msg.MediaMimeType
	}
	if msg.MediaFilename != "" {
		data["mediaFilename"] = msg.MediaFilename
	}
	if msg.WhatsAppAccount != "" {
		data["whatsappAccount"] = msg.WhatsAppAccount
	}
	if msg.InteractiveData != nil {
		data["interactiveData"] = msg.InteractiveData
	}
	if msg.ErrorMessage != "" {
		data["errorMessage"] = msg.ErrorMessage
	}
	if msg.IsReply && msg.ReplyToMessageID != nil {
		data["replyToMessageId"] = msg.ReplyToMessageID.String()
	}

	return data
}
