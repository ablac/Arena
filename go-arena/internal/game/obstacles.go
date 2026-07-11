package game

import (
	"math"
	"math/rand"
)

// GenerateObstacles creates a random set of rectangular obstacles for a round.
// Count is chosen randomly between minCount and maxCount. Each obstacle is
// placed with a 50-unit margin from the arena edges.
func GenerateObstacles(arenaW, arenaH float64, minCount, maxCount int) []Obstacle {
	return GenerateObstaclesInMask(arenaW, arenaH, minCount, maxCount, nil, 0, 0)
}

// GenerateObstaclesInMask places random obstacles that never overlap the
// blocked region of a map-shape mask (mask[col][row], true = playable) or
// each other. Obstacles used to be generated before the shape mask existed,
// so on non-square maps they routinely embedded inside carved walls — the
// client then drew obstacle boxes intersecting the wall geometry (z-fighting
// and blocks protruding through the map outline). Each obstacle gets a
// bounded number of placement attempts; on crowded shapes the round simply
// gets fewer obstacles rather than a glitched one. A nil mask preserves the
// classic square-map behavior.
func GenerateObstaclesInMask(arenaW, arenaH float64, minCount, maxCount int, mask [][]bool, cellSize, botRadius float64) []Obstacle {
	if maxCount < minCount {
		maxCount = minCount
	}
	count := minCount + rand.Intn(maxCount-minCount+1)
	obstacles := make([]Obstacle, 0, count)
	margin := 50.0

	for i := 0; i < count; i++ {
		for attempt := 0; attempt < 40; attempt++ {
			ow := 10.0 + rand.Float64()*50.0 // 10..60
			oh := 10.0 + rand.Float64()*50.0 // 10..60
			ox := margin + rand.Float64()*(arenaW-margin-margin-ow)
			oy := margin + rand.Float64()*(arenaH-margin-margin-oh)
			candidate := Obstacle{X: ox, Y: oy, Width: ow, Height: oh}

			if mask != nil && !obstacleFitsMask(candidate, mask, cellSize, botRadius) {
				continue
			}
			if obstacleOverlapsAny(candidate, obstacles, botRadius) {
				continue
			}
			obstacles = append(obstacles, candidate)
			break
		}
	}
	return obstacles
}

// obstacleFitsMask reports whether every terrain cell covered by the obstacle
// (expanded by bot-radius padding, the same footprint NewTerrainGrid stamps
// as walls) is playable in the shape mask, with one extra cell of clearance
// so an obstacle can never pinch a one-cell corridor shut against a wall.
func obstacleFitsMask(obs Obstacle, mask [][]bool, cellSize, botRadius float64) bool {
	if cellSize <= 0 {
		return true
	}
	cols := len(mask)
	if cols == 0 {
		return true
	}
	rows := len(mask[0])

	pad := botRadius + cellSize // padding footprint + 1 cell clearance
	minCX := int((obs.X - pad) / cellSize)
	minCY := int((obs.Y - pad) / cellSize)
	maxCX := int((obs.X + obs.Width + pad) / cellSize)
	maxCY := int((obs.Y + obs.Height + pad) / cellSize)

	for cx := minCX; cx <= maxCX; cx++ {
		for cy := minCY; cy <= maxCY; cy++ {
			if cx < 0 || cy < 0 || cx >= cols || cy >= rows {
				return false
			}
			if !mask[cx][cy] {
				return false
			}
		}
	}
	return true
}

// obstacleOverlapsAny reports whether the candidate's padded AABB intersects
// any already-placed obstacle's padded AABB. Overlapping obstacles rendered
// as coplanar boxes z-fight, and merged silhouettes read as glitches.
func obstacleOverlapsAny(candidate Obstacle, placed []Obstacle, botRadius float64) bool {
	pad := botRadius
	for i := range placed {
		o := &placed[i]
		if candidate.X-pad < o.X+o.Width+pad && candidate.X+candidate.Width+pad > o.X-pad &&
			candidate.Y-pad < o.Y+o.Height+pad && candidate.Y+candidate.Height+pad > o.Y-pad {
			return true
		}
	}
	return false
}

// CollidesWithObstacle checks whether a circle (centre x,y with the given
// radius) overlaps any obstacle. The obstacle AABB is expanded by radius on
// each side so a simple point-in-rect test suffices. Returns the first
// colliding obstacle, or nil.
func CollidesWithObstacle(x, y float64, obstacles []Obstacle, radius float64) *Obstacle {
	for i := range obstacles {
		obs := &obstacles[i]
		if x >= obs.X-radius && x <= obs.X+obs.Width+radius &&
			y >= obs.Y-radius && y <= obs.Y+obs.Height+radius {
			return obs
		}
	}
	return nil
}

// LineIntersectsObstacle returns true if the line segment from (x1,y1) to
// (x2,y2) intersects any obstacle rectangle.
func LineIntersectsObstacle(x1, y1, x2, y2 float64, obstacles []Obstacle) bool {
	for i := range obstacles {
		obs := &obstacles[i]

		// Check if either endpoint is inside the obstacle.
		if pointInRect(x1, y1, obs) || pointInRect(x2, y2, obs) {
			return true
		}

		// Check segment intersection with each of the four edges.
		if lineRectIntersect(x1, y1, x2, y2, obs) {
			return true
		}
	}
	return false
}

// CombatLineBlocked returns true if a combat ray between two world-space
// positions is blocked by the active terrain grid or obstacle geometry.
func CombatLineBlocked(from, to Vec2, obstacles []Obstacle) bool {
	if ActiveTerrain != nil && ActiveTerrain.GridLineBlocked(from, to) {
		return true
	}
	return LineIntersectsObstacle(from.X(), from.Y(), to.X(), to.Y(), obstacles)
}

