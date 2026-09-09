package models

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CommerceDraft is the durable checkout aggregate. Chatbot sessions retain only
// its ID; all order-bearing state lives here so a checkout can survive timeout.
type CommerceDraft struct {
	BaseModel
	OrganizationID  uuid.UUID `gorm:"type:uuid;not null;index:idx_commerce_drafts_owner,priority:1" json:"organization_id"`
	ContactID       uuid.UUID `gorm:"type:uuid;not null;index:idx_commerce_drafts_owner,priority:2" json:"contact_id"`
	WhatsAppAccount string    `gorm:"size:100;not null;index:idx_commerce_drafts_owner,priority:3" json:"whatsapp_account"`
	StoreID         string    `gorm:"size:50;not null;index" json:"store_id"`

	Status  string `gorm:"size:30;not null;default:'active';index" json:"status"`
	Version int64  `gorm:"not null;default:1" json:"version"`
	// ActiveOwnerKey is populated only while a draft is recoverable. Its
	// database uniqueness constraint prevents concurrent active drafts on
	// every supported SQL dialect without relying on a partial index.
	ActiveOwnerKey *string `gorm:"size:64;uniqueIndex" json:"-"`

	SelectedCategoryID *int       `json:"selected_category_id,omitempty"`
	Cart               JSONB      `gorm:"type:jsonb;not null;default:'{}'" json:"cart"`
	Addons             JSONBArray `gorm:"type:jsonb;not null;default:'[]'" json:"addons"`
	CapturedFields     JSONB      `gorm:"type:jsonb;not null;default:'{}'" json:"captured_fields"`
	Attachments        JSONBArray `gorm:"type:jsonb;not null;default:'[]'" json:"attachments"`
	Notes              JSONB      `gorm:"type:jsonb;not null;default:'{}'" json:"notes"`

	FulfillmentMode        string     `gorm:"size:40" json:"fulfillment_mode,omitempty"`
	FulfillmentSlotToken   string     `gorm:"type:text" json:"fulfillment_slot_token,omitempty"`
	RequestedFulfillmentAt *time.Time `json:"requested_fulfillment_at,omitempty"`
	PromisedReadyAt        *time.Time `json:"promised_ready_at,omitempty"`

	SavedAddressID  *int     `json:"saved_address_id,omitempty"`
	AddressSnapshot JSONB    `gorm:"type:jsonb;not null;default:'{}'" json:"address_snapshot"`
	Latitude        *float64 `json:"latitude,omitempty"`
	Longitude       *float64 `json:"longitude,omitempty"`

	BackendOrderID       string     `gorm:"size:100;index" json:"backend_order_id,omitempty"`
	BackendPaymentID     string     `gorm:"size:100" json:"backend_payment_id,omitempty"`
	BackendOrderStatus   string     `gorm:"size:50;index" json:"backend_order_status,omitempty"`
	BackendPaymentStatus string     `gorm:"size:50;index" json:"backend_payment_status,omitempty"`
	SubmissionKey        string     `gorm:"size:128;uniqueIndex:idx_commerce_draft_submission,where:submission_key <> ''" json:"submission_idempotency_key,omitempty"`
	TransferID           *uuid.UUID `gorm:"type:uuid;index" json:"transfer_id,omitempty"`
	LastLifecycleAt      *time.Time `gorm:"index" json:"last_lifecycle_at,omitempty"`
	LastLifecycleEventID *uuid.UUID `gorm:"type:uuid" json:"last_lifecycle_event_id,omitempty"`

	LastMessageID            string     `gorm:"size:255;index" json:"last_message_id,omitempty"`
	AbandonedAt              *time.Time `json:"abandoned_at,omitempty"`
	ReminderAt               *time.Time `json:"reminder_at,omitempty"`
	PendingPaymentReminderAt *time.Time `json:"pending_payment_reminder_at,omitempty"`
	AbandonedReminderAt      *time.Time `json:"abandoned_reminder_at,omitempty"`
	ReminderClaimedAt        *time.Time `gorm:"index" json:"reminder_claimed_at,omitempty"`
	ReminderClaimToken       string     `gorm:"size:64;index" json:"reminder_claim_token,omitempty"`
	ReminderClaimKind        string     `gorm:"size:30" json:"reminder_claim_kind,omitempty"`
	ReminderAttempts         int        `gorm:"not null;default:0" json:"reminder_attempts"`
	ReminderLastError        string     `gorm:"type:text" json:"reminder_last_error,omitempty"`

	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	Contact      *Contact      `gorm:"foreignKey:ContactID" json:"contact,omitempty"`
}

func (CommerceDraft) TableName() string { return "commerce_drafts" }

func (draft *CommerceDraft) RefreshActiveOwnerKey() {
	switch draft.Status {
	case "active", "submitting", "pending_payment":
		owner := fmt.Sprintf("%s\x00%s\x00%s\x00%s",
			draft.OrganizationID, draft.ContactID, draft.WhatsAppAccount, draft.StoreID)
		key := fmt.Sprintf("%x", sha256.Sum256([]byte(owner)))
		draft.ActiveOwnerKey = &key
	default:
		draft.ActiveOwnerKey = nil
	}
}

func (draft *CommerceDraft) BeforeCreate(_ *gorm.DB) error {
	draft.RefreshActiveOwnerKey()
	return nil
}

func (draft *CommerceDraft) BeforeSave(_ *gorm.DB) error {
	draft.RefreshActiveOwnerKey()
	return nil
}

// CommerceDraftMessage is the replay ledger for inbound webhook messages.
type CommerceDraftMessage struct {
	BaseModel
	OrganizationID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_commerce_draft_message,priority:1" json:"organization_id"`
	DraftID           uuid.UUID `gorm:"type:uuid;not null;index" json:"draft_id"`
	WhatsAppMessageID string    `gorm:"size:255;not null;uniqueIndex:idx_commerce_draft_message,priority:2" json:"whatsapp_message_id"`
}

func (CommerceDraftMessage) TableName() string { return "commerce_draft_messages" }
