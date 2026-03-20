package game

import (
	"fmt"
	"math/rand"

	"arena-server/internal/config"

	"github.com/google/uuid"
)

// ProcessCombat handles ATTACK actions for all alive bots, creating projectiles
// and staff impacts as needed.
func ProcessCombat(bots map[string]*BotState, obstacles []Obstacle, projectiles *[]Projectile, staffImpacts *[]StaffImpact, grid *SpatialGrid, tickCount int, dt float64) {
	for _, bot := range bots {
		if !bot.IsAlive || bot.PendingAction == nil || bot.Frozen {
			continue
		}
		if bot.PendingAction.Type != ActionAttack {
			continue
		}

		// Block attacking during invulnerability (dodge exploit fix).
		if bot.InvulnTicks > 0 {
			bot.LastActionResult = &ActionResult{
				Action:  "attack",
				Success: false,
				Message: "cannot attack while dodging",
			}
			continue
		}

		action := bot.PendingAction
		targetID := action.TargetID

		// Validate target.
		target, ok := bots[targetID]
		if !ok {
			bot.LastActionResult = &ActionResult{
				Action:  "attack",
				Success: false,
				Message: "target not found",
			}
			continue
		}
		if !target.IsAlive {
			bot.LastActionResult = &ActionResult{
				Action:  "attack",
				Success: false,
				Target:  targetID,
				Message: "target is dead",
			}
			continue
		}
		if targetID == bot.BotID {
			bot.LastActionResult = &ActionResult{
				Action:  "attack",
				Success: false,
				Message: "cannot attack self",
			}
			continue
		}

		// Check weapon ready.
		if !IsWeaponReady(bot.CooldownRemaining) {
			bot.LastActionResult = &ActionResult{
				Action:  "attack",
				Success: false,
				Target:  targetID,
				Message: "weapon on cooldown",
			}
			continue
		}

		wc := GetWeaponConfig(bot.Weapon)

		switch wc.Special {
		case "projectile":
			processProjectileAttack(bot, target, &wc, obstacles, projectiles, tickCount)

		case "area":
			processStaffAttack(bot, target, action, &wc, obstacles, staffImpacts, tickCount)

		case "grapple":
			processGrappleAttack(bot, target, &wc, obstacles, grid, tickCount)

		default:
			processMeleeAttack(bot, target, &wc, bots, obstacles, grid, tickCount)
		}
	}
}

// processProjectileAttack handles bow attacks by spawning a projectile.
func processProjectileAttack(bot, target *BotState, wc *WeaponConfig, obstacles []Obstacle, projectiles *[]Projectile, tickCount int) {
	// Check line of sight against actual obstacle geometry.
	if LineIntersectsObstacle(bot.Position.X(), bot.Position.Y(), target.Position.X(), target.Position.Y(), obstacles) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  target.BotID,
			Message: "no line of sight",
		}
		return
	}

	dir := target.Position.Sub(bot.Position).Normalized()

	maxAge := int(config.C.ProjectileMaxAgeSecs * float64(config.C.TickRate))

	proj := Projectile{
		ID:        uuid.New().String(),
		OwnerID:   bot.BotID,
		Position:  bot.Position,
		Direction: dir,
		Speed:     config.C.ProjectileSpeed,
		Damage:    CalculateDamage(float64(wc.Damage), bot.AttackMultiplier, 0),
		Weapon:    wc.Name,
		AgeTicks:  0,
		MaxAge:    maxAge,
	}
	*projectiles = append(*projectiles, proj)

	bot.CooldownRemaining = wc.Cooldown
	bot.RoundShotsFired++

	bot.LastActionResult = &ActionResult{
		Action:  "attack",
		Success: true,
		Target:  target.BotID,
		Message: "projectile fired",
	}
}

// processStaffAttack handles staff attacks by creating a delayed area impact.
func processStaffAttack(bot, target *BotState, action *Action, wc *WeaponConfig, obstacles []Obstacle, staffImpacts *[]StaffImpact, tickCount int) {
	// Determine target position: prefer explicit TargetPosition, fall back to target bot position.
	var targetPos Vec2
	if action.TargetPosition != nil {
		targetPos = *action.TargetPosition
	} else {
		targetPos = target.Position
	}

	// Check range (grid-based).
	if !IsInRange(bot.Position, targetPos, wc.GridRange) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  target.BotID,
			Message: "out of range",
		}
		return
	}

	// Check line of sight against actual obstacle geometry.
	if LineIntersectsObstacle(bot.Position.X(), bot.Position.Y(), targetPos.X(), targetPos.Y(), obstacles) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  target.BotID,
			Message: "no line of sight",
		}
		return
	}

	impact := StaffImpact{
		OwnerID:    bot.BotID,
		Position:   targetPos,
		Radius:     float64(wc.GridParam), // grid tiles for detonation radius
		Damage:     float64(wc.Damage),
		TicksLeft:  config.C.StaffDelayTicks,
		AttackMult: bot.AttackMultiplier,
	}
	*staffImpacts = append(*staffImpacts, impact)

	bot.CooldownRemaining = wc.Cooldown

	bot.LastActionResult = &ActionResult{
		Action:  "attack",
		Success: true,
		Target:  target.BotID,
		Message: "staff impact placed",
	}
}

