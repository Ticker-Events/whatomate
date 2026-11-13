package handlers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	fs "github.com/shridarpatil/whatomate/pkg/firestore"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zerodha/logf"
)

func TestWriteSessionArchiveTurn_RetriesOnce(t *testing.T) {
	// Serial: mutates package-level append/sync hooks.
	var calls atomic.Int32
	prev := appendSessionArchiveTurnFn
	prevSync := syncContactForArchiveFn
	t.Cleanup(func() {
		appendSessionArchiveTurnFn = prev
		syncContactForArchiveFn = prevSync
	})

	appendSessionArchiveTurnFn = func(
		_ *App,
		_ context.Context,
		_ fs.SessionArchiveDoc,
		_ int,
		_ map[string]any,
	) error {
		n := calls.Add(1)
		if n == 1 {
			return errors.New("transient firestore error")
		}
		return nil
	}
	syncContactForArchiveFn = func(*App, context.Context, *models.Contact, bool) error {
		return nil
	}

	app := &App{Log: logf.New(logf.Opts{Level: logf.ErrorLevel})}
	err := app.writeSessionArchiveTurn(&codedflow.CommittedArchiveTurn{
		Index: 0,
		Turn: codedflow.SessionArchiveTurn{
			Step: "quantity",
			Messages: []codedflow.CodedPreviewMessage{
				{Type: "text", Content: "How many?"},
			},
		},
		Meta: &codedflow.SessionArchive{
			SessionID:      uuid.New().String(),
			ContactID:      uuid.New().String(),
			OrganizationID: uuid.New().String(),
			TurnCount:      1,
			FlowKey:        "tiqr_ecommerce_v2",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
}

func TestWriteSessionArchiveTurn_FailsAfterRetry(t *testing.T) {
	// Serial: mutates package-level append/sync hooks.
	var calls atomic.Int32
	prev := appendSessionArchiveTurnFn
	prevSync := syncContactForArchiveFn
	t.Cleanup(func() {
		appendSessionArchiveTurnFn = prev
		syncContactForArchiveFn = prevSync
	})

	appendSessionArchiveTurnFn = func(
		_ *App,
		_ context.Context,
		_ fs.SessionArchiveDoc,
		_ int,
		_ map[string]any,
	) error {
		calls.Add(1)
		return errors.New("firestore down")
	}
	syncContactForArchiveFn = func(*App, context.Context, *models.Contact, bool) error {
		return nil
	}

	app := &App{Log: logf.New(logf.Opts{Level: logf.ErrorLevel})}
	err := app.writeSessionArchiveTurn(&codedflow.CommittedArchiveTurn{
		Index: 1,
		Turn:  codedflow.SessionArchiveTurn{Step: "products"},
		Meta: &codedflow.SessionArchive{
			SessionID: uuid.New().String(),
			ContactID: uuid.New().String(),
			TurnCount: 2,
		},
	})
	require.Error(t, err)
	assert.Equal(t, int32(2), calls.Load())
}

func TestEnsureContactSyncedForArchive_LoadsContact(t *testing.T) {
	// Serial: mutates package-level sync hook.
	db := testutil.SetupTestDB(t)
	org := testutil.CreateTestOrganization(t, db)
	contact := testutil.CreateTestContact(t, db, org.ID)

	var syncedID uuid.UUID
	prevSync := syncContactForArchiveFn
	t.Cleanup(func() { syncContactForArchiveFn = prevSync })
	syncContactForArchiveFn = func(_ *App, _ context.Context, c *models.Contact, _ bool) error {
		syncedID = c.ID
		return nil
	}

	app := &App{
		DB:  db,
		Log: logf.New(logf.Opts{Level: logf.ErrorLevel}),
	}
	app.ensureContactSyncedForArchive(context.Background(), contact.ID.String())
	assert.Equal(t, contact.ID, syncedID)
}
