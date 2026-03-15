package demobots

import (
	"math"
	"math/rand"
)

type tickState struct {
	Tick        int
	Position    [2]float64
	HP          float64
	MaxHP       float64
	WeaponReady bool
	Cooldown    float64
	DodgeCool   int
	InZone      bool
	ZoneCenter  [2]float64
	ZoneRadius  float64
	KillStreak  int
	HitsThisTick int
	Enemies     []entity
	Pickups     []entity
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
}

type actionResult struct {
	Action         string      `json:"action"`
	Target         string      `json:"target,omitempty"`
	Direction      *[2]float64 `json:"direction,omitempty"`
	TargetPosition *[2]float64 `json:"target_position,omitempty"`
	ItemID         string      `json:"item_id,omitempty"`
}

func parseTick(msg map[string]interface{}) tickState {
	ts := tickState{
		ZoneCenter: [2]float64{1000, 1000},
		ZoneRadius: 1000,
		MaxHP:      150,
		InZone:     true,
	}

	if v, ok := msg["tick"].(float64); ok {
		ts.Tick = int(v)
	}
	if ys, ok := msg["your_state"].(map[string]interface{}); ok {
		ts.Position = parsePos(ys["position"])
		if v, ok := ys["hp"].(float64); ok { ts.HP = v }
		if v, ok := ys["max_hp"].(float64); ok { ts.MaxHP = v }
		if v, ok := ys["weapon_ready"].(bool); ok { ts.WeaponReady = v }
		if v, ok := ys["cooldown_remaining"].(float64); ok { ts.Cooldown = v }
		if v, ok := ys["dodge_cooldown"].(float64); ok { ts.DodgeCool = int(v) }
		if v, ok := ys["in_safe_zone"].(bool); ok { ts.InZone = v }
		if v, ok := ys["zone_center"]; ok { ts.ZoneCenter = parsePos(v) }
		if v, ok := ys["zone_radius"].(float64); ok { ts.ZoneRadius = v }
		if v, ok := ys["kill_streak"].(float64); ok { ts.KillStreak = int(v) }
		if hits, ok := ys["hits_received"].([]interface{}); ok { ts.HitsThisTick = len(hits) }
	}
	if sz, ok := msg["safe_zone"].(map[string]interface{}); ok {
		if v, ok := sz["center"]; ok { ts.ZoneCenter = parsePos(v) }
		if v, ok := sz["radius"].(float64); ok { ts.ZoneRadius = v }
	}

	if ne, ok := msg["nearby_entities"].([]interface{}); ok {
		for _, raw := range ne {
			e, ok := raw.(map[string]interface{})
			if !ok { continue }
			ent := entity{Position: parsePos(e["position"]), IsAlive: true}
			if v, ok := e["type"].(string); ok { ent.Type = v }
			if v, ok := e["id"].(string); ok { ent.ID = v }
			if v, ok := e["bot_id"].(string); ok && ent.ID == "" { ent.ID = v }
			if v, ok := e["hp"].(float64); ok { ent.HP = v }
			if v, ok := e["max_hp"].(float64); ok { ent.MaxHP = v }
			if v, ok := e["weapon"].(string); ok { ent.Weapon = v }
			if v, ok := e["is_alive"].(bool); ok { ent.IsAlive = v }
			if v, ok := e["is_stunned"].(bool); ok { ent.Stunned = v }
			if v, ok := e["is_dodging"].(bool); ok { ent.Dodging = v }
			if v, ok := e["pickup_type"].(string); ok { ent.SubType = v }
			switch ent.Type {
			case "bot":
				if ent.IsAlive { ts.Enemies = append(ts.Enemies, ent) }
			case "pickup":
				ts.Pickups = append(ts.Pickups, ent)
			}
		}
	}
	return ts
}

func parsePos(v interface{}) [2]float64 {
	switch p := v.(type) {
	case []interface{}:
		var out [2]float64
		if len(p) >= 2 {
			if x, ok := p[0].(float64); ok { out[0] = x }
			if y, ok := p[1].(float64); ok { out[1] = y }
		}
		return out
	case [2]float64:
		return p
	}
	return [2]float64{0, 0}
}

