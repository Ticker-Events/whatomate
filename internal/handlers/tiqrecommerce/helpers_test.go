package tiqrecommerce

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrencySymbol(t *testing.T) {
	assert.Equal(t, "₹", currencySymbol("INR"))
	assert.Equal(t, "$", currencySymbol("USD"))
	assert.Equal(t, "€", currencySymbol("EUR"))
	assert.Equal(t, "£", currencySymbol("GBP"))
	assert.Equal(t, "AED ", currencySymbol("AED"))
}

func TestStashSessionCurrencySetsSymbol(t *testing.T) {
	session := &models.ChatbotSession{}
	stashSessionCurrency(session, "USD")
	assert.Equal(t, "USD", session.SessionData["currency"])
	assert.Equal(t, "$", session.SessionData["currency_symbol"])

	stashSessionCurrency(session, "")
	assert.Equal(t, "INR", session.SessionData["currency"])
	assert.Equal(t, "₹", session.SessionData["currency_symbol"])
}

func TestEnrichOptionsForList_PriceOnce(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"currency":        "INR",
		"currency_symbol": "₹",
	}}

	short := enrichOptionsForList(session, []any{
		map[string]any{"id": "1", "name": "Regular", "price": "120", "description": "Crispy"},
	})
	require.Len(t, short, 1)
	row, _ := asStringMap(short[0])
	assert.Equal(t, "Regular (₹120)", row["row_title"])
	assert.Equal(t, "Crispy", row["row_description"])
	assert.NotContains(t, asString(row["row_description"]), "₹120")

	longName := "Extra Long Option Name That Exceeds Limit"
	long := enrichOptionsForList(session, []any{
		map[string]any{"id": "2", "name": longName, "price": "120", "description": "Crispy pastry"},
	})
	require.Len(t, long, 1)
	row, _ = asStringMap(long[0])
	assert.Equal(t, longName, row["row_title"])
	assert.Greater(t, utf8.RuneCountInString(pricedName(longName, "₹", "120")), listTitleMaxRunes)
	assert.Equal(t, "₹120 — Crispy pastry", row["row_description"])
	assert.NotContains(t, asString(row["row_title"]), "₹120")
}

func TestEnrichProductsForCarousel_PriceOnce(t *testing.T) {
	session := &models.ChatbotSession{SessionData: models.JSONB{
		"currency":        "INR",
		"currency_symbol": "₹",
	}}

	short := enrichProductsForCarousel(session, []any{
		map[string]any{"id": "1", "name": "Kunafa", "min_price": "120", "description": "Pastry"},
	})
	require.Len(t, short, 1)
	row, _ := asStringMap(short[0])
	assert.Equal(t, "Kunafa (₹120)\nPastry", row["card_body"])
	assert.Equal(t, 1, strings.Count(asString(row["card_body"]), "₹120"))

	longName := strings.Repeat("Chocolate Celebration Cake Layer ", 8)
	long := enrichProductsForCarousel(session, []any{
		map[string]any{"id": "2", "name": longName, "min_price": "499", "description": "Rich chocolate"},
	})
	require.Len(t, long, 1)
	row, _ = asStringMap(long[0])
	body := asString(row["card_body"])
	assert.Contains(t, body, "\n₹499 — Rich chocolate")
	assert.Equal(t, 1, strings.Count(body, "₹499"))
	assert.LessOrEqual(t, utf8.RuneCountInString(body), carouselBodyMaxRunes)
	assert.NotContains(t, strings.Split(body, "\n")[0], "₹499")
	assert.Greater(t, utf8.RuneCountInString(pricedName(longName, "₹", "499")), carouselBodyMaxRunes)
}
