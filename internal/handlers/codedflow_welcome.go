package handlers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/handlers/tiqrecommerce"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"gorm.io/gorm"
)

const (
	codedFlowWelcomeMaxChars       = 700
	codedFlowWelcomeMaxCollections = 10
	codedFlowWelcomeAITimeout      = 45 * time.Second

	codedFlowWelcomeInstruction = `Write the store's first WhatsApp welcome message for a new shopper.

Use ONLY the store facts below. Do not invent products, prices, policies, or services.

Reply with ONE short message only (about 2–4 short sentences, under 500 characters):
1. Warm greeting that names the store.
2. One crisp line on what they sell (from description / about / collections).
3. Briefly mention pickup or delivery only when the facts say the store offers them.
4. Invite them to choose an option below (buy, check an order, or talk to staff).

Rules: plain text only — no markdown, no bullet lists, no prices, no policy text, and do not list every collection by name. Keep it welcoming and crisp.`
)

// generateCodedFlowWelcomeAI is swappable in tests.
var generateCodedFlowWelcomeAI = defaultGenerateCodedFlowWelcomeAI

func defaultGenerateCodedFlowWelcomeAI(a *App, settings *models.ChatbotSettings, prompt string) (string, error) {
	return a.CompleteCodedText(settings, nil, prompt, "")
}

// RefreshCodedFlowWelcome regenerates the cached welcome for tiqr_ecommerce
// from buyer REST store, store-info, and collections (not MCP).
func (a *App) RefreshCodedFlowWelcome(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}
	if !a.HasPermission(userID, models.ResourceFlowsChatbot, models.ActionWrite, orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Permission denied", nil, "")
	}

	key, _ := r.RequestCtx.UserValue("key").(string)
	key = strings.TrimSpace(key)
	if key != tiqrecommerce.FlowKey {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Welcome refresh is only available for TiQR Ecommerce", nil, "")
	}
	if codedflow.ByKey(key) == nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "TiQR Flow not found", nil, "")
	}

	accountName := strings.TrimSpace(string(r.RequestCtx.QueryArgs().Peek("account")))
	if accountName == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "WhatsApp account is required", nil, "")
	}
	if !a.whatsAppAccountInOrg(orgID, accountName) {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "WhatsApp account not found", nil, "")
	}

	settings, err := a.getChatbotSettingsCached(orgID, accountName)
	if err != nil || settings == nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Chatbot settings not found", nil, "")
	}
	if !commerceRESTConfigured(settings.AI) {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Commerce REST URL and store ID must be configured", nil, "")
	}
	if !settings.AI.Enabled || strings.TrimSpace(string(settings.AI.Provider)) == "" || strings.TrimSpace(settings.AI.APIKey) == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "AI provider must be configured to generate a welcome message", nil, "")
	}

	welcome, generatedAt, err := a.regenerateCodedFlowWelcome(orgID, accountName, settings)
	if err != nil {
		a.Log.Error("Failed to refresh coded flow welcome", "error", err, "org", orgID, "account", accountName)
		return r.SendErrorEnvelope(fasthttp.StatusBadGateway, "Failed to generate welcome message", map[string]any{
			"error": err.Error(),
		}, "")
	}

	return r.SendEnvelope(map[string]any{
		"welcome_message":      welcome,
		"welcome_generated_at": generatedAt,
	})
}

func (a *App) regenerateCodedFlowWelcome(
	orgID uuid.UUID,
	accountName string,
	settings *models.ChatbotSettings,
) (string, string, error) {
	facts, err := a.fetchCodedFlowWelcomeFacts(settings)
	if err != nil {
		return "", "", err
	}
	prompt := composeCodedFlowWelcomePrompt(facts)
	reply, err := generateCodedFlowWelcomeAI(a, settings, prompt)
	if err != nil {
		return "", "", err
	}
	reply = truncateWelcomeRunes(strings.TrimSpace(reply), codedFlowWelcomeMaxChars)
	if reply == "" {
		return "", "", fmt.Errorf("empty welcome response from AI")
	}

	now := time.Now().UTC()
	generatedAt := now.Format(time.RFC3339)
	if err := a.persistCodedFlowWelcome(orgID, accountName, reply, generatedAt); err != nil {
		return "", "", err
	}
	return reply, generatedAt, nil
}

