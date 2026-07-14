// Demo bot shared state: tick/entity types, the cached terrain grid,
// per-bot mine/gravity-well trackers, and the per-tick danger set.
package demobots

import (
	"math"
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
	cachedTerrain  *botTerrain
	cachedMapShape string
	terrainMu      sync.RWMutex
)

// getMapShape returns the current round's map shape name ("" when the map
// payload predates shape reporting). Shared across all demo bots like the
// cached terrain.
func getMapShape() string {
	terrainMu.RLock()
	shape := cachedMapShape
	terrainMu.RUnlock()
	return shape
}

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
			rowStr, ok := rowData.(string)
			if !ok {
				continue
			}
			for col := 0; col < len(rowStr) && col < width; col++ {
				cells[col][row] = rowStr[col]
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
	if shape, ok := msg["map_shape"].(string); ok && shape != "" {
		cachedMapShape = shape
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
