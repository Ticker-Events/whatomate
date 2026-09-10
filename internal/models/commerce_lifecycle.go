package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	CommerceLifecycleStatusPending    = "pending"
	CommerceLifecycleStatusProcessing = "processing"
	CommerceLifecycleStatusProcessed  = "processed"
	CommerceLifecycleStatusFailed     = "failed"
	CommerceLifecycleStatusStale      = "stale"
	CommerceLifecycleStatusDeadLetter = "dead_letter"
	CommerceLifecycleStatusTerminal   = "terminal_failed"
)

// CommerceLifecycleConfig binds a Backend store and WhatsApp account to the
// independently revocable API credential and encrypted signing secret used for
// Backend -> Whatomate lifecycle delivery.
type CommerceLifecycleConfig struct {
	BaseModel
	OrganizationID  uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_commerce_lifecycle_config,priority:1" json:"organization_id"`
	StoreID         string    `gorm:"size:50;not null;uniqueIndex:idx_commerce_lifecycle_config,priority:2" json:"store_id"`
	WhatsAppAccount string    `gorm:"size:100;not null;uniqueIndex:idx_commerce_lifecycle_config,priority:3" json:"whatsapp_account"`
	APIKeyID        uuid.UUID `gorm:"type:uuid;not null;index" json:"api_key_id"`
	SigningSecret   string    `gorm:"type:text;not null" json:"-"`
	Enabled         bool      `gorm:"not null;default:true" json:"enabled"`
}

func (CommerceLifecycleConfig) TableName() string { return "commerce_lifecycle_configs" }

// CommerceLifecycleEvent is the durable receiver ledger. EventID is globally
// unique because it is also the sender's idempotency key.
type CommerceLifecycleEvent struct {
	BaseModel
	EventID           uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex" json:"event_id"`
	OrganizationID    uuid.UUID  `gorm:"type:uuid;not null;index" json:"organization_id"`
	StoreID           string     `gorm:"size:50;not null;index" json:"store_id"`
	WhatsAppAccount   string     `gorm:"size:100;not null;index" json:"whatsapp_account"`
	EventType         string     `gorm:"size:50;not null;index" json:"event_type"`
	Version           string     `gorm:"size:10;not null" json:"version"`
	OrderID           string     `gorm:"size:100;not null;index" json:"order_id"`
	PaymentID         string     `gorm:"size:100;index" json:"payment_id,omitempty"`
	CorrelationID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"correlation_id"`
	Sequence          *int64     `gorm:"index" json:"sequence,omitempty"`
	OccurredAt        time.Time  `gorm:"not null" json:"occurred_at"`
	ReceivedAt        time.Time  `gorm:"not null" json:"received_at"`
	ProcessedAt       *time.Time `json:"processed_at,omitempty"`
	CanonicalPayload  JSONB      `gorm:"type:jsonb;not null" json:"payload"`
	Status            string     `gorm:"size:20;not null;default:'pending';index" json:"status"`
	Attempts          int        `gorm:"not null;default:0" json:"attempts"`
	LastError         string     `gorm:"type:text" json:"last_error,omitempty"`
	MessageID         *uuid.UUID `gorm:"type:uuid;index" json:"message_id,omitempty"`
	ProcessingClaimed *time.Time `gorm:"index" json:"processing_claimed_at,omitempty"`
}

func (CommerceLifecycleEvent) TableName() string { return "commerce_lifecycle_events" }
