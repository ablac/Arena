package game

import (
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
