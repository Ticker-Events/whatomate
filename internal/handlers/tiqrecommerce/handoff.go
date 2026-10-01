package tiqrecommerce

import (
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/tickermcp"
)

const (
	HandoffCartKey    = "commerce_handoff_cart"
	CartSource   = "tiqr_ecommerce"
	commerceNotesMissingKey   = "missing_fields"
	commerceNotesOrderNotesKey = "order_notes"
)

// transferTiqrEcommerce snapshots the coded-flow cart and checkout fields onto a
// commerce draft, creates a commerce agent transfer, and ends the session.
// Outside business hours it sends the out-of-hours message and ends without a
// draft. Any other failure returns an error so Transfer can fall back to the
// queue transfer.
func TransferEcommerce(base *codedflow.Conv, message string) error {
	return wrap(base).transferTiqrEcommerce(message)
}

func (c *Conv) transferTiqrEcommerce(message string) error {
	if c == nil || c.App() == nil || c.ChatCtx() == nil {
		return errors.New("coded ecommerce transfer is not configured")
	}
	account := c.ChatCtx().Account()
	contact := c.ChatCtx().Contact()
	session := c.Session()
	if account == nil || contact == nil || session == nil {
		return errors.New("coded ecommerce transfer is missing account or contact")
	}

	settings, err := c.App().GetChatbotSettingsCached(account.OrganizationID, account.Name)
	if err != nil || settings == nil {
		return fmt.Errorf("load chatbot settings for ecommerce handoff: %w", err)
	}

	body := strings.TrimSpace(c.Text(message))
	if body == "" {
		body = codedflow.AgentHandoff
	}

	if settings.BusinessHours.Enabled && len(settings.BusinessHours.Hours) > 0 &&
		!c.App().IsWithinBusinessHours(settings.BusinessHours.Hours) {
		c.App().LogInfo("Outside business hours, sending out-of-hours message instead of ecommerce handoff",
			"contact_id", contact.ID)
		if settings.BusinessHours.OutOfHoursMessage != "" {
			_ = c.App().SendAndSaveTextMessage(account, contact, settings.BusinessHours.OutOfHoursMessage)
		} else {
			_ = c.App().SendAndSaveTextMessage(account, contact, body)
		}
		codedflow.FinishSession(session)
		return nil
	}

	stageTiqrEcommerceHandoffSession(c.App(), session)

	if _, err := c.App().EnsureCommerceDraft(contact, session, settings); err != nil {
		return fmt.Errorf("ensure commerce draft for ecommerce handoff: %w", err)
	}
	_ = c.App().SyncReferencedCommerceDraft(session)

	category := tiqrEcommerceHandoffCategory(session)

	if err := c.App().PersistSessionData(session); err != nil {
		c.App().LogWarn("persist ecommerce handoff session before transfer failed", "error", err)
	}

	_ = c.App().SendAndSaveTextMessage(account, contact, body)

	transfer, created, err := c.App().CreateCommerceTransfer(account, contact, session, settings, category)
	if err != nil {
		return fmt.Errorf("create commerce transfer for ecommerce handoff: %w", err)
	}
	c.App().StageCommerceHandoffSessionData(session, category)
	if created {
		c.App().LogInfo("tiqr ecommerce handoff active",
			"transfer_id", transfer.ID, "draft_id", transfer.CommerceDraftID)
	}

	// createCommerceTransfer cancels the session in the DB. Keep the in-memory
	// session cancelled so persistChatSession does not revive it as completed.
	finishCodedEcommerceHandoffSession(session)
	return nil
}

func stageTiqrEcommerceHandoffSession(h codedflow.Host, session *models.ChatbotSession) {
	if session == nil {
		return
	}
	if session.SessionData == nil {
		session.SessionData = models.JSONB{}
	}

	if h != nil {
		h.StageEarlyHandoffCheckout(session)
		if raw, ok := session.SessionData[checkoutSessionKey].(map[string]any); ok && raw != nil {
			raw["flow"] = "coded_ecommerce"
			raw["step"] = "handoff"
			session.SessionData[checkoutSessionKey] = raw
		}
		if id := strings.TrimSpace(asString(session.SessionData["collection_id"])); id != "" {
			h.SetSelectedCategoryID(session, id)
		}
	}

	notes := jsonMapFromSession(session, "commerce_notes")
	if orderNotes := strings.TrimSpace(CodedOrderNotes(session.SessionData)); orderNotes != "" {
		notes[commerceNotesOrderNotesKey] = orderNotes
	}
	if customerNotes := strings.TrimSpace(contextValue(session.SessionData, "customer_notes", "notes")); customerNotes != "" {
		if asString(notes["customer_notes"]) == "" {
			notes["customer_notes"] = customerNotes
		}
	}
	if missing := codedMissingFieldsFromSession(session); len(missing) > 0 {
		values := make([]any, 0, len(missing))
		for _, field := range missing {
			values = append(values, field)
		}
		notes[commerceNotesMissingKey] = values
	}
	session.SessionData["commerce_notes"] = map[string]any(notes)
	session.SessionData[HandoffCartKey] = map[string]any(tiqrEcommerceCartSnapshot(session))

	if storeID := tiqrEcommerceStoreID(session); storeID != "" {
		if store, ok := asStringMap(session.SessionData["store"]); ok {
			store["id"] = storeID
			session.SessionData["store"] = store
		}
	}
}

