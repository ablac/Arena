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
	ShapeSquare    MapShape = "square"
	ShapeCircle    MapShape = "circle"
	ShapeHexagon   MapShape = "hexagon"
	ShapeDiamond   MapShape = "diamond"
	ShapeCross     MapShape = "cross"
	ShapeCaves     MapShape = "caves"
	ShapeDonut     MapShape = "donut"
	ShapeIslands   MapShape = "islands"
	ShapeRooms     MapShape = "rooms"
	ShapeSpiral    MapShape = "spiral"
	ShapeStar      MapShape = "star"
	ShapeHourglass MapShape = "hourglass"
	ShapeClover    MapShape = "clover"
)

// builtInMapShapes is the single canonical list of built-in shapes. Name
// lookups, pool defaults, and pick validation all derive from it so a new
// shape only needs a const, this entry, and a generateShapeMask case.
var builtInMapShapes = []MapShape{
	ShapeSquare, ShapeCircle, ShapeHexagon, ShapeDiamond, ShapeCross,
	ShapeCaves, ShapeDonut, ShapeIslands, ShapeRooms, ShapeSpiral,
	ShapeStar, ShapeHourglass, ShapeClover,
}

// ActiveMapShape is the shape of the currently-active round's terrain.
// Set by the engine whenever it installs a terrain grid.
var ActiveMapShape = ShapeSquare

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
	names := make([]string, len(builtInMapShapes))
	for i, shape := range builtInMapShapes {
		names[i] = string(shape)
	}
	return names
}

func IsBuiltInMapShape(name string) bool {
	for _, shape := range builtInMapShapes {
		if MapShape(name) == shape {
			return true
		}
	}
	return false
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
	switch v := config.C.MapShape; {
	case v == "random":
		pool := NormalizeMapShapePool(strings.Split(config.C.MapShapePool, ","))
		return pool[rand.Intn(len(pool))]
	case IsBuiltInMapShape(v):
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

	case ShapeDonut:
		// Annulus: circle with a blocked inner disc. Forces rotational
		// combat around the core; the zone target re-rolls off blocked
		// terrain so the endgame ring always lands on the playable band.
		rOuter := math.Min(cx, cy)
		rInner := rOuter * 0.38
		for x := 0; x < cols; x++ {
			for y := 0; y < rows; y++ {
				dx, dy := float64(x)-cx, float64(y)-cy
				d2 := dx*dx + dy*dy
				mask[x][y] = d2 <= rOuter*rOuter && d2 >= rInner*rInner
			}
		}

	case ShapeIslands:
		generateIslandsMask(mask, cols, rows, rng)

	case ShapeRooms:
		generateRoomsMask(mask, cols, rows, rng)

	case ShapeSpiral:
		generateSpiralMask(mask, cols, rows)

	case ShapeStar:
		generateStarMask(mask, cols, rows)

	case ShapeHourglass:
		generateHourglassMask(mask, cols, rows)

	case ShapeClover:
		generateCloverMask(mask, cols, rows)
	}

	ensureConnected(mask, cols, rows)

	// Degenerate output (tiny playable area) falls back to a circle. Applies
	// to every procedurally-generated shape, not just caves.
	if playableFraction(mask, cols, rows) < 0.30 && shape != ShapeCircle {
		return generateShapeMask(ShapeCircle, cols, rows, rng)
	}
	return mask
}

// maskRandFloat / maskRandIntn draw from the supplied seeded rng when
// present (admin custom maps) and the global source otherwise.
func maskRandFloat(rng *rand.Rand) float64 {
	if rng != nil {
		return rng.Float64()
	}
	return rand.Float64()
}

func maskRandIntn(rng *rand.Rand, n int) int {
	if n <= 0 {
		return 0
	}
	if rng != nil {
		return rng.Intn(n)
	}
	return rand.Intn(n)
}

