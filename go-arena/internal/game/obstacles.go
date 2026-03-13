package game

import "math/rand"

// GenerateObstacles creates a random set of rectangular obstacles for a round.
// Count is chosen randomly between minCount and maxCount. Each obstacle is
// placed with a 50-unit margin from the arena edges.
func GenerateObstacles(arenaW, arenaH float64, minCount, maxCount int) []Obstacle {
	count := minCount + rand.Intn(maxCount-minCount+1)
	obstacles := make([]Obstacle, 0, count)
	margin := 50.0

	for i := 0; i < count; i++ {
		ow := 10.0 + rand.Float64()*50.0 // 10..60
		oh := 10.0 + rand.Float64()*50.0 // 10..60
		ox := margin + rand.Float64()*(arenaW-margin-margin-ow)
		oy := margin + rand.Float64()*(arenaH-margin-margin-oh)

		obstacles = append(obstacles, Obstacle{
			X:      ox,
			Y:      oy,
			Width:  ow,
			Height: oh,
		})
	}
	return obstacles
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

// SlideAlongObstacle attempts to move from (oldX,oldY) to (newX,newY),
// sliding along obstacles if the direct path is blocked.
//
//	1. Try the full move.
//	2. Try X-only movement.
//	3. Try Y-only movement.
//	4. Stay in place.
func SlideAlongObstacle(oldX, oldY, newX, newY float64, obstacles []Obstacle, radius float64) (float64, float64) {
	// Full move
	if CollidesWithObstacle(newX, newY, obstacles, radius) == nil {
		return newX, newY
	}
	// X only
	if CollidesWithObstacle(newX, oldY, obstacles, radius) == nil {
		return newX, oldY
	}
	// Y only
	if CollidesWithObstacle(oldX, newY, obstacles, radius) == nil {
		return oldX, newY
	}
	// Blocked
	return oldX, oldY
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
		{obs.X, obs.Y, obs.X + obs.Width, obs.Y},                                 // top
		{obs.X, obs.Y + obs.Height, obs.X + obs.Width, obs.Y + obs.Height},       // bottom
		{obs.X, obs.Y, obs.X, obs.Y + obs.Height},                                 // left
		{obs.X + obs.Width, obs.Y, obs.X + obs.Width, obs.Y + obs.Height},         // right
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
