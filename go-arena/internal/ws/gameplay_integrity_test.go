package ws

import (
	"math"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
)

func withWSIntegrityConfig(t *testing.T) {
	t.Helper()
	oldBudget := config.C.StatBudget
	oldMin := config.C.StatMin
	oldMax := config.C.StatMax
	oldWidth := config.C.ArenaWidth
	oldHeight := config.C.ArenaHeight
	config.C.StatBudget = 20
	config.C.StatMin = 1
	config.C.StatMax = 10
	config.C.ArenaWidth = 2000
	config.C.ArenaHeight = 2000
	t.Cleanup(func() {
		config.C.StatBudget = oldBudget
		config.C.StatMin = oldMin
		config.C.StatMax = oldMax
		config.C.ArenaWidth = oldWidth
		config.C.ArenaHeight = oldHeight
	})
}

func TestActionMessageToActionRejectsUnknownAndInvalidPayloads(t *testing.T) {
	withWSIntegrityConfig(t)
	tests := []struct {
		name string
		msg  ActionMessage
	}{
		{name: "missing tick", msg: ActionMessage{Action: "idle"}},
		{name: "unknown action", msg: ActionMessage{Tick: 1, Action: "teleport_anywhere"}},
		{name: "move missing direction", msg: ActionMessage{Tick: 1, Action: "move"}},
		{name: "non-finite direction", msg: ActionMessage{Tick: 1, Action: "move", Direction: vecPtr(game.NewVec2(math.NaN(), 0))}},
		{name: "extreme direction", msg: ActionMessage{Tick: 1, Action: "move", Direction: vecPtr(game.NewVec2(99999, 0))}},
		{name: "zero dodge", msg: ActionMessage{Tick: 1, Action: "dodge", Direction: vecPtr(game.NewVec2(0, 0))}},
		{name: "move_to missing target", msg: ActionMessage{Tick: 1, Action: "move_to"}},
		{name: "move_to outside arena", msg: ActionMessage{Tick: 1, Action: "move_to", TargetPosition: vecPtr(game.NewVec2(1e100, 10))}},
		{name: "shove missing target", msg: ActionMessage{Tick: 1, Action: "shove"}},
		{name: "use_item missing id", msg: ActionMessage{Tick: 1, Action: "use_item"}},
		{name: "attack ambiguous target", msg: ActionMessage{Tick: 1, Action: "attack", Target: "bot", TargetPosition: vecPtr(game.NewVec2(100, 100))}},
		{name: "grapple ambiguous target", msg: ActionMessage{Tick: 1, Action: "grapple", Target: "bot", TargetPosition: vecPtr(game.NewVec2(100, 100))}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ActionMessageToAction(&tc.msg); got != nil {
				t.Fatalf("invalid action converted to %+v", got)
			}
		})
	}
}

func TestActionMessageToActionAcceptsDocumentedPayload(t *testing.T) {
	withWSIntegrityConfig(t)
	direction := game.NewVec2(1, -1)
	got := ActionMessageToAction(&ActionMessage{Tick: 1, Action: "move", Direction: &direction})
	if got == nil || got.Type != game.ActionMove || got.Direction != direction {
		t.Fatalf("valid move converted to %+v", got)
	}
}

func TestPrepareActionForBotNormalizesLegacyStaffDualTarget(t *testing.T) {
	withWSIntegrityConfig(t)
	position := game.NewVec2(100, 140)
	message := &ActionMessage{
		Tick:           17,
		Action:         "attack",
		Target:         "opponent-id",
		TargetPosition: &position,
	}

	action, err := prepareActionForBot(&game.BotState{Weapon: "staff"}, message)
	if err != nil {
		t.Fatalf("legacy staff attack was rejected: %v", err)
	}
	if action == nil || action.Type != game.ActionAttack || action.TargetPosition == nil {
		t.Fatalf("legacy staff attack converted to %+v", action)
	}
	if action.TargetID != "" {
		t.Fatalf("discarded legacy staff target survived as %q", action.TargetID)
	}
	if *action.TargetPosition != position {
		t.Fatalf("staff target position = %v, want %v", *action.TargetPosition, position)
	}
	if message.Target != "" {
		t.Fatalf("legacy staff message retained ambiguous target %q", message.Target)
	}
}

func TestPrepareActionForBotKeepsDualTargetPunitiveOutsideLegacyStaffAttack(t *testing.T) {
	withWSIntegrityConfig(t)
	tests := []struct {
		name   string
		weapon string
		action string
	}{
		{name: "sword attack", weapon: "sword", action: "attack"},
		{name: "bow attack", weapon: "bow", action: "attack"},
		{name: "staff grapple", weapon: "staff", action: "grapple"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			position := game.NewVec2(100, 140)
			message := &ActionMessage{
				Tick:           17,
				Action:         tc.action,
				Target:         "opponent-id",
				TargetPosition: &position,
			}
			if action, err := prepareActionForBot(&game.BotState{Weapon: tc.weapon}, message); err == nil || action != nil {
				t.Fatalf("ambiguous %s/%s payload accepted as %+v", tc.weapon, tc.action, action)
			}
			if message.Target != "opponent-id" {
				t.Fatalf("non-legacy payload was mutated to target %q", message.Target)
			}
		})
	}
}

func TestPrepareActionForBotStillValidatesLegacyStaffPosition(t *testing.T) {
	withWSIntegrityConfig(t)
	position := game.NewVec2(1e100, 140)
	message := &ActionMessage{
		Tick:           17,
		Action:         "attack",
		Target:         "untrusted-opponent-id",
		TargetPosition: &position,
	}

	if action, err := prepareActionForBot(&game.BotState{Weapon: "staff"}, message); err == nil || action != nil {
		t.Fatalf("out-of-bounds legacy staff position accepted as %+v", action)
	}
	if message.Target != "" {
		t.Fatalf("legacy target was not discarded before position validation: %q", message.Target)
	}
}

