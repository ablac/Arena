package api

import (
	"log/slog"
	"net/http"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/security"

	"github.com/google/uuid"
)

// GenerateKey handles POST /api/v1/keys/generate.
// It rate-limits key generation per IP, creates a new API key and a default
// bot record, and returns the plaintext key to the caller.
func GenerateKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := security.ExtractClientIP(r)

	// Rate-limit key registration per IP.
	if db.Pool != nil {
		allowed, _, err := db.CheckRateLimit(ctx, ip, config.C.RateLimitRegisterPerHour)
		if err != nil {
			slog.Error("rate limit check failed", "error", err)
			// Allow the request on error so the service degrades gracefully.
		} else if !allowed {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded for key generation")
			return
		}
	} else {
		// Fall back to Redis-based rate limiting when DB is unavailable.
		allowed, _, _, err := security.CheckRateLimit(ctx, "register:"+ip, config.C.RateLimitRegisterPerHour, 3600)
		if err != nil {
			slog.Warn("redis rate limit check failed", "error", err)
		} else if !allowed {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded for key generation")
			return
		}
	}

	// Generate API key material.
	fullKey, keyHash, keyPrefix, err := security.GenerateAPIKey()
	if err != nil {
		slog.Error("failed to generate API key", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate API key")
		return
	}

	keyID := uuid.New().String()
	botID := uuid.New().String()
	now := time.Now()

	// Persist the API key.
	if db.Pool != nil {
		if err := db.CreateAPIKey(ctx, keyID, keyHash, keyPrefix, ip); err != nil {
			slog.Error("failed to create API key", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create API key")
			return
		}

		// Create a default bot associated with the key.
		bot := &db.Bot{
			ID:              botID,
			APIKeyID:        keyID,
			Name:            "Unnamed Bot",
			AvatarColor:     "#888888",
			DefaultWeapon:   "sword",
			DefaultStats:    db.JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
			DefaultFallback: "aggressive",
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := db.CreateBot(ctx, bot); err != nil {
			slog.Error("failed to create bot", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create bot")
			return
		}
	}

	resp := KeyGenerateResponse{
		APIKey:    fullKey,
		BotID:     botID,
		CreatedAt: now,
		Message:   "API key generated successfully. Store it safely -- it cannot be recovered.",
	}
	writeJSON(w, http.StatusCreated, resp)
}

// RevokeKey handles DELETE /api/v1/keys/revoke.
// It deactivates the API key associated with the authenticated bot.
func RevokeKey(w http.ResponseWriter, r *http.Request) {
	bot := security.GetBotFromContext(r.Context())
	if bot == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if db.Pool != nil {
		if err := db.DeactivateAPIKey(r.Context(), bot.APIKeyID); err != nil {
			slog.Error("failed to deactivate API key", "error", err, "key_id", bot.APIKeyID)
			writeError(w, http.StatusInternalServerError, "failed to revoke key")
			return
		}
	}

	writeJSON(w, http.StatusOK, KeyRevokeResponse{
		Message: "API key revoked successfully",
	})
}

