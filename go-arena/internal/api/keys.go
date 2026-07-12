package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type accountAPIKeyStore interface {
	Capacity(context.Context, string) (int, int, error)
	ConsumeQuota(context.Context, string, db.AccountAPIKeyQuotaAction, int) (bool, int, error)
	List(context.Context, string) ([]db.AccountAPIKey, int, error)
	Create(context.Context, string, string, string, string, string, *db.Bot) (*db.AccountAPIKey, int, error)
	Deactivate(context.Context, string, string) (*db.AccountAPIKey, int, error)
}

type databaseAccountAPIKeyStore struct{}

func (databaseAccountAPIKeyStore) Capacity(ctx context.Context, accountID string) (int, int, error) {
	return db.GetAccountAPIKeyCapacity(ctx, accountID)
}

func (databaseAccountAPIKeyStore) ConsumeQuota(ctx context.Context, accountID string, action db.AccountAPIKeyQuotaAction, limit int) (bool, int, error) {
	return db.ConsumeAccountAPIKeyQuota(ctx, accountID, action, limit)
}

func (databaseAccountAPIKeyStore) List(ctx context.Context, accountID string) ([]db.AccountAPIKey, int, error) {
	return db.ListAccountAPIKeys(ctx, accountID)
}

func (databaseAccountAPIKeyStore) Create(ctx context.Context, accountID, keyID, keyHash, keyPrefix, ip string, bot *db.Bot) (*db.AccountAPIKey, int, error) {
	return db.CreateAccountAPIKeyAndBot(ctx, accountID, keyID, keyHash, keyPrefix, ip, bot)
}

func (databaseAccountAPIKeyStore) Deactivate(ctx context.Context, accountID, keyID string) (*db.AccountAPIKey, int, error) {
	return db.DeactivateAccountAPIKey(ctx, accountID, keyID)
}

type generateAPIKeyFunc func() (string, string, string, error)

type AccountKeysHandler struct {
	store    accountAPIKeyStore
	sessions botSessionRevoker
	generate generateAPIKeyFunc
}

func NewAccountKeysHandler(engine *game.GameEngine) *AccountKeysHandler {
	return newAccountKeysHandler(databaseAccountAPIKeyStore{}, engine, security.GenerateAPIKey)
}

func newAccountKeysHandler(store accountAPIKeyStore, sessions botSessionRevoker, generate generateAPIKeyFunc) *AccountKeysHandler {
	return &AccountKeysHandler{store: store, sessions: sessions, generate: generate}
}

func verifiedAccountKeySession(w http.ResponseWriter, r *http.Request) (*CustomerSession, bool) {
	session, ok := customerSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "customer authentication required")
		return nil, false
	}
	if session.EmailVerifiedAt == nil {
		writeError(w, http.StatusForbidden, "a verified customer email is required")
		return nil, false
	}
	return session, true
}

func (h *AccountKeysHandler) List(w http.ResponseWriter, r *http.Request) {
	session, ok := verifiedAccountKeySession(w, r)
	if !ok {
		return
	}
	keys, activeCount, err := h.store.List(r.Context(), session.AccountID)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "account API keys are unavailable")
			return
		}
		slog.Error("failed to list account API keys", "error", err, "account_id", session.AccountID)
		writeError(w, http.StatusInternalServerError, "failed to list account API keys")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"keys": keys, "active_count": activeCount, "limit": db.MaxActiveAccountAPIKeys,
	})
}

type accountKeyCreateRequest struct {
	BotName string `json:"bot_name"`
}

func (h *AccountKeysHandler) Create(w http.ResponseWriter, r *http.Request) {
	session, ok := verifiedAccountKeySession(w, r)
	if !ok {
		return
	}
	if db.Pool == nil && isDatabaseAccountAPIKeyStore(h.store) {
		writeError(w, http.StatusServiceUnavailable, "key registration requires the database")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var request accountKeyCreateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	botName := security.SanitizeBotName(request.BotName)

	ctx := r.Context()
	ip := security.ExtractClientIP(r)
	activeCount, totalCount, err := h.store.Capacity(ctx, session.AccountID)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "account API key capacity is unavailable")
			return
		}
		slog.Error("failed to preflight account API key capacity", "error", err, "account_id", session.AccountID)
		writeError(w, http.StatusInternalServerError, "failed to check account API key capacity")
		return
	}
	if activeCount >= db.MaxActiveAccountAPIKeys {
		writeAccountAPIKeyActiveLimit(w, activeCount)
		return
	}
	if totalCount >= db.MaxAccountAPIKeyHistory {
		writeAccountAPIKeyHistoryLimit(w, totalCount)
		return
	}

	allowed, remaining, err := h.store.ConsumeQuota(ctx, session.AccountID, db.AccountAPIKeyQuotaCreate, config.C.CustomerAPIKeyCreatePerHour)
	if err != nil {
		slog.Error("account API key create quota failed", "error", err, "account_id", session.AccountID)
		writeError(w, http.StatusServiceUnavailable, "account API key quota is temporarily unavailable")
		return
	}
	if !allowed {
		writeAccountAPIKeyQuotaLimit(w, "ACCOUNT_API_KEY_CREATE_RATE_LIMIT", config.C.CustomerAPIKeyCreatePerHour, remaining)
		return
	}

	// Rate-limit key registration per IP.
	if isDatabaseAccountAPIKeyStore(h.store) {
		allowed, _, err := db.CheckRateLimit(ctx, ip, config.C.RateLimitRegisterPerHour)
		if err != nil {
			slog.Error("rate limit check failed", "error", err)
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
	}

	// Generate API key material.
	fullKey, keyHash, keyPrefix, err := h.generate()
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
		Name:            botName,
		AvatarColor:     "#888888",
		DefaultWeapon:   "sword",
		DefaultStats:    db.JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		DefaultFallback: "aggressive",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	key, activeCount, err := h.store.Create(ctx, session.AccountID, keyID, keyHash, keyPrefix, ip, bot)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCustomerAPIKeyLimit):
			writeAccountAPIKeyActiveLimit(w, activeCount)
		case errors.Is(err, db.ErrCustomerAPIKeyHistoryLimit):
			writeAccountAPIKeyHistoryLimit(w, db.MaxAccountAPIKeyHistory)
		case errors.Is(err, db.ErrCustomerAccountUnverified):
			writeError(w, http.StatusForbidden, "a verified customer email is required")
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "key registration requires the database")
		default:
			slog.Error("failed to create account API key and bot", "error", err, "account_id", session.AccountID)
			writeError(w, http.StatusInternalServerError, "failed to create API key")
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"api_key": fullKey, "key": key, "active_count": activeCount, "limit": db.MaxActiveAccountAPIKeys,
	})
}

