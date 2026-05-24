package game

import (
	"math"
	"sort"

	"arena-server/internal/config"
)

// round1 rounds a float64 to 1 decimal place, matching Python's round(x, 1).
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

// botTargetID returns the target_id from the bot's pending action, or empty string.
func botTargetID(bot *BotState) string {
	if bot.PendingAction != nil {
		return bot.PendingAction.TargetID
	}
	return ""
}

func botTargetPosition(bot *BotState) *Vec2 {
	if bot.PendingAction != nil && bot.PendingAction.TargetPosition != nil {
		pos := *bot.PendingAction.TargetPosition
		return &pos
	}
	return nil
}

func bowChargeLevel(bot *BotState) float64 {
	if bot == nil || bot.Weapon != "bow" {
		return 0
	}
	maxTicks := config.C.BowChargeMaxTicks
	if maxTicks <= 0 {
		maxTicks = 6
	}
	ticks := bot.BowChargeTicks
	if ticks < 0 {
		ticks = 0
	}
	if ticks > maxTicks {
		ticks = maxTicks
	}
	return round1(float64(ticks) / float64(maxTicks))
}

func chargedShotReady(bot *BotState) bool {
	if bot == nil || bot.Weapon != "bow" {
		return false
	}
	readyTicks := config.C.BowChargeReadyTicks
	if readyTicks <= 0 {
		readyTicks = 1
	}
	return bot.BowChargeTicks >= readyTicks
}

func isRearExposedToObserver(observerPos Vec2, bot *BotState) bool {
	if bot == nil {
		return false
	}
	targetFacing := bot.Facing.Normalized()
	if targetFacing.Length() <= 0 {
		return false
	}
	fromTarget := observerPos.Sub(bot.Position).Normalized()
	if fromTarget.Length() <= 0 {
		return false
	}
	return targetFacing.X()*fromTarget.X()+targetFacing.Y()*fromTarget.Y() <= config.C.DaggerBackstabDotThreshold
}

// posToGrid converts a Vec2 to grid coordinates [col, row].
// Returns [0, 0] if no terrain grid is active.
func posToGrid(pos Vec2) [2]int {
	if ActiveTerrain != nil {
		return ActiveTerrain.WorldToGrid(pos)
	}
	return [2]int{int(pos.X()), int(pos.Y())}
}

// BuildBotNearbyView builds the protocol-compatible map for a bot as seen by
// a nearby observer. Position is reported as grid coordinates.
// observerPos is the world-space position of the observing bot (for LOS checks).
func BuildBotNearbyView(bot *BotState, observerPos Vec2) map[string]interface{} {
	var lastAction interface{}
	if bot.LastActionResult != nil {
		lastAction = bot.LastActionResult.Action
	}

	gridPos := posToGrid(bot.Position)

	// Line of sight check.
	hasLOS := ActiveTerrain != nil && !ActiveTerrain.GridLineBlocked(observerPos, bot.Position)

	// Weapon attack range.
	wc := GetWeaponConfig(bot.Weapon)

	// Threat score: (kills * 10 + hp_percent * 5)
	threatScore := round1(float64(bot.RoundKills)*10 + (bot.HP/bot.MaxHP)*500)

	return map[string]interface{}{
		"type":         "bot",
		"id":           bot.BotID,
		"bot_id":       bot.BotID,
		"name":         bot.Name,
		"position":     [2]int{gridPos[0], gridPos[1]},
		"hp":           math.Round(bot.HP),
		"max_hp":       math.Round(bot.MaxHP),
		"weapon":       bot.Weapon,
		"is_alive":     bot.IsAlive,
		"avatar_color": bot.AvatarColor,
		"last_action":  lastAction,
		"action":       lastAction,
		"target_id":    botTargetID(bot),
		"is_dodging":   bot.InvulnTicks > 0,
		"is_stunned":   bot.StunTicks > 0,
		"facing":       bot.Facing,
		"recently_disrupted_ticks": bot.RecentlyDisruptedTicks,
		"brace_ready":  bot.Weapon == "spear" && isBraceReady(bot),
		"bow_charge_ticks": bot.BowChargeTicks,
		"bow_charge_level": bowChargeLevel(bot),
		"charged_shot_ready": chargedShotReady(bot),
		"has_los":      hasLOS,
		"attack_range": wc.GridRange,
		"can_attack":   bot.CooldownRemaining <= 0,
		"rear_exposed": isRearExposedToObserver(observerPos, bot),
		"near_impact_surface": isNearImpactSurface(bot.Position, nil),
		"threat_score": threatScore,
	}
}

