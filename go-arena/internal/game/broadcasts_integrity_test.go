package game

import (
	"encoding/json"
	"testing"
	"time"
)

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
