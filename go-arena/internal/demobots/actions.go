// Action construction: the wire-format actionResult plus the movement,
// attack, dodge, and utility builders tactics compose each tick.
package demobots

import (
	"math"
)

// === Action Builders ===

type actionResult struct {
	Action         string      `json:"action"`
	Target         string      `json:"target,omitempty"`
	Direction      *[2]float64 `json:"direction,omitempty"`
	TargetPosition *[2]float64 `json:"target_position,omitempty"`
	ItemID         string      `json:"item_id,omitempty"`
	Charged        bool        `json:"charged,omitempty"`
	// destination is the movement's eventual goal, kept off the wire. The
	// tactics layer collapses goals into single-cell steps for combat
	// micro-control; the movement router reads this to upgrade long-range
	// travel into server-paced move_to pathfinding.
	destination *[2]float64
}

func moveDir(d [2]float64) actionResult {
	snapped := [2]float64{fsign(d[0]), fsign(d[1])}
	if snapped == [2]float64{} {
		return idle()
	}
	return actionResult{Action: "move", Direction: &snapped}
}

// safeStepDir adjusts a desired step direction so it does not land in a
// danger cell when a safe alternative adjacent step exists. Falls back to
// the original direction when nothing safe (or passable) is available.
func safeStepDir(pos [2]float64, d [2]float64, danger *dangerSet) [2]float64 {
	dx, dy := int(fsign(d[0])), int(fsign(d[1]))
	if dx == 0 && dy == 0 {
		return [2]float64{float64(dx), float64(dy)}
	}
	cx, cy := int(math.Round(pos[0])), int(math.Round(pos[1]))
	t := getTerrain()
	ok := func(sx, sy int) bool {
		if sx == 0 && sy == 0 {
			return false
		}
		if t != nil && t.isMoveBlocked(cx, cy, sx, sy) {
			return false
		}
		return !danger.has(cx+sx, cy+sy)
	}
	if ok(dx, dy) {
		return [2]float64{float64(dx), float64(dy)}
	}
	// Deterministic alternatives near the desired direction: perpendiculars,
	// then axis components, then diagonally-adjacent rotations.
	candidates := [][2]int{
		{-dy, dx}, {dy, -dx}, // perpendicular
		{dx, 0}, {0, dy}, // axis components
		{dx + dy, dy - dx}, {dx - dy, dy + dx}, // 45-degree rotations
	}
	for _, c := range candidates {
		sx, sy := intSign(c[0]), intSign(c[1])
		if ok(sx, sy) {
			return [2]float64{float64(sx), float64(sy)}
		}
	}
	return [2]float64{float64(dx), float64(dy)}
}

// moveDirSafe is moveDir with danger-cell avoidance for the single step.
func moveDirSafe(ts tickState, d [2]float64) actionResult {
	return moveDir(safeStepDir(ts.Position, d, ts.Danger))
}

// moveTo uses BFS pathfinding to take one danger-aware step from src toward
// dst, remembering dst so the movement router can upgrade the step into a
// server-paced move_to when the goal is far away.
func moveTo(src, dst [2]float64, danger *dangerSet) actionResult {
	d := bfsDir(src, dst, danger)
	if d[0] == 0 && d[1] == 0 {
		return idle()
	}
	destination := dst
	return actionResult{Action: "move", Direction: &d, destination: &destination}
}

func atk(t *entity, weapon string) actionResult {
	if weapon == "staff" {
		pos := t.Position
		return actionResult{Action: "attack", TargetPosition: &pos}
	}
	return actionResult{Action: "attack", Target: t.ID}
}

// atkPos attacks a position (for staff AoE targeting cluster centers).
func atkPos(pos [2]float64, weapon string) actionResult {
	a := actionResult{Action: "attack", TargetPosition: &pos}
	return a
}

func dodge(d [2]float64) actionResult {
	snapped := [2]float64{fsign(d[0]), fsign(d[1])}
	if snapped == [2]float64{} {
		return idle()
	}
	return actionResult{Action: "dodge", Direction: &snapped}
}

// dangerEscapeDistance returns the shortest passable number of grid steps to
// a cell outside danger. It is used only when the bot already occupies a
// dangerous cell, where rejecting every dangerous intermediate step would
// otherwise leave it idling in place until death.
func dangerEscapeDistance(col, row int, danger *dangerSet, terrain *botTerrain) int {
	if !danger.has(col, row) {
		return 0
	}

	type escapeNode struct {
		col, row int
		distance int
	}
	queue := []escapeNode{{col: col, row: row}}
	visited := map[[2]int]struct{}{{col, row}: {}}
	directions := [][2]int{
		{1, 0}, {-1, 0}, {0, 1}, {0, -1},
		{1, 1}, {1, -1}, {-1, 1}, {-1, -1},
	}
	const maxEscapeSearch = 32

	for head := 0; head < len(queue); head++ {
		current := queue[head]
		if current.distance >= maxEscapeSearch {
			continue
		}
		for _, dir := range directions {
			if terrain != nil && terrain.isMoveBlocked(current.col, current.row, dir[0], dir[1]) {
				continue
			}
			next := [2]int{current.col + dir[0], current.row + dir[1]}
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = struct{}{}
			distance := current.distance + 1
			if !danger.has(next[0], next[1]) {
				return distance
			}
			queue = append(queue, escapeNode{col: next[0], row: next[1], distance: distance})
		}
	}
	return math.MaxInt32
}

