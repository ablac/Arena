package game

import "arena-server/internal/config"

// GetFallbackAction generates an AI action for a bot that did not submit one
// in time. The behavior string selects among five strategies: aggressive,
// defensive, opportunistic, territorial, and hunter. Unknown behaviors default
// to aggressive. The arena parameter provides the current zone center for
// movement decisions.
func GetFallbackAction(bot *BotState, nearbyBots []*BotState, behavior string, arena *ArenaMap) *Action {
	switch behavior {
	case "aggressive":
		return aiAggressive(bot, nearbyBots, arena)
	case "defensive":
		return aiDefensive(bot, nearbyBots, arena)
	case "opportunistic":
		return aiOpportunistic(bot, nearbyBots, arena)
	case "territorial":
		return aiTerritorial(bot, nearbyBots, arena)
	case "hunter":
		return aiHunter(bot, nearbyBots, arena)
	default:
		return aiAggressive(bot, nearbyBots, arena)
	}
}

// aiAggressive attacks the nearest enemy if in range, otherwise moves toward
// them. Only moves toward zone center if outside the safe zone.
func aiAggressive(bot *BotState, nearby []*BotState, arena *ArenaMap) *Action {
	target := findNearest(bot, nearby)
	if target == nil {
		return idleOrMoveToZone(bot, arena)
	}
	if canAttack(bot, target) {
		return &Action{Type: ActionAttack, TargetID: target.BotID}
	}
	return moveTowardPos(bot, target.Position)
}

// aiDefensive attacks if in range, retreats if enemies are close, otherwise
// holds position (moves toward zone only if outside it).
func aiDefensive(bot *BotState, nearby []*BotState, arena *ArenaMap) *Action {
	target := findNearest(bot, nearby)
	if target == nil {
		return idleOrMoveToZone(bot, arena)
	}
	// Shove enemies that are dangerously close
	if canShove(bot, target) {
		return &Action{Type: ActionShove, TargetID: target.BotID}
	}
	if canAttack(bot, target) {
		return &Action{Type: ActionAttack, TargetID: target.BotID}
	}
	if IsInRange(bot.Position, target.Position, config.C.FogRadius/2) {
		return moveAwayFrom(bot, target.Position)
	}
	return idleOrMoveToZone(bot, arena)
}

// aiOpportunistic targets weak enemies (<= 70% HP), flees from strong ones.
func aiOpportunistic(bot *BotState, nearby []*BotState, arena *ArenaMap) *Action {
	// Collect weak enemies with LOS.
	var weak []*BotState
	for _, b := range nearby {
		if b.HP <= b.MaxHP*0.7 && hasLOS(bot.Position, b.Position) {
			weak = append(weak, b)
		}
	}

	if len(weak) > 0 {
		target := findLowestHP(bot, weak)
		if target != nil && canAttack(bot, target) {
			return &Action{Type: ActionAttack, TargetID: target.BotID}
		}
		if target != nil {
			return moveTowardPos(bot, target.Position)
		}
	}

	// Flee from strong enemies with LOS.
	var strong []*BotState
	for _, b := range nearby {
		if b.HP > b.MaxHP*0.7 && hasLOS(bot.Position, b.Position) {
			strong = append(strong, b)
		}
	}
	nearest := findNearest(bot, strong)
	if nearest != nil {
		return moveAwayFrom(bot, nearest.Position)
	}

	return idleOrMoveToZone(bot, arena)
}

// aiTerritorial defends a territory of 2x weapon range around the bot's
// position. Attacks intruders, moves toward zone if outside it, otherwise idles.
func aiTerritorial(bot *BotState, nearby []*BotState, arena *ArenaMap) *Action {
	wc := GetWeaponConfig(bot.Weapon)
	territoryTiles := wc.GridRange * 2

	// Find nearest enemy within territory (grid distance).
	var nearest *BotState
	nearestDist := territoryTiles + 1
	for _, b := range nearby {
		if ActiveTerrain != nil {
			bc := ActiveTerrain.WorldToGrid(bot.Position)
			oc := ActiveTerrain.WorldToGrid(b.Position)
			d := GridDistance(bc, oc)
			if d <= territoryTiles && d < nearestDist {
				nearest = b
				nearestDist = d
			}
		} else {
			d := int(bot.Position.DistanceTo(b.Position))
			if d <= territoryTiles && d < nearestDist {
				nearest = b
				nearestDist = d
			}
		}
	}

	if nearest != nil {
		if canAttack(bot, nearest) {
			return &Action{Type: ActionAttack, TargetID: nearest.BotID}
		}
		return moveTowardPos(bot, nearest.Position)
	}

	return idleOrMoveToZone(bot, arena)
}