// BuildPickupNearbyView builds the protocol-compatible map for a pickup.
// Position is reported as grid coordinates.
func BuildPickupNearbyView(p Pickup) map[string]interface{} {
	gridPos := posToGrid(p.Position)

	return map[string]interface{}{
		"type":        "pickup",
		"id":          p.ID,
		"pickup_id":   p.ID,
		"pickup_type": string(p.Type),
		"position":    [2]int{gridPos[0], gridPos[1]},
	}
}

// BuildYourState builds the full your_state dict sent to a bot each tick.
// All positions and distances are reported in grid coordinates/tiles.
func BuildYourState(bot *BotState, arena *ArenaMap, killFeed *KillFeed, tickCount int) map[string]interface{} {
	// Effective speed (apply speed boost effects).
	effectiveSpeed := bot.Speed
	for _, eff := range bot.ActiveEffects {
		if eff.Name == "speed_boost" {
			effectiveSpeed *= eff.Value
		}
	}

	// Effects list.
	effects := make([]map[string]interface{}, 0, len(bot.ActiveEffects))
	for _, eff := range bot.ActiveEffects {
		effects = append(effects, map[string]interface{}{
			"name":  eff.Name,
			"ticks": eff.RemainingTicks,
		})
	}

	// Last action result.
	var lastActionResult interface{}
	if bot.LastActionResult != nil {
		lastActionResult = bot.LastActionResult
	}

	// Hits received.
	hitsReceived := make([]interface{}, 0, len(bot.HitsReceived))
	for _, hr := range bot.HitsReceived {
		hitsReceived = append(hitsReceived, map[string]interface{}{
			"attacker_id": hr.AttackerID,
			"damage":      hr.Damage,
			"weapon":      hr.Weapon,
		})
	}

	// Kill feed (last 5).
	recentKills := killFeed.GetRecent(5)
	killFeedEntries := make([]map[string]interface{}, 0, len(recentKills))
	for _, kfe := range recentKills {
		killFeedEntries = append(killFeedEntries, map[string]interface{}{
			"killer": kfe.Killer,
			"victim": kfe.Victim,
			"weapon": kfe.Weapon,
			"tick":   kfe.Tick,
		})
	}

	// Zone info in grid coordinates.
	inSafeZone := arena.IsInZone(bot.Position)
	distToEdge := arena.DistanceToZoneEdge(bot.Position)

	gridPos := posToGrid(bot.Position)
	zoneCenter := posToGrid(arena.ZoneCenter)
	zoneTargetCenter := posToGrid(arena.ZoneTargetCenter)

	var cellSize float64 = 20
	if ActiveTerrain != nil {
		cellSize = ActiveTerrain.CellSize
	}
	zoneRadiusTiles := int(math.Round(arena.ZoneRadius / cellSize))
	zoneTargetRadiusTiles := int(math.Round(arena.ZoneTargetRadius / cellSize))
	distToEdgeTiles := int(math.Round(distToEdge / cellSize))

	state := map[string]interface{}{
		"bot_id":             bot.BotID,
		"position":           [2]int{gridPos[0], gridPos[1]},
		"hp":                 math.Round(bot.HP),
		"max_hp":             math.Round(bot.MaxHP),
		"speed":              round1(effectiveSpeed),
		"weapon":             bot.Weapon,
		"cooldown_remaining": round1(bot.CooldownRemaining),
		"weapon_ready":       bot.CooldownRemaining <= 0,
		"is_alive":           bot.IsAlive,
		"kill_streak":        bot.KillStreak,
		"round_kills":        bot.RoundKills,
		"dodge_cooldown":     bot.DodgeCooldown,
		"invuln_ticks":       bot.InvulnTicks,
		"stun_ticks":         bot.StunTicks,
		"facing":             bot.Facing,
		"recently_disrupted_ticks": bot.RecentlyDisruptedTicks,
		"brace_ready":        bot.Weapon == "spear" && isBraceReady(bot),
		"bow_charge_ticks":   bot.BowChargeTicks,
		"bow_charge_level":   bowChargeLevel(bot),
		"charged_shot_ready": chargedShotReady(bot),
		"shield_absorb":      bot.ShieldAbsorb,
		"hazard_key_active":  hasEffectByName(bot.ActiveEffects, "hazard_key"),
		"hazard_key_ticks":   effectRemainingTicks(bot.ActiveEffects, "hazard_key"),
		"relay_battery_active": hasEffectByName(bot.ActiveEffects, "relay_battery"),
		"relay_battery_ticks": effectRemainingTicks(bot.ActiveEffects, "relay_battery"),
		"effects":            effects,
		"last_action_result": lastActionResult,
		"hits_received":      hitsReceived,
		"kill_feed":          killFeedEntries,
		// Zone info (in grid tiles).
		"in_safe_zone":          inSafeZone,
		"distance_to_zone_edge": distToEdgeTiles,
		"zone_radius":           zoneRadiusTiles,
		"zone_center":           [2]int{zoneCenter[0], zoneCenter[1]},
		"zone_target_center":    [2]int{zoneTargetCenter[0], zoneTargetCenter[1]},
		"zone_target_radius":    zoneTargetRadiusTiles,
		// New gameplay state.
		"is_bounty_target":    bot.IsBountyTarget,
		"bounty_token_bonus":  bot.BountyTokenBonus,
		"mine_count":          bot.MineCount,
		"gravity_well_charge": bot.GravityWellCharge,
		"grapple_charges":     bot.GrappleCharges,
		"grapple_cooldown":    round1(bot.GrappleCooldown),
	}

	return state
}

