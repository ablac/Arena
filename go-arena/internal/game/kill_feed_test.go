package game

import "testing"

func TestKillFeedViewCache(t *testing.T) {
	kf := NewKillFeed(10)
	kf.Add("a", "b", "sword", 1)
	kf.Add("c", "d", "bow", 2)

	v1 := kf.RecentViews(5)
	if len(v1) != 2 {
		t.Fatalf("expected 2 views, got %d", len(v1))
	}
	// Same slice returned while the feed is unchanged.
	if &v1[0] != &kf.RecentViews(5)[0] {
		t.Error("expected cached slice to be reused")
	}

	all := kf.AllViews()
	if len(all) != 2 {
		t.Fatalf("expected 2 views from AllViews, got %d", len(all))
	}

	// Adding invalidates the cache.
	kf.Add("e", "f", "staff", 3)
	v2 := kf.RecentViews(5)
	if len(v2) != 3 {
		t.Fatalf("expected 3 views after add, got %d", len(v2))
	}
	if v2[2].Killer != "e" {
		t.Errorf("expected newest entry last, got %v", v2[2])
	}

	kf.Clear()
	if len(kf.RecentViews(5)) != 0 {
		t.Error("expected empty views after Clear")
	}
}
