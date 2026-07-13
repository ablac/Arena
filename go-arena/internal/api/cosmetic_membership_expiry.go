package api

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"arena-server/internal/db"
	"arena-server/internal/game"
)

const cosmeticAdminMembershipExpiryBatch = 100

var cosmeticMembershipCacheRepairPending atomic.Bool

func markCosmeticMembershipCacheRepair() {
	cosmeticMembershipCacheRepairPending.Store(true)
}

// RunCosmeticAdminMembershipExpiryLoop removes expired complimentary access
// and repairs presentation-only engine caches. Database reads still reject an
// expired membership at the timestamp boundary if this worker is delayed.
func RunCosmeticAdminMembershipExpiryLoop(ctx context.Context, engine *game.GameEngine) {
	repairPending := reconcileCosmeticAdminMembershipExpiry(ctx, engine, cosmeticMembershipCacheRepairPending.Swap(false))
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			repairPending = repairPending || cosmeticMembershipCacheRepairPending.Swap(false)
			repairPending = reconcileCosmeticAdminMembershipExpiry(ctx, engine, repairPending)
		}
	}
}

func reconcileCosmeticAdminMembershipExpiry(ctx context.Context, engine *game.GameEngine, repairPending bool) bool {
	for {
		expired, _, err := db.ExpireCosmeticAdminMemberships(
			ctx, time.Now().UTC(), cosmeticAdminMembershipExpiryBatch,
		)
		if expired > 0 {
			repairPending = true
		}
		if err != nil {
			slog.Warn("failed to expire complimentary cosmetic memberships", "error", err)
			repairPending = true
			break
		}
		if expired < cosmeticAdminMembershipExpiryBatch {
			break
		}
	}
	// Keep retrying presentation-cache repair after a transition commits. The
	// membership no longer appears in the expiry set, so the retry bit is the
	// durable handoff for this process without polling loadouts when idle.
	if !repairPending || engine == nil {
		return repairPending
	}
	botIDs := engine.ConnectedBotIDs()
	if len(botIDs) == 0 {
		return false
	}
	loadouts, err := db.GetEquippedCosmeticsForBots(ctx, botIDs)
	if err != nil {
		slog.Warn("failed to refresh connected bots after cosmetic membership reconciliation", "error", err)
		return true
	}
	for botID, equipped := range loadouts {
		engine.UpdateBotCosmetics(botID, equipped)
	}
	return false
}
