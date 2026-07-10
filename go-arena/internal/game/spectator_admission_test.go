package game

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestTryAddSpectatorEnforcesCapAtomically(t *testing.T) {
	engine := NewGameEngine()
	const (
		attempts = 100
		capacity = 7
	)

	var admitted atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if engine.TryAddSpectator(&SpectatorConn{}, capacity) {
				admitted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := int(admitted.Load()); got != capacity {
		t.Fatalf("admitted = %d, want %d", got, capacity)
	}
	if got := engine.SpectatorCount(); got != capacity {
		t.Fatalf("spectator count = %d, want %d", got, capacity)
	}
	if engine.TryAddSpectator(&SpectatorConn{}, capacity) {
		t.Fatal("admitted spectator after cap was full")
	}
}

func TestTryAddSpectatorRejectsInvalidAdmission(t *testing.T) {
	engine := NewGameEngine()
	if engine.TryAddSpectator(nil, 1) {
		t.Fatal("nil spectator was admitted")
	}
	if engine.TryAddSpectator(&SpectatorConn{}, 0) {
		t.Fatal("spectator was admitted with zero capacity")
	}
	if got := engine.SpectatorCount(); got != 0 {
		t.Fatalf("spectator count = %d, want 0", got)
	}
}
