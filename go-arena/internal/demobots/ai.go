package demobots

import (
	"math"
	"math/rand"
)

// tickState holds the parsed game state from a tick message that the AI uses
// to decide what action to take.
type tickState struct {
	Position   [2]float64
	HP         float64
	MaxHP      float64
	ZoneCenter [2]float64
	ZoneRadius float64
	Enemies    []entity
	Pickups    []entity
}

// entity represents a nearby entity (bot or pickup) extracted from a tick.
type entity struct {
	ID       string
	Type     string // "bot" or "pickup"
	Position [2]float64
	HP       float64
	MaxHP    float64
}

// actionResult is the action the AI decides to send back to the server.
type actionResult struct {
	Action    string     `json:"action"`
	Target    string     `json:"target,omitempty"`
	Direction *[2]float64 `json:"direction,omitempty"`
}

// parseTick extracts the relevant fields from a tick message into a tickState.
func parseTick(msg map[string]interface{}) tickState {
	ts := tickState{
		ZoneCenter: [2]float64{50, 50},
		ZoneRadius: 100,
		MaxHP:      100,
	}

	// Parse your_state.
	if ys, ok := msg["your_state"].(map[string]interface{}); ok {
		ts.Position = parsePos(ys["position"])
		if v, ok := ys["hp"].(float64); ok {
			ts.HP = v
		}
		if v, ok := ys["max_hp"].(float64); ok {
			ts.MaxHP = v
		}

		// Zone info is embedded in your_state.
		if v, ok := ys["zone_center"]; ok {
			ts.ZoneCenter = parsePos(v)
		}
		if v, ok := ys["zone_radius"].(float64); ok {
			ts.ZoneRadius = v
		}
	}

	// Parse safe_zone (top-level, if present).
	if sz, ok := msg["safe_zone"].(map[string]interface{}); ok {
		if v, ok := sz["center"]; ok {
			ts.ZoneCenter = parsePos(v)
		}
		if v, ok := sz["radius"].(float64); ok {
			ts.ZoneRadius = v
		}
	}

	// Parse nearby_entities.
	if ne, ok := msg["nearby_entities"].([]interface{}); ok {
		for _, raw := range ne {
			e, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			ent := entity{
				Position: parsePos(e["position"]),
			}
			if v, ok := e["type"].(string); ok {
				ent.Type = v
			}
			if v, ok := e["id"].(string); ok {
				ent.ID = v
			}
			if v, ok := e["hp"].(float64); ok {
				ent.HP = v
			}
			if v, ok := e["max_hp"].(float64); ok {
				ent.MaxHP = v
			}
			switch ent.Type {
			case "bot":
				ts.Enemies = append(ts.Enemies, ent)
			case "pickup":
				ts.Pickups = append(ts.Pickups, ent)
			}
		}
	}

	return ts
}

// parsePos extracts a [2]float64 position from an interface{} that may be a
// JSON array ([]interface{}) or a [2]float64.
func parsePos(v interface{}) [2]float64 {
	switch p := v.(type) {
	case []interface{}:
		var out [2]float64
		if len(p) >= 2 {
			if x, ok := p[0].(float64); ok {
				out[0] = x
			}
			if y, ok := p[1].(float64); ok {
				out[1] = y
			}
		}
		return out
	case [2]float64:
		return p
	default:
		return [2]float64{0, 0}
	}
}

// distance returns the Euclidean distance between two positions.
func distance(a, b [2]float64) float64 {
	dx := a[0] - b[0]
	dy := a[1] - b[1]
	return math.Sqrt(dx*dx + dy*dy)
}

// toward returns a normalized direction vector from src toward dst.
func toward(src, dst [2]float64) [2]float64 {
	dx := dst[0] - src[0]
	dy := dst[1] - src[1]
	mag := math.Sqrt(dx*dx + dy*dy)
	if mag == 0 {
		return [2]float64{0, 0}
	}
	return [2]float64{dx / mag, dy / mag}
}

// away returns a normalized direction vector from src away from dst.
func away(src, dst [2]float64) [2]float64 {
	d := toward(src, dst)
	return [2]float64{-d[0], -d[1]}
}

// findClosest finds the closest enemy and its distance from pos.
func findClosest(pos [2]float64, enemies []entity) (*entity, float64) {
	var closest *entity
	closestDist := math.Inf(1)
	for i := range enemies {
		d := distance(pos, enemies[i].Position)
		if d < closestDist {
			closestDist = d
			closest = &enemies[i]
		}
	}
	return closest, closestDist
}

