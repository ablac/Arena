package demobots

import (
	"context"
	"testing"
)

func TestStopResetsParentContextForLaterStarts(t *testing.T) {
	m := NewManager("http://127.0.0.1:1", 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.parentCtx = ctx
	m.cancel = cancel
	m.started = true

	m.Stop()

	if m.parentCtx != nil {
		t.Fatal("Stop should clear the canceled parent context so later starts can create a fresh one")
	}
	if m.cancel != nil {
		t.Fatal("Stop should clear the canceled function after shutdown")
	}
}

func TestStartTemplateRegistersEntriesBeforeLaunch(t *testing.T) {
	m := &Manager{
		serverURL: "http://127.0.0.1:1",
		bots:      make(map[string]*botEntry),
	}
	cfg := BotConfig{
		Name:     "Template",
		Weapon:   "bow",
		Strategy: "kite",
		Color:    "#00ff88",
		Stats:    map[string]int{"hp": 4, "speed": 6, "attack": 8, "defense": 2},
	}

	names := m.StartTemplate(cfg, 3)
	if len(names) != 3 {
		t.Fatalf("StartTemplate returned %d names, want 3", len(names))
	}
	if m.Count() != 3 {
		t.Fatalf("manager Count() = %d, want 3 registered entries", m.Count())
	}
	if names[0] != "Template" || names[1] != "Template-2" || names[2] != "Template-3" {
		t.Fatalf("unexpected generated names: %v", names)
	}
}
