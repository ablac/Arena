// Movement routing: upgrades destination-carrying movement decisions from
// the tactics layer into server-paced move_to actions.
//
// The server's move_to executes authoritative pathfinding (NavGrid A* with
// per-edge wall checks) against terrain that can never be stale, and it
// paces the bot across multiple ticks. Legacy demo bots instead re-ran a
// client-side BFS every tick and emitted one-cell "move" steps, which
// ground against wall corners and oscillated whenever tactics re-targeted —
// the "stuck on spiral/rooms/caves" behavior.
//
// The router keeps the client-side grid work only where it still matters:
// deciding whether the shortest route crosses danger (hazards, mines, void
// tiles, ready teleporter pads) that the server's pathfinder knows nothing
// about. Clean routes delegate wholly to the server; dangerous routes are
// walked as a chain of straight, danger-free move_to segments.
package demobots

import "math"

const (
	// Destinations closer than this keep the legacy single-step behavior:
	// combat micro-spacing wants immediate direction changes, not paths.
	routeMinDelegateDistance = 3
	// How many ticks a near-identical destination keeps the previous goal,
	// so a target jittering across a cell boundary cannot thrash the route.
	routeGoalStickTicks = 8
	// Longest straight move_to segment emitted while skirting danger.
	routeMaxSegment = 12
)

// movementRouter holds the tiny per-bot goal hysteresis state.
type movementRouter struct {
	hasGoal   bool
	goal      [2]float64
	ticksHeld int
}

func (r *movementRouter) reset() {
	*r = movementRouter{}
}

// route inspects a tactics-layer action and, when it is a long-range move
// with a known destination, replaces the single-cell step with a move_to.
// Anything else (attacks, dodges, short hops, direction-only strafes, and
// navigation-recovery corrections, which run after this) passes through.
func (r *movementRouter) route(msg map[string]interface{}, requested actionResult, botID string) actionResult {
	if requested.Action != "move" || requested.destination == nil {
		return requested
	}
	t := getTerrain()
	if t == nil {
		return requested
	}

	ts := parseTick(msg)
	start := [2]int{int(math.Round(ts.Position[0])), int(math.Round(ts.Position[1]))}
	destination := *requested.destination
	goal := [2]int{int(math.Round(destination[0])), int(math.Round(destination[1]))}
	// Combat micro-spacing keeps the reactive single-step behavior, but only
	// while the direct line is actually walkable; a goal two cells away
	// around a wall corner still deserves a real path.
	if intChebyshev(start, goal) < routeMinDelegateDistance && !t.gridLineBlocked(start, goal) {
		return requested
	}
	// Tactics sometimes aim at an unwalkable cell (an enemy standing half in
	// a doorway, a pickup on a ledge seam). The legacy step logic already
	// walks toward the closest reachable cell; keep it for those.
	if t.isBlocked(goal[0], goal[1]) {
		return requested
	}

	// Goal hysteresis: while the destination wobbles within one cell (a
	// chased bot crossing cell boundaries), keep steering at the previous
	// goal so the emitted move_to stays stable.
	if r.hasGoal && r.ticksHeld < routeGoalStickTicks {
		lastCell := [2]int{int(math.Round(r.goal[0])), int(math.Round(r.goal[1]))}
		if intChebyshev(lastCell, goal) <= 1 {
			goal = lastCell
			destination = r.goal
			r.ticksHeld++
		} else {
			r.goal = destination
			r.ticksHeld = 0
		}
	} else {
		r.hasGoal = true
		r.goal = destination
		r.ticksHeld = 0
	}

	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	buildDangerSet(danger, &ts, botID)

	// A nil path means the goal is unreachable (another island, a sealed
	// room). The legacy closest-approach stepping handles that better than
	// an idling server path, so fall back.
	plain := planGridPath(start, goal, nil)
	if plain == nil {
		return requested
	}
	if !pathCrossesDanger(plain, danger) {
		target := destination
		return actionResult{Action: "move_to", TargetPosition: &target, destination: requested.destination}
	}

	// Danger sits on the shortest route: plan around it client-side and
	// emit the furthest straight danger-free segment. The server walks
	// straight lines between LOS-clear waypoints, so a clear segment here
	// cannot be re-routed through the hazard.
	safe := planGridPath(start, goal, danger)
	if safe == nil {
		return requested
	}
	waypoint := furthestClearWaypoint(t, start, safe, danger)
	target := [2]float64{float64(waypoint[0]), float64(waypoint[1])}
	return actionResult{Action: "move_to", TargetPosition: &target, destination: requested.destination}
}

