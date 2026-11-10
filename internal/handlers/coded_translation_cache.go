package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	codedTranslationPromptVersion = "v1"
	codedTranslationCachePrefix   = "coded:tr:"
	codedTranslationCacheTTL      = 30 * 24 * time.Hour
)

// GetCodedTranslation returns a Redis-cached translation for lang+source.
// ok is false when Redis is unset or the key is missing.
func (a *App) GetCodedTranslation(lang, source string) (string, bool) {
	if a == nil || a.Redis == nil {
		return "", false
	}
	lang = strings.TrimSpace(lang)
	source = strings.TrimSpace(source)
	if lang == "" || source == "" {
		return "", false
	}
	ctx := context.Background()
	val, err := a.Redis.Get(ctx, codedTranslationCacheKey(lang, source)).Result()
	if err != nil || strings.TrimSpace(val) == "" {
		return "", false
	}
	return val, true
}

// SetCodedTranslation stores a successful translation in Redis.
func (a *App) SetCodedTranslation(lang, source, translated string) {
	if a == nil || a.Redis == nil {
		return
	}
	lang = strings.TrimSpace(lang)
	source = strings.TrimSpace(source)
	translated = strings.TrimSpace(translated)
	if lang == "" || source == "" || translated == "" {
		return
	}
	ctx := context.Background()
	_ = a.Redis.Set(ctx, codedTranslationCacheKey(lang, source), translated, codedTranslationCacheTTL).Err()
}

func codedTranslationCacheKey(lang, source string) string {
	sum := sha256.Sum256([]byte(source))
	return fmt.Sprintf("%s%s:%s:%s",
		codedTranslationCachePrefix,
		codedTranslationPromptVersion,
		strings.ToLower(lang),
		hex.EncodeToString(sum[:]),
	)
}
