package demobots

import (
	"math"
	"math/rand"
	"sync"
)

// === Types ===

type tickState struct {
	Tick             int
	Position         [2]float64
	HP               float64
	MaxHP            float64
	WeaponReady      bool
	Cooldown         float64
	DodgeCool        int
	InvulnTicks      int
	StunTicks        int
	ShoveCool        float64
	ShieldHP         float64
	InZone           bool
	ZoneDist         float64
	ZoneCenter       [2]float64
	ZoneRadius       float64
	ZoneTargetCenter [2]float64
	ZoneTargetRadius float64
	KillStreak       int
	RoundKills       int
	HitsThisTick     int
	LastActionOK     bool
	HasSpeedBoost    bool
	HasDmgBoost      bool
	Enemies          []entity
	Pickups          []entity
	Teleporters      []entity
	HazardZones      []entity
	Hints            []hint
}

type entity struct {
	ID       string
	Type     string
	SubType  string
	Position [2]float64
	HP       float64
	MaxHP    float64
	Weapon   string
	IsAlive  bool
	Stunned  bool
	Dodging  bool
	TargetID string
	Radius   float64
}

type hint struct {
	HintType   string
	Direction  [2]float64
	Distance   float64
	PickupType string
}

// botTerrain caches the map_init terrain grid for AI decisions.
type botTerrain struct {
	Width    int
	Height   int
	CellSize float64
	Cells    [][]byte // [col][row] matching server layout
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
	mineCount[botID] = 0
}

// === Gravity well tracking ===

var (
	hasGravWell   = make(map[string]bool) // botID -> has gravity well charge
	gravWellMu    sync.Mutex
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
	hasGravWell[botID] = false
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

	terrainMu.Lock()
	cachedTerrain = &botTerrain{Width: width, Height: height, CellSize: cs, Cells: cells}
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

// === BFS Pathfinding ===

type bfsNode struct {
	col, row       int
	firstDC, firstDR int
}

// bfsStep finds the first grid step direction from (sc,sr) toward (gc,gr), navigating walls.
// Returns [2]int{dx, dy} where dx,dy are -1, 0, or 1.
func bfsStep(sc, sr, gc, gr int) [2]int {
	if sc == gc && sr == gr {
		return [2]int{0, 0}
	}

	t := getTerrain()
	if t == nil {
		return [2]int{intSign(gc - sc), intSign(gr - sr)}
	}

	visited := make(map[[2]int]bool, 256)
	visited[[2]int{sc, sr}] = true
	queue := make([]bfsNode, 0, 128)

	// Seed with all passable neighbors (diagonal corner-cutting prevented).
	for dc := -1; dc <= 1; dc++ {
		for dr := -1; dr <= 1; dr++ {
			if dc == 0 && dr == 0 {
				continue
			}
			if t.isMoveBlocked(sc, sr, dc, dr) {
				continue
			}
			nc, nr := sc+dc, sr+dr
			visited[[2]int{nc, nr}] = true
			queue = append(queue, bfsNode{nc, nr, dc, dr})
		}
	}

	for i := 0; i < len(queue) && i < 200*9; i++ {
		n := queue[i]
		if n.col == gc && n.row == gr {
			return [2]int{n.firstDC, n.firstDR}
		}
		for dc := -1; dc <= 1; dc++ {
			for dr := -1; dr <= 1; dr++ {
				if dc == 0 && dr == 0 {
					continue
				}
				if t.isMoveBlocked(n.col, n.row, dc, dr) {
					continue
				}
				nc, nr := n.col+dc, n.row+dr
				key := [2]int{nc, nr}
				if !visited[key] {
					visited[key] = true
					queue = append(queue, bfsNode{nc, nr, n.firstDC, n.firstDR})
				}
			}
		}
	}

	// BFS exhausted — fall back to direct direction toward goal.
	direct := [2]int{intSign(gc - sc), intSign(gr - sr)}
	if !t.isMoveBlocked(sc, sr, direct[0], direct[1]) {
		return direct
	}
	if direct[0] != 0 && !t.isMoveBlocked(sc, sr, direct[0], 0) {
		return [2]int{direct[0], 0}
	}
	if direct[1] != 0 && !t.isMoveBlocked(sc, sr, 0, direct[1]) {
		return [2]int{0, direct[1]}
	}
	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	rand.Shuffle(len(dirs), func(i, j int) { dirs[i], dirs[j] = dirs[j], dirs[i] })
	for _, d := range dirs {
		if !t.isMoveBlocked(sc, sr, d[0], d[1]) {
			return d
		}
	}
	return [2]int{0, 0}
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

// bfsDir uses BFS to get the first step direction from src toward dst.
func bfsDir(src, dst [2]float64) [2]float64 {
	step := bfsStep(int(src[0]), int(src[1]), int(dst[0]), int(dst[1]))
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
}

func moveDir(d [2]float64) actionResult {
	snapped := [2]float64{fsign(d[0]), fsign(d[1])}
	return actionResult{Action: "move", Direction: &snapped}
}

// moveTo uses BFS pathfinding to take one step from src toward dst.
func moveTo(src, dst [2]float64) actionResult {
	d := bfsDir(src, dst)
	if d[0] == 0 && d[1] == 0 {
		return idle()
	}
	return actionResult{Action: "move", Direction: &d}
}

func atk(t *entity, weapon string) actionResult {
	a := actionResult{Action: "attack", Target: t.ID}
	if weapon == "staff" {
		pos := t.Position
		a.TargetPosition = &pos
	}
	return a
}

// atkPos attacks a position (for staff AoE targeting cluster centers).
func atkPos(pos [2]float64, weapon string) actionResult {
	a := actionResult{Action: "attack", TargetPosition: &pos}
	return a
}

func dodge(d [2]float64) actionResult {
	snapped := [2]float64{fsign(d[0]), fsign(d[1])}
	return actionResult{Action: "dodge", Direction: &snapped}
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

func useGravityWell(pos [2]float64) actionResult {
	return actionResult{Action: "use_gravity_well", TargetPosition: &pos}
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
	if ys, ok := msg["your_state"].(map[string]interface{}); ok {
		ts.Position = parsePos(ys["position"])
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
					}
				}
			}
		}
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
			if v, ok := e["pickup_type"].(string); ok {
				ent.SubType = v
			}
			if v, ok := e["radius"].(float64); ok {
				ent.Radius = v
			}
			switch ent.Type {
			case "bot":
				if ent.IsAlive {
					ts.Enemies = append(ts.Enemies, ent)
				}
			case "pickup":
				ts.Pickups = append(ts.Pickups, ent)
			case "teleporter":
				ts.Teleporters = append(ts.Teleporters, ent)
			case "hazard_zone":
				ts.HazardZones = append(ts.HazardZones, ent)
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
		d := chebyshev(pos, enemies[i].Position)
		if d < bd {
			bd = d
			b = &enemies[i]
		}
	}
	return b, bd
}

