package game

import "math"

// ActiveTerrain is the terrain grid for the current round.
// Set at the start of each round by the engine. Used by combat and movement
// code for grid-based range and position checks.
var ActiveTerrain *TerrainGrid

// TerrainGrid holds the static terrain for a round, sent to bots via map_init.
type TerrainGrid struct {
	Width    int
	Height   int
	CellSize float64
	Cells    [][]byte // [col][row]: '.' ground, '#' wall
}

// NewTerrainGrid builds a terrain grid from the arena dimensions and obstacles.
// Cells that overlap any obstacle (expanded by bot radius padding) are marked
// as walls ('#'). Everything else is ground ('.').
func NewTerrainGrid(arenaW, arenaH float64, obstacles []Obstacle, cellSize, botRadius float64) *TerrainGrid {
	cols := int(math.Ceil(arenaW / cellSize))
	rows := int(math.Ceil(arenaH / cellSize))

	cells := make([][]byte, cols)
	for x := range cells {
		cells[x] = make([]byte, rows)
		for y := range cells[x] {
			cells[x][y] = '.'
		}
	}

	// Mark obstacle cells as walls (with bot-radius padding).
	pad := botRadius
	for _, obs := range obstacles {
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
				cells[cx][cy] = '#'
			}
		}
	}

	return &TerrainGrid{
		Width:    cols,
		Height:   rows,
		CellSize: cellSize,
		Cells:    cells,
	}
}

// FullyConnected reports whether every open cell is reachable from every
// other open cell (4-connectivity, matching cardinal movement). The shape
// mask is connectivity-checked on its own during generation, but obstacles
// are stamped afterwards and can seal off a pocket — bots and pickups placed
// inside would be unreachable.
func (g *TerrainGrid) FullyConnected() bool {
	totalOpen := 0
	var start [2]int
	found := false
	for x := 0; x < g.Width; x++ {
		for y := 0; y < g.Height; y++ {
			if g.Cells[x][y] != '#' {
				totalOpen++
				if !found {
					start = [2]int{x, y}
					found = true
				}
			}
		}
	}
	if totalOpen == 0 {
		return false
	}

	visited := make([][]bool, g.Width)
	for x := range visited {
		visited[x] = make([]bool, g.Height)
	}
	stack := [][2]int{start}
	visited[start[0]][start[1]] = true
	reached := 0
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		reached++
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			nx, ny := c[0]+d[0], c[1]+d[1]
			if nx < 0 || ny < 0 || nx >= g.Width || ny >= g.Height {
				continue
			}
			if visited[nx][ny] || g.Cells[nx][ny] == '#' {
				continue
			}
			visited[nx][ny] = true
			stack = append(stack, [2]int{nx, ny})
		}
	}
	return reached == totalOpen
}

// IsBlocked returns true if the cell is out of bounds or a wall.
func (g *TerrainGrid) IsBlocked(x, y int) bool {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return true
	}
	return g.Cells[x][y] == '#'
}

// NearestOpenCell returns the closest unblocked cell to start, scanning
// complete rings of increasing Chebyshev radius (so no direction is ever
// skipped, unlike a fixed-ray probe). maxRadius <= 0 searches the whole grid.
// Returns start unchanged with ok=false when nothing open is found.
func (g *TerrainGrid) NearestOpenCell(start [2]int, maxRadius int) ([2]int, bool) {
	if !g.IsBlocked(start[0], start[1]) {
		return start, true
	}
	limit := maxRadius
	if limit <= 0 {
		limit = g.Width
		if g.Height > limit {
			limit = g.Height
		}
	}
	for radius := 1; radius <= limit; radius++ {
		for dx := -radius; dx <= radius; dx++ {
			for dy := -radius; dy <= radius; dy++ {
				if dx != -radius && dx != radius && dy != -radius && dy != radius {
					continue // ring perimeter only
				}
				nc := [2]int{start[0] + dx, start[1] + dy}
				if !g.IsBlocked(nc[0], nc[1]) {
					return nc, true
				}
			}
		}
	}
	return start, false
}

