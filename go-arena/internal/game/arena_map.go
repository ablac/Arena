package game

import (
	"math"
	"math/rand"

	"arena-server/internal/config"
)

// ArenaMap manages arena boundaries, the shrinking safe zone, and obstacles.
type ArenaMap struct {
	Width     float64
	Height    float64
	Obstacles []Obstacle

	// Safe zone (circle)
	ZoneCenter       Vec2
	ZoneRadius       float64
	ZoneTargetCenter Vec2
	ZoneTargetRadius float64

	// Zone shrink state
	InitialRadius    float64
	MinRadius        float64
	ShrinkDelayTicks int
	ShrinkStarted    bool
	ZoneLerpT        float64 // 0.0 to 1.0
}

// NewArenaMap creates an ArenaMap initialised from the global config.
func NewArenaMap() *ArenaMap {
	c := &config.C
	center := NewVec2(c.ZoneCenterX, c.ZoneCenterY)
	return &ArenaMap{
		Width:            c.ArenaWidth,
		Height:           c.ArenaHeight,
		ZoneCenter:       center,
		ZoneRadius:       c.ZoneInitialRadius,
		ZoneTargetCenter: center,
		ZoneTargetRadius: c.ZoneMinRadius,
		InitialRadius:    c.ZoneInitialRadius,
		MinRadius:        c.ZoneMinRadius,
		ShrinkDelayTicks: int(c.ZoneShrinkDelay * float64(c.TickRate)),
	}
}

// Reset prepares the map for a new round: restores the zone to its initial
// state, assigns the given obstacle slice, and picks a random drift target for
// the zone centre.
func (m *ArenaMap) Reset(obstacles []Obstacle) {
	c := &config.C

	m.Obstacles = obstacles
	m.ZoneRadius = m.InitialRadius
	m.ZoneCenter = NewVec2(c.ZoneCenterX, c.ZoneCenterY)
	m.ShrinkStarted = false
	m.ZoneLerpT = 0.0

	// Random target centre; zone must still fit inside the arena.
	margin := m.MinRadius
	tx := margin + rand.Float64()*(m.Width-2*margin)
	ty := margin + rand.Float64()*(m.Height-2*margin)
	m.ZoneTargetCenter = NewVec2(tx, ty)
	m.ZoneTargetRadius = m.MinRadius
}

// UpdateZone shrinks the safe zone based on the configured timing profile.
func (m *ArenaMap) UpdateZone(tickCount int, roundStartTick int) {
	c := &config.C
	roundTotalTicks := int(c.RoundDuration * float64(c.TickRate))
	intervalTicks := int(c.ZoneShrinkInterval * float64(c.TickRate))
	m.UpdateZoneProfile(tickCount, roundStartTick, m.ShrinkDelayTicks, intervalTicks, c.ZoneShrinkPercent, roundTotalTicks)
}

// UpdateZoneProfile shrinks the zone using explicit timing values. This lets
// special rounds override shrink speed without mutating global config.
func (m *ArenaMap) UpdateZoneProfile(tickCount int, roundStartTick int, delayTicks int, intervalTicks int, shrinkPercent float64, roundTotalTicks int) {
	elapsed := tickCount - roundStartTick

	// Still in the delay window, no shrinking yet.
	if elapsed < delayTicks {
		return
	}

	m.ShrinkStarted = true

	shrinkTicks := roundTotalTicks - delayTicks
	if shrinkTicks <= 0 {
		return
	}

	shrinkElapsed := elapsed - delayTicks
	progress := float64(shrinkElapsed) / float64(shrinkTicks)
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	m.ZoneLerpT = progress

	if intervalTicks <= 0 {
		intervalTicks = 1
	}

	shrinkFactor := 1.0 - shrinkPercent
	if shrinkFactor < 0 {
		shrinkFactor = 0
	}
	if shrinkFactor > 1 {
		shrinkFactor = 1
	}

	completedSteps := shrinkElapsed / intervalTicks
	stepProgress := float64(shrinkElapsed%intervalTicks) / float64(intervalTicks)

	startRadius := m.InitialRadius
	for i := 0; i < completedSteps && startRadius > m.MinRadius; i++ {
		startRadius *= shrinkFactor
		if startRadius < m.MinRadius {
			startRadius = m.MinRadius
		}
	}

	endRadius := startRadius
	if endRadius > m.MinRadius {
		endRadius *= shrinkFactor
		if endRadius < m.MinRadius {
			endRadius = m.MinRadius
		}
	}

	m.ZoneRadius = startRadius + (endRadius-startRadius)*stepProgress

	c := &config.C
	arenaCenter := NewVec2(c.ZoneCenterX, c.ZoneCenterY)
	m.ZoneCenter = arenaCenter.Add(m.ZoneTargetCenter.Sub(arenaCenter).Scale(progress))
	m.ZoneTargetRadius = m.MinRadius
}

