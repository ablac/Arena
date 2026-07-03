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

func TestArenaMapResetPicksUpNewSize(t *testing.T) {
	loadTestConfig(t)
	config.C.ArenaSizeDynamic = true
	config.C.ArenaSizeBaseBots = 12
	config.C.ArenaSizeMaxBots = 48
	config.C.ArenaSizeMaxScale = 2.0
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
