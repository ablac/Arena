package game

import "testing"

func TestTeleportPadBotViewUsesObserverEffectiveCooldown(t *testing.T) {
	pad := TeleportPad{ID: "pad-a", LinkedPadID: "pad-b", CooldownUntilTick: 130}
	bot := &BotState{TeleportCooldowns: map[string]int{"pad-a": 150}}

	global := BuildTeleportPadView(pad, 135, true)
	if ready, _ := global["is_ready"].(bool); !ready {
		t.Fatalf("global pad view changed by observer state: %+v", global)
	}

	observer := BuildTeleportPadViewForBot(pad, 135, true, bot)
	if ready, _ := observer["is_ready"].(bool); ready {
		t.Fatalf("pad reported ready before observer cooldown expired: %+v", observer)
	}
	if got := observer["cooldown_remaining_ticks"]; got != 15 {
		t.Fatalf("observer cooldown remaining = %v, want 15", got)
	}

	ready := BuildTeleportPadViewForBot(pad, 150, true, bot)
	if isReady, _ := ready["is_ready"].(bool); !isReady {
		t.Fatalf("pad did not become ready at observer cooldown boundary: %+v", ready)
	}
}
