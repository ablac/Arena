package game

import (
	"testing"
)

func makeTestTerrain(w, h int, cellSize float64) *TerrainGrid {
	return NewTerrainGrid(
		float64(w)*cellSize, float64(h)*cellSize,
		[]Obstacle{},
		cellSize,
		0,
	)
}

func TestTerrainGridBasic(t *testing.T) {
	tg := makeTestTerrain(10, 10, 20)
	if tg.Width != 10 || tg.Height != 10 {
		t.Errorf("expected 10x10, got %dx%d", tg.Width, tg.Height)
	}
	if tg.IsBlocked(0, 0) {
		t.Error("cell (0,0) should not be blocked (no obstacles)")
	}
}

func TestTerrainGridOutOfBounds(t *testing.T) {
	tg := makeTestTerrain(5, 5, 20)
	if !tg.IsBlocked(-1, 0) {
		t.Error("negative x should be blocked")
	}
	if !tg.IsBlocked(0, -1) {
		t.Error("negative y should be blocked")
	}
	if !tg.IsBlocked(100, 0) {
		t.Error("x beyond width should be blocked")
	}
	if !tg.IsBlocked(0, 100) {
		t.Error("y beyond height should be blocked")
	}
}

func TestTerrainGridWithObstacle(t *testing.T) {
	obs := []Obstacle{{X: 20, Y: 20, Width: 20, Height: 20}}
	tg := NewTerrainGrid(200, 200, obs, 20, 0)

	// Cell at (1,1) world (20-40, 20-40) should be blocked
	if !tg.IsBlocked(1, 1) {
		t.Error("cell (1,1) should be blocked by obstacle")
	}
}

func TestTerrainGridWorldToGrid(t *testing.T) {
	tg := makeTestTerrain(10, 10, 20)
	cell := tg.WorldToGrid(NewVec2(25, 35))
	if cell[0] != 1 || cell[1] != 1 {
		t.Errorf("expected cell (1,1), got %v", cell)
	}
}

func TestTerrainGridGridToWorld(t *testing.T) {
	tg := makeTestTerrain(10, 10, 20)
	pos := tg.GridToWorld([2]int{1, 1})
	// Center of cell (1,1) = (30, 30)
	if pos.X() != 30 || pos.Y() != 30 {
		t.Errorf("expected (30,30), got (%v,%v)", pos.X(), pos.Y())
	}
}

func TestTerrainGridIsMoveBlocked(t *testing.T) {
	tg := makeTestTerrain(5, 5, 20)
	// Move from (0,0) right to (1,0) — should not be blocked
	if tg.IsMoveBlocked(0, 0, 1, 0) {
		t.Error("move to open cell should not be blocked")
	}
	// Move from (0,0) to out-of-bounds — should be blocked
	if !tg.IsMoveBlocked(0, 0, -1, 0) {
		t.Error("move out of bounds should be blocked")
	}
}

func TestTerrainGridGridDistance(t *testing.T) {
	tests := []struct {
		a, b [2]int
		want int
	}{
		{[2]int{0, 0}, [2]int{3, 0}, 3},  // horizontal
		{[2]int{0, 0}, [2]int{0, 4}, 4},  // vertical
		{[2]int{0, 0}, [2]int{3, 4}, 4},  // diagonal (Chebyshev: max(3,4)=4)
		{[2]int{2, 2}, [2]int{2, 2}, 0},  // same cell
	}
	for _, tc := range tests {
		got := GridDistance(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("GridDistance(%v,%v)=%v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestTerrainGridSnapDirection(t *testing.T) {
	tests := []struct {
		v    float64
		want int
	}{
		{0.5, 1},
		{-0.5, -1},
		{0.0, 0},
		{0.3, 0}, // exactly at boundary
		{0.31, 1},
		{-0.31, -1},
	}
	for _, tc := range tests {
		got := SnapDirection(tc.v)
		if got != tc.want {
			t.Errorf("SnapDirection(%v)=%v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestTerrainGridLineBlocked(t *testing.T) {
	// Build a 10x10 terrain, put a wall in the middle column
	tg := makeTestTerrain(10, 10, 20)
	// manually block column 5
	for y := 0; y < 10; y++ {
		tg.Cells[5][y] = '#'
	}

	a := NewVec2(50, 50)  // cell (2,2)
	b := NewVec2(150, 50) // cell (7,2) — crosses column 5

	if !tg.GridLineBlocked(a, b) {
		t.Error("line should be blocked by wall at column 5")
	}

	// Line entirely on one side — not blocked
	c := NewVec2(10, 10)
	d := NewVec2(90, 90)
	if tg.GridLineBlocked(c, d) {
		t.Error("line should not be blocked (no wall in path)")
	}
}

func TestTerrainGridToJSON(t *testing.T) {
	tg := makeTestTerrain(3, 3, 20)
	j := tg.ToJSON()
	if len(j) != 3 {
		t.Errorf("expected 3 rows, got %d", len(j))
	}
	if len(j[0]) != 3 {
		t.Errorf("expected 3 cols, got %d", len(j[0]))
	}
}

func TestTerrainGridToCompactJSON(t *testing.T) {
	tg := makeTestTerrain(4, 4, 20)
	j := tg.ToCompactJSON()
	if len(j) != 4 {
		t.Errorf("expected 4 rows, got %d", len(j))
	}
	if len(j[0]) != 4 {
		t.Errorf("expected 4 chars per row, got %d", len(j[0]))
	}
}
