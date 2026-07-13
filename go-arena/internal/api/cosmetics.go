package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/go-chi/chi/v5"
)

type cosmeticsStore interface {
	PublicCatalog(context.Context) (*db.CosmeticCatalog, error)
	AdminCatalog(context.Context) (*db.CosmeticCatalog, error)
	UpsertCategory(context.Context, db.CosmeticCategory, string) (*db.CosmeticCategory, error)
	DeleteCategory(context.Context, string, string) (bool, error)
	UpsertItem(context.Context, db.CosmeticItem, string) (*db.CosmeticItem, error)
	DeleteItem(context.Context, string, string) (bool, error)
	UpsertPack(context.Context, db.CosmeticPack, string) (*db.CosmeticPack, error)
	DeletePack(context.Context, string, string) (bool, error)
	ListAudit(context.Context, int) ([]db.CosmeticCatalogAudit, error)
	ListForBot(context.Context, string) ([]db.BotCosmeticItem, error)
	Equipped(context.Context, string) (map[string]string, error)
	Equip(context.Context, string, string, string) (*db.CosmeticItem, error)
	AccountInventory(context.Context, string) (*db.CustomerCosmeticsInventory, error)
	LinkBot(context.Context, string, string) (*db.AccountBot, error)
	UnlinkBot(context.Context, string, string) (bool, error)
	AssignLicense(context.Context, string, string, *string) (*db.CosmeticAssignmentChange, error)
	EquipLicense(context.Context, string, string, string) (*db.CosmeticLicense, error)
	GrantLicense(context.Context, string, string, string, string) (*db.CosmeticLicense, bool, error)
	RevokeLicense(context.Context, string) (*db.CosmeticAssignmentChange, bool, error)
	AdminAccess(context.Context, string) (*db.CosmeticAdminAccess, error)
	CreateAdminMembership(context.Context, string, time.Time, string, string) (*db.CosmeticAdminMembership, int, error)
	RevokeAdminMembership(context.Context, string, string, string) (*db.CosmeticAdminMembership, []string, bool, error)
	ExpireAdminMembershipsForEmail(context.Context, string, time.Time) (int, []string, error)
}

type databaseCosmeticsStore struct{}

