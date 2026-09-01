package contactutil

import (
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
)

// ApplyConversationWait updates first-response wait fields on contact and
// returns the GORM map to persist.
//
// Inbound: reopen (is_closed=false). Set awaiting_reply_since if empty or the
// chat was closed; otherwise keep the existing clock.
// Outbound: close (is_closed=true) and clear awaiting_reply_since.
func ApplyConversationWait(contact *models.Contact, incoming bool, at time.Time) map[string]any {
	if contact == nil {
		return map[string]any{}
	}

	if incoming {
		wasClosed := contact.IsClosed
		contact.IsClosed = false
		updates := map[string]any{"is_closed": false}
		if wasClosed || contact.AwaitingReplySince == nil {
			stamp := at
			contact.AwaitingReplySince = &stamp
			updates["awaiting_reply_since"] = stamp
		}
		return updates
	}

	contact.IsClosed = true
	contact.AwaitingReplySince = nil
	return map[string]any{
		"is_closed":            true,
		"awaiting_reply_since": nil,
	}
}
