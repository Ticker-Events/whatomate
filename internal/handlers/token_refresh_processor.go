package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
)

const (
	tokenRefreshLockKey = "whatomate:token_refresh:lock"
	tokenRefreshLockTTL = 5 * time.Minute
	tokenRefreshErrMax  = 500
)

// TokenRefreshProcessor periodically refreshes Meta WhatsApp long-lived tokens
// before they expire, and flags failures for the dashboard warning.
type TokenRefreshProcessor struct {
	app      *App
	interval time.Duration
	stopCh   chan struct{}
	lockID   string
}

// NewTokenRefreshProcessor creates a processor with the given tick interval.
func NewTokenRefreshProcessor(app *App, interval time.Duration) *TokenRefreshProcessor {
	return &TokenRefreshProcessor{
		app:      app,
		interval: interval,
		stopCh:   make(chan struct{}),
		lockID:   uuid.New().String(),
	}
}

// Start begins the token refresh loop.
func (p *TokenRefreshProcessor) Start(ctx context.Context) {
	p.app.Log.Info("Token refresh processor started",
		"interval", p.interval,
		"days_before", p.app.Config.WhatsApp.TokenRefreshDaysBeforeOrDefault(),
		"lock_id", p.lockID)

	// Run once shortly after start so expiry is discovered without waiting a full tick.
	if p.tryAcquireOrRenewLock(ctx) {
		p.processDueAccounts(ctx)
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.app.Log.Info("Token refresh processor stopped by context")
			p.releaseLock()
			return
		case <-p.stopCh:
			p.app.Log.Info("Token refresh processor stopped")
			p.releaseLock()
			return
		case <-ticker.C:
			if !p.tryAcquireOrRenewLock(ctx) {
				p.app.Log.Debug("Token refresh processor skipped; another instance holds the lock")
				continue
			}
			p.processDueAccounts(ctx)
		}
	}
}

// Stop stops the processor.
func (p *TokenRefreshProcessor) Stop() {
	select {
	case <-p.stopCh:
	default:
		close(p.stopCh)
	}
}

// RunOnce processes due accounts once (for tests / manual ops).
func (p *TokenRefreshProcessor) RunOnce(ctx context.Context) {
	p.processDueAccounts(ctx)
}

func (p *TokenRefreshProcessor) tryAcquireOrRenewLock(ctx context.Context) bool {
	if p.app.Redis == nil {
		return true
	}

	ok, err := p.app.Redis.SetNX(ctx, tokenRefreshLockKey, p.lockID, tokenRefreshLockTTL).Result()
	if err != nil {
		p.app.Log.Error("Failed to acquire token refresh lock", "error", err)
		return false
	}
	if ok {
		return true
	}

	current, err := p.app.Redis.Get(ctx, tokenRefreshLockKey).Result()
	if err == redis.Nil {
		ok, err = p.app.Redis.SetNX(ctx, tokenRefreshLockKey, p.lockID, tokenRefreshLockTTL).Result()
		if err != nil {
			p.app.Log.Error("Failed to re-acquire token refresh lock", "error", err)
			return false
		}
		return ok
	}
	if err != nil {
		p.app.Log.Error("Failed to read token refresh lock", "error", err)
		return false
	}
	if current != p.lockID {
		return false
	}

	if err := p.app.Redis.Expire(ctx, tokenRefreshLockKey, tokenRefreshLockTTL).Err(); err != nil {
		p.app.Log.Error("Failed to renew token refresh lock", "error", err)
		return false
	}
	return true
}

func (p *TokenRefreshProcessor) releaseLock() {
	if p.app.Redis == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	current, err := p.app.Redis.Get(ctx, tokenRefreshLockKey).Result()
	if err != nil || current != p.lockID {
		return
	}
	_ = p.app.Redis.Del(ctx, tokenRefreshLockKey).Err()
}

func (p *TokenRefreshProcessor) processDueAccounts(ctx context.Context) {
	daysBefore := p.app.Config.WhatsApp.TokenRefreshDaysBeforeOrDefault()
	deadline := time.Now().UTC().Add(time.Duration(daysBefore) * 24 * time.Hour)

	var accounts []models.WhatsAppAccount
	// Meta accounts only: missing expiry (backfill) or due for refresh.
	err := p.app.DB.Where(
		"(provider = '' OR provider = 'meta') AND access_token <> '' AND (token_expires_at IS NULL OR token_expires_at <= ?)",
		deadline,
	).Find(&accounts).Error
	if err != nil {
		p.app.Log.Error("Failed to list accounts for token refresh", "error", err)
		return
	}

	for i := range accounts {
		if ctx.Err() != nil {
			return
		}
		p.refreshOne(ctx, &accounts[i], deadline)
	}
}