func tiqrEcommerceHandoffCategory(session *models.ChatbotSession) tickermcp.Category {
	id := asString(session.SessionData["selected_category_id"])
	if id == "" {
		id = asString(session.SessionData["collection_id"])
	}
	if id == "" {
		id = strings.TrimSpace(asString(session.SessionData["collection_id"]))
	}
	if col := sessionCollectionByID(session, id); col != nil {
		return categoryFromCollectionMapLocal(col)
	}
	name := strings.TrimSpace(asString(session.SessionData["collection_name"]))
	if name == "" {
		name = "Ecommerce request"
	}
	category := tickermcp.Category{Name: name, HandoffPolicy: "none"}
	if id != "" {
		if n, err := strconv.Atoi(id); err == nil {
			category.ID = n
		}
	}
	return category
}

func tiqrEcommerceCartSnapshot(session *models.ChatbotSession) models.JSONB {
	lines := make([]any, 0)
	if session != nil {
		for _, line := range tiqrCartLinesFromData(session.SessionData) {
			item := map[string]any{
				"product_option": line.OptionID,
				"quantity":       line.Qty,
				"option_name":    line.Name,
			}
			if line.ProductName != "" {
				item["product_name"] = line.ProductName
			}
			if line.Price != 0 {
				item["price"] = line.Price
			}
			if len(line.Capture) > 0 {
				item["capture_fields"] = line.Capture
			}
			if len(line.Labels) > 0 {
				item["capture_labels"] = line.Labels
			}
			lines = append(lines, item)
		}
	}
	return models.JSONB{
		"source": CartSource,
		"lines":  lines,
	}
}

func tiqrCartLinesFromData(data map[string]any) []TiqrCartLine {
	if data == nil {
		return nil
	}
	cart, ok := anySlice(data["tiqr_cart"])
	if !ok {
		return nil
	}
	out := make([]TiqrCartLine, 0, len(cart))
	for _, entry := range cart {
		item, ok := asStringMap(entry)
		if !ok {
			continue
		}
		optionID := strings.TrimSpace(asString(item["product_option"]))
		if optionID == "" {
			continue
		}
		name := strings.TrimSpace(asString(item["option_name"]))
		if name == "" {
			name = "Option " + optionID
		}
		qty := parsePositiveInt(asString(item["quantity"]))
		if qty < 1 {
			if n, ok := anyToFloat64(item["quantity"]); ok && int(n) >= 1 {
				qty = int(n)
			} else {
				qty = 1
			}
		}
		price, _ := anyToFloat64(item["price"])
		out = append(out, TiqrCartLine{
			OptionID:    optionID,
			Name:        name,
			ProductName: strings.TrimSpace(asString(item["product_name"])),
			Qty:         qty,
			Price:       price,
			Capture:     lineCaptureMap(item["capture_fields"]),
			Labels:      lineLabelMap(item["capture_labels"]),
		})
	}
	return out
}

func tiqrEcommerceStoreID(session *models.ChatbotSession) string {
	if session == nil || session.SessionData == nil {
		return ""
	}
	if store, ok := asStringMap(session.SessionData["store"]); ok {
		if id := strings.TrimSpace(asString(store["id"])); id != "" {
			return id
		}
	}
	return ""
}