func (databaseCosmeticsStore) PublicCatalog(ctx context.Context) (*db.CosmeticCatalog, error) {
	if db.Pool == nil {
		catalog := db.DefaultCosmeticCatalogData()
		return &catalog, nil
	}
	return db.GetPublicCosmeticCatalog(ctx)
}
func (databaseCosmeticsStore) AdminCatalog(ctx context.Context) (*db.CosmeticCatalog, error) {
	return db.GetAdminCosmeticCatalog(ctx)
}
func (databaseCosmeticsStore) UpsertCategory(ctx context.Context, category db.CosmeticCategory, actor string) (*db.CosmeticCategory, error) {
	return db.UpsertCosmeticCategory(ctx, category, actor)
}
func (databaseCosmeticsStore) DeleteCategory(ctx context.Context, categoryID, actor string) (bool, error) {
	return db.DeleteCosmeticCategory(ctx, categoryID, actor)
}
func (databaseCosmeticsStore) UpsertItem(ctx context.Context, item db.CosmeticItem, actor string) (*db.CosmeticItem, error) {
	return db.UpsertCosmeticCatalogItem(ctx, item, actor)
}
func (databaseCosmeticsStore) DeleteItem(ctx context.Context, itemID, actor string) (bool, error) {
	return db.DeleteCosmeticCatalogItem(ctx, itemID, actor)
}
func (databaseCosmeticsStore) UpsertPack(ctx context.Context, pack db.CosmeticPack, actor string) (*db.CosmeticPack, error) {
	return db.UpsertCosmeticPack(ctx, pack, actor)
}
func (databaseCosmeticsStore) DeletePack(ctx context.Context, packID, actor string) (bool, error) {
	return db.DeleteCosmeticPack(ctx, packID, actor)
}
func (databaseCosmeticsStore) ListAudit(ctx context.Context, limit int) ([]db.CosmeticCatalogAudit, error) {
	return db.ListCosmeticCatalogAudit(ctx, limit)
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
func (databaseCosmeticsStore) AccountInventory(ctx context.Context, accountID string) (*db.CustomerCosmeticsInventory, error) {
	return db.GetCustomerCosmeticsInventory(ctx, accountID)
}
func (databaseCosmeticsStore) LinkBot(ctx context.Context, accountID, botID string) (*db.AccountBot, error) {
	return db.LinkBotToCustomerAccount(ctx, accountID, botID)
}
func (databaseCosmeticsStore) UnlinkBot(ctx context.Context, accountID, botID string) (bool, error) {
	return db.UnlinkBotFromCustomerAccount(ctx, accountID, botID)
}
func (databaseCosmeticsStore) AssignLicense(ctx context.Context, accountID, licenseID string, botID *string) (*db.CosmeticAssignmentChange, error) {
	return db.AssignCosmeticLicense(ctx, accountID, licenseID, botID)
}
func (databaseCosmeticsStore) EquipLicense(ctx context.Context, accountID, botID, licenseID string) (*db.CosmeticLicense, error) {
	return db.EquipCustomerCosmeticLicense(ctx, accountID, botID, licenseID)
}
func (databaseCosmeticsStore) GrantLicense(ctx context.Context, email, cosmeticID, source, externalReference string) (*db.CosmeticLicense, bool, error) {
	return db.GrantCosmeticLicense(ctx, email, cosmeticID, source, externalReference)
}
func (databaseCosmeticsStore) RevokeLicense(ctx context.Context, licenseID string) (*db.CosmeticAssignmentChange, bool, error) {
	return db.RevokeCosmeticLicense(ctx, licenseID)
}
func (databaseCosmeticsStore) AdminAccess(ctx context.Context, email string) (*db.CosmeticAdminAccess, error) {
	return db.GetCosmeticAdminAccessByEmail(ctx, email)
}
func (databaseCosmeticsStore) CreateAdminMembership(ctx context.Context, email string, expiresAt time.Time, note, actor string) (*db.CosmeticAdminMembership, int, error) {
	return db.CreateCosmeticAdminMembership(ctx, email, expiresAt, note, actor)
}
func (databaseCosmeticsStore) RevokeAdminMembership(ctx context.Context, membershipID, actor, reason string) (*db.CosmeticAdminMembership, []string, bool, error) {
	return db.RevokeCosmeticAdminMembership(ctx, membershipID, actor, reason)
}
func (databaseCosmeticsStore) ExpireAdminMembershipsForEmail(ctx context.Context, email string, now time.Time) (int, []string, error) {
	return db.ExpireCustomerCosmeticAdminMemberships(ctx, email, now)
}

// CosmeticsHandler owns catalog, entitlement, and equip HTTP behavior. The
// store seam keeps payment fulfillment/provider work independent from routes.
type CosmeticsHandler struct {
	store                      cosmeticsStore
	engine                     *game.GameEngine
	checkoutEnabled            bool
	verifyAPIKey               func(context.Context, string) (*db.Bot, error)
	consumeAccountKeyQuota     func(context.Context, string, db.AccountAPIKeyQuotaAction, int) (bool, int, error)
	checkAccountInventoryQuota func(context.Context, string, int) (bool, error)
}

func NewCosmeticsHandler(engine *game.GameEngine) *CosmeticsHandler {
	return &CosmeticsHandler{
		store: databaseCosmeticsStore{}, engine: engine, verifyAPIKey: security.VerifyAPIKey,
		consumeAccountKeyQuota: db.ConsumeAccountAPIKeyQuota,
		checkAccountInventoryQuota: func(ctx context.Context, accountID string, limit int) (bool, error) {
			allowed, _, _, err := security.CheckRateLimit(ctx, "cosmetics-inventory-account:"+accountID, limit, 60)
			return allowed, err
		},
	}
}

func newCosmeticsHandlerWithStore(store cosmeticsStore, engine *game.GameEngine) *CosmeticsHandler {
	return &CosmeticsHandler{
		store: store, engine: engine, verifyAPIKey: security.VerifyAPIKey,
		consumeAccountKeyQuota: func(context.Context, string, db.AccountAPIKeyQuotaAction, int) (bool, int, error) {
			return true, 0, nil
		},
		checkAccountInventoryQuota: func(context.Context, string, int) (bool, error) { return true, nil },
	}
}

func (h *CosmeticsHandler) Catalog(w http.ResponseWriter, r *http.Request) {
	catalog, err := h.store.PublicCatalog(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "cosmetics catalog is unavailable")
		return
	}
	checkoutEnabled := h.checkoutEnabled && cosmeticCatalogHasPurchasablePack(catalog)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"categories": catalog.Categories,
		"packs":      catalog.Packs,
		"items":      catalog.Items,
		// A catalog sale flag is not enough to make payments safe. This remains
		// false until a verified checkout/webhook provider is wired into the
		// handler, even if an operator stages purchasable catalog entries.
		"checkout_enabled":   checkoutEnabled,
		"subscription_offer": db.DefaultCosmeticSubscriptionOffer(checkoutEnabled),
	})
}

