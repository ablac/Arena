package game

import (
	"testing"
)

func TestCollidesWithObstacleHit(t *testing.T) {
	obs := []Obstacle{{X: 100, Y: 100, Width: 50, Height: 50}}
	// Point inside
	if CollidesWithObstacle(125, 125, obs, 0) == nil {
		t.Error("expected collision at center of obstacle")
	}
}

func TestCollidesWithObstacleMiss(t *testing.T) {
	obs := []Obstacle{{X: 100, Y: 100, Width: 50, Height: 50}}
	// Point far away
	if CollidesWithObstacle(300, 300, obs, 0) != nil {
		t.Error("unexpected collision far from obstacle")
	}
}

func TestCollidesWithObstacleRadius(t *testing.T) {
	obs := []Obstacle{{X: 100, Y: 100, Width: 50, Height: 50}}
	// Point just outside but within radius
	if CollidesWithObstacle(90, 125, obs, 15) == nil {
		t.Error("expected collision due to radius")
	}
}

func TestCollidesWithObstacleEmpty(t *testing.T) {
	if CollidesWithObstacle(500, 500, []Obstacle{}, 5) != nil {
		t.Error("no collision expected with empty obstacles")
	}
}

func TestLineIntersectsObstacleHit(t *testing.T) {
	obs := []Obstacle{{X: 100, Y: 100, Width: 50, Height: 50}}
	// Line from left of obstacle to right — should intersect
	if !LineIntersectsObstacle(50, 125, 200, 125, obs) {
		t.Error("expected line to intersect obstacle")
	}
}

func TestLineIntersectsObstacleMiss(t *testing.T) {
	obs := []Obstacle{{X: 100, Y: 100, Width: 50, Height: 50}}
	// Line that passes above the obstacle
	if LineIntersectsObstacle(50, 50, 200, 50, obs) {
		t.Error("line should not intersect obstacle")
	}
}

func TestLineIntersectsObstacleEndpointInside(t *testing.T) {
	obs := []Obstacle{{X: 100, Y: 100, Width: 50, Height: 50}}
	// One endpoint inside obstacle
	if !LineIntersectsObstacle(125, 125, 200, 200, obs) {
		t.Error("line starting inside obstacle should intersect")
	}
}

func TestLineIntersectsObstacleEmpty(t *testing.T) {
	if LineIntersectsObstacle(0, 0, 100, 100, []Obstacle{}) {
		t.Error("no intersection with empty obstacles")
	}
}

func TestGenerateObstacles(t *testing.T) {
	obs := GenerateObstacles(2000, 2000, 5, 10)
	if len(obs) < 5 || len(obs) > 10 {
		t.Errorf("expected 5-10 obstacles, got %d", len(obs))
	}
	for _, o := range obs {
		if o.Width <= 0 || o.Height <= 0 {
			t.Errorf("obstacle has non-positive dimensions: %+v", o)
		}
		if o.X < 0 || o.Y < 0 {
			t.Errorf("obstacle has negative coordinates: %+v", o)
		}
	}
}

func TestSlideAlongObstacleNoColl(t *testing.T) {
	obs := []Obstacle{{X: 200, Y: 200, Width: 50, Height: 50}}
	// Movement with no obstacle nearby
	nx, ny := SlideAlongObstacle(10, 10, 20, 20, obs, 5)
	if nx != 20 || ny != 20 {
		t.Errorf("expected (20,20) got (%v,%v)", nx, ny)
	}
}

func TestEnforceObstacleBoundsNoop(t *testing.T) {
	obs := []Obstacle{{X: 200, Y: 200, Width: 50, Height: 50}}
	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10)
	EnforceObstacleBounds(bot, obs, 5)
	if bot.Position.X() != 10 || bot.Position.Y() != 10 {
		t.Errorf("position should not change, got %v", bot.Position)
	}
}

func TestEnforceObstacleBoundsPush(t *testing.T) {
	obs := []Obstacle{{X: 100, Y: 100, Width: 50, Height: 50}}
	bot := newTestBot("b", 100)
	bot.Position = NewVec2(125, 125) // inside obstacle center
	EnforceObstacleBounds(bot, obs, 5)
	// Bot should be pushed outside expanded AABB
	// expanded: X=95..155, Y=95..155 (with radius=5)
	bx := bot.Position.X()
	by := bot.Position.Y()
	// Should no longer collide
	if CollidesWithObstacle(bx, by, obs, 5) != nil {
		t.Errorf("bot still inside obstacle after enforce at (%v,%v)", bx, by)
	}
}
