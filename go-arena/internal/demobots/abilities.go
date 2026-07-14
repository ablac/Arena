// Ability and pickup prioritization: smart pickup scoring, mine
// placement, and gravity well usage.
package demobots

import (
	"math"
	"math/rand"

	"arena-server/internal/config"
)

// === Smart Pickup Prioritization ===

// trySmartPickup checks for high-value pickups and grabs them if worthwhile.
// Returns an action if a pickup should be grabbed, nil otherwise.
func trySmartPickup(ts tickState, strategy string, weapon string) *actionResult {
	if len(ts.Pickups) == 0 {
		return nil
	}
	pos := ts.Position
	routes := newSmartPickupRoutes(pos, ts.Danger)
	defer routes.release()
	hpRatio := ts.HP / ts.MaxHP
	visibleEnemies := countVisibleEnemies(ts.Enemies)
	rangedThreat := hasVisibleRangedThreat(ts.Enemies)
	pickupReachBonus := 0.0
	if ts.PickupSurge {
		pickupReachBonus = 2
	}

	// Critical health is the only pickup that outranks nearby utility. Without
	// this early branch, wounded bots detoured for gravity wells and cooldown
	// shards while a reachable heal was only a few cells away.
	health, healthD := nearestHealthPickupWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, &routes)
	if health != nil && hpRatio < 0.45 && healthD <= 8+pickupReachBonus {
		if healthD <= 1 && health.ID != "" {
			a := useItem(health.ID)
			return &a
		}
		a := moveTo(pos, health.Position, ts.Danger)
		return &a
	}

	if any, anyD := nearestPickupWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, &routes); any != nil && anyD <= 1 && any.ID != "" {
		a := useItem(any.ID)
		return &a
	}

	// Gravity well: grab only if we do not already have a charge.
	gw, gwD := nearestPickupOfTypeWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, "gravity_well", &routes)
	if gw != nil && gwD <= 8+pickupReachBonus && ts.GravityWellCharge <= 0 {
		a := moveTo(pos, gw.Position, ts.Danger)
		return &a
	}

	// Cooldown shard: prioritize when a major combat tool is currently unavailable.
	cd, cdD := nearestPickupOfTypeWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, "cooldown_shard", &routes)
	if cd != nil && cdD <= 7+pickupReachBonus {
		if ts.Cooldown > 0 || ts.DodgeCool > 0 || ts.GrappleCooldown > 0 || ts.StunTicks > 0 || ts.FastZone || ts.DoubleBounty || ts.TeleportSurge {
			a := moveTo(pos, cd.Position, ts.Danger)
			return &a
		}
	}

	// Hazard key: strongest when hazards are relevant to the current route or objective.
	hk, hkD := nearestPickupOfTypeWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, "hazard_key", &routes)
	if hk != nil && hkD <= 8+pickupReachBonus && !ts.HasHazardKey {
		if pad, padD := nearestCapturePad(pos, ts.CapturePads); pad != nil && padD <= 9 && (pad.Contested || pad.ContenderCount > 0 || !pad.Ready || ts.HazardStorm) {
			a := moveTo(pos, hk.Position, ts.Danger)
			return &a
		}
		if visibleEnemies > 0 && (rangedThreat || ts.IsBountyTarget || hpRatio < 0.7 || ts.HazardStorm) {
			a := moveTo(pos, hk.Position, ts.Danger)
			return &a
		}
		if !ts.InZone || inHazardZone(ts.ZoneTargetCenter, ts.HazardZones) {
			a := moveTo(pos, hk.Position, ts.Danger)
			return &a
		}
	}

	// Relay battery: strongest when a capture pad is nearby, contested, or enemy-owned.
	rb, rbD := nearestPickupOfTypeWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, "relay_battery", &routes)
	if rb != nil && rbD <= 8+pickupReachBonus && !ts.HasRelayBattery {
		if pad, padD := nearestCapturePad(pos, ts.CapturePads); pad != nil && padD <= 10 &&
			(pad.Contested || pad.CapturingBotID != "" || pad.OwnerID != "" || !pad.Ready) {
			a := moveTo(pos, rb.Position, ts.Danger)
			return &a
		}
	}

	// Overdrive core: strongest swing pickup when a fight is imminent.
	od, odD := nearestPickupOfTypeWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, "overdrive_core", &routes)
	if od != nil && odD <= 8+pickupReachBonus && (visibleEnemies > 0 || ts.IsBountyTarget || ts.DoubleBounty || strategy == "aggressive" || strategy == "berserker" || strategy == "assassin") {
		a := moveTo(pos, od.Position, ts.Danger)
		return &a
	}

	// Grapple charge: lightweight utility, especially valuable to grapple users and ranged kiting bots.
	gc, gcD := nearestPickupOfTypeWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, "grapple_charge", &routes)
	if gc != nil && gcD <= 7+pickupReachBonus {
		if ts.GrappleCharges <= 0 || ts.GrappleCooldown > 0 || weapon == "grapple" || strategy == "kite" || strategy == "assassin" {
			a := moveTo(pos, gc.Position, ts.Danger)
			return &a
		}
	}

	// Damage boost: grab if there is a realistic fight to use it in.
	dmg, dmgD := nearestPickupOfTypeWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, "damage_boost", &routes)
	if dmg != nil && dmgD <= 6+pickupReachBonus && (visibleEnemies > 0 || strategy == "aggressive" || strategy == "assassin" || ts.DoubleBounty) {
		a := moveTo(pos, dmg.Position, ts.Danger)
		return &a
	}

	// Bounty token: worth contesting when we can realistically convert a fight soon.
	bt, btD := nearestPickupOfTypeWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, "bounty_token", &routes)
	if bt != nil && btD <= 7+pickupReachBonus && (visibleEnemies > 0 || ts.IsBountyTarget || strategy == "aggressive" || strategy == "assassin" || ts.DoubleBounty) {
		a := moveTo(pos, bt.Position, ts.Danger)
		return &a
	}

	// Speed boost: useful for mobility styles and zone recovery.
	spd, spdD := nearestPickupOfTypeWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, "speed_boost", &routes)
	if spd != nil && spdD <= 6+pickupReachBonus && (strategy == "assassin" || strategy == "kite" || !ts.InZone || ts.FastZone) {
		a := moveTo(pos, spd.Position, ts.Danger)
		return &a
	}

	// Shield bubble: grab aggressively when ranged LOS is on us.
	sb, sbD := nearestPickupOfTypeWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, "shield_bubble", &routes)
	if sb != nil && sbD <= 5+pickupReachBonus && (hpRatio < 0.9 || rangedThreat || ts.IsBountyTarget) {
		a := moveTo(pos, sb.Position, ts.Danger)
		return &a
	}

	// Health pack: be more willing to stabilize before losing initiative.
	hp, hpD := nearestHealthPickupWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, &routes)
	if hp != nil && hpD <= 6+pickupReachBonus && hpRatio < 0.8 {
		a := moveTo(pos, hp.Position, ts.Danger)
		return &a
	}

	if any, anyD := nearestPickupWithRoutes(ts.Pickups, ts.HazardZones, ts.HasHazardKey, &routes); any != nil && visibleEnemies == 0 && anyD <= 6+pickupReachBonus {
		a := moveTo(pos, any.Position, ts.Danger)
		return &a
	}

	return nil
}

