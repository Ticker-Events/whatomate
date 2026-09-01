package firestore

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/internal/utils"
)

const (
	collectionMessages = "messages"
	collectionContacts = "contacts"
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
	}

	if contact.AssignedUserID != nil {
		contactData["assignedUserId"] = contact.AssignedUserID.String()
	}
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
	}
	if contact.AssignedUserID != nil {
		contactData["assignedUserId"] = contact.AssignedUserID.String()
	}
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

	if contact.AssignedUserID != nil {
		data["assignedUserId"] = contact.AssignedUserID.String()
	}
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
