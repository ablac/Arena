package game

import (
	"math"
	"testing"

	"arena-server/internal/config"
)

func TestNewArenaMap(t *testing.T) {
	config.Load()
	m := NewArenaMap()
	if m == nil {
		t.Fatal("NewArenaMap returned nil")
	}
	if m.Width <= 0 || m.Height <= 0 {
		t.Errorf("arena size: %vx%v", m.Width, m.Height)
	}
	if m.ZoneRadius <= 0 {
		t.Errorf("ZoneRadius=%v", m.ZoneRadius)
	}
}

func TestArenaMapReset(t *testing.T) {
	config.Load()
	m := NewArenaMap()
	m.ZoneRadius = 5
	m.Reset(nil)
	// ZoneRadius should be reset to full
	if m.ZoneRadius < m.MinRadius {
		t.Errorf("ZoneRadius %v below MinRadius %v after reset", m.ZoneRadius, m.MinRadius)
	}
}

func TestArenaMapIsInZone(t *testing.T) {
	config.Load()
	m := NewArenaMap()
	center := m.ZoneCenter
	m.ZoneRadius = 100

	// Center should be in zone
	if !m.IsInZone(center) {
		t.Error("center should be in zone")
	}

	// Far away should be out
	outside := NewVec2(center.X()+500, center.Y())
	if m.IsInZone(outside) {
		t.Error("far point should be outside zone")
	}
}

func TestArenaMapDistanceToZoneEdge(t *testing.T) {
	config.Load()
	m := NewArenaMap()
	m.ZoneRadius = 100

	// At center: should be ~0 distance to zone interior edge (or near radius)
	d := m.DistanceToZoneEdge(m.ZoneCenter)
	_ = d // just ensure no panic

	// DistanceToZoneEdge returns zoneRadius - distToCenter.
	// A point outside the zone gets a negative value (how far outside).
	outside := NewVec2(m.ZoneCenter.X()+500, m.ZoneCenter.Y())
	d2 := m.DistanceToZoneEdge(outside)
	// 100 - 500 = -400: point is 400 units outside the zone
	if d2 >= 0 {
		t.Errorf("outside point should have negative DistanceToZoneEdge (outside zone), got %v", d2)
	}
}

func TestArenaMapGetSpawnPoints(t *testing.T) {
	config.Load()
	m := NewArenaMap()
	points := m.GetSpawnPoints(4)
	if len(points) != 4 {
		t.Errorf("expected 4 spawn points, got %d", len(points))
	}
	// Spawn points should be within arena bounds
	for i, p := range points {
		if p.X() < 0 || p.X() > m.Width || p.Y() < 0 || p.Y() > m.Height {
			t.Errorf("spawn point %d out of bounds: %v", i, p)
		}
	}
}

func TestArenaMapGetSpawnPointsZero(t *testing.T) {
	config.Load()
	m := NewArenaMap()
	points := m.GetSpawnPoints(0)
	if len(points) != 0 {
		t.Errorf("expected 0 spawn points, got %d", len(points))
	}
}

func TestArenaMapGetSpawnPoint(t *testing.T) {
	config.Load()
	m := NewArenaMap()
	p := m.GetSpawnPoint()
	if p.X() < 0 || p.X() > m.Width || p.Y() < 0 || p.Y() > m.Height {
		t.Errorf("spawn point out of bounds: %v", p)
	}
}

func TestArenaMapClampToArena(t *testing.T) {
	config.Load()
	m := NewArenaMap()

	// Negative coords
	clamped := m.ClampToArena(NewVec2(-100, -100))
	if clamped.X() < 0 || clamped.Y() < 0 {
		t.Errorf("clamped negative coords: %v", clamped)
	}

	// Beyond width/height
	clamped = m.ClampToArena(NewVec2(m.Width+100, m.Height+100))
	if clamped.X() > m.Width || clamped.Y() > m.Height {
		t.Errorf("clamped large coords: %v", clamped)
	}
}

func TestArenaMapUpdateZoneShrinks(t *testing.T) {
	config.Load()
	m := NewArenaMap()
	initialRadius := m.ZoneRadius

	// Advance to a tick well past the shrink start
	roundStartTick := 0
	for i := 0; i < 1000; i++ {
		m.UpdateZone(i, roundStartTick)
	}

	if m.ZoneRadius >= initialRadius {
		t.Errorf("zone should shrink over time: initial=%v current=%v", initialRadius, m.ZoneRadius)
	}
}

func TestArenaMapUpdateZoneNoShrinkEarly(t *testing.T) {
	config.Load()
	m := NewArenaMap()
	initialRadius := m.ZoneRadius

	// Just a few ticks in — zone shouldn't shrink yet
	m.UpdateZone(1, 0)
	m.UpdateZone(2, 0)

	if m.ZoneRadius != initialRadius {
		t.Errorf("zone should not shrink in first few ticks: initial=%v current=%v",
			initialRadius, m.ZoneRadius)
	}
}

func TestArenaMapZoneRadiusMinCap(t *testing.T) {
	config.Load()
	m := NewArenaMap()

	// Run many, many ticks — zone should not go below minimum
	for i := 0; i < 100000; i++ {
		m.UpdateZone(i, 0)
	}

	if m.ZoneRadius < m.MinRadius {
		t.Errorf("zone radius %v went below minimum %v", m.ZoneRadius, m.MinRadius)
	}
}

func TestArenaMapSpawnPointsSpread(t *testing.T) {
	config.Load()
	m := NewArenaMap()
	points := m.GetSpawnPoints(4)
	if len(points) < 2 {
		return
	}
	// Spawns should be reasonably spread out
	minDist := math.Inf(1)
	for i := 0; i < len(points); i++ {
		for j := i + 1; j < len(points); j++ {
			d := points[i].DistanceTo(points[j])
			if d < minDist {
				minDist = d
			}
		}
	}
	if minDist < 50 {
		t.Errorf("spawn points too close: minDist=%v", minDist)
	}
}
