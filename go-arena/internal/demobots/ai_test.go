package demobots

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"arena-server/internal/config"
	"arena-server/internal/ws"
)

// setTerrain installs a synthetic open terrain grid (with optional wall rows)
// for pathfinding tests, and returns a cleanup that clears it.
func setTerrain(t testing.TB, width, height int, walls [][2]int) {
	t.Helper()
	rows := make([]interface{}, height)
	for r := 0; r < height; r++ {
		row := make([]byte, width)
		for c := 0; c < width; c++ {
			row[c] = '.'
		}
		for _, w := range walls {
			if w[1] == r && w[0] >= 0 && w[0] < width {
				row[w[0]] = '#'
			}
		}
		rows[r] = string(row)
	}
	parseTerrain(map[string]interface{}{
		"width":     float64(width),
		"height":    float64(height),
		"cell_size": float64(20),
		"terrain":   rows,
	})
	t.Cleanup(clearTerrain)
}

func clearTerrain() {
	terrainMu.Lock()
	cachedTerrain = nil
	terrainMu.Unlock()
}

func TestStaffAttackPayloadMatchesPublicProtocol(t *testing.T) {
	oldWidth, oldHeight := config.C.ArenaWidth, config.C.ArenaHeight
	config.C.ArenaWidth, config.C.ArenaHeight = 2000, 2000
	t.Cleanup(func() {
		config.C.ArenaWidth, config.C.ArenaHeight = oldWidth, oldHeight
	})

	target := entity{ID: "enemy", Position: [2]float64{12, 8}}
	payload := buildActionPayload(float64(17), atk(&target, "staff"))
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Staff action: %v", err)
	}

	msgType, parsed, err := ws.ParseBotMessage(raw)
	if err != nil {
		t.Fatalf("parse Staff action %s: %v", raw, err)
	}
	if msgType != "action" {
		t.Fatalf("message type = %q, want action", msgType)
	}
	actionMessage, ok := parsed.(*ws.ActionMessage)
	if !ok {
		t.Fatalf("parsed message = %T, want *ws.ActionMessage", parsed)
	}
	if got := ws.ActionMessageToAction(actionMessage); got == nil {
		t.Fatalf("demo Staff action violates the public protocol: %s", raw)
	}
}

func TestParseTickTeamsEntitiesAndModeFields(t *testing.T) {
	clearTerrain() // worldToGridPos falls back to cell size 20

	msg := map[string]interface{}{
		"type":          "tick",
		"tick":          float64(42),
		"game_mode":     "ctf",
		"sudden_death":  true,
		"bounty_target": "bot-9",
		"void_tiles": []interface{}{
			[]interface{}{float64(5), float64(6)},
			[]interface{}{float64(7), float64(8)},
		},
		"team_scores": map[string]interface{}{"1": float64(2), "2": float64(1)},
		"flags": []interface{}{
			map[string]interface{}{
				"id":            "flag_1",
				"team":          float64(1),
				"position":      []interface{}{float64(100), float64(200)}, // world → grid (5,10)
				"base_position": []interface{}{float64(100), float64(200)},
				"status":        "dropped",
				"carrier_id":    "",
			},
			map[string]interface{}{
				"id":            "flag_2",
				"team":          float64(2),
				"position":      []interface{}{float64(300), float64(300)},
				"base_position": []interface{}{float64(320), float64(320)},
				"status":        "carried",
				"carrier_id":    "enemy-1",
			},
		},
		"your_state": map[string]interface{}{
			"position": []interface{}{float64(3), float64(3)},
			"team":     float64(1),
			"hp":       float64(100),
			"max_hp":   float64(150),
		},
		"nearby_entities": []interface{}{
			map[string]interface{}{
				"type": "bot", "id": "ally-1", "team": float64(1),
				"position": []interface{}{float64(4), float64(3)}, "is_alive": true,
			},
			map[string]interface{}{
				"type": "bot", "id": "enemy-1", "team": float64(2),
				"position": []interface{}{float64(6), float64(3)}, "is_alive": true,
				"threat_score": float64(50),
			},
			map[string]interface{}{
				"type": "bot", "id": "solo-1", // no team → enemy
				"position": []interface{}{float64(7), float64(4)}, "is_alive": true,
			},
			map[string]interface{}{
				"type": "landmine", "id": "m1", "owner_id": "someone-else",
				"armed": true, "position": []interface{}{float64(2), float64(2)},
			},
			map[string]interface{}{
				"type": "gravity_well", "id": "g1", "owner_id": "someone-else",
				"pull_radius": float64(3), "position": []interface{}{float64(8), float64(8)},
			},
			map[string]interface{}{
				"type": "hazard_zone", "id": "h1",
				"width": float64(5), "height": float64(3), "active": true,
				"on_ticks": float64(30), "off_ticks": float64(20),
				"tick_counter": float64(5), "damage_per_tick": float64(4),
				"position": []interface{}{float64(10), float64(10)},
			},
		},
	}

	ts := parseTick(msg)

	if ts.Team != 1 {
		t.Errorf("Team = %d, want 1", ts.Team)
	}
	if ts.Mode != "ctf" {
		t.Errorf("Mode = %q, want ctf", ts.Mode)
	}
	if !ts.SuddenDeath {
		t.Error("SuddenDeath = false, want true")
	}
	if ts.BountyTargetID != "bot-9" {
		t.Errorf("BountyTargetID = %q, want bot-9", ts.BountyTargetID)
	}
	if len(ts.VoidTiles) != 2 || ts.VoidTiles[0] != [2]int{5, 6} || ts.VoidTiles[1] != [2]int{7, 8} {
		t.Errorf("VoidTiles = %v, want [[5 6] [7 8]]", ts.VoidTiles)
	}
	if ts.TeamScores["1"] != 2 || ts.TeamScores["2"] != 1 {
		t.Errorf("TeamScores = %v, want map[1:2 2:1]", ts.TeamScores)
	}

	// Team routing: teammate → Allies, enemy team + teamless → Enemies.
	if len(ts.Allies) != 1 || ts.Allies[0].ID != "ally-1" {
		t.Errorf("Allies = %+v, want exactly ally-1", ts.Allies)
	}
	if len(ts.Enemies) != 2 {
		t.Fatalf("len(Enemies) = %d, want 2", len(ts.Enemies))
	}
	if ts.Enemies[0].ID != "enemy-1" || ts.Enemies[0].ThreatScore != 50 || ts.Enemies[0].Team != 2 {
		t.Errorf("Enemies[0] = %+v, want enemy-1 with threat 50, team 2", ts.Enemies[0])
	}
	if ts.Enemies[1].ID != "solo-1" {
		t.Errorf("Enemies[1].ID = %q, want solo-1", ts.Enemies[1].ID)
	}

	// New entity buckets.
	if len(ts.Mines) != 1 || !ts.Mines[0].Armed || ts.Mines[0].OwnerID != "someone-else" {
		t.Errorf("Mines = %+v, want one armed mine owned by someone-else", ts.Mines)
	}
	if len(ts.GravityWells) != 1 || ts.GravityWells[0].PullRadius != 3 {
		t.Errorf("GravityWells = %+v, want one well with pull radius 3", ts.GravityWells)
	}
	if len(ts.HazardZones) != 1 {
		t.Fatalf("len(HazardZones) = %d, want 1", len(ts.HazardZones))
	}
	hz := ts.HazardZones[0]
	if hz.Width != 5 || hz.Height != 3 || hz.OnTicks != 30 || hz.OffTicks != 20 || hz.TickCounter != 5 || hz.DamagePerTick != 4 {
		t.Errorf("hazard zone fields = %+v", hz)
	}

	// Flags parsed from tickExtra shape with world→grid conversion.
	if len(ts.Flags) != 2 {
		t.Fatalf("len(Flags) = %d, want 2", len(ts.Flags))
	}
	f1 := ts.Flags[0]
	if f1.ID != "flag_1" || f1.Team != 1 || f1.Status != "dropped" {
		t.Errorf("flag_1 = %+v", f1)
	}
	if f1.Position != [2]float64{5, 10} || f1.BasePosition != [2]float64{5, 10} {
		t.Errorf("flag_1 position = %v base = %v, want grid (5,10)", f1.Position, f1.BasePosition)
	}
	f2 := ts.Flags[1]
	if f2.Status != "carried" || f2.CarrierID != "enemy-1" {
		t.Errorf("flag_2 = %+v, want carried by enemy-1", f2)
	}
	if !ts.isFlagCarrier("enemy-1") || ts.isFlagCarrier("ally-1") {
		t.Error("isFlagCarrier: want true for enemy-1, false for ally-1")
	}

	// Rectangular hazard containment (server half-extent math): width 5 →
	// halfW 2, so (8..12, 9..11) is inside.
	if !inHazardZone([2]float64{8, 9}, ts.HazardZones) {
		t.Error("inHazardZone((8,9)) = false, want true (inside 5x3 rect at (10,10))")
	}
	if inHazardZone([2]float64{7, 10}, ts.HazardZones) {
		t.Error("inHazardZone((7,10)) = true, want false (outside rect)")
	}
}

