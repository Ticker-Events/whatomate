package contactutil

import (
	"strings"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetOrCreateContact finds or creates a contact for the given phone number.
// Merges behaviors from both handler and worker implementations:
//   - Normalizes phone (strips leading "+")
//   - Tries both normalized and +prefix forms
//   - Updates profile name if changed
//   - Handles race conditions on create via upsert + re-fetch
//   - Restores soft-deleted contacts if found
//
// Returns the contact, whether it was newly created, and any error.
func GetOrCreateContact(db *gorm.DB, orgID uuid.UUID, phoneNumber, profileName string) (*models.Contact, bool, error) {
	normalizedPhone := normalizePhone(phoneNumber)
	if normalizedPhone == "" {
		return nil, false, gorm.ErrInvalidData
	}

	if contact, found, err := findExistingContact(db, orgID, normalizedPhone, profileName); found {
		return contact, false, err
	}

	contact := models.Contact{
		BaseModel:      models.BaseModel{ID: uuid.New()},
		OrganizationID: orgID,
		PhoneNumber:    normalizedPhone,
		ProfileName:    profileName,
	}

	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "organization_id"}, {Name: "phone_number"}},
		DoNothing: true,
	}).Create(&contact)
	if result.Error != nil {
		if existing, found, err := findExistingContact(db, orgID, normalizedPhone, profileName); found {
			return existing, false, err
		}
		return nil, false, result.Error
	}

	if result.RowsAffected == 0 {
		existing, found, err := findExistingContact(db, orgID, normalizedPhone, profileName)
		if !found {
			return nil, false, gorm.ErrRecordNotFound
		}
		return existing, false, err
	}

	return &contact, true, nil
}

func normalizePhone(phoneNumber string) string {
	phone := strings.TrimSpace(phoneNumber)
	if strings.HasPrefix(phone, "+") {
		phone = phone[1:]
	}
	return phone
}

func findExistingContact(db *gorm.DB, orgID uuid.UUID, normalizedPhone, profileName string) (*models.Contact, bool, error) {
	for _, phone := range []string{normalizedPhone, "+" + normalizedPhone} {
		var contact models.Contact
		if err := db.Unscoped().Where("organization_id = ? AND phone_number = ?", orgID, phone).First(&contact).Error; err != nil {
			continue
		}
		return finalizeContact(db, &contact, profileName)
	}
	return nil, false, nil
}

func finalizeContact(db *gorm.DB, contact *models.Contact, profileName string) (*models.Contact, bool, error) {
	if contact.DeletedAt.Valid {
		db.Unscoped().Model(contact).Update("deleted_at", nil)
		contact.DeletedAt.Valid = false
	}
	if profileName != "" && contact.ProfileName != profileName {
		db.Model(contact).Update("profile_name", profileName)
		contact.ProfileName = profileName
	}
	return contact, true, nil
}
