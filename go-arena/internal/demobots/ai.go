package demobots

import (
	"math"
	"math/rand"
	"sync"

	"arena-server/internal/game"
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
	IsBountyTarget   bool
	SuddenDeath      bool
	BountyTargetID   string
	MineCount        int
	GravityWellCharge int
	Enemies          []entity
	Pickups          []entity
	Hints            []hint
	TeleportPads     []entity
	HazardZones      []entity
	GravityWells     []entity
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
}

type hint struct {
	HintType   string
	Direction  [2]float64
	Distance   float64
	PickupType string
}

// botTerrain wraps game.ActiveTerrain for AI decisions.
// Previously parsed from map_init WebSocket messages; now reads directly
// from the engine's shared ActiveTerrain pointer (zero-copy, always in sync).
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

// syncTerrain reads game.ActiveTerrain and caches a local copy for AI use.
// Called at round_start instead of parsing map_init messages.
func syncTerrain() {
	t := game.ActiveTerrain
	if t == nil {
		return
	}
	terrainMu.Lock()
	cachedTerrain = &botTerrain{
		Width:    t.Width,
		Height:   t.Height,
		CellSize: t.CellSize,
		Cells:    t.Cells, // shared read-only slice — engine doesn't mutate mid-round
	}
	terrainMu.Unlock()
}