func (p *TokenRefreshProcessor) refreshOne(ctx context.Context, account *models.WhatsAppAccount, deadline time.Time) {
	encKey := p.app.Config.App.EncryptionKey
	plaintext := account.AccessToken
	if crypto.IsEncrypted(plaintext) {
		dec, err := crypto.Decrypt(plaintext, encKey)
		if err != nil {
			p.markRefreshFailed(account, fmt.Sprintf("decrypt token: %v", err))
			return
		}
		plaintext = dec
	}
	if plaintext == "" {
		return
	}

	appID, appSecret, err := p.resolveAppCreds(account)
	if err != nil || appID == "" || appSecret == "" {
		p.markRefreshFailed(account, "missing Meta app_id/app_secret for token refresh")
		return
	}

	apiVersion := account.APIVersion
	if apiVersion == "" {
		apiVersion = p.app.defaultAPIVersion()
	}

	// Backfill expiry via debug_token when unknown.
	if account.TokenExpiresAt == nil {
		appAccessToken := fmt.Sprintf("%s|%s", appID, appSecret)
		info, dbgErr := p.app.WhatsApp.GetTokenDebugInfo(ctx, plaintext, appAccessToken)
		if dbgErr != nil {
			p.app.Log.Warn("Token debug_token backfill failed",
				"account_id", account.ID, "error", dbgErr)
			// Still attempt refresh — Meta may accept fb_exchange_token.
		} else {
			expiresAt := whatsapp.ExpiresAtFromUnix(info.ExpiresAt)
			if expiresAt == nil {
				// Never-expiring / unknown — nothing to refresh.
				p.app.Log.Info("Token has no expiry; skipping refresh",
					"account_id", account.ID, "phone_id", account.PhoneID)
				_ = p.app.DB.Model(account).Updates(map[string]any{
					"token_refresh_failed": false,
					"token_refresh_error":  "",
				}).Error
				return
			}
			account.TokenExpiresAt = expiresAt
			if err := p.app.DB.Model(account).Update("token_expires_at", expiresAt).Error; err != nil {
				p.app.Log.Error("Failed to save token_expires_at", "error", err, "account_id", account.ID)
			}
			if expiresAt.After(deadline) {
				return // not due yet
			}
		}
	}

	newToken, expiresIn, refreshErr := p.app.WhatsApp.RefreshLongLivedToken(ctx, plaintext, appID, appSecret, apiVersion)
	if refreshErr != nil {
		p.app.Log.Error("Meta token refresh failed",
			"account_id", account.ID, "phone_id", account.PhoneID, "error", refreshErr)
		p.markRefreshFailed(account, refreshErr.Error())
		return
	}

	now := time.Now().UTC()
	expiresAt := whatsapp.ExpiresAtFromExpiresIn(expiresIn, now)
	if expiresAt == nil {
		// Refresh succeeded but no expires_in — try debug_token.
		appAccessToken := fmt.Sprintf("%s|%s", appID, appSecret)
		if info, dbgErr := p.app.WhatsApp.GetTokenDebugInfo(ctx, newToken, appAccessToken); dbgErr == nil {
			expiresAt = whatsapp.ExpiresAtFromUnix(info.ExpiresAt)
		}
	}

	enc, encErr := crypto.Encrypt(newToken, encKey)
	if encErr != nil {
		p.markRefreshFailed(account, fmt.Sprintf("encrypt refreshed token: %v", encErr))
		return
	}

	updates := map[string]any{
		"access_token":            enc,
		"token_expires_at":        expiresAt,
		"token_last_refreshed_at": now,
		"token_refresh_failed":    false,
		"token_refresh_error":     "",
	}
	if err := p.app.DB.Model(account).Updates(updates).Error; err != nil {
		p.app.Log.Error("Failed to save refreshed token", "error", err, "account_id", account.ID)
		p.markRefreshFailed(account, "failed to persist refreshed token")
		return
	}

	p.app.InvalidateWhatsAppAccountCache(account.PhoneID)
	p.app.Log.Info("Meta access token refreshed",
		"account_id", account.ID, "phone_id", account.PhoneID, "expires_at", expiresAt)
}

func (p *TokenRefreshProcessor) resolveAppCreds(account *models.WhatsAppAccount) (string, string, error) {
	appID, appSecret, _, err := p.app.resolveMetaAppCreds(account.OrganizationID)
	if err != nil {
		return "", "", err
	}
	// Prefer account-level AppID when set; still use org/global secret.
	if account.AppID != "" {
		appID = account.AppID
	}
	if account.AppSecret != "" {
		sec := account.AppSecret
		if crypto.IsEncrypted(sec) {
			dec, decErr := crypto.Decrypt(sec, p.app.Config.App.EncryptionKey)
			if decErr == nil && dec != "" {
				appSecret = dec
			}
		} else {
			appSecret = sec
		}
	}
	return appID, appSecret, nil
}

func (p *TokenRefreshProcessor) markRefreshFailed(account *models.WhatsAppAccount, errMsg string) {
	errMsg = truncateTokenRefreshError(errMsg)
	if err := p.app.DB.Model(account).Updates(map[string]any{
		"token_refresh_failed": true,
		"token_refresh_error":  errMsg,
	}).Error; err != nil {
		p.app.Log.Error("Failed to mark token refresh failure", "error", err, "account_id", account.ID)
	}
}

func truncateTokenRefreshError(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) <= tokenRefreshErrMax {
		return msg
	}
	return msg[:tokenRefreshErrMax]
}
