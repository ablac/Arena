package game

import (
	"fmt"
	"math"

	"arena-server/internal/config"

	"github.com/google/uuid"
)

// ProcessCombat handles ATTACK actions for all alive bots, creating projectiles
// and staff impacts as needed.
func ProcessCombat(bots map[string]*BotState, obstacles []Obstacle, projectiles *[]Projectile, staffImpacts *[]StaffImpact, arenaEvents *[]ArenaEvent, grid *SpatialGrid, tickCount int, dt float64) {
	for _, bot := range bots {
		if !bot.IsAlive || bot.PendingAction == nil {
			continue
		}
		if bot.PendingAction.Type != ActionAttack {
			continue
		}
		if rejectControlledAction(bot, "attack", true) {
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
		if wc.Special == "area" && targetID != "" && action.TargetPosition != nil {
			bot.LastActionResult = &ActionResult{
				Action:  "attack",
				Success: false,
				Message: "attack requires exactly one target",
			}
			continue
		}
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
			if !ActiveModeRules.CanDamage(bot, target) {
				bot.LastActionResult = &ActionResult{
					Action:  "attack",
					Success: false,
					Target:  targetID,
					Message: "friendly fire is disabled",
				}
				continue
			}
		}

		switch wc.Special {
		case "projectile":
			processProjectileAttack(bot, target, action, &wc, obstacles, projectiles, arenaEvents, tickCount)

		case "area":
			processStaffAttack(bot, target, action, &wc, obstacles, staffImpacts, tickCount)

		case "grapple":
			processGrappleAttack(bot, target, &wc, obstacles, arenaEvents, grid, tickCount)

		default:
			processMeleeAttack(bot, target, &wc, bots, obstacles, arenaEvents, grid, tickCount)
		}
	}
}

func setFacingToward(bot *BotState, target Vec2) {
	if bot == nil {
		return
	}
	dir := target.Sub(bot.Position).Normalized()
	if dir.Length() > 0 {
		bot.Facing = dir
	}
}

func markDisrupted(bot *BotState, ticks int) {
	if bot == nil || bot.InvulnTicks > 0 {
		return
	}
	if ticks <= 0 {
		ticks = config.C.ShieldDisruptWindowTicks
	}
	if ticks > bot.RecentlyDisruptedTicks {
		bot.RecentlyDisruptedTicks = ticks
	}
}

func isBackstab(attacker *BotState, target *BotState) bool {
	if attacker == nil || target == nil {
		return false
	}
	targetFacing := target.Facing.Normalized()
	if targetFacing.Length() <= 0 {
		return false
	}
	fromTarget := attacker.Position.Sub(target.Position).Normalized()
	if fromTarget.Length() <= 0 {
		return false
	}
	return targetFacing.X()*fromTarget.X()+targetFacing.Y()*fromTarget.Y() <= config.C.DaggerBackstabDotThreshold
}

func isBraceReady(bot *BotState) bool {
	if bot == nil {
		return false
	}
	minStill := config.C.SpearBraceStillTicks
	if minStill <= 0 {
		minStill = 1
	}
	return bot.StillTicks >= minStill
}

func isNearImpactSurface(pos Vec2, obstacles []Obstacle) bool {
	if pos.X() <= config.C.BotRadius*2 || pos.X() >= config.C.ArenaWidth-config.C.BotRadius*2 {
		return true
	}
	if pos.Y() <= config.C.BotRadius*2 || pos.Y() >= config.C.ArenaHeight-config.C.BotRadius*2 {
		return true
	}
	if ActiveTerrain != nil {
		cell := ActiveTerrain.WorldToGrid(pos)
		for _, d := range directions {
			nx, ny := cell[0]+d.dx, cell[1]+d.dy
			if ActiveTerrain.IsBlocked(nx, ny) {
				return true
			}
		}
	}
	for _, ob := range obstacles {
		padded := config.C.PathfindingCellSize * 0.75
		if pos.X() >= ob.X-padded && pos.X() <= ob.X+ob.Width+padded &&
			pos.Y() >= ob.Y-padded && pos.Y() <= ob.Y+ob.Height+padded {
			return true
		}
	}
	return false
}

func bowChargeTicks(bot *BotState) int {
	if bot == nil {
		return 0
	}
	maxTicks := config.C.BowChargeMaxTicks
	if maxTicks <= 0 {
		maxTicks = 6
	}
	if bot.BowChargeTicks < 0 {
		return 0
	}
	if bot.BowChargeTicks > maxTicks {
		return maxTicks
	}
	return bot.BowChargeTicks
}