// bestTarget picks the optimal attack target in weapon range.
func bestTarget(pos [2]float64, enemies []entity, wrange float64) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if e.Dodging {
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
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best
}

// weakest returns the enemy with the lowest HP.
func weakest(pos [2]float64, enemies []entity) (*entity, float64) {
	var best *entity
	bestHP := math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
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
	return best, chebyshev(pos, best.Position)
}

// isMelee returns true if the weapon is short range.
func isMelee(weapon string) bool {
	return weapon == "sword" || weapon == "daggers" || weapon == "shield" || weapon == "spear" || weapon == "grapple"
}

// === Pickup Logic ===

// nearestHealthPickup returns the closest health_pack pickup.
func nearestHealthPickup(pos [2]float64, pickups []entity) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pickups {
		if pickups[i].SubType != "health_pack" {
			continue
		}
		d := chebyshev(pos, pickups[i].Position)
		if d < bestD {
			bestD = d
			best = &pickups[i]
		}
	}
	return best, bestD
}

// nearestPickup returns the closest pickup of any type.
func nearestPickup(pos [2]float64, pickups []entity) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pickups {
		d := chebyshev(pos, pickups[i].Position)
		if d < bestD {
			bestD = d
			best = &pickups[i]
		}
	}
	return best, bestD
}

// nearestPickupOfType returns the closest pickup of a specific subtype.
func nearestPickupOfType(pos [2]float64, pickups []entity, subType string) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pickups {
		if pickups[i].SubType != subType {
			continue
		}
		d := chebyshev(pos, pickups[i].Position)
		if d < bestD {
			bestD = d
			best = &pickups[i]
		}
	}
	return best, bestD
}

// === Hazard Zone Helpers ===