// planGridPath returns the full shortest 8-connected path from start to
// goal as grid cells (start excluded, goal included), honoring the same
// wall corner-cutting rules as movement and, when danger is non-nil,
// refusing dangerous cells. Returns nil when no such path exists.
func planGridPath(start, goal [2]int, danger *dangerSet) [][2]int {
	t := getTerrain()
	if t == nil {
		return nil
	}
	if start == goal {
		return [][2]int{}
	}
	if t.isBlocked(goal[0], goal[1]) || t.isBlocked(start[0], start[1]) {
		return nil
	}

	blocked := func(cx, cy, dx, dy int) bool {
		if t.isMoveBlocked(cx, cy, dx, dy) {
			return true
		}
		return danger.has(cx+dx, cy+dy)
	}

	s := bfsPool.Get().(*bfsScratch)
	defer bfsPool.Put(s)
	s.reset(t.Width, t.Height)
	s.ensureParents()

	startIdx := int32(start[0]*t.Height + start[1])
	s.visit(start[0], start[1])
	s.parents[startIdx] = -1
	s.queue = append(s.queue, bfsNode{col: start[0], row: start[1]})

	goalIdx := int32(goal[0]*t.Height + goal[1])
	found := false
	for i := 0; i < len(s.queue) && !found; i++ {
		n := s.queue[i]
		fromIdx := int32(n.col*t.Height + n.row)
		for dc := -1; dc <= 1 && !found; dc++ {
			for dr := -1; dr <= 1; dr++ {
				if dc == 0 && dr == 0 {
					continue
				}
				if blocked(n.col, n.row, dc, dr) {
					continue
				}
				nc, nr := n.col+dc, n.row+dr
				if !s.visit(nc, nr) {
					continue
				}
				idx := int32(nc*t.Height + nr)
				s.parents[idx] = fromIdx
				if idx == goalIdx {
					found = true
					break
				}
				s.queue = append(s.queue, bfsNode{col: nc, row: nr})
			}
		}
	}
	if !found {
		return nil
	}

	// Reconstruct goal -> start, then reverse in place.
	var path [][2]int
	for idx := goalIdx; idx != startIdx; idx = s.parents[idx] {
		path = append(path, [2]int{int(idx) / t.Height, int(idx) % t.Height})
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// pathCrossesDanger reports whether any cell of the path is dangerous
// (lethal danger or an unexempted teleporter-pad footprint).
func pathCrossesDanger(path [][2]int, danger *dangerSet) bool {
	if danger.empty() {
		return false
	}
	for _, cell := range path {
		if danger.has(cell[0], cell[1]) {
			return true
		}
	}
	return false
}

// furthestClearWaypoint picks the furthest path cell within routeMaxSegment
// that the bot can reach on a straight wall-free, danger-free line. Falls
// back to the first path step, which is safe by construction.
func furthestClearWaypoint(t *botTerrain, start [2]int, path [][2]int, danger *dangerSet) [2]int {
	for i := len(path) - 1; i > 0; i-- {
		cell := path[i]
		if intChebyshev(start, cell) > routeMaxSegment {
			continue
		}
		if t.gridLineBlocked(start, cell) {
			continue
		}
		if lineCrossesDanger(start, cell, danger) {
			continue
		}
		return cell
	}
	return path[0]
}

// lineCrossesDanger walks the grid line from a to b (excluding a) with
// supercover stepping — a diagonal step also checks both orthogonal
// intermediates — and reports whether any touched cell is dangerous. The
// widened check matters because the server's own pacing may wiggle one cell
// off the ideal Bresenham line.
func lineCrossesDanger(a, b [2]int, danger *dangerSet) bool {
	if danger.empty() {
		return false
	}
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
		steppedX, steppedY := false, false
		if e2 > -dy {
			err -= dy
			x0 += sx
			steppedX = true
		}
		if e2 < dx {
			err += dx
			y0 += sy
			steppedY = true
		}
		if danger.has(x0, y0) {
			return true
		}
		// Supercover: on a diagonal step, the physical route may pass
		// through either orthogonal neighbor.
		if steppedX && steppedY {
			if danger.has(x0-sx, y0) || danger.has(x0, y0-sy) {
				return true
			}
		}
	}
}