func effectiveAttackMultiplier(bot *BotState) float64 {
	if bot == nil {
		return 1
	}
	return bot.AttackMultiplier * effectDamageMultiplier(bot)
}

// processProjectileAttack handles bow attacks by spawning a projectile.
func processProjectileAttack(bot, target *BotState, action *Action, wc *WeaponConfig, obstacles []Obstacle, projectiles *[]Projectile, arenaEvents *[]ArenaEvent, tickCount int) {
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
	chargeTicks := 0
	chargeIntensity := 1.0
	damageBase := weaponDamage(wc)
	cooldown := wc.Cooldown
	if wc.Name == "bow" {
		chargeTicks = bowChargeTicks(bot)
		if action != nil && action.Charged && chargeTicks > 0 {
			damageBase *= 1 + float64(chargeTicks)*config.C.BowChargeDamagePerTick
			speed *= 1 + float64(chargeTicks)*config.C.BowChargeSpeedPerTick
			cooldown *= 1 + float64(chargeTicks)*config.C.BowChargeCooldownPerTick
			chargeIntensity = 1 + math.Min(0.9, 0.15*float64(chargeTicks))
		}
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
		Color:     bot.AvatarColor,
		Position:  bot.Position,
		Direction: dir,
		Speed:     speed,
		HitRadius: hitRadius,
		Damage:    CalculateDamage(damageBase, effectiveAttackMultiplier(bot), 0),
		Weapon:    wc.Name,
		Intensity: chargeIntensity,
		AgeTicks:  0,
		MaxAge:    maxAge,
	}
	*projectiles = append(*projectiles, proj)
	if wc.Name == "bow" && arenaEvents != nil {
		*arenaEvents = append(*arenaEvents, buildBowShotEvent(bot.BotID, bot.AvatarColor, bot.Position, aimPos, tickCount, chargeIntensity))
	}

	bot.CooldownRemaining = cooldown * effectCooldownMultiplier(bot)
	bot.BowChargeTicks = 0
	bot.RoundShotsFired++
	setFacingToward(bot, aimPos)

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

	moveUnitsPerSecond := effectiveMoveSpeed(bot) * float64(config.C.TickRate) / 2.0
	if ActiveTerrain != nil {
		moveUnitsPerSecond = config.C.PathfindingCellSize * terrainMoveCellsPerTick(bot) * float64(config.C.TickRate)
	}

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
	targetRef := action.TargetID
	if targetRef == "" && target != nil {
		targetRef = target.BotID
	}
	if !IsInRange(bot.Position, targetPos, wc.GridRange) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  targetRef,
			Message: "out of range",
		}
		return
	}

	// Check line of sight against actual obstacle geometry.
	if CombatLineBlocked(bot.Position, targetPos, obstacles) {
		bot.LastActionResult = &ActionResult{
			Action:  "attack",
			Success: false,
			Target:  targetRef,
			Message: "no line of sight",
		}
		return
	}

	impact := StaffImpact{
		OwnerID:     bot.BotID,
		Position:    targetPos,
		Radius:      float64(wc.GridParam), // grid tiles for detonation radius
		Damage:      weaponDamage(wc),
		DamageScale: weaponDamageScale(wc),
		TicksLeft:   config.C.StaffDelayTicks,
		AttackMult:  effectiveAttackMultiplier(bot),
	}
	*staffImpacts = append(*staffImpacts, impact)

	bot.CooldownRemaining = wc.Cooldown * effectCooldownMultiplier(bot)
	bot.RoundShotsFired++
	setFacingToward(bot, targetPos)

	bot.LastActionResult = &ActionResult{
		Action:  "attack",
		Success: true,
		Target:  action.TargetID,
		Message: "staff impact placed",
	}
}

