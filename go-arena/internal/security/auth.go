package security

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"

	"arena-server/internal/config"
	"arena-server/internal/db"

	"golang.org/x/crypto/bcrypt"
)

// contextKey is an unexported type used for context keys in this package,
// preventing collisions with keys defined in other packages.
type contextKey string

const botContextKey contextKey = "bot"

// base62Chars is the character set used for API key generation.
const base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// GenerateAPIKey creates a new API key and returns the full plaintext key,
// a bcrypt hash of the key, and a 12-character prefix for fast DB lookups.
func GenerateAPIKey() (fullKey string, keyHash string, keyPrefix string, err error) {
	// Generate 32 random bytes and encode them as base62.
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	encoded := make([]byte, 32)
	base := big.NewInt(int64(len(base62Chars)))
	for i := range encoded {
		idx := new(big.Int).SetUint64(uint64(raw[i]))
		idx.Mod(idx, base)
		encoded[i] = base62Chars[idx.Int64()]
	}

	fullKey = config.C.APIKeyPrefix + string(encoded)

	hashBytes, err := bcrypt.GenerateFromPassword([]byte(fullKey), config.C.BcryptRounds)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to hash API key: %w", err)
	}
	keyHash = string(hashBytes)

	keyPrefix = fullKey[:12]

	return fullKey, keyHash, keyPrefix, nil
}

// VerifyAPIKey validates a full API key against the database. It looks up the
// key by its 12-character prefix, compares the bcrypt hash, updates last_seen,
// and returns the associated bot.
func VerifyAPIKey(ctx context.Context, fullKey string) (*db.Bot, error) {
	if len(fullKey) < 12 {
		return nil, fmt.Errorf("invalid API key: too short")
	}

	prefix := fullKey[:12]

	apiKey, err := db.GetAPIKeyByPrefix(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("API key lookup failed: %w", err)
	}
	if apiKey == nil {
		return nil, fmt.Errorf("API key not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(apiKey.KeyHash), []byte(fullKey)); err != nil {
		return nil, fmt.Errorf("invalid API key: %w", err)
	}

	if err := db.UpdateAPIKeyLastSeen(ctx, apiKey.ID); err != nil {
		slog.Warn("failed to update API key last_seen", "error", err, "key_id", apiKey.ID)
	}

	bot, err := db.GetBotByAPIKeyID(ctx, apiKey.ID)
	if err != nil {
		return nil, fmt.Errorf("bot lookup failed: %w", err)
	}
	if bot == nil {
		return nil, fmt.Errorf("no bot associated with API key")
	}

	return bot, nil
}

// AuthMiddleware is an HTTP middleware that extracts the API key from the
// X-Arena-Key header, verifies it, and stores the associated bot in the
// request context.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-Arena-Key")
		if apiKey == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "missing X-Arena-Key header",
				"code":    "MISSING_API_KEY",
				"details": map[string]interface{}{"header": "X-Arena-Key"},
			})
			return
		}

		bot, err := VerifyAPIKey(r.Context(), apiKey)
		if err != nil {
			slog.Warn("auth failed", "error", err, "remote", r.RemoteAddr)
			code := "INVALID_API_KEY"
			msg := "invalid API key"
			if strings.Contains(err.Error(), "too short") {
				code = "API_KEY_TOO_SHORT"
				msg = "API key is too short"
			} else if strings.Contains(err.Error(), "not found") {
				code = "API_KEY_NOT_FOUND"
				msg = "API key not found"
			} else if strings.Contains(err.Error(), "no bot associated") {
				code = "NO_BOT_FOR_KEY"
				msg = "no bot associated with this API key"
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": msg,
				"code":  code,
			})
			return
		}

		ctx := context.WithValue(r.Context(), botContextKey, bot)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetBotFromContext retrieves the authenticated bot from the request context.
// Returns nil if no bot is present (i.e. the request did not pass through
// AuthMiddleware).
func GetBotFromContext(ctx context.Context) *db.Bot {
	bot, _ := ctx.Value(botContextKey).(*db.Bot)
	return bot
}
