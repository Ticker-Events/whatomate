package firestoresync

import (
	"context"

	"cloud.google.com/go/firestore"
	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/internal/models"
	"google.golang.org/api/option"
)

const contactsCollection = "contacts"

// Store projects Whatomate contacts onto Firestore documents.
type Store struct {
	client *firestore.Client
}

// New returns a Store when Firebase is configured, or (nil, nil) when disabled.
func New(ctx context.Context, cfg config.FirebaseConfig) (*Store, error) {
	if cfg.ProjectID == "" {
		return nil, nil
	}

	var opts []option.ClientOption
	if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	}

	client, err := firestore.NewClient(ctx, cfg.ProjectID, opts...)
	if err != nil {
		return nil, err
	}

	return &Store{client: client}, nil
}

// ContactDocument is the Whatomate-shaped Firestore payload for a contact.
// awaiting_reply_since is omitted when nil so callers can delete the field.
func ContactDocument(contact *models.Contact) map[string]any {
	if contact == nil {
		return map[string]any{}
	}

	data := map[string]any{
		"id":                   contact.ID.String(),
		"phone_number":         contact.PhoneNumber,
		"name":                 contact.ProfileName,
		"profile_name":         contact.ProfileName,
		"whatsapp_account":     contact.WhatsAppAccount,
		"last_message_preview": contact.LastMessagePreview,
		"is_read":              contact.IsRead,
		"is_closed":            contact.IsClosed,
		"organization_id":      contact.OrganizationID.String(),
	}
	if contact.LastMessageAt != nil {
		data["last_message_at"] = *contact.LastMessageAt
	}
	if contact.LastInboundAt != nil {
		data["last_inbound_at"] = *contact.LastInboundAt
	}
	if contact.AssignedUserID != nil {
		data["assigned_user_id"] = contact.AssignedUserID.String()
	}
	if contact.AwaitingReplySince != nil {
		data["awaiting_reply_since"] = *contact.AwaitingReplySince
	}
	return data
}

// SyncContact merges the contact into contacts/{id}. Clears awaiting_reply_since
// when the clock is empty so SLA heat does not keep a stale timestamp.
func (s *Store) SyncContact(ctx context.Context, contact *models.Contact) error {
	if s == nil || s.client == nil || contact == nil {
		return nil
	}

	data := ContactDocument(contact)
	if contact.AwaitingReplySince == nil {
		data["awaiting_reply_since"] = firestore.Delete
	}

	_, err := s.client.Collection(contactsCollection).Doc(contact.ID.String()).Set(ctx, data, firestore.MergeAll)
	return err
}

// Close releases the Firestore client.
func (s *Store) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