func dist(a, b [2]float64) float64 {
	dx := a[0] - b[0]; dy := a[1] - b[1]
	return math.Sqrt(dx*dx + dy*dy)
}

func dirToward(src, dst [2]float64) [2]float64 {
	dx := dst[0] - src[0]; dy := dst[1] - src[1]
	mag := math.Sqrt(dx*dx + dy*dy)
	if mag == 0 { return [2]float64{0, 0} }
	return [2]float64{dx / mag, dy / mag}
}

func dirAway(src, dst [2]float64) [2]float64 {
	d := dirToward(src, dst)
	return [2]float64{-d[0], -d[1]}
}

func dirPerp(src, dst [2]float64) [2]float64 {
	d := dirToward(src, dst)
	if rand.Float64() < 0.5 { return [2]float64{-d[1], d[0]} }
	return [2]float64{d[1], -d[0]}
}

// === Action builders ===

func goTo(pos [2]float64) actionResult {
	return actionResult{Action: "move_to", TargetPosition: &pos}
}

func moveDir(dir [2]float64) actionResult {
	return actionResult{Action: "move", Direction: &dir}
}

func attack(target *entity, weapon string) actionResult {
	a := actionResult{Action: "attack", Target: target.ID}
	if weapon == "staff" { dir := target.Position; a.Direction = &dir }
	return a
}

func dodge(dir [2]float64) actionResult {
	return actionResult{Action: "dodge", Direction: &dir}
}

func useItem(id string) actionResult {
	return actionResult{Action: "use_item", ItemID: id}
}

func idle() actionResult {
	return actionResult{Action: "idle"}
}

// === Target selection ===

func closestEnemy(pos [2]float64, enemies []entity) (*entity, float64) {
	var best *entity; bestD := math.Inf(1)
	for i := range enemies {
		d := dist(pos, enemies[i].Position)
		if d < bestD { bestD = d; best = &enemies[i] }
	}
	return best, bestD
}

func weakestInRange(pos [2]float64, enemies []entity, rng float64) *entity {
	var best *entity; bestHP := math.Inf(1)
	for i := range enemies {
		d := dist(pos, enemies[i].Position)
		if d <= rng && enemies[i].HP < bestHP && !enemies[i].Dodging {
			bestHP = enemies[i].HP; best = &enemies[i]
		}
	}
	return best
}

func nearestPickup(pos [2]float64, pickups []entity) (*entity, float64) {
	var best *entity; bestD := math.Inf(1)
	for i := range pickups {
		d := dist(pos, pickups[i].Position)
		if d < bestD { bestD = d; best = &pickups[i] }
	}
	return best, bestD
}

func bestPickup(pos [2]float64, pickups []entity, hpRatio float64) (*entity, float64) {
	var best *entity; bestScore := 0.0; bestD := 0.0
	for i := range pickups {
		d := dist(pos, pickups[i].Position)
		if d > 30 { continue } // don't chase far pickups
		score := 0.0
		switch pickups[i].SubType {
		case "health_pack":
			score = (1.0 - hpRatio) * 80
		case "shield_bubble":
			score = 50
		case "damage_boost":
			score = 40
		case "speed_boost":
			score = 25
		default:
			score = 15
		}
		// Nearby pickups are more valuable
		score += math.Max(0, 20-d)
		if score > bestScore { bestScore = score; best = &pickups[i]; bestD = d }
	}
	return best, bestD
}

// === Main AI ===

