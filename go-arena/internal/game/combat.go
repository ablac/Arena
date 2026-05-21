package game

import (
	"fmt"
	"math"
	"math/rand"

	"arena-server/internal/config"

	"github.com/google/uuid"
)

// ProcessCombat handles ATTACK actions for all alive bots, creating projectiles
// and staff impacts as needed.
func ProcessCombat(bots map[string]*BotState, obstacles []Obstacle, projectiles *[]Projectile, staffImpacts *[]StaffImpact, arenaEvents *[]ArenaEvent, grid *SpatialGrid, tickCount int, dt float64) {
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
		wc := GetWeaponConfig(bot.Weapon)

		// Check weapon ready.
		if !IsWeaponReady(bot.CooldownRemaining) {
			bot.LastActionResult = &ActionResult{
				Action:  "attack",
				Success: false,
				Target:  action.TargetID,
				Message: "weapon on cooldown",
			}
			continue
		}

		targetID := action.TargetID
		var target *BotState
		if wc.Special != "area" || targetID != "" {
			// Validate target for direct-target weapons and targeted staff casts.
			var ok bool
			target, ok = bots[targetID]
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
		}

		switch wc.Special {
		case "projectile":
			processProjectileAttack(bot, target, &wc, obstacles, projectiles, tickCount)

		case "area":
			processStaffAttack(bot, target, action, &wc, obstacles, staffImpacts, tickCount)

		case "grapple":
			processGrappleAttack(bot, target, &wc, obstacles, arenaEvents, grid, tickCount)

		default:
			processMeleeAttack(bot, target, &wc, bots, obstacles, grid, tickCount)
		}
	}
}

