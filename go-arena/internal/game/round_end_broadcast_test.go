package game

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
)

// roundEndBroadcastConfig applies the shared config/global setup for the
// round_end spectator broadcast tests and registers cleanup.
func roundEndBroadcastConfig(t *testing.T) {
	t.Helper()
	isolateBotStatsPersistence(t)
	botStatsPersistenceMu.Lock()
	applyBotStatsDeltas = func(context.Context, []db.BotStatsDelta) error { return nil }
	botStatsPersistenceMu.Unlock()

	previousConfig := config.C
	previousTerrain := ActiveTerrain
	previousShape := ActiveMapShape
	t.Cleanup(func() {
		config.C = previousConfig
		ActiveTerrain = previousTerrain
		ActiveMapShape = previousShape
	})
	config.C.WeaponAutoBalanceEnabled = false
	config.C.ArenaWidth = 200
	config.C.ArenaHeight = 200
	config.C.PathfindingCellSize = 20
	config.C.BotRadius = 5
	config.C.ZoneCenterX = 100
	config.C.ZoneCenterY = 100
	config.C.ZoneInitialRadius = 100
	config.C.ZoneMinRadius = 20
	config.C.ArenaSizeDynamic = false
	config.C.IntermissionTime = 7.5
	// A carved (non-square) shape so the pre-generated terrain produces
	// boundary mask rects for the next_map assertions.
	config.C.MapShape = "circle"
}

