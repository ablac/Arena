package game

import (
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"

	"arena-server/internal/config"
)

// MapShape identifies the playable outline of the arena. The arena is still
// a Width×Height rectangle in world coordinates; non-square shapes are carved
// into the terrain grid as blocked cells, which movement, pathfinding, LOS,
// and wall enforcement already respect.
type MapShape string

const (
	ShapeSquare  MapShape = "square"
	ShapeCircle  MapShape = "circle"
	ShapeHexagon MapShape = "hexagon"
	ShapeDiamond MapShape = "diamond"
	ShapeCross   MapShape = "cross"
	ShapeCaves   MapShape = "caves"
)

// ActiveMapShape is the shape of the currently-active round's terrain.
// Set by the engine whenever it installs a terrain grid.
var ActiveMapShape = ShapeSquare

// randomShapePool are the shapes eligible when MapShape is "random".
var randomShapePool = []MapShape{ShapeSquare, ShapeCircle, ShapeHexagon, ShapeDiamond, ShapeCross, ShapeCaves}

// CustomMapTemplate describes a generated map shape saved from the Admin Panel.
// It is intentionally seed/base-shape based rather than arbitrary geometry so
// the engine can keep using the same collision, terrain, and pathfinding code.
type CustomMapTemplate struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	BaseShape   string `json:"base_shape"`
	Seed        int64  `json:"seed"`
	Enabled     bool   `json:"enabled"`
}

var customMaps = struct {
	mu    sync.RWMutex
	items map[string]CustomMapTemplate
}{items: make(map[string]CustomMapTemplate)}

func BuiltInMapShapeNames() []string {
	return []string{
		string(ShapeSquare),
		string(ShapeCircle),
		string(ShapeHexagon),
		string(ShapeDiamond),
		string(ShapeCross),
		string(ShapeCaves),
	}
}

func IsBuiltInMapShape(name string) bool {
	switch MapShape(name) {
	case ShapeSquare, ShapeCircle, ShapeHexagon, ShapeDiamond, ShapeCross, ShapeCaves:
		return true
	default:
		return false
	}
}

func CustomMapShapeName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, " ", "-")
	return "custom:" + name
}

func RegisterCustomMap(t CustomMapTemplate) CustomMapTemplate {
	t.Name = strings.TrimPrefix(CustomMapShapeName(t.Name), "custom:")
	if t.DisplayName == "" {
		t.DisplayName = t.Name
	}
	if !IsBuiltInMapShape(t.BaseShape) {
		t.BaseShape = string(ShapeCaves)
	}
	if t.Seed == 0 {
		t.Seed = 1
	}
	customMaps.mu.Lock()
	customMaps.items[t.Name] = t
	customMaps.mu.Unlock()
	return t
}

func RemoveCustomMap(name string) {
	name = strings.TrimPrefix(CustomMapShapeName(name), "custom:")
	customMaps.mu.Lock()
	delete(customMaps.items, name)
	customMaps.mu.Unlock()
}