func PickAction(strategy string, msg map[string]interface{}, weapon string) actionResult {
	ts := parseTick(msg)
	pos := ts.Position
	hp := ts.HP
	maxHP := ts.MaxHP
	hpRatio := hp / maxHP
	wrange := WeaponRanges[weapon]
	if wrange == 0 { wrange = 2.5 }
	canAttack := ts.WeaponReady || ts.Cooldown <= 0
	canDodge := ts.DodgeCool <= 0

	closest, closestDist := closestEnemy(pos, ts.Enemies)

	// === REACT: Just got hit — dodge away ===
	if ts.HitsThisTick > 0 && canDodge && closest != nil && closestDist < wrange*3 {
		return dodge(dirAway(pos, closest.Position))
	}

	// === CRITICAL HP: Dodge away, find health ===
	if hpRatio < 0.2 && closest != nil && closestDist < wrange*2 {
		if canDodge {
			return dodge(dirAway(pos, closest.Position))
		}
		dir := dirAway(pos, closest.Position)
		return moveDir(dir)
	}

	// === PICKUP: Grab nearby health when hurt ===
	if hpRatio < 0.6 {
		p, pDist := bestPickup(pos, ts.Pickups, hpRatio)
		if p != nil && pDist < 12 {
			return useItem(p.ID) // try to collect
		}
		if p != nil && pDist < 20 {
			return goTo(p.Position) // navigate to it
		}
	}

	// === ZONE: Get back in zone ===
	if !ts.InZone {
		// If enemy in range, hit them on the way
		if closest != nil && closestDist <= wrange && canAttack {
			return attack(closest, weapon)
		}
		return goTo(ts.ZoneCenter)
	}

	// === PICKUP: Grab any pickup if no enemies nearby ===
	if len(ts.Enemies) == 0 {
		p, pDist := nearestPickup(pos, ts.Pickups)
		if p != nil && pDist < 12 {
			return useItem(p.ID)
		}
		if p != nil && pDist < 25 {
			return goTo(p.Position)
		}
	}

	// === COMBAT: Strategy-specific ===
	switch strategy {
	case "aggressive":
		return aiAggressive(ts, closest, closestDist, wrange, weapon, canAttack, canDodge)
	case "defensive":
		return aiDefensive(ts, closest, closestDist, wrange, weapon, canAttack, canDodge)
	case "kite":
		return aiKite(ts, closest, closestDist, wrange, weapon, canAttack, canDodge)
	case "territorial":
		return aiTerritorial(ts, closest, closestDist, wrange, weapon, canAttack, canDodge)
	case "assassin":
		return aiAssassin(ts, closest, closestDist, wrange, weapon, canAttack, canDodge)
	case "berserker":
		return aiBerserker(ts, closest, closestDist, wrange, weapon, canAttack, canDodge)
	default:
		return aiAggressive(ts, closest, closestDist, wrange, weapon, canAttack, canDodge)
	}
}

// AGGRESSIVE: Attack first, ask questions later. Chase hard, finish kills.
func aiAggressive(ts tickState, closest *entity, closestDist, wrange float64, weapon string, canAttack, canDodge bool) actionResult {
	if closest == nil {
		return goTo(ts.ZoneCenter)
	}

	// Finish weak targets first
	weak := weakestInRange(ts.Position, ts.Enemies, wrange)
	if weak != nil && canAttack {
		return attack(weak, weapon)
	}

	// In range — attack
	if closestDist <= wrange && canAttack {
		return attack(closest, weapon)
	}

	// Close but on cooldown — strafe, don't run away
	if closestDist <= wrange*1.5 && !canAttack {
		return moveDir(dirPerp(ts.Position, closest.Position))
	}

	// Chase — pathfind
	return goTo(closest.Position)
}

// DEFENSIVE: Hold ground, attack when enemies come to you, retreat when hurt.
func aiDefensive(ts tickState, closest *entity, closestDist, wrange float64, weapon string, canAttack, canDodge bool) actionResult {
	if closest == nil {
		d := dist(ts.Position, ts.ZoneCenter)
		if d > ts.ZoneRadius*0.3 { return goTo(ts.ZoneCenter) }
		return idle()
	}

	// Multiple threats — dodge
	threats := 0
	for _, e := range ts.Enemies {
		if dist(ts.Position, e.Position) < wrange*2 { threats++ }
	}
	if threats >= 2 && canDodge {
		return dodge(dirAway(ts.Position, closest.Position))
	}

	// In range — attack (prefer stunned)
	if closestDist <= wrange && canAttack {
		for i := range ts.Enemies {
			if ts.Enemies[i].Stunned && dist(ts.Position, ts.Enemies[i].Position) <= wrange {
				return attack(&ts.Enemies[i], weapon)
			}
		}
		return attack(closest, weapon)
	}

	// Too close — back off
	if closestDist < wrange*0.5 {
		return moveDir(dirAway(ts.Position, closest.Position))
	}

	return idle()
}

