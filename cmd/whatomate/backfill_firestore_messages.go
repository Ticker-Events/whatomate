package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/internal/database"
	"github.com/shridarpatil/whatomate/internal/models"
	firestoresync "github.com/shridarpatil/whatomate/pkg/firestore"
	"github.com/zerodha/logf"
)

// runBackfillFirestoreMessages re-writes Postgres messages for one contact into
// Firestore so tiqr.store can read the full thread.
func runBackfillFirestoreMessages(args []string) {
	fs := flag.NewFlagSet("backfill-firestore-messages", flag.ExitOnError)
	configPath := fs.String("config", "config.toml", "Path to config file")
	contactIDStr := fs.String("contact", "", "Contact UUID to backfill (required)")
	dryRun := fs.Bool("dry-run", false, "Count messages without writing to Firestore")
	batchSize := fs.Int("batch", 200, "Messages per DB page")
	_ = fs.Parse(args)

	lo := logf.New(logf.Opts{
		EnableColor:     true,
		Level:           logf.InfoLevel,
		TimestampFormat: "2006-01-02 15:04:05",
		DefaultFields:   []any{"app", "whatomate-backfill-firestore-messages"},
	})

	contactID, err := uuid.Parse(*contactIDStr)
	if err != nil || *contactIDStr == "" {
		fmt.Fprintln(os.Stderr, "Error: -contact <uuid> is required")
		fs.Usage()
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		lo.Fatal("Failed to load config", "error", err)
	}

	db, err := database.NewPostgres(&cfg.Database, cfg.App.Debug)
	if err != nil {
		lo.Fatal("Failed to connect to database", "error", err)
	}

	var contact models.Contact
	if err := db.Where("id = ?", contactID).First(&contact).Error; err != nil {
		lo.Fatal("Contact not found", "contact_id", contactID, "error", err)
	}

	if *dryRun {
		var count int64
		if err := db.Model(&models.Message{}).Where("contact_id = ?", contactID).Count(&count).Error; err != nil {
			lo.Fatal("Failed to count messages", "error", err)
		}
		fmt.Printf("dry-run: contact %s has %d messages (org %s)\n", contact.ID, count, contact.OrganizationID)
		return
	}

	ctx := context.Background()
	fsClient, err := firestoresync.Init(ctx, cfg.Firebase.Credentials, lo)
	if err != nil {
		lo.Fatal("Failed to init Firestore", "error", err)
	}
	if fsClient == nil || !fsClient.Enabled() {
		lo.Fatal("Firebase is not configured; set [firebase].credentials")
	}
	defer fsClient.Close()

	page := *batchSize
	if page < 1 {
		page = 200
	}

	var (
		offset  int
		written int
		failed  int
	)
	for {
		var messages []models.Message
		if err := db.Where("contact_id = ?", contactID).
			Order("created_at ASC").
			Offset(offset).
			Limit(page).
			Find(&messages).Error; err != nil {
			lo.Fatal("Failed to load messages", "error", err)
		}
		if len(messages) == 0 {
			break
		}

		for i := range messages {
			msg := &messages[i]
			writeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := fsClient.SyncMessage(writeCtx, contact.OrganizationID, msg, &contact, false)
			cancel()
			if err != nil {
				failed++
				lo.Error("Failed to sync message", "message_id", msg.ID, "error", err)
				continue
			}
			written++
		}

		offset += len(messages)
		if len(messages) < page {
			break
		}
	}

	fmt.Printf("backfill complete: contact %s written=%d failed=%d\n", contact.ID, written, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
