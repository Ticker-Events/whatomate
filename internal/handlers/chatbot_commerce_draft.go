package handlers

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	draftrepo "github.com/shridarpatil/whatomate/internal/commerce"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
)

const commerceDraftIDKey = "commerce_draft_id"

func commerceDraftID(session *models.ChatbotSession) uuid.UUID {
	if session == nil || session.SessionData == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(strings.TrimSpace(asString(session.SessionData[commerceDraftIDKey])))
	return id
}

func (a *App) ensureCommerceDraft(contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings) (*models.CommerceDraft, error) {
	if a == nil || a.DB == nil || contact == nil || session == nil || settings == nil {
		return nil, errors.New("durable checkout is not configured")
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	repo := draftrepo.NewDraftRepository(a.DB)
	if id := commerceDraftID(session); id != uuid.Nil {
		draft, err := repo.Get(id, session.OrganizationID)
		if err != nil {
			return nil, fmt.Errorf("load referenced commerce draft: %w", err)
		}
		return draft, nil
	}
	storeID := strings.TrimSpace(settings.AI.CommerceStoreID)

	// Never hydrate LatestActive onto a session that has no draft yet — that
	// leaked captured fields / addons / cart from a previous chatbot session.
	if err := repo.AbandonActive(session.OrganizationID, contact.ID, session.WhatsAppAccount, storeID); err != nil {
		return nil, fmt.Errorf("abandon previous commerce draft: %w", err)
	}

	draft := &models.CommerceDraft{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  session.OrganizationID,
		ContactID:       contact.ID,
		WhatsAppAccount: session.WhatsAppAccount,
		StoreID:         storeID,
		Cart:            models.JSONB{},
		Addons:          models.JSONBArray{},
		CapturedFields:  models.JSONB{},
		Notes:           models.JSONB{},
	}
	if err := repo.Create(draft); err != nil {
		return nil, err
	}
	// Create may reload a concurrent owner (e.g. pending_payment). Bind the id
	// only — do not copy cart/capture/addons into this session.
	session.SessionData[commerceDraftIDKey] = draft.ID.String()
	return draft, nil
}

// recoverCommerceDraft used to hydrate LatestActive draft contents into any
// session missing commerce_draft_id. That leaked prior-session captured fields
// and addons into new chats. Drafts are created on demand via ensureCommerceDraft.
func (a *App) recoverCommerceDraft(contact *models.Contact, session *models.ChatbotSession, settings *models.ChatbotSettings) {
	_ = contact
	_ = session
	_ = settings
}

func (a *App) recordCommerceMessage(session *models.ChatbotSession, messageID string) bool {
	id := commerceDraftID(session)
	if id == uuid.Nil || strings.TrimSpace(messageID) == "" {
		return true
	}
	repo := draftrepo.NewDraftRepository(a.DB)
	draft, err := repo.Get(id, session.OrganizationID)
	if err != nil {
		a.Log.Error("load commerce draft for message ledger failed", "error", err, "draft_id", id)
		return false
	}
	err = repo.RecordMessage(id, session.OrganizationID, draft.Version, messageID)
	if errors.Is(err, draftrepo.ErrDuplicateMessage) {
		return false
	}
	if err != nil {
		a.Log.Error("record commerce message failed closed", "error", err)
		return false
	}
	return true
}

func (a *App) syncReferencedCommerceDraft(session *models.ChatbotSession) error {
	id := commerceDraftID(session)
	if id == uuid.Nil || a == nil || a.DB == nil {
		return nil
	}
	repo := draftrepo.NewDraftRepository(a.DB)
	draft, err := repo.Get(id, session.OrganizationID)
	if err != nil {
		return err
	}
	if draft.Status != "active" {
		return nil
	}
	expected := draft.Version
	draft.Cart = commerceDraftCart(session)
	draft.Addons = tiqrecommerce.HandoffDisplayAddons(session, draft.Addons)
	draft.CapturedFields = jsonMapFromSession(session, "commerce_captured_fields")
	draft.Notes = jsonMapFromSession(session, "commerce_notes")
	if categoryID, parseErr := strconv.Atoi(selectedCategoryID(session)); parseErr == nil && categoryID > 0 {
		draft.SelectedCategoryID = &categoryID
	}
	if state := getCheckoutState(session); state != nil {
		draft.FulfillmentMode = state.DeliveryMode
		draft.FulfillmentSlotToken = state.SlotToken
		draft.SavedAddressID = state.SavedAddressID
		draft.AddressSnapshot = models.JSONB(state.NewAddress)
		if draft.AddressSnapshot == nil {
			draft.AddressSnapshot = models.JSONB{}
		}
		if state.HasLocation {
			draft.Latitude, draft.Longitude = &state.Latitude, &state.Longitude
		}
	}
	return repo.Save(draft, expected)
}

func hydrateSessionFromDraft(session *models.ChatbotSession, draft *models.CommerceDraft) {
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}
	session.SessionData[commerceDraftIDKey] = draft.ID.String()
	session.SessionData[cartKey] = map[string]any(draft.Cart)
	session.SessionData["commerce_addons"] = draft.Addons
	session.SessionData["commerce_captured_fields"] = draft.CapturedFields
	session.SessionData["commerce_notes"] = draft.Notes
	if draft.SelectedCategoryID != nil {
		session.SessionData[selectedCategoryKey] = strconv.Itoa(*draft.SelectedCategoryID)
	}
	if draft.FulfillmentMode != "" || draft.FulfillmentSlotToken != "" {
		setCheckoutState(session, &checkoutState{
			Step:           "delivery_mode",
			DeliveryMode:   draft.FulfillmentMode,
			SlotToken:      draft.FulfillmentSlotToken,
			SavedAddressID: draft.SavedAddressID,
			NewAddress:     map[string]any(draft.AddressSnapshot),
		})
	}
}

func jsonMapFromSession(session *models.ChatbotSession, key string) models.JSONB {
	if value, ok := session.SessionData[key].(map[string]any); ok {
		return cloneJSONMap(value)
	}
	return models.JSONB{}
}

func jsonArrayFromSession(session *models.ChatbotSession, key string) models.JSONBArray {
	if session == nil || session.SessionData == nil {
		return models.JSONBArray{}
	}
	switch value := session.SessionData[key].(type) {
	case []any:
		out := make(models.JSONBArray, len(value))
		copy(out, value)
		return out
	case models.JSONBArray:
		out := make(models.JSONBArray, len(value))
		copy(out, value)
		return out
	default:
		return models.JSONBArray{}
	}
}

func cloneJSONMap(source map[string]any) models.JSONB {
	out := models.JSONB{}
	for key, value := range source {
		out[key] = value
	}
	return out
}

func commerceDraftCart(session *models.ChatbotSession) models.JSONB {
	if cart := tiqrecommerce.HandoffDisplayCart(session); len(cart) > 0 {
		return cart
	}
	return cartJSON(session)
}

func cartJSON(session *models.ChatbotSession) models.JSONB {
	out := models.JSONB{}
	if session == nil || session.SessionData == nil {
		return out
	}
	for key, line := range normalizeCartMap(session.SessionData[cartKey]) {
		out[key] = line
	}
	return out
}
