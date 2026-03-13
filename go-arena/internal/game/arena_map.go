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

	// Random target centre — zone must still fit inside the arena.
	margin := m.MinRadius
	tx := margin + rand.Float64()*(m.Width-2*margin)
	ty := margin + rand.Float64()*(m.Height-2*margin)
	m.ZoneTargetCenter = NewVec2(tx, ty)
	m.ZoneTargetRadius = m.MinRadius
}

// UpdateZone shrinks the safe zone based on how many ticks have elapsed since
// the round started.
//
// Before the shrink delay expires the zone stays at its initial size. After
// that it linearly interpolates from InitialRadius to MinRadius over the
// remaining round duration, and the centre drifts toward ZoneTargetCenter.
func (m *ArenaMap) UpdateZone(tickCount int, roundStartTick int) {
	c := &config.C

	elapsed := tickCount - roundStartTick

	// Still in the delay window — no shrinking yet.
	if elapsed < m.ShrinkDelayTicks {
		return
	}

	m.ShrinkStarted = true

	roundTotalTicks := int(c.RoundDuration * float64(c.TickRate))
	shrinkTicks := roundTotalTicks - m.ShrinkDelayTicks
	if shrinkTicks <= 0 {
		return
	}

	shrinkElapsed := elapsed - m.ShrinkDelayTicks
	t := float64(shrinkElapsed) / float64(shrinkTicks)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	m.ZoneLerpT = t

	// Interpolate radius: initial -> min.
	m.ZoneRadius = m.InitialRadius + (m.MinRadius-m.InitialRadius)*t

	// Drift centre from arena mid-point toward the random target.
	arenaCenter := NewVec2(c.ZoneCenterX, c.ZoneCenterY)
	m.ZoneCenter = arenaCenter.Add(m.ZoneTargetCenter.Sub(arenaCenter).Scale(t))

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

// GetSpawnPoint generates a random spawn position inside the current safe zone
// that does not collide with any obstacle. After 100 failed attempts the zone
// centre is used as a fallback. The result is always clamped to the arena.
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
