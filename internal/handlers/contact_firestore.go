package handlers

import (
	"context"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
)

func (a *App) syncContactToFirestore(contact *models.Contact) {
	if a == nil || a.Firestore == nil || !a.Firestore.Enabled() || contact == nil {
		return
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		maskPhone := a.ShouldMaskPhoneNumbers(contact.OrganizationID)
		if err := a.Firestore.SyncContact(ctx, contact.OrganizationID, contact, maskPhone); err != nil {
			a.Log.Error("Failed to sync contact to Firestore", "contact_id", contact.ID, "error", err)
		}
	}()
}
