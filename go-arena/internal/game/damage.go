package game

import (
	"arena-server/internal/config"
)

func effectCooldownMultiplier(bot *BotState) float64 {
	if bot == nil {
		return 1
	}
	mult := 1.0
	for _, eff := range bot.ActiveEffects {
		if eff.Name == "cooldown_shard" && eff.Value > 0 {
			mult *= eff.Value
		}
	}
	if mult < 0.1 {
		return 0.1
	}
	return mult
}

func effectDamageMultiplier(bot *BotState) float64 {
	if bot == nil {
		return 1
	}
	mult := 1.0
	for _, eff := range bot.ActiveEffects {
		switch eff.Name {
		case "damage_boost", "capture_pad_power":
			if eff.Value > 0 {
				mult *= eff.Value
			}
		}
	}
	return mult
}

func scaledCooldownTicks(base int, mult float64) int {
	value := int(float64(base) * mult)
	if value < 1 {
		return 1
	}
	return value
}

// ApplyDamage applies damage from attacker to target, respecting invulnerability,
// shield passive, and shield absorb. Returns the actual damage dealt.
func ApplyDamage(target, attacker *BotState, baseDamage float64, weapon string, tickCount int) float64 {
	// Invulnerable targets take no damage.
	if target.InvulnTicks > 0 {
		return 0
	}

	actual := baseDamage

	// Shield weapon passive: 50% damage reduction when the target wields a shield.
	if target.Weapon == "shield" {
		shieldCfg := GetWeaponConfig("shield")
		actual *= (1.0 - shieldCfg.Param) // Param is 0.5 -> 50% reduction
	}

	// Shield absorb: soak damage before it reaches HP.
	if target.ShieldAbsorb > 0 {
		absorb := actual
		if absorb > target.ShieldAbsorb {
			absorb = target.ShieldAbsorb
		}
		actual -= absorb
		target.ShieldAbsorb -= absorb
	}

	// Apply to HP.
	target.HP -= actual

	// Track round stats.
	target.RoundDamageTaken += actual
	attacker.RoundDamageDealt += actual

	// Kill attribution.
	target.LastDamagedBy = attacker.BotID
	target.LastDamageTick = tickCount

	// Record the hit.
	target.HitsReceived = append(target.HitsReceived, HitRecord{
		AttackerID: attacker.BotID,
		Damage:     actual,
		Weapon:     weapon,
	})

	// Emit damage event to dashboard.
	if GameEventHook != nil {
		dist := attacker.Position.DistanceTo(target.Position)
		GameEventHook("damage", map[string]interface{}{
			"attacker_id":   attacker.BotID,
			"attacker_name": attacker.Name,
			"target_id":     target.BotID,
			"target_name":   target.Name,
			"damage":        round1(actual),
			"base_damage":   round1(baseDamage),
			"weapon":        weapon,
			"target_hp":     round1(target.HP),
			"distance":      round1(dist),
			"tick":          tickCount,
		})
	}

	return actual
}

// ApplyHitKnockback pushes the target away from the attacker position.
func ApplyHitKnockback(target *BotState, attackerPos Vec2, knockbackDist float64, obstacles []Obstacle) {
	dir := target.Position.Sub(attackerPos).Normalized()
	if dir.Length() < 1e-10 {
		// Target is at the exact same position; push in an arbitrary direction.
		dir = NewVec2(1, 0)
	}

	newX := target.Position.X() + dir.X()*knockbackDist
	newY := target.Position.Y() + dir.Y()*knockbackDist

	// Slide along obstacles.
	newX, newY = SlideAlongObstacle(target.Position.X(), target.Position.Y(), newX, newY, obstacles, config.C.BotRadius)

	// Clamp to arena bounds and check for wall slam.
	wallHit := false

	if newX < config.C.BotRadius {
		newX = config.C.BotRadius
		wallHit = true
	}
	if newX > config.C.ArenaWidth-config.C.BotRadius {
		newX = config.C.ArenaWidth - config.C.BotRadius
		wallHit = true
	}
	if newY < config.C.BotRadius {
		newY = config.C.BotRadius
		wallHit = true
	}
	if newY > config.C.ArenaHeight-config.C.BotRadius {
		newY = config.C.ArenaHeight - config.C.BotRadius
		wallHit = true
	}

	target.Position = NewVec2(newX, newY)
	target.LastValidPosition = target.Position

	// Wall slam damage — track attribution so kills are credited.
	if wallHit {
		target.HP -= config.C.KnockbackWallDamage
		target.RoundDamageTaken += config.C.KnockbackWallDamage
	}
}

