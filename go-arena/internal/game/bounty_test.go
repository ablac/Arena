package game

import "testing"

// bountyFixture builds a BountySystem with two bots tied on (BountyPoints,
// WinStreak) plus a bots map, exercising Update through the exported surface.
func bountyFixture(pointsA, streakA, pointsB, streakB int) (*BountySystem, map[string]*BotState) {
	bs := NewBountySystem()
	bs.entries["alpha"] = &BountyEntry{BotID: "alpha", Name: "Alpha", WinStreak: streakA, BountyPoints: pointsA}
	bs.entries["bravo"] = &BountyEntry{BotID: "bravo", Name: "Bravo", WinStreak: streakB, BountyPoints: pointsB}
	bots := map[string]*BotState{
		"alpha": {BotID: "alpha", Name: "Alpha", IsAlive: true},
		"bravo": {BotID: "bravo", Name: "Bravo", IsAlive: true},
	}
	return bs, bots
}

// Issue #14: a tie must not flap the crown between the two tied bots every
// tick. Run Update many times over a map (randomized iteration order) and
// assert the target never changes once picked.
func TestBountyTargetStableOnTie(t *testing.T) {
	bs, bots := bountyFixture(10, 3, 10, 3)

	bs.Update(bots)
	first := bs.TargetID
	if first == "" {
		t.Fatal("expected a target to be selected among two tied bots")
	}
	// Deterministic first pick: lowest BotID wins a genuine no-incumbent tie.
	if first != "alpha" {
		t.Fatalf("expected deterministic tie pick 'alpha' (lowest BotID), got %q", first)
	}
	for i := 0; i < 500; i++ {
		bs.Update(bots)
		if bs.TargetID != first {
			t.Fatalf("crown flapped on iteration %d: was %q, now %q", i, first, bs.TargetID)
		}
	}
}

// A genuinely-better challenger DOES take the crown, and then holds it.
func TestBountyChallengerTakesCrownThenHolds(t *testing.T) {
	bs, bots := bountyFixture(10, 3, 10, 3)
	bs.Update(bots)
	if bs.TargetID != "alpha" {
		t.Fatalf("setup: expected 'alpha', got %q", bs.TargetID)
	}

	// bravo pulls strictly ahead on points.
	bs.entries["bravo"].BountyPoints = 12
	bs.Update(bots)
	if bs.TargetID != "bravo" {
		t.Fatalf("strictly-better challenger 'bravo' should take the crown, got %q", bs.TargetID)
	}
	// Now alpha ties bravo again: incumbent bravo must KEEP it (hysteresis),
	// even though alpha has the lower BotID.
	bs.entries["alpha"].BountyPoints = 12
	for i := 0; i < 200; i++ {
		bs.Update(bots)
		if bs.TargetID != "bravo" {
			t.Fatalf("incumbent 'bravo' lost the crown on a tie at iteration %d (got %q)", i, bs.TargetID)
		}
	}
}

// The incumbent losing its lead hands the crown to the new strict leader.
func TestBountyIncumbentDethronedWhenBeaten(t *testing.T) {
	bs, bots := bountyFixture(12, 3, 10, 3)
	bs.Update(bots)
	if bs.TargetID != "alpha" {
		t.Fatalf("setup: expected leader 'alpha', got %q", bs.TargetID)
	}
	bs.entries["bravo"].BountyPoints = 20 // bravo now strictly ahead
	bs.Update(bots)
	if bs.TargetID != "bravo" {
		t.Fatalf("expected 'bravo' to take the crown after beating the incumbent, got %q", bs.TargetID)
	}
}

// A dead incumbent must not retain the crown.
func TestBountyDeadIncumbentReleasesCrown(t *testing.T) {
	bs, bots := bountyFixture(12, 3, 10, 3)
	bs.Update(bots)
	if bs.TargetID != "alpha" {
		t.Fatalf("setup: expected 'alpha', got %q", bs.TargetID)
	}
	bots["alpha"].IsAlive = false
	bs.Update(bots)
	if bs.TargetID != "bravo" {
		t.Fatalf("dead incumbent should release the crown to 'bravo', got %q", bs.TargetID)
	}
}
