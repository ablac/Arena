package game

import "testing"

// TestGravityWellPullsForFullConfiguredDuration guards against
// UpdateGravityWells decrementing RemainingTicks before checking expiry,
// which would consume one tick of the well's life before it ever got to
// pull anything - delivering only N-1 of the N ticks (documented to bots in
// botsetup.go as "3 seconds") a well is supposed to last.
func TestGravityWellPullsForFullConfiguredDuration(t *testing.T) {
	withIntegrityTestRules(t)
	ActiveTerrain = openMovementTerrain(12, 6)

	const duration = 5
	wells := []GravityWell{{
		OwnerID:        "owner",
		Position:       ActiveTerrain.GridToWorld([2]int{6, 2}),
		RemainingTicks: duration,
		PullRadius:     10,
		PullForce:      1,
	}}
	bots := map[string]*BotState{}
	grid := NewSpatialGrid(20)

	for tick := 1; tick <= duration; tick++ {
		UpdateGravityWells(&wells, bots, grid)
		if len(wells) != 1 {
			t.Fatalf("well expired after only %d of its configured %d ticks", tick, duration)
		}
	}
	UpdateGravityWells(&wells, bots, grid)
	if len(wells) != 0 {
		t.Fatalf("well survived past its configured %d-tick duration", duration)
	}
}