// buildAttack constructs an attack action, handling staff's direction-based
// targeting. This matches the Python logic where staff attacks include a
// "direction" field set to the target's position.
func buildAttack(closest *entity, weapon string, _ [2]float64) actionResult {
	a := actionResult{
		Action: "attack",
		Target: closest.ID,
	}
	if weapon == "staff" {
		dir := closest.Position
		a.Direction = &dir
	}
	return a
}

// PickAction decides what action a demo bot should take based on its strategy,
// the current game state, and its weapon. This is a direct port of
// demo_bot_ai.py pick_action.
func PickAction(strategy string, msg map[string]interface{}, weapon string) actionResult {
	ts := parseTick(msg)

	pos := ts.Position
	hp := ts.HP
	maxHP := ts.MaxHP
	wrange := WeaponRanges[weapon]
	if wrange == 0 {
		wrange = 2.0
	}

	closest, closestDist := findClosest(pos, ts.Enemies)

	// Low HP -- grab nearby health pickup.
	if hp < maxHP*0.3 && len(ts.Pickups) > 0 {
		nearest := ts.Pickups[0]
		nearestDist := distance(pos, nearest.Position)
		for i := 1; i < len(ts.Pickups); i++ {
			d := distance(pos, ts.Pickups[i].Position)
			if d < nearestDist {
				nearestDist = d
				nearest = ts.Pickups[i]
			}
		}
		dir := toward(pos, nearest.Position)
		return actionResult{Action: "move", Direction: &dir}
	}

	// Outside safe zone -- move toward center.
	if distance(pos, ts.ZoneCenter) > ts.ZoneRadius*0.8 {
		dir := toward(pos, ts.ZoneCenter)
		return actionResult{Action: "move", Direction: &dir}
	}

	switch strategy {
	case "aggressive":
		return pickAggressive(closest, closestDist, pos, ts.ZoneCenter, wrange, weapon)
	case "defensive":
		return pickDefensive(closest, closestDist, pos, wrange, weapon)
	case "kite":
		return pickKite(closest, closestDist, pos, wrange, weapon)
	case "territorial":
		return pickTerritorial(closest, closestDist, pos, wrange, weapon)
	default:
		return actionResult{Action: "idle"}
	}
}

func pickAggressive(closest *entity, closestDist float64, pos, zoneCenter [2]float64, wrange float64, weapon string) actionResult {
	if closest == nil {
		dir := toward(pos, zoneCenter)
		return actionResult{Action: "move", Direction: &dir}
	}
	if closestDist <= wrange {
		return buildAttack(closest, weapon, pos)
	}
	dir := toward(pos, closest.Position)
	return actionResult{Action: "move", Direction: &dir}
}

func pickDefensive(closest *entity, closestDist float64, pos [2]float64, wrange float64, weapon string) actionResult {
	if closest == nil {
		return actionResult{Action: "idle"}
	}
	if closestDist <= wrange {
		return buildAttack(closest, weapon, pos)
	}
	if closestDist < wrange*2 {
		dir := away(pos, closest.Position)
		return actionResult{Action: "move", Direction: &dir}
	}
	return actionResult{Action: "idle"}
}

func pickKite(closest *entity, closestDist float64, pos [2]float64, wrange float64, weapon string) actionResult {
	if closest == nil {
		angle := rand.Float64() * 2 * math.Pi
		dir := [2]float64{math.Cos(angle), math.Sin(angle)}
		return actionResult{Action: "move", Direction: &dir}
	}
	if closestDist > wrange*0.3 && closestDist <= wrange {
		return buildAttack(closest, weapon, pos)
	}
	if closestDist < wrange*0.4 {
		if rand.Float64() < 0.3 {
			dir := away(pos, closest.Position)
			return actionResult{Action: "dodge", Direction: &dir}
		}
		dir := away(pos, closest.Position)
		return actionResult{Action: "move", Direction: &dir}
	}
	dir := toward(pos, closest.Position)
	return actionResult{Action: "move", Direction: &dir}
}

func pickTerritorial(closest *entity, closestDist float64, pos [2]float64, wrange float64, weapon string) actionResult {
	if closest == nil {
		return actionResult{Action: "idle"}
	}
	if closestDist <= wrange {
		return buildAttack(closest, weapon, pos)
	}
	if closestDist <= wrange*3 {
		dir := toward(pos, closest.Position)
		return actionResult{Action: "move", Direction: &dir}
	}
	return actionResult{Action: "idle"}
}