// cosmeticCatalogHasPurchasablePack mirrors the launch checkout contract: only
// whole packs can be sold, and every category/item dependency must be active.
// This also keeps Admin's all-record projection from advertising checkout when
// the public projection has no offer that CreateCosmeticOrder can accept.
func cosmeticCatalogHasPurchasablePack(catalog *db.CosmeticCatalog) bool {
	if catalog == nil {
		return false
	}
	activeCategories := make(map[string]bool, len(catalog.Categories))
	for _, category := range catalog.Categories {
		activeCategories[category.ID] = category.IsActive
	}
	activeItems := make(map[string]db.CosmeticItem, len(catalog.Items))
	for _, item := range catalog.Items {
		activeItems[item.ID] = item
	}
	for _, pack := range catalog.Packs {
		if !pack.IsActive || !pack.IsPurchasable || pack.IsFree || pack.PriceCents != db.CosmeticPackPriceForCategory(pack.CategoryID) ||
			!strings.EqualFold(pack.Currency, "USD") || !activeCategories[pack.CategoryID] {
			continue
		}
		itemIDs := pack.ItemIDs
		if len(itemIDs) == 0 && len(pack.Items) > 0 {
			itemIDs = make([]string, 0, len(pack.Items))
			for _, item := range pack.Items {
				itemIDs = append(itemIDs, item.ID)
				if _, exists := activeItems[item.ID]; !exists {
					activeItems[item.ID] = item
				}
			}
		}
		if len(itemIDs) == 0 {
			continue
		}
		allActive := true
		trailItemCount := 0
		for _, itemID := range itemIDs {
			item, exists := activeItems[itemID]
			if !exists || !item.IsActive || !activeCategories[item.CategoryID] || !db.IsValidCosmeticSlot(item.Slot) ||
				((item.Slot == db.CosmeticSlotTrail) != (item.CategoryID == db.CosmeticTrailCategoryID)) {
				allActive = false
				break
			}
			if item.Slot == db.CosmeticSlotTrail {
				trailItemCount++
			}
		}
		validProductShape := pack.CategoryID != db.CosmeticTrailCategoryID && trailItemCount == 0
		if pack.CategoryID == db.CosmeticTrailCategoryID {
			validProductShape = len(itemIDs) == 1 && trailItemCount == 1
		}
		if allActive && validProductShape {
			return true
		}
	}
	return false
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
	Email             string `json:"email"`
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
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.CosmeticID = strings.TrimSpace(req.CosmeticID)
	req.Source = strings.TrimSpace(strings.ToLower(req.Source))
	req.ExternalReference = strings.TrimSpace(req.ExternalReference)
	if req.Source == "" {
		req.Source = "manual"
	}
	if _, err := db.NormalizeCustomerEmail(req.Email); err != nil {
		return req, errors.New("invalid cosmetic grant")
	}
	if req.CosmeticID == "" || len(req.CosmeticID) > 80 || req.Source != "manual" ||
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
	license, created, err := h.store.GrantLicense(r.Context(), req.Email, req.CosmeticID, req.Source, req.ExternalReference)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCosmeticNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, db.ErrCosmeticLicenseGrantConflict):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, db.ErrCustomerEmailInvalid):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, db.ErrCosmeticLicenseReferenceRequired):
			writeError(w, http.StatusBadRequest, err.Error())
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
		"granted":    created,
		"idempotent": !created,
		"license":    license,
	})
}

type cosmeticRevokeRequest struct {
	LicenseID string `json:"license_id"`
}

func (h *CosmeticsHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	licenseID := strings.TrimSpace(chi.URLParam(r, "license_id"))
	if licenseID == "" {
		var req cosmeticRevokeRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err == nil {
			licenseID = strings.TrimSpace(req.LicenseID)
		}
	}
	if licenseID == "" || len(licenseID) > 100 {
		writeError(w, http.StatusBadRequest, "invalid cosmetic revocation")
		return
	}
	change, revoked, err := h.store.RevokeLicense(r.Context(), licenseID)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "database not available")
			return
		}
		if errors.Is(err, db.ErrCosmeticAdminMembershipLicense) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke cosmetic")
		return
	}
	if change != nil {
		h.refreshBotVisuals(r.Context(), change.PreviousBotID)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"revoked":    revoked,
		"license_id": licenseID,
		"license":    change,
	})
}

