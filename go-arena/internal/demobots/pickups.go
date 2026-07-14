// Pickup logic: nearest/typed pickup queries with hazard gating.
package demobots

import (
	"math"
)

// === Pickup Logic ===

const maxSmartPickupTravel = 10

// smartPickupRoutes is a single bounded distance field shared by every pickup
// ranking pass in one decision. A tick commonly evaluates a dozen pickup
// types; rebuilding the same BFS for every candidate would turn live danger
// awareness into an N-pickups x BFS hot path.
type smartPickupRoutes struct {
	src     [2]float64
	start   [2]int
	danger  *dangerSet
	terrain *botTerrain
	scratch *bfsScratch
}

func newSmartPickupRoutes(src [2]float64, danger *dangerSet) smartPickupRoutes {
	routes := smartPickupRoutes{
		src:     src,
		start:   [2]int{int(math.Round(src[0])), int(math.Round(src[1]))},
		danger:  danger,
		terrain: getTerrain(),
	}
	if routes.terrain == nil || routes.terrain.isBlocked(routes.start[0], routes.start[1]) {
		return routes
	}

	scratch := bfsPool.Get().(*bfsScratch)
	scratch.reset(routes.terrain.Width, routes.terrain.Height)
	scratch.visit(routes.start[0], routes.start[1])
	scratch.setDistance(routes.start[0], routes.start[1], 0)
	scratch.queue = append(scratch.queue, bfsNode{col: routes.start[0], row: routes.start[1]})
	for i := 0; i < len(scratch.queue); i++ {
		node := scratch.queue[i]
		if node.distance >= maxSmartPickupTravel {
			continue
		}
		for dc := -1; dc <= 1; dc++ {
			for dr := -1; dr <= 1; dr++ {
				if dc == 0 && dr == 0 || routes.terrain.isMoveBlocked(node.col, node.row, dc, dr) {
					continue
				}
				nextCol, nextRow := node.col+dc, node.row+dr
				if danger.has(nextCol, nextRow) || !scratch.visit(nextCol, nextRow) {
					continue
				}
				distance := node.distance + 1
				scratch.setDistance(nextCol, nextRow, distance)
				scratch.queue = append(scratch.queue, bfsNode{col: nextCol, row: nextRow, distance: distance})
			}
		}
	}
	routes.scratch = scratch
	return routes
}

func (r *smartPickupRoutes) release() {
	if r.scratch != nil {
		bfsPool.Put(r.scratch)
		r.scratch = nil
	}
}

func (r *smartPickupRoutes) distance(dst [2]float64) float64 {
	goal := [2]int{int(math.Round(dst[0])), int(math.Round(dst[1]))}
	if r.terrain == nil {
		if r.danger.has(goal[0], goal[1]) {
			return math.Inf(1)
		}
		return chebyshev(r.src, dst)
	}
	if r.terrain.isBlocked(goal[0], goal[1]) || r.danger.has(goal[0], goal[1]) {
		return math.Inf(1)
	}
	if r.start == goal {
		return 0
	}
	if r.scratch == nil {
		return math.Inf(1)
	}
	if distance, ok := r.scratch.distance(goal[0], goal[1]); ok {
		return float64(distance)
	}
	return math.Inf(1)
}

// smartPickupTravelDistance measures the route under the current tick's
// terrain and danger constraints. The depth bound is semantic rather than an
// arbitrary node budget: smart-pickup policy never chases beyond ten steps,
// so a longer route is intentionally not a candidate.
func smartPickupTravelDistance(src, dst [2]float64, danger *dangerSet) float64 {
	routes := newSmartPickupRoutes(src, danger)
	defer routes.release()
	return routes.distance(dst)
}

// nearestHealthPickup returns the closest health_pack pickup.
func nearestHealthPickup(pos [2]float64, pickups, hazards []entity, hazardImmune bool) (*entity, float64) {
	return nearestHealthPickupWithDanger(pos, pickups, hazards, hazardImmune, nil)
}

func nearestHealthPickupWithDanger(pos [2]float64, pickups, hazards []entity, hazardImmune bool, danger *dangerSet) (*entity, float64) {
	routes := newSmartPickupRoutes(pos, danger)
	defer routes.release()
	return nearestHealthPickupWithRoutes(pickups, hazards, hazardImmune, &routes)
}

func nearestHealthPickupWithRoutes(pickups, hazards []entity, hazardImmune bool, routes *smartPickupRoutes) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pickups {
		if pickups[i].SubType != "health_pack" {
			continue
		}
		if pickupBlockedByActiveHazard(pickups[i].Position, hazards, hazardImmune) {
			continue
		}
		d := routes.distance(pickups[i].Position)
		if d < bestD {
			bestD = d
			best = &pickups[i]
		}
	}
	return best, bestD
}

// nearestPickup returns the closest pickup of any type.
func nearestPickup(pos [2]float64, pickups, hazards []entity, hazardImmune bool) (*entity, float64) {
	return nearestPickupWithDanger(pos, pickups, hazards, hazardImmune, nil)
}

