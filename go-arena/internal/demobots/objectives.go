// Mode objectives and area helpers: CTF flag play, hazard zone
// avoidance, and enemy cluster detection for staff AoE.
package demobots

import (
	"math"
)

// === CTF Objective Logic ===

// tryCTFObjective implements capture-the-flag play: carriers run the flag
// home, defenders hunt enemy carriers, dropped flags get returned, and the
// faster personalities go for steals. Returns nil outside CTF rounds or when
// combat should take over.
func tryCTFObjective(ts tickState, strategy string, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) *actionResult {
	if ts.Mode != "ctf" || len(ts.Flags) == 0 || ts.Team == 0 {
		return nil
	}
	pos := ts.Position
	hpRatio := ts.HP / math.Max(ts.MaxHP, 1)

	var myFlag *entity    // my team's flag
	var carrying *entity  // the flag I am carrying (an enemy team's)
	var enemyFlag *entity // nearest enemy flag
	for i := range ts.Flags {
		f := &ts.Flags[i]
		if f.CarrierID == botID {
			carrying = f
		}
		if f.Team == ts.Team {
			myFlag = f
		} else if enemyFlag == nil || chebyshev(pos, f.Position) < chebyshev(pos, enemyFlag.Position) {
			enemyFlag = f
		}
	}

	// 1. Carrying a flag: run it home (danger-aware). Still trade with a
	// directly adjacent enemy when the weapon is ready, and dodge when hit.
	if carrying != nil && myFlag != nil {
		if near != nil && nearD <= 1 && canAtk && wrange >= 1 && near.HasLOS && !near.Dodging {
			a := atk(near, weapon)
			return &a
		}
		if ts.HitsThisTick > 0 && canDodge && near != nil && nearD <= wrange+2 {
			a := dodgeSafe(ts, perpDir(gridDir(pos, near.Position)))
			return &a
		}
		a := moveTo(pos, myFlag.BasePosition, ts.Danger)
		return &a
	}

	// 2. An enemy carries MY team's flag: the carrier is the priority target.
	if myFlag != nil && myFlag.Status == "carried" && myFlag.CarrierID != "" {
		for i := range ts.Enemies {
			e := &ts.Enemies[i]
			if e.ID != myFlag.CarrierID {
				continue
			}
			if canAtk && chebyshev(pos, e.Position) <= wrange && e.HasLOS && !e.Dodging {
				a := atk(e, weapon)
				return &a
			}
			a := moveTo(pos, e.Position, ts.Danger)
			return &a
		}
		// Carrier outside fog: flag positions are global — give chase while
		// it isn't hopeless.
		if chebyshev(pos, myFlag.Position) <= 20 {
			a := moveTo(pos, myFlag.Position, ts.Danger)
			return &a
		}
	}

	// 3. My team's flag is dropped reasonably close: touch it to return it.
	if myFlag != nil && myFlag.Status == "dropped" && chebyshev(pos, myFlag.Position) < 25 {
		a := moveTo(pos, myFlag.Position, ts.Danger)
		return &a
	}

	// 4. Steal runs: mobile/aggressive personalities go for the enemy flag
	// when healthy and not already pinned in a knife fight; defensive and
	// territorial types keep holding mid instead.
	stealer := strategy == "aggressive" || strategy == "assassin" || strategy == "kite"
	if enemyFlag != nil && stealer && hpRatio > 0.5 && (near == nil || nearD > 2) &&
		(enemyFlag.Status == "at_base" || enemyFlag.Status == "dropped") {
		// Leave the run to a strictly closer visible ally so the whole team
		// doesn't abandon combat for the same flag.
		myD := chebyshev(pos, enemyFlag.Position)
		for i := range ts.Allies {
			if chebyshev(ts.Allies[i].Position, enemyFlag.Position) < myD-1 {
				return nil
			}
		}
		a := moveTo(pos, enemyFlag.Position, ts.Danger)
		return &a
	}
	return nil
}

// === Hazard Zone Helpers ===

// inHazardZone checks if a position is inside any active hazard zone.
// Hazard zones are rectangles (width/height in grid cells, mirroring the
// server's isBotInHazardZone: center cell ± integer half-extents); burn
// fields are radial. Inactive (pulsed-off) zones are safe.
func inHazardZone(pos [2]float64, hazards []entity) bool {
	cx, cy := int(math.Round(pos[0])), int(math.Round(pos[1]))
	for _, h := range hazards {
		if !h.Active {
			continue
		}
		if h.Width > 0 || h.Height > 0 {
			zc, zr := int(math.Round(h.Position[0])), int(math.Round(h.Position[1]))
			halfW, halfH := h.Width/2, h.Height/2
			if cx >= zc-halfW && cx <= zc+halfW && cy >= zr-halfH && cy <= zr+halfH {
				return true
			}
			continue
		}
		r := h.Radius
		if r <= 0 {
			r = 2 // default burn-field radius
		}
		if chebyshev(pos, h.Position) <= r {
			return true
		}
	}
	return false
}