func (a *App) fetchCodedFlowWelcomeFacts(settings *models.ChatbotSettings) (codedFlowWelcomeFacts, error) {
	client := newTiqrStoreRESTClient(settings.AI.CommerceRESTURL)
	storeID := strings.TrimSpace(settings.AI.CommerceStoreID)
	ctx, cancel := context.WithTimeout(context.Background(), codedFlowWelcomeAITimeout)
	defer cancel()

	storeRaw, err := invokeTiqrStoreRESTOperation(ctx, client, "get_store", storeID, "", nil)
	if err != nil {
		return codedFlowWelcomeFacts{}, fmt.Errorf("get_store: %w", err)
	}
	store := tiqrStoreResultMap("get_store", storeRaw)

	var info map[string]any
	infoRaw, infoErr := invokeTiqrStoreRESTOperation(ctx, client, "get_store_info", storeID, "", nil)
	if infoErr == nil {
		info = tiqrStoreResultMap("get_store_info", infoRaw)
	}

	collectionsRaw, err := invokeTiqrStoreRESTOperation(ctx, client, "list_collections", storeID, "", map[string]string{
		"limit": "20",
	})
	if err != nil {
		return codedFlowWelcomeFacts{}, fmt.Errorf("list_collections: %w", err)
	}
	collectionsPage := tiqrStoreResultMap("list_collections", collectionsRaw)
	aliasBuyerListResults(collectionsPage)

	return buildCodedFlowWelcomeFacts(store, info, collectionsPage), nil
}

type codedFlowWelcomeFacts struct {
	Name            string
	Description     string
	AboutUs         string
	Address         string
	DeliveryModes   []string
	FreeDeliveryKm  string
	DeliveryRadius  string
	CollectionNames []string
	MoreCollections int
}

func buildCodedFlowWelcomeFacts(store, info, collectionsPage map[string]any) codedFlowWelcomeFacts {
	facts := codedFlowWelcomeFacts{
		Name:        strings.TrimSpace(asString(store["name"])),
		Description: truncateWelcomeRunes(strings.TrimSpace(asString(store["description"])), 280),
		Address:     strings.TrimSpace(asString(store["address"])),
	}
	if info != nil {
		facts.AboutUs = truncateWelcomeRunes(strings.TrimSpace(asString(info["about_us"])), 280)
	}
	facts.DeliveryModes = deliveryModesFromStore(store)
	if offersDelivery(facts.DeliveryModes) {
		facts.FreeDeliveryKm = strings.TrimSpace(asString(store["free_delivery_radius"]))
		facts.DeliveryRadius = strings.TrimSpace(asString(store["delivery_radius"]))
	}
	facts.CollectionNames, facts.MoreCollections = collectionNamesFromPage(collectionsPage)
	return facts
}

func deliveryModesFromStore(store map[string]any) []string {
	raw, ok := store["delivery_modes"]
	if !ok || raw == nil {
		return nil
	}
	var modes []string
	switch v := raw.(type) {
	case []string:
		modes = append(modes, v...)
	case []any:
		for _, item := range v {
			if s := strings.TrimSpace(asString(item)); s != "" {
				modes = append(modes, s)
			}
		}
	}
	return modes
}

func offersDelivery(modes []string) bool {
	for _, m := range modes {
		if strings.EqualFold(strings.TrimSpace(m), "DELIVERY_TO_LOCATION") {
			return true
		}
	}
	return false
}

