package game

import "testing"

func TestAddModeTickExtraCTF(t *testing.T) {
	rules := ModeRulesFor(ModeCTF)
	flags := []*CTFFlag{
		{ID: "flag_1", Team: 1, Position: NewVec2(100, 200), BasePosition: NewVec2(100, 200), Status: FlagAtBase},
		{ID: "flag_2", Team: 2, Position: NewVec2(300, 300), BasePosition: NewVec2(320, 320), Status: FlagCarried, CarrierID: "bot-7"},
	}
	scores := map[int]int{1: 2, 2: 1}

	extra := map[string]interface{}{}
	AddModeTickExtra(extra, rules, scores, flags)

	if got, _ := extra["game_mode"].(string); got != "ctf" {
		t.Errorf("game_mode = %q, want ctf", got)
	}

	ts, ok := extra["team_scores"].(map[string]int)
	if !ok {
		t.Fatalf("team_scores missing or wrong type: %T", extra["team_scores"])
	}
	if ts["1"] != 2 || ts["2"] != 1 {
		t.Errorf("team_scores = %v, want map[1:2 2:1]", ts)
	}

	fv, ok := extra["flags"].([]map[string]interface{})
	if !ok {
		t.Fatalf("flags missing or wrong type: %T", extra["flags"])
	}
	if len(fv) != 2 {
		t.Fatalf("len(flags) = %d, want 2", len(fv))
	}
	if fv[0]["id"] != "flag_1" || fv[0]["status"] != "at_base" {
		t.Errorf("flags[0] = %v", fv[0])
	}
	if fv[1]["carrier_id"] != "bot-7" {
		t.Errorf("flags[1] carrier_id = %v, want bot-7", fv[1]["carrier_id"])
	}
}

func TestAddModeTickExtraFFA(t *testing.T) {
	extra := map[string]interface{}{}
	AddModeTickExtra(extra, ModeRulesFor(ModeFFA), nil, nil)

	if got, _ := extra["game_mode"].(string); got != "ffa" {
		t.Errorf("game_mode = %q, want ffa", got)
	}
	if _, ok := extra["team_scores"]; ok {
		t.Error("team_scores must be omitted in FFA")
	}
	if _, ok := extra["flags"]; ok {
		t.Error("flags must be omitted in FFA")
	}
}

func TestVoidTilesNear(t *testing.T) {
	sd := NewSuddenDeathSystem()
	sd.VoidTiles[[2]int{5, 5}] = true
	sd.VoidTiles[[2]int{10, 4}] = true
	sd.VoidTiles[[2]int{50, 50}] = true

	got := sd.VoidTilesNear([2]int{4, 4}, 7)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (far tile filtered out): %v", len(got), got)
	}
	seen := map[[2]int]bool{}
	for _, c := range got {
		seen[c] = true
	}
	if !seen[[2]int{5, 5}] || !seen[[2]int{10, 4}] {
		t.Errorf("VoidTilesNear = %v, want cells (5,5) and (10,4)", got)
	}
}
