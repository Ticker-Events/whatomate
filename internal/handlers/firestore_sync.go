package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/internal/storage"
	fs "github.com/shridarpatil/whatomate/pkg/firestore"
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
		synced := msg
		if msg != nil {
			copy := *msg
			copy.MediaURL = firestoreMediaURL(a.S3Client, msg.MediaURL)
			synced = &copy
		}
		if err := a.Firestore.SyncMessage(ctx, orgID, synced, contact, maskPhone); err != nil {
			a.Log.Error("Failed to sync message to Firestore", "error", err, "message_id", msg.ID)
		}
	}()
}

// firestoreMediaURL is the address tiqr.store loads. Relative storage keys become
// the Spaces URL. Values that are already absolute are left unchanged.
func firestoreMediaURL(client *storage.S3Client, stored string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" || strings.Contains(stored, "://") {
		return stored
	}
	if client == nil {
		return stored
	}
	if url := client.PublicURL(strings.TrimPrefix(stored, "/")); url != "" {
		return url
	}
	return stored
}

// displayMediaURL is the address shown in handoff summary, notes, and session
// archives. Prefer the public Spaces URL when available; otherwise /api/media/{id}.
func displayMediaURL(client *storage.S3Client, stored, messageID string) string {
	if public := firestoreMediaURL(client, stored); strings.Contains(public, "://") {
		return public
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return strings.TrimSpace(stored)
	}
	return "/api/media/" + messageID
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

// syncContactChatbotPausedToFirestore writes chatbot pause state onto the contact document.
func (a *App) syncContactChatbotPausedToFirestore(orgID, contactID uuid.UUID, transferID *uuid.UUID, paused bool) {
	if a == nil || a.Firestore == nil || !a.Firestore.Enabled() {
		return
	}

	var transferCopy *uuid.UUID
	if transferID != nil {
		id := *transferID
		transferCopy = &id
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := a.Firestore.SyncContactChatbotPaused(ctx, orgID, contactID, transferCopy, paused); err != nil {
			a.Log.Error("Failed to sync contact chatbot paused to Firestore", "error", err, "contact_id", contactID)
		}
	}()
}

func (a *App) syncCommerceHandoffToFirestore(orgID, contactID uuid.UUID, transfer *models.AgentTransfer, contact *models.Contact) {
	if a == nil || a.Firestore == nil || !a.Firestore.Enabled() || transfer == nil {
		return
	}
	tags := make([]string, 0, len(contact.Tags))
	for _, raw := range contact.Tags {
		if tag, ok := raw.(string); ok {
			tags = append(tags, tag)
		}
	}
	summary := ""
	if transfer.CommerceDraftID != nil {
		summary = "Commerce request " + transfer.CommerceDraftID.String() + " is awaiting an agent"
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.Firestore.SyncCommerceHandoff(ctx, orgID, contactID, transfer.ID, summary, tags); err != nil {
			a.Log.Error("Failed to sync commerce handoff to Firestore", "error", err, "transfer_id", transfer.ID)
		}
	}()
}

// flushSessionArchiveToFirestore writes the preview-shaped coded-flow archive
// to contacts/{id}/sessionArchives/{sessionId} with turn chunking.
func (a *App) flushSessionArchiveToFirestore(session *models.ChatbotSession) {
	if a == nil || a.Firestore == nil || !a.Firestore.Enabled() || session == nil {
		return
	}
	archive := codedflow.MarkArchiveEnded(session)
	if archive == nil || len(archive.Turns) == 0 {
		archive = codedflow.LoadSessionArchive(session)
	}
	if archive == nil || len(archive.Turns) == 0 {
		return
	}
	meta := fs.SessionArchiveDoc{
		Version:        archive.Version,
		FlowKey:        archive.FlowKey,
		SessionID:      archive.SessionID,
		ContactID:      archive.ContactID,
		OrganizationID: archive.OrganizationID,
		StartedAt:      archive.StartedAt,
		EndedAt:        archive.EndedAt,
		Status:         archive.Status,
		TurnCount:      len(archive.Turns),
	}
	turns := make([]map[string]any, 0, len(archive.Turns))
	for _, turn := range archive.Turns {
		b, err := json.Marshal(turn)
		if err != nil {
			continue
		}
		row := map[string]any{}
		if err := json.Unmarshal(b, &row); err != nil {
			continue
		}
		turns = append(turns, row)
	}
	if err := a.persistSessionData(session); err != nil {
		a.Log.Warn("persist session archive before Firestore flush failed",
			"error", err, "session_id", session.ID)
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := a.Firestore.SyncSessionArchive(ctx, meta, turns); err != nil {
			a.Log.Error("Failed to sync session archive to Firestore",
				"error", err, "session_id", session.ID)
		}
	}()
}