// processMeleeAttack handles sword, daggers, shield, and spear attacks.
func processMeleeAttack(bot, target *BotState, wc *WeaponConfig, bots map[string]*BotState, obstacles []Obstacle, arenaEvents *[]ArenaEvent, grid *SpatialGrid, tickCount int) {
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
	rawBaseDamage := weaponDamage(wc)
	backstabbed := false
	bashed := false
	braced := false
	if wc.Special == "backstab" && isBackstab(bot, target) {
		rawBaseDamage *= config.C.DaggerBackstabBonusMultiplier
		backstabbed = true
	}
	if wc.Special == "bash" && target.RecentlyDisruptedTicks > 0 {
		rawBaseDamage *= config.C.ShieldBashBonusMultiplier
		bashed = true
	}
	if wc.Special == "knockback" && isBraceReady(bot) {
		rawBaseDamage *= config.C.SpearBraceBonusMultiplier
		braced = true
	}
	rawDmg := CalculateDamage(rawBaseDamage, effectiveAttackMultiplier(bot), target.DefenseReduction)
	dealt := ApplyDamage(target, bot, rawDmg, wc.Name, tickCount)

	// Apply standard knockback (1 grid tile).
	ApplyAttributedGridKnockback(target, bot, bot.Position, 1, obstacles, wc.Name, tickCount)

	bot.CooldownRemaining = wc.Cooldown * effectCooldownMultiplier(bot)
	bot.RoundShotsFired++
	if dealt > 0 {
		bot.RoundShotsHit++
	}
	setFacingToward(bot, target.Position)

	bot.LastActionResult = &ActionResult{
		Action:  "attack",
		Success: true,
		Target:  target.BotID,
		Damage:  dealt,
		Message: fmt.Sprintf("hit with %s", wc.Name),
	}

	if backstabbed && arenaEvents != nil {
		*arenaEvents = append(*arenaEvents, buildBackstabEvent(bot, target, tickCount))
	}
	if bashed && arenaEvents != nil {
		*arenaEvents = append(*arenaEvents, buildShieldBashEvent(bot, target, tickCount))
	}
	if braced && arenaEvents != nil {
		*arenaEvents = append(*arenaEvents, buildSpearBraceEvent(bot, target, tickCount))
	}

	// Handle weapon specials.
	switch wc.Special {
	case "cleave":
		processCleave(bot, target, wc, bots, obstacles, grid, tickCount)

	case "bash":
		processShieldBash(target)

	case "knockback":
		processExtraKnockback(target, bot, wc, obstacles, braced, tickCount)
	}
	if braced {
		markDisrupted(target, config.C.ShieldDisruptWindowTicks)
	}
	if braced {
		bot.StillTicks = 0
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
		if !ActiveModeRules.CanDamage(bot, other) {
			continue
		}
		if !IsInRange(bot.Position, other.Position, cleaveGridRange) {
			continue
		}
		if CombatLineBlocked(bot.Position, other.Position, obstacles) {
			continue
		}

		cleaveDmg := CalculateDamage(weaponDamage(wc), effectiveAttackMultiplier(bot), other.DefenseReduction) * 0.5
		ApplyDamage(other, bot, cleaveDmg, wc.Name, tickCount)
		ApplyAttributedGridKnockback(other, bot, bot.Position, 1, obstacles, wc.Name, tickCount)
		cleaveCount++
	}
}

// processShieldBash applies a short stun and refreshes the disrupt window.
func processShieldBash(target *BotState) {
	if target == nil || target.InvulnTicks > 0 {
		return
	}
	target.StunTicks = config.C.StunDurationTicks
	markDisrupted(target, config.C.ShieldDisruptWindowTicks)
}

// processExtraKnockback applies additional knockback from spear hits.
func processExtraKnockback(target, attacker *BotState, wc *WeaponConfig, obstacles []Obstacle, braced bool, tickCount int) {
	// Spear knockback: push 1 additional tile.
	ApplyAttributedGridKnockback(target, attacker, attacker.Position, 1, obstacles, wc.Name, tickCount)
	if braced && config.C.SpearBraceBonusKnockback > 0 {
		ApplyAttributedGridKnockback(target, attacker, attacker.Position, config.C.SpearBraceBonusKnockback, obstacles, wc.Name, tickCount)
	}
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

	initialDist := GridDistance(posToGrid(bot.Position), posToGrid(target.Position))

	// Deal damage
	rawDmg := CalculateDamage(weaponDamage(wc), effectiveAttackMultiplier(bot), target.DefenseReduction)
	dealt := ApplyDamage(target, bot, rawDmg, wc.Name, tickCount)
	totalDealt := dealt

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

	bot.CooldownRemaining = wc.Cooldown * effectCooldownMultiplier(bot)
	bot.RoundShotsFired++
	if dealt > 0 {
		bot.RoundShotsHit++
	}
	setFacingToward(bot, target.Position)

	slammed := target.InvulnTicks <= 0 &&
		initialDist >= maxInt(1, config.C.GrappleSlamMinRange) &&
		isNearImpactSurface(target.Position, obstacles)
	if slammed {
		bonusBase := weaponDamage(wc) * math.Max(0, config.C.GrappleSlamBonusMultiplier-1)
		if bonusBase > 0 {
			bonusDmg := CalculateDamage(bonusBase, effectiveAttackMultiplier(bot), target.DefenseReduction)
			totalDealt += ApplyDamage(target, bot, bonusDmg, "grapple_slam", tickCount)
		}
		if config.C.GrappleSlamStunTicks > target.StunTicks {
			target.StunTicks = config.C.GrappleSlamStunTicks
		}
		ApplyAttributedGridKnockback(target, bot, bot.Position, 1, obstacles, "grapple_slam", tickCount)
		markDisrupted(target, config.C.ShieldDisruptWindowTicks)
		if arenaEvents != nil {
			*arenaEvents = append(*arenaEvents, buildGrappleSlamEvent(bot, target, tickCount))
		}
	}

	bot.LastActionResult = &ActionResult{
		Action:  "attack",
		Success: true,
		Target:  target.BotID,
		Damage:  totalDealt,
		Message: func() string {
			if slammed {
				return "grapple slam"
			}
			return "grappled target"
		}(),
	}
}