// parseTerrain is kept for backward compatibility but now just calls syncTerrain.
// The msg parameter is ignored.
func parseTerrain(_ map[string]interface{}) {
	syncTerrain()
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
	// This may walk into a wall occasionally, but the anti-stuck
	// mechanism will nudge the bot if it gets truly stuck.
	direct := [2]int{intSign(gc - sc), intSign(gr - sr)}
	if !t.isMoveBlocked(sc, sr, direct[0], direct[1]) {
		return direct
	}
	// Direct path blocked — try cardinal components separately
	if direct[0] != 0 && !t.isMoveBlocked(sc, sr, direct[0], 0) {
		return [2]int{direct[0], 0}
	}
	if direct[1] != 0 && !t.isMoveBlocked(sc, sr, 0, direct[1]) {
		return [2]int{0, direct[1]}
	}
	// Fully stuck — try any passable direction
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
			switch ent.Type {
			case "bot":
				if ent.IsAlive {
					ts.Enemies = append(ts.Enemies, ent)
				}
			case "pickup":
				ts.Pickups = append(ts.Pickups, ent)
			case "teleport_pad":
				ts.TeleportPads = append(ts.TeleportPads, ent)
			case "hazard_zone":
				ts.HazardZones = append(ts.HazardZones, ent)
			case "gravity_well":
				ts.GravityWells = append(ts.GravityWells, ent)
			case "bounty_target":
				ts.Enemies = append(ts.Enemies, ent)
			}
		}
	}
	// Parse new gameplay fields.
	if v, ok := msg["sudden_death"].(bool); ok {
		ts.SuddenDeath = v
	}
	if v, ok := msg["bounty_target"].(string); ok {
		ts.BountyTargetID = v
	}
	if ys, ok := msg["your_state"].(map[string]interface{}); ok {
		if v, ok := ys["is_bounty_target"].(bool); ok {
			ts.IsBountyTarget = v
		}
		if v, ok := ys["mine_count"].(float64); ok {
			ts.MineCount = int(v)
		}
		if v, ok := ys["gravity_well_charge"].(float64); ok {
			ts.GravityWellCharge = int(v)
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

// isInActiveHazard checks if a position is inside any active hazard zone.
func isInActiveHazard(pos [2]float64, hazards []entity) bool {
	for _, h := range hazards {
		if h.SubType != "" { // SubType could be used, but check "active" field
			continue
		}
		// Hazard zones have position — check if we're nearby (within ~2 tiles)
		d := chebyshev(pos, h.Position)
		if d <= 2 {
			return true
		}
	}
	return false
}

// nearestTeleportPad returns the nearest teleport pad.
func nearestTeleportPad(pos [2]float64, pads []entity) (*entity, float64) {
	var best *entity
	bestD := math.Inf(1)
	for i := range pads {
		d := chebyshev(pos, pads[i].Position)
		if d < bestD {
			bestD = d
			best = &pads[i]
		}
	}
	return best, bestD
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

// === Main AI ===

// PickAction decides the bot's action for this tick.
// attackRange is the Chebyshev grid range from the server's loadout_confirmed.
func PickAction(strategy string, msg map[string]interface{}, weapon string, attackRange int) actionResult {
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

	isAggStrat := strategy == "aggressive" || strategy == "berserker" || strategy == "assassin"
	canShv := ts.ShoveCool <= 0

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

	// === HAZARD AVOIDANCE: Move away from active hazard zones ===
	if isInActiveHazard(pos, ts.HazardZones) {
		// Try to move toward zone center (away from hazard)
		if near != nil && nearD <= wrange && canAtk {
			return atk(near, weapon) // attack on the way out
		}
		return moveTo(pos, ts.ZoneCenter)
	}

	// === TELEPORT: Use teleport pads when fleeing at low HP ===
	if strategy != "berserker" && hpRatio < 0.25 && near != nil && nearD <= 2 {
		tp, tpD := nearestTeleportPad(pos, ts.TeleportPads)
		if tp != nil && tpD <= 1 {
			return moveTo(pos, tp.Position) // step onto pad to escape
		}
	}

	// === BOUNTY DEFENSE: If we ARE the bounty, play more carefully ===
	if ts.IsBountyTarget && near != nil && nearD <= 2 && hpRatio < 0.4 && canDodge {
		return dodge(gridDirAway(pos, near.Position))
	}

	// === SUDDEN DEATH: Avoid void tiles by moving toward zone center ===
	if ts.SuddenDeath {
		t := getTerrain()
		if t != nil {
			col, row := int(pos[0]), int(pos[1])
			if col >= 0 && row >= 0 && col < t.Width && row < t.Height && t.Cells[col][row] == 'V' {
				// Standing on void — move to zone center immediately
				return moveTo(pos, ts.ZoneCenter)
			}
			// Check adjacent cells for void and prefer moving away
			voidCount := 0
			for dc := -1; dc <= 1; dc++ {
				for dr := -1; dr <= 1; dr++ {
					nc, nr := col+dc, row+dr
					if nc >= 0 && nr >= 0 && nc < t.Width && nr < t.Height && t.Cells[nc][nr] == 'V' {
						voidCount++
					}
				}
			}
			if voidCount >= 3 {
				return moveTo(pos, ts.ZoneCenter)
			}
		}
	}

	// === GRAVITY WELL: Deploy in enemy clusters (2+ enemies within 3 tiles) ===
	if ts.GravityWellCharge > 0 && len(ts.Enemies) >= 2 {
		// Find cluster center of nearby enemies
		clusterCount := 0
		var cx, cy float64
		for _, e := range ts.Enemies {
			if chebyshev(pos, e.Position) <= 5 {
				cx += e.Position[0]
				cy += e.Position[1]
				clusterCount++
			}
		}
		if clusterCount >= 2 {
			cx /= float64(clusterCount)
			cy /= float64(clusterCount)
			target := [2]float64{math.Round(cx), math.Round(cy)}
			return useGravityWell(target)
		}
	}

	// === LANDMINES: Place near teleport exits, health packs, or choke points ===
	if ts.MineCount < 3 && len(ts.Enemies) > 0 {
		// Place mine if near a teleport pad exit (enemies will teleport here)
		tp, tpD := nearestTeleportPad(pos, ts.TeleportPads)
		if tp != nil && tpD <= 1 {
			return placeMine()
		}
		// Place mine near health packs (enemies will come for them)
		hp, hpD := nearestHealthPickup(pos, ts.Pickups)
		if hp != nil && hpD <= 1 && hpRatio > 0.6 {
			return placeMine()
		}
	}

	// === GRAPPLE GAP-CLOSE: Pull self to ranged enemies ===
	if weapon == "grapple" && canAtk && near != nil && nearD > 1 && nearD <= wrange {
		// Prioritize ranged enemies to close the gap
		for i := range ts.Enemies {
			e := &ts.Enemies[i]
			d := chebyshev(pos, e.Position)
			if d > 1 && d <= wrange && (e.Weapon == "bow" || e.Weapon == "staff") {
				return atk(e, weapon)
			}
		}
	}

	// === FLEE: Only at very low HP, and never for berserkers ===
	if strategy != "berserker" && hpRatio < 0.15 && near != nil && nearD <= 2 {
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
		if near != nil && nearD <= wrange && canAtk {
			return atk(near, weapon)
		}
		if near != nil && nearD <= 1 && canShv {
			return shove(near.ID)
		}
		return moveTo(pos, ts.ZoneCenter)
	}

	// === PICKUP: Only grab health right next to us when hurt ===
	if hpRatio < 0.50 {
		hp, hpD := nearestHealthPickup(pos, ts.Pickups)
		if hp != nil && hpD <= 2 {
			return moveTo(pos, hp.Position)
		}
	}

	// === NO ENEMIES VISIBLE: Hunt them down (last bot standing wins!) ===
	if len(ts.Enemies) == 0 {
		// Follow bot hints first — these point toward distant enemies
		for _, h := range ts.Hints {
			if h.HintType == "bot" {
				target := [2]float64{pos[0] + h.Direction[0]*h.Distance, pos[1] + h.Direction[1]*h.Distance}
				return moveTo(pos, target)
			}
		}
		// Grab nearby pickup on the way
		p, pd := nearestPickup(pos, ts.Pickups)
		if p != nil && pd <= 3 {
			return moveTo(pos, p.Position)
		}
		// No hints — head to zone center where enemies converge
		// (zone shrinks over time, so center is where fights happen)
		return moveTo(pos, ts.ZoneTargetCenter)
	}

	// === COMBAT: Strategy-specific ===
	switch strategy {
	case "aggressive":
		return aiAggressive(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "berserker":
		return aiBerserker(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "kite":
		return aiKite(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "assassin":
		return aiAssassin(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "defensive":
		return aiDefensive(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "territorial":
		return aiTerritorial(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	default:
		return aiAggressive(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	}
}

// (zone awareness handled by the shared preamble — combat strategies fight freely)

// AGGRESSIVE: Rush enemies, attack on cooldown, shove when close, chase relentlessly.
func aiAggressive(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter)
	}
	canShv := ts.ShoveCool <= 0

	// Zone safety — don't chase enemies out of the zone
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

	// Chase — but don't follow enemies outside the zone (zone damage kills)
	return moveTo(ts.Position, near.Position)
}

// BERSERKER: Never retreat, dodge INTO enemies, shove constantly, fight to the death.
func aiBerserker(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter)
	}
	canShv := ts.ShoveCool <= 0

	// Even berserkers respect the zone — dying to zone damage isn't fighting
	// Attack anything in range
	if nearD <= wrange && canAtk {
		target := bestTarget(ts.Position, ts.Enemies, wrange)
		if target != nil {
			return atk(target, weapon)
		}
		return atk(near, weapon)
	}

	// Adjacent on cooldown — always shove
	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	// Gap close with dodge toward enemy
	if nearD > 1 && nearD <= wrange+3 && canDodge {
		return dodge(gridDir(ts.Position, near.Position))
	}

	// Chase relentlessly — but don't leave the zone
	return moveTo(ts.Position, near.Position)
}

// KITE: Stay at range but prioritize attacking. Shove if enemies close in.
func aiKite(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter)
	}
	canShv := ts.ShoveCool <= 0

	// Always attack first if possible
	if nearD <= wrange && canAtk {
		target := bestTarget(ts.Position, ts.Enemies, wrange)
		if target != nil {
			return atk(target, weapon)
		}
		return atk(near, weapon)
	}

	// Enemy too close — shove them away, then shoot
	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	// Enemy closing in on cooldown — back off to maintain range
	if nearD <= 2 && !canAtk {
		if canDodge {
			return dodge(gridDirAway(ts.Position, near.Position))
		}
		return moveDir(gridDirAway(ts.Position, near.Position))
	}

	// On cooldown at range — strafe
	if nearD <= wrange && !canAtk {
		return moveDir(perpDir(gridDir(ts.Position, near.Position)))
	}

	// Too far — approach (but not outside zone)
	return moveTo(ts.Position, near.Position)
}

// ASSASSIN: Hunt weakest, dodge to gap-close, shove + burst, relentless.
func aiAssassin(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	canShv := ts.ShoveCool <= 0

	// Find weakest enemy (prioritize low HP targets)
	prey, preyD := weakest(ts.Position, ts.Enemies)
	if prey == nil {
		if near != nil {
			prey = near
			preyD = nearD
		} else {
			return moveTo(ts.Position, ts.ZoneTargetCenter)
		}
	}

	// In range — burst attack
	if preyD <= wrange && canAtk {
		return atk(prey, weapon)
	}

	// Adjacent on cooldown — shove for stun then burst next tick
	if preyD <= 1 && canShv {
		return shove(prey.ID)
	}

	// Close — dodge IN to gap-close for the kill
	if preyD > 1 && preyD <= wrange+2 && canDodge {
		return dodge(gridDir(ts.Position, prey.Position))
	}

	// Hunt them down — but don't leave the zone
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

	// Attack best target in range — always prioritize damage
	target := bestTarget(ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	// Adjacent on cooldown — shove to create space and stun
	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	// On cooldown — advance toward enemy to stay in fight
	if nearD <= wrange+2 && !canAtk {
		if nearD > 1 {
			return moveTo(ts.Position, near.Position)
		}
		return moveDir(perpDir(gridDir(ts.Position, near.Position)))
	}

	// Approach enemies within territory
	if nearD <= wrange+5 {
		return moveTo(ts.Position, near.Position)
	}

	// Hold near zone center
	return moveTo(ts.Position, ts.ZoneTargetCenter)
}

// TERRITORIAL: Aggressively hold zone center, shove + attack all intruders.
func aiTerritorial(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		d := chebyshev(ts.Position, ts.ZoneTargetCenter)
		if d > 3 {
			return moveTo(ts.Position, ts.ZoneTargetCenter)
		}
		return idle()
	}
	canShv := ts.ShoveCool <= 0

	// Attack anything in range
	target := bestTarget(ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	// Adjacent — shove to push enemies out of territory
	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	// Chase enemies that enter territory
	if nearD <= wrange+4 {
		return moveTo(ts.Position, near.Position)
	}

	// Return to zone center
	return moveTo(ts.Position, ts.ZoneTargetCenter)
}
