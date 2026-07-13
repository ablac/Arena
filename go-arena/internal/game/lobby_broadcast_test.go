package game

import (
	"encoding/json"
	"fmt"
	"testing"

	"arena-server/internal/config"
)

func TestLobbyBroadcastSharesOneMarshaledPayloadAcrossBots(t *testing.T) {
	originalConfig := config.C
	config.C.MinBotsToStart = 1000
	config.C.TickRate = 10
	t.Cleanup(func() { config.C = originalConfig })

	t.Run("lobby phase", func(t *testing.T) {
		bots := newLobbyBroadcastTestBots(4)
		engine := &GameEngine{
			Bots:        bots,
			WaitingBots: make(map[string]*BotState),
			Round:       RoundState{Phase: PhaseLobby},
			TickCount:   2,
		}

		engine.tickLobby(&config.C)

		assertSharedLobbyPayload(t, bots)
	})

	t.Run("waiting room", func(t *testing.T) {
		waitingBots := newLobbyBroadcastTestBots(4)
		engine := &GameEngine{
			Bots:        make(map[string]*BotState),
			WaitingBots: waitingBots,
			Round:       RoundState{Phase: PhaseActive},
		}

		engine.sendLobbyStateUpdate()

		assertSharedLobbyPayload(t, waitingBots)
	})
}

func newLobbyBroadcastTestBots(count int) map[string]*BotState {
	bots := make(map[string]*BotState, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("bot-%03d", i)
		bots[id] = &BotState{
			BotID:       id,
			Name:        fmt.Sprintf("Player %03d", count-i),
			AvatarColor: "#00aaff",
			Weapon:      "sword",
			SendChan:    make(chan []byte, 1),
		}
	}
	return bots
}

func assertSharedLobbyPayload(t *testing.T, bots map[string]*BotState) {
	t.Helper()

	var first []byte
	for id, bot := range bots {
		select {
		case payload := <-bot.SendChan:
			if len(payload) == 0 {
				t.Fatalf("bot %s received an empty lobby payload", id)
			}
			if first == nil {
				first = payload
				continue
			}
			if &payload[0] != &first[0] {
				t.Fatalf("bot %s received a separately marshaled lobby buffer; want one shared payload per broadcast", id)
			}
		default:
			t.Fatalf("bot %s did not receive a lobby payload", id)
		}
	}

	var message struct {
		Type    string `json:"type"`
		Players []struct {
			Name string `json:"name"`
		} `json:"players"`
	}
	if err := json.Unmarshal(first, &message); err != nil {
		t.Fatalf("decode lobby payload: %v", err)
	}
	if message.Type != "lobby" {
		t.Fatalf("lobby payload type = %q, want lobby", message.Type)
	}
	for i := 1; i < len(message.Players); i++ {
		if message.Players[i-1].Name > message.Players[i].Name {
			t.Fatalf("lobby players are not sorted by name: %#v", message.Players)
		}
	}
}
