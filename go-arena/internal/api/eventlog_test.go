package api

import (
	"sync"
	"testing"
)

func TestEventBusUsesOneGlobalEventSequence(t *testing.T) {
	bus := NewEventBus()
	bus.Emit(DashboardEvent{Type: EventConnection, Data: map[string]interface{}{"name": "one"}})
	bus.Emit(DashboardEvent{Type: EventGameEvent, Data: map[string]interface{}{"name": "two"}})
	bus.Emit(DashboardEvent{Type: EventHTTPRequest, Data: map[string]interface{}{"name": "three"}})

	all := bus.AllEvents.GetAll()
	if len(all) != 3 {
		t.Fatalf("all event count = %d, want 3", len(all))
	}
	for i, evt := range all {
		want := int64(i + 1)
		if evt.ID != want {
			t.Fatalf("all event %d ID = %d, want %d", i, evt.ID, want)
		}
	}
	if got := bus.Connections.GetAll()[0].ID; got != all[0].ID {
		t.Fatalf("connection history ID = %d, live/global ID = %d", got, all[0].ID)
	}
	if got := bus.GameEvents.GetAll()[0].ID; got != all[1].ID {
		t.Fatalf("game history ID = %d, live/global ID = %d", got, all[1].ID)
	}
}

func TestEventBusConcurrentEmitRemainsMonotonic(t *testing.T) {
	bus := NewEventBus()
	const count = 100
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Emit(DashboardEvent{Type: EventGameEvent, Data: map[string]interface{}{}})
		}()
	}
	wg.Wait()

	all := bus.AllEvents.GetAll()
	if len(all) != count {
		t.Fatalf("all event count = %d, want %d", len(all), count)
	}
	for i, evt := range all {
		want := int64(i + 1)
		if evt.ID != want {
			t.Fatalf("event %d ID = %d, want %d", i, evt.ID, want)
		}
	}
}