// KITE: Stay at optimal range, strafe during cooldown, never let enemies close.
func aiKite(ts tickState, closest *entity, closestDist, wrange float64, weapon string, canAttack, canDodge bool) actionResult {
	if closest == nil {
		// Patrol near zone center
		d := dist(ts.Position, ts.ZoneCenter)
		if d > ts.ZoneRadius*0.4 { return goTo(ts.ZoneCenter) }
		return idle()
	}

	optimalRange := wrange * 0.75

	// Too close — dodge or retreat
	if closestDist < optimalRange*0.4 {
		if canDodge {
			return dodge(dirAway(ts.Position, closest.Position))
		}
		return moveDir(dirAway(ts.Position, closest.Position))
	}

	// In range — attack if ready
	if closestDist <= wrange {
		if canAttack {
			weak := weakestInRange(ts.Position, ts.Enemies, wrange)
			if weak != nil { return attack(weak, weapon) }
			return attack(closest, weapon)
		}
		// On cooldown — strafe to be unpredictable
		return moveDir(dirPerp(ts.Position, closest.Position))
	}

	// Too far — close to optimal range
	return goTo(closest.Position)
}

// TERRITORIAL: Hold zone center, intercept enemies entering territory.
func aiTerritorial(ts tickState, closest *entity, closestDist, wrange float64, weapon string, canAttack, canDodge bool) actionResult {
	if closest == nil {
		d := dist(ts.Position, ts.ZoneCenter)
		if d > ts.ZoneRadius*0.25 { return goTo(ts.ZoneCenter) }
		return idle()
	}

	// In range — attack
	if closestDist <= wrange && canAttack {
		return attack(closest, weapon)
	}

	// Enemy in our territory — intercept
	if closestDist <= wrange*3 {
		return goTo(closest.Position)
	}

	// Hold center
	d := dist(ts.Position, ts.ZoneCenter)
	if d > ts.ZoneRadius*0.25 { return goTo(ts.ZoneCenter) }
	return idle()
}

// ASSASSIN: Hunt weak targets, dodge-close, burst damage.
func aiAssassin(ts tickState, closest *entity, closestDist, wrange float64, weapon string, canAttack, canDodge bool) actionResult {
	// Find lowest HP enemy
	var prey *entity
	preyDist := math.Inf(1)
	for i := range ts.Enemies {
		if ts.Enemies[i].HP < ts.Enemies[i].MaxHP*0.5 {
			d := dist(ts.Position, ts.Enemies[i].Position)
			if d < preyDist {
				prey = &ts.Enemies[i]
				preyDist = d
			}
		}
	}
	if prey == nil {
		prey = closest
		preyDist = closestDist
	}

	if prey == nil {
		return goTo(ts.ZoneCenter)
	}

	// In range — burst
	if preyDist <= wrange && canAttack {
		return attack(prey, weapon)
	}

	// Close — dodge to gap close
	if preyDist <= wrange*2.5 && canDodge {
		return dodge(dirToward(ts.Position, prey.Position))
	}

	// On cooldown and close — strafe
	if preyDist <= wrange*1.5 && !canAttack {
		return moveDir(dirPerp(ts.Position, prey.Position))
	}

	// Hunt
	return goTo(prey.Position)
}

// BERSERKER: All-in, never retreat. Dodge offensively. Shove on cooldown.
func aiBerserker(ts tickState, closest *entity, closestDist, wrange float64, weapon string, canAttack, canDodge bool) actionResult {
	if closest == nil {
		return goTo(ts.ZoneCenter)
	}

	// In range — attack always
	if closestDist <= wrange && canAttack {
		return attack(closest, weapon)
	}

	// Very close but on cooldown — shove
	if closestDist <= 3 && !canAttack {
		return actionResult{Action: "shove", Target: closest.ID}
	}

	// Gap close with dodge
	if closestDist <= wrange*3 && canDodge {
		return dodge(dirToward(ts.Position, closest.Position))
	}

	// On cooldown and close — strafe
	if closestDist <= wrange*1.5 && !canAttack {
		return moveDir(dirPerp(ts.Position, closest.Position))
	}

	// Rush
	return goTo(closest.Position)
}