func TestInHazardZoneInactiveIsSafe(t *testing.T) {
	hz := []entity{{Type: "hazard_zone", Position: [2]float64{5, 5}, Width: 3, Height: 3, Active: false}}
	if inHazardZone([2]float64{5, 5}, hz) {
		t.Error("inactive hazard zone should be safe")
	}
}

func TestBFSStepDangerAware(t *testing.T) {
	setTerrain(t, 12, 12, nil)

	danger := &dangerSet{}
	danger.reset()
	// Wall of danger cells across the straight path from (0,5) to (4,5).
	danger.add(1, 4)
	danger.add(1, 5)
	danger.add(1, 6)

	step := bfsStep(0, 5, 4, 5, danger)
	if step == [2]int{0, 0} {
		t.Fatal("bfsStep returned no step")
	}
	if danger.has(0+step[0], 5+step[1]) {
		t.Errorf("bfsStep stepped into a danger cell: %v", step)
	}

	// Without danger the step heads straight toward the goal (BFS ties may
	// pick a diagonal, but the x component must advance).
	step = bfsStep(0, 5, 4, 5, nil)
	if step[0] != 1 {
		t.Errorf("undangered bfsStep = %v, want x step of 1", step)
	}

	// Fully surrounded by danger: must still move (falls back through danger).
	surrounded := &dangerSet{}
	surrounded.reset()
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			if dx == 0 && dy == 0 {
				continue
			}
			surrounded.add(5+dx, 5+dy)
		}
	}
	step = bfsStep(5, 5, 9, 5, surrounded)
	if step == [2]int{0, 0} {
		t.Error("fully surrounded bot froze; want fallback step through danger")
	}
}

func TestBFSStepStagesOutsideDangerousGoal(t *testing.T) {
	setTerrain(t, 12, 12, nil)

	danger := &dangerSet{}
	danger.reset()
	danger.add(3, 5) // target/flag is inside a burn field

	step := bfsStep(2, 5, 3, 5, danger)
	if step == [2]int{0, 0} {
		t.Fatal("pathfinder froze instead of staging beside dangerous goal")
	}
	if danger.has(2+step[0], 5+step[1]) {
		t.Fatalf("pathfinder stepped directly into dangerous goal: %v", step)
	}
}

func TestBestTargetPreference(t *testing.T) {
	pos := [2]float64{0, 0}

	// Bounty target outranks a nearer plain enemy.
	ts := &tickState{BountyTargetID: "b2"}
	enemies := []entity{
		{ID: "b1", Position: [2]float64{1, 0}, HP: 100, MaxHP: 150, IsAlive: true, HasLOS: true},
		{ID: "b2", Position: [2]float64{3, 0}, HP: 100, MaxHP: 150, IsAlive: true, HasLOS: true},
	}
	got := bestTarget(ts, pos, enemies, 8)
	if got == nil || got.ID != "b2" {
		t.Errorf("bestTarget with bounty = %+v, want b2", got)
	}

	// Enemy flag carrier outranks a nearer plain enemy in CTF.
	ts = &tickState{
		Mode:  "ctf",
		Flags: []entity{{Type: "flag", ID: "flag_1", Team: 1, Status: "carried", CarrierID: "b3"}},
	}
	enemies = []entity{
		{ID: "b1", Position: [2]float64{1, 0}, HP: 100, MaxHP: 150, IsAlive: true, HasLOS: true},
		{ID: "b3", Position: [2]float64{3, 0}, HP: 100, MaxHP: 150, IsAlive: true, HasLOS: true},
	}
	got = bestTarget(ts, pos, enemies, 8)
	if got == nil || got.ID != "b3" {
		t.Errorf("bestTarget with carrier = %+v, want b3", got)
	}

	// No bounty/carrier: nearest low-HP logic still applies.
	ts = &tickState{}
	got = bestTarget(ts, pos, enemies, 8)
	if got == nil || got.ID != "b1" {
		t.Errorf("plain bestTarget = %+v, want b1", got)
	}
}

func TestBuildDangerSet(t *testing.T) {
	ts := &tickState{
		HazardZones: []entity{
			{Type: "hazard_zone", Position: [2]float64{10, 10}, Width: 3, Height: 3, Active: true},
			{Type: "hazard_zone", Position: [2]float64{20, 20}, Width: 3, Height: 3, Active: false},
			{Type: "burn_field", Position: [2]float64{30, 30}, Radius: 2, Active: true},
		},
		GravityWells: []entity{
			{Type: "gravity_well", ID: "g1", OwnerID: "me", PullRadius: 3, Position: [2]float64{40, 40}},
			{Type: "gravity_well", ID: "g2", OwnerID: "them", PullRadius: 2, Position: [2]float64{50, 50}},
		},
		Mines: []entity{
			{Type: "landmine", OwnerID: "them", Armed: true, Position: [2]float64{60, 60}},
			{Type: "landmine", OwnerID: "me", Armed: true, Position: [2]float64{70, 70}},
			{Type: "landmine", OwnerID: "them", Armed: false, Position: [2]float64{80, 80}},
		},
		VoidTiles: [][2]int{{90, 90}},
	}

	d := &dangerSet{}
	buildDangerSet(d, ts, "me")

	if !d.has(10, 10) || !d.has(11, 11) {
		t.Error("active hazard zone rect missing from danger set")
	}
	if d.has(20, 20) {
		t.Error("inactive hazard zone should not be dangerous")
	}
	if !d.has(30, 28) || !d.has(32, 32) {
		t.Error("burn field radius missing from danger set")
	}
	if d.has(40, 40) {
		t.Error("own gravity well should not be dangerous")
	}
	if !d.has(50, 48) {
		t.Error("enemy gravity well pull radius missing")
	}
	if !d.has(60, 61) {
		t.Error("armed enemy mine blast area missing")
	}
	if d.has(70, 70) {
		t.Error("own mine should not be dangerous")
	}
	if d.has(80, 80) {
		t.Error("unarmed mine should not be dangerous")
	}
	if !d.has(90, 90) {
		t.Error("void tile missing from danger set")
	}

	// Hazard key: zones and burn fields become safe, the rest stays dangerous.
	ts.HasHazardKey = true
	buildDangerSet(d, ts, "me")
	if d.has(10, 10) || d.has(30, 30) {
		t.Error("hazard key should clear hazard/burn danger")
	}
	if !d.has(90, 90) || !d.has(60, 60) {
		t.Error("hazard key must not clear void tiles or mines")
	}
}

func TestBuildActionPayloadPreservesChargedShot(t *testing.T) {
	action := actionResult{Action: "attack", Target: "enemy", Charged: true}
	payload := buildActionPayload(float64(17), action)

	if got, ok := payload["charged"].(bool); !ok || !got {
		t.Fatalf("charged action payload = %#v, want charged=true", payload)
	}
	if payload["target"] != "enemy" || payload["action"] != "attack" {
		t.Fatalf("action payload lost combat fields: %#v", payload)
	}

	plain := buildActionPayload(float64(18), actionResult{Action: "attack", Target: "enemy"})
	if _, ok := plain["charged"]; ok {
		t.Fatalf("plain attack unexpectedly serialized charged field: %#v", plain)
	}
}

func TestBestTargetFocusesAllyTarget(t *testing.T) {
	ts := &tickState{
		Allies: []entity{{ID: "ally", TargetID: "focus", HP: 100, MaxHP: 100}},
	}
	enemies := []entity{
		{ID: "near", Position: [2]float64{1, 0}, HP: 80, MaxHP: 100, IsAlive: true, HasLOS: true},
		{ID: "focus", Position: [2]float64{3, 0}, HP: 80, MaxHP: 100, IsAlive: true, HasLOS: true},
	}

	got := bestTarget(ts, [2]float64{0, 0}, enemies, 8)
	if got == nil || got.ID != "focus" {
		t.Fatalf("bestTarget = %+v, want ally's focus target", got)
	}
}

func TestRangedGrapplePreservesSpacing(t *testing.T) {
	ts := tickState{
		Position:        [2]float64{5, 5},
		HP:              100,
		MaxHP:           100,
		GrappleCharges:  1,
		GrappleCooldown: 0,
		Enemies: []entity{{
			ID: "melee", Position: [2]float64{15, 5}, HP: 100, MaxHP: 100,
			Weapon: "sword", IsAlive: true, HasLOS: true,
		}},
	}

	if got := tryUniversalGrapple(ts, "bow", 8); got != nil {
		t.Fatalf("bow pulled a healthy melee enemy into close range: %+v", *got)
	}

	ts.Enemies[0] = entity{
		ID: "ranged", Position: [2]float64{15, 5}, HP: 100, MaxHP: 100,
		Weapon: "bow", IsAlive: true, HasLOS: true,
	}
	got := tryUniversalGrapple(ts, "bow", 8)
	if got == nil || got.Action != "grapple" || got.Target != "ranged" {
		t.Fatalf("bow failed to disrupt out-of-range ranged enemy: %+v", got)
	}
}