// BuildSpectatorState builds the full arena snapshot for spectator clients.
// Spectators still receive float positions for smooth rendering.
func BuildSpectatorState(bots map[string]*BotState, arena *ArenaMap, pickups []Pickup, killFeed *KillFeed, tickCount int, roundStartTick int, waitingBots map[string]*BotState, roundModifier RoundModifier) SpectatorState {
	botViews := make([]map[string]interface{}, 0, len(bots))
	for _, bot := range bots {
		// Spectators get float positions for smooth canvas rendering.
		var lastAction interface{}
		if bot.LastActionResult != nil {
			lastAction = bot.LastActionResult.Action
		}
		botViews = append(botViews, map[string]interface{}{
			"type":         "bot",
			"id":           bot.BotID,
			"bot_id":       bot.BotID,
			"name":         bot.Name,
			"position":     bot.Position,
			"hp":           math.Round(bot.HP),
			"max_hp":       math.Round(bot.MaxHP),
			"weapon":       bot.Weapon,
			"is_alive":     bot.IsAlive,
			"avatar_color": bot.AvatarColor,
			"last_action":  lastAction,
			"action":       lastAction,
			"target_id":    botTargetID(bot),
			"target_position": botTargetPosition(bot),
			"is_dodging":   bot.InvulnTicks > 0,
			"is_stunned":   bot.StunTicks > 0,
			"cooldown_remaining": round1(bot.CooldownRemaining),
			"facing":       bot.Facing,
			"recently_disrupted_ticks": bot.RecentlyDisruptedTicks,
			"brace_ready":  bot.Weapon == "spear" && isBraceReady(bot),
			"bow_charge_ticks": bot.BowChargeTicks,
			"bow_charge_level": bowChargeLevel(bot),
			"charged_shot_ready": chargedShotReady(bot),
			"kill_streak":        bot.KillStreak,
			"round_kills":        bot.RoundKills,
			"shield_absorb":      round1(bot.ShieldAbsorb),
			"hazard_key_active":  hasEffectByName(bot.ActiveEffects, "hazard_key"),
			"hazard_key_ticks":   effectRemainingTicks(bot.ActiveEffects, "hazard_key"),
			"relay_battery_active": hasEffectByName(bot.ActiveEffects, "relay_battery"),
			"relay_battery_ticks": effectRemainingTicks(bot.ActiveEffects, "relay_battery"),
			"mine_count":         bot.MineCount,
			"grapple_charges":    bot.GrappleCharges,
			"grapple_cooldown":   round1(bot.GrappleCooldown),
			"gravity_well_charge": bot.GravityWellCharge,
			"is_bounty_target":   bot.IsBountyTarget,
			"bounty_token_bonus": bot.BountyTokenBonus,
		})
	}
	sort.Slice(botViews, func(i, j int) bool {
		return botViews[i]["name"].(string) < botViews[j]["name"].(string)
	})

	pickupViews := make([]map[string]interface{}, 0, len(pickups))
	for _, p := range pickups {
		pickupViews = append(pickupViews, map[string]interface{}{
			"type":        "pickup",
			"id":          p.ID,
			"pickup_id":   p.ID,
			"pickup_type": string(p.Type),
			"position":    p.Position,
		})
	}

	recentKills := killFeed.GetAll()
	killFeedViews := make([]map[string]interface{}, 0, len(recentKills))
	for _, kfe := range recentKills {
		killFeedViews = append(killFeedViews, map[string]interface{}{
			"killer": kfe.Killer,
			"victim": kfe.Victim,
			"weapon": kfe.Weapon,
			"tick":   kfe.Tick,
		})
	}

	safeZone := map[string]interface{}{
		"center":        arena.ZoneCenter,
		"radius":        round1(arena.ZoneRadius),
		"target_center": arena.ZoneTargetCenter,
		"target_radius": round1(arena.ZoneTargetRadius),
	}

	// Send collision-accurate obstacles: expand by botRadius padding and snap
	// to grid cell boundaries so the visual walls match exactly what blocks
	// movement on the server.
	visObstacles := arena.Obstacles
	if ActiveTerrain != nil {
		visObstacles = make([]Obstacle, len(arena.Obstacles))
		cs := ActiveTerrain.CellSize
		pad := config.C.BotRadius
		for i, obs := range arena.Obstacles {
			ox := obs.X - pad
			oy := obs.Y - pad
			ow := obs.Width + 2*pad
			oh := obs.Height + 2*pad
			// Snap to grid cell boundaries.
			minCX := math.Floor(ox / cs)
			minCY := math.Floor(oy / cs)
			maxCX := math.Floor((ox+ow)/cs) + 1
			maxCY := math.Floor((oy+oh)/cs) + 1
			visObstacles[i] = Obstacle{
				X:      minCX * cs,
				Y:      minCY * cs,
				Width:  (maxCX - minCX) * cs,
				Height: (maxCY - minCY) * cs,
			}
		}
	}

	// Build waiting bots list for the lobby tab during active rounds.
	var waitingViews []map[string]interface{}
	if len(waitingBots) > 0 {
		waitingViews = make([]map[string]interface{}, 0, len(waitingBots))
		for _, bot := range waitingBots {
			waitingViews = append(waitingViews, map[string]interface{}{
				"name":         bot.Name,
				"avatar_color": bot.AvatarColor,
				"weapon":       bot.Weapon,
			})
		}
		sort.Slice(waitingViews, func(i, j int) bool {
			return waitingViews[i]["name"].(string) < waitingViews[j]["name"].(string)
		})
	}

	return SpectatorState{
		Type:        "arena_state",
		Tick:        tickCount,
		RoundTick:   tickCount - roundStartTick,
		RoundModifier: string(roundModifier),
		Bots:        botViews,
		SafeZone:    safeZone,
		Pickups:     pickupViews,
		KillFeed:    killFeedViews,
		Obstacles:   visObstacles,
		WaitingBots: waitingViews,
	}
}
