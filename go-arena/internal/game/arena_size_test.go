package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestDynamicArenaSize(t *testing.T) {
	loadTestConfig(t)
	config.C.ArenaSizeDynamic = true
	config.C.ArenaSizeBaseBots = 12
	config.C.ArenaSizeMaxBots = 48
	config.C.ArenaSizeMaxScale = 2.0
	// This test covers the scale-UP curve plus the ops escape hatch:
	// ARENA_SIZE_MIN_SCALE 1.0 keeps counts below BaseBots at base size.
	config.C.ArenaSizeMinScale = 1.0

	// Force base capture with the configured 2000x2000.
	ApplyDynamicArenaSize(0)
	baseW := config.C.ArenaWidth

	// At or below the base bot count: base size.
	if s := ApplyDynamicArenaSize(12); s != 1.0 || config.C.ArenaWidth != baseW {
		t.Errorf("at base bots: scale=%.2f width=%.0f, want 1.0 / %.0f", s, config.C.ArenaWidth, baseW)
	}

	// Midpoint: halfway to max scale.
	if s := ApplyDynamicArenaSize(30); s != 1.5 || config.C.ArenaWidth != baseW*1.5 {
		t.Errorf("midpoint: scale=%.2f width=%.0f, want 1.5 / %.0f", s, config.C.ArenaWidth, baseW*1.5)
	}

	// At or beyond max bots: capped at max scale, zone centre scales too.
	if s := ApplyDynamicArenaSize(100); s != 2.0 || config.C.ArenaWidth != baseW*2 {
		t.Errorf("max: scale=%.2f width=%.0f, want 2.0 / %.0f", s, config.C.ArenaWidth, baseW*2)
	}
	if config.C.ZoneCenterX != baseW {
		t.Errorf("zone centre should scale with the arena: got %.0f, want %.0f", config.C.ZoneCenterX, baseW)
	}

	// Repeated application never compounds.
	ApplyDynamicArenaSize(100)
	if config.C.ArenaWidth != baseW*2 {
		t.Errorf("scaling compounded: width=%.0f, want %.0f", config.C.ArenaWidth, baseW*2)
	}

	// Shrinks back down when the crowd leaves and restores base when disabled.
	ApplyDynamicArenaSize(12)
	if config.C.ArenaWidth != baseW {
		t.Errorf("did not shrink back to base: width=%.0f", config.C.ArenaWidth)
	}
	config.C.ArenaSizeDynamic = false
	if s := ApplyDynamicArenaSize(100); s != 1.0 || config.C.ArenaWidth != baseW {
		t.Errorf("disabled: scale=%.2f width=%.0f, want 1.0 / %.0f", s, config.C.ArenaWidth, baseW)
	}
	config.C.ArenaSizeDynamic = true
}