func TestKiteUsesWeaponAppropriateSpacing(t *testing.T) {
	setTerrain(t, 20, 10, nil)
	danger := &dangerSet{}
	danger.reset()
	ts := tickState{
		Tick: 10, Position: [2]float64{5, 5}, HP: 100, MaxHP: 100,
		Danger: danger,
	}
	enemy := entity{
		ID: "melee", Position: [2]float64{10, 5}, HP: 100, MaxHP: 100,
		Weapon: "sword", IsAlive: true, HasLOS: true,
	}
	ts.Enemies = []entity{enemy}

	got := aiKite(ts, &ts.Enemies[0], 5, 8, "bow", false, false, "bow-bot")
	if got.Action != "move" || got.Direction == nil || got.Direction[0] != -1 || got.Direction[1] != 0 {
		t.Fatalf("bow cooldown spacing action = %+v, want move directly away", got)
	}
}

func TestSafeDodgeAvoidsBlockedDirection(t *testing.T) {
	setTerrain(t, 12, 12, [][2]int{{6, 5}})
	danger := &dangerSet{}
	danger.reset()

	got, ok := safeDodgeDir([2]float64{5, 5}, [2]float64{1, 0}, danger)
	if !ok {
		t.Fatal("safeDodgeDir reported no route despite open perpendicular cells")
	}
	if got != [2]float64{0, 1} {
		t.Fatalf("safeDodgeDir = %v, want deterministic perpendicular [0 1]", got)
	}
}

func TestSafeDodgeRefusesSurroundedDanger(t *testing.T) {
	setTerrain(t, 12, 12, nil)
	danger := &dangerSet{}
	danger.reset()
	for dx := -2; dx <= 2; dx++ {
		for dy := -2; dy <= 2; dy++ {
			if dx != 0 || dy != 0 {
				danger.add(5+dx, 5+dy)
			}
		}
	}

	if got, ok := safeDodgeDir([2]float64{5, 5}, [2]float64{1, 0}, danger); ok {
		t.Fatalf("safeDodgeDir returned unsafe surrounded route %v", got)
	}
	ts := tickState{Position: [2]float64{5, 5}, Danger: danger}
	if got := dodgeSafe(ts, [2]float64{1, 0}); got.Action != "idle" {
		t.Fatalf("dodgeSafe surrounded action = %+v, want idle", got)
	}
}

func TestPickActionDodgesTowardSafetyFromDeepHazard(t *testing.T) {
	setTerrain(t, 20, 20, nil)
	center := [2]float64{5, 5}
	msg := map[string]interface{}{
		"type": "tick",
		"tick": float64(15),
		"your_state": map[string]interface{}{
			"position":       []interface{}{center[0], center[1]},
			"hp":             float64(100),
			"max_hp":         float64(100),
			"dodge_cooldown": float64(0),
		},
		"nearby_entities": []interface{}{
			map[string]interface{}{
				"type": "hazard_zone", "position": []interface{}{center[0], center[1]},
				"width": float64(5), "height": float64(5), "active": true,
			},
		},
	}

	got := PickAction("aggressive", msg, "sword", 1, "me")
	if got.Action != "dodge" || got.Direction == nil {
		t.Fatalf("deep-hazard action = %+v, want a dodge toward safety", got)
	}

	danger := &dangerSet{}
	danger.reset()
	danger.addRect(center, 5, 5)
	startDistance := dangerEscapeDistance(5, 5, danger, getTerrain())
	endCol := 5 + 2*int(got.Direction[0])
	endRow := 5 + 2*int(got.Direction[1])
	endDistance := dangerEscapeDistance(endCol, endRow, danger, getTerrain())
	if endDistance >= startDistance {
		t.Fatalf("deep-hazard dodge %v changed escape distance %d -> %d, want a measurable reduction",
			*got.Direction, startDistance, endDistance)
	}
}

func TestPickActionEscapesActiveHazardWhenInactiveHazardIsCloser(t *testing.T) {
	setTerrain(t, 20, 20, nil)
	msg := map[string]interface{}{
		"type": "tick",
		"tick": float64(15),
		"your_state": map[string]interface{}{
			"position":       []interface{}{float64(5), float64(5)},
			"hp":             float64(100),
			"max_hp":         float64(100),
			"dodge_cooldown": float64(20),
		},
		"nearby_entities": []interface{}{
			map[string]interface{}{
				"type": "hazard_zone", "position": []interface{}{float64(7), float64(5)},
				"width": float64(5), "height": float64(5), "active": true,
			},
			map[string]interface{}{
				"type": "hazard_zone", "position": []interface{}{float64(5), float64(5)},
				"width": float64(3), "height": float64(3), "active": false,
			},
		},
	}

	got := PickAction("aggressive", msg, "sword", 1, "me")
	if got.Action != "move" || got.Direction == nil || *got.Direction == [2]float64{} {
		t.Fatalf("hazard escape action = %+v, want a non-zero move", got)
	}
	if got.Direction[0] != -1 {
		t.Fatalf("hazard escape direction = %v, want left toward nearest active-zone exit", *got.Direction)
	}
}

func TestCapturePadOwnerHoldsForControlPulse(t *testing.T) {
	danger := &dangerSet{}
	danger.reset()
	ts := tickState{
		Position: [2]float64{5, 5}, HP: 100, MaxHP: 100, InZone: true, Danger: danger,
		CapturePads: []entity{{
			ID: "pad", Position: [2]float64{5, 5}, Ready: false, OwnerID: "me",
		}},
	}

	got := tryCapturePadObjective(ts, "territorial", nil, 0, "me")
	if got == nil || got.Action != "idle" {
		t.Fatalf("captured-pad action = %+v, want idle to hold the control pulse", got)
	}
}

func TestOwnedCoolingCapturePadYieldsToOpponentHint(t *testing.T) {
	setTerrain(t, 20, 20, nil)
	msg := map[string]interface{}{
		"type": "tick",
		"tick": float64(200),
		"your_state": map[string]interface{}{
			"position": []interface{}{float64(5), float64(5)},
			"hp":       float64(100), "max_hp": float64(100),
			"speed": float64(5.5), "weapon_ready": false,
			"dodge_cooldown": float64(20), "shove_cooldown": float64(20),
			"in_safe_zone": true, "distance_to_zone_edge": float64(10),
		},
		"safe_zone": map[string]interface{}{
			"center": []interface{}{float64(5), float64(5)}, "radius": float64(10),
			"target_center": []interface{}{float64(5), float64(5)}, "target_radius": float64(6),
		},
		"nearby_entities": []interface{}{
			map[string]interface{}{
				"type": "capture_pad", "id": "owned-pad",
				"position": []interface{}{float64(5), float64(5)},
				"radius":   float64(2), "is_ready": false,
				"owner_id": "me", "is_contested": false,
			},
		},
		"hints": []interface{}{
			map[string]interface{}{
				"hint_type": "bot", "direction": []interface{}{float64(1), float64(0)},
				"distance": float64(10),
			},
		},
	}

	got := PickAction("territorial", msg, "shield", 1, "me")
	if got.Action != "move" || got.Direction == nil || got.Direction[0] <= 0 {
		t.Fatalf("owned cooling pad ignored known opponent: %+v", got)
	}
}

func TestMoveDirSafeChecksTerrainWithoutDynamicDanger(t *testing.T) {
	setTerrain(t, 20, 20, [][2]int{{6, 5}})
	danger := &dangerSet{}
	danger.reset()
	ts := tickState{Position: [2]float64{5, 5}, Danger: danger}

	got := moveDirSafe(ts, [2]float64{1, 0})
	if got.Action != "move" || got.Direction == nil {
		t.Fatalf("safe move beside wall = %+v, want a passable move", got)
	}
	terrain := getTerrain()
	if terrain.isMoveBlocked(5, 5, int(got.Direction[0]), int(got.Direction[1])) {
		t.Fatalf("safe move still selected blocked direction %v", *got.Direction)
	}
}

func TestCapturePadChoosesReadyPadOverNearEnemyCooldown(t *testing.T) {
	setTerrain(t, 20, 20, nil)
	danger := &dangerSet{}
	danger.reset()
	ts := tickState{
		Position: [2]float64{5, 5}, HP: 100, MaxHP: 100, InZone: true, Danger: danger,
		CapturePads: []entity{
			{ID: "cooldown", Position: [2]float64{6, 5}, Ready: false, OwnerID: "enemy"},
			{ID: "ready", Position: [2]float64{10, 5}, Ready: true},
		},
	}

	got := tryCapturePadObjective(ts, "territorial", nil, 0, "me")
	if got == nil || got.Action != "move" || got.Direction == nil || got.Direction[0] != 1 {
		t.Fatalf("capture-pad action = %+v, want movement toward ready pad", got)
	}
}

