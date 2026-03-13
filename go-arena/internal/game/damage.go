package game

import (
	"arena-server/internal/config"
)

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

	// Wall slam damage.
	if wallHit {
		target.HP -= config.C.KnockbackWallDamage
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

	// Invulnerability ticks.
	if bot.InvulnTicks > 0 {
		bot.InvulnTicks--
	}

	// Dodge cooldown ticks.
	if bot.DodgeCooldown > 0 {
		bot.DodgeCooldown--
	}
}

// TickEffects decrements effect timers and removes expired effects.
func TickEffects(bot *BotState) {
	alive := bot.ActiveEffects[:0]
	for i := range bot.ActiveEffects {
		bot.ActiveEffects[i].RemainingTicks--
		if bot.ActiveEffects[i].RemainingTicks > 0 {
			alive = append(alive, bot.ActiveEffects[i])
		}
	}
	bot.ActiveEffects = alive
}
