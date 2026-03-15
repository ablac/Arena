package demobots

import (
	"math"
	"math/rand"
)

type tickState struct {
	Tick            int
	Position        [2]float64
	HP              float64
	MaxHP           float64
	WeaponReady     bool
	Cooldown        float64
	DodgeCool       int
	InvulnTicks     int
	StunTicks       int
	ShieldHP        float64
	InZone          bool
	ZoneDist        float64
	ZoneCenter      [2]float64
	ZoneRadius      float64
	ZoneTargetCenter [2]float64
	ZoneTargetRadius float64
	KillStreak      int
	RoundKills      int
	HitsThisTick    int
	LastActionOK    bool
	HasSpeedBoost   bool
	HasDmgBoost     bool
	Enemies         []entity
	Pickups         []entity
	Hints           []hint
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

type actionResult struct {
	Action         string      `json:"action"`
	Target         string      `json:"target,omitempty"`
	Direction      *[2]float64 `json:"direction,omitempty"`
	TargetPosition *[2]float64 `json:"target_position,omitempty"`
	ItemID         string      `json:"item_id,omitempty"`
}

func parseTick(msg map[string]interface{}) tickState {
	ts := tickState{
		ZoneCenter: [2]float64{1000, 1000}, ZoneRadius: 1000,
		ZoneTargetCenter: [2]float64{1000, 1000}, ZoneTargetRadius: 500,
		MaxHP: 150, InZone: true, LastActionOK: true,
	}
	if v, ok := msg["tick"].(float64); ok { ts.Tick = int(v) }
	if ys, ok := msg["your_state"].(map[string]interface{}); ok {
		ts.Position = parsePos(ys["position"])
		if v, ok := ys["hp"].(float64); ok { ts.HP = v }
		if v, ok := ys["max_hp"].(float64); ok { ts.MaxHP = v }
		if v, ok := ys["weapon_ready"].(bool); ok { ts.WeaponReady = v }
		if v, ok := ys["cooldown_remaining"].(float64); ok { ts.Cooldown = v }
		if v, ok := ys["dodge_cooldown"].(float64); ok { ts.DodgeCool = int(v) }
		if v, ok := ys["invuln_ticks"].(float64); ok { ts.InvulnTicks = int(v) }
		if v, ok := ys["stun_ticks"].(float64); ok { ts.StunTicks = int(v) }
		if v, ok := ys["shield_absorb"].(float64); ok { ts.ShieldHP = v }
		if v, ok := ys["in_safe_zone"].(bool); ok { ts.InZone = v }
		if v, ok := ys["distance_to_zone_edge"].(float64); ok { ts.ZoneDist = v }
		if v, ok := ys["zone_center"]; ok { ts.ZoneCenter = parsePos(v) }
		if v, ok := ys["zone_radius"].(float64); ok { ts.ZoneRadius = v }
		if v, ok := ys["zone_target_center"]; ok { ts.ZoneTargetCenter = parsePos(v) }
		if v, ok := ys["zone_target_radius"].(float64); ok { ts.ZoneTargetRadius = v }
		if v, ok := ys["kill_streak"].(float64); ok { ts.KillStreak = int(v) }
		if v, ok := ys["round_kills"].(float64); ok { ts.RoundKills = int(v) }
		if hits, ok := ys["hits_received"].([]interface{}); ok { ts.HitsThisTick = len(hits) }
		if lar, ok := ys["last_action_result"].(map[string]interface{}); ok {
			if v, ok := lar["success"].(bool); ok { ts.LastActionOK = v }
		}
		if effs, ok := ys["effects"].([]interface{}); ok {
			for _, raw := range effs {
				if e, ok := raw.(map[string]interface{}); ok {
					if name, ok := e["name"].(string); ok {
						if name == "speed_boost" { ts.HasSpeedBoost = true }
						if name == "damage_boost" { ts.HasDmgBoost = true }
					}
				}
			}
		}
	}
	if sz, ok := msg["safe_zone"].(map[string]interface{}); ok {
		if v, ok := sz["center"]; ok { ts.ZoneCenter = parsePos(v) }
		if v, ok := sz["radius"].(float64); ok { ts.ZoneRadius = v }
		if v, ok := sz["target_center"]; ok { ts.ZoneTargetCenter = parsePos(v) }
		if v, ok := sz["target_radius"].(float64); ok { ts.ZoneTargetRadius = v }
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
			if v, ok := e["target_id"].(string); ok { ent.TargetID = v }
			if v, ok := e["pickup_type"].(string); ok { ent.SubType = v }
			switch ent.Type {
			case "bot":
				if ent.IsAlive { ts.Enemies = append(ts.Enemies, ent) }
			case "pickup":
				ts.Pickups = append(ts.Pickups, ent)
			}
		}
	}
	// Parse hints for when no enemies are visible.
	if h, ok := msg["hints"].([]interface{}); ok {
		for _, raw := range h {
			if hm, ok := raw.(map[string]interface{}); ok {
				hi := hint{}
				if v, ok := hm["hint_type"].(string); ok { hi.HintType = v }
				if v, ok := hm["direction"]; ok { hi.Direction = parsePos(v) }
				if v, ok := hm["distance"].(float64); ok { hi.Distance = v }
				if v, ok := hm["pickup_type"].(string); ok { hi.PickupType = v }
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

// === Math helpers ===

func dist(a, b [2]float64) float64 {
	dx := a[0] - b[0]; dy := a[1] - b[1]
	return math.Sqrt(dx*dx + dy*dy)
}
func dirTo(src, dst [2]float64) [2]float64 {
	dx := dst[0] - src[0]; dy := dst[1] - src[1]
	m := math.Sqrt(dx*dx + dy*dy)
	if m == 0 { return [2]float64{0, 0} }
	return [2]float64{dx / m, dy / m}
}
func dirAway(src, dst [2]float64) [2]float64 {
	d := dirTo(src, dst)
	return [2]float64{-d[0], -d[1]}
}
func dirPerp(src, dst [2]float64) [2]float64 {
	d := dirTo(src, dst)
	if rand.Float64() < 0.5 { return [2]float64{-d[1], d[0]} }
	return [2]float64{d[1], -d[0]}
}
// offsetPos returns a position offset from src in direction of dst by amount.
func offsetPos(src, dst [2]float64, amount float64) [2]float64 {
	d := dirTo(src, dst)
	return [2]float64{src[0] + d[0]*amount, src[1] + d[1]*amount}
}

// === Action builders ===

func goTo(pos [2]float64) actionResult   { return actionResult{Action: "move_to", TargetPosition: &pos} }
func moveDir(d [2]float64) actionResult  { return actionResult{Action: "move", Direction: &d} }
func atk(t *entity, w string) actionResult {
	a := actionResult{Action: "attack", Target: t.ID}
	if w == "staff" { dir := t.Position; a.Direction = &dir }
	return a
}
func dodge(d [2]float64) actionResult    { return actionResult{Action: "dodge", Direction: &d} }
func shove(id string) actionResult       { return actionResult{Action: "shove", Target: id} }
func useItem(id string) actionResult     { return actionResult{Action: "use_item", ItemID: id} }
func idle() actionResult                 { return actionResult{Action: "idle"} }

// === Target selection ===

func closest(pos [2]float64, enemies []entity) (*entity, float64) {
	var b *entity; bd := math.Inf(1)
	for i := range enemies {
		d := dist(pos, enemies[i].Position)
		if d < bd { bd = d; b = &enemies[i] }
	}
	return b, bd
}

// bestTarget picks the optimal attack target considering HP, distance, and state.
func bestTarget(pos [2]float64, enemies []entity, wrange float64) *entity {
	var best *entity
	bestScore := -math.Inf(1)
	for i := range enemies {
		e := &enemies[i]
		if e.Dodging { continue } // skip invulnerable targets
		d := dist(pos, e.Position)
		if d > wrange { continue }
		// Score: prioritize low HP, stunned, and close targets
		score := 100 - e.HP
		if e.Stunned { score += 50 } // free hits on stunned enemies
		score -= d * 2                // prefer closer
		if e.HP < e.MaxHP*0.3 { score += 30 } // finish off weak
		if score > bestScore { bestScore = score; best = e }
	}
	return best
}

// isMelee returns true if the weapon is short range.
func isMelee(weapon string) bool {
	return weapon == "sword" || weapon == "daggers" || weapon == "shield" || weapon == "spear"
}

// === Pickup logic ===

func bestPickup(pos [2]float64, pickups []entity, hpRatio float64, hasShield bool) (*entity, float64) {
	var best *entity; bestScore := 0.0; bestD := 0.0
	for i := range pickups {
		d := dist(pos, pickups[i].Position)
		if d > 25 { continue }
		score := 0.0
		switch pickups[i].SubType {
		case "health_pack":
			score = (1.0 - hpRatio) * 100
			if hpRatio > 0.8 { score = 0 }
		case "shield_bubble":
			if !hasShield { score = 60 } else { score = 10 }
		case "damage_boost":
			score = 45
		case "speed_boost":
			score = 35
		default:
			score = 15
		}
		score += math.Max(0, 15-d) // nearby bonus
		if score > bestScore { bestScore = score; best = &pickups[i]; bestD = d }
	}
	if bestScore <= 5 { return nil, 0 }
	return best, bestD
}

// === Main AI ===

func PickAction(strategy string, msg map[string]interface{}, weapon string) actionResult {
	ts := parseTick(msg)
	pos := ts.Position
	hpRatio := ts.HP / ts.MaxHP
	wrange := WeaponRanges[weapon]
	if wrange == 0 { wrange = 2.5 }
	canAtk := ts.WeaponReady
	canDodge := ts.DodgeCool <= 0
	near, nearD := closest(pos, ts.Enemies)

	// Stunned — can't do anything
	if ts.StunTicks > 0 { return idle() }

	// === REACT: Got hit — dodge if possible ===
	if ts.HitsThisTick > 0 && canDodge && near != nil && nearD < wrange*3 {
		// Dodge perpendicular to attacker (harder to predict)
		return dodge(dirPerp(pos, near.Position))
	}

	// === CRITICAL HP: Survive ===
	if hpRatio < 0.15 {
		// Try to grab a health pickup first
		p, pd := bestPickup(pos, ts.Pickups, hpRatio, ts.ShieldHP > 0)
		if p != nil && p.SubType == "health_pack" && pd < 12 {
			return useItem(p.ID)
		}
		if near != nil && nearD < wrange*2 {
			if canDodge {
				return dodge(dirAway(pos, near.Position))
			}
			return moveDir(dirAway(pos, near.Position))
		}
		// Navigate to health if visible
		if p != nil && p.SubType == "health_pack" {
			return goTo(p.Position)
		}
	}

	// === PICKUP: Collect if in range ===
	p, pd := bestPickup(pos, ts.Pickups, hpRatio, ts.ShieldHP > 0)
	if p != nil && pd < 12 {
		return useItem(p.ID)
	}
	// Navigate to high-value pickup if hurt or no enemies nearby
	if p != nil && pd < 20 && (hpRatio < 0.5 || len(ts.Enemies) == 0) {
		return goTo(p.Position)
	}

	// === ZONE: Get inside zone, prefer moving toward future zone center ===
	if !ts.InZone {
		if near != nil && nearD <= wrange && canAtk {
			return atk(near, weapon) // fight on the way
		}
		// Move toward zone target center (where it's shrinking to)
		return goTo(ts.ZoneTargetCenter)
	}

	// === NO ENEMIES VISIBLE: Use hints ===
	if len(ts.Enemies) == 0 {
		// Check for pickup hints
		for _, h := range ts.Hints {
			if h.HintType == "pickup" && h.Distance < 40 {
				target := [2]float64{pos[0] + h.Direction[0]*h.Distance, pos[1] + h.Direction[1]*h.Distance}
				return goTo(target)
			}
		}
		// Check for bot hints — hunt them down
		for _, h := range ts.Hints {
			if h.HintType == "bot" && h.Distance < 60 {
				target := [2]float64{pos[0] + h.Direction[0]*h.Distance, pos[1] + h.Direction[1]*h.Distance}
				return goTo(target)
			}
		}
		// Move toward zone center
		d := dist(pos, ts.ZoneTargetCenter)
		if d > ts.ZoneRadius*0.3 { return goTo(ts.ZoneTargetCenter) }
		return idle()
	}

	// === COMBAT: Strategy-specific ===
	switch strategy {
	case "aggressive":
		return aiAggressive(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "defensive":
		return aiDefensive(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "kite":
		return aiKite(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "territorial":
		return aiTerritorial(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "assassin":
		return aiAssassin(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	case "berserker":
		return aiBerserker(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	default:
		return aiAggressive(ts, near, nearD, wrange, weapon, canAtk, canDodge)
	}
}

// AGGRESSIVE: Always engaging. Focus weak targets. Shove when on cooldown.
func aiAggressive(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil { return goTo(ts.ZoneTargetCenter) }

	// Attack best target in range
	target := bestTarget(ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	// Close but on cooldown — shove if very close, else strafe
	if nearD <= 3 && !canAtk {
		return shove(near.ID)
	}
	if nearD <= wrange*1.5 && !canAtk {
		return moveDir(dirPerp(ts.Position, near.Position))
	}

	// Chase — pathfind around obstacles
	return goTo(near.Position)
}

// DEFENSIVE: Hold ground. Prioritize stunned targets. Dodge when outnumbered.
func aiDefensive(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		d := dist(ts.Position, ts.ZoneTargetCenter)
		if d > ts.ZoneRadius*0.3 { return goTo(ts.ZoneTargetCenter) }
		return idle()
	}

	// Count threats
	threats := 0
	for _, e := range ts.Enemies {
		if dist(ts.Position, e.Position) < wrange*2 { threats++ }
	}
	if threats >= 2 && canDodge {
		return dodge(dirAway(ts.Position, near.Position))
	}

	// Attack best target
	target := bestTarget(ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	// Too close — shove and retreat
	if nearD < wrange*0.4 {
		if nearD <= 3 { return shove(near.ID) }
		return moveDir(dirAway(ts.Position, near.Position))
	}

	// On cooldown — strafe
	if nearD <= wrange*1.5 && !canAtk {
		return moveDir(dirPerp(ts.Position, near.Position))
	}

	return idle()
}

// KITE: Maintain optimal range. Strafe during cooldown. Dodge gap-closers.
func aiKite(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		d := dist(ts.Position, ts.ZoneTargetCenter)
		if d > ts.ZoneRadius*0.3 { return goTo(ts.ZoneTargetCenter) }
		return idle()
	}

	optRange := wrange * 0.7

	// Enemy too close — dodge or retreat
	if nearD < optRange*0.4 {
		if canDodge { return dodge(dirPerp(ts.Position, near.Position)) }
		return moveDir(dirAway(ts.Position, near.Position))
	}

	// In range — attack best target or strafe
	if nearD <= wrange {
		if canAtk {
			target := bestTarget(ts.Position, ts.Enemies, wrange)
			if target != nil { return atk(target, weapon) }
			return atk(near, weapon)
		}
		return moveDir(dirPerp(ts.Position, near.Position))
	}

	// Too far — approach to optimal range (not melee range)
	if nearD > wrange {
		approachPoint := offsetPos(near.Position, ts.Position, optRange)
		return goTo(approachPoint)
	}

	return idle()
}

// TERRITORIAL: Hold zone center. Intercept invaders. Shove trespassers.
func aiTerritorial(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		d := dist(ts.Position, ts.ZoneTargetCenter)
		if d > ts.ZoneRadius*0.2 { return goTo(ts.ZoneTargetCenter) }
		return idle()
	}

	// Attack in range
	target := bestTarget(ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	// Shove intruders
	if nearD <= 3 && !canAtk {
		return shove(near.ID)
	}

	// Enemy in territory — intercept
	if nearD <= wrange*3 {
		if !canAtk && nearD <= wrange*1.5 {
			return moveDir(dirPerp(ts.Position, near.Position))
		}
		return goTo(near.Position)
	}

	// Hold center
	d := dist(ts.Position, ts.ZoneTargetCenter)
	if d > ts.ZoneRadius*0.2 { return goTo(ts.ZoneTargetCenter) }
	return idle()
}

// ASSASSIN: Hunt weak targets. Dodge to gap-close. Burst then disengage.
func aiAssassin(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	// Find weakest enemy
	var prey *entity; preyD := math.Inf(1)
	for i := range ts.Enemies {
		e := &ts.Enemies[i]
		d := dist(ts.Position, e.Position)
		// Heavily prioritize low HP
		effectiveD := d
		if e.HP < e.MaxHP*0.4 { effectiveD -= 20 }
		if e.Stunned { effectiveD -= 15 }
		if effectiveD < preyD { prey = e; preyD = d }
	}
	if prey == nil {
		if near != nil { prey = near; preyD = nearD } else { return goTo(ts.ZoneTargetCenter) }
	}

	// In range — burst
	if preyD <= wrange && canAtk {
		return atk(prey, weapon)
	}

	// Close — dodge IN to gap-close
	if preyD <= wrange*2.5 && canDodge {
		return dodge(dirTo(ts.Position, prey.Position))
	}

	// On cooldown close — shove for stun
	if preyD <= 3 && !canAtk {
		return shove(prey.ID)
	}

	// Strafe on cooldown
	if preyD <= wrange*1.5 && !canAtk {
		return moveDir(dirPerp(ts.Position, prey.Position))
	}

	return goTo(prey.Position)
}

// BERSERKER: Never retreat. Dodge offensively. Shove constantly. Fight to the death.
func aiBerserker(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil { return goTo(ts.ZoneTargetCenter) }

	// Attack always
	if nearD <= wrange && canAtk {
		target := bestTarget(ts.Position, ts.Enemies, wrange)
		if target != nil { return atk(target, weapon) }
		return atk(near, weapon)
	}

	// Very close on cooldown — shove
	if nearD <= 3 && !canAtk {
		return shove(near.ID)
	}

	// Gap close with dodge
	if nearD <= wrange*3 && canDodge {
		return dodge(dirTo(ts.Position, near.Position))
	}

	// On cooldown close — strafe
	if nearD <= wrange*1.5 && !canAtk {
		return moveDir(dirPerp(ts.Position, near.Position))
	}

	return goTo(near.Position)
}
