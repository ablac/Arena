package game

import "testing"

// TestBuildHazardZoneViewReportsActualDamageSpan guards against the
// reported width/height drifting from the real hitbox. isBotInHazardZone
// and hazardRectOpen both extend zone.Width/2 tiles in each direction from
// center (integer division), covering 2*(Width/2)+1 tiles - one tile wider
// than the raw Width value whenever Width is even. A bot trusting the raw
// field to compute a safe standing distance would still take damage just
// outside what it was told was the zone.
func TestBuildHazardZoneViewReportsActualDamageSpan(t *testing.T) {
	previousTerrain := ActiveTerrain
	ActiveTerrain = openMovementTerrain(20, 20)
	t.Cleanup(func() { ActiveTerrain = previousTerrain })

	cases := []struct {
		width, height int
		wantW, wantH  int
	}{
		{width: 2, height: 2, wantW: 3, wantH: 3},
		{width: 3, height: 3, wantW: 3, wantH: 3}, // already exact for odd values
		{width: 4, height: 4, wantW: 5, wantH: 5},
	}

	for _, tc := range cases {
		zone := HazardZone{
			ID: "hz", Position: ActiveTerrain.GridToWorld([2]int{10, 10}),
			Width: tc.width, Height: tc.height, Active: true,
		}
		view := BuildHazardZoneView(zone, false, RoundModifierNone)
		gotW, _ := view["width"].(int)
		gotH, _ := view["height"].(int)
		if gotW != tc.wantW || gotH != tc.wantH {
			t.Fatalf("Width=%d Height=%d: reported view = {width:%d height:%d}, want {width:%d height:%d}",
				tc.width, tc.height, gotW, gotH, tc.wantW, tc.wantH)
		}

		// The reported span must match how many cells actually damage a bot.
		halfW := tc.width / 2
		for dx := -gotW / 2; dx <= gotW/2; dx++ {
			pos := ActiveTerrain.GridToWorld([2]int{10 + dx, 10})
			inZone := isBotInHazardZone(pos, &zone)
			wantIn := dx >= -halfW && dx <= halfW
			if inZone != wantIn {
				t.Fatalf("Width=%d dx=%d: isBotInHazardZone=%v, want %v (reported width=%d should match the real span)",
					tc.width, dx, inZone, wantIn, gotW)
			}
		}
	}
}