// ProcessStaffImpacts ticks down staff impacts and applies damage when they detonate.
func ProcessStaffImpacts(staffImpacts *[]StaffImpact, burnFields *[]BurnField, bots map[string]*BotState, antiTeam *AntiTeamTracker, tickCount int) []ArenaEvent {
	active := (*staffImpacts)[:0]
	var events []ArenaEvent

	for i := range *staffImpacts {
		impact := &(*staffImpacts)[i]
		impact.TicksLeft--

		if impact.TicksLeft <= 0 {
			events = append(events, buildStaffDetonationEvent(*impact, tickCount))
			attacker := bots[impact.OwnerID]
			castHit := false
			// Detonate: damage all bots within grid radius except the owner.
			impactGridRadius := int(impact.Radius) // Radius stores grid tiles
			for _, bot := range bots {
				if !bot.IsAlive || bot.BotID == impact.OwnerID {
					continue
				}

				if !IsInRange(bot.Position, impact.Position, impactGridRadius) {
					continue
				}

				if attacker == nil {
					continue
				}

				dmg := CalculateDamage(impact.Damage, impact.AttackMult, bot.DefenseReduction)
				if dealt := ApplyDamage(bot, attacker, dmg, "staff", tickCount); dealt > 0 {
					castHit = true
					if antiTeam != nil {
						antiTeam.RecordAttack(attacker.BotID, bot.BotID)
					}
				}
			}
			if castHit && attacker != nil {
				// Accuracy is cast-based, like cleave and grapple: one successful
				// cast is one hit regardless of how many targets the AOE affected.
				attacker.RoundShotsHit++
			}
			burnTicks := config.C.StaffBurnFieldTicks
			if burnTicks > 0 && burnFields != nil {
				radius := float64(config.C.StaffBurnFieldRadius)
				if radius <= 0 {
					radius = 1
				}
				damageScale := impact.DamageScale
				if !finitePositive(damageScale) {
					damageScale = 1
				}
				field := BurnField{
					ID:           fmt.Sprintf("burn:%s:%d:%0.0f:%0.0f", impact.OwnerID, tickCount, impact.Position.X(), impact.Position.Y()),
					OwnerID:      impact.OwnerID,
					Position:     impact.Position,
					Radius:       radius,
					Damage:       config.C.StaffBurnFieldDamage * damageScale,
					AttackMult:   impact.AttackMult,
					TicksLeft:    burnTicks,
					TickInterval: maxInt(1, config.C.StaffBurnFieldTickInterval),
					HitRecorded:  castHit,
				}
				*burnFields = append(*burnFields, field)
				events = append(events, buildBurnFieldSpawnEvent(field, tickCount))
			}
			// Impact is consumed; do not keep it.
		} else {
			active = append(active, *impact)
		}
	}

	*staffImpacts = active
	return events
}

func ProcessBurnFields(burnFields *[]BurnField, bots map[string]*BotState, antiTeam *AntiTeamTracker, tickCount int) {
	if burnFields == nil {
		return
	}
	active := (*burnFields)[:0]
	for i := range *burnFields {
		field := &(*burnFields)[i]
		field.TicksLeft--
		field.PulseTick++
		if field.TicksLeft <= 0 {
			continue
		}

		if field.Damage > 0 && field.TickInterval > 0 && field.PulseTick%field.TickInterval == 0 {
			attacker := bots[field.OwnerID]
			if attacker != nil {
				attackMult := field.AttackMult
				if !finitePositive(attackMult) {
					attackMult = 1
				}
				for _, bot := range bots {
					if !bot.IsAlive || bot.BotID == field.OwnerID {
						continue
					}
					if hasEffectByName(bot.ActiveEffects, "hazard_key") {
						continue
					}
					if _, enteredRange := firstMovementPositionInRange(bot, field.Position, int(field.Radius)); !enteredRange {
						continue
					}
					dmg := CalculateDamage(field.Damage, attackMult, bot.DefenseReduction)
					if dealt := ApplyDamage(bot, attacker, dmg, "staff_burn", tickCount); dealt > 0 {
						if antiTeam != nil {
							antiTeam.RecordAttack(attacker.BotID, bot.BotID)
						}
						if !field.HitRecorded {
							// A lingering field can turn a missed detonation into one
							// successful cast, but subsequent targets/ticks are effects,
							// not independent accuracy actions.
							attacker.RoundShotsHit++
							field.HitRecorded = true
						}
					}
				}
			}
		}

		active = append(active, *field)
	}
	*burnFields = active
}