func TestCapturePadContenderStaysUntilCaptureCompletes(t *testing.T) {
	danger := &dangerSet{}
	danger.reset()
	ts := tickState{
		Position: [2]float64{5, 5}, HP: 100, MaxHP: 100, InZone: true, Danger: danger,
		CapturePads: []entity{{
			ID: "ready", Position: [2]float64{5, 5}, Ready: true,
			CapturingBotID: "me", ProgressTicks: 10, CaptureTicks: 30,
		}},
	}

	got := tryCapturePadObjective(ts, "territorial", nil, 0, "me")
	if got == nil || got.Action != "idle" {
		t.Fatalf("in-progress capture action = %+v, want idle until capture completes", got)
	}
}

func TestPickActionUsesEmergencyHealthBeforeUtility(t *testing.T) {
	setTerrain(t, 20, 20, nil)
	msg := map[string]interface{}{
		"type": "tick",
		"tick": float64(12),
		"your_state": map[string]interface{}{
			"position": []interface{}{float64(5), float64(5)},
			"hp":       float64(20), "max_hp": float64(100),
			"weapon_ready":    false,
			"dodge_cooldown":  float64(20),
			"grapple_charges": float64(0),
		},
		"nearby_entities": []interface{}{
			map[string]interface{}{
				"type": "bot", "id": "enemy", "position": []interface{}{float64(6), float64(5)},
				"hp": float64(100), "max_hp": float64(100), "weapon": "sword",
				"is_alive": true, "has_los": true, "can_attack": true,
			},
			map[string]interface{}{
				"type": "pickup", "id": "heal", "pickup_type": "health_pack",
				"position": []interface{}{float64(5), float64(5)},
			},
		},
	}

	got := PickAction("aggressive", msg, "sword", 1, "me")
	if got.Action != "use_item" || got.ItemID != "heal" {
		t.Fatalf("critical bot action = %+v, want immediate health pickup", got)
	}
}

func TestCriticalBotPrioritizesReachableHealthOverUtilityPickup(t *testing.T) {
	setTerrain(t, 20, 20, nil)
	msg := map[string]interface{}{
		"type": "tick",
		"tick": float64(13),
		"your_state": map[string]interface{}{
			"position": []interface{}{float64(5), float64(5)},
			"hp":       float64(25), "max_hp": float64(100),
			"weapon_ready": false, "dodge_cooldown": float64(20),
		},
		"nearby_entities": []interface{}{
			map[string]interface{}{
				"type": "pickup", "id": "utility", "pickup_type": "gravity_well",
				"position": []interface{}{float64(5), float64(7)},
			},
			map[string]interface{}{
				"type": "pickup", "id": "heal", "pickup_type": "health_pack",
				"position": []interface{}{float64(9), float64(5)},
			},
		},
	}

	got := PickAction("aggressive", msg, "sword", 1, "me")
	if got.Action != "move" || got.Direction == nil || got.Direction[0] <= 0 {
		t.Fatalf("critical bot health route = %+v, want movement toward health", got)
	}
	// Assert the decision at the pickup-ranking seam as well.
	ts := parseTick(msg)
	danger := &dangerSet{}
	danger.reset()
	ts.Danger = danger
	pickup := trySmartPickup(ts, "aggressive", "sword")
	if pickup == nil || pickup.Action != "move" {
		t.Fatalf("critical health decision = %+v, want movement", pickup)
	}
	healthDirection := moveTo(ts.Position, [2]float64{9, 5}, danger)
	if pickup.Direction == nil || healthDirection.Direction == nil || *pickup.Direction != *healthDirection.Direction {
		t.Fatalf("critical health decision = %+v, want route toward health pack", pickup)
	}
}

func TestSmartPickupSkipsUtilityBehindBlockedDangerRoute(t *testing.T) {
	walls := make([][2]int, 0, 40)
	for col := 0; col < 20; col++ {
		walls = append(walls, [2]int{col, 0}, [2]int{col, 2})
	}
	setTerrain(t, 20, 3, walls)

	danger := &dangerSet{}
	danger.reset()
	danger.add(7, 1)
	ts := tickState{
		Position: [2]float64{5, 1}, HP: 100, MaxHP: 100, InZone: true,
		Pickups: []entity{{
			ID: "blocked-utility", Type: "pickup", SubType: "gravity_well", Position: [2]float64{9, 1},
		}},
		Danger: danger,
	}

	if got := trySmartPickup(ts, "aggressive", "sword"); got != nil {
		t.Fatalf("smart pickup chased an unreachable utility through lethal danger: %+v", *got)
	}
}

func TestCriticalBotDoesNotFleeTowardHealthBehindBlockedDangerRoute(t *testing.T) {
	walls := make([][2]int, 0, 40)
	for col := 0; col < 20; col++ {
		walls = append(walls, [2]int{col, 0}, [2]int{col, 2})
	}
	setTerrain(t, 20, 3, walls)

	msg := map[string]interface{}{
		"type": "tick",
		"tick": float64(14),
		"your_state": map[string]interface{}{
			"position": []interface{}{float64(5), float64(1)},
			"hp":       float64(20), "max_hp": float64(100),
			"weapon_ready": false, "dodge_cooldown": float64(20),
			"in_safe_zone": true,
		},
		"nearby_entities": []interface{}{
			map[string]interface{}{
				"type": "bot", "id": "enemy", "position": []interface{}{float64(13), float64(1)},
				"hp": float64(100), "max_hp": float64(100), "weapon": "bow",
				"is_alive": true, "has_los": true, "can_attack": true,
			},
			map[string]interface{}{
				"type": "hazard_zone", "position": []interface{}{float64(7), float64(1)},
				"width": float64(1), "height": float64(1), "active": true,
			},
			map[string]interface{}{
				"type": "pickup", "id": "blocked-heal", "pickup_type": "health_pack",
				"position": []interface{}{float64(9), float64(1)},
			},
		},
	}

	got := PickAction("defensive", msg, "sword", 1, "me")
	if got.Action != "move" || got.Direction == nil || got.Direction[0] >= 0 {
		t.Fatalf("critical flee action = %+v, want movement away from threat instead of toward blocked health", got)
	}
}

func TestSmartPickupRoutesBuildsSharedFieldWithoutDanger(t *testing.T) {
	walls := make([][2]int, 0, 5)
	for row := 2; row <= 6; row++ {
		walls = append(walls, [2]int{5, row})
	}
	setTerrain(t, 20, 20, walls)

	routes := newSmartPickupRoutes([2]float64{3, 4}, nil)
	defer routes.release()
	if routes.scratch == nil {
		t.Fatal("empty-danger pickup routing did not build a shared distance field")
	}
	if got := routes.distance([2]float64{7, 4}); got != 8 {
		t.Fatalf("wall-separated pickup distance = %.0f, want 8", got)
	}
}

func BenchmarkTrySmartPickupRouteField(b *testing.B) {
	walls := make([][2]int, 0, 30)
	for row := 0; row < 30; row++ {
		walls = append(walls, [2]int{10, row})
	}
	setTerrain(b, 30, 30, walls)
	danger := &dangerSet{}
	danger.reset()
	danger.add(7, 15)

	subTypes := []string{
		"health_pack", "gravity_well", "cooldown_shard", "hazard_key",
		"relay_battery", "overdrive_core", "grapple_charge", "damage_boost",
		"bounty_token", "speed_boost", "shield_bubble",
	}
	pickups := make([]entity, 0, 88)
	for i := 0; i < 88; i++ {
		pickups = append(pickups, entity{
			ID:       fmt.Sprintf("pickup-%d", i),
			Type:     "pickup",
			SubType:  subTypes[i%len(subTypes)],
			Position: [2]float64{float64(12 + i%5), float64(10 + (i/5)%11)},
		})
	}

	for _, benchmark := range []struct {
		name   string
		danger *dangerSet
	}{
		{name: "empty_danger_wall_separated"},
		{name: "live_danger_wall_separated", danger: danger},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			ts := tickState{
				Position: [2]float64{5, 15}, HP: 100, MaxHP: 100,
				Pickups: pickups, Danger: benchmark.danger,
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = trySmartPickup(ts, "defensive", "sword")
			}
		})
	}
}

func TestPickActionEscapesBeforeOffensiveGrapple(t *testing.T) {
	setTerrain(t, 30, 20, nil)
	msg := map[string]interface{}{
		"type": "tick",
		"tick": float64(20),
		"your_state": map[string]interface{}{
			"position": []interface{}{float64(10), float64(10)},
			"hp":       float64(35), "max_hp": float64(100),
			"weapon_ready":    false,
			"grapple_charges": float64(1), "grapple_cooldown": float64(0),
		},
		"nearby_entities": []interface{}{
			map[string]interface{}{
				"type": "bot", "id": "enemy", "position": []interface{}{float64(12), float64(10)},
				"hp": float64(100), "max_hp": float64(100), "weapon": "sword",
				"is_alive": true, "has_los": true, "can_attack": true,
			},
		},
	}

	got := PickAction("aggressive", msg, "sword", 1, "me")
	if got.Action != "grapple" || got.Target != "" || got.TargetPosition == nil {
		t.Fatalf("low-HP grapple action = %+v, want anchor escape rather than enemy pull", got)
	}
	if got.TargetPosition[0] >= 10 {
		t.Fatalf("anchor %v did not move away from enemy at x=12", *got.TargetPosition)
	}
}