func pickupBlockedByActiveHazard(pos [2]float64, hazards []entity, hazardImmune bool) bool {
	if hazardImmune {
		return false
	}
	return inHazardZone(pos, hazards)
}

// === Cluster Detection (for Staff AoE) ===

// enemyClusterCenter finds the centroid of the largest cluster of enemies
// within clusterRadius of each other. Returns centroid and count.
func enemyClusterCenter(enemies []entity, clusterRadius float64) ([2]float64, int) {
	if len(enemies) == 0 {
		return [2]float64{0, 0}, 0
	}

	bestCenter := enemies[0].Position
	bestCount := 1

	for i := range enemies {
		count := 0
		sx, sy := 0.0, 0.0
		for j := range enemies {
			if chebyshev(enemies[i].Position, enemies[j].Position) <= clusterRadius {
				count++
				sx += enemies[j].Position[0]
				sy += enemies[j].Position[1]
			}
		}
		if count > bestCount {
			bestCount = count
			bestCenter = [2]float64{sx / float64(count), sy / float64(count)}
		}
	}
	return bestCenter, bestCount
}

// enemiesWithinRange counts enemies within range of a position.
func enemiesWithinRange(pos [2]float64, enemies []entity, r float64) int {
	count := 0
	for _, e := range enemies {
		if chebyshev(pos, e.Position) <= r {
			count++
		}
	}
	return count
}

func bestStaffCast(ts *tickState, pos [2]float64, enemies []entity, wrange float64) (*actionResult, bool) {
	candidates := visibleEnemiesInRange(pos, enemies, wrange)
	if len(candidates) == 0 {
		return nil, false
	}

	// Cluster radius 2 matches the staff's server-side AoE radius
	// (weapon_balance.go GridParam: 2) so casts land on cells that actually
	// hit multiple bots — and pass finalizeWeaponAction's radius-2 check.
	clusterCenter, clusterCount := enemyClusterCenter(candidates, 2)
	if clusterCount >= 2 && chebyshev(pos, clusterCenter) <= wrange {
		a := atkPos(clusterCenter, "staff")
		return &a, true
	}

	target := bestTarget(ts, pos, candidates, wrange)
	if target != nil {
		a := atk(target, "staff")
		return &a, true
	}

	near, nearD := closestVisible(pos, candidates)
	if near != nil && nearD <= wrange {
		a := atk(near, "staff")
		return &a, true
	}

	return nil, false
}

func finalizeWeaponAction(ts tickState, weapon string, wrange float64, action actionResult) actionResult {
	if weapon == "bow" && action.Action == "attack" {
		var target *entity
		for i := range ts.Enemies {
			if ts.Enemies[i].ID == action.Target {
				target = &ts.Enemies[i]
				break
			}
		}
		if target != nil && shouldUseChargedBow(ts, target, chebyshev(ts.Position, target.Position), wrange) {
			return chargeAttack(action)
		}
		return action
	}
	if weapon != "staff" || action.Action != "attack" {
		return action
	}

	if action.TargetPosition != nil {
		castPos := *action.TargetPosition
		if chebyshev(ts.Position, castPos) <= wrange {
			candidates := visibleEnemiesInRange(ts.Position, ts.Enemies, wrange)
			if enemiesWithinRange(castPos, candidates, 2) > 0 {
				return action
			}
		}
	}

	if action.Target != "" {
		for i := range ts.Enemies {
			e := &ts.Enemies[i]
			if e.ID == action.Target && e.IsAlive && e.HasLOS && chebyshev(ts.Position, e.Position) <= wrange {
				return atk(e, "staff")
			}
		}
	}

	if best, ok := bestStaffCast(&ts, ts.Position, ts.Enemies, wrange); ok {
		return *best
	}

	near, _ := closestVisible(ts.Position, ts.Enemies)
	if near != nil {
		return moveTo(ts.Position, near.Position, ts.Danger)
	}
	return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
}

// tryImmediateAttack converts a ready, in-range hit before non-emergency
// utility. Previously sword, staff, bow, and ordinary grapple opportunities
// could be displaced by an adjacent boost pickup, mine, or offensive grapple,
// wasting the weapon cooldown window. Spear keeps its brace setup and bow
// keeps intentional charge holds in their weapon-specific branches.
func tryImmediateAttack(ts tickState, weapon string, wrange float64) *actionResult {
	if !ts.WeaponReady {
		return nil
	}
	if weapon == "spear" && !ts.BraceReady {
		return nil
	}
	if weapon == "staff" {
		if cast, ok := bestStaffCast(&ts, ts.Position, ts.Enemies, wrange); ok {
			return cast
		}
		return nil
	}

	target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
	if target == nil {
		return nil
	}
	if weapon == "bow" {
		dist := chebyshev(ts.Position, target.Position)
		if shouldHoldBowCharge(ts, target, dist, wrange) {
			return nil
		}
	}
	a := atk(target, weapon)
	return &a
}
