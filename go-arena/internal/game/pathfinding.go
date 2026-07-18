package game

import (
	"math"

	"arena-server/internal/config"
)

// NavGrid is a grid overlay of the arena used for A* pathfinding.  Cells that
// overlap obstacles (plus a small padding) are marked as blocked.
type NavGrid struct {
	CellSize float64
	Width    int // number of columns
	Height   int // number of rows
	Blocked  [][]bool

	// scratch holds reusable A* buffers. FindPath is only ever called from
	// the single tick goroutine (processMoveTo), and chasing fallback bots
	// re-path nearly every tick, so per-call map allocations and per-node
	// interface boxing were the dominant allocator in crowded rounds.
	// Lazily sized to Width*Height; NavGrids are rebuilt per round so the
	// dimensions never change within one grid's lifetime.
	scratch *pathScratch
}

// pathScratch carries flat generation-stamped arrays (indexed cx*Height+cy)
// replacing the per-FindPath gScore/cameFrom maps, plus a reusable open heap
// and nearest-unblocked BFS queue. A cell's gScore/cameFrom entries are valid
// only when visitGen[idx] equals the current generation, so resets are O(1).
type pathScratch struct {
	gen      uint32
	visitGen []uint32
	gScore   []float64
	cameFrom []int32 // packed predecessor index, -1 for the start cell
	open     pqHeap
	nuQueue  [][2]int // nearestUnblocked BFS queue
}

func (g *NavGrid) ensureScratch() *pathScratch {
	n := g.Width * g.Height
	if g.scratch == nil || len(g.scratch.visitGen) != n {
		g.scratch = &pathScratch{
			visitGen: make([]uint32, n),
			gScore:   make([]float64, n),
			cameFrom: make([]int32, n),
		}
	}
	return g.scratch
}

// nextGen starts a new visitation epoch. On uint32 wraparound the stamp array
// is cleared so stale stamps can never alias the new generation.
func (s *pathScratch) nextGen() uint32 {
	s.gen++
	if s.gen == 0 {
		clear(s.visitGen)
		s.gen = 1
	}
	return s.gen
}

// botPadding is added to every obstacle when building the blocked grid so that
// paths keep a small clearance from obstacle edges.
const botPadding = 2.0

// sqrt2 is the diagonal movement cost.
const sqrt2 = 1.4142135623730951

// directions lists the 8-directional neighbour offsets together with their
// movement cost.  The first four are cardinal (cost 1), the last four diagonal
// (cost sqrt2).
var directions = [8]struct {
	dx, dy int
	cost   float64
}{
	{1, 0, 1.0},
	{-1, 0, 1.0},
	{0, 1, 1.0},
	{0, -1, 1.0},
	{1, 1, sqrt2},
	{1, -1, sqrt2},
	{-1, 1, sqrt2},
	{-1, -1, sqrt2},
}

// NewNavGrid builds a navigation grid from the arena dimensions and obstacle
// list.  CellSize is read from config.C.PathfindingCellSize.
func NewNavGrid(arenaW, arenaH float64, obstacles []Obstacle, botRadius float64) *NavGrid {
	cellSize := config.C.PathfindingCellSize
	cols := int(math.Ceil(arenaW / cellSize))
	rows := int(math.Ceil(arenaH / cellSize))

	blocked := make([][]bool, cols)
	for cx := range blocked {
		blocked[cx] = make([]bool, rows)
	}

	pad := botRadius
	for _, obs := range obstacles {
		// Expand the obstacle AABB by padding.
		ox := obs.X - pad
		oy := obs.Y - pad
		ow := obs.Width + 2*pad
		oh := obs.Height + 2*pad

		minCX := int(ox / cellSize)
		minCY := int(oy / cellSize)
		maxCX := int((ox + ow) / cellSize)
		maxCY := int((oy + oh) / cellSize)

		if minCX < 0 {
			minCX = 0
		}
		if minCY < 0 {
			minCY = 0
		}
		if maxCX >= cols {
			maxCX = cols - 1
		}
		if maxCY >= rows {
			maxCY = rows - 1
		}

		for cx := minCX; cx <= maxCX; cx++ {
			for cy := minCY; cy <= maxCY; cy++ {
				blocked[cx][cy] = true
			}
		}
	}

	return &NavGrid{
		CellSize: cellSize,
		Width:    cols,
		Height:   rows,
		Blocked:  blocked,
	}
}

// WorldToCell converts a world position to grid cell indices, clamped to the
// grid bounds.
func (g *NavGrid) WorldToCell(pos Vec2) [2]int {
	cx := int(math.Floor(pos.X() / g.CellSize))
	cy := int(math.Floor(pos.Y() / g.CellSize))
	if cx < 0 {
		cx = 0
	}
	if cy < 0 {
		cy = 0
	}
	if cx >= g.Width {
		cx = g.Width - 1
	}
	if cy >= g.Height {
		cy = g.Height - 1
	}
	return [2]int{cx, cy}
}