func TestPickActionWaitsForSafeSpearBrace(t *testing.T) {
	setTerrain(t, 20, 20, nil)
	msg := map[string]interface{}{
		"type": "tick",
		"tick": float64(30),
		"your_state": map[string]interface{}{
			"position": []interface{}{float64(5), float64(5)},
			"hp":       float64(100), "max_hp": float64(100),
			"weapon_ready": true, "brace_ready": false,
		},
		"nearby_entities": []interface{}{
			map[string]interface{}{
				"type": "bot", "id": "enemy", "position": []interface{}{float64(7), float64(5)},
				"hp": float64(100), "max_hp": float64(100), "weapon": "sword",
				"is_alive": true, "has_los": true, "can_attack": true,
			},
		},
	}

	got := PickAction("aggressive", msg, "spear", 2, "me")
	if got.Action != "idle" {
		t.Fatalf("safe spear setup action = %+v, want idle to build brace", got)
	}
}

func TestGuardStrategiesKeepPatrollingInsideTheirZone(t *testing.T) {
	for _, strategy := range []string{"defensive", "territorial"} {
		t.Run(strategy, func(t *testing.T) {
			setTerrain(t, 40, 40, nil)
			position := [2]float64{20, 20}
			center := [2]float64{20, 20}
			stationaryTicks := 0
			maxStationaryTicks := 0

			for tick := 1; tick <= 240; tick++ {
				msg := map[string]interface{}{
					"type":       "tick",
					"tick":       float64(tick),
					"round_tick": float64(tick),
					"your_state": map[string]interface{}{
						"position":              []interface{}{position[0], position[1]},
						"hp":                    float64(100),
						"max_hp":                float64(100),
						"weapon_ready":          false,
						"dodge_cooldown":        float64(20),
						"shove_cooldown":        float64(20),
						"in_safe_zone":          true,
						"distance_to_zone_edge": float64(10),
					},
					"safe_zone": map[string]interface{}{
						"center":        []interface{}{center[0], center[1]},
						"radius":        float64(10),
						"target_center": []interface{}{center[0], center[1]},
						"target_radius": float64(5),
					},
					"nearby_entities": []interface{}{
						map[string]interface{}{
							"type": "bot", "id": "far-enemy",
							"position": []interface{}{float64(33), float64(33)},
							"hp":       float64(100), "max_hp": float64(100),
							"weapon": "sword", "is_alive": true,
							"has_los": true, "can_attack": false,
						},
					},
				}

				action := PickAction(strategy, msg, "sword", 1, "guard-"+strategy)
				if action.Action == "move" && action.Direction != nil {
					position[0] += action.Direction[0]
					position[1] += action.Direction[1]
					stationaryTicks = 0
				} else {
					stationaryTicks++
					if stationaryTicks > maxStationaryTicks {
						maxStationaryTicks = stationaryTicks
					}
				}

				if got := chebyshev(position, center); got > 5 {
					t.Fatalf("tick %d moved outside guarded target zone: position=%v distance=%v action=%+v", tick, position, got, action)
				}
			}

			if maxStationaryTicks > 0 {
				t.Fatalf("guard stopped for %d consecutive ticks; position=%v", maxStationaryTicks, position)
			}
		})
	}
}

func TestNoVisibleEnemyAtZoneCenterKeepsHunting(t *testing.T) {
	setTerrain(t, 40, 40, nil)
	for _, strategy := range []string{"aggressive", "berserker", "kite", "assassin", "defensive", "territorial"} {
		t.Run(strategy, func(t *testing.T) {
			msg := map[string]interface{}{
				"type":       "tick",
				"tick":       float64(500),
				"round_tick": float64(500),
				"your_state": map[string]interface{}{
					"position":              []interface{}{float64(20), float64(20)},
					"hp":                    float64(100),
					"max_hp":                float64(100),
					"speed":                 float64(5.5),
					"weapon_ready":          false,
					"dodge_cooldown":        float64(20),
					"shove_cooldown":        float64(20),
					"in_safe_zone":          true,
					"distance_to_zone_edge": float64(10),
				},
				"safe_zone": map[string]interface{}{
					"center":        []interface{}{float64(20), float64(20)},
					"radius":        float64(10),
					"target_center": []interface{}{float64(20), float64(20)},
					"target_radius": float64(6),
				},
			}

			got := PickAction(strategy, msg, "daggers", 1, "hunter-"+strategy)
			if got.Action != "move" || got.Direction == nil || *got.Direction == [2]float64{} {
				t.Fatalf("no-target center action = %+v, want a hunting patrol move", got)
			}
		})
	}
}

func TestNoVisibleEnemyPatrolDoesNotReverseBackToCenter(t *testing.T) {
	setTerrain(t, 40, 40, nil)
	position := [2]float64{20, 20}
	var previousDirection [2]float64
	seen := map[[2]float64]struct{}{position: {}}

	for tick := 1; tick <= 30; tick++ {
		msg := map[string]interface{}{
			"type": "tick", "tick": float64(tick), "round_tick": float64(tick),
			"your_state": map[string]interface{}{
				"position": []interface{}{position[0], position[1]},
				"hp":       float64(100), "max_hp": float64(100), "speed": float64(5.5),
				"weapon_ready": false, "dodge_cooldown": float64(20), "shove_cooldown": float64(20),
				"in_safe_zone": true, "distance_to_zone_edge": float64(10),
			},
			"safe_zone": map[string]interface{}{
				"center": []interface{}{float64(20), float64(20)}, "radius": float64(10),
				"target_center": []interface{}{float64(20), float64(20)}, "target_radius": float64(6),
			},
		}
		got := PickAction("territorial", msg, "shield", 1, "patrol-hunter")
		if got.Action != "move" || got.Direction == nil || *got.Direction == [2]float64{} {
			t.Fatalf("tick %d patrol action = %+v, want movement", tick, got)
		}
		if tick > 1 && *got.Direction == [2]float64{-previousDirection[0], -previousDirection[1]} {
			t.Fatalf("tick %d reversed the previous patrol step: previous=%v current=%v", tick, previousDirection, *got.Direction)
		}
		previousDirection = *got.Direction
		position[0] += got.Direction[0]
		position[1] += got.Direction[1]
		seen[position] = struct{}{}
	}
	if len(seen) <= 2 {
		t.Fatalf("patrol visited only %d positions: %v", len(seen), seen)
	}
}

func TestGuardPatrolShrinksBlockedWaypointRing(t *testing.T) {
	center := [2]float64{15, 15}
	walls := make([][2]int, 0, len(guardPatrolDirections))
	for _, direction := range guardPatrolDirections {
		walls = append(walls, [2]int{
			int(center[0] + direction[0]*3),
			int(center[1] + direction[1]*3),
		})
	}
	setTerrain(t, 30, 30, walls)
	danger := &dangerSet{}
	danger.reset()
	ts := tickState{
		Position: center, HP: 100, MaxHP: 100, InZone: true, Danger: danger,
		ZoneCenter: center, ZoneRadius: 8, ZoneTargetCenter: center, ZoneTargetRadius: 5,
	}

	got := guardPatrol(ts, 3, "blocked-ring-guard")
	if got.Action != "move" || got.Direction == nil || *got.Direction == [2]float64{} {
		t.Fatalf("blocked outer patrol ring stopped despite local room: %+v", got)
	}
}

func TestGuardPatrolDoesNotReverseAtWaypointMidpoints(t *testing.T) {
	setTerrain(t, 50, 50, nil)
	danger := &dangerSet{}
	danger.reset()
	center := [2]float64{25, 25}
	position := center
	var previousDirection [2]float64

	for step := 1; step <= 80; step++ {
		ts := tickState{
			Position: position, HP: 100, MaxHP: 100, InZone: true, Danger: danger,
			ZoneCenter: center, ZoneRadius: 10, ZoneTargetCenter: center, ZoneTargetRadius: 6,
		}
		got := guardPatrol(ts, 4, "midpoint-guard")
		if got.Action != "move" || got.Direction == nil || *got.Direction == [2]float64{} {
			t.Fatalf("step %d patrol stopped: %+v", step, got)
		}
		if step > 1 && *got.Direction == [2]float64{-previousDirection[0], -previousDirection[1]} {
			t.Fatalf("step %d patrol reversed at a waypoint midpoint: previous=%v current=%v position=%v", step, previousDirection, *got.Direction, position)
		}
		previousDirection = *got.Direction
		position[0] += got.Direction[0]
		position[1] += got.Direction[1]
		if distance := chebyshev(position, center); distance > ts.ZoneTargetRadius {
			t.Fatalf("step %d patrol escaped target zone: position=%v distance=%v", step, position, distance)
		}
	}
}