// === Gravity Well Logic ===

// tryGravityWell checks if the bot should deploy a gravity well.
func tryGravityWell(ts tickState, botID string) *actionResult {
	if ts.GravityWellCharge <= 0 && !getHasGravWell(botID) {
		return nil
	}

	// Deploy if 3+ enemies within 6 tiles
	nearby := enemiesWithinRange(ts.Position, ts.Enemies, 6)
	if nearby >= 3 {
		center, _ := enemyClusterCenter(ts.Enemies, 6)
		a := useGravityWell(center)
		return &a
	}
	return nil
}

// tryUniversalGrapple uses the global grapple ability to finish weak targets,
// disrupt ranged threats, or force fights when carrying a bounty.
func tryUniversalGrapple(ts tickState, weapon string, wrange float64) *actionResult {
	if ts.GrappleCharges <= 0 || ts.GrappleCooldown > 0 || len(ts.Enemies) == 0 {
		return nil
	}
	const grappleRange = 12.0 // matches config GrappleAbilityRangeTiles (ARENA_GRAPPLE_RANGE_TILES, default 12)

	var best *entity
	bestScore := -math.Inf(1)

	for i := range ts.Enemies {
		e := &ts.Enemies[i]
		if e.ID == "" || !e.IsAlive || !e.HasLOS {
			continue
		}
		d := chebyshev(ts.Position, e.Position)
		if d <= 1 || d > grappleRange {
			continue
		}

		enemyHPRatio := 1.0
		if e.MaxHP > 0 {
			enemyHPRatio = e.HP / math.Max(e.MaxHP, 1)
		}
		finisher := enemyHPRatio <= 0.2
		objectiveTarget := e.Type == "bounty_target" || ts.isFlagCarrier(e.ID)
		if weapon == "bow" || weapon == "staff" {
			// Pulling a healthy melee enemy onto a ranged bot destroys the
			// spacing advantage. Ranged bots reserve target grapples for a
			// finisher/objective or an out-of-range ranged duel.
			if d <= wrange && !finisher {
				continue
			}
			if isMelee(e.Weapon) && !finisher && !objectiveTarget {
				continue
			}
		}

		score := d
		if d > wrange {
			score += 25
		}
		if e.Type == "bounty_target" {
			score += 50
		}
		if e.Weapon == "bow" || e.Weapon == "staff" {
			score += 20
		}
		if e.Stunned {
			score -= 15
		}
		if e.MaxHP > 0 {
			score += (1 - enemyHPRatio) * 35
			if enemyHPRatio <= 0.40 {
				score += 25
			}
		}
		if ts.IsBountyTarget {
			score += 15
		}
		if isMelee(weapon) {
			score += d * 1.5
		}
		if d > 12 && e.Type != "bounty_target" {
			score -= 30
		}
		if weapon == "grapple" && d >= 2 && d <= 8 {
			score += 35
		}
		if isMelee(weapon) && d > wrange && d <= wrange+6 {
			score += 18
		}

		if score > bestScore {
			bestScore = score
			best = e
		}
	}

	if best == nil {
		return nil
	}

	dist := chebyshev(ts.Position, best.Position)
	bestHPRatio := 1.0
	if best.MaxHP > 0 {
		bestHPRatio = best.HP / math.Max(best.MaxHP, 1)
	}
	shouldGrapple := best.Type == "bounty_target" ||
		ts.isFlagCarrier(best.ID) ||
		ts.IsBountyTarget ||
		best.Weapon == "bow" ||
		best.Weapon == "staff" ||
		weapon == "grapple" ||
		bestHPRatio <= 0.45 ||
		(dist > wrange && dist <= wrange+4)
	if weapon == "bow" || weapon == "staff" {
		shouldGrapple = best.Type == "bounty_target" || ts.isFlagCarrier(best.ID) ||
			bestHPRatio <= 0.2 || ((best.Weapon == "bow" || best.Weapon == "staff") && dist > wrange)
	}
	if !shouldGrapple {
		return nil
	}

	a := grapple(best.ID)
	return &a
}