// processMeleeAttack handles sword, daggers, shield, and spear attacks.
func processMeleeAttack(bot, target *BotState, wc *WeaponConfig, bots map[string]*BotState, obstacles []Obstacle, grid *SpatialGrid, tickCount int) {
	// Check range (grid-based).
	if !IsInRange(bot.Position, target.Position, wc.GridRange) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  target.BotID,
			Message: "out of range",
		}
		return
	}

	// Check line of sight against actual obstacle geometry.
	if LineIntersectsObstacle(bot.Position.X(), bot.Position.Y(), target.Position.X(), target.Position.Y(), obstacles) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  target.BotID,
			Message: "no line of sight",
		}
		return
	}

	// Calculate and apply damage.
	rawDmg := CalculateDamage(float64(wc.Damage), bot.AttackMultiplier, target.DefenseReduction)
	dealt := ApplyDamage(target, bot, rawDmg, wc.Name, tickCount)

	// Apply standard knockback (1 grid tile).
	ApplyGridKnockback(target, bot.Position, 1, obstacles)

	bot.CooldownRemaining = wc.Cooldown
	bot.RoundShotsFired++
	if dealt > 0 {
		bot.RoundShotsHit++
	}

	bot.LastActionResult = &ActionResult{
		Action:  "attack",
		Success: true,
		Target:  target.BotID,
		Damage:  dealt,
		Message: fmt.Sprintf("hit with %s", wc.Name),
	}

	// Handle weapon specials.
	switch wc.Special {
	case "cleave":
		processCleave(bot, target, wc, bots, obstacles, grid, tickCount)

	case "double_strike":
		processDoubleStrike(bot, target, wc, tickCount)

	case "block":
		processBlock(target)

	case "knockback":
		processExtraKnockback(target, bot.Position, wc, obstacles)
	}
}

// processCleave deals 50% splash damage to up to 2 nearby enemies.
func processCleave(bot, primaryTarget *BotState, wc *WeaponConfig, bots map[string]*BotState, obstacles []Obstacle, grid *SpatialGrid, tickCount int) {
	// Cleave hits up to 2 additional targets within GridRange+1 tiles.
	cleaveGridRange := wc.GridRange + 1
	cleaveFloatRange := float64(cleaveGridRange) * config.C.PathfindingCellSize
	nearby := grid.QueryRadius(bot.Position, cleaveFloatRange)

	cleaveCount := 0
	for _, id := range nearby {
		if cleaveCount >= 2 {
			break
		}
		if id == bot.BotID || id == primaryTarget.BotID {
			continue
		}
		other, ok := bots[id]
		if !ok || !other.IsAlive {
			continue
		}
		if !IsInRange(bot.Position, other.Position, cleaveGridRange) {
			continue
		}

		cleaveDmg := CalculateDamage(float64(wc.Damage), bot.AttackMultiplier, other.DefenseReduction) * 0.5
		ApplyDamage(other, bot, cleaveDmg, wc.Name, tickCount)
		ApplyGridKnockback(other, bot.Position, 1, obstacles)
		cleaveCount++
	}
}

// processDoubleStrike has a 20% chance to deal a second full-damage hit.
func processDoubleStrike(bot, target *BotState, wc *WeaponConfig, tickCount int) {
	if rand.Float64() < wc.Param { // Param = 0.2
		secondDmg := CalculateDamage(float64(wc.Damage), bot.AttackMultiplier, target.DefenseReduction)
		ApplyDamage(target, bot, secondDmg, wc.Name, tickCount)
	}
}

// processBlock applies a stun to the target.
func processBlock(target *BotState) {
	target.StunTicks = config.C.StunDurationTicks
}

// processExtraKnockback applies additional knockback from spear hits.
func processExtraKnockback(target *BotState, attackerPos Vec2, wc *WeaponConfig, obstacles []Obstacle) {
	// Spear knockback: push 1 additional tile.
	ApplyGridKnockback(target, attackerPos, 1, obstacles)
}

