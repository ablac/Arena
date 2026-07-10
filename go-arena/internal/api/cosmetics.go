package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"
)

type cosmeticsStore interface {
	ListCatalog(context.Context) ([]db.CosmeticItem, error)
	ListForBot(context.Context, string) ([]db.BotCosmeticItem, error)
	Equipped(context.Context, string) (map[string]string, error)
	Equip(context.Context, string, string, string) (*db.CosmeticItem, error)
	Grant(context.Context, string, string, string, string) (bool, error)
	Revoke(context.Context, string, string) (bool, error)
}

type databaseCosmeticsStore struct{}

func (databaseCosmeticsStore) ListCatalog(ctx context.Context) ([]db.CosmeticItem, error) {
	if db.Pool == nil {
		return db.DefaultCosmeticCatalog(), nil
	}
	return db.ListCosmeticCatalog(ctx)
}
func (databaseCosmeticsStore) ListForBot(ctx context.Context, botID string) ([]db.BotCosmeticItem, error) {
	return db.ListBotCosmetics(ctx, botID)
}
func (databaseCosmeticsStore) Equipped(ctx context.Context, botID string) (map[string]string, error) {
	return db.GetEquippedCosmetics(ctx, botID)
}
func (databaseCosmeticsStore) Equip(ctx context.Context, botID, slot, cosmeticID string) (*db.CosmeticItem, error) {
	return db.EquipCosmetic(ctx, botID, slot, cosmeticID)
}
func (databaseCosmeticsStore) Grant(ctx context.Context, botID, cosmeticID, source, externalReference string) (bool, error) {
	return db.GrantCosmeticEntitlement(ctx, botID, cosmeticID, source, externalReference)
}
func (databaseCosmeticsStore) Revoke(ctx context.Context, botID, cosmeticID string) (bool, error) {
	return db.RevokeCosmeticEntitlement(ctx, botID, cosmeticID)
}

// CosmeticsHandler owns catalog, entitlement, and equip HTTP behavior. The
// store seam keeps payment fulfillment/provider work independent from routes.
type CosmeticsHandler struct {
	store           cosmeticsStore
	engine          *game.GameEngine
	checkoutEnabled bool
}

func NewCosmeticsHandler(engine *game.GameEngine) *CosmeticsHandler {
	return &CosmeticsHandler{store: databaseCosmeticsStore{}, engine: engine}
}

func newCosmeticsHandlerWithStore(store cosmeticsStore, engine *game.GameEngine) *CosmeticsHandler {
	return &CosmeticsHandler{store: store, engine: engine}
}

func (h *CosmeticsHandler) Catalog(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListCatalog(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "cosmetics catalog is unavailable")
		return
	}
	hasPurchasableItem := false
	for _, item := range items {
		hasPurchasableItem = hasPurchasableItem || item.IsPurchasable
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		// A catalog sale flag is not enough to make payments safe. This remains
		// false until a verified checkout/webhook provider is wired into the
		// handler, even if an operator stages purchasable catalog entries.
		"checkout_enabled": h.checkoutEnabled && hasPurchasableItem,
	})
}

func (h *CosmeticsHandler) BotInventory(w http.ResponseWriter, r *http.Request) {
	bot := security.GetBotFromContext(r.Context())
	if bot == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	items, err := h.store.ListForBot(r.Context(), bot.ID)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "database not available")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load cosmetics")
		return
	}
	equipped := make(map[string]string)
	for _, item := range items {
		if item.Equipped {
			equipped[item.Slot] = item.ID
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bot_id":   bot.ID,
		"items":    items,
		"equipped": equipped,
	})
}

type equipCosmeticRequest struct {
	Slot       string `json:"slot"`
	CosmeticID string `json:"cosmetic_id"`
}

func (h *CosmeticsHandler) Equip(w http.ResponseWriter, r *http.Request) {
	bot := security.GetBotFromContext(r.Context())
	if bot == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req equipCosmeticRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Slot = strings.TrimSpace(strings.ToLower(req.Slot))
	req.CosmeticID = strings.TrimSpace(req.CosmeticID)
	if !db.IsValidCosmeticSlot(req.Slot) || req.CosmeticID == "" || len(req.CosmeticID) > 80 {
		writeError(w, http.StatusBadRequest, "slot and cosmetic_id are required and must be valid")
		return
	}

	item, err := h.store.Equip(r.Context(), bot.ID, req.Slot, req.CosmeticID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrInvalidCosmeticSlot), errors.Is(err, db.ErrCosmeticSlotMismatch):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, db.ErrCosmeticNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, db.ErrCosmeticNotOwned):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, db.ErrCosmeticInactive):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "database not available")
		default:
			writeError(w, http.StatusInternalServerError, "failed to equip cosmetic")
		}
		return
	}

	equipped, err := h.store.Equipped(r.Context(), bot.ID)
	liveRefreshed := false
	if err == nil && h.engine != nil {
		liveRefreshed = h.engine.UpdateBotCosmetics(bot.ID, equipped)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":         "cosmetic equipped",
		"item":            item,
		"equipped_assets": equipped,
		"live_refreshed":  liveRefreshed,
		"gameplay":        "unchanged",
	})
}

var entitlementSourcePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

type cosmeticGrantRequest struct {
	BotID             string `json:"bot_id"`
	CosmeticID        string `json:"cosmetic_id"`
	Source            string `json:"source"`
	ExternalReference string `json:"external_reference"`
}

func decodeCosmeticGrant(r *http.Request) (cosmeticGrantRequest, error) {
	var req cosmeticGrantRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	req.BotID = strings.TrimSpace(req.BotID)
	req.CosmeticID = strings.TrimSpace(req.CosmeticID)
	req.Source = strings.TrimSpace(strings.ToLower(req.Source))
	req.ExternalReference = strings.TrimSpace(req.ExternalReference)
	if req.Source == "" {
		req.Source = "manual"
	}
	if req.BotID == "" || len(req.BotID) > 80 || req.CosmeticID == "" || len(req.CosmeticID) > 80 ||
		!entitlementSourcePattern.MatchString(req.Source) || len(req.ExternalReference) > 160 {
		return req, errors.New("invalid cosmetic grant")
	}
	return req, nil
}

func (h *CosmeticsHandler) Grant(w http.ResponseWriter, r *http.Request) {
	req, err := decodeCosmeticGrant(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cosmetic grant")
		return
	}
	created, err := h.store.Grant(r.Context(), req.BotID, req.CosmeticID, req.Source, req.ExternalReference)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCosmeticBotNotFound), errors.Is(err, db.ErrCosmeticNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, db.ErrCosmeticGrantConflict):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "database not available")
		default:
			writeError(w, http.StatusInternalServerError, "failed to grant cosmetic")
		}
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]interface{}{
		"granted":     created,
		"idempotent":  !created,
		"bot_id":      req.BotID,
		"cosmetic_id": req.CosmeticID,
	})
}

func (h *CosmeticsHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	req, err := decodeCosmeticGrant(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cosmetic revocation")
		return
	}
	revoked, err := h.store.Revoke(r.Context(), req.BotID, req.CosmeticID)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "database not available")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke cosmetic")
		return
	}
	if h.engine != nil {
		if equipped, loadErr := h.store.Equipped(r.Context(), req.BotID); loadErr == nil {
			h.engine.UpdateBotCosmetics(req.BotID, equipped)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"revoked":     revoked,
		"bot_id":      req.BotID,
		"cosmetic_id": req.CosmeticID,
	})
}
