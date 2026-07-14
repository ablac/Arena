// Grid pathfinding primitives shared by the demo bots: pooled BFS
// scratch, first-step queries, bounded travel-distance estimates, and
// the small geometry helpers they lean on.
package demobots

import (
	"math"
	"math/rand"
	"sync"
)

// === BFS Pathfinding ===

type bfsNode struct {
	col, row         int
	firstDC, firstDR int
	distance         int
}

// bfsScratch is reusable BFS working memory. The old implementation
// allocated a map[[2]int]bool plus a queue on every call — at 10 ticks/sec
// per demo bot, with almost every AI branch calling moveTo, that was the
// dominant allocation source in the demobot package. A generation-stamped
// flat array avoids both the per-call allocation and the hashing.
type bfsScratch struct {
	visited    []uint32
	distances  []uint8
	parents    []int32 // valid only where visited == stamp; see ensureParents
	stamp      uint32
	queue      []bfsNode
	cols, rows int
}

var bfsPool = sync.Pool{New: func() interface{} { return &bfsScratch{} }}

func (s *bfsScratch) reset(cols, rows int) {
	if s.cols != cols || s.rows != rows || len(s.visited) != cols*rows || len(s.distances) != cols*rows {
		s.visited = make([]uint32, cols*rows)
		s.distances = make([]uint8, cols*rows)
		s.cols, s.rows = cols, rows
		s.stamp = 0
	}
	s.stamp++
	if s.stamp == 0 { // generation counter wrapped: hard-clear once
		for i := range s.visited {
			s.visited[i] = 0
		}
		s.stamp = 1
	}
	s.queue = s.queue[:0]
}

// ensureParents sizes the parent-index array used by full-path planning.
// Cells are only read where visited matches the current stamp, so stale
// values never need clearing.
func (s *bfsScratch) ensureParents() {
	if len(s.parents) != s.cols*s.rows {
		s.parents = make([]int32, s.cols*s.rows)
	}
}

// visit marks the cell and reports whether it was newly visited.
// Out-of-grid cells count as already visited so they are never enqueued.
func (s *bfsScratch) visit(c, r int) bool {
	if c < 0 || r < 0 || c >= s.cols || r >= s.rows {
		return false
	}
	idx := c*s.rows + r
	if s.visited[idx] == s.stamp {
		return false
	}
	s.visited[idx] = s.stamp
	return true
}

func (s *bfsScratch) setDistance(c, r, distance int) {
	s.distances[c*s.rows+r] = uint8(distance)
}

func (s *bfsScratch) distance(c, r int) (int, bool) {
	if c < 0 || r < 0 || c >= s.cols || r >= s.rows {
		return 0, false
	}
	idx := c*s.rows + r
	if s.visited[idx] != s.stamp {
		return 0, false
	}
	return int(s.distances[idx]), true
}

// bfsStep finds the first grid step direction from (sc,sr) toward (gc,gr),
// navigating walls and avoiding danger cells (hazards, void tiles, mines).
// If no safe step exists at all, it retries once ignoring lethal danger so
// bots never freeze when fully surrounded, while still preserving teleporter
// avoidance. Returns [2]int{dx, dy} in {-1,0,1}.
func bfsStep(sc, sr, gc, gr int, danger *dangerSet) [2]int {
	step, ok := bfsStepConstrained(sc, sr, gc, gr, danger)
	if !ok && !danger.empty() {
		// Preserve teleporter avoidance in the last-resort route. Lethal danger
		// may be ignored when otherwise fully enclosed (the historical escape
		// behavior), but an ordinary navigation decision must never silently
		// become an accidental teleport.
		step, _ = bfsStepConstrainedMode(sc, sr, gc, gr, danger, true)
	}
	return step
}

// bfsStepConstrained is bfsStep with a fixed danger set (nil = ignore danger).
// The boolean result reports whether any step (including the heuristic
// fallbacks) was found under the constraints.
func bfsStepConstrained(sc, sr, gc, gr int, danger *dangerSet) ([2]int, bool) {
	return bfsStepConstrainedMode(sc, sr, gc, gr, danger, false)
}