func codedMissingFieldsFromSession(session *models.ChatbotSession) []string {
	if session == nil || session.SessionData == nil {
		return nil
	}
	records, ok := anySlice(session.SessionData[codedflow.CallsKey])
	if !ok {
		return nil
	}
	for i := len(records) - 1; i >= 0; i-- {
		rec, ok := asStringMap(records[i])
		if !ok || asString(rec["plan"]) != "recover" {
			continue
		}
		if asString(rec["kind"]) != /*recover*/ "missing_field" {
			return nil
		}
		asks, ok := anySlice(rec["asks"])
		if !ok {
			if field := strings.TrimSpace(asString(rec["field"])); field != "" {
				return []string{field}
			}
			return nil
		}
		out := make([]string, 0, len(asks))
		seen := map[string]bool{}
		for _, item := range asks {
			ask, ok := asStringMap(item)
			if !ok {
				continue
			}
			field := strings.TrimSpace(asString(ask["field"]))
			if field == "" || seen[field] {
				continue
			}
			seen[field] = true
			out = append(out, field)
		}
		return out
	}
	return nil
}

func sessionHandoffCart(session *models.ChatbotSession) models.JSONB {
	if session == nil || session.SessionData == nil {
		return nil
	}
	raw, ok := asStringMap(session.SessionData[HandoffCartKey])
	if !ok || len(raw) == 0 {
		return nil
	}
	return cloneJSONMap(raw)
}

func finishCodedEcommerceHandoffSession(session *models.ChatbotSession) {
	if session == nil {
		return
	}
	if session.SessionData != nil {
		delete(session.SessionData, codedflow.DataKey)
		delete(session.SessionData, HandoffCartKey)
	}
	session.CurrentStep = ""
	session.StepRetries = 0
	session.CurrentFlowID = nil
	session.Status = models.SessionStatusCancelled
	now := time.Now()
	session.CompletedAt = &now
}

func CommerceHandoffContact(draft *models.CommerceDraft) map[string]any {
	if draft == nil {
		return map[string]any{}
	}
	addr := map[string]any(draft.AddressSnapshot)
	out := map[string]any{}
	if name := strings.TrimSpace(asString(addr["name"])); name != "" {
		out["name"] = name
	}
	if email := strings.TrimSpace(asString(addr["email"])); email != "" {
		out["email"] = email
	}
	phone := strings.TrimSpace(asString(addr["phone"]))
	if phone == "" {
		phone = strings.TrimSpace(asString(addr["phone_number"]))
	}
	if phone != "" {
		out["phone"] = phone
		out["phone_number"] = phone
	}
	return out
}

func CommerceHandoffMissingFields(draft *models.CommerceDraft) []string {
	if draft == nil || draft.Notes == nil {
		return nil
	}
	raw, ok := anySlice(draft.Notes[commerceNotesMissingKey])
	if !ok {
		if field := strings.TrimSpace(asString(draft.Notes[commerceNotesMissingKey])); field != "" {
			return []string{field}
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if field := strings.TrimSpace(asString(item)); field != "" {
			out = append(out, field)
		}
	}
	return out
}

func CommerceHandoffNotesText(draft *models.CommerceDraft) string {
	if draft == nil || draft.Notes == nil {
		return ""
	}
	if notes := strings.TrimSpace(asString(draft.Notes[commerceNotesOrderNotesKey])); notes != "" {
		return notes
	}
	return strings.TrimSpace(asString(draft.Notes["customer_notes"]))
}

// ensureDraftStoreID prefers the store loaded by the coded flow when the draft
// was created with an empty CommerceStoreID.
func ensureDraftStoreID(draft *models.CommerceDraft, session *models.ChatbotSession) {
	if draft == nil || strings.TrimSpace(draft.StoreID) != "" {
		return
	}
	if id := tiqrEcommerceStoreID(session); id != "" {
		draft.StoreID = id
	}
}

func ApplySessionHandoffCart(draft *models.CommerceDraft, session *models.ChatbotSession) {
	if draft == nil {
		return
	}
	if cart := sessionHandoffCart(session); len(cart) > 0 {
		draft.Cart = cart
	}
	ensureDraftStoreID(draft, session)
}

func categoryFromCollectionMapLocal(raw map[string]any) tickermcp.Category {
	if raw == nil {
		return tickermcp.Category{Name: "Ecommerce request", HandoffPolicy: "none"}
	}
	cat := tickermcp.Category{
		Name:           asString(raw["name"]),
		Description:    asString(raw["description"]),
		AIInstructions: asString(raw["ai_instructions"]),
		HandoffPolicy:  strings.ToLower(strings.TrimSpace(asString(raw["handoff_policy"]))),
		HandoffMessage: asString(raw["handoff_message"]),
	}
	if id := fieldString(raw, "id"); id != "" {
		if n, err := strconv.Atoi(id); err == nil {
			cat.ID = n
		}
	}
	if cat.Name == "" {
		cat.Name = "Ecommerce request"
	}
	return cat
}