func anchorGrappleDestinationSafe(from, target [2]float64, pads map[string]entity, danger *dangerSet) bool {
	start := [2]int{int(math.Round(from[0])), int(math.Round(from[1]))}
	goal := [2]int{int(math.Round(target[0])), int(math.Round(target[1]))}
	if start == goal || danger.has(goal[0], goal[1]) {
		return false
	}
	if terrain := getTerrain(); terrain != nil {
		// The server rejects an anchor before consuming its charge when the
		// endpoint or combat ray crosses terrain. Mirror that validation here so
		// a rejected anchor cannot be selected again on every bot tick.
		if terrain.isBlocked(goal[0], goal[1]) || terrain.gridLineBlocked(start, goal) {
			return false
		}
	}
	collectRadius := config.C.TeleportCollectRadius
	if collectRadius < 0 {
		collectRadius = 0
	}
	for _, pad := range pads {
		if isReadyTeleporter(pad) && chebyshev(target, pad.Position) <= float64(collectRadius) {
			return false
		}
	}
	return true
}

func clampAnchorToTerrain(target [2]float64) [2]float64 {
	// Anchor grapples use grid coordinates. Emergency escape vectors can point
	// beyond an edge, but the public action protocol intentionally rejects
	// negative coordinates. Clamp to the known map so the escape stays valid
	// and still moves as far away from the threat as the arena permits.
	target[0] = math.Max(0, target[0])
	target[1] = math.Max(0, target[1])
	if terrain := getTerrain(); terrain != nil {
		target[0] = math.Min(target[0], float64(max(0, terrain.Width-1)))
		target[1] = math.Min(target[1], float64(max(0, terrain.Height-1)))
	}
	return target
}

