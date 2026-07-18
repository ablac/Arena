package game

import (
	"encoding/json"
	"testing"
	"time"

	"arena-server/internal/config"
)

func TestConnectedGridSizeUsesCeilForFractionalDynamicArena(t *testing.T) {
	loadTestConfig(t)
	originalWidth := config.C.ArenaWidth
	originalHeight := config.C.ArenaHeight
	originalCellSize := config.C.PathfindingCellSize
	t.Cleanup(func() {
		config.C.ArenaWidth = originalWidth
		config.C.ArenaHeight = originalHeight
		config.C.PathfindingCellSize = originalCellSize
	})

	config.C.ArenaWidth = 2111.111111111111
	config.C.ArenaHeight = 2111.111111111111
	config.C.PathfindingCellSize = 20

	message := BuildConnectedMessage(&BotState{}, nil)
	gridSize, ok := message["grid_size"].([2]int)
	if !ok {
		t.Fatalf("connected grid_size type = %T, want [2]int", message["grid_size"])
	}
	if gridSize != [2]int{106, 106} {
		t.Fatalf("connected grid_size = %v, want ceil-aligned [106 106]", gridSize)
	}
}

func TestSendRoundStartDoesNotRevealOpponentPositions(t *testing.T) {
	bot := &BotState{
		BotID:    "bot-a",
		Position: NewVec2(10, 12),
		SendChan: make(chan []byte, 1),
	}
	opponent := &BotState{
		BotID:    "bot-b",
		Position: NewVec2(80, 90),
	}
	bots := map[string]*BotState{
		bot.BotID:      bot,
		opponent.BotID: opponent,
	}

	SendRoundStart(bot, RoundState{RoundNumber: 7}, bots, NewArenaMap())

	select {
	case payload := <-bot.SendChan:
		var message map[string]interface{}
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Fatalf("decode round_start: %v", err)
		}
		positions, exists := message["all_positions"].(map[string]interface{})
		if !exists {
			t.Fatalf("round_start removed the legacy all_positions shape: %s", payload)
		}
		if len(positions) != 1 || positions[bot.BotID] == nil {
			t.Fatalf("legacy all_positions = %v, want only the receiving bot", positions)
		}
		if _, leaked := positions[opponent.BotID]; leaked {
			t.Fatalf("round_start leaks opponent position: %s", payload)
		}
		if got := int(message["bots_in_round"].(float64)); got != len(bots) {
			t.Fatalf("bots_in_round = %d, want %d", got, len(bots))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for round_start")
	}
}

func TestTickDeliveryCoalescesWithoutDisplacingControlMessages(t *testing.T) {
	bot := &BotState{
		BotID:    "slow-bot",
		SendChan: make(chan []byte, 1),
		TickChan: make(chan []byte, 1),
	}
	control := []byte(`{"type":"round_start"}`)
	bot.SendChan <- control
	arena := NewArenaMap()
	yourState := &YourStateView{IsAlive: true}
	safeZone := BuildSafeZoneGridView(arena)

	SendTickUpdate(bot, NewTickMessage(yourState, nil, 10, safeZone, nil, 8))
	SendTickUpdate(bot, NewTickMessage(yourState, nil, 11, safeZone, nil, 8))

	if got := <-bot.SendChan; string(got) != string(control) {
		t.Fatalf("control queue was displaced by tick: %s", got)
	}
	select {
	case payload := <-bot.TickChan:
		var message map[string]interface{}
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Fatalf("decode latest tick: %v", err)
		}
		if got := int(message["tick"].(float64)); got != 11 {
			t.Fatalf("coalesced tick=%d, want latest 11", got)
		}
	default:
		t.Fatal("latest tick was not queued")
	}
}

func TestRoundEndDiscardsPendingActiveTick(t *testing.T) {
	bot := &BotState{
		BotID:    "ending-bot",
		SendChan: make(chan []byte, 1),
		TickChan: make(chan []byte, 1),
	}
	bot.TickChan <- []byte(`{"type":"tick","tick":99,"your_state":{"is_alive":true}}`)

	SendRoundEnd(bot, RoundEndInfo{RoundNumber: 4}, 5)

	select {
	case stale := <-bot.TickChan:
		t.Fatalf("pending active tick survived round_end: %s", stale)
	default:
	}
	select {
	case payload := <-bot.SendChan:
		var message map[string]interface{}
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Fatalf("decode round_end: %v", err)
		}
		if message["type"] != "round_end" {
			t.Fatalf("control message type=%v, want round_end", message["type"])
		}
	default:
		t.Fatal("round_end was not queued")
	}
}
