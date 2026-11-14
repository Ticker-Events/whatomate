package models

import (
	"time"

	"github.com/google/uuid"
)

// SessionArchiveTurnRetry holds one archive turn until Firestore accepts it.
// Flow debug reads Firestore only; this row is a durable retry queue.
type SessionArchiveTurnRetry struct {
	BaseModel
	OrganizationID uuid.UUID `gorm:"type:uuid;index;not null" json:"organization_id"`
	ContactID      uuid.UUID `gorm:"type:uuid;index;not null" json:"contact_id"`
	SessionID      uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_archive_retry_session_turn" json:"session_id"`
	TurnIndex      int       `gorm:"not null;uniqueIndex:idx_archive_retry_session_turn" json:"turn_index"`
	Payload        JSONB     `gorm:"type:jsonb;not null" json:"payload"`
	Attempts       int       `gorm:"not null;default:0" json:"attempts"`
	LastError      string    `gorm:"type:text" json:"last_error,omitempty"`
	NextAttemptAt  time.Time `gorm:"index;not null" json:"next_attempt_at"`
}

func (SessionArchiveTurnRetry) TableName() string {
	return "session_archive_turn_retries"
}