func cacheTeleportTestMap(t *testing.T, pads []interface{}, hazards []interface{}) {
	t.Helper()
	oldBudget := config.C.StatBudget
	oldSpeedBase := config.C.StatSpeedBase
	oldSpeedPerPoint := config.C.StatSpeedPerPoint
	oldCollectRadius := config.C.TeleportCollectRadius
	config.C.StatBudget = 20
	config.C.StatSpeedBase = 3
	config.C.StatSpeedPerPoint = 0.5
	config.C.TeleportCollectRadius = 1
	t.Cleanup(func() {
		config.C.StatBudget = oldBudget
		config.C.StatSpeedBase = oldSpeedBase
		config.C.StatSpeedPerPoint = oldSpeedPerPoint
		config.C.TeleportCollectRadius = oldCollectRadius
	})
	rows := make([]interface{}, 24)
	for y := range rows {
		row := make([]byte, 24)
		for x := range row {
			row[x] = '.'
		}
		rows[y] = string(row)
	}
	parseTerrain(map[string]interface{}{
		"width":         float64(24),
		"height":        float64(24),
		"cell_size":     float64(20),
		"terrain":       rows,
		"teleport_pads": pads,
		"hazard_zones":  hazards,
	})
	t.Cleanup(clearTerrain)
}

func teleportTick(position [2]float64, hp, speed float64, entities ...map[string]interface{}) map[string]interface{} {
	nearby := make([]interface{}, len(entities))
	for i := range entities {
		nearby[i] = entities[i]
	}
	return map[string]interface{}{
		"type":       "tick",
		"tick":       float64(500),
		"round_tick": float64(500),
		"your_state": map[string]interface{}{
			"position":              []interface{}{position[0], position[1]},
			"hp":                    hp,
			"max_hp":                float64(100),
			"speed":                 speed,
			"weapon_ready":          false,
			"dodge_cooldown":        float64(20),
			"in_safe_zone":          true,
			"distance_to_zone_edge": float64(8),
			"zone_center":           []interface{}{float64(12), float64(12)},
			"zone_radius":           float64(12),
			"zone_target_center":    []interface{}{float64(12), float64(12)},
			"zone_target_radius":    float64(5),
		},
		"nearby_entities": nearby,
	}
}

func TestParseTerrainCachesTeleportLinksAndStaticHazards(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{
			"type": "teleport_pad", "id": "a", "linked_pad_id": "b",
			"position": []interface{}{float64(8), float64(10)}, "is_ready": true,
		},
		map[string]interface{}{
			"type": "teleport_pad", "id": "b", "linked_pad_id": "a",
			"position": []interface{}{float64(18), float64(16)}, "is_ready": true,
		},
	}, []interface{}{
		map[string]interface{}{
			"type": "hazard_zone", "id": "hz", "position": []interface{}{float64(18), float64(16)},
			"width": float64(3), "height": float64(3),
		},
	})

	terrain := getTerrain()
	if terrain == nil || len(terrain.Teleporters) != 2 {
		t.Fatalf("cached teleporters = %#v, want two linked pads", terrain)
	}
	if got := terrain.Teleporters["a"]; got.LinkedID != "b" || got.Position != [2]float64{8, 10} {
		t.Fatalf("cached pad a = %+v", got)
	}
	if len(terrain.HazardZones) != 1 || terrain.HazardZones[0].Position != [2]float64{18, 16} {
		t.Fatalf("cached static hazards = %+v", terrain.HazardZones)
	}
}

func TestExplicitlyUnreadyTeleporterNeverCountsAsReady(t *testing.T) {
	setTerrain(t, 20, 20, nil)
	ts := parseTick(teleportTick([2]float64{5, 5}, 100, 5.5,
		map[string]interface{}{
			"type": "teleport_pad", "id": "a", "linked_pad_id": "b",
			"position": []interface{}{float64(7), float64(5)},
			"is_ready": false, "cooldown_remaining_ticks": float64(0),
		},
	))
	if len(ts.Teleporters) != 1 {
		t.Fatalf("teleporters = %+v", ts.Teleporters)
	}
	if isReadyTeleporter(ts.Teleporters[0]) {
		t.Fatal("explicit is_ready=false pad counted as ready")
	}
}

func TestReadyTeleporterAvoidanceCoversBoostedMultiCellMove(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "a", "linked_pad_id": "b", "position": []interface{}{float64(8), float64(10)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "b", "linked_pad_id": "a", "position": []interface{}{float64(18), float64(18)}, "is_ready": true},
	}, nil)
	msg := teleportTick([2]float64{4, 10}, 100, 16,
		map[string]interface{}{
			"type": "bot", "id": "enemy", "position": []interface{}{float64(11), float64(10)},
			"hp": float64(100), "max_hp": float64(100), "weapon": "sword", "is_alive": true, "has_los": true,
		},
		map[string]interface{}{
			"type": "teleport_pad", "id": "a", "linked_pad_id": "b",
			"position": []interface{}{float64(8), float64(10)}, "is_ready": true,
		},
	)

	got := PickAction("aggressive", msg, "sword", 1, "me")
	if got.Action != "move" || got.Direction == nil {
		t.Fatalf("action = %+v, want a routed move", got)
	}
	pos := [2]int{4, 10}
	pad := [2]int{8, 10}
	for step := 0; step < 2; step++ { // speed 16 can execute two cells in one server tick
		pos[0] += int(got.Direction[0])
		pos[1] += int(got.Direction[1])
		if intChebyshev(pos, pad) <= 1 {
			t.Fatalf("boosted move crossed ready pad trigger at step %d: pos=%v action=%+v", step+1, pos, got)
		}
	}
}

func TestReadyTeleporterFootprintAllowsOutwardEgress(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "a", "linked_pad_id": "b", "position": []interface{}{float64(8), float64(10)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "b", "linked_pad_id": "a", "position": []interface{}{float64(18), float64(18)}, "is_ready": true},
	}, nil)
	pad := [2]int{8, 10}

	for _, start := range [][2]float64{{8, 10}, {7, 10}} {
		t.Run(fmt.Sprintf("start_%d_%d", int(start[0]), int(start[1])), func(t *testing.T) {
			msg := teleportTick(start, 100, 5.5,
				map[string]interface{}{
					"type": "teleport_pad", "id": "a", "linked_pad_id": "b",
					"position": []interface{}{float64(8), float64(10)}, "is_ready": true,
				},
			)

			got := PickAction("aggressive", msg, "sword", 1, "me")
			if got.Action != "move" || got.Direction == nil {
				t.Fatalf("action from ready pad footprint = %+v, want an outward move", got)
			}
			from := [2]int{int(start[0]), int(start[1])}
			next := [2]int{from[0] + int(got.Direction[0]), from[1] + int(got.Direction[1])}
			if intChebyshev(next, pad) <= intChebyshev(from, pad) {
				t.Fatalf("ready-pad egress moved %v -> %v toward pad %v", from, next, pad)
			}
		})
	}
}

func TestUnreadyTeleporterDoesNotCreateRoutingDetour(t *testing.T) {
	setTerrain(t, 20, 20, nil)
	msg := teleportTick([2]float64{4, 10}, 100, 5.5,
		map[string]interface{}{
			"type": "bot", "id": "enemy", "position": []interface{}{float64(10), float64(10)},
			"hp": float64(100), "max_hp": float64(100), "weapon": "sword", "is_alive": true, "has_los": true,
		},
		map[string]interface{}{
			"type": "teleport_pad", "id": "a", "linked_pad_id": "b",
			"position": []interface{}{float64(7), float64(10)},
			"is_ready": false, "cooldown_remaining_ticks": float64(12),
		},
	)
	ts := parseTick(msg)
	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	buildDangerSet(danger, &ts, "me")
	if danger.hasPad(7, 10) {
		t.Fatal("unready pad was added to the ordinary-routing avoidance set")
	}
}

func TestTeleporterPressureEscapeRequiresCriticalRisk(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "a", "linked_pad_id": "b", "position": []interface{}{float64(8), float64(10)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "b", "linked_pad_id": "a", "position": []interface{}{float64(18), float64(18)}, "is_ready": true},
	}, nil)
	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	danger.reset()
	ts := tickState{
		Position: [2]float64{6, 10}, HP: 60, MaxHP: 100, InZone: true,
		ZoneCenter: [2]float64{12, 12}, ZoneRadius: 12, Speed: 5.5, Danger: danger,
		Enemies:     []entity{{ID: "enemy", Type: "bot", Position: [2]float64{4, 10}, IsAlive: true}},
		Teleporters: []entity{{ID: "a", Type: "teleport_pad", LinkedID: "b", Position: [2]float64{8, 10}, Ready: true}},
	}
	if got := tryTeleporterPressureEscape(ts, "kite", &ts.Enemies[0], 2); got != nil {
		t.Fatalf("moderate pressure triggered teleporter escape: %+v", *got)
	}
}