func ListCustomMaps() []CustomMapTemplate {
	customMaps.mu.RLock()
	defer customMaps.mu.RUnlock()
	out := make([]CustomMapTemplate, 0, len(customMaps.items))
	for _, item := range customMaps.items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func getCustomMap(shape MapShape) (CustomMapTemplate, bool) {
	name := strings.TrimPrefix(string(shape), "custom:")
	customMaps.mu.RLock()
	t, ok := customMaps.items[name]
	customMaps.mu.RUnlock()
	return t, ok && t.Enabled
}

func IsKnownMapShape(name string) bool {
	if name == "random" || IsBuiltInMapShape(name) {
		return true
	}
	_, ok := getCustomMap(MapShape(name))
	return ok
}

func NormalizeMapShapePool(names []string) []MapShape {
	seen := make(map[string]bool)
	pool := make([]MapShape, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(strings.ToLower(raw))
		if name == "" || name == "random" || seen[name] {
			continue
		}
		if IsBuiltInMapShape(name) {
			seen[name] = true
			pool = append(pool, MapShape(name))
			continue
		}
		if strings.HasPrefix(name, "custom:") {
			if _, ok := getCustomMap(MapShape(name)); ok {
				seen[name] = true
				pool = append(pool, MapShape(name))
			}
		}
	}
	if len(pool) == 0 {
		return []MapShape{ShapeSquare}
	}
	return pool
}

func RandomShapePoolNames() []string {
	pool := NormalizeMapShapePool(strings.Split(config.C.MapShapePool, ","))
	out := make([]string, 0, len(pool))
	for _, shape := range pool {
		out = append(out, string(shape))
	}
	return out
}

func SetRandomShapePool(names []string) []string {
	pool := NormalizeMapShapePool(names)
	out := make([]string, 0, len(pool))
	for _, shape := range pool {
		out = append(out, string(shape))
	}
	config.C.MapShapePool = strings.Join(out, ",")
	return out
}

// PickMapShape resolves the configured map shape, rolling a random one per
// call when set to "random". Unknown values fall back to square.
func PickMapShape() MapShape {
	switch v := config.C.MapShape; v {
	case "random":
		pool := NormalizeMapShapePool(strings.Split(config.C.MapShapePool, ","))
		return pool[rand.Intn(len(pool))]
	case string(ShapeCircle), string(ShapeHexagon), string(ShapeDiamond), string(ShapeCross), string(ShapeCaves):
		return MapShape(v)
	default:
		if _, ok := getCustomMap(MapShape(v)); ok {
			return MapShape(v)
		}
		return ShapeSquare
	}
}

// GenerateShapeMask returns a cols×rows grid where true means playable.
// Returns nil for square (nothing to carve).
func GenerateShapeMask(shape MapShape, cols, rows int) [][]bool {
	return generateShapeMask(shape, cols, rows, nil)
}

func GenerateShapeMaskWithSeed(shape MapShape, cols, rows int, seed int64) [][]bool {
	return generateShapeMask(shape, cols, rows, rand.New(rand.NewSource(seed)))
}

func generateShapeMask(shape MapShape, cols, rows int, rng *rand.Rand) [][]bool {
	if custom, ok := getCustomMap(shape); ok {
		return GenerateShapeMaskWithSeed(MapShape(custom.BaseShape), cols, rows, custom.Seed)
	}
	if shape == ShapeSquare || cols <= 0 || rows <= 0 {
		return nil
	}

	mask := make([][]bool, cols)
	for x := range mask {
		mask[x] = make([]bool, rows)
	}

	cx := float64(cols-1) / 2
	cy := float64(rows-1) / 2

	switch shape {
	case ShapeCircle:
		r := math.Min(cx, cy)
		for x := 0; x < cols; x++ {
			for y := 0; y < rows; y++ {
				dx, dy := float64(x)-cx, float64(y)-cy
				mask[x][y] = dx*dx+dy*dy <= r*r
			}
		}

	case ShapeDiamond:
		r := math.Min(cx, cy)
		for x := 0; x < cols; x++ {
			for y := 0; y < rows; y++ {
				mask[x][y] = math.Abs(float64(x)-cx)+math.Abs(float64(y)-cy) <= r
			}
		}

	case ShapeHexagon:
		// Flat-top hexagon inscribed in the grid.
		r := math.Min(cx, cy)
		for x := 0; x < cols; x++ {
			for y := 0; y < rows; y++ {
				dx := math.Abs(float64(x)-cx) / r
				dy := math.Abs(float64(y)-cy) / r
				mask[x][y] = dy <= 0.866 && 0.866*dx+0.5*dy <= 0.866
			}
		}

	case ShapeCross:
		// Union of a horizontal and a vertical bar through the centre.
		armW := float64(cols) * 0.22
		armH := float64(rows) * 0.22
		for x := 0; x < cols; x++ {
			for y := 0; y < rows; y++ {
				inV := math.Abs(float64(x)-cx) <= armW
				inH := math.Abs(float64(y)-cy) <= armH
				mask[x][y] = inV || inH
			}
		}

	case ShapeCaves:
		generateCaveMask(mask, cols, rows, rng)
	}

	ensureConnected(mask, cols, rows)

	// Degenerate output (tiny playable area) falls back to a circle.
	if playableFraction(mask, cols, rows) < 0.30 && shape == ShapeCaves {
		return generateShapeMask(ShapeCircle, cols, rows, rng)
	}
	return mask
}

// generateRoundTerrain rolls a map shape and builds a round's obstacles and
// grids with the shape carved into both, sized for the given bot count.
// Returns the boundary rectangles of the carved area for client rendering.
func generateRoundTerrain(botCount int) (obstacles []Obstacle, nav *NavGrid, terrain *TerrainGrid, shape MapShape, maskRects []Obstacle) {
	c := &config.C
	scale := ApplyDynamicArenaSize(botCount)
	shape = PickMapShape()

	// Preserve obstacle density on scaled maps: counts grow with area.
	area := scale * scale
	obsMin := int(float64(c.ObstacleCountMin) * area)
	obsMax := int(float64(c.ObstacleCountMax) * area)
	obstacles = GenerateObstacles(c.ArenaWidth, c.ArenaHeight, obsMin, obsMax)
	nav = NewNavGrid(c.ArenaWidth, c.ArenaHeight, obstacles, c.BotRadius)
	terrain = NewTerrainGrid(c.ArenaWidth, c.ArenaHeight, obstacles, c.PathfindingCellSize, c.BotRadius)

	if mask := GenerateShapeMask(shape, terrain.Width, terrain.Height); mask != nil {
		terrain.ApplyMask(mask)
		nav.ApplyMask(mask)
		maskRects = MaskToRects(mask, terrain.Width, terrain.Height, terrain.CellSize)
	}
	return obstacles, nav, terrain, shape, maskRects
}

// generateCaveMask fills the mask with a cellular-automata cave: random
// noise smoothed over several iterations, giving each round a unique
// organic arena outline.
func generateCaveMask(mask [][]bool, cols, rows int, rng *rand.Rand) {
	// Seed: ~58% open, forced walls at the border.
	for x := 0; x < cols; x++ {
		for y := 0; y < rows; y++ {
			if x == 0 || y == 0 || x == cols-1 || y == rows-1 {
				mask[x][y] = false
				continue
			}
			if rng != nil {
				mask[x][y] = rng.Float64() < 0.58
			} else {
				mask[x][y] = rand.Float64() < 0.58
			}
		}
	}

	// Keep a generous open core so the CA can't wall off the middle.
	cx, cy := cols/2, rows/2
	coreR := minInt(cols, rows) / 5
	for x := cx - coreR; x <= cx+coreR; x++ {
		for y := cy - coreR; y <= cy+coreR; y++ {
			if x > 0 && y > 0 && x < cols-1 && y < rows-1 {
				mask[x][y] = true
			}
		}
	}

	// Smooth: a cell stays open when 5+ of its 8 neighbours are open.
	next := make([][]bool, cols)
	for x := range next {
		next[x] = make([]bool, rows)
	}
	for iter := 0; iter < 4; iter++ {
		for x := 1; x < cols-1; x++ {
			for y := 1; y < rows-1; y++ {
				open := 0
				for dx := -1; dx <= 1; dx++ {
					for dy := -1; dy <= 1; dy++ {
						if dx == 0 && dy == 0 {
							continue
						}
						if mask[x+dx][y+dy] {
							open++
						}
					}
				}
				next[x][y] = open >= 5 || (mask[x][y] && open >= 4)
			}
		}
		for x := 1; x < cols-1; x++ {
			copy(mask[x][1:rows-1], next[x][1:rows-1])
		}
	}
}

// ensureConnected keeps only the largest connected playable region so bots
// can never spawn in an unreachable pocket.
func ensureConnected(mask [][]bool, cols, rows int) {
	labels := make([][]int, cols)
	for x := range labels {
		labels[x] = make([]int, rows)
	}

	sizes := []int{0} // label 0 = unlabelled
	var stack [][2]int
	for x := 0; x < cols; x++ {
		for y := 0; y < rows; y++ {
			if !mask[x][y] || labels[x][y] != 0 {
				continue
			}
			label := len(sizes)
			sizes = append(sizes, 0)
			stack = append(stack[:0], [2]int{x, y})
			labels[x][y] = label
			for len(stack) > 0 {
				c := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				sizes[label]++
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					nx, ny := c[0]+d[0], c[1]+d[1]
					if nx < 0 || ny < 0 || nx >= cols || ny >= rows {
						continue
					}
					if mask[nx][ny] && labels[nx][ny] == 0 {
						labels[nx][ny] = label
						stack = append(stack, [2]int{nx, ny})
					}
				}
			}
		}
	}

	// Find the largest region and wall off everything else.
	best := 0
	for label := 1; label < len(sizes); label++ {
		if sizes[label] > sizes[best] {
			best = label
		}
	}
	if best == 0 {
		return
	}
	for x := 0; x < cols; x++ {
		for y := 0; y < rows; y++ {
			if mask[x][y] && labels[x][y] != best {
				mask[x][y] = false
			}
		}
	}
}

