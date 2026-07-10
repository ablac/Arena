package demobots

import (
	"testing"
)

// setTerrain installs a synthetic open terrain grid (with optional wall rows)
// for pathfinding tests, and returns a cleanup that clears it.
func setTerrain(t *testing.T, width, height int, walls [][2]int) {
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