func nearestPickupWithDanger(pos [2]float64, pickups, hazards []entity, hazardImmune bool, danger *dangerSet) (*entity, float64) {
	routes := newSmartPickupRoutes(pos, danger)
	defer routes.release()
	return nearestPickupWithRoutes(pickups, hazards, hazardImmune, &routes)
}

func nearestPickupWithRoutes(pickups, hazards []entity, hazardImmune bool, routes *smartPickupRoutes) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pickups {
		if pickupBlockedByActiveHazard(pickups[i].Position, hazards, hazardImmune) {
			continue
		}
		d := routes.distance(pickups[i].Position)
		if d < bestD {
			bestD = d
			best = &pickups[i]
		}
	}
	return best, bestD
}

// nearestPickupOfType returns the closest pickup of a specific subtype.
func nearestPickupOfType(pos [2]float64, pickups, hazards []entity, hazardImmune bool, subType string) (*entity, float64) {
	return nearestPickupOfTypeWithDanger(pos, pickups, hazards, hazardImmune, subType, nil)
}

func nearestPickupOfTypeWithDanger(pos [2]float64, pickups, hazards []entity, hazardImmune bool, subType string, danger *dangerSet) (*entity, float64) {
	routes := newSmartPickupRoutes(pos, danger)
	defer routes.release()
	return nearestPickupOfTypeWithRoutes(pickups, hazards, hazardImmune, subType, &routes)
}

func nearestPickupOfTypeWithRoutes(pickups, hazards []entity, hazardImmune bool, subType string, routes *smartPickupRoutes) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pickups {
		if pickups[i].SubType != subType {
			continue
		}
		if pickupBlockedByActiveHazard(pickups[i].Position, hazards, hazardImmune) {
			continue
		}
		d := routes.distance(pickups[i].Position)
		if d < bestD {
			bestD = d
			best = &pickups[i]
		}
	}
	return best, bestD
}

func nearestCapturePad(pos [2]float64, pads []entity) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pads {
		d := tacticalTravelDistance(pos, pads[i].Position)
		if d < bestD {
			bestD = d
			best = &pads[i]
		}
	}
	return best, bestD
}

func tryCapturePadObjective(ts tickState, strategy string, near *entity, nearD float64, botID string) *actionResult {
	if len(ts.CapturePads) == 0 || !ts.InZone {
		return nil
	}

	hpRatio := ts.HP / math.Max(ts.MaxHP, 1)
	pressure := enemiesWithinRange(ts.Position, ts.Enemies, 4)
	objectiveBias := strategy == "territorial" || strategy == "defensive" || strategy == "aggressive"
	opponentKnown := len(ts.Enemies) > 0
	if !opponentKnown {
		for _, h := range ts.Hints {
			if h.HintType == "bot" {
				opponentKnown = true
				break
			}
		}
	}

	// Ready pads can be captured now. A cooling-down pad cannot be captured,
	// but its owner must remain the sole contender to receive control pulses.
	// The old early !Ready return made that ownership branch unreachable.
	var pad, heldPad *entity
	padD, heldPadD := math.Inf(1), math.Inf(1)
	for i := range ts.CapturePads {
		candidate := &ts.CapturePads[i]
		d := chebyshev(ts.Position, candidate.Position)
		if candidate.Ready && d < padD {
			pad, padD = candidate, d
		}
		if !candidate.Ready && candidate.OwnerID == botID && d < heldPadD {
			heldPad, heldPadD = candidate, d
		}
	}
	if heldPad != nil && !heldPad.Contested && pressure <= 1 && hpRatio >= 0.45 && !opponentKnown {
		if heldPadD <= 1 && pressure == 0 {
			a := idle()
			return &a
		}
		if heldPadD <= 7 && (pad == nil || heldPadD+2 < padD) {
			a := moveTo(ts.Position, heldPad.Position, ts.Danger)
			return &a
		}
	}
	if pad == nil {
		return nil
	}

	enemyOwned := pad.OwnerID != "" && pad.OwnerID != botID

	if hpRatio < 0.35 && pressure > 0 {
		return nil
	}
	if pressure >= 2 && hpRatio < 0.6 && !objectiveBias {
		return nil
	}
	if ts.FastZone && (ts.ZoneDist < 4 || padD > 6) {
		return nil
	}
	if near != nil && nearD <= 2 && ts.WeaponReady {
		return nil
	}
	if padD <= 1 && !pad.Contested {
		a := idle()
		return &a
	}
	if pad.Contested && padD <= 8 && (objectiveBias || ts.HasHazardKey || ts.IsBountyTarget) {
		a := moveTo(ts.Position, pad.Position, ts.Danger)
		return &a
	}
	if pad.CapturingBotID != "" && pad.CapturingBotID != botID && padD <= 8 {
		a := moveTo(ts.Position, pad.Position, ts.Danger)
		return &a
	}
	if padD <= 9 && (pressure == 0 || objectiveBias || enemyOwned || ts.IsBountyTarget) {
		a := moveTo(ts.Position, pad.Position, ts.Danger)
		return &a
	}
	return nil
}
