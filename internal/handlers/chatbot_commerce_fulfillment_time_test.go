package handlers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseFulfillmentTimeText(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+30*60)
	now := time.Date(2026, 3, 10, 14, 0, 0, 0, loc)

	t.Run("relative minutes", func(t *testing.T) {
		got, err := parseFulfillmentTimeText("in 45 minutes", now, loc, "")
		require.NoError(t, err)
		require.Equal(t, time.Date(2026, 3, 10, 14, 45, 0, 0, loc), got)
	})

	t.Run("relative hours", func(t *testing.T) {
		got, err := parseFulfillmentTimeText("after 2 hours", now, loc, "")
		require.NoError(t, err)
		require.Equal(t, time.Date(2026, 3, 10, 16, 0, 0, 0, loc), got)
	})

	t.Run("asap uses earliest", func(t *testing.T) {
		earliest := time.Date(2026, 3, 10, 15, 30, 0, 0, loc).Format(time.RFC3339)
		got, err := parseFulfillmentTimeText("asap", now, loc, earliest)
		require.NoError(t, err)
		require.Equal(t, time.Date(2026, 3, 10, 15, 30, 0, 0, loc), got)
	})

	t.Run("today absolute", func(t *testing.T) {
		got, err := parseFulfillmentTimeText("today 5:30pm", now, loc, "")
		require.NoError(t, err)
		require.Equal(t, time.Date(2026, 3, 10, 17, 30, 0, 0, loc), got)
	})

	t.Run("bare past clock rolls tomorrow", func(t *testing.T) {
		got, err := parseFulfillmentTimeText("10am", now, loc, "")
		require.NoError(t, err)
		require.Equal(t, time.Date(2026, 3, 11, 10, 0, 0, 0, loc), got)
	})

	t.Run("tomorrow", func(t *testing.T) {
		got, err := parseFulfillmentTimeText("tomorrow 10:15", now, loc, "")
		require.NoError(t, err)
		require.Equal(t, time.Date(2026, 3, 11, 10, 15, 0, 0, loc), got)
	})
}