// processProjectileAttack handles bow attacks by spawning a projectile.
func processProjectileAttack(bot, target *BotState, wc *WeaponConfig, obstacles []Obstacle, projectiles *[]Projectile, tickCount int) {
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
	if CombatLineBlocked(bot.Position, target.Position, obstacles) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  target.BotID,
			Message: "no line of sight",
		}
		return
	}

	speed := config.C.ProjectileSpeed
	if speed <= 0 {
		speed = 240
	}
	if wc.Name == "bow" {
		speed *= 1.25
	}

	aimPos := estimateProjectileAimPoint(bot, target, obstacles, speed, wc)
	dir := aimPos.Sub(bot.Position).Normalized()
	if dir.Length() <= 0 {
		dir = target.Position.Sub(bot.Position).Normalized()
	}

	maxAge := int(math.Ceil((wc.Range+2*config.C.BotRadius)/speed*float64(config.C.TickRate))) + 1
	fallbackAge := int(config.C.ProjectileMaxAgeSecs * float64(config.C.TickRate))
	if maxAge < fallbackAge {
		maxAge = fallbackAge
	}
	hitRadius := config.C.ProjectileHitRadius
	if wc.Name == "bow" {
		hitRadius += 1.5
	}

	proj := Projectile{
		ID:        uuid.New().String(),
		OwnerID:   bot.BotID,
		Position:  bot.Position,
		Direction: dir,
		Speed:     speed,
		HitRadius: hitRadius,
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

func estimateProjectileAimPoint(bot, target *BotState, obstacles []Obstacle, projectileSpeed float64, wc *WeaponConfig) Vec2 {
	if projectileSpeed <= 0 {
		return target.Position
	}

	distance := bot.Position.DistanceTo(target.Position)
	travelTime := distance / projectileSpeed
	if travelTime <= 0 {
		return target.Position
	}
	if travelTime > 0.55 {
		travelTime = 0.55
	}

	velocity := estimateBotVelocity(target)
	if velocity.Length() <= 0 {
		return target.Position
	}

	predicted := clampAimToArena(target.Position.Add(velocity.Scale(travelTime)))
	if !IsInRange(bot.Position, predicted, wc.GridRange) {
		return target.Position
	}
	if CombatLineBlocked(bot.Position, predicted, obstacles) {
		return target.Position
	}
	return predicted
}

func estimateBotVelocity(bot *BotState) Vec2 {
	if bot == nil || bot.PendingAction == nil {
		return Vec2{}
	}

	cells := 1.0
	for _, eff := range bot.ActiveEffects {
		if eff.Name == "speed_boost" {
			cells = 2.0
			break
		}
	}
	moveUnitsPerSecond := config.C.PathfindingCellSize * cells * float64(config.C.TickRate) / 2.0

	switch bot.PendingAction.Type {
	case ActionMove:
		dir := Vec2{float64(SnapDirection(bot.PendingAction.Direction.X())), float64(SnapDirection(bot.PendingAction.Direction.Y()))}.Normalized()
		return dir.Scale(moveUnitsPerSecond)
	case ActionMoveTo:
		if bot.PendingAction.TargetPosition == nil {
			return Vec2{}
		}
		dir := bot.PendingAction.TargetPosition.Sub(bot.Position).Normalized()
		return dir.Scale(moveUnitsPerSecond)
	case ActionDodge:
		dir := Vec2{float64(SnapDirection(bot.PendingAction.Direction.X())), float64(SnapDirection(bot.PendingAction.Direction.Y()))}.Normalized()
		if dir.Length() <= 0 {
			return Vec2{}
		}
		return dir.Scale(moveUnitsPerSecond * config.C.DodgeSpeedMult)
	default:
		return Vec2{}
	}
}

func clampAimToArena(pos Vec2) Vec2 {
	x := pos.X()
	y := pos.Y()
	r := config.C.BotRadius
	if x < r {
		x = r
	}
	if x > config.C.ArenaWidth-r {
		x = config.C.ArenaWidth - r
	}
	if y < r {
		y = r
	}
	if y > config.C.ArenaHeight-r {
		y = config.C.ArenaHeight - r
	}
	return Vec2{x, y}
}

// processStaffAttack handles staff attacks by creating a delayed area impact.
func processStaffAttack(bot, target *BotState, action *Action, wc *WeaponConfig, obstacles []Obstacle, staffImpacts *[]StaffImpact, tickCount int) {
	// Determine target position: prefer explicit TargetPosition, fall back to target bot position.
	var targetPos Vec2
	if action.TargetPosition != nil {
		targetPos = normalizeActionTargetPosition(*action.TargetPosition)
	} else if target != nil {
		targetPos = target.Position
	} else {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Message: "target position required",
		}
		return
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
	if CombatLineBlocked(bot.Position, targetPos, obstacles) {
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
	bot.RoundShotsFired++

	bot.LastActionResult = &ActionResult{
		Action:  "attack",
		Success: true,
		Target:  action.TargetID,
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
	if CombatLineBlocked(bot.Position, target.Position, obstacles) {
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
func processGrappleAttack(bot, target *BotState, wc *WeaponConfig, obstacles []Obstacle, arenaEvents *[]ArenaEvent, grid *SpatialGrid, tickCount int) {
	if !IsInRange(bot.Position, target.Position, wc.GridRange) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  target.BotID,
			Message: "out of range",
		}
		return
	}

	if CombatLineBlocked(bot.Position, target.Position, obstacles) {
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

	from := bot.Position
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
	if arenaEvents != nil {
		*arenaEvents = append(*arenaEvents, buildGrappleEvent(bot.BotID, target.BotID, from, target.Position, bot.Position, false, tickCount))
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
				if dealt := ApplyDamage(bot, attacker, dmg, "staff", tickCount); dealt > 0 {
					attacker.RoundShotsHit++
				}
			}
			// Impact is consumed; do not keep it.
		} else {
			active = append(active, *impact)
		}
	}

	*staffImpacts = active
}

// BuildStaffImpactView creates a protocol-compatible view of a pending staff blast.
func BuildStaffImpactView(impact StaffImpact, useGridPos bool) map[string]interface{} {
	view := map[string]interface{}{
		"type":       "staff_impact",
		"owner_id":   impact.OwnerID,
		"radius":     impact.Radius,
		"ticks_left": impact.TicksLeft,
	}
	if useGridPos {
		gridPos := posToGrid(impact.Position)
		view["position"] = [2]int{gridPos[0], gridPos[1]}
	} else {
		view["position"] = impact.Position
	}
	return view
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