func playableFraction(mask [][]bool, cols, rows int) float64 {
	open := 0
	for x := 0; x < cols; x++ {
		for y := 0; y < rows; y++ {
			if mask[x][y] {
				open++
			}
		}
	}
	return float64(open) / float64(cols*rows)
}

// MaskToRects converts the blocked (non-playable) area of a mask into a
// compact list of world-space rectangles: horizontal runs per row, merged
// vertically when consecutive rows share an identical run. These are shipped
// to spectators alongside regular obstacles so the visible walls exactly
// match server collision.
func MaskToRects(mask [][]bool, cols, rows int, cellSize float64) []Obstacle {
	if mask == nil {
		return nil
	}
	type run struct {
		x0, x1 int // inclusive cell range
	}
	var rects []Obstacle
	// open[startCol<<16|endCol] -> index into rects of a still-growing rect
	prev := make(map[[2]int]int)

	for y := 0; y < rows; y++ {
		cur := make(map[[2]int]int)
		x := 0
		for x < cols {
			if mask[x][y] {
				x++
				continue
			}
			x0 := x
			for x < cols && !mask[x][y] {
				x++
			}
			key := [2]int{x0, x - 1}
			if idx, ok := prev[key]; ok {
				// Extend the rect from the previous row downward.
				rects[idx].Height += cellSize
				cur[key] = idx
			} else {
				rects = append(rects, Obstacle{
					X:      float64(x0) * cellSize,
					Y:      float64(y) * cellSize,
					Width:  float64(x-x0) * cellSize,
					Height: cellSize,
				})
				cur[key] = len(rects) - 1
			}
		}
		prev = cur
	}
	return rects
}

// ApplyMask marks every non-playable mask cell as blocked for pathfinding.
func (n *NavGrid) ApplyMask(mask [][]bool) {
	if mask == nil {
		return
	}
	for x := 0; x < n.Width && x < len(mask); x++ {
		for y := 0; y < n.Height && y < len(mask[x]); y++ {
			if !mask[x][y] {
				n.Blocked[x][y] = true
			}
		}
	}
}

// ApplyMask stamps blocked cells ('#') for every non-playable mask cell.
func (g *TerrainGrid) ApplyMask(mask [][]bool) {
	if mask == nil {
		return
	}
	for x := 0; x < g.Width && x < len(mask); x++ {
		for y := 0; y < g.Height && y < len(mask[x]); y++ {
			if !mask[x][y] {
				g.Cells[x][y] = '#'
			}
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