func isDatabaseAccountAPIKeyStore(store accountAPIKeyStore) bool {
	_, ok := store.(databaseAccountAPIKeyStore)
	return ok
}

func writeAccountAPIKeyActiveLimit(w http.ResponseWriter, activeCount int) {
	writeJSON(w, http.StatusConflict, map[string]interface{}{
		"error": db.ErrCustomerAPIKeyLimit.Error(), "code": "API_KEY_LIMIT",
		"active_count": activeCount, "limit": db.MaxActiveAccountAPIKeys,
	})
}

func writeAccountAPIKeyHistoryLimit(w http.ResponseWriter, historyCount int) {
	writeJSON(w, http.StatusConflict, map[string]interface{}{
		"error": db.ErrCustomerAPIKeyHistoryLimit.Error(), "code": "API_KEY_HISTORY_LIMIT",
		"history_count": historyCount, "history_limit": db.MaxAccountAPIKeyHistory,
		"support": "Contact Arena support to review your account's archived API-key history.",
	})
}

func writeAccountAPIKeyQuotaLimit(w http.ResponseWriter, code string, limit, remaining int) {
	writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
		"error": "account API key mutation rate limit exceeded", "code": code,
		"limit": limit, "remaining": remaining, "window": "1h", "retry_after": 3600,
	})
}

func (h *AccountKeysHandler) Deactivate(w http.ResponseWriter, r *http.Request) {
	session, ok := verifiedAccountKeySession(w, r)
	if !ok {
		return
	}
	keyID := strings.TrimSpace(chi.URLParam(r, "key_id"))
	if keyID == "" || len(keyID) > 128 {
		writeError(w, http.StatusBadRequest, "key_id is required")
		return
	}
	allowed, remaining, err := h.store.ConsumeQuota(r.Context(), session.AccountID, db.AccountAPIKeyQuotaRevoke, config.C.CustomerAPIKeyRevokePerHour)
	if err != nil {
		slog.Error("account API key revoke quota failed", "error", err, "account_id", session.AccountID)
		writeError(w, http.StatusServiceUnavailable, "account API key quota is temporarily unavailable")
		return
	}
	if !allowed {
		writeAccountAPIKeyQuotaLimit(w, "ACCOUNT_API_KEY_REVOKE_RATE_LIMIT", config.C.CustomerAPIKeyRevokePerHour, remaining)
		return
	}
	key, activeCount, err := h.store.Deactivate(r.Context(), session.AccountID, keyID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCustomerAPIKeyNotOwned):
			writeError(w, http.StatusNotFound, "API key not found")
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "account API keys are unavailable")
		default:
			slog.Error("failed to deactivate account API key", "error", err, "account_id", session.AccountID, "key_id", keyID)
			writeError(w, http.StatusInternalServerError, "failed to revoke API key")
		}
		return
	}
	if h.sessions != nil && key != nil {
		h.sessions.KickBot(key.BotID, "API key revoked")
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"revoked": true, "active_count": activeCount, "limit": db.MaxActiveAccountAPIKeys,
	})
}

// GenerateKey keeps direct package callers on the authenticated account path.
func GenerateKey(w http.ResponseWriter, r *http.Request) {
	NewAccountKeysHandler(nil).Create(w, r)
}

func RetiredAnonymousKeyGeneration(w http.ResponseWriter, r *http.Request) {
	replacementPath := "/api/v1/account/keys"
	if strings.HasPrefix(r.URL.Path, "/arena/") {
		replacementPath = "/arena" + replacementPath
	}
	writeJSON(w, http.StatusGone, map[string]interface{}{
		"error": "anonymous API key generation has been retired",
		"code":  "ACCOUNT_REQUIRED",
		"replacement": map[string]string{
			"method": "POST",
			"path":   replacementPath,
		},
	})
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
