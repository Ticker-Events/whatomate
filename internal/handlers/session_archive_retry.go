package handlers

import (
	"context"
	"time"

	"github.com/shridarpatil/whatomate/internal/handlers/codedflow"
	"github.com/shridarpatil/whatomate/internal/models"
	fs "github.com/shridarpatil/whatomate/pkg/firestore"
)

const sessionArchiveRetryInterval = 30 * time.Second

// StartSessionArchiveRetryProcessor retries failed Firestore archive writes.
func (a *App) StartSessionArchiveRetryProcessor(ctx context.Context) {
	if a == nil || a.DB == nil {
		return
	}
	ticker := time.NewTicker(sessionArchiveRetryInterval)
	defer ticker.Stop()
	a.processSessionArchiveRetries(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.processSessionArchiveRetries(ctx)
		}
	}
}

func (a *App) processSessionArchiveRetries(ctx context.Context) {
	if a == nil || a.DB == nil || a.Firestore == nil || !a.Firestore.Enabled() {
		return
	}
	var rows []models.SessionArchiveTurnRetry
	now := time.Now().UTC()
	if err := a.DB.Where("next_attempt_at <= ?", now).
		Order("next_attempt_at asc").
		Limit(25).
		Find(&rows).Error; err != nil {
		a.Log.Error("session archive retry load failed", "error", err)
		return
	}
	for i := range rows {
		if ctx.Err() != nil {
			return
		}
		a.retrySessionArchiveTurn(&rows[i])
	}
}

func (a *App) retrySessionArchiveTurn(row *models.SessionArchiveTurnRetry) {
	if row == nil {
		return
	}
	bundle, err := codedflow.BundleFromMap(map[string]any(row.Payload))
	if err != nil {
		a.Log.Error("session archive retry payload invalid",
			"error", err, "session_id", row.SessionID, "turn_index", row.TurnIndex)
		_ = a.DB.Unscoped().Delete(row).Error
		return
	}
	metaMap, _ := row.Payload["meta"].(map[string]any)
	meta := fs.SessionArchiveDoc{
		Version:        codedflow.AnyToInt(metaMap["version"]),
		FlowKey:        codedflow.AsString(metaMap["flow_key"]),
		SessionID:      row.SessionID.String(),
		ContactID:      row.ContactID.String(),
		OrganizationID: row.OrganizationID.String(),
		StartedAt:      codedflow.AsString(metaMap["started_at"]),
		EndedAt:        codedflow.AsString(metaMap["ended_at"]),
		Status:         codedflow.AsString(metaMap["status"]),
		TurnCount:      codedflow.AnyToInt(metaMap["turn_count"]),
	}
	if meta.Version == 0 {
		meta.Version = 1
	}
	fsBundle := sessionArchiveWriteBundleFromCoded(bundle)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a.ensureContactSyncedForArchive(ctx, meta.ContactID)
	if err := appendSessionArchiveBundleFn(a, ctx, meta, fsBundle); err != nil {
		row.Attempts++
		row.LastError = err.Error()
		row.NextAttemptAt = time.Now().UTC().Add(sessionArchiveRetryInterval)
		if saveErr := a.DB.Save(row).Error; saveErr != nil {
			a.Log.Error("session archive retry save failed",
				"error", saveErr, "session_id", row.SessionID, "turn_index", row.TurnIndex)
		}
		return
	}
	if err := a.DB.Unscoped().Delete(row).Error; err != nil {
		a.Log.Error("session archive retry delete failed",
			"error", err, "session_id", row.SessionID, "turn_index", row.TurnIndex)
	}
}
