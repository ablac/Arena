package game

import "testing"

func TestTeleportPadBotViewUsesObserverEffectiveCooldown(t *testing.T) {
	pad := TeleportPad{ID: "pad-a", LinkedPadID: "pad-b", CooldownUntilTick: 130}
	bot := &BotState{TeleportCooldowns: map[string]int{"pad-a": 150}}

	global := BuildTeleportPadView(pad, 135, true)
	if !global.IsReady {
		t.Fatalf("global pad view changed by observer state: %+v", global)
	}

	observer := BuildTeleportPadViewForBot(pad, 135, true, bot)
	if observer.IsReady {
		t.Fatalf("pad reported ready before observer cooldown expired: %+v", observer)
	}
	if observer.CooldownRemainingTicks != 15 {
		t.Fatalf("observer cooldown remaining = %v, want 15", observer.CooldownRemainingTicks)
	}

	ready := BuildTeleportPadViewForBot(pad, 150, true, bot)
	if !ready.IsReady {
		t.Fatalf("pad did not become ready at observer cooldown boundary: %+v", ready)
	}
}
