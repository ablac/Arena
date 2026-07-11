package game

import (
	"testing"

	"arena-server/internal/config"
)

func setTeleportTestWorld(t *testing.T) *SpatialGrid {
	t.Helper()
	oldTerrain := ActiveTerrain
	oldCollectRadius := config.C.TeleportCollectRadius
	oldCooldown := config.C.TeleportCooldownTicks
	oldLock := config.C.TeleportPadLockTicks
	ActiveTerrain = openMovementTerrain(16, 6)
	config.C.TeleportCollectRadius = 1
	config.C.TeleportCooldownTicks = 50
	config.C.TeleportPadLockTicks = 30
	t.Cleanup(func() {
		ActiveTerrain = oldTerrain
		config.C.TeleportCollectRadius = oldCollectRadius
		config.C.TeleportCooldownTicks = oldCooldown
		config.C.TeleportPadLockTicks = oldLock
	})
	return NewSpatialGrid(20)
}

func teleportTestPads() []TeleportPad {
	return []TeleportPad{
		{ID: "a", LinkedPadID: "b", Position: ActiveTerrain.GridToWorld([2]int{2, 2}), Color: "#00ffff"},
		{ID: "b", LinkedPadID: "a", Position: ActiveTerrain.GridToWorld([2]int{10, 2}), Color: "#00ffff"},
	}
}

func teleportTestBot(id string, cell [2]int, grid *SpatialGrid) *BotState {
	bot := &BotState{BotID: id, IsAlive: true, Position: ActiveTerrain.GridToWorld(cell)}
	grid.Insert(bot.BotID, bot.Position)
	return bot
}

func TestProcessTeleportsRejectsLockedUnlitPairAndRearmsAtBoundary(t *testing.T) {
	grid := setTeleportTestWorld(t)
	pads := teleportTestPads()
	first := teleportTestBot("first", [2]int{2, 2}, grid)

	events := ProcessTeleports(map[string]*BotState{first.BotID: first}, pads, grid, 100, RoundModifierNone)
	if len(events) != 1 {
		t.Fatalf("initial teleport events = %d, want 1", len(events))
	}
	if got := ActiveTerrain.WorldToGrid(first.Position); got != [2]int{10, 2} {
		t.Fatalf("initial teleport destination = %v, want [10 2]", got)
	}
	if pads[0].CooldownUntilTick != 130 || pads[1].CooldownUntilTick != 130 {
		t.Fatalf("pair lock expiry = [%d %d], want [130 130]", pads[0].CooldownUntilTick, pads[1].CooldownUntilTick)
	}

	locked := teleportTestBot("locked", [2]int{2, 2}, grid)
	for _, tick := range []int{101, 129} {
		locked.TeleportTouchedPads = nil
		if events := ProcessTeleports(map[string]*BotState{locked.BotID: locked}, pads, grid, tick, RoundModifierNone); len(events) != 0 {
			t.Fatalf("locked pair teleported at tick %d: events=%d", tick, len(events))
		}
		if got := ActiveTerrain.WorldToGrid(locked.Position); got != [2]int{2, 2} {
			t.Fatalf("locked bot moved at tick %d to %v", tick, got)
		}
	}

	before := BuildTeleportPadView(pads[0], 129, true)
	if ready, _ := before["is_ready"].(bool); ready {
		t.Fatal("pad view reported ready one tick before lock expiry")
	}
	if remaining, _ := before["cooldown_remaining_ticks"].(int); remaining != 1 {
		t.Fatalf("remaining cooldown at tick 129 = %d, want 1", remaining)
	}

	rearmed := teleportTestBot("rearmed", [2]int{2, 2}, grid)
	if events := ProcessTeleports(map[string]*BotState{rearmed.BotID: rearmed}, pads, grid, 130, RoundModifierNone); len(events) != 1 {
		t.Fatalf("rearmed pair events at exact boundary = %d, want 1", len(events))
	}
	after := BuildTeleportPadView(pads[0], 130, true)
	if after["is_ready"] != false || after["cooldown_remaining_ticks"] != 30 {
		t.Fatalf("view after reactivation = %#v, want unready with a fresh 30-tick lock", after)
	}
}

func TestBuildTeleportPadViewReadyAtExactBoundary(t *testing.T) {
	setTeleportTestWorld(t)
	pad := TeleportPad{ID: "a", Position: ActiveTerrain.GridToWorld([2]int{2, 2}), CooldownUntilTick: 130}

	before := BuildTeleportPadView(pad, 129, true)
	if before["is_ready"] != false || before["cooldown_remaining_ticks"] != 1 {
		t.Fatalf("tick 129 view = %#v, want unready with one tick remaining", before)
	}
	at := BuildTeleportPadView(pad, 130, true)
	if at["is_ready"] != true || at["cooldown_remaining_ticks"] != 0 {
		t.Fatalf("tick 130 view = %#v, want ready with zero remaining", at)
	}
}

func TestProcessTeleportsRequiresExitBeforeSameBotCanReenter(t *testing.T) {
	grid := setTeleportTestWorld(t)
	pads := teleportTestPads()
	bot := teleportTestBot("same", [2]int{2, 2}, grid)
	bots := map[string]*BotState{bot.BotID: bot}

	if events := ProcessTeleports(bots, pads, grid, 100, RoundModifierNone); len(events) != 1 {
		t.Fatalf("initial events = %d, want 1", len(events))
	}
	if events := ProcessTeleports(bots, pads, grid, 151, RoundModifierNone); len(events) != 0 {
		t.Fatalf("bot bounced while still touching linked pad: events=%d", len(events))
	}

	bot.Position = ActiveTerrain.GridToWorld([2]int{7, 2})
	grid.Update(bot.BotID, bot.Position)
	ProcessTeleports(bots, pads, grid, 152, RoundModifierNone)
	bot.Position = ActiveTerrain.GridToWorld([2]int{10, 2})
	grid.Update(bot.BotID, bot.Position)
	if events := ProcessTeleports(bots, pads, grid, 153, RoundModifierNone); len(events) != 1 {
		t.Fatalf("bot did not teleport after exit and re-entry: events=%d", len(events))
	}
}

func TestSpawnBotAtClearsTeleportStateForNewRound(t *testing.T) {
	grid := setTeleportTestWorld(t)
	bot := &BotState{
		BotID:               "reset",
		MaxHP:               100,
		TeleportCooldowns:   map[string]int{"old-a": 900, "old-b": 900},
		TeleportTouchedPads: map[string]bool{"old-a": true},
	}

	SpawnBotAt(bot, ActiveTerrain.GridToWorld([2]int{4, 2}), grid, 200)
	if len(bot.TeleportCooldowns) != 0 {
		t.Fatalf("teleport cooldowns survived round spawn: %#v", bot.TeleportCooldowns)
	}
	if len(bot.TeleportTouchedPads) != 0 {
		t.Fatalf("touched pads survived round spawn: %#v", bot.TeleportTouchedPads)
	}
}