func receiveSpectatorMessage(t *testing.T, ch <-chan *SpectatorMessage) map[string]interface{} {
	t.Helper()
	select {
	case message := <-ch:
		if message == nil {
			t.Fatal("spectator received a nil message")
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(message.Payload, &decoded); err != nil {
			t.Fatalf("spectator payload is not valid JSON: %v", err)
		}
		return decoded
	case <-time.After(time.Second):
		t.Fatal("spectator did not receive a broadcast")
		return nil
	}
}

func TestEndRoundBroadcastsRoundEndWithWinnerAndNextMap(t *testing.T) {
	roundEndBroadcastConfig(t)

	engine := NewGameEngine()
	engine.Round = RoundState{Phase: PhaseActive, RoundNumber: 4, RoundID: "round-4"}
	winner := &BotState{
		BotID:       "winner-bot",
		Name:        "Winner",
		AvatarColor: "#ffce54",
		IsAlive:     true,
		Elo:         1000,
		Position:    NewVec2(100, 100),
	}
	loser := &BotState{
		BotID:       "loser-bot",
		Name:        "Loser",
		AvatarColor: "#47d7ff",
		Elo:         1000,
		Position:    NewVec2(50, 50),
	}
	engine.Bots[winner.BotID] = winner
	engine.Bots[loser.BotID] = loser

	spectator := &SpectatorConn{SendChan: make(chan *SpectatorMessage, 4)}
	engine.AddSpectator(spectator)

	engine.endRound()
	if engine.outbox.spectatorEvent == nil {
		t.Fatal("endRound did not stage a spectator round_end event")
	}
	engine.flushTickOutbox()
	if engine.outbox.spectatorEvent != nil {
		t.Fatal("flushTickOutbox left the staged round_end event behind")
	}

	msg := receiveSpectatorMessage(t, spectator.SendChan)
	if msg["type"] != "round_end" {
		t.Fatalf("message type = %v, want round_end", msg["type"])
	}
	if got := msg["round_number"]; got != float64(4) {
		t.Fatalf("round_number = %v, want 4", got)
	}
	if got := msg["intermission_secs"]; got != 7.5 {
		t.Fatalf("intermission_secs = %v, want 7.5", got)
	}

	winnerView, ok := msg["winner"].(map[string]interface{})
	if !ok {
		t.Fatalf("winner submap missing or malformed: %v", msg["winner"])
	}
	if winnerView["id"] != "winner-bot" || winnerView["name"] != "Winner" || winnerView["color"] != "#ffce54" {
		t.Fatalf("winner = %v, want id/name/color of the last bot standing", winnerView)
	}

	nextMap, ok := msg["next_map"].(map[string]interface{})
	if !ok {
		t.Fatalf("next_map submap missing or malformed: %v", msg["next_map"])
	}
	if got := nextMap["shape"]; got != string(engine.NextMapShape) {
		t.Fatalf("next_map.shape = %v, want pre-generated shape %q", got, engine.NextMapShape)
	}
	size, ok := nextMap["arena_size"].([]interface{})
	if !ok || len(size) != 2 {
		t.Fatalf("next_map.arena_size = %v, want [w h]", nextMap["arena_size"])
	}
	// endRound's terrain pre-generation applies the NEXT round's dynamic
	// sizing to config before the message is staged, and ArenaMap.Reset
	// copies the same values at startRound — so the broadcast must match
	// config exactly for the client's keyframe parity.
	if size[0] != config.C.ArenaWidth || size[1] != config.C.ArenaHeight {
		t.Fatalf("next_map.arena_size = %v, want [%v %v]", size, config.C.ArenaWidth, config.C.ArenaHeight)
	}

	wantObstacles := ExpandObstaclesForClient(engine.NextObstacles, engine.NextMaskRects, engine.NextTerrain.CellSize)
	obstacles, ok := nextMap["obstacles"].([]interface{})
	if !ok {
		t.Fatalf("next_map.obstacles missing or malformed: %v", nextMap["obstacles"])
	}
	if len(obstacles) != len(wantObstacles) {
		t.Fatalf("next_map.obstacles has %d rects, want %d (expanded obstacles + mask rects)", len(obstacles), len(wantObstacles))
	}
	if len(wantObstacles) > 0 {
		first, ok := obstacles[0].(map[string]interface{})
		if !ok {
			t.Fatalf("next_map.obstacles[0] malformed: %v", obstacles[0])
		}
		if first["x"] != wantObstacles[0].X || first["y"] != wantObstacles[0].Y ||
			first["width"] != wantObstacles[0].Width || first["height"] != wantObstacles[0].Height {
			t.Fatalf("next_map.obstacles[0] = %v, want %+v", first, wantObstacles[0])
		}
	}

	if len(engine.NextMaskRects) == 0 {
		t.Fatal("circle map pre-generation produced no mask rects — test setup no longer exercises the carved-boundary path")
	}
	maskRects, ok := nextMap["mask_rects"].([]interface{})
	if !ok {
		t.Fatalf("next_map.mask_rects missing or malformed: %v", nextMap["mask_rects"])
	}
	if len(maskRects) != len(engine.NextMaskRects) {
		t.Fatalf("next_map.mask_rects has %d rects, want %d from the pre-generated terrain", len(maskRects), len(engine.NextMaskRects))
	}
}

func TestEndRoundBroadcastOmitsWinnerWhenNoneResolved(t *testing.T) {
	roundEndBroadcastConfig(t)

	engine := NewGameEngine()
	engine.Round = RoundState{Phase: PhaseActive, RoundNumber: 2, RoundID: "round-2"}
	// No bots at all: DetermineWinner has nobody to rank, so the round ends
	// without a winner and the broadcast must omit the key entirely.
	spectator := &SpectatorConn{SendChan: make(chan *SpectatorMessage, 4)}
	engine.AddSpectator(spectator)

	engine.endRound()
	engine.flushTickOutbox()

	msg := receiveSpectatorMessage(t, spectator.SendChan)
	if msg["type"] != "round_end" {
		t.Fatalf("message type = %v, want round_end", msg["type"])
	}
	if _, present := msg["winner"]; present {
		t.Fatalf("winner key present on a no-winner round: %v", msg["winner"])
	}
	if _, present := msg["next_map"]; !present {
		t.Fatal("next_map missing from a no-winner round_end")
	}
}

func TestRoundEndEventSkippedWithoutSpectators(t *testing.T) {
	roundEndBroadcastConfig(t)

	engine := NewGameEngine()
	engine.Round = RoundState{Phase: PhaseActive, RoundNumber: 1, RoundID: "round-1"}

	engine.endRound()
	if engine.outbox.spectatorEvent == nil {
		t.Fatal("endRound did not stage the round_end event")
	}
	// No spectators connected: the flush must clear the slot without
	// marshaling or panicking.
	engine.flushTickOutbox()
	if engine.outbox.spectatorEvent != nil {
		t.Fatal("flushTickOutbox left the staged round_end event behind with no spectators")
	}
}