// IsInZone returns true when pos lies inside (or on the edge of) the current
// safe zone circle.
func (m *ArenaMap) IsInZone(pos Vec2) bool {
	return pos.DistanceTo(m.ZoneCenter) <= m.ZoneRadius
}

// DistanceToZoneEdge returns the signed distance from pos to the zone
// boundary. Positive means inside the zone; negative means outside.
func (m *ArenaMap) DistanceToZoneEdge(pos Vec2) float64 {
	return m.ZoneRadius - pos.DistanceTo(m.ZoneCenter)
}

// GetSpawnPoints generates evenly-spaced spawn positions around the inside edge
// of the safe zone circle. Bots are placed at ~85% of the zone radius so they
// start near the perimeter but safely inside. If a position collides with an
// obstacle, nearby angles are tried before falling back to the zone centre.
func (m *ArenaMap) GetSpawnPoints(count int) []Vec2 {
	botR := config.C.BotRadius
	spawnRadius := m.ZoneRadius * 0.85
	points := make([]Vec2, 0, count)

	// Random rotation offset so spawns aren't always at the same angles.
	offset := rand.Float64() * 2 * math.Pi

	for i := 0; i < count; i++ {
		baseAngle := offset + (2*math.Pi*float64(i))/float64(count)
		placed := false

		// Try the ideal angle, then nudge up to +/-30 degrees to avoid obstacles.
		for nudge := 0; nudge < 10; nudge++ {
			sign := float64(1)
			if nudge%2 == 1 {
				sign = -1
			}
			angle := baseAngle + sign*float64((nudge+1)/2)*0.1 // ~5.7 degree increments

			x := m.ZoneCenter.X() + spawnRadius*math.Cos(angle)
			y := m.ZoneCenter.Y() + spawnRadius*math.Sin(angle)
			pos := m.ClampToArena(NewVec2(x, y))

			if m.IsInZone(pos) && CollidesWithObstacle(pos.X(), pos.Y(), m.Obstacles, botR) == nil {
				points = append(points, pos)
				placed = true
				break
			}
		}

		if !placed {
			points = append(points, m.ClampToArena(m.ZoneCenter))
		}
	}

	return points
}

// GetSpawnPoint generates a single random spawn position inside the safe zone.
func (m *ArenaMap) GetSpawnPoint() Vec2 {
	botR := config.C.BotRadius

	for i := 0; i < 100; i++ {
		angle := rand.Float64() * 2 * math.Pi
		r := m.ZoneRadius * math.Sqrt(rand.Float64()) * 0.8

		x := m.ZoneCenter.X() + r*math.Cos(angle)
		y := m.ZoneCenter.Y() + r*math.Sin(angle)
		pos := NewVec2(x, y)
		pos = m.ClampToArena(pos)

		if m.IsInZone(pos) && CollidesWithObstacle(pos.X(), pos.Y(), m.Obstacles, botR) == nil {
			return pos
		}
	}

	// Fallback: zone centre, clamped.
	return m.ClampToArena(m.ZoneCenter)
}

// ClampToArena restricts a position so that a bot circle of BotRadius stays
// entirely within the arena rectangle.
func (m *ArenaMap) ClampToArena(pos Vec2) Vec2 {
	botR := config.C.BotRadius
	x := math.Max(botR, math.Min(m.Width-botR, pos.X()))
	y := math.Max(botR, math.Min(m.Height-botR, pos.Y()))
	return NewVec2(x, y)
}
