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

// IsBlocked returns true if the cell is out of bounds or a wall.
func (g *TerrainGrid) IsBlocked(x, y int) bool {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return true
	}
	return g.Cells[x][y] == '#'
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
// between world-space positions a and b (DDA ray-march). This prevents
// attacks and projectiles from passing through wall cells.
func (g *TerrainGrid) GridLineBlocked(a, b Vec2) bool {
	c1 := g.WorldToGrid(a)
	c2 := g.WorldToGrid(b)
	if c1 == c2 {
		return false // same cell — no wall between them
	}

	dx := c2[0] - c1[0]
	dy := c2[1] - c1[1]

	adx := dx
	if adx < 0 {
		adx = -adx
	}
	ady := dy
	if ady < 0 {
		ady = -ady
	}
	steps := adx
	if ady > steps {
		steps = ady
	}
	if steps == 0 {
		return false
	}

	for i := 1; i <= steps; i++ {
		cx := c1[0] + dx*i/steps
		cy := c1[1] + dy*i/steps
		if g.IsBlocked(cx, cy) {
			return true
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
