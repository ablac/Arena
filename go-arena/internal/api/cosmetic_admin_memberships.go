package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"arena-server/internal/db"

	"github.com/go-chi/chi/v5"
)

const maxAdminCosmeticMembershipDuration = 5 * 365 * 24 * time.Hour

type adminCosmeticMembershipRequest struct {
	Email        string `json:"email"`
	DurationDays int    `json:"duration_days"`
	ExpiresAt    string `json:"expires_at"`
	Note         string `json:"note"`
}

func (h *CosmeticsHandler) AdminCosmeticAccess(w http.ResponseWriter, r *http.Request) {
	setAdminCatalogNoStore(w)
	email, err := db.NormalizeCustomerEmail(r.URL.Query().Get("email"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "valid email is required")
		return
	}
	if err := h.reconcileAdminMembershipExpiryForEmail(r, email); err != nil {
		writeAdminCosmeticMembershipError(w, err, "failed to reconcile expired cosmetic access")
		return
	}
	access, err := h.store.AdminAccess(r.Context(), email)
	if err != nil {
		writeAdminCosmeticMembershipError(w, err, "failed to load customer cosmetic access")
		return
	}
	if access == nil {
		access = &db.CosmeticAdminAccess{
			Email: email, Licenses: []db.CosmeticLicense{}, Memberships: []db.CosmeticAdminMembership{},
		}
	}
	writeJSON(w, http.StatusOK, access)
}

func (h *CosmeticsHandler) CreateAdminCosmeticMembership(w http.ResponseWriter, r *http.Request) {
	setAdminCatalogNoStore(w)
	var request adminCosmeticMembershipRequest
	if err := decodeStrictCosmeticAdminJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid cosmetic membership")
		return
	}
	email, err := db.NormalizeCustomerEmail(request.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cosmetic membership")
		return
	}
	hasDuration := request.DurationDays > 0
	hasExpiry := strings.TrimSpace(request.ExpiresAt) != ""
	if hasDuration == hasExpiry || request.DurationDays < 0 || request.DurationDays > 5*365 || len(strings.TrimSpace(request.Note)) > 500 {
		writeError(w, http.StatusBadRequest, "provide either duration_days or expires_at")
		return
	}
	now := time.Now().UTC()
	var expiresAt time.Time
	if hasDuration {
		expiresAt = now.Add(time.Duration(request.DurationDays) * 24 * time.Hour)
	} else {
		expiresAt, err = time.Parse(time.RFC3339, strings.TrimSpace(request.ExpiresAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "expires_at must be an RFC3339 timestamp")
			return
		}
	}
	if !expiresAt.After(now) || expiresAt.After(now.Add(maxAdminCosmeticMembershipDuration)) {
		writeError(w, http.StatusBadRequest, "membership expiry must be in the future and within five years")
		return
	}
	if err := h.reconcileAdminMembershipExpiryForEmail(r, email); err != nil {
		writeAdminCosmeticMembershipError(w, err, "failed to reconcile expired cosmetic access")
		return
	}
	membership, licensesCreated, err := h.store.CreateAdminMembership(
		r.Context(), email, expiresAt.UTC(), strings.TrimSpace(request.Note), cosmeticAdminActor(r),
	)
	if err != nil {
		writeAdminCosmeticMembershipError(w, err, "failed to grant cosmetic membership")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"granted": true, "membership": membership, "licenses_created": licensesCreated,
	})
}

type adminCosmeticMembershipRevokeRequest struct {
	MembershipID string `json:"membership_id"`
	Reason       string `json:"reason"`
}

func (h *CosmeticsHandler) RevokeAdminCosmeticMembership(w http.ResponseWriter, r *http.Request) {
	setAdminCatalogNoStore(w)
	membershipID := strings.TrimSpace(chi.URLParam(r, "membership_id"))
	var request adminCosmeticMembershipRevokeRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeStrictCosmeticAdminJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid cosmetic membership revocation")
			return
		}
	}
	if membershipID == "" {
		membershipID = strings.TrimSpace(request.MembershipID)
	}
	if membershipID == "" || len(membershipID) > 100 || len(strings.TrimSpace(request.Reason)) > 500 {
		writeError(w, http.StatusBadRequest, "invalid cosmetic membership revocation")
		return
	}
	membership, affectedBotIDs, revoked, err := h.store.RevokeAdminMembership(
		r.Context(), membershipID, cosmeticAdminActor(r), strings.TrimSpace(request.Reason),
	)
	if err != nil {
		writeAdminCosmeticMembershipError(w, err, "failed to revoke cosmetic membership")
		return
	}
	refreshed, refreshFailures := h.refreshMembershipVisuals(r.Context(), affectedBotIDs)
	if refreshFailures > 0 {
		markCosmeticMembershipCacheRepair()
		slog.Warn("queued cosmetic membership visual cache repair", "failures", refreshFailures)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"revoked": revoked, "membership_id": membershipID, "membership": membership,
		"affected_bots": affectedBotIDs, "live_refreshed": refreshed, "live_refresh_failures": refreshFailures,
	})
}

func (h *CosmeticsHandler) reconcileAdminMembershipExpiryForEmail(r *http.Request, email string) error {
	expired, affectedBotIDs, err := h.store.ExpireAdminMembershipsForEmail(r.Context(), email, time.Now().UTC())
	if err != nil {
		return err
	}
	if expired == 0 {
		return nil
	}
	_, refreshFailures := h.refreshMembershipVisuals(r.Context(), affectedBotIDs)
	if refreshFailures > 0 {
		markCosmeticMembershipCacheRepair()
		slog.Warn("queued expired cosmetic membership visual cache repair", "failures", refreshFailures)
	}
	return nil
}

func (h *CosmeticsHandler) refreshMembershipVisuals(ctx context.Context, botIDs []string) (int, int) {
	if h.engine == nil || len(botIDs) == 0 {
		return 0, 0
	}
	connected := make(map[string]struct{})
	for _, botID := range h.engine.ConnectedBotIDs() {
		connected[botID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(botIDs))
	refreshed := 0
	failed := 0
	for _, rawBotID := range botIDs {
		botID := strings.TrimSpace(rawBotID)
		if botID == "" {
			continue
		}
		if _, duplicate := seen[botID]; duplicate {
			continue
		}
		seen[botID] = struct{}{}
		if _, ok := connected[botID]; !ok {
			continue
		}
		equipped, err := h.store.Equipped(ctx, botID)
		if err != nil {
			failed++
			continue
		}
		if h.engine.UpdateBotCosmetics(botID, equipped) {
			refreshed++
		}
	}
	return refreshed, failed
}

func writeAdminCosmeticMembershipError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, db.ErrCustomerEmailInvalid), errors.Is(err, db.ErrCosmeticAdminMembershipInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, db.ErrCosmeticAdminMembershipActive):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, db.ErrNoDatabase):
		writeError(w, http.StatusServiceUnavailable, "database not available")
	default:
		writeError(w, http.StatusInternalServerError, fallback)
	}
}
