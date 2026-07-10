package game

import "testing"

func TestUpdateBotCosmeticsCopiesLoadout(t *testing.T) {
	engine := NewGameEngine()
	engine.Bots["bot-1"] = &BotState{BotID: "bot-1"}

	loadout := map[string]string{"bot_skin": "neon_grid"}
	if !engine.UpdateBotCosmetics("bot-1", loadout) {
		t.Fatal("connected bot was not updated")
	}
	loadout["bot_skin"] = "mutated_after_call"
	if got := engine.Bots["bot-1"].Cosmetics["bot_skin"]; got != "neon_grid" {
		t.Fatalf("engine retained caller-owned map, got %q", got)
	}

	if engine.UpdateBotCosmetics("missing", map[string]string{}) {
		t.Fatal("missing bot unexpectedly reported as updated")
	}
}

func TestBuildSpectatorStateIncludesCosmeticsWithoutChangingMechanics(t *testing.T) {
	arena := NewArenaMap()
	bot := &BotState{
		BotID: "bot-1", Name: "Cosmo", AvatarColor: "#00ff88", Weapon: "sword",
		Position: Vec2{10, 20}, HP: 100, MaxHP: 100, IsAlive: true,
		Cosmetics: map[string]string{
			"bot_skin": "neon_grid", "weapon_skin": "solar_flare", "attachment": "signal_antenna",
		},
	}
	state := BuildSpectatorState(
		map[string]*BotState{bot.BotID: bot}, arena, nil, NewKillFeed(10), 10, 0, nil, RoundModifierNone,
	)
	if len(state.Bots) != 1 {
		t.Fatalf("spectator bots = %d, want 1", len(state.Bots))
	}
	view := state.Bots[0]
	got, ok := view["cosmetics"].(map[string]string)
	if !ok {
		t.Fatalf("cosmetics type = %T, want map[string]string", view["cosmetics"])
	}
	if got["attachment"] != "signal_antenna" {
		t.Fatalf("attachment = %q, want signal_antenna", got["attachment"])
	}
	if view["weapon"] != "sword" || view["hp"] != float64(100) {
		t.Fatalf("cosmetics changed gameplay view: weapon=%v hp=%v", view["weapon"], view["hp"])
	}
}
