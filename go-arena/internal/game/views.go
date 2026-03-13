package game

import (
	"math"
)

// round1 rounds a float64 to 1 decimal place, matching Python's round(x, 1).
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

// BuildBotNearbyView builds the protocol-compatible map for a bot as seen by
// a nearby observer.
func BuildBotNearbyView(bot *BotState) map[string]interface{} {
	var lastAction interface{}
	if bot.LastActionResult != nil {
		lastAction = bot.LastActionResult.Action
	}

	return map[string]interface{}{
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
		"is_dodging":   bot.InvulnTicks > 0,
		"is_stunned":   bot.StunTicks > 0,
	}
}

// BuildPickupNearbyView builds the protocol-compatible map for a pickup.
func BuildPickupNearbyView(p Pickup) map[string]interface{} {
	return map[string]interface{}{
		"type":        "pickup",
		"id":          p.ID,
		"pickup_id":   p.ID,
		"pickup_type": string(p.Type),
		"position":    p.Position,
	}
}

// BuildYourState builds the full your_state dict sent to a bot each tick.
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

	// Zone info.
	inSafeZone := arena.IsInZone(bot.Position)
	distToEdge := arena.DistanceToZoneEdge(bot.Position)

	state := map[string]interface{}{
		"bot_id":             bot.BotID,
		"position":           bot.Position,
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
		"shield_absorb":      bot.ShieldAbsorb,
		"effects":            effects,
		"last_action_result": lastActionResult,
		"hits_received":      hitsReceived,
		"kill_feed":          killFeedEntries,
		// Zone info.
		"in_safe_zone":          inSafeZone,
		"distance_to_zone_edge": round1(distToEdge),
		"zone_radius":           round1(arena.ZoneRadius),
		"zone_center":           arena.ZoneCenter,
		"zone_target_center":    arena.ZoneTargetCenter,
		"zone_target_radius":    round1(arena.ZoneTargetRadius),
	}

	return state
}

// BuildSpectatorState builds the full arena snapshot for spectator clients.
func BuildSpectatorState(bots map[string]*BotState, arena *ArenaMap, pickups []Pickup, killFeed *KillFeed, tickCount int) SpectatorState {
	botViews := make([]map[string]interface{}, 0, len(bots))
	for _, bot := range bots {
		botViews = append(botViews, BuildBotNearbyView(bot))
	}

	pickupViews := make([]map[string]interface{}, 0, len(pickups))
	for _, p := range pickups {
		pickupViews = append(pickupViews, BuildPickupNearbyView(p))
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

	return SpectatorState{
		Type:      "arena_state",
		Tick:      tickCount,
		Bots:      botViews,
		SafeZone:  safeZone,
		Pickups:   pickupViews,
		KillFeed:  killFeedViews,
		Obstacles: arena.Obstacles,
	}
}
