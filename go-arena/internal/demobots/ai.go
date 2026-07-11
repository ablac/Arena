package demobots

import (
	"math"
	"math/rand"
	"sync"

	"arena-server/internal/config"
)

// === Types ===

type tickState struct {
	Tick              int
	RoundTick         int
	Team              int
	Mode              string
	SuddenDeath       bool
	SuddenDeathStall  bool
	Position          [2]float64
	Speed             float64
	HP                float64
	MaxHP             float64
	WeaponReady       bool
	Cooldown          float64
	DodgeCool         int
	InvulnTicks       int
	StunTicks         int
	ShoveCool         float64
	ShieldHP          float64
	InZone            bool
	ZoneDist          float64
	ZoneCenter        [2]float64
	ZoneRadius        float64
	ZoneTargetCenter  [2]float64
	ZoneTargetRadius  float64
	KillStreak        int
	RoundKills        int
	HitsThisTick      int
	LastActionOK      bool
	HasSpeedBoost     bool
	HasDmgBoost       bool
	HasHazardKey      bool
	HasRelayBattery   bool
	BraceReady        bool
	BowChargeTicks    int
	BowChargeLevel    float64
	ChargedShotReady  bool
	MineCount         int
	NearbyMines       int
	GravityWellCharge int
	GrappleCharges    int
	GrappleCooldown   float64
	IsBountyTarget    bool
	RoundModifier     string
	FastZone          bool
	PickupSurge       bool
	DoubleBounty      bool
	TeleportSurge     bool
	HazardStorm       bool
	BountyTargetID    string
	Enemies           []entity
	Allies            []entity
	Pickups           []entity
	Teleporters       []entity
	CapturePads       []entity
	HazardZones       []entity
	Mines             []entity
	GravityWells      []entity
	Flags             []entity
	VoidTiles         [][2]int
	TeamScores        map[string]int
	Hints             []hint
	Danger            *dangerSet
}

// isEnemyFlagCarrier reports whether the given bot ID carries any flag.
// Allies never appear in ts.Enemies, so callers scoring enemies can use this
// directly to detect enemy carriers.
func (ts *tickState) isFlagCarrier(id string) bool {
	if id == "" {
		return false
	}
	for i := range ts.Flags {
		if ts.Flags[i].CarrierID == id {
			return true
		}
	}
	return false
}

type entity struct {
	ID                string
	Type              string
	SubType           string
	Position          [2]float64
	HP                float64
	MaxHP             float64
	Weapon            string
	IsAlive           bool
	Stunned           bool
	Dodging           bool
	TargetID          string
	Facing            [2]float64
	Radius            float64
	LinkedID          string
	Color             string
	Ready             bool
	Cooldown          int
	OwnerID           string
	CapturingBotID    string
	ProgressTicks     int
	CaptureTicks      int
	ContenderCount    int
	HasLOS            bool
	CanAttack         bool
	Active            bool
	Contested         bool
	DisruptedTicks    int
	BraceReady        bool
	BowChargeLevel    float64
	ChargedShotReady  bool
	RearExposed       bool
	NearImpactSurface bool
	Team              int
	ThreatScore       float64
	// Hazard zone rect + pulse fields (server sends width/height in grid
	// cells instead of a radius for hazard_zone entities).
	Width         int
	Height        int
	OnTicks       int
	OffTicks      int
	TickCounter   int
	DamagePerTick float64
	// Landmine fields.
	Armed bool
	// Gravity well fields.
	PullRadius int
	// CTF flag fields (parsed from the top-level "flags" tick array).
	Status       string
	CarrierID    string
	BasePosition [2]float64
}

type hint struct {
	HintType   string
	Direction  [2]float64
	Distance   float64
	PickupType string
}

// botTerrain caches the map_init terrain grid for AI decisions.
type botTerrain struct {
	Width       int
	Height      int
	CellSize    float64
	Cells       [][]byte // [col][row] matching server layout
	Teleporters map[string]entity
	HazardZones []entity
}

var (
	cachedTerrain *botTerrain
	terrainMu     sync.RWMutex
)

// === Mine tracking ===

var (
	mineCount   = make(map[string]int) // botID -> mines placed this life
	mineCountMu sync.Mutex
)

func getMineCount(botID string) int {
	mineCountMu.Lock()
	defer mineCountMu.Unlock()
	return mineCount[botID]
}

func incMineCount(botID string) {
	mineCountMu.Lock()
	defer mineCountMu.Unlock()
	mineCount[botID]++
}

func resetMineCount(botID string) {
	mineCountMu.Lock()
	defer mineCountMu.Unlock()
	// Delete rather than zero so entries for stale bot IDs don't accumulate.
	delete(mineCount, botID)
}

// === Gravity well tracking ===

var (
	hasGravWell = make(map[string]bool) // botID -> has gravity well charge
	gravWellMu  sync.Mutex
)

func getHasGravWell(botID string) bool {
	gravWellMu.Lock()
	defer gravWellMu.Unlock()
	return hasGravWell[botID]
}

func setHasGravWell(botID string, v bool) {
	gravWellMu.Lock()
	defer gravWellMu.Unlock()
	hasGravWell[botID] = v
}

func resetGravWell(botID string) {
	gravWellMu.Lock()
	defer gravWellMu.Unlock()
	// Delete rather than zero so entries for stale bot IDs don't accumulate.
	delete(hasGravWell, botID)
}

// parseTerrain extracts and caches terrain from a map_init message.
func parseTerrain(msg map[string]interface{}) {
	w, _ := msg["width"].(float64)
	h, _ := msg["height"].(float64)
	cs, _ := msg["cell_size"].(float64)
	width := int(w)
	height := int(h)
	if width <= 0 || height <= 0 {
		return
	}

	cells := make([][]byte, width)
	for x := range cells {
		cells[x] = make([]byte, height)
		for y := range cells[x] {
			cells[x][y] = '.'
		}
	}

	if rows, ok := msg["terrain"].([]interface{}); ok {
		for row, rowData := range rows {
			if row >= height {
				break
			}
			// Compact format: each row is a single string like "..##.."
			if rowStr, ok := rowData.(string); ok {
				for col := 0; col < len(rowStr) && col < width; col++ {
					cells[col][row] = rowStr[col]
				}
				continue
			}
			// Legacy format: each row is an array of single-char strings
			cols, ok := rowData.([]interface{})
			if !ok {
				continue
			}
			for col, cell := range cols {
				if col >= width {
					break
				}
				if s, ok := cell.(string); ok && len(s) > 0 {
					cells[col][row] = s[0]
				}
			}
		}
	}

	teleporters := make(map[string]entity)
	if rawPads, ok := msg["teleport_pads"].([]interface{}); ok {
		for _, raw := range rawPads {
			pad, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			ent := entity{Type: "teleport_pad", Position: parsePos(pad["position"]), Ready: true}
			if v, ok := pad["id"].(string); ok {
				ent.ID = v
			}
			if v, ok := pad["linked_pad_id"].(string); ok {
				ent.LinkedID = v
			}
			if v, ok := pad["color"].(string); ok {
				ent.Color = v
			}
			if v, ok := pad["is_ready"].(bool); ok {
				ent.Ready = v
			}
			if v, ok := pad["cooldown_remaining_ticks"].(float64); ok {
				ent.Cooldown = int(v)
			}
			if ent.ID != "" {
				teleporters[ent.ID] = ent
			}
		}
	}

	var hazardZones []entity
	if rawZones, ok := msg["hazard_zones"].([]interface{}); ok {
		for _, raw := range rawZones {
			zone, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			ent := entity{Type: "hazard_zone", Position: parsePos(zone["position"])}
			if v, ok := zone["id"].(string); ok {
				ent.ID = v
			}
			if v, ok := zone["width"].(float64); ok {
				ent.Width = int(v)
			}
			if v, ok := zone["height"].(float64); ok {
				ent.Height = int(v)
			}
			if v, ok := zone["radius"].(float64); ok {
				ent.Radius = v
			}
			hazardZones = append(hazardZones, ent)
		}
	}

	terrainMu.Lock()
	cachedTerrain = &botTerrain{
		Width: width, Height: height, CellSize: cs, Cells: cells,
		Teleporters: teleporters, HazardZones: hazardZones,
	}
	terrainMu.Unlock()
}

func getTerrain() *botTerrain {
	terrainMu.RLock()
	t := cachedTerrain
	terrainMu.RUnlock()
	return t
}

// isBlocked returns true if the grid cell is a wall, void, or out of bounds.
func (t *botTerrain) isBlocked(col, row int) bool {
	if col < 0 || row < 0 || col >= t.Width || row >= t.Height {
		return true
	}
	c := t.Cells[col][row]
	return c == '#' || c == 'V'
}

// isMoveBlocked checks if moving from (cx,cy) by (dx,dy) is blocked,
// including diagonal corner-cutting prevention.
func (t *botTerrain) isMoveBlocked(cx, cy, dx, dy int) bool {
	if t.isBlocked(cx+dx, cy+dy) {
		return true
	}
	if dx != 0 && dy != 0 {
		if t.isBlocked(cx+dx, cy) || t.isBlocked(cx, cy+dy) {
			return true
		}
	}
	return false
}

// === Danger Set ===

// dangerSet is the per-tick set of lethal cells plus active teleporter trigger
// footprints the bot should not enter accidentally. Instances are pooled
// (demo bots run concurrently, so package-level scratch would race) and rebuilt
// each PickAction without allocation growth.
type dangerSet struct {
	cells     map[[2]int]struct{}
	padCells  map[[2]int]struct{}
	allowPad  [2]int
	allowDist int
	hasAllow  bool
}

var dangerPool = sync.Pool{New: func() interface{} { return &dangerSet{} }}

func (d *dangerSet) reset() {
	d.hasAllow = false
	d.allowDist = 0
	if d.cells == nil {
		d.cells = make(map[[2]int]struct{}, 64)
	} else {
		clear(d.cells)
	}
	if d.padCells == nil {
		d.padCells = make(map[[2]int]struct{}, 24)
	} else {
		clear(d.padCells)
	}
}

func (d *dangerSet) empty() bool {
	return d == nil || (len(d.cells) == 0 && len(d.padCells) == 0)
}

func (d *dangerSet) has(col, row int) bool {
	if d == nil {
		return false
	}
	cell := [2]int{col, row}
	if _, lethal := d.cells[cell]; lethal {
		return true
	}
	return d.hasPad(col, row)
}

func (d *dangerSet) hasPad(col, row int) bool {
	if d == nil {
		return false
	}
	cell := [2]int{col, row}
	if _, blocked := d.padCells[cell]; !blocked {
		return false
	}
	return !d.hasAllow || intChebyshev(cell, d.allowPad) > d.allowDist
}

func (d *dangerSet) hasLethal(col, row int) bool {
	if d == nil {
		return false
	}
	_, lethal := d.cells[[2]int{col, row}]
	return lethal
}

// blocksExceptPad keeps lethal danger and every other ready pad blocked while
// opening only the selected source pad's soft avoidance footprint. Candidate
// evaluation uses this instead of mutating the tick's shared danger set.
func (d *dangerSet) blocksExceptPad(col, row int, pad [2]int, allowRadius int) bool {
	if d == nil {
		return false
	}
	cell := [2]int{col, row}
	if _, lethal := d.cells[cell]; lethal {
		return true
	}
	if _, blocked := d.padCells[cell]; !blocked {
		return false
	}
	return intChebyshev(cell, pad) > allowRadius
}

func (d *dangerSet) addPad(center [2]float64, radius int) {
	cc, cr := int(math.Round(center[0])), int(math.Round(center[1]))
	for col := cc - radius; col <= cc+radius; col++ {
		for row := cr - radius; row <= cr+radius; row++ {
			d.padCells[[2]int{col, row}] = struct{}{}
		}
	}
}

// allowPadNear opens only the selected teleporter's soft avoidance footprint.
// Lethal cells remain blocked even when they overlap the selected pad.
func (d *dangerSet) allowPadNear(center [2]float64, radius int) {
	if d == nil {
		return
	}
	d.allowPad = [2]int{int(math.Round(center[0])), int(math.Round(center[1]))}
	d.allowDist = radius
	d.hasAllow = true
}

func (d *dangerSet) add(col, row int) {
	d.cells[[2]int{col, row}] = struct{}{}
}

// addSquare marks all cells within a Chebyshev radius of center.
func (d *dangerSet) addSquare(center [2]float64, radius int) {
	d.addRect(center, 2*radius+1, 2*radius+1)
}

// addRect marks a rectangle of width x height grid cells centered on center,
// using the same integer half-extent math as the server's hazard damage check.
func (d *dangerSet) addRect(center [2]float64, width, height int) {
	cc, cr := int(math.Round(center[0])), int(math.Round(center[1]))
	halfW, halfH := width/2, height/2
	for col := cc - halfW; col <= cc+halfW; col++ {
		for row := cr - halfH; row <= cr+halfH; row++ {
			d.add(col, row)
		}
	}
}