// WorldToGrid converts a world-space Vec2 to grid cell coordinates.
func (g *TerrainGrid) WorldToGrid(pos Vec2) [2]int {
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

// GridToWorld returns the world-space centre of a grid cell.
func (g *TerrainGrid) GridToWorld(cell [2]int) Vec2 {
	return NewVec2(
		(float64(cell[0])+0.5)*g.CellSize,
		(float64(cell[1])+0.5)*g.CellSize,
	)
}

// ToJSON returns the terrain as a row-major 2D array of single-character
// strings, suitable for JSON serialisation: terrain[row][col].
func (g *TerrainGrid) ToJSON() [][]string {
	result := make([][]string, g.Height)
	for y := 0; y < g.Height; y++ {
		row := make([]string, g.Width)
		for x := 0; x < g.Width; x++ {
			row[x] = string(g.Cells[x][y])
		}
		result[y] = row
	}
	return result
}

// ToCompactJSON returns the terrain as an array of row strings, e.g.
// ["..##..", "......"] — much smaller JSON than the 2D char array format.
func (g *TerrainGrid) ToCompactJSON() []string {
	result := make([]string, g.Height)
	for y := 0; y < g.Height; y++ {
		row := make([]byte, g.Width)
		for x := 0; x < g.Width; x++ {
			row[x] = g.Cells[x][y]
		}
		result[y] = string(row)
	}
	return result
}

// ToCompactJSONWithFeatures returns the terrain with active round features
// stamped directly onto the grid: 'T' teleport pads, 'H' hazard zones, and
// 'C' capture pads.
// Overlays are drawn on top of ground cells only (walls are preserved).
func (g *TerrainGrid) ToCompactJSONWithFeatures(pads []TeleportPad, zones []HazardZone, capturePads []CapturePad) []string {
	// Start with a mutable copy
	grid := make([][]byte, g.Height)
	for y := 0; y < g.Height; y++ {
		row := make([]byte, g.Width)
		for x := 0; x < g.Width; x++ {
			row[x] = g.Cells[x][y]
		}
		grid[y] = row
	}

	// Stamp teleport pads (single cell at pad position)
	for _, pad := range pads {
		cell := g.WorldToGrid(pad.Position)
		x, y := cell[0], cell[1]
		if x >= 0 && x < g.Width && y >= 0 && y < g.Height && grid[y][x] == '.' {
			grid[y][x] = 'T'
		}
	}

	// Stamp hazard zones (rectangular area)
	for _, zone := range zones {
		cell := g.WorldToGrid(zone.Position)
		cx, cy := cell[0], cell[1]
		hw, hh := zone.Width/2, zone.Height/2
		for dy := -hh; dy <= hh; dy++ {
			for dx := -hw; dx <= hw; dx++ {
				x, y := cx+dx, cy+dy
				if x >= 0 && x < g.Width && y >= 0 && y < g.Height && grid[y][x] == '.' {
					grid[y][x] = 'H'
				}
			}
		}
	}

	// Stamp capture pads (single cell at pad position).
	for _, pad := range capturePads {
		cell := g.WorldToGrid(pad.Position)
		x, y := cell[0], cell[1]
		if x >= 0 && x < g.Width && y >= 0 && y < g.Height && grid[y][x] == '.' {
			grid[y][x] = 'C'
		}
	}

	result := make([]string, g.Height)
	for y := 0; y < g.Height; y++ {
		result[y] = string(grid[y])
	}
	return result
}

// GridDistance returns the Chebyshev distance (king distance) between two
// grid cells. This is the natural distance metric for 8-directional movement.
func GridDistance(a, b [2]int) int {
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

// IsMoveBlocked checks whether moving from (cx,cy) by (dx,dy) is blocked.
// For diagonal moves (both dx and dy nonzero) it also checks the two
// adjacent cardinal cells to prevent corner-cutting through walls.
func (g *TerrainGrid) IsMoveBlocked(cx, cy, dx, dy int) bool {
	nx, ny := cx+dx, cy+dy
	if g.IsBlocked(nx, ny) {
		return true
	}
	// Diagonal: both cardinal neighbours must be passable.
	if dx != 0 && dy != 0 {
		if g.IsBlocked(cx+dx, cy) || g.IsBlocked(cx, cy+dy) {
			return true
		}
	}
	return false
}

// GridLineBlocked returns true if any blocked cell lies on the grid line
// between world-space positions a and b. This prevents attacks and
// projectiles from passing through wall cells.
func (g *TerrainGrid) GridLineBlocked(a, b Vec2) bool {
	return g.SegmentBlocked(a, b)
}

// SegmentBlocked walks every grid cell the world-space segment a→b actually
// crosses (Amanatides–Woo voxel traversal) and reports whether any of them is
// blocked. The starting cell is excluded; the destination cell is included.
// Unlike an index-interpolation march, this cannot skip cells on steep
// diagonals, so fast projectiles can't tunnel through wall corners.
func (g *TerrainGrid) SegmentBlocked(a, b Vec2) bool {
	c1 := g.WorldToGrid(a)
	c2 := g.WorldToGrid(b)
	if c1 == c2 {
		return false // same cell — no wall between them
	}

	x, y := c1[0], c1[1]
	dx := b.X() - a.X()
	dy := b.Y() - a.Y()

	stepX, stepY := 0, 0
	tMaxX, tMaxY := math.Inf(1), math.Inf(1)
	tDeltaX, tDeltaY := math.Inf(1), math.Inf(1)

	if dx > 0 {
		stepX = 1
		tMaxX = ((float64(x)+1)*g.CellSize - a.X()) / dx
		tDeltaX = g.CellSize / dx
	} else if dx < 0 {
		stepX = -1
		tMaxX = (float64(x)*g.CellSize - a.X()) / dx
		tDeltaX = -g.CellSize / dx
	}
	if dy > 0 {
		stepY = 1
		tMaxY = ((float64(y)+1)*g.CellSize - a.Y()) / dy
		tDeltaY = g.CellSize / dy
	} else if dy < 0 {
		stepY = -1
		tMaxY = (float64(y)*g.CellSize - a.Y()) / dy
		tDeltaY = -g.CellSize / dy
	}

	// Bounded walk: a segment crossing N columns and M rows visits at most
	// N+M cells beyond the start. (WorldToGrid clamps out-of-grid points, so
	// the destination cell may be unreachable; the bound terminates us then.)
	adx := c2[0] - c1[0]
	if adx < 0 {
		adx = -adx
	}
	ady := c2[1] - c1[1]
	if ady < 0 {
		ady = -ady
	}
	for i := 0; i < adx+ady+2; i++ {
		if tMaxX < tMaxY {
			x += stepX
			tMaxX += tDeltaX
		} else {
			y += stepY
			tMaxY += tDeltaY
		}
		if g.IsBlocked(x, y) {
			return true
		}
		if x == c2[0] && y == c2[1] {
			return false
		}
	}
	return false
}

// SnapDirection clamps a float direction component to -1, 0, or 1.
func SnapDirection(v float64) int {
	if v > 0.3 {
		return 1
	}
	if v < -0.3 {
		return -1
	}
	return 0
}