func tryAnchorGrapple(ts tickState, strategy string, near *entity, nearD, wrange float64) *actionResult {
	if ts.GrappleCharges <= 0 || ts.GrappleCooldown > 0 {
		return nil
	}
	const grappleRange = 12.0 // matches config GrappleAbilityRangeTiles (ARENA_GRAPPLE_RANGE_TILES, default 12)
	pads := teleporterByID(ts.Teleporters)

	hpRatio := ts.HP / math.Max(ts.MaxHP, 1)
	if !ts.InZone {
		dist := chebyshev(ts.Position, ts.ZoneTargetCenter)
		if dist >= 5 && dist <= grappleRange && (near == nil || nearD > 3) &&
			anchorGrappleDestinationSafe(ts.Position, ts.ZoneTargetCenter, pads, ts.Danger) {
			a := grapplePos(ts.ZoneTargetCenter)
			return &a
		}
	}

	if near != nil && hpRatio < 0.45 {
		away := clampAnchorToTerrain([2]float64{
			ts.Position[0] + (ts.Position[0]-near.Position[0])*4,
			ts.Position[1] + (ts.Position[1]-near.Position[1])*4,
		})
		if chebyshev(ts.Position, away) <= grappleRange && anchorGrappleDestinationSafe(ts.Position, away, pads, ts.Danger) {
			a := grapplePos(away)
			return &a
		}
	}

	if strategy == "kite" && near != nil && nearD > wrange+2 && nearD <= grappleRange {
		offset := gridDirAway(near.Position, ts.Position)
		anchor := clampAnchorToTerrain([2]float64{
			near.Position[0] + offset[0]*2,
			near.Position[1] + offset[1]*2,
		})
		if chebyshev(ts.Position, anchor) <= grappleRange && anchorGrappleDestinationSafe(ts.Position, anchor, pads, ts.Danger) {
			a := grapplePos(anchor)
			return &a
		}
	}
	return nil
}

func teleporterByID(teleporters []entity) map[string]entity {
	pads := make(map[string]entity, len(teleporters)+6)
	if terrain := getTerrain(); terrain != nil {
		for id, tp := range terrain.Teleporters {
			pads[id] = tp
		}
	}
	// Live nearby entities override the static map snapshot, especially for
	// readiness. The linked exit may remain outside fog and come only from the
	// map cache.
	for _, tp := range teleporters {
		if tp.ID != "" {
			pads[tp.ID] = tp
		}
	}
	return pads
}

func isReadyTeleporter(tp entity) bool {
	return tp.Ready
}

