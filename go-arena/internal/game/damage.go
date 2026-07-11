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
			continue
		}
		if eff.Name == "overdrive_core" {
			if eff.AuxValue > 0 {
				mult *= eff.AuxValue
			} else if eff.Value > 0 {
				mult *= eff.Value
			}
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
		case "damage_boost", "capture_pad_power", "overdrive_core":
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

// recordAttributedDamage keeps the exact effective hit that most recently
// damaged a bot. Death handling uses this snapshot rather than reconstructing
// a source from the attacker's current loadout or using cumulative round
// damage as if it were the killing blow.
func recordAttributedDamage(target, attacker *BotState, actual float64, source string, tickCount int) {
	if target == nil || attacker == nil || actual <= 0 {
		return
	}
	recordWeaponEngagement(attacker, target, source)
	target.LastDamagedBy = attacker.BotID
	target.LastDamageTick = tickCount
	target.LastDamageSource = source
	target.LastDamageAmount = actual
	target.HitsReceived = append(target.HitsReceived, HitRecord{
		AttackerID: attacker.BotID,
		Damage:     actual,
		Weapon:     source,
	})
}

// damageSourceMatchesEquippedWeapon separates the weapon's own output from
// universal abilities, mines, pickups, bounty points, and environmental
// shoves. Derived effects remain weapon output because their tuning is part of
// the same weapon config and should move with it.
func damageSourceMatchesEquippedWeapon(attacker *BotState, source string) bool {
	if attacker == nil || source == "" {
		return false
	}
	if source == attacker.Weapon {
		return true
	}
	switch attacker.Weapon {
	case "staff":
		return source == "staff_burn"
	case "grapple":
		return source == "grapple_slam"
	default:
		return false
	}
}

// recordWeaponEngagement captures only opponents actually affected by the
// equipped weapon. The auto-balancer uses this per-round set for cohort
// diversity; unrelated bots present in the arena must not count as evidence.
func recordWeaponEngagement(attacker, target *BotState, source string) {
	if attacker == nil || target == nil || target.BotID == "" || target.BotID == attacker.BotID ||
		!damageSourceMatchesEquippedWeapon(attacker, source) {
		return
	}
	if attacker.RoundWeaponOpponentIDs == nil {
		attacker.RoundWeaponOpponentIDs = make(map[string]struct{})
	}
	attacker.RoundWeaponOpponentIDs[target.BotID] = struct{}{}
}

// ApplyDamage applies damage from attacker to target, respecting invulnerability,
// shield passive, and shield absorb. Returns the actual damage dealt.
func ApplyDamage(target, attacker *BotState, baseDamage float64, weapon string, tickCount int) float64 {
	// Invulnerable targets take no damage.
	if target.InvulnTicks > 0 {
		return 0
	}

	// Team modes: no friendly fire unless the ruleset allows it.
	if !ActiveModeRules.CanDamage(attacker, target) {
		return 0
	}

	// Sudden death doubles all incoming damage so closed-zone fights resolve.
	actual := baseDamage * SuddenDeathDamageMultiplier()

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
	if damageSourceMatchesEquippedWeapon(attacker, weapon) {
		attacker.RoundWeaponDamageDealt += actual
	}

	recordAttributedDamage(target, attacker, actual, weapon, tickCount)

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
	applyHitKnockback(target, attackerPos, knockbackDist, obstacles, nil, "", 0)
}

func applyHitKnockback(target *BotState, attackerPos Vec2, knockbackDist float64, obstacles []Obstacle, attacker *BotState, source string, tickCount int) {
	// Invulnerable (dodging) bots are immune to displacement and the wall-slam
	// damage it can cause, matching the shove and projectile rules.
	if target.InvulnTicks > 0 {
		return
	}
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
		applyWallSlamDamage(target, attacker, source, tickCount)
	}
}

func applyWallSlamDamage(target, attacker *BotState, source string, tickCount int) {
	damage := config.C.KnockbackWallDamage * SuddenDeathDamageMultiplier()
	if target == nil || damage <= 0 || target.InvulnTicks > 0 {
		return
	}
	target.HP -= damage
	target.RoundDamageTaken += damage
	if attacker != nil && attacker != target {
		attacker.RoundDamageDealt += damage
		if damageSourceMatchesEquippedWeapon(attacker, source) {
			attacker.RoundWeaponDamageDealt += damage
		}
		recordAttributedDamage(target, attacker, damage, source, tickCount)
	}
}

// ApplyGridKnockback pushes the target away from the attacker by gridTiles
// cells. The target is moved to the centre of the destination cell. If the
// destination is blocked, closer cells are tried. Wall-slam damage is applied
// if the target hits the arena boundary.
func ApplyGridKnockback(target *BotState, attackerPos Vec2, gridTiles int, obstacles []Obstacle) {
	applyGridKnockback(target, attackerPos, gridTiles, obstacles, nil, "", 0)
}

// ApplyAttributedGridKnockback records any resulting wall-slam damage as the
// supplied attacker's hit. This is required for non-damaging shoves, which do
// not have an earlier ApplyDamage call to establish correct kill attribution.
func ApplyAttributedGridKnockback(target, attacker *BotState, attackerPos Vec2, gridTiles int, obstacles []Obstacle, source string, tickCount int) {
	applyGridKnockback(target, attackerPos, gridTiles, obstacles, attacker, source, tickCount)
}

func applyGridKnockback(target *BotState, attackerPos Vec2, gridTiles int, obstacles []Obstacle, attacker *BotState, source string, tickCount int) {
	// Same invulnerability rule as applyHitKnockback (which also covers the
	// ActiveTerrain == nil fallback below).
	if target.InvulnTicks > 0 {
		return
	}
	if ActiveTerrain == nil {
		applyHitKnockback(target, attackerPos, float64(gridTiles)*config.C.PathfindingCellSize, obstacles, attacker, source, tickCount)
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
		applyWallSlamDamage(target, attacker, source, tickCount)
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