// SlideAlongObstacle attempts to move from (oldX,oldY) to (newX,newY),
// using stepped collision to prevent tunnelling through thin obstacles.
//
//	1. Step along the path checking for collisions.
//	2. If blocked, try X-only then Y-only sliding.
//	3. Return the farthest valid position.
func SlideAlongObstacle(oldX, oldY, newX, newY float64, obstacles []Obstacle, radius float64) (float64, float64) {
	dx := newX - oldX
	dy := newY - oldY
	dist := math.Sqrt(dx*dx + dy*dy)

	// If movement is small enough, single check is fine
	stepSize := radius * 0.8 // step in increments smaller than bot radius
	if dist <= stepSize {
		if CollidesWithObstacle(newX, newY, obstacles, radius) == nil {
			return newX, newY
		}
		if CollidesWithObstacle(newX, oldY, obstacles, radius) == nil {
			return newX, oldY
		}
		if CollidesWithObstacle(oldX, newY, obstacles, radius) == nil {
			return oldX, newY
		}
		return oldX, oldY
	}

	// Step along the path
	steps := int(math.Ceil(dist / stepSize))
	if steps > 20 {
		steps = 20 // cap iterations
	}

	lastGoodX, lastGoodY := oldX, oldY
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		cx := oldX + dx*t
		cy := oldY + dy*t
		if CollidesWithObstacle(cx, cy, obstacles, radius) != nil {
			// Hit something — try sliding at this step
			if CollidesWithObstacle(cx, lastGoodY, obstacles, radius) == nil {
				lastGoodX = cx
			} else if CollidesWithObstacle(lastGoodX, cy, obstacles, radius) == nil {
				lastGoodY = cy
			}
			return lastGoodX, lastGoodY
		}
		lastGoodX, lastGoodY = cx, cy
	}
	return lastGoodX, lastGoodY
}

// EnforceObstacleBounds pushes a bot out of any obstacle it overlaps.
// This is a safety net called after all movement to prevent bots from
// ever being inside an obstacle.
func EnforceObstacleBounds(bot *BotState, obstacles []Obstacle, radius float64) {
	for _, obs := range obstacles {
		// Expanded AABB
		left := obs.X - radius
		right := obs.X + obs.Width + radius
		top := obs.Y - radius
		bottom := obs.Y + obs.Height + radius

		bx := bot.Position.X()
		by := bot.Position.Y()

		if bx >= left && bx <= right && by >= top && by <= bottom {
			// Bot is inside expanded obstacle — push to nearest edge
			pushLeft := bx - left
			pushRight := right - bx
			pushTop := by - top
			pushBottom := bottom - by

			minPush := pushLeft
			pushX, pushY := left-0.1, by
			if pushRight < minPush {
				minPush = pushRight
				pushX, pushY = right+0.1, by
			}
			if pushTop < minPush {
				minPush = pushTop
				pushX, pushY = bx, top-0.1
			}
			if pushBottom < minPush {
				pushX, pushY = bx, bottom+0.1
			}

			// Never push the bot INTO blocked terrain (this legacy path only
			// runs without a terrain grid, but guard anyway: a push target
			// inside a wall would start a revert/push jitter loop).
			if terrainBlockedAt(NewVec2(pushX, pushY)) {
				continue
			}
			bot.Position = NewVec2(pushX, pushY)
		}
	}
}

// pointInRect returns true if (x,y) lies inside the obstacle rectangle
// (inclusive bounds).
func pointInRect(x, y float64, obs *Obstacle) bool {
	return x >= obs.X && x <= obs.X+obs.Width &&
		y >= obs.Y && y <= obs.Y+obs.Height
}

// lineRectIntersect checks if the line segment from (x1,y1) to (x2,y2)
// crosses any edge of the obstacle rectangle using the cross-product
// segment intersection test.
func lineRectIntersect(x1, y1, x2, y2 float64, obs *Obstacle) bool {
	// Four edges of the rectangle.
	edges := [4][4]float64{
		{obs.X, obs.Y, obs.X + obs.Width, obs.Y},                           // top
		{obs.X, obs.Y + obs.Height, obs.X + obs.Width, obs.Y + obs.Height}, // bottom
		{obs.X, obs.Y, obs.X, obs.Y + obs.Height},                          // left
		{obs.X + obs.Width, obs.Y, obs.X + obs.Width, obs.Y + obs.Height},  // right
	}

	for _, e := range edges {
		if segmentsIntersect(x1, y1, x2, y2, e[0], e[1], e[2], e[3]) {
			return true
		}
	}
	return false
}

// segmentsIntersect returns true if line segment (ax1,ay1)-(ax2,ay2) properly
// crosses segment (bx1,by1)-(bx2,by2) using the cross-product orientation
// test.
func segmentsIntersect(ax1, ay1, ax2, ay2, bx1, by1, bx2, by2 float64) bool {
	cross := func(ox, oy, px, py, qx, qy float64) float64 {
		return (px-ox)*(qy-oy) - (py-oy)*(qx-ox)
	}

	d1 := cross(bx1, by1, bx2, by2, ax1, ay1)
	d2 := cross(bx1, by1, bx2, by2, ax2, ay2)
	d3 := cross(ax1, ay1, ax2, ay2, bx1, by1)
	d4 := cross(ax1, ay1, ax2, ay2, bx2, by2)

	if ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) &&
		((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0)) {
		return true
	}
	return false
}