func TestDynamicArenaSizeScaleDown(t *testing.T) {
	loadTestConfig(t)
	config.C.ArenaSizeDynamic = true
	config.C.ArenaSizeBaseBots = 12
	config.C.ArenaSizeMaxBots = 48
	config.C.ArenaSizeMaxScale = 2.0
	config.C.ArenaSizeMinScale = 0.6
	defer func() {
		config.C.ArenaSizeMinScale = 1.0
		ApplyDynamicArenaSize(config.C.ArenaSizeBaseBots) // restore base dims
	}()

	// Capture the pristine base via the escape hatch (min-scale ignored at base).
	ApplyDynamicArenaSize(config.C.ArenaSizeBaseBots)
	baseW := config.C.ArenaWidth

	// The 2-bot floor: a duel plays on the smallest arena.
	if s := ApplyDynamicArenaSize(2); s != 0.6 || config.C.ArenaWidth != baseW*0.6 {
		t.Errorf("2-bot floor: scale=%.2f width=%.0f, want 0.6 / %.0f", s, config.C.ArenaWidth, baseW*0.6)
	}

	// Below the floor anchor: clamped to min scale, never smaller.
	if s := ApplyDynamicArenaSize(0); s != 0.6 {
		t.Errorf("below floor: scale=%.2f, want clamp at 0.6", s)
	}

	// Midpoint of the down-curve: 7 bots, t=(7-2)/(12-2)=0.5, scale 0.8.
	if s := ApplyDynamicArenaSize(7); s != 0.8 || config.C.ArenaWidth != baseW*0.8 {
		t.Errorf("down midpoint: scale=%.2f width=%.0f, want 0.8 / %.0f", s, config.C.ArenaWidth, baseW*0.8)
	}

	// The exact BaseBots boundary: precisely base size, no discontinuity.
	if s := ApplyDynamicArenaSize(12); s != 1.0 || config.C.ArenaWidth != baseW {
		t.Errorf("base boundary: scale=%.2f width=%.0f, want 1.0 / %.0f", s, config.C.ArenaWidth, baseW)
	}

	// Zone centre scales down in lockstep so the cover circle stays centred.
	ApplyDynamicArenaSize(2)
	if config.C.ZoneCenterX != baseW*0.6/2 {
		t.Errorf("zone centre did not scale down: got %.0f, want %.0f", config.C.ZoneCenterX, baseW*0.6/2)
	}

	// Degenerate min-scale configs disable shrinking instead of exploding.
	for _, bad := range []float64{0, -1, 1.5} {
		config.C.ArenaSizeMinScale = bad
		if s := ApplyDynamicArenaSize(2); s != 1.0 {
			t.Errorf("degenerate min-scale %v: scale=%.2f, want 1.0", bad, s)
		}
	}
	config.C.ArenaSizeMinScale = 0.6

	// Dynamic sizing disabled restores base regardless of count.
	config.C.ArenaSizeDynamic = false
	if s := ApplyDynamicArenaSize(2); s != 1.0 || config.C.ArenaWidth != baseW {
		t.Errorf("disabled: scale=%.2f width=%.0f, want 1.0 / %.0f", s, config.C.ArenaWidth, baseW)
	}
	config.C.ArenaSizeDynamic = true
}

func TestMidRoundCountChangeDoesNotResizeMap(t *testing.T) {
	loadTestConfig(t)
	config.C.ArenaSizeDynamic = true
	config.C.ArenaSizeBaseBots = 12
	config.C.ArenaSizeMaxBots = 48
	config.C.ArenaSizeMaxScale = 2.0
	config.C.ArenaSizeMinScale = 0.6
	defer func() {
		config.C.ArenaSizeMinScale = 1.0
		ApplyDynamicArenaSize(config.C.ArenaSizeBaseBots)
	}()

	// Round starts as a duel: the live map is built at the small size.
	ApplyDynamicArenaSize(2)
	m := NewArenaMap()
	m.Reset(nil)
	duelW := m.Width

	// Bots join mid-round: sizing may be recomputed for the NEXT round
	// (intermission pre-gen), but the live map must not change until Reset.
	ApplyDynamicArenaSize(48)
	if m.Width != duelW {
		t.Errorf("live map resized mid-round: %.0f, want %.0f", m.Width, duelW)
	}

	// The next round's Reset picks the new size up.
	m.Reset(nil)
	if m.Width == duelW {
		t.Error("Reset did not pick up the recomputed size for the next round")
	}
}

func TestArenaMapResetPicksUpNewSize(t *testing.T) {
	loadTestConfig(t)
	config.C.ArenaSizeDynamic = true
	config.C.ArenaSizeBaseBots = 12
	config.C.ArenaSizeMaxBots = 48
	config.C.ArenaSizeMaxScale = 2.0
	config.C.ArenaSizeMinScale = 1.0
	ApplyDynamicArenaSize(0)
	defer ApplyDynamicArenaSize(0) // restore base for other tests

	m := NewArenaMap()
	baseW := m.Width

	ApplyDynamicArenaSize(48)
	m.Reset(nil)
	if m.Width != baseW*2 || m.Height != baseW*2 {
		t.Errorf("Reset did not pick up scaled dims: %vx%v, want %vx%v", m.Width, m.Height, baseW*2, baseW*2)
	}
	// Zone must cover the scaled map: farthest corner inside.
	if !m.IsInZone(NewVec2(m.Width, m.Height)) {
		t.Error("scaled map corner not inside initial zone")
	}
}