// buildDangerSet populates the danger set for this tick from the parsed state.
func buildDangerSet(d *dangerSet, ts *tickState, botID string) {
	d.reset()
	for i := range ts.HazardZones {
		// Hazard key grants immunity to both hazard zones and burn fields.
		if ts.HasHazardKey {
			break
		}
		h := &ts.HazardZones[i]
		if !h.Active {
			continue
		}
		if h.Width > 0 || h.Height > 0 {
			// Rectangular pulsing hazard zone.
			d.addRect(h.Position, h.Width, h.Height)
			continue
		}
		// Radial burn field.
		r := int(h.Radius)
		if r <= 0 {
			r = 2
		}
		d.addSquare(h.Position, r)
	}
	for i := range ts.GravityWells {
		w := &ts.GravityWells[i]
		if w.OwnerID == botID {
			continue // own well never pulls us
		}
		r := w.PullRadius
		if r <= 0 {
			r = 3 // server default ARENA_GRAVITY_WELL_PULL_RADIUS
		}
		d.addSquare(w.Position, r)
	}
	for i := range ts.Mines {
		m := &ts.Mines[i]
		if m.OwnerID == botID || !m.Armed {
			continue
		}
		// Mine blast radius is small (server default 1 tile).
		d.addSquare(m.Position, 1)
	}
	for _, vt := range ts.VoidTiles {
		d.add(vt[0], vt[1])
	}

	// The server can move a boosted bot more than one cell for a single action.
	// Expand the pad footprint by that extra travel so a direction that is safe
	// for its first cell cannot cross an active trigger later in the same tick.
	avoidRadius := teleporterAvoidanceRadius(*ts)
	for i := range ts.Teleporters {
		if isReadyTeleporter(ts.Teleporters[i]) {
			d.addPad(ts.Teleporters[i].Position, avoidRadius)
		}
	}
}

func maxMoveCellsPerTick(ts tickState) int {
	referencePoints := float64(config.C.StatBudget) / 4
	referenceSpeed := config.C.StatSpeedBase + referencePoints*config.C.StatSpeedPerPoint
	if referenceSpeed <= 0 || ts.Speed <= 0 {
		return 1
	}
	cells := int(math.Ceil(0.5 * ts.Speed / referenceSpeed))
	if cells < 1 {
		return 1
	}
	return cells
}

func teleporterAvoidanceRadius(ts tickState) int {
	radius := config.C.TeleportCollectRadius + maxMoveCellsPerTick(ts) - 1
	if radius < 0 {
		return 0
	}
	return radius
}

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
	stamp      uint32
	queue      []bfsNode
	cols, rows int
}

var bfsPool = sync.Pool{New: func() interface{} { return &bfsScratch{} }}

