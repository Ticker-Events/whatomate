package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
)

// syncMessageToFirestore writes a new or updated message and contact summary to Firestore.
func (a *App) syncMessageToFirestore(orgID uuid.UUID, msg *models.Message, contact *models.Contact) {
	if a.Firestore == nil || !a.Firestore.Enabled() {
		return
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		maskPhone := a.ShouldMaskPhoneNumbers(orgID)
		if err := a.Firestore.SyncMessage(ctx, orgID, msg, contact, maskPhone); err != nil {
			a.Log.Error("Failed to sync message to Firestore", "error", err, "message_id", msg.ID)
		}
	}()
}

// syncMessageStatusToFirestore updates message delivery status in Firestore.
func (a *App) syncMessageStatusToFirestore(messageID uuid.UUID, status models.MessageStatus, errorMessage string) {
	if a.Firestore == nil || !a.Firestore.Enabled() {
		return
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := a.Firestore.UpdateMessageStatus(ctx, messageID, status, errorMessage); err != nil {
			a.Log.Error("Failed to sync message status to Firestore", "error", err, "message_id", messageID)
		}
	}()
}

// syncContactUnreadResetToFirestore clears the unread badge on a contact document.
func (a *App) syncContactUnreadResetToFirestore(orgID, contactID uuid.UUID) {
	if a.Firestore == nil || !a.Firestore.Enabled() {
		return
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := a.Firestore.ResetContactUnread(ctx, orgID, contactID); err != nil {
			a.Log.Error("Failed to reset contact unread in Firestore", "error", err, "contact_id", contactID)
		}
	}()
}