func (h *CosmeticsHandler) refreshBotVisuals(ctx context.Context, botID *string) bool {
	if h.engine == nil || botID == nil || strings.TrimSpace(*botID) == "" {
		return false
	}
	equipped, err := h.store.Equipped(ctx, *botID)
	if err != nil {
		return false
	}
	return h.engine.UpdateBotCosmetics(*botID, equipped)
}

// refreshConnectedBotVisuals invalidates the engine's presentation-only cache
// after an administrator changes item/category availability. DB resolution is
// authoritative; failures leave that bot unchanged and are repaired by its
// next equip/reconnect instead of making the catalog mutation itself fail.
func (h *CosmeticsHandler) refreshConnectedBotVisuals(ctx context.Context) int {
	if h.engine == nil {
		return 0
	}
	refreshed := 0
	for _, botID := range h.engine.ConnectedBotIDs() {
		equipped, err := h.store.Equipped(ctx, botID)
		if err != nil {
			continue
		}
		if h.engine.UpdateBotCosmetics(botID, equipped) {
			refreshed++
		}
	}
	return refreshed
}

func customerSession(r *http.Request) (*CustomerSession, bool) {
	session := CustomerSessionFromContext(r.Context())
	return session, session != nil && session.AccountID != ""
}

func (h *CosmeticsHandler) AccountInventory(w http.ResponseWriter, r *http.Request) {
	session, ok := customerSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "customer authentication required")
		return
	}
	if h.checkAccountInventoryQuota != nil {
		allowed, err := h.checkAccountInventoryQuota(r.Context(), session.AccountID, config.C.CosmeticsAccountReadRPM)
		// Inventory reads degrade open when Redis is unavailable, but an
		// authoritative over-quota result stops the account-locking DB sync.
		if err == nil && !allowed {
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"error": "account cosmetics rate limit exceeded", "code": "ACCOUNT_COSMETICS_RATE_LIMIT",
			})
			return
		}
	}
	inventory, err := h.store.AccountInventory(r.Context(), session.AccountID)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "database not available")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load customer cosmetics")
		return
	}
	inventory.SubscriptionOffer = db.DefaultCosmeticSubscriptionOffer(h.checkoutEnabled)
	writeJSON(w, http.StatusOK, inventory)
}

type linkAccountBotRequest struct {
	APIKey string `json:"api_key"`
}

func (h *CosmeticsHandler) LinkAccountBot(w http.ResponseWriter, r *http.Request) {
	session, ok := customerSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "customer authentication required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var req linkAccountBotRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || strings.TrimSpace(req.APIKey) == "" || len(req.APIKey) > 256 {
		writeError(w, http.StatusBadRequest, "api_key is required")
		return
	}
	allowed, remaining, err := h.consumeAccountKeyQuota(
		r.Context(), session.AccountID, db.AccountAPIKeyQuotaLink, config.C.CustomerBotLinkPerHour,
	)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "account bot-link quota is temporarily unavailable")
		return
	}
	if !allowed {
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error": "account bot-link rate limit exceeded", "code": "ACCOUNT_BOT_LINK_RATE_LIMIT",
			"limit": config.C.CustomerBotLinkPerHour, "remaining": remaining,
			"window": "1h", "retry_after": 3600,
		})
		return
	}
	bot, err := h.verifyAPIKey(r.Context(), strings.TrimSpace(req.APIKey))
	if err != nil || bot == nil {
		writeError(w, http.StatusUnauthorized, "invalid API key")
		return
	}
	linkedBot, err := h.store.LinkBot(r.Context(), session.AccountID, bot.ID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCustomerBotAlreadyLinked):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, db.ErrCustomerAPIKeyAlreadyOwned):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, db.ErrCustomerAPIKeyLimit):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, db.ErrCustomerAPIKeyHistoryLimit):
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error": err.Error(), "code": "API_KEY_HISTORY_LIMIT",
				"history_limit": db.MaxAccountAPIKeyHistory,
				"support":       "Contact Arena support to review your account's archived API-key history.",
			})
		case errors.Is(err, db.ErrCustomerBotKeyInactive):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, db.ErrCustomerAccountUnverified):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "database not available")
		default:
			writeError(w, http.StatusInternalServerError, "failed to link bot")
		}
		return
	}
	inventory, err := h.store.AccountInventory(r.Context(), session.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bot linked but inventory refresh failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"linked_bot": linkedBot,
		"inventory":  inventory,
	})
}

