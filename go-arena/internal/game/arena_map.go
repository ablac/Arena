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

	// MaskRects are the blocked-boundary rectangles of a non-square map
	// shape, sent to clients alongside obstacles so visible walls match
	// server collision. Empty for square maps.
	MaskRects []Obstacle

	// Cached spectator-facing obstacle rectangles (expanded + grid-snapped).
	// Valid for the terrain grid they were computed against.
	visObstacles  []Obstacle
	visObsTerrain *TerrainGrid
}

// VisualObstacles returns collision-accurate obstacle rectangles for clients:
// expanded by BotRadius padding and snapped to terrain grid cell boundaries so
// visual walls match exactly what blocks movement. Obstacles are static for a
// round, so the result is computed once per (obstacles, terrain) pair.
func (m *ArenaMap) VisualObstacles() []Obstacle {
	if ActiveTerrain == nil {
		return m.Obstacles
	}
	if m.visObstacles != nil && m.visObsTerrain == ActiveTerrain {
		return m.visObstacles
	}

	vis := make([]Obstacle, len(m.Obstacles), len(m.Obstacles)+len(m.MaskRects))
	cs := ActiveTerrain.CellSize
	pad := config.C.BotRadius
	for i, obs := range m.Obstacles {
		ox := obs.X - pad
		oy := obs.Y - pad
		ow := obs.Width + 2*pad
		oh := obs.Height + 2*pad
		// Snap to grid cell boundaries.
		minCX := math.Floor(ox / cs)
		minCY := math.Floor(oy / cs)
		maxCX := math.Floor((ox+ow)/cs) + 1
		maxCY := math.Floor((oy+oh)/cs) + 1
		vis[i] = Obstacle{
			X:      minCX * cs,
			Y:      minCY * cs,
			Width:  (maxCX - minCX) * cs,
			Height: (maxCY - minCY) * cs,
		}
	}
	// Non-square map shapes: append the carved boundary rectangles (already
	// grid-aligned) so clients render the map outline as solid walls.
	vis = append(vis, m.MaskRects...)
	m.visObstacles = vis
	m.visObsTerrain = ActiveTerrain
	return vis
}

// initialZoneRadius returns the starting radius for the safe zone. When
// ZoneCoverMap is enabled the circle circumscribes the whole arena (every
// corner starts inside the zone), so the boundary only becomes visible once
// shrinking pulls it inside the map. Otherwise the configured radius is used.
func initialZoneRadius(c *config.Config, center Vec2, width, height float64) float64 {
	if !c.ZoneCoverMap {
		return c.ZoneInitialRadius
	}
	// Distance from the zone centre to the farthest arena corner.
	dx := math.Max(center.X(), width-center.X())
	dy := math.Max(center.Y(), height-center.Y())
	return math.Hypot(dx, dy)
}