// generateIslandsMask carves 4-5 elliptical islands joined by wide bridges,
// giving an archipelago with chokepoints between open fighting areas.
func generateIslandsMask(mask [][]bool, cols, rows int, rng *rand.Rand) {
	type island struct{ cx, cy, rx, ry float64 }
	count := 4 + maskRandIntn(rng, 2)
	islands := make([]island, 0, count)

	minDim := math.Min(float64(cols), float64(rows))
	for i := 0; i < count; i++ {
		// Spread the island centres around the map with jitter: an anchor on
		// a ring plus one near the middle keeps layouts varied but usable.
		angle := (2*math.Pi*float64(i))/float64(count) + maskRandFloat(rng)*0.8
		ringR := minDim * (0.26 + maskRandFloat(rng)*0.08)
		icx := float64(cols)/2 + math.Cos(angle)*ringR
		icy := float64(rows)/2 + math.Sin(angle)*ringR
		r := minDim * (0.12 + maskRandFloat(rng)*0.06)
		islands = append(islands, island{icx, icy, r * (0.8 + maskRandFloat(rng)*0.4), r * (0.8 + maskRandFloat(rng)*0.4)})
	}

	for x := 0; x < cols; x++ {
		for y := 0; y < rows; y++ {
			open := false
			for _, is := range islands {
				dx := (float64(x) - is.cx) / is.rx
				dy := (float64(y) - is.cy) / is.ry
				if dx*dx+dy*dy <= 1 {
					open = true
					break
				}
			}
			mask[x][y] = open && x > 0 && y > 0 && x < cols-1 && y < rows-1
		}
	}

	// Bridges between consecutive islands (and a closing loop) so
	// ensureConnected doesn't have to discard any of them.
	for i := range islands {
		a, b := islands[i], islands[(i+1)%len(islands)]
		carveCorridor(mask, cols, rows, a.cx, a.cy, b.cx, b.cy, 2)
	}
}

// generateRoomsMask carves a dungeon: rectangular rooms linked by L-shaped
// corridors, all walls elsewhere.
func generateRoomsMask(mask [][]bool, cols, rows int, rng *rand.Rand) {
	for x := range mask {
		for y := range mask[x] {
			mask[x][y] = false
		}
	}

	type room struct{ x0, y0, x1, y1 int }
	count := 8 + maskRandIntn(rng, 3)
	rooms := make([]room, 0, count)
	for i := 0; i < count; i++ {
		w := 14 + maskRandIntn(rng, cols/6)
		h := 14 + maskRandIntn(rng, rows/6)
		x0 := 2 + maskRandIntn(rng, maxInt(1, cols-w-4))
		y0 := 2 + maskRandIntn(rng, maxInt(1, rows-h-4))
		rooms = append(rooms, room{x0, y0, x0 + w, y0 + h})
	}

	for _, r := range rooms {
		for x := r.x0; x <= r.x1 && x < cols-1; x++ {
			for y := r.y0; y <= r.y1 && y < rows-1; y++ {
				mask[x][y] = true
			}
		}
	}

	// L-shaped corridors between consecutive room centres, plus one long
	// link from the last room back to the first for a loop.
	for i := range rooms {
		a, b := rooms[i], rooms[(i+1)%len(rooms)]
		acx, acy := float64(a.x0+a.x1)/2, float64(a.y0+a.y1)/2
		bcx, bcy := float64(b.x0+b.x1)/2, float64(b.y0+b.y1)/2
		carveCorridor(mask, cols, rows, acx, acy, bcx, acy, 2)
		carveCorridor(mask, cols, rows, bcx, acy, bcx, bcy, 2)
	}
}

// generateSpiralMask carves a single wide spiral corridor from the rim to an
// open centre arena — a dramatic endgame as the zone collapses inward.
func generateSpiralMask(mask [][]bool, cols, rows int) {
	for x := range mask {
		for y := range mask[x] {
			mask[x][y] = false
		}
	}

	ccx, ccy := float64(cols-1)/2, float64(rows-1)/2
	maxR := math.Min(ccx, ccy) - 1
	const turns = 2.25
	halfWidth := math.Max(4, math.Min(float64(cols), float64(rows))*0.07)

	// Walk the Archimedean spiral from rim to centre carving open discs.
	steps := int(turns * 2 * math.Pi * maxR) // dense enough to leave no gaps
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		theta := t * turns * 2 * math.Pi
		r := (1 - t) * maxR
		px := ccx + math.Cos(theta)*r
		py := ccy + math.Sin(theta)*r
		carveDisc(mask, cols, rows, px, py, halfWidth)
	}
	// Open centre arena where the spiral terminates.
	carveDisc(mask, cols, rows, ccx, ccy, math.Max(halfWidth*2.2, maxR*0.18))
}

// generateStarMask carves a five-point arena with broad arms and a generous
// centre, creating alternating long sightlines and defensive pockets.
func generateStarMask(mask [][]bool, cols, rows int) {
	cx, cy := float64(cols-1)/2, float64(rows-1)/2
	outerR := math.Min(cx, cy) - 1
	for x := 1; x < cols-1; x++ {
		for y := 1; y < rows-1; y++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			angle := math.Atan2(dy, dx) - math.Pi/2
			limit := outerR * (0.72 + 0.22*math.Cos(5*angle))
			mask[x][y] = math.Hypot(dx, dy) <= limit
		}
	}
}