func bfsStepConstrainedMode(sc, sr, gc, gr int, danger *dangerSet, ignoreLethal bool) ([2]int, bool) {
	if sc == gc && sr == gr {
		return [2]int{0, 0}, true
	}

	t := getTerrain()
	if t == nil {
		return [2]int{intSign(gc - sc), intSign(gr - sr)}, true
	}

	blocked := func(cx, cy, dx, dy int) bool {
		if t.isMoveBlocked(cx, cy, dx, dy) {
			return true
		}
		nc, nr := cx+dx, cy+dy
		if ignoreLethal {
			return danger.hasPad(nc, nr)
		}
		return danger.has(nc, nr)
	}

	s := bfsPool.Get().(*bfsScratch)
	defer bfsPool.Put(s)
	s.reset(t.Width, t.Height)
	s.visit(sc, sr)

	// Seed with all passable neighbors (diagonal corner-cutting prevented).
	for dc := -1; dc <= 1; dc++ {
		for dr := -1; dr <= 1; dr++ {
			if dc == 0 && dr == 0 {
				continue
			}
			if blocked(sc, sr, dc, dr) {
				continue
			}
			nc, nr := sc+dc, sr+dr
			if s.visit(nc, nr) {
				s.queue = append(s.queue, bfsNode{col: nc, row: nr, firstDC: dc, firstDR: dr, distance: 1})
			}
		}
	}

	// Track the explored cell that gets closest to the goal: when the goal is
	// unreachable (wall cell, disconnected pocket), stepping toward the
	// closest reachable cell beats the greedy fallback that grinds into the
	// nearest wall face.
	goal := [2]int{gc, gr}
	bestDist := intChebyshev([2]int{sc, sr}, goal)
	var bestStep [2]int
	haveBest := false

	// No artificial node budget: the visited stamps already bound the walk to
	// one visit per grid cell. A capped walk (formerly 1800 nodes ≈ 20 tiles
	// of radius) exhausted on every cross-map caves path, dropping bots into
	// the greedy fallback where they ground against walls.
	for i := 0; i < len(s.queue); i++ {
		n := s.queue[i]
		if n.col == gc && n.row == gr {
			return [2]int{n.firstDC, n.firstDR}, true
		}
		if d := intChebyshev([2]int{n.col, n.row}, goal); d < bestDist {
			bestDist = d
			bestStep = [2]int{n.firstDC, n.firstDR}
			haveBest = true
		}
		for dc := -1; dc <= 1; dc++ {
			for dr := -1; dr <= 1; dr++ {
				if dc == 0 && dr == 0 {
					continue
				}
				if blocked(n.col, n.row, dc, dr) {
					continue
				}
				if s.visit(n.col+dc, n.row+dr) {
					s.queue = append(s.queue, bfsNode{
						col: n.col + dc, row: n.row + dr,
						firstDC: n.firstDC, firstDR: n.firstDR, distance: n.distance + 1,
					})
				}
			}
		}
	}

	// Goal unreachable — approach the closest reachable cell instead.
	if haveBest {
		return bestStep, true
	}

	// BFS exhausted — fall back to direct direction toward goal.
	direct := [2]int{intSign(gc - sc), intSign(gr - sr)}
	if !blocked(sc, sr, direct[0], direct[1]) {
		return direct, true
	}
	if direct[0] != 0 && !blocked(sc, sr, direct[0], 0) {
		return [2]int{direct[0], 0}, true
	}
	if direct[1] != 0 && !blocked(sc, sr, 0, direct[1]) {
		return [2]int{0, direct[1]}, true
	}
	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	rand.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })
	for _, d := range dirs {
		if !blocked(sc, sr, d[0], d[1]) {
			return d, true
		}
	}
	return [2]int{0, 0}, false
}