func (s *bfsScratch) reset(cols, rows int) {
	if s.cols != cols || s.rows != rows || len(s.visited) != cols*rows {
		s.visited = make([]uint32, cols*rows)
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

// === Action Builders ===

type actionResult struct {
	Action         string      `json:"action"`
	Target         string      `json:"target,omitempty"`
	Direction      *[2]float64 `json:"direction,omitempty"`
	TargetPosition *[2]float64 `json:"target_position,omitempty"`
	ItemID         string      `json:"item_id,omitempty"`
	Charged        bool        `json:"charged,omitempty"`
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
	if (dx == 0 && dy == 0) || danger.empty() {
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

// moveTo uses BFS pathfinding to take one danger-aware step from src toward dst.
func moveTo(src, dst [2]float64, danger *dangerSet) actionResult {
	d := bfsDir(src, dst, danger)
	if d[0] == 0 && d[1] == 0 {
		return idle()
	}
	return actionResult{Action: "move", Direction: &d}
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

// === Tick Parsing ===

func parseTick(msg map[string]interface{}) tickState {
	ts := tickState{
		ZoneCenter: [2]float64{1000, 1000}, ZoneRadius: 1000,
		ZoneTargetCenter: [2]float64{1000, 1000}, ZoneTargetRadius: 500,
		MaxHP: 150, InZone: true, LastActionOK: true,
	}
	if v, ok := msg["tick"].(float64); ok {
		ts.Tick = int(v)
	}
	if v, ok := msg["round_tick"].(float64); ok {
		ts.RoundTick = int(v)
	}
	if v, ok := msg["round_modifier"].(string); ok {
		ts.RoundModifier = v
		ts.FastZone = v == "fast_zone"
		ts.PickupSurge = v == "pickup_surge"
		ts.DoubleBounty = v == "double_bounty"
		ts.TeleportSurge = v == "teleport_surge"
		ts.HazardStorm = v == "hazard_storm"
	}
	if v, ok := msg["game_mode"].(string); ok {
		ts.Mode = v
	}
	if v, ok := msg["sudden_death"].(bool); ok {
		ts.SuddenDeath = v
	}
	if v, ok := msg["sudden_death_stall"].(bool); ok {
		ts.SuddenDeathStall = v
	}
	if v, ok := msg["bounty_target"].(string); ok {
		ts.BountyTargetID = v
	}
	if vt, ok := msg["void_tiles"].([]interface{}); ok {
		for _, raw := range vt {
			if cell, ok := raw.([]interface{}); ok && len(cell) >= 2 {
				x, _ := cell[0].(float64)
				y, _ := cell[1].(float64)
				ts.VoidTiles = append(ts.VoidTiles, [2]int{int(x), int(y)})
			}
		}
	}
	if scores, ok := msg["team_scores"].(map[string]interface{}); ok {
		ts.TeamScores = make(map[string]int, len(scores))
		for k, raw := range scores {
			if v, ok := raw.(float64); ok {
				ts.TeamScores[k] = int(v)
			}
		}
	}
	if flags, ok := msg["flags"].([]interface{}); ok {
		for _, raw := range flags {
			f, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			// BuildFlagView sends spectator-style world coordinates, so
			// convert to grid tiles like everything else the AI reasons in.
			ent := entity{Type: "flag", Position: worldToGridPos(parsePos(f["position"])), IsAlive: true}
			if v, ok := f["id"].(string); ok {
				ent.ID = v
			}
			if v, ok := f["team"].(float64); ok {
				ent.Team = int(v)
			}
			if v, ok := f["base_position"]; ok {
				ent.BasePosition = worldToGridPos(parsePos(v))
			}
			if v, ok := f["status"].(string); ok {
				ent.Status = v
			}
			if v, ok := f["carrier_id"].(string); ok {
				ent.CarrierID = v
			}
			ts.Flags = append(ts.Flags, ent)
		}
	}
	if ys, ok := msg["your_state"].(map[string]interface{}); ok {
		ts.Position = parsePos(ys["position"])
		if v, ok := ys["speed"].(float64); ok {
			ts.Speed = v
		}
		if v, ok := ys["team"].(float64); ok {
			ts.Team = int(v)
		}
		if v, ok := ys["hp"].(float64); ok {
			ts.HP = v
		}
		if v, ok := ys["max_hp"].(float64); ok {
			ts.MaxHP = v
		}
		if v, ok := ys["weapon_ready"].(bool); ok {
			ts.WeaponReady = v
		}
		if v, ok := ys["cooldown_remaining"].(float64); ok {
			ts.Cooldown = v
		}
		if v, ok := ys["dodge_cooldown"].(float64); ok {
			ts.DodgeCool = int(v)
		}
		if v, ok := ys["shove_cooldown"].(float64); ok {
			ts.ShoveCool = v
		}
		if v, ok := ys["invuln_ticks"].(float64); ok {
			ts.InvulnTicks = int(v)
		}
		if v, ok := ys["stun_ticks"].(float64); ok {
			ts.StunTicks = int(v)
		}
		if v, ok := ys["shield_absorb"].(float64); ok {
			ts.ShieldHP = v
		}
		if v, ok := ys["in_safe_zone"].(bool); ok {
			ts.InZone = v
		}
		if v, ok := ys["distance_to_zone_edge"].(float64); ok {
			ts.ZoneDist = v
		}
		if v, ok := ys["zone_center"]; ok {
			ts.ZoneCenter = parsePos(v)
		}
		if v, ok := ys["zone_radius"].(float64); ok {
			ts.ZoneRadius = v
		}
		if v, ok := ys["zone_target_center"]; ok {
			ts.ZoneTargetCenter = parsePos(v)
		}
		if v, ok := ys["zone_target_radius"].(float64); ok {
			ts.ZoneTargetRadius = v
		}
		if v, ok := ys["kill_streak"].(float64); ok {
			ts.KillStreak = int(v)
		}
		if v, ok := ys["round_kills"].(float64); ok {
			ts.RoundKills = int(v)
		}
		if hits, ok := ys["hits_received"].([]interface{}); ok {
			ts.HitsThisTick = len(hits)
		}
		if lar, ok := ys["last_action_result"].(map[string]interface{}); ok {
			if v, ok := lar["success"].(bool); ok {
				ts.LastActionOK = v
			}
		}
		if effs, ok := ys["effects"].([]interface{}); ok {
			for _, raw := range effs {
				if e, ok := raw.(map[string]interface{}); ok {
					if name, ok := e["name"].(string); ok {
						if name == "speed_boost" {
							ts.HasSpeedBoost = true
						}
						if name == "damage_boost" {
							ts.HasDmgBoost = true
						}
						if name == "hazard_key" {
							ts.HasHazardKey = true
						}
						if name == "relay_battery" {
							ts.HasRelayBattery = true
						}
					}
				}
			}
		}
		if v, ok := ys["gravity_well_charge"].(float64); ok {
			ts.GravityWellCharge = int(v)
		}
		if v, ok := ys["grapple_charges"].(float64); ok {
			ts.GrappleCharges = int(v)
		}
		if v, ok := ys["grapple_cooldown"].(float64); ok {
			ts.GrappleCooldown = v
		}
		if v, ok := ys["is_bounty_target"].(bool); ok {
			ts.IsBountyTarget = v
		}
		if v, ok := ys["brace_ready"].(bool); ok {
			ts.BraceReady = v
		}
		if v, ok := ys["bow_charge_ticks"].(float64); ok {
			ts.BowChargeTicks = int(v)
		}
		if v, ok := ys["bow_charge_level"].(float64); ok {
			ts.BowChargeLevel = v
		}
		if v, ok := ys["charged_shot_ready"].(bool); ok {
			ts.ChargedShotReady = v
		}
		if v, ok := ys["mine_count"].(float64); ok {
			ts.MineCount = int(v)
		}
	}
	if v, ok := msg["nearby_mines"].(float64); ok {
		ts.NearbyMines = int(v)
	}
	if sz, ok := msg["safe_zone"].(map[string]interface{}); ok {
		if v, ok := sz["center"]; ok {
			ts.ZoneCenter = parsePos(v)
		}
		if v, ok := sz["radius"].(float64); ok {
			ts.ZoneRadius = v
		}
		if v, ok := sz["target_center"]; ok {
			ts.ZoneTargetCenter = parsePos(v)
		}
		if v, ok := sz["target_radius"].(float64); ok {
			ts.ZoneTargetRadius = v
		}
	}
	if ne, ok := msg["nearby_entities"].([]interface{}); ok {
		for _, raw := range ne {
			e, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			ent := entity{Position: parsePos(e["position"]), IsAlive: true}
			if v, ok := e["type"].(string); ok {
				ent.Type = v
			}
			if v, ok := e["id"].(string); ok {
				ent.ID = v
			}
			if v, ok := e["pickup_id"].(string); ok && ent.ID == "" {
				ent.ID = v
			}
			if v, ok := e["bot_id"].(string); ok && ent.ID == "" {
				ent.ID = v
			}
			if v, ok := e["hp"].(float64); ok {
				ent.HP = v
			}
			if v, ok := e["max_hp"].(float64); ok {
				ent.MaxHP = v
			}
			if v, ok := e["weapon"].(string); ok {
				ent.Weapon = v
			}
			if v, ok := e["is_alive"].(bool); ok {
				ent.IsAlive = v
			}
			if v, ok := e["is_stunned"].(bool); ok {
				ent.Stunned = v
			}
			if v, ok := e["is_dodging"].(bool); ok {
				ent.Dodging = v
			}
			if v, ok := e["target_id"].(string); ok {
				ent.TargetID = v
			}
			if v, ok := e["facing"]; ok {
				ent.Facing = parsePos(v)
			}
			if v, ok := e["pickup_type"].(string); ok {
				ent.SubType = v
			}
			if v, ok := e["radius"].(float64); ok {
				ent.Radius = v
			}
			if v, ok := e["linked_pad_id"].(string); ok {
				ent.LinkedID = v
			}
			if v, ok := e["color"].(string); ok {
				ent.Color = v
			}
			if v, ok := e["is_ready"].(bool); ok {
				ent.Ready = v
			} else {
				ent.Ready = true
			}
			if v, ok := e["cooldown_remaining_ticks"].(float64); ok {
				ent.Cooldown = int(v)
			}
			if v, ok := e["owner_id"].(string); ok {
				ent.OwnerID = v
			}
			if v, ok := e["capturing_bot_id"].(string); ok {
				ent.CapturingBotID = v
			}
			if v, ok := e["progress_ticks"].(float64); ok {
				ent.ProgressTicks = int(v)
			}
			if v, ok := e["capture_ticks"].(float64); ok {
				ent.CaptureTicks = int(v)
			}
			if v, ok := e["contender_count"].(float64); ok {
				ent.ContenderCount = int(v)
			}
			if v, ok := e["has_los"].(bool); ok {
				ent.HasLOS = v
			} else {
				ent.HasLOS = true
			}
			if v, ok := e["can_attack"].(bool); ok {
				ent.CanAttack = v
			}
			if v, ok := e["active"].(bool); ok {
				ent.Active = v
			}
			if v, ok := e["is_contested"].(bool); ok {
				ent.Contested = v
			}
			if v, ok := e["recently_disrupted_ticks"].(float64); ok {
				ent.DisruptedTicks = int(v)
			}
			if v, ok := e["brace_ready"].(bool); ok {
				ent.BraceReady = v
			}
			if v, ok := e["bow_charge_level"].(float64); ok {
				ent.BowChargeLevel = v
			}
			if v, ok := e["charged_shot_ready"].(bool); ok {
				ent.ChargedShotReady = v
			}
			if v, ok := e["rear_exposed"].(bool); ok {
				ent.RearExposed = v
			}
			if v, ok := e["near_impact_surface"].(bool); ok {
				ent.NearImpactSurface = v
			}
			if v, ok := e["team"].(float64); ok {
				ent.Team = int(v)
			}
			if v, ok := e["threat_score"].(float64); ok {
				ent.ThreatScore = v
			}
			if v, ok := e["width"].(float64); ok {
				ent.Width = int(v)
			}
			if v, ok := e["height"].(float64); ok {
				ent.Height = int(v)
			}
			if v, ok := e["on_ticks"].(float64); ok {
				ent.OnTicks = int(v)
			}
			if v, ok := e["off_ticks"].(float64); ok {
				ent.OffTicks = int(v)
			}
			if v, ok := e["tick_counter"].(float64); ok {
				ent.TickCounter = int(v)
			}
			if v, ok := e["damage_per_tick"].(float64); ok {
				ent.DamagePerTick = v
			}
			if v, ok := e["armed"].(bool); ok {
				ent.Armed = v
			}
			if v, ok := e["pull_radius"].(float64); ok {
				ent.PullRadius = int(v)
			}
			switch ent.Type {
			case "bot":
				if ent.IsAlive {
					// Teammates are allies, everyone else is an enemy.
					if ent.Team != 0 && ent.Team == ts.Team {
						ts.Allies = append(ts.Allies, ent)
					} else {
						ts.Enemies = append(ts.Enemies, ent)
					}
				}
			case "bounty_target":
				ts.Enemies = append(ts.Enemies, ent)
			case "pickup":
				ts.Pickups = append(ts.Pickups, ent)
			case "teleport_pad", "teleporter":
				ts.Teleporters = append(ts.Teleporters, ent)
			case "capture_pad":
				ts.CapturePads = append(ts.CapturePads, ent)
			case "hazard_zone":
				ts.HazardZones = append(ts.HazardZones, ent)
			case "burn_field":
				ts.HazardZones = append(ts.HazardZones, ent)
			case "landmine":
				ts.Mines = append(ts.Mines, ent)
			case "gravity_well":
				ts.GravityWells = append(ts.GravityWells, ent)
			}
		}
	}
	if h, ok := msg["hints"].([]interface{}); ok {
		for _, raw := range h {
			if hm, ok := raw.(map[string]interface{}); ok {
				hi := hint{}
				if v, ok := hm["hint_type"].(string); ok {
					hi.HintType = v
				}
				if v, ok := hm["direction"]; ok {
					hi.Direction = parsePos(v)
				}
				if v, ok := hm["distance"].(float64); ok {
					hi.Distance = v
				}
				if v, ok := hm["pickup_type"].(string); ok {
					hi.PickupType = v
				}
				ts.Hints = append(ts.Hints, hi)
			}
		}
	}
	return ts
}

// worldToGridPos converts a world-space position to grid tile coordinates,
// mirroring the server's TerrainGrid.WorldToGrid floor division.
func worldToGridPos(p [2]float64) [2]float64 {
	cs := 20.0
	if t := getTerrain(); t != nil && t.CellSize > 0 {
		cs = t.CellSize
	}
	return [2]float64{math.Floor(p[0] / cs), math.Floor(p[1] / cs)}
}

func parsePos(v interface{}) [2]float64 {
	switch p := v.(type) {
	case []interface{}:
		if len(p) >= 2 {
			x, _ := p[0].(float64)
			y, _ := p[1].(float64)
			return [2]float64{x, y}
		}
	case [2]float64:
		return p
	}
	return [2]float64{0, 0}
}

// === Target Selection ===

func closest(pos [2]float64, enemies []entity) (*entity, float64) {
	var b *entity
	bd := math.Inf(1)
	for i := range enemies {
		d := tacticalTravelDistance(pos, enemies[i].Position)
		if d < bd {
			bd = d
			b = &enemies[i]
		}
	}
	return b, bd
}

func closestVisible(pos [2]float64, enemies []entity) (*entity, float64) {
	var b *entity
	bd := math.Inf(1)
	for i := range enemies {
		if !enemies[i].HasLOS {
			continue
		}
		d := tacticalTravelDistance(pos, enemies[i].Position)
		if d < bd {
			bd = d
			b = &enemies[i]
		}
	}
	return b, bd
}

func countVisibleEnemies(enemies []entity) int {
	count := 0
	for _, e := range enemies {
		if e.HasLOS {
			count++
		}
	}
	return count
}

func hasVisibleRangedThreat(enemies []entity) bool {
	for _, e := range enemies {
		if !e.HasLOS {
			continue
		}
		if e.Weapon == "bow" || e.Weapon == "staff" {
			return true
		}
	}
	return false
}

// teamTargetBonus coordinates team-mode demo bots without sharing any state
// beyond the public nearby-entity protocol. Focus fire finishes targets before
// they can heal, while a smaller protection bonus peels enemies off a wounded
// ally. In FFA ts.Allies is empty, so targeting is unchanged.
func teamTargetBonus(ts *tickState, enemyID string) float64 {
	if ts == nil || enemyID == "" {
		return 0
	}
	bonus := 0.0
	for i := range ts.Allies {
		ally := &ts.Allies[i]
		if ally.TargetID == enemyID {
			bonus += 32
		}
		if ally.MaxHP > 0 && ally.HP/ally.MaxHP <= 0.4 {
			for j := range ts.Enemies {
				if ts.Enemies[j].ID == enemyID && ts.Enemies[j].TargetID == ally.ID {
					bonus += 24
					break
				}
			}
		}
	}
	return math.Min(bonus, 96)
}

// bestTarget picks the optimal attack target in weapon range, weighting
// low HP, stuns, proximity, threat score, bounty targets, and (in CTF)
// enemy flag carriers.
func bestTarget(ts *tickState, pos [2]float64, enemies []entity, wrange float64) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if e.Dodging {
			continue
		}
		if !e.HasLOS {
			continue
		}
		d := chebyshev(pos, e.Position)
		if d > wrange {
			continue
		}
		score := 100 - e.HP
		if e.Stunned {
			score += 50
		}
		score -= d * 5
		if e.HP < e.MaxHP*0.3 {
			score += 40
		}
		score += e.ThreatScore * 0.3
		if e.Type == "bounty_target" || (ts.BountyTargetID != "" && e.ID == ts.BountyTargetID) {
			score += 120
		}
		if ts.Mode == "ctf" && ts.isFlagCarrier(e.ID) {
			score += 80
		}
		score += teamTargetBonus(ts, e.ID)
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

func isRearArc(attackerPos [2]float64, target entity) bool {
	fx, fy := target.Facing[0], target.Facing[1]
	if math.Abs(fx)+math.Abs(fy) < 0.01 {
		return false
	}
	dx := attackerPos[0] - target.Position[0]
	dy := attackerPos[1] - target.Position[1]
	dist := math.Hypot(dx, dy)
	if dist < 0.01 {
		return false
	}
	dx /= dist
	dy /= dist
	dot := fx*dx + fy*dy
	return dot <= -0.35
}

func bestBackstabTarget(pos [2]float64, enemies []entity, wrange float64) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if !e.IsAlive || !e.HasLOS || e.Dodging {
			continue
		}
		d := chebyshev(pos, e.Position)
		if d > wrange {
			continue
		}
		score := 100 - e.HP - d*4
		if e.RearExposed || isRearArc(pos, *e) {
			score += 65
		}
		if e.Stunned || e.DisruptedTicks > 0 {
			score += 30
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

func daggerFlankPosition(ts tickState, target *entity) ([2]float64, bool) {
	if target == nil {
		return [2]float64{}, false
	}
	fx, fy := fsign(target.Facing[0]), fsign(target.Facing[1])
	if fx == 0 && fy == 0 {
		return [2]float64{}, false
	}
	behind := [2]float64{target.Position[0] - fx, target.Position[1] - fy}
	c, r := int(math.Round(behind[0])), int(math.Round(behind[1]))
	if terrainBlocked(c, r) || ts.Danger.has(c, r) {
		return [2]float64{}, false
	}
	return behind, true
}

func bestShieldBashTarget(pos [2]float64, enemies []entity, wrange float64) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if !e.IsAlive || !e.HasLOS || e.Dodging {
			continue
		}
		d := chebyshev(pos, e.Position)
		if d > wrange {
			continue
		}
		score := 100 - e.HP - d*5
		if e.DisruptedTicks > 0 || e.Stunned {
			score += 80
		}
		if e.Weapon == "bow" || e.Weapon == "staff" {
			score += 12
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

func terrainBlocked(col, row int) bool {
	t := getTerrain()
	if t == nil {
		return false
	}
	return t.isBlocked(col, row)
}

// gridLineBlocked reports whether any wall/void cell lies on the grid line
// from a to b (start cell excluded). Bot-side approximation of the server's
// LOS check whose result arrives as has_los on nearby views; used for cells
// where we have no server-provided answer (e.g. candidate cover cells).
func (t *botTerrain) gridLineBlocked(a, b [2]int) bool {
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
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
		if t.isBlocked(x0, y0) && !(x0 == x1 && y0 == y1) {
			return true
		}
	}
}

// strongestRangedThreat returns the visible bow/staff enemy with the highest
// threat score (falling back to HP when the server didn't send one).
func strongestRangedThreat(ts *tickState) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range ts.Enemies {
		e := &ts.Enemies[i]
		if !e.HasLOS || !e.IsAlive {
			continue
		}
		if e.Weapon != "bow" && e.Weapon != "staff" {
			continue
		}
		score := e.ThreatScore
		if score <= 0 {
			score = e.HP
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

// findLOSBreakCell samples the 8 neighbors and the 16 cells at Chebyshev
// radius 2 around pos and returns the first passable, non-dangerous cell
// where terrain blocks the line to the threat. Used to duck out of ranged
// fire instead of trading into it.
func findLOSBreakCell(pos, threatPos [2]float64, danger *dangerSet) ([2]float64, bool) {
	t := getTerrain()
	if t == nil {
		return [2]float64{}, false
	}
	cx, cy := int(math.Round(pos[0])), int(math.Round(pos[1]))
	tc := [2]int{int(math.Round(threatPos[0])), int(math.Round(threatPos[1]))}
	for r := 1; r <= 2; r++ {
		for dx := -r; dx <= r; dx++ {
			for dy := -r; dy <= r; dy++ {
				if dx != -r && dx != r && dy != -r && dy != r {
					continue // ring cells only
				}
				c, w := cx+dx, cy+dy
				if t.isBlocked(c, w) || danger.has(c, w) {
					continue
				}
				if t.gridLineBlocked([2]int{c, w}, tc) {
					return [2]float64{float64(c), float64(w)}, true
				}
			}
		}
	}
	return [2]float64{}, false
}

func nearImpactSurface(pos [2]float64) bool {
	col, row := int(math.Round(pos[0])), int(math.Round(pos[1]))
	if terrainBlocked(col-1, row) || terrainBlocked(col+1, row) || terrainBlocked(col, row-1) || terrainBlocked(col, row+1) {
		return true
	}
	return false
}

func bestGrappleSlamTarget(pos [2]float64, enemies []entity, wrange float64) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if !e.IsAlive || !e.HasLOS || e.Dodging {
			continue
		}
		d := chebyshev(pos, e.Position)
		if d < 3 || d > wrange {
			continue
		}
		if !(e.NearImpactSurface || nearImpactSurface(e.Position)) {
			continue
		}
		score := 100 - e.HP - d*4
		if e.Weapon == "bow" || e.Weapon == "staff" {
			score += 18
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

func shouldUseChargedBow(ts tickState, target *entity, dist, wrange float64) bool {
	if target == nil || ts.BowChargeTicks <= 0 {
		return false
	}
	if target.Stunned {
		return true
	}
	if ts.ChargedShotReady && dist >= math.Max(4, wrange-2) {
		return true
	}
	if ts.BowChargeTicks >= 4 && (target.Weapon == "staff" || target.Weapon == "bow") {
		return true
	}
	return ts.BowChargeTicks >= 5
}

func shouldHoldBowCharge(ts tickState, target *entity, dist, wrange float64) bool {
	if target == nil || !target.HasLOS {
		return false
	}
	if ts.ChargedShotReady {
		return false
	}
	if ts.BowChargeTicks >= config.C.BowChargeReadyTicks {
		return false
	}
	if dist <= 2 {
		return false
	}
	if enemiesWithinRange(ts.Position, ts.Enemies, 2) >= 2 {
		return false
	}
	if target.Stunned {
		return true
	}
	if target.Weapon == "bow" || target.Weapon == "staff" {
		return true
	}
	return dist >= math.Max(4, wrange-2)
}

func visibleEnemiesInRange(pos [2]float64, enemies []entity, wrange float64) []entity {
	filtered := make([]entity, 0, len(enemies))
	for _, e := range enemies {
		if !e.IsAlive || !e.HasLOS || e.Dodging {
			continue
		}
		if chebyshev(pos, e.Position) > wrange {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// weakest returns the enemy with the lowest HP.
func weakest(pos [2]float64, enemies []entity) (*entity, float64) {
	var best *entity
	bestHP := math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if !e.HasLOS {
			continue
		}
		hp := e.HP
		if e.Stunned {
			hp -= 50
		}
		if hp < bestHP {
			bestHP = hp
			best = e
		}
	}
	if best == nil {
		return nil, 0
	}
	return best, tacticalTravelDistance(pos, best.Position)
}

// isMelee returns true if the weapon is short range.
func isMelee(weapon string) bool {
	return weapon == "sword" || weapon == "daggers" || weapon == "shield" || weapon == "spear" || weapon == "grapple"
}

// === Pickup Logic ===

// nearestHealthPickup returns the closest health_pack pickup.
func nearestHealthPickup(pos [2]float64, pickups, hazards []entity, hazardImmune bool) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pickups {
		if pickups[i].SubType != "health_pack" {
			continue
		}
		if pickupBlockedByActiveHazard(pickups[i].Position, hazards, hazardImmune) {
			continue
		}
		d := tacticalTravelDistance(pos, pickups[i].Position)
		if d < bestD {
			bestD = d
			best = &pickups[i]
		}
	}
	return best, bestD
}

// nearestPickup returns the closest pickup of any type.
func nearestPickup(pos [2]float64, pickups, hazards []entity, hazardImmune bool) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pickups {
		if pickupBlockedByActiveHazard(pickups[i].Position, hazards, hazardImmune) {
			continue
		}
		d := tacticalTravelDistance(pos, pickups[i].Position)
		if d < bestD {
			bestD = d
			best = &pickups[i]
		}
	}
	return best, bestD
}

// nearestPickupOfType returns the closest pickup of a specific subtype.
func nearestPickupOfType(pos [2]float64, pickups, hazards []entity, hazardImmune bool, subType string) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pickups {
		if pickups[i].SubType != subType {
			continue
		}
		if pickupBlockedByActiveHazard(pickups[i].Position, hazards, hazardImmune) {
			continue
		}
		d := tacticalTravelDistance(pos, pickups[i].Position)
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
	if heldPad != nil && !heldPad.Contested && pressure <= 1 && hpRatio >= 0.45 {
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

// === CTF Objective Logic ===

// tryCTFObjective implements capture-the-flag play: carriers run the flag
// home, defenders hunt enemy carriers, dropped flags get returned, and the
// faster personalities go for steals. Returns nil outside CTF rounds or when
// combat should take over.
func tryCTFObjective(ts tickState, strategy string, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) *actionResult {
	if ts.Mode != "ctf" || len(ts.Flags) == 0 || ts.Team == 0 {
		return nil
	}
	pos := ts.Position
	hpRatio := ts.HP / math.Max(ts.MaxHP, 1)

	var myFlag *entity    // my team's flag
	var carrying *entity  // the flag I am carrying (an enemy team's)
	var enemyFlag *entity // nearest enemy flag
	for i := range ts.Flags {
		f := &ts.Flags[i]
		if f.CarrierID == botID {
			carrying = f
		}
		if f.Team == ts.Team {
			myFlag = f
		} else if enemyFlag == nil || chebyshev(pos, f.Position) < chebyshev(pos, enemyFlag.Position) {
			enemyFlag = f
		}
	}

	// 1. Carrying a flag: run it home (danger-aware). Still trade with a
	// directly adjacent enemy when the weapon is ready, and dodge when hit.
	if carrying != nil && myFlag != nil {
		if near != nil && nearD <= 1 && canAtk && wrange >= 1 && near.HasLOS && !near.Dodging {
			a := atk(near, weapon)
			return &a
		}
		if ts.HitsThisTick > 0 && canDodge && near != nil && nearD <= wrange+2 {
			a := dodgeSafe(ts, perpDir(gridDir(pos, near.Position)))
			return &a
		}
		a := moveTo(pos, myFlag.BasePosition, ts.Danger)
		return &a
	}

	// 2. An enemy carries MY team's flag: the carrier is the priority target.
	if myFlag != nil && myFlag.Status == "carried" && myFlag.CarrierID != "" {
		for i := range ts.Enemies {
			e := &ts.Enemies[i]
			if e.ID != myFlag.CarrierID {
				continue
			}
			if canAtk && chebyshev(pos, e.Position) <= wrange && e.HasLOS && !e.Dodging {
				a := atk(e, weapon)
				return &a
			}
			a := moveTo(pos, e.Position, ts.Danger)
			return &a
		}
		// Carrier outside fog: flag positions are global — give chase while
		// it isn't hopeless.
		if chebyshev(pos, myFlag.Position) <= 20 {
			a := moveTo(pos, myFlag.Position, ts.Danger)
			return &a
		}
	}

	// 3. My team's flag is dropped reasonably close: touch it to return it.
	if myFlag != nil && myFlag.Status == "dropped" && chebyshev(pos, myFlag.Position) < 25 {
		a := moveTo(pos, myFlag.Position, ts.Danger)
		return &a
	}

	// 4. Steal runs: mobile/aggressive personalities go for the enemy flag
	// when healthy and not already pinned in a knife fight; defensive and
	// territorial types keep holding mid instead.
	stealer := strategy == "aggressive" || strategy == "assassin" || strategy == "kite"
	if enemyFlag != nil && stealer && hpRatio > 0.5 && (near == nil || nearD > 2) &&
		(enemyFlag.Status == "at_base" || enemyFlag.Status == "dropped") {
		// Leave the run to a strictly closer visible ally so the whole team
		// doesn't abandon combat for the same flag.
		myD := chebyshev(pos, enemyFlag.Position)
		for i := range ts.Allies {
			if chebyshev(ts.Allies[i].Position, enemyFlag.Position) < myD-1 {
				return nil
			}
		}
		a := moveTo(pos, enemyFlag.Position, ts.Danger)
		return &a
	}
	return nil
}

// === Hazard Zone Helpers ===

// inHazardZone checks if a position is inside any active hazard zone.
// Hazard zones are rectangles (width/height in grid cells, mirroring the
// server's isBotInHazardZone: center cell ± integer half-extents); burn
// fields are radial. Inactive (pulsed-off) zones are safe.
func inHazardZone(pos [2]float64, hazards []entity) bool {
	cx, cy := int(math.Round(pos[0])), int(math.Round(pos[1]))
	for _, h := range hazards {
		if !h.Active {
			continue
		}
		if h.Width > 0 || h.Height > 0 {
			zc, zr := int(math.Round(h.Position[0])), int(math.Round(h.Position[1]))
			halfW, halfH := h.Width/2, h.Height/2
			if cx >= zc-halfW && cx <= zc+halfW && cy >= zr-halfH && cy <= zr+halfH {
				return true
			}
			continue
		}
		r := h.Radius
		if r <= 0 {
			r = 2 // default burn-field radius
		}
		if chebyshev(pos, h.Position) <= r {
			return true
		}
	}
	return false
}

func pickupBlockedByActiveHazard(pos [2]float64, hazards []entity, hazardImmune bool) bool {
	if hazardImmune {
		return false
	}
	return inHazardZone(pos, hazards)
}

// === Cluster Detection (for Staff AoE) ===

// enemyClusterCenter finds the centroid of the largest cluster of enemies
// within clusterRadius of each other. Returns centroid and count.
func enemyClusterCenter(enemies []entity, clusterRadius float64) ([2]float64, int) {
	if len(enemies) == 0 {
		return [2]float64{0, 0}, 0
	}

	bestCenter := enemies[0].Position
	bestCount := 1

	for i := range enemies {
		cx, cy := enemies[i].Position[0], enemies[i].Position[1]
		count := 0
		sx, sy := 0.0, 0.0
		for j := range enemies {
			if chebyshev(enemies[i].Position, enemies[j].Position) <= clusterRadius {
				count++
				sx += enemies[j].Position[0]
				sy += enemies[j].Position[1]
			}
		}
		if count > bestCount {
			bestCount = count
			bestCenter = [2]float64{sx / float64(count), sy / float64(count)}
			_ = cx
			_ = cy
		}
	}
	return bestCenter, bestCount
}

// enemiesWithinRange counts enemies within range of a position.
func enemiesWithinRange(pos [2]float64, enemies []entity, r float64) int {
	count := 0
	for _, e := range enemies {
		if chebyshev(pos, e.Position) <= r {
			count++
		}
	}
	return count
}

func bestStaffCast(ts *tickState, pos [2]float64, enemies []entity, wrange float64) (*actionResult, bool) {
	candidates := visibleEnemiesInRange(pos, enemies, wrange)
	if len(candidates) == 0 {
		return nil, false
	}

	// Cluster radius 2 matches the staff's server-side AoE radius
	// (weapon_balance.go GridParam: 2) so casts land on cells that actually
	// hit multiple bots — and pass finalizeWeaponAction's radius-2 check.
	clusterCenter, clusterCount := enemyClusterCenter(candidates, 2)
	if clusterCount >= 2 && chebyshev(pos, clusterCenter) <= wrange {
		a := atkPos(clusterCenter, "staff")
		return &a, true
	}

	target := bestTarget(ts, pos, candidates, wrange)
	if target != nil {
		a := atk(target, "staff")
		return &a, true
	}

	near, nearD := closestVisible(pos, candidates)
	if near != nil && nearD <= wrange {
		a := atk(near, "staff")
		return &a, true
	}

	return nil, false
}

func finalizeWeaponAction(ts tickState, weapon string, wrange float64, action actionResult) actionResult {
	if weapon == "bow" && action.Action == "attack" {
		var target *entity
		for i := range ts.Enemies {
			if ts.Enemies[i].ID == action.Target {
				target = &ts.Enemies[i]
				break
			}
		}
		if target != nil && shouldUseChargedBow(ts, target, chebyshev(ts.Position, target.Position), wrange) {
			return chargeAttack(action)
		}
		return action
	}
	if weapon != "staff" || action.Action != "attack" {
		return action
	}

	if action.TargetPosition != nil {
		castPos := *action.TargetPosition
		if chebyshev(ts.Position, castPos) <= wrange {
			candidates := visibleEnemiesInRange(ts.Position, ts.Enemies, wrange)
			if enemiesWithinRange(castPos, candidates, 2) > 0 {
				return action
			}
		}
	}

	if action.Target != "" {
		for i := range ts.Enemies {
			e := &ts.Enemies[i]
			if e.ID == action.Target && e.IsAlive && e.HasLOS && chebyshev(ts.Position, e.Position) <= wrange {
				return atk(e, "staff")
			}
		}
	}

	if best, ok := bestStaffCast(&ts, ts.Position, ts.Enemies, wrange); ok {
		return *best
	}

	near, _ := closestVisible(ts.Position, ts.Enemies)
	if near != nil {
		return moveTo(ts.Position, near.Position, ts.Danger)
	}
	return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
}

// tryImmediateAttack converts a ready, in-range hit before non-emergency
// utility. Previously sword, staff, bow, and ordinary grapple opportunities
// could be displaced by an adjacent boost pickup, mine, or offensive grapple,
// wasting the weapon cooldown window. Spear keeps its brace setup and bow
// keeps intentional charge holds in their weapon-specific branches.
func tryImmediateAttack(ts tickState, weapon string, wrange float64) *actionResult {
	if !ts.WeaponReady {
		return nil
	}
	if weapon == "spear" && !ts.BraceReady {
		return nil
	}
	if weapon == "staff" {
		if cast, ok := bestStaffCast(&ts, ts.Position, ts.Enemies, wrange); ok {
			return cast
		}
		return nil
	}

	target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
	if target == nil {
		return nil
	}
	if weapon == "bow" {
		dist := chebyshev(ts.Position, target.Position)
		if shouldHoldBowCharge(ts, target, dist, wrange) {
			return nil
		}
	}
	a := atk(target, weapon)
	return &a
}

// === Smart Pickup Prioritization ===

// trySmartPickup checks for high-value pickups and grabs them if worthwhile.
// Returns an action if a pickup should be grabbed, nil otherwise.
func trySmartPickup(ts tickState, strategy string, weapon string) *actionResult {
	pos := ts.Position
	hpRatio := ts.HP / ts.MaxHP
	visibleEnemies := countVisibleEnemies(ts.Enemies)
	rangedThreat := hasVisibleRangedThreat(ts.Enemies)
	pickupReachBonus := 0.0
	if ts.PickupSurge {
		pickupReachBonus = 2
	}

	if any, anyD := nearestPickup(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey); any != nil && anyD <= 1 && any.ID != "" {
		a := useItem(any.ID)
		return &a
	}

	// Gravity well: grab only if we do not already have a charge.
	gw, gwD := nearestPickupOfType(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, "gravity_well")
	if gw != nil && gwD <= 8+pickupReachBonus && ts.GravityWellCharge <= 0 {
		a := moveTo(pos, gw.Position, ts.Danger)
		return &a
	}

	// Cooldown shard: prioritize when a major combat tool is currently unavailable.
	cd, cdD := nearestPickupOfType(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, "cooldown_shard")
	if cd != nil && cdD <= 7+pickupReachBonus {
		if ts.Cooldown > 0 || ts.DodgeCool > 0 || ts.GrappleCooldown > 0 || ts.StunTicks > 0 || ts.FastZone || ts.DoubleBounty || ts.TeleportSurge {
			a := moveTo(pos, cd.Position, ts.Danger)
			return &a
		}
	}

	// Hazard key: strongest when hazards are relevant to the current route or objective.
	hk, hkD := nearestPickupOfType(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, "hazard_key")
	if hk != nil && hkD <= 8+pickupReachBonus && !ts.HasHazardKey {
		if pad, padD := nearestCapturePad(pos, ts.CapturePads); pad != nil && padD <= 9 && (pad.Contested || pad.ContenderCount > 0 || !pad.Ready || ts.HazardStorm) {
			a := moveTo(pos, hk.Position, ts.Danger)
			return &a
		}
		if visibleEnemies > 0 && (rangedThreat || ts.IsBountyTarget || hpRatio < 0.7 || ts.HazardStorm) {
			a := moveTo(pos, hk.Position, ts.Danger)
			return &a
		}
		if !ts.InZone || inHazardZone(ts.ZoneTargetCenter, ts.HazardZones) {
			a := moveTo(pos, hk.Position, ts.Danger)
			return &a
		}
	}

	// Relay battery: strongest when a capture pad is nearby, contested, or enemy-owned.
	rb, rbD := nearestPickupOfType(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, "relay_battery")
	if rb != nil && rbD <= 8+pickupReachBonus && !ts.HasRelayBattery {
		if pad, padD := nearestCapturePad(pos, ts.CapturePads); pad != nil && padD <= 10 &&
			(pad.Contested || pad.CapturingBotID != "" || pad.OwnerID != "" || !pad.Ready) {
			a := moveTo(pos, rb.Position, ts.Danger)
			return &a
		}
	}

	// Overdrive core: strongest swing pickup when a fight is imminent.
	od, odD := nearestPickupOfType(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, "overdrive_core")
	if od != nil && odD <= 8+pickupReachBonus && (visibleEnemies > 0 || ts.IsBountyTarget || ts.DoubleBounty || strategy == "aggressive" || strategy == "berserker" || strategy == "assassin") {
		a := moveTo(pos, od.Position, ts.Danger)
		return &a
	}

	// Grapple charge: lightweight utility, especially valuable to grapple users and ranged kiting bots.
	gc, gcD := nearestPickupOfType(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, "grapple_charge")
	if gc != nil && gcD <= 7+pickupReachBonus {
		if ts.GrappleCharges <= 0 || ts.GrappleCooldown > 0 || weapon == "grapple" || strategy == "kite" || strategy == "assassin" {
			a := moveTo(pos, gc.Position, ts.Danger)
			return &a
		}
	}

	// Damage boost: grab if there is a realistic fight to use it in.
	dmg, dmgD := nearestPickupOfType(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, "damage_boost")
	if dmg != nil && dmgD <= 6+pickupReachBonus && (visibleEnemies > 0 || strategy == "aggressive" || strategy == "assassin" || ts.DoubleBounty) {
		a := moveTo(pos, dmg.Position, ts.Danger)
		return &a
	}

	// Bounty token: worth contesting when we can realistically convert a fight soon.
	bt, btD := nearestPickupOfType(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, "bounty_token")
	if bt != nil && btD <= 7+pickupReachBonus && (visibleEnemies > 0 || ts.IsBountyTarget || strategy == "aggressive" || strategy == "assassin" || ts.DoubleBounty) {
		a := moveTo(pos, bt.Position, ts.Danger)
		return &a
	}

	// Speed boost: useful for mobility styles and zone recovery.
	spd, spdD := nearestPickupOfType(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, "speed_boost")
	if spd != nil && spdD <= 6+pickupReachBonus && (strategy == "assassin" || strategy == "kite" || !ts.InZone || ts.FastZone) {
		a := moveTo(pos, spd.Position, ts.Danger)
		return &a
	}

	// Shield bubble: grab aggressively when ranged LOS is on us.
	sb, sbD := nearestPickupOfType(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, "shield_bubble")
	if sb != nil && sbD <= 5+pickupReachBonus && (hpRatio < 0.9 || rangedThreat || ts.IsBountyTarget) {
		a := moveTo(pos, sb.Position, ts.Danger)
		return &a
	}

	// Health pack: be more willing to stabilize before losing initiative.
	hp, hpD := nearestHealthPickup(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey)
	if hp != nil && hpD <= 6+pickupReachBonus && hpRatio < 0.8 {
		a := moveTo(pos, hp.Position, ts.Danger)
		return &a
	}

	if any, anyD := nearestPickup(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey); any != nil && visibleEnemies == 0 && anyD <= 6+pickupReachBonus {
		a := moveTo(pos, any.Position, ts.Danger)
		return &a
	}

	return nil
}

// === Mine Placement Logic ===

// tryPlaceMine checks if the bot should place a mine this tick.
// Returns an action if a mine should be placed, nil otherwise.
func tryPlaceMine(ts tickState, botID string, near *entity, nearD float64) *actionResult {
	if ts.MineCount >= 3 {
		return nil
	}

	pos := ts.Position

	// Being chased: enemy within 3 tiles behind me → place mine
	if near != nil && nearD <= 3 && near.TargetID == botID {
		a := placeMine()
		return &a
	}

	// Near zone center with no enemies close → mine high-traffic area
	distToCenter := chebyshev(pos, ts.ZoneTargetCenter)
	if distToCenter <= 5 && (near == nil || nearD > 3) && rand.Float64() < 0.15 {
		a := placeMine()
		return &a
	}

	return nil
}

// === Gravity Well Logic ===

// tryGravityWell checks if the bot should deploy a gravity well.
func tryGravityWell(ts tickState, botID string) *actionResult {
	if ts.GravityWellCharge <= 0 && !getHasGravWell(botID) {
		return nil
	}

	// Deploy if 3+ enemies within 6 tiles
	nearby := enemiesWithinRange(ts.Position, ts.Enemies, 6)
	if nearby >= 3 {
		center, _ := enemyClusterCenter(ts.Enemies, 6)
		a := useGravityWell(center)
		return &a
	}
	return nil
}

// tryUniversalGrapple uses the global grapple ability to finish weak targets,
// disrupt ranged threats, or force fights when carrying a bounty.
func tryUniversalGrapple(ts tickState, weapon string, wrange float64) *actionResult {
	if ts.GrappleCharges <= 0 || ts.GrappleCooldown > 0 || len(ts.Enemies) == 0 {
		return nil
	}
	const grappleRange = 12.0 // matches config GrappleAbilityRangeTiles (ARENA_GRAPPLE_RANGE_TILES, default 12)

	var best *entity
	bestScore := -math.Inf(1)

	for i := range ts.Enemies {
		e := &ts.Enemies[i]
		if e.ID == "" || !e.IsAlive || !e.HasLOS {
			continue
		}
		d := chebyshev(ts.Position, e.Position)
		if d <= 1 || d > grappleRange {
			continue
		}

		enemyHPRatio := 1.0
		if e.MaxHP > 0 {
			enemyHPRatio = e.HP / math.Max(e.MaxHP, 1)
		}
		finisher := enemyHPRatio <= 0.2
		objectiveTarget := e.Type == "bounty_target" || ts.isFlagCarrier(e.ID)
		if weapon == "bow" || weapon == "staff" {
			// Pulling a healthy melee enemy onto a ranged bot destroys the
			// spacing advantage. Ranged bots reserve target grapples for a
			// finisher/objective or an out-of-range ranged duel.
			if d <= wrange && !finisher {
				continue
			}
			if isMelee(e.Weapon) && !finisher && !objectiveTarget {
				continue
			}
		}

		score := d
		if d > wrange {
			score += 25
		}
		if e.Type == "bounty_target" {
			score += 50
		}
		if e.Weapon == "bow" || e.Weapon == "staff" {
			score += 20
		}
		if e.Stunned {
			score -= 15
		}
		if e.MaxHP > 0 {
			score += (1 - enemyHPRatio) * 35
			if enemyHPRatio <= 0.40 {
				score += 25
			}
		}
		if ts.IsBountyTarget {
			score += 15
		}
		if isMelee(weapon) {
			score += d * 1.5
		}
		if d > 12 && e.Type != "bounty_target" {
			score -= 30
		}
		if weapon == "grapple" && d >= 2 && d <= 8 {
			score += 35
		}
		if isMelee(weapon) && d > wrange && d <= wrange+6 {
			score += 18
		}

		if score > bestScore {
			bestScore = score
			best = e
		}
	}

	if best == nil {
		return nil
	}

	dist := chebyshev(ts.Position, best.Position)
	bestHPRatio := 1.0
	if best.MaxHP > 0 {
		bestHPRatio = best.HP / math.Max(best.MaxHP, 1)
	}
	shouldGrapple := best.Type == "bounty_target" ||
		ts.isFlagCarrier(best.ID) ||
		ts.IsBountyTarget ||
		best.Weapon == "bow" ||
		best.Weapon == "staff" ||
		weapon == "grapple" ||
		bestHPRatio <= 0.45 ||
		(dist > wrange && dist <= wrange+4)
	if weapon == "bow" || weapon == "staff" {
		shouldGrapple = best.Type == "bounty_target" || ts.isFlagCarrier(best.ID) ||
			bestHPRatio <= 0.2 || ((best.Weapon == "bow" || best.Weapon == "staff") && dist > wrange)
	}
	if !shouldGrapple {
		return nil
	}

	a := grapple(best.ID)
	return &a
}

func anchorGrappleDestinationSafe(target [2]float64, pads map[string]entity) bool {
	collectRadius := config.C.TeleportCollectRadius
	if collectRadius < 0 {
		collectRadius = 0
	}
	for _, pad := range pads {
		if isReadyTeleporter(pad) && chebyshev(target, pad.Position) <= float64(collectRadius) {
			return false
		}
	}
	return true
}

func clampAnchorToTerrain(target [2]float64) [2]float64 {
	// Anchor grapples use grid coordinates. Emergency escape vectors can point
	// beyond an edge, but the public action protocol intentionally rejects
	// negative coordinates. Clamp to the known map so the escape stays valid
	// and still moves as far away from the threat as the arena permits.
	target[0] = math.Max(0, target[0])
	target[1] = math.Max(0, target[1])
	if terrain := getTerrain(); terrain != nil {
		target[0] = math.Min(target[0], float64(max(0, terrain.Width-1)))
		target[1] = math.Min(target[1], float64(max(0, terrain.Height-1)))
	}
	return target
}

func tryAnchorGrapple(ts tickState, strategy string, near *entity, nearD, wrange float64) *actionResult {
	if ts.GrappleCharges <= 0 || ts.GrappleCooldown > 0 {
		return nil
	}
	const grappleRange = 12.0 // matches config GrappleAbilityRangeTiles (ARENA_GRAPPLE_RANGE_TILES, default 12)
	pads := teleporterByID(ts.Teleporters)

	hpRatio := ts.HP / math.Max(ts.MaxHP, 1)
	if !ts.InZone {
		dist := chebyshev(ts.Position, ts.ZoneTargetCenter)
		if dist >= 5 && dist <= grappleRange && (near == nil || nearD > 3) &&
			anchorGrappleDestinationSafe(ts.ZoneTargetCenter, pads) {
			a := grapplePos(ts.ZoneTargetCenter)
			return &a
		}
	}

	if near != nil && hpRatio < 0.45 {
		away := clampAnchorToTerrain([2]float64{
			ts.Position[0] + (ts.Position[0]-near.Position[0])*4,
			ts.Position[1] + (ts.Position[1]-near.Position[1])*4,
		})
		if chebyshev(ts.Position, away) <= grappleRange && anchorGrappleDestinationSafe(away, pads) {
			a := grapplePos(away)
			return &a
		}
	}

	if strategy == "kite" && near != nil && nearD > wrange+2 && nearD <= grappleRange {
		offset := gridDirAway(near.Position, ts.Position)
		anchor := clampAnchorToTerrain([2]float64{
			near.Position[0] + offset[0]*2,
			near.Position[1] + offset[1]*2,
		})
		if chebyshev(ts.Position, anchor) <= grappleRange && anchorGrappleDestinationSafe(anchor, pads) {
			a := grapplePos(anchor)
			return &a
		}
	}
	return nil
}

func teleporterByID(teleporters []entity) map[string]entity {
	pads := make(map[string]entity, len(teleporters)+6)
	if terrain := getTerrain(); terrain != nil {
		for id, tp := range terrain.Teleporters {
			pads[id] = tp
		}
	}
	// Live nearby entities override the static map snapshot, especially for
	// readiness. The linked exit may remain outside fog and come only from the
	// map cache.
	for _, tp := range teleporters {
		if tp.ID != "" {
			pads[tp.ID] = tp
		}
	}
	return pads
}

func isReadyTeleporter(tp entity) bool {
	return tp.Ready
}

func preferredGridDirections(from, target [2]int) [8][2]int {
	all := [8][2]int{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}}
	var ordered [8][2]int
	count := 0
	add := func(dir [2]int) {
		if dir == [2]int{} {
			return
		}
		for i := 0; i < count; i++ {
			if ordered[i] == dir {
				return
			}
		}
		ordered[count] = dir
		count++
	}

	dx, dy := intSign(target[0]-from[0]), intSign(target[1]-from[1])
	add([2]int{dx, dy})
	add([2]int{dx, 0})
	add([2]int{0, dy})
	for _, dir := range all {
		add(dir)
	}
	return ordered
}

func teleporterFirstMoveIsSafe(ts tickState, padCell [2]int, allowRadius int, dir [2]int) bool {
	terrain := getTerrain()
	if terrain == nil {
		return false
	}
	col, row := int(math.Round(ts.Position[0])), int(math.Round(ts.Position[1]))
	moved := false
	for step := 0; step < maxMoveCellsPerTick(ts); step++ {
		if terrain.isMoveBlocked(col, row, dir[0], dir[1]) {
			break
		}
		col += dir[0]
		row += dir[1]
		moved = true
		if ts.Danger.blocksExceptPad(col, row, padCell, allowRadius) {
			return false
		}
	}
	return moved
}

// teleporterSourceApproach finds a short, genuinely traversable route into a
// source pad's trigger footprint. Unlike tactical target ranking, this search
// respects live lethal danger. It opens only the selected pad's soft avoidance
// cells, never lethal cells, and validates the full first boosted move.
func teleporterSourceApproach(ts tickState, pad entity) (actionResult, float64, bool) {
	terrain := getTerrain()
	if terrain == nil {
		return actionResult{}, 0, false
	}
	start := [2]int{int(math.Round(ts.Position[0])), int(math.Round(ts.Position[1]))}
	padCell := [2]int{int(math.Round(pad.Position[0])), int(math.Round(pad.Position[1]))}
	if terrain.isBlocked(start[0], start[1]) || terrain.isBlocked(padCell[0], padCell[1]) {
		return actionResult{}, 0, false
	}
	collectRadius := config.C.TeleportCollectRadius
	if collectRadius < 0 {
		collectRadius = 0
	}
	allowRadius := teleporterAvoidanceRadius(ts)
	goal := func(col, row int) bool {
		return intChebyshev([2]int{col, row}, padCell) <= collectRadius &&
			!ts.Danger.blocksExceptPad(col, row, padCell, allowRadius)
	}
	if goal(start[0], start[1]) {
		return idle(), 0, true
	}

	const maxApproachDistance = 3
	scratch := bfsPool.Get().(*bfsScratch)
	defer bfsPool.Put(scratch)
	scratch.reset(terrain.Width, terrain.Height)
	scratch.visit(start[0], start[1])

	for _, dir := range preferredGridDirections(start, padCell) {
		if terrain.isMoveBlocked(start[0], start[1], dir[0], dir[1]) ||
			ts.Danger.blocksExceptPad(start[0]+dir[0], start[1]+dir[1], padCell, allowRadius) ||
			!teleporterFirstMoveIsSafe(ts, padCell, allowRadius, dir) {
			continue
		}
		col, row := start[0]+dir[0], start[1]+dir[1]
		if !scratch.visit(col, row) {
			continue
		}
		node := bfsNode{col: col, row: row, firstDC: dir[0], firstDR: dir[1], distance: 1}
		if goal(col, row) {
			a := moveDir([2]float64{float64(dir[0]), float64(dir[1])})
			return a, 1, true
		}
		scratch.queue = append(scratch.queue, node)
	}

	for head := 0; head < len(scratch.queue); head++ {
		node := scratch.queue[head]
		if node.distance >= maxApproachDistance {
			continue
		}
		for _, dir := range preferredGridDirections([2]int{node.col, node.row}, padCell) {
			if terrain.isMoveBlocked(node.col, node.row, dir[0], dir[1]) {
				continue
			}
			col, row := node.col+dir[0], node.row+dir[1]
			if ts.Danger.blocksExceptPad(col, row, padCell, allowRadius) || !scratch.visit(col, row) {
				continue
			}
			distance := node.distance + 1
			if goal(col, row) {
				a := moveDir([2]float64{float64(node.firstDC), float64(node.firstDR)})
				return a, float64(distance), true
			}
			scratch.queue = append(scratch.queue, bfsNode{
				col: col, row: row, firstDC: node.firstDC, firstDR: node.firstDR, distance: distance,
			})
		}
	}
	return actionResult{}, 0, false
}

func strategicMineTile(pos [2]float64, zoneCenter [2]float64) bool {
	t := getTerrain()
	if t == nil {
		return chebyshev(pos, zoneCenter) <= 4
	}

	cell := [2]int{int(math.Round(pos[0])), int(math.Round(pos[1]))}
	open := 0
	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for _, d := range dirs {
		if !t.isMoveBlocked(cell[0], cell[1], d[0], d[1]) {
			open++
		}
	}
	return open <= 2 || chebyshev(pos, zoneCenter) <= 4
}

func knownStaticHazardAt(pos [2]float64) bool {
	terrain := getTerrain()
	if terrain == nil {
		return false
	}
	for _, h := range terrain.HazardZones {
		if h.Width > 0 || h.Height > 0 {
			halfW, halfH := h.Width/2, h.Height/2
			if math.Abs(pos[0]-h.Position[0]) <= float64(halfW) && math.Abs(pos[1]-h.Position[1]) <= float64(halfH) {
				return true
			}
			continue
		}
		radius := h.Radius
		if radius <= 0 {
			radius = 2
		}
		if chebyshev(pos, h.Position) <= radius {
			return true
		}
	}
	return false
}

func nearestEnemyDistanceFrom(pos [2]float64, enemies []entity) float64 {
	best := math.Inf(1)
	for i := range enemies {
		if !enemies[i].IsAlive {
			continue
		}
		if d := chebyshev(pos, enemies[i].Position); d < best {
			best = d
		}
	}
	return best
}

func teleportExitIsSafe(ts tickState, exit entity, currentEnemyDistance float64) bool {
	terrain := getTerrain()
	if terrain == nil {
		return false
	}
	exitCell := [2]int{int(math.Round(exit.Position[0])), int(math.Round(exit.Position[1]))}
	if terrain.isBlocked(exitCell[0], exitCell[1]) || knownStaticHazardAt(exit.Position) {
		return false
	}
	// Treat all currently-known dynamic danger as unsafe too. Far exits cannot
	// expose every mine/burn field through fog, so the static map check above is
	// intentionally conservative about pulsing hazard zones.
	if inHazardZone(exit.Position, ts.HazardZones) {
		return false
	}
	if ts.Danger != nil && ts.Danger.hasLethal(exitCell[0], exitCell[1]) {
		return false
	}
	if chebyshev(exit.Position, ts.ZoneCenter) > math.Max(0, ts.ZoneRadius-1) {
		return false
	}
	exitEnemyDistance := nearestEnemyDistanceFrom(exit.Position, ts.Enemies)
	if !math.IsInf(currentEnemyDistance, 1) && exitEnemyDistance < currentEnemyDistance-1 {
		return false
	}
	return true
}

// tryTeleporterPressureEscape spends a teleporter charge only for an imminent
// survival problem and only when the linked exit is known and demonstrably
// safer. Normal engagement and convenience shortcuts deliberately avoid pads.
func tryTeleporterPressureEscape(ts tickState, strategy string, near *entity, nearD float64) *actionResult {
	_ = strategy
	if len(ts.Teleporters) == 0 {
		return nil
	}

	hpRatio := ts.HP / math.Max(ts.MaxHP, 1)
	closePressure := enemiesWithinRange(ts.Position, ts.Enemies, 3)
	criticalCombat := near != nil && ((hpRatio <= 0.25 && nearD <= 3) || (hpRatio <= 0.40 && closePressure >= 2))
	criticalZone := !ts.InZone && ts.ZoneDist <= -2
	if !criticalCombat && !criticalZone {
		return nil
	}
	if hp, hpD := nearestHealthPickup(ts.Position, ts.Pickups, ts.HazardZones, ts.HasHazardKey); hp != nil && hpD <= 3 {
		return nil
	}

	pads := teleporterByID(ts.Teleporters)
	currentEnemyDistance := nearestEnemyDistanceFrom(ts.Position, ts.Enemies)
	currentZoneDistance := chebyshev(ts.Position, ts.ZoneTargetCenter)
	bestScore := -math.Inf(1)
	var bestAction actionResult
	found := false

	for i := range ts.Teleporters {
		tp := &ts.Teleporters[i]
		if !isReadyTeleporter(*tp) || tp.LinkedID == "" {
			continue
		}
		approach, dToPad, reachable := teleporterSourceApproach(ts, *tp)
		if !reachable {
			continue
		}
		linked, ok := pads[tp.LinkedID]
		if !ok || !teleportExitIsSafe(ts, linked, currentEnemyDistance) {
			continue
		}

		exitEnemyDistance := nearestEnemyDistanceFrom(linked.Position, ts.Enemies)
		exitZoneDistance := chebyshev(linked.Position, ts.ZoneTargetCenter)
		combatImprovement := 0.0
		if !math.IsInf(currentEnemyDistance, 1) {
			combatImprovement = exitEnemyDistance - currentEnemyDistance
		}
		zoneImprovement := currentZoneDistance - exitZoneDistance
		if criticalCombat && combatImprovement < 2 {
			continue
		}
		if criticalZone && zoneImprovement < 2 {
			continue
		}
		score := combatImprovement*4 + zoneImprovement - dToPad
		if score > bestScore {
			bestScore = score
			bestAction = approach
			found = true
		}
	}
	if !found {
		return nil
	}
	return &bestAction
}

// tryPlaceMineAdvanced places mines more proactively in hot lanes and while
// retreating, using the server-provided mine counts instead of local guesses.
func tryPlaceMineAdvanced(ts tickState, strategy, weapon string, near *entity, nearD float64) *actionResult {
	if ts.MineCount >= 3 || ts.NearbyMines >= 2 {
		return nil
	}

	pos := ts.Position
	hpRatio := ts.HP / math.Max(ts.MaxHP, 1)
	distToCenter := chebyshev(pos, ts.ZoneTargetCenter)
	pressure := enemiesWithinRange(pos, ts.Enemies, 4)
	earlyRound := ts.RoundTick < 35
	onStrategicTile := strategicMineTile(pos, ts.ZoneTargetCenter)

	if near != nil && nearD <= 2 && (strategy == "territorial" || strategy == "kite" || hpRatio < 0.7) {
		a := placeMine()
		return &a
	}

	for _, tp := range ts.Teleporters {
		if !isReadyTeleporter(tp) {
			continue
		}
		if chebyshev(pos, tp.Position) <= 1 && (near != nil && nearD <= 4 || pressure >= 2) {
			a := placeMine()
			return &a
		}
	}

	if earlyRound && pressure == 0 && !onStrategicTile && !ts.TeleportSurge {
		return nil
	}
	if earlyRound && near == nil && distToCenter > 4 {
		return nil
	}

	if onStrategicTile && distToCenter <= 6 && pressure >= 1 && rand.Float64() < 0.65 {
		a := placeMine()
		return &a
	}
	if distToCenter <= 3 && (strategy == "territorial" || weapon == "shield") && (near == nil || nearD > 1) && pressure >= 1 && rand.Float64() < 0.45 {
		a := placeMine()
		return &a
	}
	if distToCenter <= 5 && onStrategicTile && (near == nil || nearD > 2) && pressure >= 1 && rand.Float64() < 0.25 {
		a := placeMine()
		return &a
	}
	if ts.IsBountyTarget && onStrategicTile && pressure >= 2 && rand.Float64() < 0.5 {
		a := placeMine()
		return &a
	}
	return nil
}

// === Main AI ===

// PickAction decides the bot's action for this tick.
// attackRange is the Chebyshev grid range from the server's loadout_confirmed.
func PickAction(strategy string, msg map[string]interface{}, weapon string, attackRange int, botID string) actionResult {
	ts := parseTick(msg)

	// Per-tick danger set: cells movement must route around (pooled — demo
	// bots pick actions concurrently).
	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	buildDangerSet(danger, &ts, botID)
	ts.Danger = danger

	pos := ts.Position
	hpRatio := ts.HP / ts.MaxHP
	wrange := float64(attackRange)
	if wrange <= 0 {
		wrange = WeaponRanges[weapon]
	}
	canAtk := ts.WeaponReady
	canDodge := ts.DodgeCool <= 0
	visibleEnemies := countVisibleEnemies(ts.Enemies)
	needsDefensiveDisengage := visibleEnemies > 0 &&
		(hpRatio < 0.3 || (hpRatio < 0.45 && wrange <= 2 && hasVisibleRangedThreat(ts.Enemies)))
	near, nearD := closestVisible(pos, ts.Enemies)
	if near == nil {
		near, nearD = closest(pos, ts.Enemies)
	}

	// Stunned — can't act
	if ts.StunTicks > 0 {
		return idle()
	}

	// === SUDDEN DEATH: step off/away from void tiles immediately ===
	// Void tiles are already in the danger set, so normal pathing avoids
	// them; this branch handles standing on (or right next to) one.
	if ts.SuddenDeath && len(ts.VoidTiles) > 0 {
		cell := [2]int{int(math.Round(pos[0])), int(math.Round(pos[1]))}
		minVoid := math.MaxInt32
		for _, vt := range ts.VoidTiles {
			d := intChebyshev(cell, vt)
			if d < minVoid {
				minVoid = d
			}
		}
		if minVoid <= 1 {
			dir := safeStepDir(pos, gridDir(pos, ts.ZoneTargetCenter), ts.Danger)
			if minVoid == 0 && canDodge {
				return dodgeSafe(ts, dir)
			}
			return moveDir(dir)
		}
	}

	// === DANGER: leave the shortest safe way immediately. This covers active
	// hazards, enemy gravity wells, armed mines, and a void tile underfoot.
	cell := [2]int{int(math.Round(pos[0])), int(math.Round(pos[1]))}
	if ts.Danger.hasLethal(cell[0], cell[1]) {
		return escapeDanger(ts, canDodge)
	}

	// Picking up an adjacent health pack is immediate and cannot be deferred
	// behind mines, grapples, or objective movement. The old ordering could
	// leave a critically wounded bot idling/moving on top of a heal while it
	// spent the tick on an offensive utility action.
	if hpRatio < 0.65 {
		if hp, hpD := nearestHealthPickup(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey); hp != nil && hpD <= 1 && hp.ID != "" {
			return useItem(hp.ID)
		}
	}

	// During the sudden-death stall penalty, passive play damages everyone and
	// only renewed combat stops the ramp. Healthy demo bots therefore force
	// contact instead of hiding; critically wounded bots keep survival logic.
	if ts.SuddenDeathStall && hpRatio >= 0.3 {
		if near != nil {
			if canAtk && near.HasLOS && nearD <= wrange {
				return finalizeWeaponAction(ts, weapon, wrange, atk(near, weapon))
			}
			return finalizeWeaponAction(ts, weapon, wrange, chaseApproach(ts, near, wrange, weapon))
		}
		for _, hint := range ts.Hints {
			if hint.HintType == "bot" {
				target := [2]float64{pos[0] + hint.Direction[0]*hint.Distance, pos[1] + hint.Direction[1]*hint.Distance}
				return moveTo(pos, target, ts.Danger)
			}
		}
		return moveTo(pos, ts.ZoneCenter, ts.Danger)
	}

	// === CTF OBJECTIVE PLAY: carry, chase carriers, return, steal ===
	if ctf := tryCTFObjective(ts, strategy, near, nearD, wrange, weapon, canAtk, canDodge, botID); ctf != nil {
		return *ctf
	}

	isAggStrat := strategy == "aggressive" || strategy == "berserker" || strategy == "assassin"
	canShv := ts.ShoveCool <= 0

	if ts.DoubleBounty {
		for i := range ts.Enemies {
			target := &ts.Enemies[i]
			if target.Type != "bounty_target" || !target.IsAlive {
				continue
			}
			d := chebyshev(pos, target.Position)
			if canAtk && d <= wrange {
				return finalizeWeaponAction(ts, weapon, wrange, atk(target, weapon))
			}
			if d <= wrange+4 {
				return moveTo(pos, target.Position, ts.Danger)
			}
		}
	}

	if weapon == "shield" {
		if canAtk {
			if target := bestShieldBashTarget(pos, ts.Enemies, wrange); target != nil {
				return finalizeWeaponAction(ts, weapon, wrange, atk(target, weapon))
			}
		}
		if near != nil && nearD <= 1 && canShv && near.DisruptedTicks <= 0 && !near.Stunned {
			return shove(near.ID)
		}
	}

	if weapon == "daggers" && canAtk {
		if target := bestBackstabTarget(pos, ts.Enemies, wrange); target != nil {
			return finalizeWeaponAction(ts, weapon, wrange, atk(target, weapon))
		}
	}

	if weapon == "spear" {
		if canAtk && near != nil && nearD <= wrange && ts.BraceReady {
			return finalizeWeaponAction(ts, weapon, wrange, atk(near, weapon))
		}
		if canAtk && near != nil && near.HasLOS && nearD >= 2 && nearD <= wrange && !ts.BraceReady {
			enemyRange := WeaponRanges[near.Weapon]
			if enemyRange <= 0 {
				enemyRange = 1
			}
			// Spend a safe beat bracing when the opponent cannot answer at
			// the current distance; the next thrust gains the spear's damage
			// and knockback bonus instead of throwing the signature away.
			if !near.CanAttack || nearD > enemyRange {
				return idle()
			}
		}
		if near != nil && nearD > wrange && nearD <= wrange+1 && !ts.BraceReady &&
			(strategy == "territorial" || strategy == "defensive") {
			return idle()
		}
	}

	if weapon == "grapple" && canAtk {
		if target := bestGrappleSlamTarget(pos, ts.Enemies, wrange); target != nil {
			return finalizeWeaponAction(ts, weapon, wrange, atk(target, weapon))
		}
	}

	if weapon == "bow" && canAtk {
		target := bestTarget(&ts, pos, ts.Enemies, wrange)
		if target != nil {
			dist := chebyshev(pos, target.Position)
			if shouldHoldBowCharge(ts, target, dist, wrange) {
				if near != nil && nearD <= 2 && canDodge {
					return dodgeSafe(ts, gridDirAway(ts.Position, near.Position))
				}
				return moveDirSafe(ts, perpDir(gridDir(ts.Position, target.Position)))
			}
		}
	}

	// Preserve a grapple charge for survival: escaping pressure must happen
	// before offensive target grapples, otherwise low-HP bots can pull the
	// threat they meant to flee directly onto themselves.
	if near != nil && hpRatio < 0.45 {
		if ga := tryAnchorGrapple(ts, strategy, near, nearD, wrange); ga != nil {
			return *ga
		}
	}

	// A ready hit in range beats optional utility. Emergency health, CTF,
	// weapon setup, and low-HP escape above still retain higher priority.
	if !needsDefensiveDisengage {
		if attack := tryImmediateAttack(ts, weapon, wrange); attack != nil {
			return finalizeWeaponAction(ts, weapon, wrange, *attack)
		}
	}

	// === GRAVITY WELL: Deploy if 3+ enemies nearby ===
	if gw := tryGravityWell(ts, botID); gw != nil {
		setHasGravWell(botID, false)
		return *gw
	}

	// === UNIVERSAL GRAPPLE: Pull high-value targets into kill range ===
	if gp := tryUniversalGrapple(ts, weapon, wrange); gp != nil {
		return *gp
	}

	// === ANCHOR GRAPPLE: use the hook for repositioning, not just target pulls ===
	if ga := tryAnchorGrapple(ts, strategy, near, nearD, wrange); ga != nil {
		return *ga
	}

	// === TELEPORTER ESCAPE: spend the charge only for verified critical safety ===
	if tp := tryTeleporterPressureEscape(ts, strategy, near, nearD); tp != nil {
		return *tp
	}

	// === MINE PLACEMENT ===
	if mine := tryPlaceMineAdvanced(ts, strategy, weapon, near, nearD); mine != nil {
		return *mine
	}

	// === DODGE CHARGED ATTACKS: sidestep ready bow shots and braced spears ===
	// Skipped when we can land our own hit this tick — trading beats juking.
	if canDodge && !(canAtk && near != nil && nearD <= wrange) {
		for i := range ts.Enemies {
			e := &ts.Enemies[i]
			if !e.HasLOS || !e.IsAlive {
				continue
			}
			charged := (e.Weapon == "bow" && e.ChargedShotReady) ||
				(e.Weapon == "spear" && e.BraceReady)
			if !charged {
				continue
			}
			if chebyshev(pos, e.Position) <= WeaponRanges[e.Weapon]+1 {
				return dodgeSafe(ts, perpDir(gridDir(pos, e.Position)))
			}
		}
	}

	// === REACT: Got hit — only kite/defensive dodge; aggressive types fight back ===
	if ts.HitsThisTick > 0 && canDodge && near != nil && nearD <= wrange+3 {
		if !isAggStrat {
			return dodgeSafe(ts, perpDir(gridDir(pos, near.Position)))
		}
		// Aggressive bots: shove the attacker instead of dodging
		if canShv && nearD <= 1 {
			return shove(near.ID)
		}
	}

	// === FLEE: Assassins disengage at 20%, others at 15% ===
	fleeThreshold := 0.15
	if strategy == "assassin" {
		fleeThreshold = 0.20
	}
	if strategy != "berserker" && strategy != "territorial" && hpRatio < fleeThreshold && near != nil && nearD <= 2 {
		// Grab adjacent health pickup if available
		hp, hpD := nearestHealthPickup(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey)
		if hp != nil && hpD <= 1 {
			return useItem(hp.ID)
		}
		if canDodge {
			return dodgeSafe(ts, gridDirAway(pos, near.Position))
		}
	}

	// === DISENGAGE & BREAK LOS: melee bots under ranged fire at <45% HP,
	// anyone at <30% — duck behind terrain or grab a closer health pack.
	if needsDefensiveDisengage {
		threat := strongestRangedThreat(&ts)
		if threat == nil {
			threat = near
		}
		if threat != nil {
			// A health pickup closer than the threat beats hiding.
			if hp, hpD := nearestHealthPickup(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey); hp != nil && hpD < chebyshev(pos, threat.Position) {
				if hpD <= 1 {
					return useItem(hp.ID)
				}
				return moveTo(pos, hp.Position, ts.Danger)
			}
			if cover, ok := findLOSBreakCell(pos, threat.Position, ts.Danger); ok {
				return moveTo(pos, cover, ts.Danger)
			}
			return moveDirSafe(ts, gridDirAway(pos, threat.Position))
		}
	}

	// === ZONE SAFETY: Move into safe zone, but attack on the way ===
	if !ts.InZone {
		// Zone edge tactics: shove enemies OUT of zone
		if near != nil && nearD <= 1 && canShv {
			// If enemy is between us and zone edge, shove them further out
			return shove(near.ID)
		}
		if near != nil && nearD <= wrange && canAtk {
			return finalizeWeaponAction(ts, weapon, wrange, atk(near, weapon))
		}
		return moveTo(pos, ts.ZoneCenter, ts.Danger)
	}

	if (ts.FastZone || ts.HazardStorm) && ts.ZoneDist <= 3 && near == nil {
		return moveTo(pos, ts.ZoneTargetCenter, ts.Danger)
	}

	// === ZONE EDGE TACTICS: Shove enemies out of zone ===
	if ts.ZoneDist <= 3 && near != nil && nearD <= 1 && canShv {
		// Check if enemy is between us and zone edge → shove them OUT
		enemyZoneDist := chebyshev(near.Position, ts.ZoneCenter) - ts.ZoneRadius
		if enemyZoneDist > -2 { // enemy near zone edge
			return shove(near.ID)
		}
	}

	// === PROACTIVE ZONE DRIFT: reposition ahead of the shrink when the zone
	// edge is close (or the round is late and we're far from the next zone)
	// and no enemy is within fighting distance. If carved terrain blocks the
	// direct route, start sooner so rings, rooms, islands, and caves do not
	// strand the bot behind a long detour during the shrink.
	if near == nil || nearD > wrange+2 {
		distToZoneTarget := chebyshev(pos, ts.ZoneTargetCenter)
		driftMargin := 4.0
		lateTick := 1200
		if terrain := getTerrain(); terrain != nil {
			start := [2]int{int(math.Round(pos[0])), int(math.Round(pos[1]))}
			goal := [2]int{int(math.Round(ts.ZoneTargetCenter[0])), int(math.Round(ts.ZoneTargetCenter[1]))}
			if terrain.gridLineBlocked(start, goal) {
				driftMargin = 7
				lateTick = 900
			}
		}
		lateRound := ts.RoundTick > lateTick
		if ts.ZoneDist < driftMargin || (lateRound && distToZoneTarget > ts.ZoneTargetRadius) {
			if a := moveTo(pos, ts.ZoneTargetCenter, ts.Danger); a.Action != "idle" {
				return a
			}
		}
	}

	// === OBJECTIVE PLAY: contest or claim the capture pad when the fight allows it ===
	if pad := tryCapturePadObjective(ts, strategy, near, nearD, botID); pad != nil {
		return *pad
	}

	// === SMART PICKUPS ===
	if pickup := trySmartPickup(ts, strategy, weapon); pickup != nil {
		return *pickup
	}

	// === ANTI-BOUNTY AWARENESS (kill_streak >= 3) ===
	// Adjustments are handled within each strategy below

	// === NO ENEMIES VISIBLE: Hunt them down ===
	// (trySmartPickup already ran unconditionally above.)
	if visibleEnemies == 0 {
		for _, h := range ts.Hints {
			if h.HintType == "pickup" {
				target := [2]float64{
					pos[0] + h.Direction[0]*math.Min(h.Distance, 6),
					pos[1] + h.Direction[1]*math.Min(h.Distance, 6),
				}
				return moveTo(pos, target, ts.Danger)
			}
		}
		for _, h := range ts.Hints {
			if h.HintType == "bot" {
				target := [2]float64{pos[0] + h.Direction[0]*h.Distance, pos[1] + h.Direction[1]*h.Distance}
				return moveTo(pos, target, ts.Danger)
			}
		}
		p, pd := nearestPickup(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey)
		if p != nil && pd <= 3 {
			return moveTo(pos, p.Position, ts.Danger)
		}
		return moveTo(pos, ts.ZoneTargetCenter, ts.Danger)
	}

	// === COMBAT: Strategy-specific ===
	switch strategy {
	case "aggressive":
		return finalizeWeaponAction(ts, weapon, wrange, aiAggressive(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	case "berserker":
		return finalizeWeaponAction(ts, weapon, wrange, aiBerserker(ts, near, nearD, wrange, weapon, canAtk, canDodge))
	case "kite":
		return finalizeWeaponAction(ts, weapon, wrange, aiKite(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	case "assassin":
		return finalizeWeaponAction(ts, weapon, wrange, aiAssassin(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	case "defensive":
		return finalizeWeaponAction(ts, weapon, wrange, aiDefensive(ts, near, nearD, wrange, weapon, canAtk, canDodge))
	case "territorial":
		return finalizeWeaponAction(ts, weapon, wrange, aiTerritorial(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	default:
		return finalizeWeaponAction(ts, weapon, wrange, aiAggressive(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	}
}

// chaseApproach closes distance on a target. Melee bots approaching a ranged
// enemy zigzag deterministically (tick parity) between the direct BFS step
// and a perpendicular offset, making charged shots harder to line up.
func chaseApproach(ts tickState, target *entity, wrange float64, weapon string) actionResult {
	dist := chebyshev(ts.Position, target.Position)
	if !isMelee(weapon) || dist <= wrange || (target.Weapon != "bow" && target.Weapon != "staff") {
		return moveTo(ts.Position, target.Position, ts.Danger)
	}
	dir := bfsDir(ts.Position, target.Position, ts.Danger)
	if dir[0] == 0 && dir[1] == 0 {
		return idle()
	}
	// Offset perpendicular on alternating tick pairs while still far out.
	if dist > 2 && (ts.Tick/2)%2 == 1 {
		perp := [2]float64{-dir[1], dir[0]} // fixed CW perpendicular — deterministic
		px, py := int(perp[0]), int(perp[1])
		cx, cy := int(math.Round(ts.Position[0])), int(math.Round(ts.Position[1]))
		t := getTerrain()
		if (px != 0 || py != 0) && (t == nil || !t.isMoveBlocked(cx, cy, px, py)) && !ts.Danger.has(cx+px, cy+py) {
			return moveDir(perp)
		}
	}
	return moveDir(dir)
}

// baitPunish handles the adjacent stand-off: our weapon is cooling down while
// the enemy's is ready — dodging (or shoving) beats strafing into the swing.
func baitPunish(ts tickState, near *entity, canDodge bool) *actionResult {
	if near == nil || !near.CanAttack || !isMelee(near.Weapon) {
		return nil
	}
	if canShv := ts.ShoveCool <= 0; canShv {
		a := shove(near.ID)
		return &a
	}
	if canDodge && ts.DodgeCool <= 0 {
		a := dodgeSafe(ts, perpDir(gridDir(ts.Position, near.Position)))
		return &a
	}
	return nil
}

// AGGRESSIVE: Rush enemies, attack on cooldown, shove when close, chase relentlessly.
// Used by Lancers (spear) — knockback into walls for bonus damage.
func aiAggressive(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
	}
	canShv := ts.ShoveCool <= 0

	// Attack best target in range
	target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	// Adjacent on cooldown — shove to stun, then burst next tick
	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	// Close on cooldown — advance to stay in melee
	if nearD <= wrange && !canAtk {
		if nearD > 1 {
			return moveTo(ts.Position, near.Position, ts.Danger)
		}
		// Adjacent with their weapon ready and ours cooling — don't stand in the swing.
		if p := baitPunish(ts, near, canDodge); p != nil {
			return *p
		}
		return moveDirSafe(ts, perpDir(gridDir(ts.Position, near.Position)))
	}

	// Chase — zigzag against ranged kiters so shots are harder to line up
	return chaseApproach(ts, near, wrange, weapon)
}

// BERSERKER: Never retreat, dodge INTO enemies, shove constantly, fight to the death.
func aiBerserker(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
	}
	canShv := ts.ShoveCool <= 0

	if nearD <= wrange && canAtk {
		target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
		if target != nil {
			return atk(target, weapon)
		}
		return atk(near, weapon)
	}

	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	if nearD > 1 && nearD <= wrange+3 && canDodge {
		return dodgeSafe(ts, gridDir(ts.Position, near.Position))
	}

	// Chase — zigzag against ranged kiters so shots are harder to line up
	return chaseApproach(ts, near, wrange, weapon)
}

func preferredKiteRange(weapon string, wrange float64) float64 {
	switch weapon {
	case "bow":
		return math.Max(5, wrange-1.5)
	case "staff":
		return math.Max(4, wrange-1.5)
	default:
		return math.Max(3, wrange-1)
	}
}

// KITE: prioritize ranged spacing and staff clusters. Bow users hold a
// longer lane than staff users instead of sharing the old 3.5-tile sweet spot.
func aiKite(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
	}
	canShv := ts.ShoveCool <= 0
	isBounty := ts.KillStreak >= 3

	// Delayed staff impacts and bow projectiles are poor point-blank trades.
	// Create separation before committing the ranged attack.
	if (weapon == "bow" || weapon == "staff") && nearD <= 1 {
		if canShv {
			return shove(near.ID)
		}
		if canDodge {
			return dodgeSafe(ts, gridDirAway(ts.Position, near.Position))
		}
		return moveDirSafe(ts, gridDirAway(ts.Position, near.Position))
	}

	// Staff AoE: target cluster center instead of individual enemies
	if weapon == "staff" && canAtk {
		if cast, ok := bestStaffCast(&ts, ts.Position, ts.Enemies, wrange); ok {
			return *cast
		}
	} else if canAtk {
		// Non-staff kite weapons
		target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
		if target != nil {
			return atk(target, weapon)
		}
		if nearD <= wrange {
			return atk(near, weapon)
		}
	}

	// Enemy at range 1 — shove THEN dodge away
	if nearD <= 1 {
		if canShv {
			return shove(near.ID)
		}
		if canDodge {
			return dodgeSafe(ts, gridDirAway(ts.Position, near.Position))
		}
		return moveDirSafe(ts, gridDirAway(ts.Position, near.Position))
	}

	// Maintain a weapon-specific lane rather than letting bow users drift into
	// the staff's much shorter old sweet spot.
	idealRange := preferredKiteRange(weapon, wrange)
	if isBounty {
		idealRange = wrange - 0.5 // Play extra cautiously when bounty target
	}

	if nearD < idealRange-0.5 && !canAtk {
		// Too close — back off
		if canDodge {
			return dodgeSafe(ts, gridDirAway(ts.Position, near.Position))
		}
		return moveDirSafe(ts, gridDirAway(ts.Position, near.Position))
	}

	// On cooldown at range — strafe to be harder to hit
	if nearD <= wrange && !canAtk {
		return moveDirSafe(ts, perpDir(gridDir(ts.Position, near.Position)))
	}

	// Too far — approach to get in range
	if nearD > wrange {
		// Approach cluster if multiple enemies
		clusterCenter, clusterCount := enemyClusterCenter(ts.Enemies, 3)
		if clusterCount >= 2 {
			return moveTo(ts.Position, clusterCenter, ts.Danger)
		}
		return moveTo(ts.Position, near.Position, ts.Danger)
	}

	return moveTo(ts.Position, near.Position, ts.Danger)
}

// ASSASSIN: Hunt weakest target, grapple for gap-close, shove + burst, disengage at 20%.
// Used by Hooks (grapple) and Shredders (daggers).
func aiAssassin(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	canShv := ts.ShoveCool <= 0
	isBounty := ts.KillStreak >= 3

	// Find weakest enemy (always hunt the lowest HP target)
	prey, preyD := weakest(ts.Position, ts.Enemies)
	if prey == nil {
		if near != nil {
			prey = near
			preyD = nearD
		} else {
			return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
		}
	}

	// Bounty target — grab health packs more aggressively
	if isBounty {
		hp, hpD := nearestHealthPickup(ts.Position, ts.Pickups, ts.HazardZones, ts.HasHazardKey)
		if hp != nil && hpD <= 3 && ts.HP/ts.MaxHP < 0.6 {
			if hpD <= 1 {
				return useItem(hp.ID)
			}
			return moveTo(ts.Position, hp.Position, ts.Danger)
		}
	}

	// Grapple weapon: attack at range 3-4 to pull toward target, then shove when adjacent
	if weapon == "grapple" {
		// In grapple range — attack (server handles the pull)
		if preyD <= wrange && canAtk {
			return atk(prey, weapon)
		}
		// Adjacent after grapple — shove for stun, then burst next tick
		if preyD <= 1 && canShv {
			return shove(prey.ID)
		}
	} else if weapon == "daggers" {
		// Daggers: dodge INTO target for gap close, then shove + burst
		if preyD <= wrange && canAtk {
			return atk(prey, weapon)
		}
		if !canAtk && preyD <= 3 && !prey.RearExposed && !isRearArc(ts.Position, *prey) {
			if flank, ok := daggerFlankPosition(ts, prey); ok && chebyshev(ts.Position, flank) > 0 {
				return moveTo(ts.Position, flank, ts.Danger)
			}
		}
		if preyD <= 1 && canShv {
			return shove(prey.ID)
		}
		// Gap close with dodge toward prey
		if preyD > 1 && preyD <= 3 && canDodge {
			return dodgeSafe(ts, gridDir(ts.Position, prey.Position))
		}
	} else {
		// Generic assassin behavior
		if preyD <= wrange && canAtk {
			return atk(prey, weapon)
		}
		if preyD <= 1 && canShv {
			return shove(prey.ID)
		}
	}

	// Close — dodge IN to gap-close for the kill
	if preyD > 1 && preyD <= wrange+2 && canDodge {
		return dodgeSafe(ts, gridDir(ts.Position, prey.Position))
	}

	// Hunt them down
	return moveTo(ts.Position, prey.Position, ts.Danger)
}

// DEFENSIVE: Counter-attack focused, shove intruders, hold ground but fight.
func aiDefensive(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		d := chebyshev(ts.Position, ts.ZoneTargetCenter)
		if d > 5 {
			return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
		}
		return idle()
	}
	canShv := ts.ShoveCool <= 0

	target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	if nearD <= wrange+2 && !canAtk {
		if nearD > 1 {
			return moveTo(ts.Position, near.Position, ts.Danger)
		}
		// Adjacent with their weapon ready and ours cooling — don't stand in the swing.
		if p := baitPunish(ts, near, canDodge); p != nil {
			return *p
		}
		return moveDirSafe(ts, perpDir(gridDir(ts.Position, near.Position)))
	}

	if nearD <= wrange+5 {
		return moveTo(ts.Position, near.Position, ts.Danger)
	}

	return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
}

// TERRITORIAL: Hold zone TARGET center, shove EVERY adjacent enemy, place mines at center.
// Used by Juggernauts (shield) — 180 HP + 50% block = unkillable.
func aiTerritorial(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	canShv := ts.ShoveCool <= 0
	distToCenter := chebyshev(ts.Position, ts.ZoneTargetCenter)

	if near == nil {
		if distToCenter > 3 {
			return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
		}
		return idle()
	}

	// Priority 1: Shove EVERY adjacent enemy on cooldown (free stun + pushes them out)
	if nearD <= 1 && canShv {
		// Find the best shove target — prefer enemies near zone edge
		bestShove := near
		for i := range ts.Enemies {
			e := &ts.Enemies[i]
			if chebyshev(ts.Position, e.Position) <= 1 {
				// Prefer shoving enemies that are near the zone edge
				eDist := chebyshev(e.Position, ts.ZoneCenter) - ts.ZoneRadius
				nDist := chebyshev(bestShove.Position, ts.ZoneCenter) - ts.ZoneRadius
				if eDist > nDist {
					bestShove = e
				}
			}
		}
		return shove(bestShove.ID)
	}

	// Priority 2: Attack anything in range (alternate between shove and attack on different targets)
	target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	// Priority 3: Chase enemies that enter territory, but NEVER more than 5 tiles from center
	if nearD <= wrange+4 && distToCenter <= 5 {
		return moveTo(ts.Position, near.Position, ts.Danger)
	}

	// Priority 4: Return to zone TARGET center (anticipate shrink)
	if distToCenter > 2 {
		return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
	}

	// At center, no targets in range — idle
	return idle()
}