func TestCriticalTeleporterEscapeRejectsUnsafeKnownExit(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "a", "linked_pad_id": "b", "position": []interface{}{float64(8), float64(10)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "b", "linked_pad_id": "a", "position": []interface{}{float64(4), float64(10)}, "is_ready": true},
	}, []interface{}{
		map[string]interface{}{"type": "hazard_zone", "id": "hz", "position": []interface{}{float64(4), float64(10)}, "width": float64(3), "height": float64(3)},
	})
	msg := teleportTick([2]float64{6, 10}, 20, 5.5,
		map[string]interface{}{
			"type": "bot", "id": "enemy", "position": []interface{}{float64(3), float64(10)},
			"hp": float64(100), "max_hp": float64(100), "weapon": "sword", "is_alive": true, "has_los": true, "can_attack": true,
		},
		map[string]interface{}{
			"type": "teleport_pad", "id": "a", "linked_pad_id": "b",
			"position": []interface{}{float64(8), float64(10)}, "is_ready": true,
		},
	)
	got := PickAction("kite", msg, "bow", 5, "me")
	if got.Action == "move" && got.Direction != nil && got.Direction[0] > 0 {
		t.Fatalf("critical bot moved toward pad with hazardous/enemy-adjacent exit: %+v", got)
	}
}

func TestCriticalTeleporterEscapeUsesSafeLinkedExitOutsideFog(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "a", "linked_pad_id": "b", "position": []interface{}{float64(8), float64(10)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "b", "linked_pad_id": "a", "position": []interface{}{float64(18), float64(18)}, "is_ready": true},
	}, nil)
	// The live tick contains only the source pad. Its linked exit is ten-plus
	// tiles away and therefore must come from the full map cache, not fog data.
	msg := teleportTick([2]float64{6, 10}, 20, 5.5,
		map[string]interface{}{
			"type": "bot", "id": "enemy", "position": []interface{}{float64(3), float64(10)},
			"hp": float64(100), "max_hp": float64(100), "weapon": "sword", "is_alive": true, "has_los": true, "can_attack": true,
		},
		map[string]interface{}{
			"type": "teleport_pad", "id": "a", "linked_pad_id": "b",
			"position": []interface{}{float64(8), float64(10)}, "is_ready": true,
		},
	)
	got := PickAction("kite", msg, "bow", 5, "me")
	if got.Action != "move" || got.Direction == nil || got.Direction[0] <= 0 {
		t.Fatalf("critical bot did not move toward safe known teleporter: %+v", got)
	}
}

func TestDeliberateTeleportNeverUnblocksLethalPadCell(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "a", "linked_pad_id": "b", "position": []interface{}{float64(8), float64(10)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "b", "linked_pad_id": "a", "position": []interface{}{float64(18), float64(18)}, "is_ready": true},
	}, nil)
	msg := teleportTick([2]float64{6, 10}, 20, 5.5,
		map[string]interface{}{
			"type": "bot", "id": "enemy", "position": []interface{}{float64(3), float64(10)},
			"hp": float64(100), "max_hp": float64(100), "weapon": "sword", "is_alive": true, "has_los": true, "can_attack": true,
		},
		map[string]interface{}{
			"type": "teleport_pad", "id": "a", "linked_pad_id": "b",
			"position": []interface{}{float64(8), float64(10)}, "is_ready": true,
		},
		map[string]interface{}{
			"type": "landmine", "id": "mine", "owner_id": "enemy",
			"position": []interface{}{float64(8), float64(10)}, "armed": true,
		},
	)
	got := PickAction("kite", msg, "bow", 5, "me")
	if got.Action == "move" && got.Direction != nil && got.Direction[0] > 0 && got.Direction[1] == 0 {
		t.Fatalf("teleporter exemption routed directly through lethal mine: %+v", got)
	}
}

func TestTeleporterEscapeSkipsLethalSourceForSafeAlternative(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "unsafe", "linked_pad_id": "unsafe-exit", "position": []interface{}{float64(8), float64(10)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "unsafe-exit", "linked_pad_id": "unsafe", "position": []interface{}{float64(18), float64(18)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "safe", "linked_pad_id": "safe-exit", "position": []interface{}{float64(6), float64(8)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "safe-exit", "linked_pad_id": "safe", "position": []interface{}{float64(18), float64(17)}, "is_ready": true},
	}, nil)
	msg := teleportTick([2]float64{6, 10}, 20, 5.5,
		map[string]interface{}{
			"type": "bot", "id": "enemy", "position": []interface{}{float64(3), float64(10)},
			"hp": float64(100), "max_hp": float64(100), "weapon": "sword", "is_alive": true, "has_los": true, "can_attack": true,
		},
		map[string]interface{}{
			"type": "teleport_pad", "id": "unsafe", "linked_pad_id": "unsafe-exit",
			"position": []interface{}{float64(8), float64(10)}, "is_ready": true,
		},
		map[string]interface{}{
			"type": "teleport_pad", "id": "safe", "linked_pad_id": "safe-exit",
			"position": []interface{}{float64(6), float64(8)}, "is_ready": true,
		},
		map[string]interface{}{
			"type": "landmine", "id": "mine", "owner_id": "enemy",
			"position": []interface{}{float64(8), float64(10)}, "armed": true,
		},
	)
	ts := parseTick(msg)
	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	buildDangerSet(danger, &ts, "me")
	ts.Danger = danger

	got := tryTeleporterPressureEscape(ts, "kite", &ts.Enemies[0], 3)
	if got == nil || got.Action != "move" || got.Direction == nil || *got.Direction != [2]float64{0, -1} {
		t.Fatalf("escape with unsafe first source = %+v, want north toward the safe alternative", got)
	}
}

func TestTeleporterEscapeRejectsDangerBlockedApproach(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "source", "linked_pad_id": "exit", "position": []interface{}{float64(8), float64(10)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "exit", "linked_pad_id": "source", "position": []interface{}{float64(18), float64(18)}, "is_ready": true},
	}, nil)
	msg := teleportTick([2]float64{5, 10}, 20, 5.5,
		map[string]interface{}{
			"type": "bot", "id": "enemy", "position": []interface{}{float64(3), float64(10)},
			"hp": float64(100), "max_hp": float64(100), "weapon": "sword", "is_alive": true, "has_los": true, "can_attack": true,
		},
		map[string]interface{}{
			"type": "teleport_pad", "id": "source", "linked_pad_id": "exit",
			"position": []interface{}{float64(8), float64(10)}, "is_ready": true,
		},
		map[string]interface{}{
			"type": "hazard_zone", "id": "barrier", "position": []interface{}{float64(6), float64(10)},
			"width": float64(1), "height": float64(24), "active": true,
		},
	)
	ts := parseTick(msg)
	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	buildDangerSet(danger, &ts, "me")
	ts.Danger = danger

	if got := tryTeleporterPressureEscape(ts, "kite", &ts.Enemies[0], 2); got != nil {
		t.Fatalf("danger-blocked source produced an escape action instead of falling through: %+v", *got)
	}
}

func TestLethalTeleporterSourceFallsThroughToLaterSurvivalAction(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "source", "linked_pad_id": "exit", "position": []interface{}{float64(8), float64(10)}, "is_ready": true},
		map[string]interface{}{"type": "teleport_pad", "id": "exit", "linked_pad_id": "source", "position": []interface{}{float64(18), float64(18)}, "is_ready": true},
	}, nil)
	msg := teleportTick([2]float64{6, 10}, 20, 5.5,
		map[string]interface{}{
			"type": "bot", "id": "enemy", "position": []interface{}{float64(4), float64(10)},
			"hp": float64(100), "max_hp": float64(100), "weapon": "sword", "is_alive": true, "has_los": true, "can_attack": true,
		},
		map[string]interface{}{
			"type": "teleport_pad", "id": "source", "linked_pad_id": "exit",
			"position": []interface{}{float64(8), float64(10)}, "is_ready": true,
		},
		map[string]interface{}{
			"type": "landmine", "id": "mine", "owner_id": "enemy",
			"position": []interface{}{float64(8), float64(10)}, "armed": true,
		},
	)

	got := PickAction("kite", msg, "bow", 5, "me")
	if got.Action != "place_mine" {
		t.Fatalf("action after rejecting lethal teleporter source = %+v, want later survival mine logic", got)
	}
}