// bestDangerEscapeDir chooses the passable adjacent step with the shortest
// remaining route out of danger. Center-based "move away" heuristics fail for
// overlapping hazards and at the exact center of a rectangular zone, where
// they can produce a zero direction.
func bestDangerEscapeDir(pos [2]float64, danger *dangerSet) ([2]float64, bool) {
	if danger == nil || danger.empty() {
		return [2]float64{}, false
	}
	cx, cy := int(math.Round(pos[0])), int(math.Round(pos[1]))
	if !danger.has(cx, cy) {
		return [2]float64{}, false
	}

	t := getTerrain()
	directions := [][2]int{
		{1, 0}, {-1, 0}, {0, 1}, {0, -1},
		{1, 1}, {1, -1}, {-1, 1}, {-1, -1},
	}
	bestDistance := math.MaxInt32
	var best [2]float64
	found := false
	for _, dir := range directions {
		if t != nil && t.isMoveBlocked(cx, cy, dir[0], dir[1]) {
			continue
		}
		distance := dangerEscapeDistance(cx+dir[0], cy+dir[1], danger, t)
		if !found || distance < bestDistance {
			bestDistance = distance
			best = [2]float64{float64(dir[0]), float64(dir[1])}
			found = true
		}
	}
	return best, found
}

func escapeDanger(ts tickState, canDodge bool) actionResult {
	dir, ok := bestDangerEscapeDir(ts.Position, ts.Danger)
	if !ok {
		return idle()
	}
	if canDodge {
		return dodgeSafe(ts, dir)
	}
	return moveDir(dir)
}

// safeDodgeDir validates both cells of the server's two-cell dodge. A dodge
// may stop after the first cell when the second is a wall, but it must never
// enter danger from safety. When already inside danger, it instead chooses a
// passable dodge whose endpoint is measurably closer to safety.
func safeDodgeDir(pos, desired [2]float64, danger *dangerSet) ([2]float64, bool) {
	dx, dy := int(fsign(desired[0])), int(fsign(desired[1]))
	if dx == 0 && dy == 0 {
		dx = 1
	}
	candidates := [][2]int{
		{dx, dy}, {-dy, dx}, {dy, -dx},
		{dx, 0}, {0, dy}, {-dx, -dy},
		{1, 0}, {-1, 0}, {0, 1}, {0, -1},
		{1, 1}, {1, -1}, {-1, 1}, {-1, -1},
	}
	t := getTerrain()
	cx, cy := int(math.Round(pos[0])), int(math.Round(pos[1]))
	startingInDanger := danger.has(cx, cy)
	startEscapeDistance := dangerEscapeDistance(cx, cy, danger, t)
	bestEscapeDistance := math.MaxInt32
	var bestEscape [2]float64
	foundEscape := false
	seen := make(map[[2]int]struct{}, len(candidates))
	for _, raw := range candidates {
		sx, sy := intSign(raw[0]), intSign(raw[1])
		step := [2]int{sx, sy}
		if sx == 0 && sy == 0 {
			continue
		}
		if _, ok := seen[step]; ok {
			continue
		}
		seen[step] = struct{}{}

		px, py := cx, cy
		moved := false
		safePath := true
		for n := 0; n < 2; n++ {
			if t != nil && t.isMoveBlocked(px, py, sx, sy) {
				break
			}
			px += sx
			py += sy
			if danger.has(px, py) {
				safePath = false
			}
			moved = true
		}
		if moved && safePath {
			return [2]float64{float64(sx), float64(sy)}, true
		}
		if moved && startingInDanger {
			escapeDistance := dangerEscapeDistance(px, py, danger, t)
			if escapeDistance < bestEscapeDistance {
				bestEscapeDistance = escapeDistance
				bestEscape = [2]float64{float64(sx), float64(sy)}
				foundEscape = true
			}
		}
	}
	if startingInDanger && foundEscape && bestEscapeDistance < startEscapeDistance {
		return bestEscape, true
	}
	// If terrain geometry prevents a strictly shorter measured route, still
	// take the best passable raw escape rather than repeating idle forever.
	if startingInDanger && foundEscape {
		return bestEscape, true
	}
	return [2]float64{}, false
}

func dodgeSafe(ts tickState, desired [2]float64) actionResult {
	dir, ok := safeDodgeDir(ts.Position, desired, ts.Danger)
	if !ok {
		return idle()
	}
	return dodge(dir)
}

func shove(id string) actionResult {
	return actionResult{Action: "shove", Target: id}
}

func idle() actionResult {
	return actionResult{Action: "idle"}
}

func placeMine() actionResult {
	return actionResult{Action: "place_mine"}
}

func useItem(id string) actionResult {
	return actionResult{Action: "use_item", ItemID: id}
}

func useGravityWell(pos [2]float64) actionResult {
	return actionResult{Action: "use_gravity_well", TargetPosition: &pos}
}

func grapple(id string) actionResult {
	return actionResult{Action: "grapple", Target: id}
}

func grapplePos(pos [2]float64) actionResult {
	return actionResult{Action: "grapple", TargetPosition: &pos}
}

func chargeAttack(a actionResult) actionResult {
	if a.Action == "attack" {
		a.Charged = true
	}
	return a
}
