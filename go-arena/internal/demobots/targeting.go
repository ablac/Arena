// Target selection: enemy scoring, backstab/flank/bash picks, ranged
// threat assessment, and line-of-sight break search.
package demobots

import (
	"math"

	"arena-server/internal/config"
)

// === Target Selection ===

func closest(pos [2]float64, enemies []entity) (*entity, float64) {
	var b *entity
	bd := math.Inf(1)
	for i := range enemies {
		d := tacticalTravelDistance(pos, enemies[i].Position)
		if d < bd {
			bd = d
			b = &enemies[i]
		}
	}
	return b, bd
}

func closestVisible(pos [2]float64, enemies []entity) (*entity, float64) {
	var b *entity
	bd := math.Inf(1)
	for i := range enemies {
		if !enemies[i].HasLOS {
			continue
		}
		d := tacticalTravelDistance(pos, enemies[i].Position)
		if d < bd {
			bd = d
			b = &enemies[i]
		}
	}
	return b, bd
}

func countVisibleEnemies(enemies []entity) int {
	count := 0
	for _, e := range enemies {
		if e.HasLOS {
			count++
		}
	}
	return count
}

func hasVisibleRangedThreat(enemies []entity) bool {
	for _, e := range enemies {
		if !e.HasLOS {
			continue
		}
		if e.Weapon == "bow" || e.Weapon == "staff" {
			return true
		}
	}
	return false
}

// teamTargetBonus coordinates team-mode demo bots without sharing any state
// beyond the public nearby-entity protocol. Focus fire finishes targets before
// they can heal, while a smaller protection bonus peels enemies off a wounded
// ally. In FFA ts.Allies is empty, so targeting is unchanged.
func teamTargetBonus(ts *tickState, enemyID string) float64 {
	if ts == nil || enemyID == "" {
		return 0
	}
	bonus := 0.0
	for i := range ts.Allies {
		ally := &ts.Allies[i]
		if ally.TargetID == enemyID {
			bonus += 32
		}
		if ally.MaxHP > 0 && ally.HP/ally.MaxHP <= 0.4 {
			for j := range ts.Enemies {
				if ts.Enemies[j].ID == enemyID && ts.Enemies[j].TargetID == ally.ID {
					bonus += 24
					break
				}
			}
		}
	}
	return math.Min(bonus, 96)
}

// bestTarget picks the optimal attack target in weapon range, weighting
// low HP, stuns, proximity, threat score, bounty targets, and (in CTF)
// enemy flag carriers.
func bestTarget(ts *tickState, pos [2]float64, enemies []entity, wrange float64) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if e.Dodging {
			continue
		}
		if !e.HasLOS {
			continue
		}
		d := chebyshev(pos, e.Position)
		if d > wrange {
			continue
		}
		score := 100 - e.HP
		if e.Stunned {
			score += 50
		}
		score -= d * 5
		if e.HP < e.MaxHP*0.3 {
			score += 40
		}
		score += e.ThreatScore * 0.3
		if e.Type == "bounty_target" || (ts.BountyTargetID != "" && e.ID == ts.BountyTargetID) {
			score += 120
		}
		if ts.Mode == "ctf" && ts.isFlagCarrier(e.ID) {
			score += 80
		}
		score += teamTargetBonus(ts, e.ID)
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

func isRearArc(attackerPos [2]float64, target entity) bool {
	fx, fy := target.Facing[0], target.Facing[1]
	if math.Abs(fx)+math.Abs(fy) < 0.01 {
		return false
	}
	dx := attackerPos[0] - target.Position[0]
	dy := attackerPos[1] - target.Position[1]
	dist := math.Hypot(dx, dy)
	if dist < 0.01 {
		return false
	}
	dx /= dist
	dy /= dist
	dot := fx*dx + fy*dy
	return dot <= -0.35
}