func TestZoneAnchorGrappleAvoidsCachedReadyPad(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "zone-pad", "linked_pad_id": "zone-exit", "position": []interface{}{float64(16), float64(10)}, "is_ready": true},
	}, nil)
	ts := tickState{
		Position: [2]float64{10, 10}, HP: 100, MaxHP: 100, InZone: false,
		ZoneTargetCenter: [2]float64{16, 10}, GrappleCharges: 1,
	}
	if got := tryAnchorGrapple(ts, "defensive", nil, math.Inf(1), 1); got != nil {
		t.Fatalf("zone anchor targeted cached ready pad: %+v", *got)
	}
}

func TestBlockedZoneAnchorGrappleFallsThroughToSafeMovement(t *testing.T) {
	walls := make([][2]int, 0, 5)
	for row := 8; row <= 12; row++ {
		walls = append(walls, [2]int{13, row})
	}
	setTerrain(t, 30, 30, walls)

	ts := tickState{
		Position: [2]float64{10, 10}, HP: 44, MaxHP: 140, InZone: false,
		ZoneCenter: [2]float64{16, 10}, ZoneTargetCenter: [2]float64{16, 10},
		ZoneRadius: 8, ZoneTargetRadius: 6, GrappleCharges: 1,
	}
	if got := tryAnchorGrapple(ts, "kite", nil, math.Inf(1), 5); got != nil {
		t.Fatalf("blocked zone anchor produced a grapple retry: %+v", *got)
	}

	msg := map[string]interface{}{
		"type": "tick", "tick": float64(400),
		"your_state": map[string]interface{}{
			"position": []interface{}{float64(10), float64(10)},
			"hp":       float64(44), "max_hp": float64(140), "speed": float64(5.5),
			"weapon_ready": false, "dodge_cooldown": float64(20),
			"in_safe_zone": false, "distance_to_zone_edge": float64(-2),
			"grapple_charges": float64(1), "grapple_cooldown": float64(0),
		},
		"safe_zone": map[string]interface{}{
			"center": []interface{}{float64(16), float64(10)}, "radius": float64(8),
			"target_center": []interface{}{float64(16), float64(10)}, "target_radius": float64(6),
		},
	}
	got := PickAction("kite", msg, "staff", 5, "blocked-anchor-bot")
	if got.Action != "move" || got.Direction == nil || *got.Direction == [2]float64{} {
		t.Fatalf("blocked anchor fallback = %+v, want safe zone path movement", got)
	}
	if terrain := getTerrain(); terrain.isMoveBlocked(10, 10, int(got.Direction[0]), int(got.Direction[1])) {
		t.Fatalf("blocked anchor fallback selected an impassable step: %v", *got.Direction)
	}
}

func TestRetreatAnchorGrappleAvoidsLiveReadyPad(t *testing.T) {
	cacheTeleportTestMap(t, nil, nil)
	near := entity{ID: "enemy", Position: [2]float64{12, 10}, IsAlive: true}
	ts := tickState{
		Position: [2]float64{10, 10}, HP: 40, MaxHP: 100, InZone: true,
		GrappleCharges: 1,
		Teleporters:    []entity{{ID: "retreat-pad", Position: [2]float64{2, 10}, Ready: true}},
	}
	if got := tryAnchorGrapple(ts, "defensive", &near, 2, 1); got != nil {
		t.Fatalf("retreat anchor targeted live ready pad: %+v", *got)
	}
}

func TestRetreatAnchorGrappleNearArenaEdgeMatchesPublicProtocol(t *testing.T) {
	setTerrain(t, 30, 30, nil)
	oldWidth, oldHeight := config.C.ArenaWidth, config.C.ArenaHeight
	config.C.ArenaWidth, config.C.ArenaHeight = 2000, 2000
	t.Cleanup(func() {
		config.C.ArenaWidth, config.C.ArenaHeight = oldWidth, oldHeight
	})

	for _, tc := range []struct {
		name     string
		position [2]float64
		enemy    [2]float64
	}{
		{name: "lower edge", position: [2]float64{1, 1}, enemy: [2]float64{2, 1}},
		{name: "upper edge", position: [2]float64{28, 28}, enemy: [2]float64{27, 28}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			near := entity{ID: "enemy", Position: tc.enemy, IsAlive: true}
			ts := tickState{
				Position: tc.position, HP: 40, MaxHP: 100, InZone: true,
				GrappleCharges: 1,
			}
			got := tryAnchorGrapple(ts, "kite", &near, 1, 5)
			if got == nil || got.TargetPosition == nil {
				t.Fatal("low-HP edge escape produced no anchor grapple")
			}
			if got.TargetPosition[0] < 0 || got.TargetPosition[1] < 0 ||
				got.TargetPosition[0] > 29 || got.TargetPosition[1] > 29 {
				t.Fatalf("edge grapple escaped terrain bounds: %v", *got.TargetPosition)
			}

			payload := buildActionPayload(float64(18), *got)
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal edge grapple: %v", err)
			}
			_, parsed, err := ws.ParseBotMessage(raw)
			if err != nil {
				t.Fatalf("parse edge grapple %s: %v", raw, err)
			}
			if action := ws.ActionMessageToAction(parsed.(*ws.ActionMessage)); action == nil {
				t.Fatalf("edge grapple violates the public protocol: %s", raw)
			}
		})
	}
}

func TestKiteAnchorGrappleAvoidsCachedReadyPad(t *testing.T) {
	cacheTeleportTestMap(t, []interface{}{
		map[string]interface{}{"type": "teleport_pad", "id": "kite-pad", "linked_pad_id": "kite-exit", "position": []interface{}{float64(17), float64(10)}, "is_ready": true},
	}, nil)
	near := entity{ID: "enemy", Position: [2]float64{15, 10}, IsAlive: true}
	ts := tickState{
		Position: [2]float64{10, 10}, HP: 100, MaxHP: 100, InZone: true,
		GrappleCharges: 1,
	}
	if got := tryAnchorGrapple(ts, "kite", &near, 5, 1); got != nil {
		t.Fatalf("kite anchor targeted cached ready pad: %+v", *got)
	}
}

func TestNearestPickupUsesMeasuredRouteAroundMapWalls(t *testing.T) {
	walls := make([][2]int, 0, 9)
	for y := 0; y <= 8; y++ {
		walls = append(walls, [2]int{5, y})
	}
	setTerrain(t, 20, 20, walls)
	pickups := []entity{
		{ID: "walled", Type: "pickup", Position: [2]float64{7, 3}, SubType: "health_pack"},
		{ID: "reachable", Type: "pickup", Position: [2]float64{3, 8}, SubType: "health_pack"},
	}
	got, distance := nearestPickup([2]float64{3, 3}, pickups, nil, false)
	if got == nil || got.ID != "reachable" {
		t.Fatalf("nearest pickup = %+v at %.1f, want measured reachable route", got, distance)
	}
	if distance != 5 {
		t.Fatalf("reachable pickup distance = %.1f, want measured distance 5", distance)
	}
}

func TestSuddenDeathStallForcesHealthyBotToEngage(t *testing.T) {
	setTerrain(t, 30, 30, nil)
	msg := map[string]interface{}{
		"type":               "tick",
		"tick":               float64(3100),
		"sudden_death":       true,
		"sudden_death_stall": true,
		"your_state": map[string]interface{}{
			"position": []interface{}{float64(5), float64(5)},
			"hp":       float64(90), "max_hp": float64(100),
			"weapon_ready": true, "dodge_cooldown": float64(0),
		},
		"nearby_entities": []interface{}{
			map[string]interface{}{
				"type": "bot", "id": "enemy", "position": []interface{}{float64(15), float64(5)},
				"hp": float64(100), "max_hp": float64(100), "weapon": "sword",
				"is_alive": true, "has_los": true,
			},
		},
	}

	for _, strategy := range []string{"defensive", "territorial", "kite"} {
		got := PickAction(strategy, msg, "sword", 1, "me")
		if got.Action != "move" && got.Action != "move_to" {
			t.Errorf("%s under stall chose %+v, want movement toward combat", strategy, got)
		}
	}
}

func TestCarvedRouteStartsZoneDriftEarlier(t *testing.T) {
	walls := make([][2]int, 0, 12)
	for y := 2; y <= 13; y++ {
		walls = append(walls, [2]int{10, y})
	}
	setTerrain(t, 30, 30, walls)
	msg := map[string]interface{}{
		"type":       "tick",
		"round_tick": float64(1000),
		"your_state": map[string]interface{}{
			"position": []interface{}{float64(6), float64(8)},
			"hp":       float64(100), "max_hp": float64(100),
			"weapon_ready": false, "dodge_cooldown": float64(0),
			"in_safe_zone": true, "distance_to_zone_edge": float64(10),
		},
		"safe_zone": map[string]interface{}{
			"center": []interface{}{float64(6), float64(8)}, "radius": float64(20),
			"target_center": []interface{}{float64(16), float64(8)}, "target_radius": float64(3),
		},
	}

	got := PickAction("defensive", msg, "sword", 1, "me")
	if got.Action != "move" && got.Action != "move_to" {
		t.Fatalf("detour-aware zone drift chose %+v, want early movement", got)
	}
}