// tacticalTravelDistance returns a measured shortest-path distance when a
// nearby target is separated by carved terrain. The search is deliberately
// bounded: targets with a clear grid line or outside local decision range use
// the cheap Chebyshev distance, while room walls, island channels, and donut
// cores within fog receive an actual route cost without adding an unbounded
// second full-map BFS to every decision.
func tacticalTravelDistance(src, dst [2]float64) float64 {
	direct := chebyshev(src, dst)
	t := getTerrain()
	if t == nil || direct == 0 || direct > 12 {
		return direct
	}
	start := [2]int{int(math.Round(src[0])), int(math.Round(src[1]))}
	goal := [2]int{int(math.Round(dst[0])), int(math.Round(dst[1]))}
	if t.isBlocked(goal[0], goal[1]) {
		return math.Inf(1)
	}
	if !t.gridLineBlocked(start, goal) {
		return direct
	}

	const nodeLimit = 768
	s := bfsPool.Get().(*bfsScratch)
	defer bfsPool.Put(s)
	s.reset(t.Width, t.Height)
	s.visit(start[0], start[1])
	s.queue = append(s.queue, bfsNode{col: start[0], row: start[1]})

	for i := 0; i < len(s.queue) && i < nodeLimit; i++ {
		n := s.queue[i]
		if n.col == goal[0] && n.row == goal[1] {
			return float64(n.distance)
		}
		for dc := -1; dc <= 1; dc++ {
			for dr := -1; dr <= 1; dr++ {
				if dc == 0 && dr == 0 || t.isMoveBlocked(n.col, n.row, dc, dr) {
					continue
				}
				nc, nr := n.col+dc, n.row+dr
				if s.visit(nc, nr) {
					s.queue = append(s.queue, bfsNode{col: nc, row: nr, distance: n.distance + 1})
				}
			}
		}
	}
	return math.Inf(1)
}

func intSign(v int) int {
	if v > 0 {
		return 1
	}
	if v < 0 {
		return -1
	}
	return 0
}

// === Math Helpers ===

// chebyshev returns the Chebyshev (grid) distance — matches server range checks.
func chebyshev(a, b [2]float64) float64 {
	return math.Max(math.Abs(a[0]-b[0]), math.Abs(a[1]-b[1]))
}

// intChebyshev is chebyshev for integer grid cells.
func intChebyshev(a, b [2]int) int {
	dx := a[0] - b[0]
	if dx < 0 {
		dx = -dx
	}
	dy := a[1] - b[1]
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

func fsign(v float64) float64 {
	if v > 0.01 {
		return 1
	}
	if v < -0.01 {
		return -1
	}
	return 0
}

// gridDir returns the grid direction [-1,0,1] from src toward dst.
func gridDir(src, dst [2]float64) [2]float64 {
	return [2]float64{fsign(dst[0] - src[0]), fsign(dst[1] - src[1])}
}

// gridDirAway returns the grid direction away from dst.
func gridDirAway(src, dst [2]float64) [2]float64 {
	d := gridDir(src, dst)
	return [2]float64{-d[0], -d[1]}
}

// bfsDir uses BFS to get the first step direction from src toward dst,
// avoiding cells in the danger set (nil = no danger constraints).
func bfsDir(src, dst [2]float64, danger *dangerSet) [2]float64 {
	step := bfsStep(int(src[0]), int(src[1]), int(dst[0]), int(dst[1]), danger)
	return [2]float64{float64(step[0]), float64(step[1])}
}

// perpDir returns a perpendicular direction (randomly CW or CCW).
func perpDir(d [2]float64) [2]float64 {
	if rand.Float64() < 0.5 {
		return [2]float64{-d[1], d[0]}
	}
	return [2]float64{d[1], -d[0]}
}

// stablePerpDir keeps move-only strafes on one bot-specific side. Re-rolling
// the side every tick can make a bot command exact opposite moves at a wall;
// emergency dodges intentionally retain the less predictable random helper.
func stablePerpDir(d [2]float64, botID string) [2]float64 {
	if guardPatrolStart(botID)%2 == 0 {
		return [2]float64{-d[1], d[0]}
	}
	return [2]float64{d[1], -d[0]}
}