// StaffImpactView is the typed protocol view of a pending staff blast.
// Position is grid coordinates ([2]int) for bots and world coordinates (Vec2)
// for spectators, matching the useGridPos flag of BuildStaffImpactView.
type StaffImpactView struct {
	Type      string  `json:"type"`
	OwnerID   string  `json:"owner_id"`
	Radius    float64 `json:"radius"`
	TicksLeft int     `json:"ticks_left"`
	Position  any     `json:"position"`
}

// BuildStaffImpactView creates a protocol-compatible view of a pending staff blast.
func BuildStaffImpactView(impact StaffImpact, useGridPos bool) StaffImpactView {
	view := StaffImpactView{
		Type:      "staff_impact",
		OwnerID:   impact.OwnerID,
		Radius:    impact.Radius,
		TicksLeft: impact.TicksLeft,
	}
	if useGridPos {
		view.Position = posToGrid(impact.Position)
	} else {
		view.Position = impact.Position
	}
	return view
}

// BurnFieldView is the typed protocol view of a lingering staff burn field.
type BurnFieldView struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	OwnerID   string  `json:"owner_id"`
	Radius    float64 `json:"radius"`
	TicksLeft int     `json:"ticks_left"`
	Active    bool    `json:"active"`
	Position  any     `json:"position"`
}

func BuildBurnFieldView(field BurnField, useGridPos bool) BurnFieldView {
	view := BurnFieldView{
		Type:      "burn_field",
		ID:        field.ID,
		OwnerID:   field.OwnerID,
		Radius:    field.Radius,
		TicksLeft: field.TicksLeft,
		Active:    true,
	}
	if useGridPos {
		view.Position = posToGrid(field.Position)
	} else {
		view.Position = field.Position
	}
	return view
}

// ProcessShoves handles all SHOVE actions for the current tick.
// Shoves deal no damage but knock the target back far and apply a short stun.
func ProcessShoves(bots map[string]*BotState, obstacles []Obstacle, tickCount int) {

	for _, bot := range bots {
		if !bot.IsAlive || bot.PendingAction == nil {
			continue
		}
		if bot.PendingAction.Type != ActionShove {
			continue
		}
		if rejectControlledAction(bot, "shove", true) {
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
		if target == bot {
			bot.LastActionResult = &ActionResult{
				Action:  "shove",
				Success: false,
				Target:  targetID,
				Message: "cannot shove self",
			}
			continue
		}
		if !ActiveModeRules.CanDamage(bot, target) {
			bot.LastActionResult = &ActionResult{
				Action:  "shove",
				Success: false,
				Target:  targetID,
				Message: "friendly fire is disabled",
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

		// Range and knockback are discrete grid-tile settings. Round custom
		// fractional values to the nearest tile and keep both effects usable.
		shoveRangeTiles := max(1, int(math.Round(config.C.ShoveRange)))
		if !IsInRange(bot.Position, target.Position, shoveRangeTiles) {
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

		shoveKnockbackTiles := max(1, int(math.Round(config.C.ShoveKnockback)))
		ApplyAttributedGridKnockback(target, bot, bot.Position, shoveKnockbackTiles, obstacles, "shove_wall_slam", tickCount)

		// Apply stun.
		if config.C.ShoveStunTicks > target.StunTicks {
			target.StunTicks = config.C.ShoveStunTicks
		}
		markDisrupted(target, config.C.ShieldDisruptWindowTicks)

		bot.ShoveCooldown = config.C.ShoveCooldown * effectCooldownMultiplier(bot)
		setFacingToward(bot, target.Position)

		bot.LastActionResult = &ActionResult{
			Action:  "shove",
			Success: true,
			Target:  targetID,
			Message: "shoved",
		}
	}
}
