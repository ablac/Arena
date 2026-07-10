package api

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"arena-server/internal/config"
	"arena-server/internal/db"
)

// Admin game configuration is restart-staged. config.C is read directly from
// hundreds of simulation call sites, so publishing live mutations would require
// an unsafe partial synchronization migration. The handler instead validates a
// complete desired snapshot, commits canonical overrides, and leaves the active
// startup snapshot untouched until the next process start.

func cloneOverrideValues(values map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func (h *AdminHandler) ensureConfigStateLocked() {
	if !h.activeConfigSet {
		h.activeConfig = config.C
		h.activeConfigSet = true
	}
	if h.gameOverrides == nil {
		h.gameOverrides = make(map[string]interface{})
	}
}

func (h *AdminHandler) desiredGameConfigLocked() (config.Config, map[string]interface{}, error) {
	h.ensureConfigStateLocked()
	candidate, canonical := applyGameConfigUpdatesTo(h.activeConfig, h.gameOverrides)
	if rejected := rejectedConfigKeys(h.gameOverrides, canonical); len(rejected) > 0 {
		return h.activeConfig, nil, fmt.Errorf("stored game overrides contain rejected keys: %v", rejected)
	}
	return candidate, canonical, nil
}

func (h *AdminHandler) stageGameConfigUpdatesLocked(
	ctx context.Context,
	updates map[string]interface{},
) (map[string]interface{}, []string, bool, error) {
	h.ensureConfigStateLocked()
	merged := cloneOverrideValues(h.gameOverrides)
	for key, value := range updates {
		merged[key] = value
	}

	candidate, canonical := applyGameConfigUpdatesTo(h.activeConfig, merged)
	if rejected := rejectedConfigKeys(merged, canonical); len(rejected) > 0 {
		// A stale unknown database key is a server-state problem and must not be
		// hidden behind a successful partial save. Invalid keys supplied by this
		// request may still be rejected alongside other valid, durable values.
		for _, key := range rejected {
			if _, supplied := updates[key]; !supplied {
				return nil, nil, false, fmt.Errorf("stored game overrides contain rejected keys: %v", rejected)
			}
		}
	}

	applied := selectOverrideKeys(canonical, updates)
	rejected := rejectedConfigKeys(updates, applied)
	if len(applied) == 0 {
		return applied, rejected, false, nil
	}
	if err := h.persistAdminOverrides(ctx, db.AdminOverrideScopeGameConfig, applied); err != nil {
		return nil, nil, false, err
	}

	// Canonical values become the desired restart snapshot only after the
	// database commit succeeds.
	h.gameOverrides = canonical
	return applied, rejected, configOverridesDiffer(h.activeConfig, candidate, canonical), nil
}

func selectOverrideKeys(canonical, requested map[string]interface{}) map[string]interface{} {
	selected := make(map[string]interface{})
	for key := range requested {
		if value, ok := canonical[key]; ok {
			selected[key] = value
		}
	}
	return selected
}

func configOverridesDiffer(active, desired config.Config, keys map[string]interface{}) bool {
	activeValues := gameConfigValues(active)
	desiredValues := gameConfigValues(desired)
	for key := range keys {
		if !configValueEqual(activeValues[key], desiredValues[key]) {
			return true
		}
	}
	return false
}

func configValueEqual(a, b interface{}) bool {
	af, aNumber := toFloat(a)
	bf, bNumber := toFloat(b)
	if aNumber && bNumber {
		return af == bf
	}
	return reflect.DeepEqual(a, b)
}

func mapShapePoolNames(value string) []string {
	canonical, ok := canonicalMapShapePool(value)
	if !ok || canonical == "" {
		return []string{"square"}
	}
	return strings.Split(canonical, ",")
}

func gameConfigValues(c config.Config) map[string]interface{} {
	return map[string]interface{}{
		"tick_rate":              c.TickRate,
		"max_bots":               c.MaxBots,
		"max_spectators":         c.MaxSpectators,
		"arena_width":            c.ArenaWidth,
		"arena_height":           c.ArenaHeight,
		"round_duration":         c.RoundDuration,
		"intermission_time":      c.IntermissionTime,
		"lobby_countdown":        c.LobbyCountdown,
		"min_bots_to_start":      c.MinBotsToStart,
		"stat_budget":            c.StatBudget,
		"game_mode":              c.GameModeName,
		"team_count":             c.TeamCount,
		"friendly_fire":          c.FriendlyFire,
		"round_modifier_chance":  c.RoundModifierChance,
		"map_shape":              c.MapShape,
		"map_shape_pool":         c.MapShapePool,
		"zone_damage":            c.ZoneDamagePerTick,
		"zone_shrink_pct":        c.ZoneShrinkPercent,
		"zone_shrink_interval":   c.ZoneShrinkInterval,
		"zone_min_radius":        c.ZoneMinRadius,
		"zone_shrink_delay":      c.ZoneShrinkDelay,
		"zone_initial_radius":    c.ZoneInitialRadius,
		"zone_cover_map":         c.ZoneCoverMap,
		"obstacle_count_min":     c.ObstacleCountMin,
		"obstacle_count_max":     c.ObstacleCountMax,
		"arena_size_dynamic":     c.ArenaSizeDynamic,
		"arena_size_base_bots":   c.ArenaSizeBaseBots,
		"arena_size_max_bots":    c.ArenaSizeMaxBots,
		"arena_size_min_scale":   c.ArenaSizeMinScale,
		"arena_size_max_scale":   c.ArenaSizeMaxScale,
		"dodge_speed_mult":       c.DodgeSpeedMult,
		"dodge_invuln_ticks":     c.DodgeInvulnTicks,
		"dodge_cooldown_ticks":   c.DodgeCooldownTicks,
		"projectile_speed":       c.ProjectileSpeed,
		"afk_timeout_ticks":      c.AFKTimeoutTicks,
		"stat_hp_base":           c.StatHPBase,
		"stat_hp_per_point":      c.StatHPPerPoint,
		"stat_speed_base":        c.StatSpeedBase,
		"stat_speed_per_point":   c.StatSpeedPerPoint,
		"stat_attack_base":       c.StatAttackBase,
		"stat_attack_per_point":  c.StatAttackPerPoint,
		"stat_defense_per_point": c.StatDefensePerPoint,
	}
}

func (h *AdminHandler) gameConfigResponseLocked() map[string]interface{} {
	h.ensureConfigStateLocked()
	desired, canonical, err := h.desiredGameConfigLocked()
	values := gameConfigValues(desired)
	activeValues := gameConfigValues(h.activeConfig)
	pendingValues := make(map[string]interface{})
	pendingActiveValues := make(map[string]interface{})
	if err == nil {
		for key := range canonical {
			if !configValueEqual(activeValues[key], values[key]) {
				pendingValues[key] = values[key]
				pendingActiveValues[key] = activeValues[key]
			}
		}
	}
	persistence := map[string]interface{}{
		"available":        h.adminOverridePersistenceAvailable(),
		"activation":       "server_restart",
		"restart_required": len(pendingValues) > 0,
		"overridden_keys":  sortedMapKeys(h.gameOverrides),
		"pending_values":   pendingValues,
		"active_values":    pendingActiveValues,
	}
	if err != nil {
		persistence["error"] = "stored overrides could not be validated"
	}
	values["_persistence"] = persistence
	return values
}