func bestBackstabTarget(pos [2]float64, enemies []entity, wrange float64) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if !e.IsAlive || !e.HasLOS || e.Dodging {
			continue
		}
		d := chebyshev(pos, e.Position)
		if d > wrange {
			continue
		}
		score := 100 - e.HP - d*4
		if e.RearExposed || isRearArc(pos, *e) {
			score += 65
		}
		if e.Stunned || e.DisruptedTicks > 0 {
			score += 30
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

func daggerFlankPosition(ts tickState, target *entity) ([2]float64, bool) {
	if target == nil {
		return [2]float64{}, false
	}
	fx, fy := fsign(target.Facing[0]), fsign(target.Facing[1])
	if fx == 0 && fy == 0 {
		return [2]float64{}, false
	}
	behind := [2]float64{target.Position[0] - fx, target.Position[1] - fy}
	c, r := int(math.Round(behind[0])), int(math.Round(behind[1]))
	if terrainBlocked(c, r) || ts.Danger.has(c, r) {
		return [2]float64{}, false
	}
	return behind, true
}

func bestShieldBashTarget(pos [2]float64, enemies []entity, wrange float64) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if !e.IsAlive || !e.HasLOS || e.Dodging {
			continue
		}
		d := chebyshev(pos, e.Position)
		if d > wrange {
			continue
		}
		score := 100 - e.HP - d*5
		if e.DisruptedTicks > 0 || e.Stunned {
			score += 80
		}
		if e.Weapon == "bow" || e.Weapon == "staff" {
			score += 12
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

func terrainBlocked(col, row int) bool {
	t := getTerrain()
	if t == nil {
		return false
	}
	return t.isBlocked(col, row)
}

// gridLineBlocked reports whether any wall/void cell lies on the grid line
// from a to b (start cell excluded). Bot-side approximation of the server's
// LOS check whose result arrives as has_los on nearby views; used for cells
// where we have no server-provided answer (e.g. candidate cover cells).
func (t *botTerrain) gridLineBlocked(a, b [2]int) bool {
	x0, y0 := a[0], a[1]
	x1, y1 := b[0], b[1]
	dx, dy := x1-x0, y1-y0
	sx, sy := intSign(dx), intSign(dy)
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	err := dx - dy
	for {
		if x0 == x1 && y0 == y1 {
			return false
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
		if t.isBlocked(x0, y0) && !(x0 == x1 && y0 == y1) {
			return true
		}
	}
}

// strongestRangedThreat returns the visible bow/staff enemy with the highest
// threat score (falling back to HP when the server didn't send one).
func strongestRangedThreat(ts *tickState) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range ts.Enemies {
		e := &ts.Enemies[i]
		if !e.HasLOS || !e.IsAlive {
			continue
		}
		if e.Weapon != "bow" && e.Weapon != "staff" {
			continue
		}
		score := e.ThreatScore
		if score <= 0 {
			score = e.HP
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

// findLOSBreakCell samples the 8 neighbors and the 16 cells at Chebyshev
// radius 2 around pos and returns the first passable, non-dangerous cell
// where terrain blocks the line to the threat. Used to duck out of ranged
// fire instead of trading into it.
func findLOSBreakCell(pos, threatPos [2]float64, danger *dangerSet) ([2]float64, bool) {
	t := getTerrain()
	if t == nil {
		return [2]float64{}, false
	}
	cx, cy := int(math.Round(pos[0])), int(math.Round(pos[1]))
	tc := [2]int{int(math.Round(threatPos[0])), int(math.Round(threatPos[1]))}
	for r := 1; r <= 2; r++ {
		for dx := -r; dx <= r; dx++ {
			for dy := -r; dy <= r; dy++ {
				if dx != -r && dx != r && dy != -r && dy != r {
					continue // ring cells only
				}
				c, w := cx+dx, cy+dy
				if t.isBlocked(c, w) || danger.has(c, w) {
					continue
				}
				if t.gridLineBlocked([2]int{c, w}, tc) {
					return [2]float64{float64(c), float64(w)}, true
				}
			}
		}
	}
	return [2]float64{}, false
}

func nearImpactSurface(pos [2]float64) bool {
	col, row := int(math.Round(pos[0])), int(math.Round(pos[1]))
	if terrainBlocked(col-1, row) || terrainBlocked(col+1, row) || terrainBlocked(col, row-1) || terrainBlocked(col, row+1) {
		return true
	}
	return false
}

func bestGrappleSlamTarget(pos [2]float64, enemies []entity, wrange float64) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if !e.IsAlive || !e.HasLOS || e.Dodging {
			continue
		}
		d := chebyshev(pos, e.Position)
		if d < 3 || d > wrange {
			continue
		}
		if !(e.NearImpactSurface || nearImpactSurface(e.Position)) {
			continue
		}
		score := 100 - e.HP - d*4
		if e.Weapon == "bow" || e.Weapon == "staff" {
			score += 18
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

func shouldUseChargedBow(ts tickState, target *entity, dist, wrange float64) bool {
	if target == nil || ts.BowChargeTicks <= 0 {
		return false
	}
	if target.Stunned {
		return true
	}
	if ts.ChargedShotReady && dist >= math.Max(4, wrange-2) {
		return true
	}
	if ts.BowChargeTicks >= 4 && (target.Weapon == "staff" || target.Weapon == "bow") {
		return true
	}
	return ts.BowChargeTicks >= 5
}

func shouldHoldBowCharge(ts tickState, target *entity, dist, wrange float64) bool {
	if target == nil || !target.HasLOS {
		return false
	}
	if ts.ChargedShotReady {
		return false
	}
	if ts.BowChargeTicks >= config.C.BowChargeReadyTicks {
		return false
	}
	if dist <= 2 {
		return false
	}
	if enemiesWithinRange(ts.Position, ts.Enemies, 2) >= 2 {
		return false
	}
	if target.Stunned {
		return true
	}
	if target.Weapon == "bow" || target.Weapon == "staff" {
		return true
	}
	return dist >= math.Max(4, wrange-2)
}

func visibleEnemiesInRange(pos [2]float64, enemies []entity, wrange float64) []entity {
	filtered := make([]entity, 0, len(enemies))
	for _, e := range enemies {
		if !e.IsAlive || !e.HasLOS || e.Dodging {
			continue
		}
		if chebyshev(pos, e.Position) > wrange {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// weakest returns the enemy with the lowest HP.
func weakest(pos [2]float64, enemies []entity) (*entity, float64) {
	var best *entity
	bestHP := math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if !e.HasLOS {
			continue
		}
		hp := e.HP
		if e.Stunned {
			hp -= 50
		}
		if hp < bestHP {
			bestHP = hp
			best = e
		}
	}
	if best == nil {
		return nil, 0
	}
	return best, tacticalTravelDistance(pos, best.Position)
}

// isMelee returns true if the weapon is short range.
func isMelee(weapon string) bool {
	return weapon == "sword" || weapon == "daggers" || weapon == "shield" || weapon == "spear" || weapon == "grapple"
}