// inHazardZone checks if a position is inside any active hazard zone.
func inHazardZone(pos [2]float64, hazards []entity) bool {
	for _, h := range hazards {
		r := h.Radius
		if r <= 0 {
			r = 2 // default hazard radius
		}
		if chebyshev(pos, h.Position) <= r {
			return true
		}
	}
	return false
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

// === Smart Pickup Prioritization ===

// trySmartPickup checks for high-value pickups and grabs them if worthwhile.
// Returns an action if a pickup should be grabbed, nil otherwise.
func trySmartPickup(ts tickState, strategy string) *actionResult {
	pos := ts.Position
	hpRatio := ts.HP / ts.MaxHP

	// Gravity well: ALWAYS grab if within 5 tiles
	gw, gwD := nearestPickupOfType(pos, ts.Pickups, "gravity_well")
	if gw != nil && gwD <= 5 {
		a := moveTo(pos, gw.Position)
		return &a
	}

	// Damage boost: grab if within 3 tiles AND enemies visible
	dmg, dmgD := nearestPickupOfType(pos, ts.Pickups, "damage_boost")
	if dmg != nil && dmgD <= 3 && len(ts.Enemies) > 0 {
		a := moveTo(pos, dmg.Position)
		return &a
	}

	// Speed boost: grab if within 3 tiles AND playing assassin/kite
	spd, spdD := nearestPickupOfType(pos, ts.Pickups, "speed_boost")
	if spd != nil && spdD <= 3 && (strategy == "assassin" || strategy == "kite") {
		a := moveTo(pos, spd.Position)
		return &a
	}

	// Shield bubble: grab if within 3 tiles AND HP < 70%
	sb, sbD := nearestPickupOfType(pos, ts.Pickups, "shield_bubble")
	if sb != nil && sbD <= 3 && hpRatio < 0.70 {
		a := moveTo(pos, sb.Position)
		return &a
	}

	// Health pack: grab when HP < 50% and within 2 tiles
	hp, hpD := nearestHealthPickup(pos, ts.Pickups)
	if hp != nil && hpD <= 2 && hpRatio < 0.50 {
		a := moveTo(pos, hp.Position)
		return &a
	}

	return nil
}

// === Mine Placement Logic ===

// tryPlaceMine checks if the bot should place a mine this tick.
// Returns an action if a mine should be placed, nil otherwise.
func tryPlaceMine(ts tickState, botID string, near *entity, nearD float64) *actionResult {
	mines := getMineCount(botID)
	if mines >= 3 {
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
	if !getHasGravWell(botID) {
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

// === Teleporter Escape ===

// tryTeleporterEscape checks if the bot should escape via teleporter.
func tryTeleporterEscape(ts tickState) *actionResult {
	if ts.HP/ts.MaxHP > 0.25 {
		return nil
	}
	// Only if no health packs nearby
	_, hpD := nearestHealthPickup(ts.Position, ts.Pickups)
	if hpD <= 3 {
		return nil
	}
	// Check for teleporter within 2 tiles
	for _, tp := range ts.Teleporters {
		if chebyshev(ts.Position, tp.Position) <= 2 {
			a := moveTo(ts.Position, tp.Position)
			return &a
		}
	}
	return nil
}

// === Main AI ===

// PickAction decides the bot's action for this tick.
// attackRange is the Chebyshev grid range from the server's loadout_confirmed.
func PickAction(strategy string, msg map[string]interface{}, weapon string, attackRange int, botID string) actionResult {
	ts := parseTick(msg)
	pos := ts.Position
	hpRatio := ts.HP / ts.MaxHP
	wrange := float64(attackRange)
	if wrange <= 0 {
		wrange = WeaponRanges[weapon]
	}
	canAtk := ts.WeaponReady
	canDodge := ts.DodgeCool <= 0
	near, nearD := closest(pos, ts.Enemies)

	// Stunned — can't act
	if ts.StunTicks > 0 {
		return idle()
	}

	// === HAZARD ZONE: Get out immediately (highest priority after stun) ===
	if inHazardZone(pos, ts.HazardZones) {
		// Move away from nearest hazard center
		if len(ts.HazardZones) > 0 {
			nearest := ts.HazardZones[0]
			bestD := chebyshev(pos, nearest.Position)
			for _, h := range ts.HazardZones[1:] {
				d := chebyshev(pos, h.Position)
				if d < bestD {
					bestD = d
					nearest = h
				}
			}
			away := gridDirAway(pos, nearest.Position)
			if canDodge {
				return dodge(away)
			}
			return moveDir(away)
		}
	}

	isAggStrat := strategy == "aggressive" || strategy == "berserker" || strategy == "assassin"
	canShv := ts.ShoveCool <= 0

	// === GRAVITY WELL: Deploy if 3+ enemies nearby ===
	if gw := tryGravityWell(ts, botID); gw != nil {
		setHasGravWell(botID, false)
		return *gw
	}

	// === MINE PLACEMENT ===
	if mine := tryPlaceMine(ts, botID, near, nearD); mine != nil {
		incMineCount(botID)
		return *mine
	}

	// === REACT: Got hit — only kite/defensive dodge; aggressive types fight back ===
	if ts.HitsThisTick > 0 && canDodge && near != nil && nearD <= wrange+3 {
		if !isAggStrat {
			return dodge(perpDir(gridDir(pos, near.Position)))
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
		// Try teleporter escape
		if tp := tryTeleporterEscape(ts); tp != nil {
			return *tp
		}
		// Grab adjacent health pickup if available
		hp, hpD := nearestHealthPickup(pos, ts.Pickups)
		if hp != nil && hpD <= 1 {
			return moveTo(pos, hp.Position)
		}
		if canDodge {
			return dodge(gridDirAway(pos, near.Position))
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
			return atk(near, weapon)
		}
		return moveTo(pos, ts.ZoneCenter)
	}

	// === ZONE EDGE TACTICS: Shove enemies out of zone ===
	if ts.ZoneDist <= 3 && near != nil && nearD <= 1 && canShv {
		// Check if enemy is between us and zone edge → shove them OUT
		enemyZoneDist := chebyshev(near.Position, ts.ZoneCenter) - ts.ZoneRadius
		if enemyZoneDist > -2 { // enemy near zone edge
			return shove(near.ID)
		}
	}

	// === SMART PICKUPS ===
	if pickup := trySmartPickup(ts, strategy); pickup != nil {
		return *pickup
	}

	// === ANTI-BOUNTY AWARENESS (kill_streak >= 3) ===
	// Adjustments are handled within each strategy below

	// === NO ENEMIES VISIBLE: Hunt them down ===
	if len(ts.Enemies) == 0 {
		for _, h := range ts.Hints {
			if h.HintType == "bot" {
				target := [2]float64{pos[0] + h.Direction[0]*h.Distance, pos[1] + h.Direction[1]*h.Distance}
				return moveTo(pos, target)
			}
		}
		p, pd := nearestPickup(pos, ts.Pickups)
		if p != nil && pd <= 3 {
			return moveTo(pos, p.Position)
		}
		return moveTo(pos, ts.ZoneTargetCenter)
	}

	// === COMBAT: Strategy-specific ===
	switch strategy {
	case "aggressive":
		return aiAggressive(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID)
	case "berserker":
		return aiBerserker(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "kite":
		return aiKite(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID)
	case "assassin":
		return aiAssassin(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID)
	case "defensive":
		return aiDefensive(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "territorial":
		return aiTerritorial(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID)
	default:
		return aiAggressive(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID)
	}
}

// AGGRESSIVE: Rush enemies, attack on cooldown, shove when close, chase relentlessly.
// Used by Lancers (spear) — knockback into walls for bonus damage.
func aiAggressive(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter)
	}
	canShv := ts.ShoveCool <= 0

	// Attack best target in range
	target := bestTarget(ts.Position, ts.Enemies, wrange)
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
			return moveTo(ts.Position, near.Position)
		}
		return moveDir(perpDir(gridDir(ts.Position, near.Position)))
	}

	// Chase — but don't follow enemies outside the zone
	return moveTo(ts.Position, near.Position)
}

// BERSERKER: Never retreat, dodge INTO enemies, shove constantly, fight to the death.
func aiBerserker(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter)
	}
	canShv := ts.ShoveCool <= 0

	if nearD <= wrange && canAtk {
		target := bestTarget(ts.Position, ts.Enemies, wrange)
		if target != nil {
			return atk(target, weapon)
		}
		return atk(near, weapon)
	}

	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	if nearD > 1 && nearD <= wrange+3 && canDodge {
		return dodge(gridDir(ts.Position, near.Position))
	}

	return moveTo(ts.Position, near.Position)
}

// KITE: AoE kiting for Staff users. Prioritize enemy clusters, maintain 3-4 tile range.
func aiKite(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter)
	}
	canShv := ts.ShoveCool <= 0
	isBounty := ts.KillStreak >= 3

	// Staff AoE: target cluster center instead of individual enemies
	if weapon == "staff" && canAtk {
		clusterCenter, clusterCount := enemyClusterCenter(ts.Enemies, 3)
		clusterDist := chebyshev(ts.Position, clusterCenter)

		// If 2+ enemies are clustered and within range, AoE the cluster center
		if clusterCount >= 2 && clusterDist <= wrange {
			return atkPos(clusterCenter, weapon)
		}

		// Otherwise attack best single target
		target := bestTarget(ts.Position, ts.Enemies, wrange)
		if target != nil {
			return atk(target, weapon)
		}
		if nearD <= wrange {
			return atk(near, weapon)
		}
	} else if canAtk {
		// Non-staff kite weapons
		target := bestTarget(ts.Position, ts.Enemies, wrange)
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
			return dodge(gridDirAway(ts.Position, near.Position))
		}
		return moveDir(gridDirAway(ts.Position, near.Position))
	}

	// Maintain 3-4 tile distance (sweet spot for staff)
	idealRange := 3.5
	if isBounty {
		idealRange = wrange - 0.5 // Play extra cautiously when bounty target
	}

	if nearD < idealRange-0.5 && !canAtk {
		// Too close — back off
		if canDodge {
			return dodge(gridDirAway(ts.Position, near.Position))
		}
		return moveDir(gridDirAway(ts.Position, near.Position))
	}

	// On cooldown at range — strafe to be harder to hit
	if nearD <= wrange && !canAtk {
		return moveDir(perpDir(gridDir(ts.Position, near.Position)))
	}

	// Too far — approach to get in range
	if nearD > wrange {
		// Approach cluster if multiple enemies
		clusterCenter, clusterCount := enemyClusterCenter(ts.Enemies, 3)
		if clusterCount >= 2 {
			return moveTo(ts.Position, clusterCenter)
		}
		return moveTo(ts.Position, near.Position)
	}

	return moveTo(ts.Position, near.Position)
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
			return moveTo(ts.Position, ts.ZoneTargetCenter)
		}
	}

	// Bounty target — grab health packs more aggressively
	if isBounty {
		hp, hpD := nearestHealthPickup(ts.Position, ts.Pickups)
		if hp != nil && hpD <= 3 && ts.HP/ts.MaxHP < 0.6 {
			return moveTo(ts.Position, hp.Position)
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
		if preyD <= 1 && canShv {
			return shove(prey.ID)
		}
		// Gap close with dodge toward prey
		if preyD > 1 && preyD <= 3 && canDodge {
			return dodge(gridDir(ts.Position, prey.Position))
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
		return dodge(gridDir(ts.Position, prey.Position))
	}

	// Hunt them down
	return moveTo(ts.Position, prey.Position)
}

// DEFENSIVE: Counter-attack focused, shove intruders, hold ground but fight.
func aiDefensive(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		d := chebyshev(ts.Position, ts.ZoneTargetCenter)
		if d > 5 {
			return moveTo(ts.Position, ts.ZoneTargetCenter)
		}
		return idle()
	}
	canShv := ts.ShoveCool <= 0

	target := bestTarget(ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	if nearD <= wrange+2 && !canAtk {
		if nearD > 1 {
			return moveTo(ts.Position, near.Position)
		}
		return moveDir(perpDir(gridDir(ts.Position, near.Position)))
	}

	if nearD <= wrange+5 {
		return moveTo(ts.Position, near.Position)
	}

	return moveTo(ts.Position, ts.ZoneTargetCenter)
}

// TERRITORIAL: Hold zone TARGET center, shove EVERY adjacent enemy, place mines at center.
// Used by Juggernauts (shield) — 180 HP + 50% block = unkillable.
func aiTerritorial(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	canShv := ts.ShoveCool <= 0
	distToCenter := chebyshev(ts.Position, ts.ZoneTargetCenter)

	if near == nil {
		if distToCenter > 3 {
			return moveTo(ts.Position, ts.ZoneTargetCenter)
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
	target := bestTarget(ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	// Priority 3: Chase enemies that enter territory, but NEVER more than 5 tiles from center
	if nearD <= wrange+4 && distToCenter <= 5 {
		return moveTo(ts.Position, near.Position)
	}

	// Priority 4: Return to zone TARGET center (anticipate shrink)
	if distToCenter > 2 {
		return moveTo(ts.Position, ts.ZoneTargetCenter)
	}

	// At center, no targets in range — idle
	return idle()
}
