package handlers

import (
	"context"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
)

func (a *App) syncContactToFirestore(contact *models.Contact) {
	if a == nil || a.ContactStore == nil || contact == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.ContactStore.SyncContact(ctx, contact); err != nil {
		a.Log.Error("Failed to sync contact to Firestore", "contact_id", contact.ID, "error", err)
	}
}