// generateHourglassMask carves two broad fighting zones joined by a wide
// waist. The centre is narrow enough to matter without becoming a pinch point.
func generateHourglassMask(mask [][]bool, cols, rows int) {
	cx, cy := float64(cols-1)/2, float64(rows-1)/2
	for x := 1; x < cols-1; x++ {
		for y := 1; y < rows-1; y++ {
			nx := math.Abs(float64(x)-cx) / cx
			ny := math.Abs(float64(y)-cy) / cy
			mask[x][y] = ny <= 0.96 && nx <= 0.28+0.68*ny
		}
	}
}

// generateCloverMask joins four overlapping circular lobes around an open
// centre, giving combatants several routes between roomy skirmish areas.
func generateCloverMask(mask [][]bool, cols, rows int) {
	cx, cy := float64(cols-1)/2, float64(rows-1)/2
	r := math.Min(cx, cy)
	offset := r * 0.30
	lobeR := r * 0.55
	centres := [][2]float64{
		{cx + offset, cy}, {cx - offset, cy},
		{cx, cy + offset}, {cx, cy - offset},
	}
	for x := 1; x < cols-1; x++ {
		for y := 1; y < rows-1; y++ {
			for _, centre := range centres {
				if math.Hypot(float64(x)-centre[0], float64(y)-centre[1]) <= lobeR {
					mask[x][y] = true
					break
				}
			}
		}
	}
}

// carveDisc opens all cells within radius of (px, py), keeping a 1-cell rim.
func carveDisc(mask [][]bool, cols, rows int, px, py, radius float64) {
	minX := maxInt(1, int(px-radius))
	maxX := minInt(cols-2, int(px+radius+1))
	minY := maxInt(1, int(py-radius))
	maxY := minInt(rows-2, int(py+radius+1))
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			dx, dy := float64(x)-px, float64(y)-py
			if dx*dx+dy*dy <= radius*radius {
				mask[x][y] = true
			}
		}
	}
}

// carveCorridor opens a straight corridor of the given half-width between
// two points by stamping discs along the segment.
func carveCorridor(mask [][]bool, cols, rows int, x0, y0, x1, y1 float64, halfWidth float64) {
	dist := math.Hypot(x1-x0, y1-y0)
	steps := maxInt(1, int(dist))
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		carveDisc(mask, cols, rows, x0+(x1-x0)*t, y0+(y1-y0)*t, halfWidth)
	}
}

// generateRoundTerrain rolls a map shape and builds a round's obstacles and
// grids with the shape carved into both, sized for the given bot count.
// Returns the boundary rectangles of the carved area for client rendering.
// The shape mask is generated FIRST so obstacle placement can reject spots
// inside carved walls, and the combined grid is connectivity-checked so an
// obstacle can never seal off part of the playable area.
func generateRoundTerrain(botCount int) (obstacles []Obstacle, nav *NavGrid, terrain *TerrainGrid, shape MapShape, maskRects []Obstacle) {
	c := &config.C
	scale := ApplyDynamicArenaSize(botCount)
	shape = PickMapShape()

	// Same cell math as NewTerrainGrid / NewNavGrid.
	cols := int(math.Ceil(c.ArenaWidth / c.PathfindingCellSize))
	rows := int(math.Ceil(c.ArenaHeight / c.PathfindingCellSize))
	mask := GenerateShapeMask(shape, cols, rows)

	// Preserve obstacle density on scaled maps: counts grow with area.
	area := scale * scale
	obsMin := int(float64(c.ObstacleCountMin) * area)
	obsMax := int(float64(c.ObstacleCountMax) * area)

	// Rare layouts can still pinch the map into disconnected pockets (e.g.
	// an obstacle across a narrow cave tunnel). Re-roll the obstacles a few
	// times if that happens; keep the last attempt if it never resolves —
	// no worse than the old behavior, and the anti-stuck rescue still works.
	for attempt := 0; attempt < 5; attempt++ {
		obstacles = GenerateObstaclesInMask(c.ArenaWidth, c.ArenaHeight, obsMin, obsMax, mask, c.PathfindingCellSize, c.BotRadius)
		terrain = NewTerrainGrid(c.ArenaWidth, c.ArenaHeight, obstacles, c.PathfindingCellSize, c.BotRadius)
		terrain.ApplyMask(mask)
		if terrain.FullyConnected() {
			break
		}
	}

	nav = NewNavGrid(c.ArenaWidth, c.ArenaHeight, obstacles, c.BotRadius)
	if mask != nil {
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
