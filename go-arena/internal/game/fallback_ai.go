package game

import "arena-server/internal/config"

// GetFallbackAction generates an AI action for a bot that did not submit one
// in time. The behavior string selects among five strategies: aggressive,
// defensive, opportunistic, territorial, and hunter. Unknown behaviors default
// to aggressive.
func GetFallbackAction(bot *BotState, nearbyBots []*BotState, behavior string) *Action {
	switch behavior {
	case "aggressive":
		return aiAggressive(bot, nearbyBots)
	case "defensive":
		return aiDefensive(bot, nearbyBots)
	case "opportunistic":
		return aiOpportunistic(bot, nearbyBots)
	case "territorial":
		return aiTerritorial(bot, nearbyBots)
	case "hunter":
		return aiHunter(bot, nearbyBots)
	default:
		return aiAggressive(bot, nearbyBots)
	}
}

// aiAggressive attacks the nearest enemy if in range, otherwise moves toward
// them. Roams toward the arena center when no enemies are visible.
func aiAggressive(bot *BotState, nearby []*BotState) *Action {
	target := findNearest(bot, nearby)
	if target == nil {
		return moveTowardCenter(bot)
	}
	if canAttack(bot, target) {
		return &Action{Type: ActionAttack, TargetID: target.BotID}
	}
	dir := directionToward(bot.Position, target.Position)
	return &Action{Type: ActionMove, Direction: dir}
}

// aiDefensive attacks if in range, retreats if enemies are close, otherwise
// roams toward center.
func aiDefensive(bot *BotState, nearby []*BotState) *Action {
	target := findNearest(bot, nearby)
	if target == nil {
		return moveTowardCenter(bot)
	}
	if canAttack(bot, target) {
		return &Action{Type: ActionAttack, TargetID: target.BotID}
	}
	if bot.Position.DistanceTo(target.Position) < config.C.ViewRadius*0.5 {
		dir := directionAway(bot.Position, target.Position)
		return &Action{Type: ActionMove, Direction: dir}
	}
	return moveTowardCenter(bot)
}

// aiOpportunistic targets weak enemies (<= 70% HP), flees from strong ones.
func aiOpportunistic(bot *BotState, nearby []*BotState) *Action {
	// Collect weak enemies.
	var weak []*BotState
	for _, b := range nearby {
		if b.HP <= b.MaxHP*0.7 {
			weak = append(weak, b)
		}
	}

	if len(weak) > 0 {
		target := findLowestHP(bot, weak)
		if target != nil && canAttack(bot, target) {
			return &Action{Type: ActionAttack, TargetID: target.BotID}
		}
		if target != nil {
			dir := directionToward(bot.Position, target.Position)
			return &Action{Type: ActionMove, Direction: dir}
		}
	}

	// Flee from strong enemies.
	var strong []*BotState
	for _, b := range nearby {
		if b.HP > b.MaxHP*0.7 {
			strong = append(strong, b)
		}
	}
	nearest := findNearest(bot, strong)
	if nearest != nil {
		dir := directionAway(bot.Position, nearest.Position)
		return &Action{Type: ActionMove, Direction: dir}
	}

	return moveTowardCenter(bot)
}

// aiTerritorial defends a territory of 2x weapon range around the bot's
// position. Attacks intruders, returns to center if drifted too far, otherwise
// idles.
func aiTerritorial(bot *BotState, nearby []*BotState) *Action {
	wc := GetWeaponConfig(bot.Weapon)
	territory := wc.Range * 2

	// Find nearest enemy within territory.
	var nearest *BotState
	nearestDist := territory + 1
	for _, b := range nearby {
		d := bot.Position.DistanceTo(b.Position)
		if d <= territory && d < nearestDist {
			nearest = b
			nearestDist = d
		}
	}

	if nearest != nil {
		if canAttack(bot, nearest) {
			return &Action{Type: ActionAttack, TargetID: nearest.BotID}
		}
		dir := directionToward(bot.Position, nearest.Position)
		return &Action{Type: ActionMove, Direction: dir}
	}

	// Drifted too far from center — return.
	center := arenaCenter()
	if bot.Position.DistanceTo(center) > territory {
		dir := directionToward(bot.Position, center)
		return &Action{Type: ActionMove, Direction: dir}
	}

	return &Action{Type: ActionIdle}
}

// aiHunter chases the enemy with the highest kill streak.
func aiHunter(bot *BotState, nearby []*BotState) *Action {
	target := findHighestStreak(bot, nearby)
	if target == nil {
		return moveTowardCenter(bot)
	}
	if canAttack(bot, target) {
		return &Action{Type: ActionAttack, TargetID: target.BotID}
	}
	dir := directionToward(bot.Position, target.Position)
	return &Action{Type: ActionMove, Direction: dir}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// findNearest returns the nearest alive bot from others, or nil.
func findNearest(bot *BotState, others []*BotState) *BotState {
	var best *BotState
	bestDist := 1e18
	for _, o := range others {
		if !o.IsAlive {
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

// findLowestHP returns the alive bot with the lowest HP from others, or nil.
func findLowestHP(_ *BotState, others []*BotState) *BotState {
	var best *BotState
	bestHP := 1e18
	for _, o := range others {
		if !o.IsAlive {
			continue
		}
		if o.HP < bestHP {
			best = o
			bestHP = o.HP
		}
	}
	return best
}

// findHighestStreak returns the alive bot with the highest kill streak, or nil.
func findHighestStreak(_ *BotState, others []*BotState) *BotState {
	var best *BotState
	bestStreak := -1
	for _, o := range others {
		if !o.IsAlive {
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
	return IsInRange(bot.Position, target.Position, wc.Range)
}

// moveTowardCenter produces a move action toward the arena center, or an idle
// action if the bot is already there.
func moveTowardCenter(bot *BotState) *Action {
	center := arenaCenter()
	dir := directionToward(bot.Position, center)
	if dir.Length() < 1e-10 {
		return &Action{Type: ActionIdle}
	}
	return &Action{Type: ActionMove, Direction: dir}
}

// arenaCenter returns the center of the arena.
func arenaCenter() Vec2 {
	return NewVec2(config.C.ArenaWidth/2, config.C.ArenaHeight/2)
}
