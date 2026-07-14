package game

import (
	"testing"

	"arena-server/internal/config"
)

func setPickupTestWorld(t *testing.T) {
	t.Helper()
	oldTerrain := ActiveTerrain
	oldRadius := config.C.PickupCollectRadius
	ActiveTerrain = openMovementTerrain(16, 6)
	config.C.PickupCollectRadius = 2.0
	t.Cleanup(func() {
		ActiveTerrain = oldTerrain
		config.C.PickupCollectRadius = oldRadius
	})
}

// TestCheckAutoCollectHealsWithinConfiguredRadius guards against the pickup
// collect radius silently drifting from config.C.PickupCollectRadius, which
// is also the value published to bots via the /api/v1/bot-setup contract
// ("collect_radius_tiles"). A bot within that radius — not just standing on
// the exact same grid cell — must be healed by a nearby health pack.
func TestCheckAutoCollectHealsWithinConfiguredRadius(t *testing.T) {
	setPickupTestWorld(t)
	bot := &BotState{
		BotID: "healer", IsAlive: true, HP: 50, MaxHP: 100,
		Position: ActiveTerrain.GridToWorld([2]int{5, 2}),
	}
	pickups := []Pickup{{
		ID: "hp1", Type: PickupHealthPack, Value: 30,
		Position: ActiveTerrain.GridToWorld([2]int{7, 2}), // 2 cells away
	}}

	CheckAutoCollect(map[string]*BotState{bot.BotID: bot}, &pickups)

	if bot.HP != 80 {
		t.Fatalf("bot.HP = %v, want 80 (healed while within collect radius)", bot.HP)
	}
	if len(pickups) != 0 {
		t.Fatalf("pickup was not consumed: %#v", pickups)
	}
}

// TestCheckAutoCollectIgnoresPickupOutsideRadius confirms the radius is
// actually enforced, not just widened to "always collect."
func TestCheckAutoCollectIgnoresPickupOutsideRadius(t *testing.T) {
	setPickupTestWorld(t)
	bot := &BotState{
		BotID: "far", IsAlive: true, HP: 50, MaxHP: 100,
		Position: ActiveTerrain.GridToWorld([2]int{5, 2}),
	}
	pickups := []Pickup{{
		ID: "hp1", Type: PickupHealthPack, Value: 30,
		Position: ActiveTerrain.GridToWorld([2]int{9, 2}), // 4 cells away
	}}

	CheckAutoCollect(map[string]*BotState{bot.BotID: bot}, &pickups)

	if bot.HP != 50 {
		t.Fatalf("bot.HP = %v, want 50 (unchanged, pickup out of range)", bot.HP)
	}
	if len(pickups) != 1 {
		t.Fatalf("pickup was consumed despite being out of range: %#v", pickups)
	}
}

// TestCollectByActionRespectsConfiguredRadius mirrors the auto-collect check
// for the explicit use_item action path.
func TestCollectByActionRespectsConfiguredRadius(t *testing.T) {
	setPickupTestWorld(t)
	bot := &BotState{
		BotID: "actor", IsAlive: true, HP: 50, MaxHP: 100,
		Position: ActiveTerrain.GridToWorld([2]int{5, 2}),
	}
	pickups := []Pickup{{
		ID: "hp1", Type: PickupHealthPack, Value: 30,
		Position: ActiveTerrain.GridToWorld([2]int{7, 2}), // 2 cells away
	}}

	if ok := CollectByAction(bot, "hp1", &pickups); !ok {
		t.Fatal("CollectByAction returned false for a pickup within the configured radius")
	}
	if bot.HP != 80 {
		t.Fatalf("bot.HP = %v, want 80", bot.HP)
	}
}