func (h *CosmeticsHandler) UnlinkAccountBot(w http.ResponseWriter, r *http.Request) {
	session, ok := customerSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "customer authentication required")
		return
	}
	botID := strings.TrimSpace(chi.URLParam(r, "bot_id"))
	if botID == "" || len(botID) > 80 {
		writeError(w, http.StatusBadRequest, "invalid bot_id")
		return
	}
	unlinked, err := h.store.UnlinkBot(r.Context(), session.AccountID, botID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCustomerBotNotLinked):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "database not available")
		default:
			writeError(w, http.StatusInternalServerError, "failed to unlink bot")
		}
		return
	}
	h.refreshBotVisuals(r.Context(), &botID)
	inventory, err := h.store.AccountInventory(r.Context(), session.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bot unlinked but inventory refresh failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"unlinked":  unlinked,
		"bot_id":    botID,
		"inventory": inventory,
	})
}

type assignLicenseRequest struct {
	BotID *string `json:"bot_id"`
}

func (h *CosmeticsHandler) AssignAccountLicense(w http.ResponseWriter, r *http.Request) {
	session, ok := customerSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "customer authentication required")
		return
	}
	licenseID := strings.TrimSpace(chi.URLParam(r, "license_id"))
	if licenseID == "" || len(licenseID) > 100 {
		writeError(w, http.StatusBadRequest, "invalid license_id")
		return
	}
	var botID *string
	if r.Method != http.MethodDelete {
		var req assignLicenseRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil || req.BotID == nil || strings.TrimSpace(*req.BotID) == "" {
			writeError(w, http.StatusBadRequest, "bot_id is required")
			return
		}
		value := strings.TrimSpace(*req.BotID)
		if len(value) > 80 {
			writeError(w, http.StatusBadRequest, "invalid bot_id")
			return
		}
		botID = &value
	}
	change, err := h.store.AssignLicense(r.Context(), session.AccountID, licenseID, botID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCosmeticLicenseNotFound), errors.Is(err, db.ErrCosmeticLicenseNotOwned):
			writeError(w, http.StatusNotFound, db.ErrCosmeticLicenseNotFound.Error())
		case errors.Is(err, db.ErrCustomerBotNotLinked):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, db.ErrCosmeticInactive):
			writeError(w, http.StatusConflict, "cosmetic license is not active")
		case errors.Is(err, db.ErrCustomerBotKeyInactive):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "database not available")
		default:
			writeError(w, http.StatusInternalServerError, "failed to update cosmetic assignment")
		}
		return
	}
	h.refreshBotVisuals(r.Context(), change.PreviousBotID)
	inventory, err := h.store.AccountInventory(r.Context(), session.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "assignment updated but inventory refresh failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"assignment": change,
		"inventory":  inventory,
	})
}

type equipLicenseRequest struct {
	LicenseID string `json:"license_id"`
}

func (h *CosmeticsHandler) EquipAccountLicense(w http.ResponseWriter, r *http.Request) {
	session, ok := customerSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "customer authentication required")
		return
	}
	botID := strings.TrimSpace(chi.URLParam(r, "bot_id"))
	var req equipLicenseRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || botID == "" || len(botID) > 80 ||
		strings.TrimSpace(req.LicenseID) == "" || len(req.LicenseID) > 100 {
		writeError(w, http.StatusBadRequest, "bot_id and license_id are required")
		return
	}
	req.LicenseID = strings.TrimSpace(req.LicenseID)
	license, err := h.store.EquipLicense(r.Context(), session.AccountID, botID, req.LicenseID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCosmeticLicenseNotFound), errors.Is(err, db.ErrCosmeticLicenseNotOwned):
			writeError(w, http.StatusNotFound, db.ErrCosmeticLicenseNotFound.Error())
		case errors.Is(err, db.ErrCustomerBotNotLinked):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, db.ErrCosmeticInactive):
			writeError(w, http.StatusConflict, "cosmetic license is not active")
		case errors.Is(err, db.ErrCustomerBotKeyInactive):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "database not available")
		default:
			writeError(w, http.StatusInternalServerError, "failed to equip cosmetic license")
		}
		return
	}
	equipped, _ := h.store.Equipped(r.Context(), botID)
	liveRefreshed := false
	if h.engine != nil {
		liveRefreshed = h.engine.UpdateBotCosmetics(botID, equipped)
	}
	inventory, err := h.store.AccountInventory(r.Context(), session.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cosmetic equipped but inventory refresh failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"license":         license,
		"equipped_assets": equipped,
		"live_refreshed":  liveRefreshed,
		"inventory":       inventory,
		"gameplay":        "unchanged",
	})
}