func preferredGridDirections(from, target [2]int) [8][2]int {
	all := [8][2]int{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
	var ordered [8][2]int
	count := 0
	add := func(dir [2]int) {
		if dir == [2]int{} {
			return
		}
		for i := 0; i < count; i++ {
			if ordered[i] == dir {
				return
			}
		}
		ordered[count] = dir
		count++
	}

	dx, dy := intSign(target[0]-from[0]), intSign(target[1]-from[1])
	add([2]int{dx, dy})
	add([2]int{dx, 0})
	add([2]int{0, dy})
	for _, dir := range all {
		add(dir)
	}
	return ordered
}

func teleporterFirstMoveIsSafe(ts tickState, padCell [2]int, allowRadius int, dir [2]int) bool {
	terrain := getTerrain()
	if terrain == nil {
		return false
	}
	col, row := int(math.Round(ts.Position[0])), int(math.Round(ts.Position[1]))
	moved := false
	for step := 0; step < maxMoveCellsPerTick(ts); step++ {
		if terrain.isMoveBlocked(col, row, dir[0], dir[1]) {
			break
		}
		col += dir[0]
		row += dir[1]
		moved = true
		if ts.Danger.blocksExceptPad(col, row, padCell, allowRadius) {
			return false
		}
	}
	return moved
}

// teleporterSourceApproach finds a short, genuinely traversable route into a
// source pad's trigger footprint. Unlike tactical target ranking, this search
// respects live lethal danger. It opens only the selected pad's soft avoidance
// cells, never lethal cells, and validates the full first boosted move.
func teleporterSourceApproach(ts tickState, pad entity) (actionResult, float64, bool) {
	terrain := getTerrain()
	if terrain == nil {
		return actionResult{}, 0, false
	}
	start := [2]int{int(math.Round(ts.Position[0])), int(math.Round(ts.Position[1]))}
	padCell := [2]int{int(math.Round(pad.Position[0])), int(math.Round(pad.Position[1]))}
	if terrain.isBlocked(start[0], start[1]) || terrain.isBlocked(padCell[0], padCell[1]) {
		return actionResult{}, 0, false
	}
	collectRadius := config.C.TeleportCollectRadius
	if collectRadius < 0 {
		collectRadius = 0
	}
	allowRadius := teleporterAvoidanceRadius(ts)
	goal := func(col, row int) bool {
		return intChebyshev([2]int{col, row}, padCell) <= collectRadius &&
			!ts.Danger.blocksExceptPad(col, row, padCell, allowRadius)
	}
	if goal(start[0], start[1]) {
		return idle(), 0, true
	}

	const maxApproachDistance = 3
	scratch := bfsPool.Get().(*bfsScratch)
	defer bfsPool.Put(scratch)
	scratch.reset(terrain.Width, terrain.Height)
	scratch.visit(start[0], start[1])

	for _, dir := range preferredGridDirections(start, padCell) {
		if terrain.isMoveBlocked(start[0], start[1], dir[0], dir[1]) ||
			ts.Danger.blocksExceptPad(start[0]+dir[0], start[1]+dir[1], padCell, allowRadius) ||
			!teleporterFirstMoveIsSafe(ts, padCell, allowRadius, dir) {
			continue
		}
		col, row := start[0]+dir[0], start[1]+dir[1]
		if !scratch.visit(col, row) {
			continue
		}
		node := bfsNode{col: col, row: row, firstDC: dir[0], firstDR: dir[1], distance: 1}
		if goal(col, row) {
			a := moveDir([2]float64{float64(dir[0]), float64(dir[1])})
			return a, 1, true
		}
		scratch.queue = append(scratch.queue, node)
	}

	for head := 0; head < len(scratch.queue); head++ {
		node := scratch.queue[head]
		if node.distance >= maxApproachDistance {
			continue
		}
		for _, dir := range preferredGridDirections([2]int{node.col, node.row}, padCell) {
			if terrain.isMoveBlocked(node.col, node.row, dir[0], dir[1]) {
				continue
			}
			col, row := node.col+dir[0], node.row+dir[1]
			if ts.Danger.blocksExceptPad(col, row, padCell, allowRadius) || !scratch.visit(col, row) {
				continue
			}
			distance := node.distance + 1
			if goal(col, row) {
				a := moveDir([2]float64{float64(node.firstDC), float64(node.firstDR)})
				return a, float64(distance), true
			}
			scratch.queue = append(scratch.queue, bfsNode{
				col: col, row: row, firstDC: node.firstDC, firstDR: node.firstDR, distance: distance,
			})
		}
	}
	return actionResult{}, 0, false
}

func strategicMineTile(pos [2]float64, zoneCenter [2]float64) bool {
	t := getTerrain()
	if t == nil {
		return chebyshev(pos, zoneCenter) <= 4
	}

	cell := [2]int{int(math.Round(pos[0])), int(math.Round(pos[1]))}
	open := 0
	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for _, d := range dirs {
		if !t.isMoveBlocked(cell[0], cell[1], d[0], d[1]) {
			open++
		}
	}
	return open <= 2 || chebyshev(pos, zoneCenter) <= 4
}

func knownStaticHazardAt(pos [2]float64) bool {
	terrain := getTerrain()
	if terrain == nil {
		return false
	}
	for _, h := range terrain.HazardZones {
		if h.Width > 0 || h.Height > 0 {
			halfW, halfH := h.Width/2, h.Height/2
			if math.Abs(pos[0]-h.Position[0]) <= float64(halfW) && math.Abs(pos[1]-h.Position[1]) <= float64(halfH) {
				return true
			}
			continue
		}
		radius := h.Radius
		if radius <= 0 {
			radius = 2
		}
		if chebyshev(pos, h.Position) <= radius {
			return true
		}
	}
	return false
}

func nearestEnemyDistanceFrom(pos [2]float64, enemies []entity) float64 {
	best := math.Inf(1)
	for i := range enemies {
		if !enemies[i].IsAlive {
			continue
		}
		if d := chebyshev(pos, enemies[i].Position); d < best {
			best = d
		}
	}
	return best
}

func teleportExitIsSafe(ts tickState, exit entity, currentEnemyDistance float64) bool {
	terrain := getTerrain()
	if terrain == nil {
		return false
	}
	exitCell := [2]int{int(math.Round(exit.Position[0])), int(math.Round(exit.Position[1]))}
	if terrain.isBlocked(exitCell[0], exitCell[1]) || knownStaticHazardAt(exit.Position) {
		return false
	}
	// Treat all currently-known dynamic danger as unsafe too. Far exits cannot
	// expose every mine/burn field through fog, so the static map check above is
	// intentionally conservative about pulsing hazard zones.
	if inHazardZone(exit.Position, ts.HazardZones) {
		return false
	}
	if ts.Danger != nil && ts.Danger.hasLethal(exitCell[0], exitCell[1]) {
		return false
	}
	if chebyshev(exit.Position, ts.ZoneCenter) > math.Max(0, ts.ZoneRadius-1) {
		return false
	}
	exitEnemyDistance := nearestEnemyDistanceFrom(exit.Position, ts.Enemies)
	if !math.IsInf(currentEnemyDistance, 1) && exitEnemyDistance < currentEnemyDistance-1 {
		return false
	}
	return true
}

// tryTeleporterPressureEscape spends a teleporter charge only for an imminent
// survival problem and only when the linked exit is known and demonstrably
// safer. Normal engagement and convenience shortcuts deliberately avoid pads.
func tryTeleporterPressureEscape(ts tickState, strategy string, near *entity, nearD float64) *actionResult {
	_ = strategy
	if len(ts.Teleporters) == 0 {
		return nil
	}

	hpRatio := ts.HP / math.Max(ts.MaxHP, 1)
	closePressure := enemiesWithinRange(ts.Position, ts.Enemies, 3)
	criticalCombat := near != nil && ((hpRatio <= 0.25 && nearD <= 3) || (hpRatio <= 0.40 && closePressure >= 2))
	criticalZone := !ts.InZone && ts.ZoneDist <= -2
	if !criticalCombat && !criticalZone {
		return nil
	}
	if hp, hpD := nearestHealthPickup(ts.Position, ts.Pickups, ts.HazardZones, ts.HasHazardKey); hp != nil && hpD <= 3 {
		return nil
	}

	pads := teleporterByID(ts.Teleporters)
	currentEnemyDistance := nearestEnemyDistanceFrom(ts.Position, ts.Enemies)
	currentZoneDistance := chebyshev(ts.Position, ts.ZoneTargetCenter)
	bestScore := -math.Inf(1)
	var bestAction actionResult
	found := false

	for i := range ts.Teleporters {
		tp := &ts.Teleporters[i]
		if !isReadyTeleporter(*tp) || tp.LinkedID == "" {
			continue
		}
		approach, dToPad, reachable := teleporterSourceApproach(ts, *tp)
		if !reachable {
			continue
		}
		linked, ok := pads[tp.LinkedID]
		if !ok || !teleportExitIsSafe(ts, linked, currentEnemyDistance) {
			continue
		}

		exitEnemyDistance := nearestEnemyDistanceFrom(linked.Position, ts.Enemies)
		exitZoneDistance := chebyshev(linked.Position, ts.ZoneTargetCenter)
		combatImprovement := 0.0
		if !math.IsInf(currentEnemyDistance, 1) {
			combatImprovement = exitEnemyDistance - currentEnemyDistance
		}
		zoneImprovement := currentZoneDistance - exitZoneDistance
		if criticalCombat && combatImprovement < 2 {
			continue
		}
		if criticalZone && zoneImprovement < 2 {
			continue
		}
		score := combatImprovement*4 + zoneImprovement - dToPad
		if score > bestScore {
			bestScore = score
			bestAction = approach
			found = true
		}
	}
	if !found {
		return nil
	}
	return &bestAction
}

// tryPlaceMineAdvanced places mines more proactively in hot lanes and while
// retreating, using the server-provided mine counts instead of local guesses.
func tryPlaceMineAdvanced(ts tickState, strategy, weapon string, near *entity, nearD float64) *actionResult {
	if ts.MineCount >= 3 || ts.NearbyMines >= 2 {
		return nil
	}

	pos := ts.Position
	hpRatio := ts.HP / math.Max(ts.MaxHP, 1)
	distToCenter := chebyshev(pos, ts.ZoneTargetCenter)
	pressure := enemiesWithinRange(pos, ts.Enemies, 4)
	earlyRound := ts.RoundTick < 35
	onStrategicTile := strategicMineTile(pos, ts.ZoneTargetCenter)

	if near != nil && nearD <= 2 && (strategy == "territorial" || strategy == "kite" || hpRatio < 0.7) {
		a := placeMine()
		return &a
	}

	for _, tp := range ts.Teleporters {
		if !isReadyTeleporter(tp) {
			continue
		}
		if chebyshev(pos, tp.Position) <= 1 && (near != nil && nearD <= 4 || pressure >= 2) {
			a := placeMine()
			return &a
		}
	}

	if earlyRound && pressure == 0 && !onStrategicTile && !ts.TeleportSurge {
		return nil
	}
	if earlyRound && near == nil && distToCenter > 4 {
		return nil
	}

	if onStrategicTile && distToCenter <= 6 && pressure >= 1 && rand.Float64() < 0.65 {
		a := placeMine()
		return &a
	}
	if distToCenter <= 3 && (strategy == "territorial" || weapon == "shield") && (near == nil || nearD > 1) && pressure >= 1 && rand.Float64() < 0.45 {
		a := placeMine()
		return &a
	}
	if distToCenter <= 5 && onStrategicTile && (near == nil || nearD > 2) && pressure >= 1 && rand.Float64() < 0.25 {
		a := placeMine()
		return &a
	}
	if ts.IsBountyTarget && onStrategicTile && pressure >= 2 && rand.Float64() < 0.5 {
		a := placeMine()
		return &a
	}
	return nil
}
