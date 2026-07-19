package game

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestBroadcastToSpectatorsSharesOnePreparedMessage(t *testing.T) {
	first := &SpectatorConn{SendChan: make(chan *SpectatorMessage, 1)}
	second := &SpectatorConn{SendChan: make(chan *SpectatorMessage, 1)}
	payload := []byte(`{"type":"arena_state","tick":42}`)

	BroadcastToSpectators([]*SpectatorConn{first, second}, payload)

	read := func(name string, ch <-chan *SpectatorMessage) *SpectatorMessage {
		t.Helper()
		select {
		case message := <-ch:
			if message == nil {
				t.Fatalf("%s received a nil message", name)
			}
			return message
		case <-time.After(time.Second):
			t.Fatalf("%s did not receive the broadcast", name)
			return nil
		}
	}

	firstMessage := read("first spectator", first.SendChan)
	secondMessage := read("second spectator", second.SendChan)
	if firstMessage != secondMessage {
		t.Fatal("spectators received different message envelopes")
	}
	if firstMessage.Prepared == nil {
		t.Fatal("broadcast did not prepare a reusable WebSocket frame")
	}
	if string(firstMessage.Payload) != string(payload) {
		t.Fatalf("inspectable payload = %q, want %q", firstMessage.Payload, payload)
	}
}

func TestBroadcastToSpectatorsPreservesFinalStateAndRoundEndAtRealCapacity(t *testing.T) {
	const realSendCapacity = SpectatorSendBufferSize
	const arenaFrames = realSendCapacity + 17
	const lobbyFrames = realSendCapacity + 17
	spectator := &SpectatorConn{
		SendChan:     make(chan *SpectatorMessage, realSendCapacity),
		StateChan:    make(chan *SpectatorMessage, 1),
		RoundEndChan: make(chan SpectatorRoundEndBatch, 1),
	}

	for tick := range arenaFrames {
		BroadcastToSpectators(
			[]*SpectatorConn{spectator},
			[]byte(fmt.Sprintf(`{"type":"arena_state","tick":%d}`, tick)),
		)
	}
	BroadcastToSpectators(
		[]*SpectatorConn{spectator},
		[]byte(`{"type":"round_end","round_number":7}`),
	)
	for countdown := range lobbyFrames {
		BroadcastToSpectators(
			[]*SpectatorConn{spectator},
			[]byte(fmt.Sprintf(`{"bots_connected":3,"countdown":%d,"type":"lobby_state"}`, countdown)),
		)
	}

	if got := len(spectator.SendChan); got != realSendCapacity {
		t.Fatalf("spectator send queue length = %d, want bounded capacity %d", got, realSendCapacity)
	}
	if got := len(spectator.StateChan); got != 0 {
		t.Fatalf("final arena state remained outside lifecycle batch: state queue length=%d", got)
	}
	var batch SpectatorRoundEndBatch
	select {
	case batch = <-spectator.RoundEndChan:
	default:
		t.Fatal("real-capacity broadcast dropped round_end lifecycle batch")
	}
	if batch.FinalState == nil || batch.RoundEnd == nil {
		t.Fatalf("incomplete lifecycle batch: final=%v round_end=%v", batch.FinalState != nil, batch.RoundEnd != nil)
	}
	var finalState struct {
		Type string `json:"type"`
		Tick int    `json:"tick"`
	}
	if err := json.Unmarshal(batch.FinalState.Payload, &finalState); err != nil {
		t.Fatalf("decode final arena state %q: %v", batch.FinalState.Payload, err)
	}
	var roundEnd struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(batch.RoundEnd.Payload, &roundEnd); err != nil {
		t.Fatalf("decode round_end %q: %v", batch.RoundEnd.Payload, err)
	}
	if finalState.Type != "arena_state" || finalState.Tick != arenaFrames-1 || roundEnd.Type != "round_end" {
		t.Fatalf("lifecycle batch = final %q tick %d then %q, want newest tick %d then round_end", finalState.Type, finalState.Tick, roundEnd.Type, arenaFrames-1)
	}
	if got := len(spectator.RoundEndChan); got != 0 {
		t.Fatalf("round_end lifecycle queue length after one receive = %d, want exactly one batch", got)
	}
}