// CellToWorld returns the world-space centre of the given grid cell.
func (g *NavGrid) CellToWorld(cell [2]int) Vec2 {
	return NewVec2(
		(float64(cell[0])+0.5)*g.CellSize,
		(float64(cell[1])+0.5)*g.CellSize,
	)
}

// IsBlocked returns true if the cell at (cx, cy) is out of bounds or blocked
// by an obstacle.
func (g *NavGrid) IsBlocked(cx, cy int) bool {
	if cx < 0 || cy < 0 || cx >= g.Width || cy >= g.Height {
		return true
	}
	return g.Blocked[cx][cy]
}

// ---------------------------------------------------------------------------
// A* search
// ---------------------------------------------------------------------------

// FindPath computes an A* path from start to goal on the given NavGrid.
//
// The returned slice contains world-space waypoints.  The start position is
// NOT included; the goal IS included as the last waypoint.  An empty slice is
// returned when no path can be found.
func FindPath(start, goal Vec2, grid *NavGrid) []Vec2 {
	startCell := grid.WorldToCell(start)
	goalCell := grid.WorldToCell(goal)

	if startCell == goalCell {
		return []Vec2{goal}
	}

	// If the goal cell is blocked, find the nearest unblocked cell.
	if grid.IsBlocked(goalCell[0], goalCell[1]) {
		alt, ok := nearestUnblocked(goalCell, grid)
		if !ok {
			return nil
		}
		goalCell = alt
	}

	// If the start cell is blocked, find the nearest unblocked cell.
	if grid.IsBlocked(startCell[0], startCell[1]) {
		alt, ok := nearestUnblocked(startCell, grid)
		if !ok {
			return nil
		}
		startCell = alt
	}

	// A* open set (typed min-heap by f-score) over flat generation-stamped
	// scratch arrays: no per-node interface boxing, no per-call maps.
	s := grid.ensureScratch()
	gen := s.nextGen()
	open := s.open[:0]

	idx := func(c [2]int) int32 { return int32(c[0]*grid.Height + c[1]) }

	counter := 0
	startIdx := idx(startCell)
	s.visitGen[startIdx] = gen
	s.gScore[startIdx] = 0
	s.cameFrom[startIdx] = -1

	h := octileHeuristic(startCell, goalCell)
	open = open.pushItem(pqItem{f: h, seq: counter, cell: startCell})

	defer func() { s.open = open[:0] }()

	for len(open) > 0 {
		var cur pqItem
		open, cur = open.popItem()
		current := cur.cell

		if current == goalCell {
			// Reconstruct and convert to world coords.
			pathCells := reconstructPath(s, grid, current)
			waypoints := make([]Vec2, 0, len(pathCells))
			for i, c := range pathCells {
				if i == len(pathCells)-1 {
					// Use exact goal position for the last waypoint.
					waypoints = append(waypoints, goal)
				} else {
					waypoints = append(waypoints, grid.CellToWorld(c))
				}
			}
			waypoints = smoothPath(waypoints, grid)
			return waypoints
		}

		currentG := s.gScore[idx(current)]

		for _, d := range directions {
			nx := current[0] + d.dx
			ny := current[1] + d.dy

			if nx < 0 || nx >= grid.Width || ny < 0 || ny >= grid.Height {
				continue
			}
			if grid.Blocked[nx][ny] {
				continue
			}

			// Diagonal moves require both adjacent cardinal cells to be clear
			// (no corner cutting).
			if d.dx != 0 && d.dy != 0 {
				if grid.IsBlocked(current[0]+d.dx, current[1]) ||
					grid.IsBlocked(current[0], current[1]+d.dy) {
					continue
				}
			}

			neighbor := [2]int{nx, ny}
			nIdx := idx(neighbor)
			tentativeG := currentG + d.cost

			if s.visitGen[nIdx] == gen && tentativeG >= s.gScore[nIdx] {
				continue
			}

			s.visitGen[nIdx] = gen
			s.gScore[nIdx] = tentativeG
			s.cameFrom[nIdx] = idx(current)
			counter++
			open = open.pushItem(pqItem{
				f:    tentativeG + octileHeuristic(neighbor, goalCell),
				seq:  counter,
				cell: neighbor,
			})
		}
	}

	// No path found.
	return nil
}

// octileHeuristic is the admissible heuristic for 8-directional grids.
func octileHeuristic(a, b [2]int) float64 {
	dx := math.Abs(float64(a[0] - b[0]))
	dy := math.Abs(float64(a[1] - b[1]))
	return math.Max(dx, dy) + (sqrt2-1)*math.Min(dx, dy)
}

// reconstructPath walks the scratch cameFrom chain backwards from current to
// start and returns the cell sequence excluding the start cell.
func reconstructPath(s *pathScratch, grid *NavGrid, current [2]int) [][2]int {
	path := [][2]int{current}
	cur := int32(current[0]*grid.Height + current[1])
	for {
		prev := s.cameFrom[cur]
		if prev < 0 {
			break
		}
		cell := [2]int{int(prev) / grid.Height, int(prev) % grid.Height}
		path = append(path, cell)
		cur = prev
	}
	// Reverse.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	// Remove start cell.
	if len(path) > 1 {
		path = path[1:]
	}
	return path
}

