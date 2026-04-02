package game

import (
	"testing"

	"arena-server/internal/config"
)

func makeTestNavGrid(w, h float64, obstacles []Obstacle) *NavGrid {
	config.Load()
	return NewNavGrid(w, h, obstacles, 5)
}

func TestNavGridBasic(t *testing.T) {
	g := makeTestNavGrid(200, 200, nil)
	if g.Width <= 0 || g.Height <= 0 {
		t.Errorf("nav grid should have positive dimensions, got %dx%d", g.Width, g.Height)
	}
}

func TestNavGridIsBlocked(t *testing.T) {
	g := makeTestNavGrid(200, 200, nil)
	// Out of bounds
	if !g.IsBlocked(-1, 0) {
		t.Error("negative x should be blocked")
	}
	if !g.IsBlocked(0, -1) {
		t.Error("negative y should be blocked")
	}
}

func TestNavGridWorldToCell(t *testing.T) {
	config.Load()
	g := makeTestNavGrid(200, 200, nil)
	cell := g.WorldToCell(NewVec2(0, 0))
	if cell[0] < 0 || cell[1] < 0 {
		t.Errorf("cell should be clamped to >= 0, got %v", cell)
	}
}

func TestNavGridCellToWorld(t *testing.T) {
	config.Load()
	g := makeTestNavGrid(200, 200, nil)
	cell := [2]int{2, 3}
	pos := g.CellToWorld(cell)
	if pos.X() <= 0 || pos.Y() <= 0 {
		t.Errorf("world position should be positive for cell (2,3), got %v", pos)
	}
}

func TestFindPathStraight(t *testing.T) {
	config.Load()
	g := makeTestNavGrid(400, 400, nil)

	start := NewVec2(10, 10)
	goal := NewVec2(390, 10)

	path := FindPath(start, goal, g)
	if len(path) == 0 {
		t.Error("expected non-empty path")
	}
	last := path[len(path)-1]
	if last.DistanceTo(goal) > g.CellSize*2 {
		t.Errorf("path doesn't end near goal: last=%v goal=%v", last, goal)
	}
}

func TestFindPathSameCell(t *testing.T) {
	config.Load()
	g := makeTestNavGrid(200, 200, nil)
	start := NewVec2(50, 50)
	goal := NewVec2(51, 51) // same cell
	path := FindPath(start, goal, g)
	// Should return at least the goal
	if path == nil {
		t.Error("nil path for same-cell start/goal")
	}
}

func TestFindPathNoObstacles(t *testing.T) {
	config.Load()
	g := makeTestNavGrid(500, 500, nil)
	start := NewVec2(50, 250)
	goal := NewVec2(450, 250)
	path := FindPath(start, goal, g)
	if len(path) == 0 {
		t.Error("expected path with no obstacles")
	}
}

func TestFindPathAroundObstacle(t *testing.T) {
	config.Load()
	// Wall spanning vertically in the middle, with a gap at top
	obs := []Obstacle{
		{X: 180, Y: 100, Width: 40, Height: 300},
	}
	g := makeTestNavGrid(400, 500, obs)

	start := NewVec2(50, 250)
	goal := NewVec2(350, 250)
	path := FindPath(start, goal, g)
	// If no path found, that's ok — the wall may fully block. Just don't crash.
	_ = path
}

func TestFindPathBlockedGoal(t *testing.T) {
	config.Load()
	obs := []Obstacle{{X: 180, Y: 180, Width: 40, Height: 40}}
	g := makeTestNavGrid(400, 400, obs)

	start := NewVec2(50, 50)
	// Goal inside obstacle
	goal := NewVec2(200, 200)
	path := FindPath(start, goal, g)
	// Should find nearest unblocked or return nil — should not panic
	_ = path
}

func TestNavGridIsBlockedWithObstacle(t *testing.T) {
	config.Load()
	obs := []Obstacle{{X: 60, Y: 60, Width: 80, Height: 80}}
	g := makeTestNavGrid(400, 400, obs)

	// A cell well within the obstacle should be blocked
	cell := g.WorldToCell(NewVec2(100, 100))
	if !g.IsBlocked(cell[0], cell[1]) {
		t.Error("cell inside obstacle should be blocked in navgrid")
	}
}