// processGrappleAttack handles grapple weapon: ranged hit that pulls attacker to target.
func processGrappleAttack(bot, target *BotState, wc *WeaponConfig, obstacles []Obstacle, grid *SpatialGrid, tickCount int) {
	if !IsInRange(bot.Position, target.Position, wc.GridRange) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  target.BotID,
			Message: "out of range",
		}
		return
	}

	if LineIntersectsObstacle(bot.Position.X(), bot.Position.Y(), target.Position.X(), target.Position.Y(), obstacles) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  target.BotID,
			Message: "no line of sight",
		}
		return
	}

	// Deal damage
	rawDmg := CalculateDamage(float64(wc.Damage), bot.AttackMultiplier, target.DefenseReduction)
	dealt := ApplyDamage(target, bot, rawDmg, wc.Name, tickCount)

	// Pull attacker to within 1 tile of target
	if ActiveTerrain != nil {
		targetCell := ActiveTerrain.WorldToGrid(target.Position)
		botCell := ActiveTerrain.WorldToGrid(bot.Position)
		// Find the cell adjacent to target that's closest to bot
		bestCell := botCell
		bestDist := 999
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				if dx == 0 && dy == 0 {
					continue
				}
				nc := [2]int{targetCell[0] + dx, targetCell[1] + dy}
				if ActiveTerrain.IsBlocked(nc[0], nc[1]) {
					continue
				}
				d := GridDistance(botCell, nc)
				if d < bestDist {
					bestDist = d
					bestCell = nc
				}
			}
		}
		if bestCell != botCell {
			bot.Position = ActiveTerrain.GridToWorld(bestCell)
			bot.LastValidPosition = bot.Position
			grid.Update(bot.BotID, bot.Position)
		}
	}

	bot.CooldownRemaining = wc.Cooldown
	bot.RoundShotsFired++
	if dealt > 0 {
		bot.RoundShotsHit++
	}

	bot.LastActionResult = &ActionResult{
		Action:  "attack",
		Success: true,
		Target:  target.BotID,
		Damage:  dealt,
		Message: "grappled target",
	}
}

// ProcessStaffImpacts ticks down staff impacts and applies damage when they detonate.
func ProcessStaffImpacts(staffImpacts *[]StaffImpact, bots map[string]*BotState, tickCount int) {
	active := (*staffImpacts)[:0]

	for i := range *staffImpacts {
		impact := &(*staffImpacts)[i]
		impact.TicksLeft--

		if impact.TicksLeft <= 0 {
			// Detonate: damage all bots within grid radius except the owner.
			impactGridRadius := int(impact.Radius) // Radius stores grid tiles
			for _, bot := range bots {
				if !bot.IsAlive || bot.BotID == impact.OwnerID {
					continue
				}

				if !IsInRange(bot.Position, impact.Position, impactGridRadius) {
					continue
				}

				// Look up the attacker for stat tracking.
				attacker, ok := bots[impact.OwnerID]
				if !ok {
					continue
				}

				dmg := CalculateDamage(impact.Damage, impact.AttackMult, bot.DefenseReduction)
				ApplyDamage(bot, attacker, dmg, "staff", tickCount)
			}
			// Impact is consumed; do not keep it.
		} else {
			active = append(active, *impact)
		}
	}

	*staffImpacts = active
}

// ProcessShoves handles all SHOVE actions for the current tick.
// Shoves deal no damage but knock the target back far and apply a short stun.
func ProcessShoves(bots map[string]*BotState, obstacles []Obstacle) {

	for _, bot := range bots {
		if !bot.IsAlive || bot.PendingAction == nil {
			continue
		}
		if bot.PendingAction.Type != ActionShove {
			continue
		}
		if bot.StunTicks > 0 || bot.Frozen {
			bot.LastActionResult = &ActionResult{
				Action:  "shove",
				Success: false,
				Message: "stunned",
			}
			continue
		}

		targetID := bot.PendingAction.TargetID
		target, ok := bots[targetID]
		if !ok || !target.IsAlive {
			bot.LastActionResult = &ActionResult{
				Action:  "shove",
				Success: false,
				Message: "invalid target",
			}
			continue
		}
		if bot.ShoveCooldown > 0 {
			bot.LastActionResult = &ActionResult{
				Action:  "shove",
				Success: false,
				Message: "shove on cooldown",
			}
			continue
		}

		// Range check (grid-based: must be adjacent, 1 tile).
		if !IsInRange(bot.Position, target.Position, 1) {
			bot.LastActionResult = &ActionResult{
				Action:  "shove",
				Success: false,
				Target:  targetID,
				Message: "out of range",
			}
			continue
		}

		// Can't shove invulnerable targets.
		if target.InvulnTicks > 0 {
			bot.LastActionResult = &ActionResult{
				Action:  "shove",
				Success: false,
				Target:  targetID,
				Message: "target dodging",
			}
			continue
		}

		// Apply knockback (2 grid tiles for shove).
		ApplyGridKnockback(target, bot.Position, 2, obstacles)

		// Apply stun.
		if config.C.ShoveStunTicks > target.StunTicks {
			target.StunTicks = config.C.ShoveStunTicks
		}

		bot.ShoveCooldown = config.C.ShoveCooldown

		bot.LastActionResult = &ActionResult{
			Action:  "shove",
			Success: true,
			Target:  targetID,
			Message: "shoved",
		}
	}
}