// nearestUnblocked performs a BFS from cell to find the closest unblocked
// cell. Reuses the grid's scratch visit stamps and queue: it runs whenever
// the chased target stands in a padded-blocked cell, i.e. potentially every
// re-path tick.
func nearestUnblocked(cell [2]int, grid *NavGrid) ([2]int, bool) {
	s := grid.ensureScratch()
	gen := s.nextGen()

	s.visitGen[cell[0]*grid.Height+cell[1]] = gen
	queue := append(s.nuQueue[:0], cell)
	head := 0
	defer func() { s.nuQueue = queue[:0] }()
	maxSearch := 200

	for head < len(queue) && maxSearch > 0 {
		maxSearch--
		cur := queue[head]
		head++

		for _, d := range directions {
			nx := cur[0] + d.dx
			ny := cur[1] + d.dy
			if nx < 0 || nx >= grid.Width || ny < 0 || ny >= grid.Height {
				continue
			}
			nIdx := nx*grid.Height + ny
			if s.visitGen[nIdx] == gen {
				continue
			}
			s.visitGen[nIdx] = gen
			if !grid.Blocked[nx][ny] {
				return [2]int{nx, ny}, true
			}
			queue = append(queue, [2]int{nx, ny})
		}
	}

	return [2]int{}, false
}

// smoothPath removes redundant intermediate waypoints.  It greedily skips
// waypoints whenever a direct line-of-sight exists between the anchor point
// and a further waypoint.
func smoothPath(waypoints []Vec2, grid *NavGrid) []Vec2 {
	if len(waypoints) <= 2 {
		return waypoints
	}

	smoothed := []Vec2{waypoints[0]}
	i := 0
	for i < len(waypoints)-1 {
		farthest := i + 1
		for j := len(waypoints) - 1; j > i+1; j-- {
			if lineClear(smoothed[len(smoothed)-1], waypoints[j], grid) {
				farthest = j
				break
			}
		}
		smoothed = append(smoothed, waypoints[farthest])
		i = farthest
	}
	return smoothed
}

// lineClear returns true if a straight line between two world positions does
// not cross any blocked grid cell.  Uses a DDA-style ray march.
func lineClear(a, b Vec2, grid *NavGrid) bool {
	cs := grid.CellSize
	ax := a.X() / cs
	ay := a.Y() / cs
	bx := b.X() / cs
	by := b.Y() / cs

	dx := bx - ax
	dy := by - ay
	steps := int(math.Max(math.Abs(dx), math.Abs(dy))*2) + 1
	if steps == 0 {
		return true
	}

	sx := dx / float64(steps)
	sy := dy / float64(steps)

	for s := 0; s <= steps; s++ {
		px := ax + sx*float64(s)
		py := ay + sy*float64(s)

		cx := int(px)
		cy := int(py)
		if cx < 0 {
			cx = 0
		}
		if cy < 0 {
			cy = 0
		}
		if cx >= grid.Width {
			cx = grid.Width - 1
		}
		if cy >= grid.Height {
			cy = grid.Height - 1
		}

		if grid.Blocked[cx][cy] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Priority queue for A*. A typed min-heap on []pqItem instead of
// container/heap: the heap.Interface API takes and returns `any`, which
// boxed a 32-byte pqItem into an interface — one heap allocation per push
// AND per pop, i.e. thousands per FindPath call on long paths.
// ---------------------------------------------------------------------------

type pqItem struct {
	f    float64 // f-score (g + h)
	seq  int     // tie-breaker (insertion order)
	cell [2]int
}

type pqHeap []pqItem

func (h pqHeap) less(i, j int) bool {
	return h[i].f < h[j].f || (h[i].f == h[j].f && h[i].seq < h[j].seq)
}

// pushItem appends it and sifts up, returning the (possibly regrown) heap.
func (h pqHeap) pushItem(it pqItem) pqHeap {
	h = append(h, it)
	i := len(h) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if !h.less(i, parent) {
			break
		}
		h[i], h[parent] = h[parent], h[i]
		i = parent
	}
	return h
}

// popItem removes and returns the minimum item, sifting the moved tail down.
func (h pqHeap) popItem() (pqHeap, pqItem) {
	root := h[0]
	n := len(h) - 1
	h[0] = h[n]
	h = h[:n]
	i := 0
	for {
		l, r := 2*i+1, 2*i+2
		smallest := i
		if l < n && h.less(l, smallest) {
			smallest = l
		}
		if r < n && h.less(r, smallest) {
			smallest = r
		}
		if smallest == i {
			break
		}
		h[i], h[smallest] = h[smallest], h[i]
		i = smallest
	}
	return h, root
}