// aiHunter chases the enemy with the highest kill streak.
func aiHunter(bot *BotState, nearby []*BotState, arena *ArenaMap) *Action {
	target := findHighestStreak(bot, nearby)
	if target == nil {
		return idleOrMoveToZone(bot, arena)
	}
	if canAttack(bot, target) {
		return &Action{Type: ActionAttack, TargetID: target.BotID}
	}
	return moveTowardPos(bot, target.Position)
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// hasLOS returns true if the observer has line of sight to the target.
func hasLOS(from, to Vec2) bool {
	return ActiveTerrain == nil || !ActiveTerrain.GridLineBlocked(from, to)
}

// findNearest returns the nearest alive bot from others with line of sight, or nil.
func findNearest(bot *BotState, others []*BotState) *BotState {
	var best *BotState
	bestDist := 1e18
	for _, o := range others {
		if !o.IsAlive {
			continue
		}
		if !hasLOS(bot.Position, o.Position) {
			continue
		}
		d := bot.Position.DistanceTo(o.Position)
		if d < bestDist {
			best = o
			bestDist = d
		}
	}
	return best
}

// findLowestHP returns the alive bot with the lowest HP and LOS from others, or nil.
func findLowestHP(bot *BotState, others []*BotState) *BotState {
	var best *BotState
	bestHP := 1e18
	for _, o := range others {
		if !o.IsAlive {
			continue
		}
		if !hasLOS(bot.Position, o.Position) {
			continue
		}
		if o.HP < bestHP {
			best = o
			bestHP = o.HP
		}
	}
	return best
}

// findHighestStreak returns the alive bot with the highest kill streak and LOS, or nil.
func findHighestStreak(bot *BotState, others []*BotState) *BotState {
	var best *BotState
	bestStreak := -1
	for _, o := range others {
		if !o.IsAlive {
			continue
		}
		if !hasLOS(bot.Position, o.Position) {
			continue
		}
		if o.KillStreak > bestStreak {
			best = o
			bestStreak = o.KillStreak
		}
	}
	return best
}

// directionToward returns a normalized direction vector from a toward b.
func directionToward(from, to Vec2) Vec2 {
	return to.Sub(from).Normalized()
}

// directionAway returns a normalized direction vector from a away from b.
func directionAway(from, to Vec2) Vec2 {
	d := directionToward(from, to)
	return d.Scale(-1)
}

// canShove returns true if the bot's shove is off cooldown and the target is
// within shove range.
func canShove(bot *BotState, target *BotState) bool {
	if !target.IsAlive {
		return false
	}
	if bot.ShoveCooldown > 0 {
		return false
	}
	return IsInRange(bot.Position, target.Position, 1)
}

// canAttack returns true if the bot's weapon is ready and the target is alive
// and within weapon range.
func canAttack(bot *BotState, target *BotState) bool {
	if !target.IsAlive {
		return false
	}
	if !IsWeaponReady(bot.CooldownRemaining) {
		return false
	}
	wc := GetWeaponConfig(bot.Weapon)
	return IsInRange(bot.Position, target.Position, wc.GridRange)
}

// moveTowardPos returns a move_to action that uses server-side pathfinding,
// which navigates around walls instead of walking into them.
func moveTowardPos(bot *BotState, target Vec2) *Action {
	return &Action{Type: ActionMoveTo, TargetPosition: &target}
}

// moveAwayFrom moves in the opposite direction from the threat, using
// grid-aware direction to avoid walking into walls.
func moveAwayFrom(bot *BotState, threat Vec2) *Action {
	dir := directionAway(bot.Position, threat)
	if dir.Length() < 1e-10 {
		return &Action{Type: ActionIdle}
	}
	return &Action{Type: ActionMove, Direction: dir}
}

// idleOrMoveToZone idles if the bot is inside the safe zone, otherwise moves
// toward the zone center using pathfinding.
func idleOrMoveToZone(bot *BotState, arena *ArenaMap) *Action {
	if arena.IsInZone(bot.Position) {
		return &Action{Type: ActionIdle}
	}
	return moveTowardPos(bot, arena.ZoneCenter)
}
