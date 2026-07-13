package security

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

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

const (
	apiKeyDigestFamilyPrefix = "sha256:"
	apiKeyDigestPrefix       = "sha256:v1:"
	bcryptHashLength         = 60
	apiKeyLastSeenWriteEvery = time.Minute
)

// GenerateAPIKey creates a new API key and returns the full plaintext key,
// a rollback-safe composite credential, and a 12-character prefix for fast DB
// lookups. The credential starts with a valid bcrypt hash for old readers and
// appends a versioned digest for fast verification by current readers. A fast
// digest is appropriate because the server generates a 32-symbol random token;
// it must not be reused for user-chosen, low-entropy secrets.
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
	keyHash = string(hashBytes) + digestAPIKey(fullKey)

	keyPrefix = fullKey[:12]

	return fullKey, keyHash, keyPrefix, nil
}

func digestAPIKey(fullKey string) string {
	digest := sha256.Sum256([]byte(fullKey))
	return apiKeyDigestPrefix + hex.EncodeToString(digest[:])
}

func verifyAPIKeyDigest(storedDigest, fullKey string) error {
	if !strings.HasPrefix(storedDigest, apiKeyDigestPrefix) {
		return fmt.Errorf("unsupported API key digest version")
	}
	encodedDigest := storedDigest[len(apiKeyDigestPrefix):]
	if len(encodedDigest) != hex.EncodedLen(sha256.Size) {
		return fmt.Errorf("invalid API key digest encoding")
	}
	var expected [sha256.Size]byte
	if _, err := hex.Decode(expected[:], []byte(encodedDigest)); err != nil {
		return fmt.Errorf("invalid API key digest encoding")
	}
	candidate := sha256.Sum256([]byte(fullKey))
	if subtle.ConstantTimeCompare(expected[:], candidate[:]) != 1 {
		return fmt.Errorf("API key digest mismatch")
	}
	return nil
}

// verifyAPIKeyCredential returns a replacement credential only when a legacy
// bcrypt credential was successfully verified. Current composite credentials
// retain a valid bcrypt hash in their first 60 bytes for rollback compatibility
// while using the appended digest on the connection hot path. Unknown SHA-256
// versions fail closed instead of falling through to the legacy verifier.
func verifyAPIKeyCredential(storedHash, fullKey string) (replacementHash string, err error) {
	if strings.HasPrefix(storedHash, apiKeyDigestFamilyPrefix) {
		return "", verifyAPIKeyDigest(storedHash, fullKey)
	}

	if len(storedHash) > bcryptHashLength {
		bcryptHash := storedHash[:bcryptHashLength]
		if _, err := bcrypt.Cost([]byte(bcryptHash)); err != nil {
			return "", fmt.Errorf("invalid composite API key credential: %w", err)
		}
		digest := storedHash[bcryptHashLength:]
		if !strings.HasPrefix(digest, apiKeyDigestFamilyPrefix) {
			return "", fmt.Errorf("invalid composite API key credential")
		}
		return "", verifyAPIKeyDigest(digest, fullKey)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(fullKey)); err != nil {
		return "", err
	}
	return storedHash + digestAPIKey(fullKey), nil
}

type apiKeyLookupFunc func(context.Context, string) (*db.ApiKey, *db.Bot, error)
type apiKeyAuthRecordFunc func(context.Context, string, string) error

// VerifyAPIKey validates a full API key against the database. It looks up the
// key by its 12-character prefix, verifies its stored credential, and returns
// the associated bot. Current credentials update last_seen only when the joined
// timestamp is missing or at least one minute old. A successfully verified
// legacy bcrypt credential is upgraded to the rollback-safe composite and
// records last_seen immediately in the same DB write.
func VerifyAPIKey(ctx context.Context, fullKey string) (*db.Bot, error) {
	return verifyAPIKey(ctx, fullKey, db.GetAPIKeyAndBotByPrefix, recordAPIKeyAuthentication)
}

func verifyAPIKey(
	ctx context.Context,
	fullKey string,
	lookup apiKeyLookupFunc,
	record apiKeyAuthRecordFunc,
) (*db.Bot, error) {
	if len(fullKey) < 12 {
		return nil, fmt.Errorf("invalid API key: too short")
	}

	prefix := fullKey[:12]

	apiKey, bot, err := lookup(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("API key lookup failed: %w", err)
	}
	if apiKey == nil {
		return nil, fmt.Errorf("API key not found")
	}

	replacementHash, err := verifyAPIKeyCredential(apiKey.KeyHash, fullKey)
	if err != nil {
		return nil, fmt.Errorf("invalid API key: %w", err)
	}

	if shouldRecordAPIKeyAuthentication(apiKey.LastSeen, replacementHash, time.Now()) {
		if err := record(ctx, apiKey.ID, replacementHash); err != nil {
			slog.Warn("failed to record API key authentication", "error", err, "key_id", apiKey.ID)
		}
	}

	if bot == nil {
		return nil, fmt.Errorf("no bot associated with API key")
	}

	return bot, nil
}

func shouldRecordAPIKeyAuthentication(lastSeen *time.Time, replacementHash string, now time.Time) bool {
	if replacementHash != "" || lastSeen == nil {
		return true
	}
	return !lastSeen.After(now.Add(-apiKeyLastSeenWriteEvery))
}

func recordAPIKeyAuthentication(ctx context.Context, keyID, replacementHash string) error {
	if replacementHash != "" {
		return db.UpdateAPIKeyHashAndLastSeen(ctx, keyID, replacementHash)
	}
	return db.UpdateAPIKeyLastSeen(ctx, keyID)
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

// WithBotContext attaches an already-authenticated bot to a context. Runtime
// code should normally rely on AuthMiddleware; this helper keeps downstream
// handlers testable without weakening the private context-key boundary.
func WithBotContext(ctx context.Context, bot *db.Bot) context.Context {
	return context.WithValue(ctx, botContextKey, bot)
}