func TestParseBotMessageRejectsMalformedVectorShape(t *testing.T) {
	withWSIntegrityConfig(t)
	for _, payload := range [][]byte{
		[]byte(`{"type":"action","tick":1,"action":"move","direction":{}}`),
		[]byte(`{"type":"action","tick":1,"action":"move","direction":[1,0,5]}`),
	} {
		if _, _, err := ParseBotMessage(payload); err == nil {
			t.Fatalf("malformed vector accepted: %s", payload)
		}
	}
}

func TestParseBotMessageRejectsUndocumentedFields(t *testing.T) {
	withWSIntegrityConfig(t)
	payload := []byte(`{"type":"action","tick":1,"action":"idle","bonus_stat_points":999}`)
	if _, _, err := ParseBotMessage(payload); err == nil {
		t.Fatalf("undocumented action fields accepted: %s", payload)
	}
}

func TestParseBotMessageRejectsDuplicateFields(t *testing.T) {
	withWSIntegrityConfig(t)
	for _, payload := range [][]byte{
		[]byte(`{"type":"action","tick":1,"action":"idle","action":"move"}`),
		[]byte(`{"type":"action","type":"auth","tick":1,"action":"idle","api_key":"forged"}`),
	} {
		if _, _, err := ParseBotMessage(payload); err == nil {
			t.Fatalf("duplicate JSON field accepted: %s", payload)
		}
	}
}

func TestApplySelectedLoadoutIsAllOrNothing(t *testing.T) {
	withWSIntegrityConfig(t)
	bot := &game.BotState{
		Weapon:           "sword",
		Stats:            map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		FallbackBehavior: "aggressive",
	}
	originalStats := cloneStats(bot.Stats)
	invalid := &LoadoutMessage{
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 20, "speed": 20, "attack": 20, "defense": 20},
		Fallback: "hunter",
	}

	if err := applySelectedLoadout(bot, invalid); err == nil {
		t.Fatal("over-budget loadout was accepted")
	}
	if bot.Weapon != "sword" || bot.FallbackBehavior != "aggressive" || !statsEqual(bot.Stats, originalStats) {
		t.Fatalf("invalid loadout partially applied: weapon=%q stats=%v fallback=%q", bot.Weapon, bot.Stats, bot.FallbackBehavior)
	}
}

func TestApplySelectedLoadoutCopiesValidatedStats(t *testing.T) {
	withWSIntegrityConfig(t)
	bot := &game.BotState{Weapon: "sword", Stats: map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5}, FallbackBehavior: "aggressive"}
	loadout := &LoadoutMessage{
		Weapon:   "bow",
		Stats:    map[string]int{"hp": 7, "speed": 4, "attack": 6, "defense": 3},
		Fallback: "defensive",
	}

	if err := applySelectedLoadout(bot, loadout); err != nil {
		t.Fatalf("valid loadout rejected: %v", err)
	}
	loadout.Stats["hp"] = 10
	if bot.Stats["hp"] != 7 {
		t.Fatalf("bot stats alias client payload: %v", bot.Stats)
	}
}

func TestNormalizeStoredLoadoutRepairsHistoricalInvalidValues(t *testing.T) {
	withWSIntegrityConfig(t)
	record := &db.Bot{
		DefaultWeapon:   "laser",
		DefaultStats:    db.JSONBStats{"hp": 20, "speed": 20, "attack": 20, "defense": 20},
		DefaultFallback: "run_away_forever",
	}

	if changed := normalizeStoredLoadout(record); !changed {
		t.Fatal("invalid stored loadout was not normalized")
	}
	if record.DefaultWeapon != "sword" || record.DefaultFallback != "aggressive" {
		t.Fatalf("stored defaults not repaired: weapon=%q fallback=%q", record.DefaultWeapon, record.DefaultFallback)
	}
	if got := map[string]int(record.DefaultStats); got["hp"] != 5 || got["speed"] != 5 || got["attack"] != 5 || got["defense"] != 5 {
		t.Fatalf("stored stats normalized to %v, want balanced budget", got)
	}
}

func TestViolationTrackerLocksAPIKeyAfterThreeRecentStrikes(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	tracker := newViolationTracker(3, time.Minute, 30*time.Second)
	tracker.now = func() time.Time { return now }

	for strike := 1; strike <= 2; strike++ {
		result := tracker.Record("key-1")
		if result.Locked || result.Strikes != strike {
			t.Fatalf("strike %d result = %+v", strike, result)
		}
	}
	result := tracker.Record("key-1")
	if !result.Locked || result.RetryAfter != 30*time.Second {
		t.Fatalf("third strike result = %+v, want 30s lock", result)
	}
	if remaining, locked := tracker.IsLocked("key-1"); !locked || remaining != 30*time.Second {
		t.Fatalf("lock readback = remaining %v locked %v", remaining, locked)
	}

	now = now.Add(31 * time.Second)
	if remaining, locked := tracker.IsLocked("key-1"); locked || remaining != 0 {
		t.Fatalf("expired lock persisted: remaining %v locked %v", remaining, locked)
	}
	result = tracker.Record("key-1")
	if result.Locked || result.Strikes != 1 {
		t.Fatalf("post-lock strike did not restart window: %+v", result)
	}
}

func vecPtr(v game.Vec2) *game.Vec2 { return &v }

func statsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
