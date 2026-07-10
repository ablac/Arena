package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/google/uuid"
)

// GenerateKey handles POST /api/v1/keys/generate.
// It rate-limits key generation per IP, creates a new API key and a default
// bot record, and returns the plaintext key to the caller.
func GenerateKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := security.ExtractClientIP(r)

	// API-key authentication is database-backed. Returning a plaintext key in
	// database-optional mode would create a credential that can never pass the
	// next authenticated request or WebSocket connection.
	if db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "key registration requires the database")
		return
	}

	// Rate-limit key registration per IP.
	allowed, _, err := db.CheckRateLimit(ctx, ip, config.C.RateLimitRegisterPerHour)
	if err != nil {
		slog.Error("rate limit check failed", "error", err)
		// Registration writes to this same database moments later. Failing
		// open here only bypasses abuse controls and cannot provide a reliable
		// degraded registration path.
		writeError(w, http.StatusServiceUnavailable, "key registration is temporarily unavailable")
		return
	} else if !allowed {
		writeStructuredError(w, GlobalEventBus, http.StatusTooManyRequests,
			"rate limit exceeded for key generation", "RATE_LIMITED",
			map[string]interface{}{
				"limit":       config.C.RateLimitRegisterPerHour,
				"window":      "1h",
				"retry_after": 3600,
			})
		return
	}

	// Generate API key material.
	fullKey, keyHash, keyPrefix, err := security.GenerateAPIKey()
	if err != nil {
		slog.Error("failed to generate API key", "error", err)
		writeStructuredError(w, GlobalEventBus, http.StatusInternalServerError,
			"failed to generate API key", "KEY_GEN_ERROR", nil)
		return
	}

	keyID := uuid.New().String()
	botID := uuid.New().String()
	now := time.Now()

	// Persist the API key and its bot atomically. A bot insert failure must not
	// leave an active credential with no associated bot.
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
	if err := db.CreateAPIKeyAndBot(ctx, keyID, keyHash, keyPrefix, ip, bot); err != nil {
		slog.Error("failed to create API key and bot", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create API key")
		return
	}

	resp := KeyGenerateResponse{
		APIKey:    fullKey,
		BotID:     botID,
		CreatedAt: now,
		Message:   "API key generated successfully. Store it safely -- it cannot be recovered.",
	}
	writeJSON(w, http.StatusCreated, resp)
}

type botSessionRevoker interface {
	KickBot(botID, reason string) bool
}

type deactivateAPIKeyFunc func(context.Context, string) error

// RevokeKey handles DELETE /api/v1/keys/revoke. Database deactivation happens
// before the live session is removed, so a reconnect cannot race the kick with
// an active credential.
func RevokeKey(engine *game.GameEngine) http.HandlerFunc {
	return revokeKeyHandler(engine, db.DeactivateAPIKey)
}

func revokeKeyHandler(sessions botSessionRevoker, deactivate deactivateAPIKeyFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bot := security.GetBotFromContext(r.Context())
		if bot == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		if err := deactivate(r.Context(), bot.APIKeyID); err != nil {
			slog.Error("failed to deactivate API key", "error", err, "key_id", bot.APIKeyID)
			writeError(w, http.StatusInternalServerError, "failed to revoke key")
			return
		}

		if sessions != nil {
			sessions.KickBot(bot.ID, "API key revoked")
		}

		writeJSON(w, http.StatusOK, KeyRevokeResponse{
			Message: "API key revoked successfully",
		})
	}
}
