package game

import (
	"testing"
)

func TestKillFeedAdd(t *testing.T) {
	kf := NewKillFeed(5)
	kf.Add("killer1", "victim1", "sword", 1)
	kf.Add("killer2", "victim2", "bow", 2)

	all := kf.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	if all[0].Killer != "killer1" || all[0].Weapon != "sword" {
		t.Errorf("first entry wrong: %+v", all[0])
	}
}

func TestKillFeedCap(t *testing.T) {
	kf := NewKillFeed(3)
	for i := 0; i < 5; i++ {
		kf.Add("k", "v", "sword", i)
	}
	all := kf.GetAll()
	if len(all) != 3 {
		t.Errorf("expected 3 entries (capped), got %d", len(all))
	}
	// Oldest should be gone; last entry tick=4
	if all[len(all)-1].Tick != 4 {
		t.Errorf("last entry tick=%v, want 4", all[len(all)-1].Tick)
	}
}

func TestKillFeedGetRecent(t *testing.T) {
	kf := NewKillFeed(10)
	for i := 0; i < 7; i++ {
		kf.Add("k", "v", "sword", i)
	}
	recent := kf.GetRecent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent, got %d", len(recent))
	}
	if recent[2].Tick != 6 {
		t.Errorf("last recent tick=%v, want 6", recent[2].Tick)
	}
}

func TestKillFeedGetRecentAll(t *testing.T) {
	kf := NewKillFeed(10)
	kf.Add("k", "v", "sword", 1)
	kf.Add("k", "v", "sword", 2)

	// Request more than available
	recent := kf.GetRecent(100)
	if len(recent) != 2 {
		t.Errorf("expected 2, got %d", len(recent))
	}
}

func TestKillFeedGetSince(t *testing.T) {
	kf := NewKillFeed(10)
	kf.Add("k", "v", "sword", 5)
	kf.Add("k", "v", "bow", 10)
	kf.Add("k", "v", "daggers", 15)

	since := kf.GetSince(7)
	if len(since) != 2 {
		t.Fatalf("expected 2 entries since tick 7, got %d", len(since))
	}
	for _, e := range since {
		if e.Tick <= 7 {
			t.Errorf("entry with tick %v should not be in GetSince(7)", e.Tick)
		}
	}
}

func TestKillFeedClear(t *testing.T) {
	kf := NewKillFeed(10)
	kf.Add("k", "v", "sword", 1)
	kf.Clear()

	if len(kf.GetAll()) != 0 {
		t.Error("kill feed not empty after clear")
	}
}

func TestKillFeedEmpty(t *testing.T) {
	kf := NewKillFeed(5)
	all := kf.GetAll()
	if all == nil {
		// nil is fine, just not a crash
	}
	if len(kf.GetRecent(3)) != 0 {
		t.Error("GetRecent on empty should return empty")
	}
	if len(kf.GetSince(0)) != 0 {
		t.Error("GetSince on empty should return empty")
	}
}