// ApplyGridKnockback pushes the target away from the attacker by gridTiles
// cells. The target is moved to the centre of the destination cell. If the
// destination is blocked, closer cells are tried. Wall-slam damage is applied
// if the target hits the arena boundary.
func ApplyGridKnockback(target *BotState, attackerPos Vec2, gridTiles int, obstacles []Obstacle) {
	if ActiveTerrain == nil {
		ApplyHitKnockback(target, attackerPos, float64(gridTiles)*config.C.PathfindingCellSize, obstacles)
		return
	}

	dir := target.Position.Sub(attackerPos).Normalized()
	if dir.Length() < 1e-10 {
		dir = NewVec2(1, 0)
	}

	dx := SnapDirection(dir.X())
	dy := SnapDirection(dir.Y())
	if dx == 0 && dy == 0 {
		dx = 1 // fallback
	}

	currentCell := ActiveTerrain.WorldToGrid(target.Position)

	// Walk cell by cell up to gridTiles; stop at the first wall or diagonal
	// corner so we never knock a bot through a blocked cell.
	destCell := currentCell
	placed := false
	prev := currentCell
	for step := 1; step <= gridTiles; step++ {
		next := [2]int{currentCell[0] + dx*step, currentCell[1] + dy*step}
		if ActiveTerrain.IsMoveBlocked(prev[0], prev[1], dx, dy) {
			break
		}
		prev = next
		destCell = next
		placed = true
	}

	if !placed {
		return // can't push anywhere
	}

	// Final validation: never place bot in a blocked cell.
	if ActiveTerrain.IsBlocked(destCell[0], destCell[1]) {
		return
	}

	wallHit := false
	if destCell[0] <= 0 || destCell[0] >= ActiveTerrain.Width-1 ||
		destCell[1] <= 0 || destCell[1] >= ActiveTerrain.Height-1 {
		wallHit = true
	}

	target.Position = ActiveTerrain.GridToWorld(destCell)
	target.LastValidPosition = target.Position

	if wallHit {
		target.HP -= config.C.KnockbackWallDamage
		target.RoundDamageTaken += config.C.KnockbackWallDamage
	}
}

// TickTimers decrements cooldowns and timed counters for a single bot.
func TickTimers(bot *BotState, dt float64) {
	// Weapon cooldown.
	bot.CooldownRemaining -= dt
	if bot.CooldownRemaining < 0 {
		bot.CooldownRemaining = 0
	}

	// Stun ticks.
	if bot.StunTicks > 0 {
		bot.StunTicks--
	}

	if bot.RecentlyDisruptedTicks > 0 {
		bot.RecentlyDisruptedTicks--
	}

	// Invulnerability ticks.
	if bot.InvulnTicks > 0 {
		bot.InvulnTicks--
	}

	// Dodge cooldown ticks.
	if bot.DodgeCooldown > 0 {
		bot.DodgeCooldown--
	}

	// Shove cooldown.
	bot.ShoveCooldown -= dt
	if bot.ShoveCooldown < 0 {
		bot.ShoveCooldown = 0
	}

	// Grapple cooldown.
	bot.GrappleCooldown -= dt
	if bot.GrappleCooldown < 0 {
		bot.GrappleCooldown = 0
	}

	// Bow charge builds while the weapon is ready and the bot is not firing.
	if bot.Weapon == "bow" {
		if bot.CooldownRemaining > 0 {
			bot.BowChargeTicks = 0
		} else if bot.PendingAction != nil && bot.PendingAction.Type == ActionAttack {
			// Fired this tick or is actively committing an attack.
			bot.BowChargeTicks = 0
		} else {
			bot.BowChargeTicks++
			if maxTicks := config.C.BowChargeMaxTicks; maxTicks > 0 && bot.BowChargeTicks > maxTicks {
				bot.BowChargeTicks = maxTicks
			}
		}
	} else {
		bot.BowChargeTicks = 0
	}

	if bot.TeleportHazardGraceTicks > 0 {
		bot.TeleportHazardGraceTicks--
	}
}

// TickEffects decrements effect timers and removes expired effects.
func TickEffects(bot *BotState) {
	alive := bot.ActiveEffects[:0]
	hasBountyToken := false
	for i := range bot.ActiveEffects {
		bot.ActiveEffects[i].RemainingTicks--
		if bot.ActiveEffects[i].RemainingTicks > 0 {
			if bot.ActiveEffects[i].Name == "bounty_token" {
				hasBountyToken = true
			}
			alive = append(alive, bot.ActiveEffects[i])
		}
	}
	bot.ActiveEffects = alive
	if !hasBountyToken {
		bot.BountyTokenBonus = 0
	}
}