func collectionNamesFromPage(page map[string]any) (names []string, more int) {
	if page == nil {
		return nil, 0
	}
	items, ok := anySlice(page["results"])
	if !ok {
		items, ok = anySlice(page["categories"])
	}
	if !ok {
		return nil, 0
	}
	type row struct {
		name     string
		priority int
	}
	rows := make([]row, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok || m == nil {
			continue
		}
		name := strings.TrimSpace(asString(m["name"]))
		if name == "" {
			continue
		}
		rows = append(rows, row{name: name, priority: asToolInt(m["listing_priority"])})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].priority != rows[j].priority {
			return rows[i].priority < rows[j].priority
		}
		return strings.ToLower(rows[i].name) < strings.ToLower(rows[j].name)
	})
	limit := codedFlowWelcomeMaxCollections
	if len(rows) < limit {
		limit = len(rows)
	}
	names = make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		names = append(names, rows[i].name)
	}
	if len(rows) > codedFlowWelcomeMaxCollections {
		more = len(rows) - codedFlowWelcomeMaxCollections
	}
	return names, more
}

func composeCodedFlowWelcomePrompt(facts codedFlowWelcomeFacts) string {
	var b strings.Builder
	b.WriteString(codedFlowWelcomeInstruction)
	b.WriteString("\n\nStore facts:\n")
	writeFact(&b, "name", facts.Name)
	writeFact(&b, "description", facts.Description)
	writeFact(&b, "about_us", facts.AboutUs)
	writeFact(&b, "address", facts.Address)
	if len(facts.DeliveryModes) > 0 {
		writeFact(&b, "delivery_modes", strings.Join(facts.DeliveryModes, ", "))
	}
	writeFact(&b, "free_delivery_radius_km", facts.FreeDeliveryKm)
	writeFact(&b, "delivery_radius_km", facts.DeliveryRadius)
	if len(facts.CollectionNames) > 0 {
		line := strings.Join(facts.CollectionNames, ", ")
		if facts.MoreCollections > 0 {
			line = fmt.Sprintf("%s (+%d more)", line, facts.MoreCollections)
		}
		writeFact(&b, "collections", line)
	}
	return b.String()
}

func writeFact(b *strings.Builder, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString("- ")
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteByte('\n')
}

func (a *App) persistCodedFlowWelcome(orgID uuid.UUID, accountName, welcome, generatedAt string) error {
	var binding models.CodedFlowBinding
	err := a.DB.Where(
		"organization_id = ? AND whats_app_account = ? AND flow_key = ?",
		orgID, accountName, tiqrecommerce.FlowKey,
	).First(&binding).Error
	switch {
	case err == nil:
		settings := models.JSONB{}
		for k, v := range binding.Settings {
			settings[k] = v
		}
		settings[codedSettingWelcomeMessage] = welcome
		settings[codedSettingWelcomeGeneratedAt] = generatedAt
		binding.Settings = settings
		return a.DB.Save(&binding).Error
	case errors.Is(err, gorm.ErrRecordNotFound):
		binding = models.CodedFlowBinding{
			BaseModel:       models.BaseModel{ID: uuid.New()},
			OrganizationID:  orgID,
			WhatsAppAccount: accountName,
			FlowKey:         tiqrecommerce.FlowKey,
			Keywords:        models.StringArray{},
			IsEnabled:       false,
			IsDefault:       false,
			Settings: models.JSONB{
				codedSettingWelcomeMessage:     welcome,
				codedSettingWelcomeGeneratedAt: generatedAt,
			},
		}
		return a.DB.Create(&binding).Error
	default:
		return err
	}
}

// EcommerceWelcomeMessage returns the cached welcome text for tiqr_ecommerce
// on this account, or empty when unset.
func (a *App) EcommerceWelcomeMessage(orgID uuid.UUID, accountName string) string {
	if a == nil || a.DB == nil || accountName == "" {
		return ""
	}
	var binding models.CodedFlowBinding
	err := a.DB.Where(
		"organization_id = ? AND whats_app_account = ? AND flow_key = ?",
		orgID, accountName, tiqrecommerce.FlowKey,
	).First(&binding).Error
	if err != nil || binding.Settings == nil {
		return ""
	}
	msg, _ := binding.Settings[codedSettingWelcomeMessage].(string)
	return strings.TrimSpace(msg)
}

func truncateWelcomeRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max]))
}
