package game

import (
	"container/heap"
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

	// A* open set (min-heap by f-score).
	open := &pqHeap{}
	heap.Init(open)

	counter := 0
	gScore := make(map[[2]int]float64)
	gScore[startCell] = 0
	cameFrom := make(map[[2]int][2]int)

	h := octileHeuristic(startCell, goalCell)
	heap.Push(open, pqItem{f: h, seq: counter, cell: startCell})

	for open.Len() > 0 {
		cur := heap.Pop(open).(pqItem)
		current := cur.cell

		if current == goalCell {
			// Reconstruct and convert to world coords.
			pathCells := reconstructPath(cameFrom, current)
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

		currentG := gScore[current]

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
			tentativeG := currentG + d.cost

			if prev, ok := gScore[neighbor]; ok && tentativeG >= prev {
				continue
			}

			gScore[neighbor] = tentativeG
			cameFrom[neighbor] = current
			counter++
			heap.Push(open, pqItem{
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

// reconstructPath walks the cameFrom map backwards from current to start and
// returns the cell sequence excluding the start cell.
func reconstructPath(cameFrom map[[2]int][2]int, current [2]int) [][2]int {
	path := [][2]int{current}
	for {
		prev, ok := cameFrom[current]
		if !ok {
			break
		}
		path = append(path, prev)
		current = prev
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

// nearestUnblocked performs a BFS from cell to find the closest unblocked cell.
func nearestUnblocked(cell [2]int, grid *NavGrid) ([2]int, bool) {
	type qEntry struct {
		cx, cy int
	}

	visited := make(map[[2]int]bool)
	visited[cell] = true
	queue := []qEntry{{cell[0], cell[1]}}
	maxSearch := 200

	for len(queue) > 0 && maxSearch > 0 {
		maxSearch--
		cur := queue[0]
		queue = queue[1:]

		for _, d := range directions {
			nx := cur.cx + d.dx
			ny := cur.cy + d.dy
			if nx < 0 || nx >= grid.Width || ny < 0 || ny >= grid.Height {
				continue
			}
			key := [2]int{nx, ny}
			if visited[key] {
				continue
			}
			visited[key] = true
			if !grid.Blocked[nx][ny] {
				return key, true
			}
			queue = append(queue, qEntry{nx, ny})
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
// Priority queue for A* (implements container/heap.Interface)
// ---------------------------------------------------------------------------

type pqItem struct {
	f    float64 // f-score (g + h)
	seq  int     // tie-breaker (insertion order)
	cell [2]int
}

type pqHeap []pqItem

func (h pqHeap) Len() int            { return len(h) }
func (h pqHeap) Less(i, j int) bool  { return h[i].f < h[j].f || (h[i].f == h[j].f && h[i].seq < h[j].seq) }
func (h pqHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *pqHeap) Push(x any) { *h = append(*h, x.(pqItem)) }
func (h *pqHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