// NewArenaMap creates an ArenaMap initialised from the global config.
func NewArenaMap() *ArenaMap {
	c := &config.C
	center := NewVec2(c.ZoneCenterX, c.ZoneCenterY)
	initial := initialZoneRadius(c, center, c.ArenaWidth, c.ArenaHeight)
	return &ArenaMap{
		Width:            c.ArenaWidth,
		Height:           c.ArenaHeight,
		ZoneCenter:       center,
		ZoneRadius:       initial,
		ZoneTargetCenter: center,
		ZoneTargetRadius: c.ZoneMinRadius,
		InitialRadius:    initial,
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
	m.visObstacles = nil
	m.visObsTerrain = nil
	// Refresh dimensions: dynamic arena sizing can change them per round.
	m.Width = c.ArenaWidth
	m.Height = c.ArenaHeight
	m.ZoneCenter = NewVec2(c.ZoneCenterX, c.ZoneCenterY)
	// Recompute in case arena dimensions or zone config changed at runtime.
	m.InitialRadius = initialZoneRadius(c, m.ZoneCenter, m.Width, m.Height)
	m.ZoneRadius = m.InitialRadius
	m.ShrinkStarted = false
	m.ZoneLerpT = 0.0

	// Random target centre; zone must still fit inside the arena. Prefer a
	// centre on passable terrain so the final circle isn't parked on a wall
	// or outside a non-square map shape.
	margin := m.MinRadius
	for attempt := 0; attempt < 20; attempt++ {
		tx := margin + rand.Float64()*(m.Width-2*margin)
		ty := margin + rand.Float64()*(m.Height-2*margin)
		m.ZoneTargetCenter = NewVec2(tx, ty)
		if !terrainBlockedAt(m.ZoneTargetCenter) {
			break
		}
	}
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
// maxRadiusInsideArena returns the largest circle radius around the zone
// centre that still fits inside the arena rectangle (minus a bot-sized
// margin). Used to keep spawn rings on the map when the zone circumscribes it.
func (m *ArenaMap) maxRadiusInsideArena() float64 {
	botR := config.C.BotRadius
	r := math.Min(
		math.Min(m.ZoneCenter.X(), m.Width-m.ZoneCenter.X()),
		math.Min(m.ZoneCenter.Y(), m.Height-m.ZoneCenter.Y()),
	) - botR*2
	if r < botR {
		r = botR
	}
	return r
}

func (m *ArenaMap) GetSpawnPoints(count int) []Vec2 {
	botR := config.C.BotRadius
	spawnRadius := math.Min(m.ZoneRadius*0.85, m.maxRadiusInsideArena()*0.9)
	points := make([]Vec2, 0, count)

	// Random rotation offset so spawns aren't always at the same angles.
	offset := rand.Float64() * 2 * math.Pi

	for i := 0; i < count; i++ {
		baseAngle := offset + (2*math.Pi*float64(i))/float64(count)
		placed := false

		// Try the ideal angle at full radius, then nudge up to +/-30 degrees;
		// on non-square maps the ring may leave the playable shape, so also
		// retry at smaller radii before giving up.
		for _, radiusScale := range [3]float64{1.0, 0.65, 0.4} {
			r := spawnRadius * radiusScale
			for nudge := 0; nudge < 10; nudge++ {
				sign := float64(1)
				if nudge%2 == 1 {
					sign = -1
				}
				angle := baseAngle + sign*float64((nudge+1)/2)*0.1 // ~5.7 degree increments

				x := m.ZoneCenter.X() + r*math.Cos(angle)
				y := m.ZoneCenter.Y() + r*math.Sin(angle)
				pos := m.ClampToArena(NewVec2(x, y))

				if m.IsInZone(pos) && CollidesWithObstacle(pos.X(), pos.Y(), m.Obstacles, botR) == nil && !terrainBlockedAt(pos) {
					points = append(points, pos)
					placed = true
					break
				}
			}
			if placed {
				break
			}
		}

		if !placed {
			points = append(points, m.PassableNear(m.ZoneCenter))
		}
	}

	return points
}

// GetSpawnPoint generates a single random spawn position inside the safe zone.
func (m *ArenaMap) GetSpawnPoint() Vec2 {
	botR := config.C.BotRadius

	maxR := math.Min(m.ZoneRadius, m.maxRadiusInsideArena())
	for i := 0; i < 100; i++ {
		angle := rand.Float64() * 2 * math.Pi
		r := maxR * math.Sqrt(rand.Float64()) * 0.8

		x := m.ZoneCenter.X() + r*math.Cos(angle)
		y := m.ZoneCenter.Y() + r*math.Sin(angle)
		pos := NewVec2(x, y)
		pos = m.ClampToArena(pos)

		if m.IsInZone(pos) && CollidesWithObstacle(pos.X(), pos.Y(), m.Obstacles, botR) == nil && !terrainBlockedAt(pos) {
			return pos
		}
	}

	// Fallback: nearest passable spot to the zone centre.
	return m.PassableNear(m.ZoneCenter)
}

// PassableNear returns the closest position to pos that is neither inside an
// obstacle nor on a blocked terrain cell (which includes carved-out areas of
// non-square map shapes). It spirals outward over grid cells from pos; if
// nothing is found within the search budget it returns pos clamped to the
// arena, and the engine's per-tick wall enforcement takes over from there.
func (m *ArenaMap) PassableNear(pos Vec2) Vec2 {
	pos = m.ClampToArena(pos)
	botR := config.C.BotRadius

	passable := func(p Vec2) bool {
		return CollidesWithObstacle(p.X(), p.Y(), m.Obstacles, botR) == nil && !terrainBlockedAt(p)
	}
	if passable(pos) {
		return pos
	}
	if ActiveTerrain == nil {
		return pos
	}

	center := ActiveTerrain.WorldToGrid(pos)
	for radius := 1; radius <= 20; radius++ {
		for dx := -radius; dx <= radius; dx++ {
			for dy := -radius; dy <= radius; dy++ {
				// Ring only — interior cells were covered by smaller radii.
				if dx > -radius && dx < radius && dy > -radius && dy < radius {
					continue
				}
				cell := [2]int{center[0] + dx, center[1] + dy}
				if ActiveTerrain.IsBlocked(cell[0], cell[1]) {
					continue
				}
				candidate := m.ClampToArena(ActiveTerrain.GridToWorld(cell))
				if passable(candidate) {
					return candidate
				}
			}
		}
	}
	return pos
}

// ClampToArena restricts a position so that a bot circle of BotRadius stays
// entirely within the arena rectangle.
func (m *ArenaMap) ClampToArena(pos Vec2) Vec2 {
	botR := config.C.BotRadius
	x := math.Max(botR, math.Min(m.Width-botR, pos.X()))
	y := math.Max(botR, math.Min(m.Height-botR, pos.Y()))
	return NewVec2(x, y)
}
