package game

import (
	"math"
	"testing"

	"arena-server/internal/config"
)

func loadTestConfig(t *testing.T) {
	t.Helper()
	config.Load()
}

func TestInitialZoneRadiusCoversMap(t *testing.T) {
	loadTestConfig(t)
	config.C.ZoneCoverMap = true

	m := NewArenaMap()
	corners := []Vec2{
		NewVec2(0, 0),
		NewVec2(m.Width, 0),
		NewVec2(0, m.Height),
		NewVec2(m.Width, m.Height),
	}
	for _, corner := range corners {
		if !m.IsInZone(corner) {
			t.Errorf("corner %v should start inside the zone (radius %.1f)", corner, m.ZoneRadius)
		}
	}

	want := math.Hypot(m.Width/2, m.Height/2)
	if math.Abs(m.ZoneRadius-want) > 1e-9 {
		t.Errorf("initial radius = %.3f, want circumscribed %.3f", m.ZoneRadius, want)
	}
}

func TestInitialZoneRadiusLegacyConfig(t *testing.T) {
	loadTestConfig(t)
	config.C.ZoneCoverMap = false
	defer func() { config.C.ZoneCoverMap = true }()

	m := NewArenaMap()
	if m.ZoneRadius != config.C.ZoneInitialRadius {
		t.Errorf("with ZoneCoverMap=false radius = %.1f, want configured %.1f", m.ZoneRadius, config.C.ZoneInitialRadius)
	}
}

func TestShapeMasksConnectedAndSized(t *testing.T) {
	loadTestConfig(t)
	const cols, rows = 100, 100

	for _, shape := range []MapShape{ShapeCircle, ShapeHexagon, ShapeDiamond, ShapeCross, ShapeCaves} {
		mask := GenerateShapeMask(shape, cols, rows)
		if mask == nil {
			t.Fatalf("%s: expected a mask", shape)
		}

		frac := playableFraction(mask, cols, rows)
		if frac < 0.25 {
			t.Errorf("%s: playable fraction %.2f too small", shape, frac)
		}

		// Connectivity: count reachable cells from any open cell and compare
		// with the total open count.
		var start [2]int
		found := false
		total := 0
		for x := 0; x < cols && !found; x++ {
			for y := 0; y < rows; y++ {
				if mask[x][y] {
					start = [2]int{x, y}
					found = true
					break
				}
			}
		}
		for x := 0; x < cols; x++ {
			for y := 0; y < rows; y++ {
				if mask[x][y] {
					total++
				}
			}
		}
		visited := make(map[[2]int]bool)
		stack := [][2]int{start}
		visited[start] = true
		for len(stack) > 0 {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				n := [2]int{c[0] + d[0], c[1] + d[1]}
				if n[0] < 0 || n[1] < 0 || n[0] >= cols || n[1] >= rows {
					continue
				}
				if mask[n[0]][n[1]] && !visited[n] {
					visited[n] = true
					stack = append(stack, n)
				}
			}
		}
		if len(visited) != total {
			t.Errorf("%s: playable area not connected: reached %d of %d cells", shape, len(visited), total)
		}
	}
}

func TestMaskToRectsCoverBlockedCells(t *testing.T) {
	const cols, rows = 40, 40
	const cellSize = 20.0
	mask := GenerateShapeMask(ShapeCircle, cols, rows)
	rects := MaskToRects(mask, cols, rows, cellSize)

	for x := 0; x < cols; x++ {
		for y := 0; y < rows; y++ {
			cx := (float64(x) + 0.5) * cellSize
			cy := (float64(y) + 0.5) * cellSize
			inRect := false
			for _, r := range rects {
				if cx >= r.X && cx <= r.X+r.Width && cy >= r.Y && cy <= r.Y+r.Height {
					inRect = true
					break
				}
			}
			if mask[x][y] && inRect {
				t.Fatalf("playable cell (%d,%d) covered by a boundary rect", x, y)
			}
			if !mask[x][y] && !inRect {
				t.Fatalf("blocked cell (%d,%d) not covered by any boundary rect", x, y)
			}
		}
	}
}

func TestPassableNearAvoidsBlockedCells(t *testing.T) {
	loadTestConfig(t)

	prevTerrain := ActiveTerrain
	defer func() { ActiveTerrain = prevTerrain }()

	// 2000x2000 arena, cell size 20 -> 100x100 grid. Block a 10-cell-radius
	// square around the centre so the naive zone-centre fallback would land
	// on a wall.
	terrain := NewTerrainGrid(2000, 2000, nil, 20, 0)
	for x := 40; x <= 60; x++ {
		for y := 40; y <= 60; y++ {
			terrain.Cells[x][y] = '#'
		}
	}
	ActiveTerrain = terrain

	m := NewArenaMap()
	pos := m.PassableNear(m.ZoneCenter)
	cell := terrain.WorldToGrid(pos)
	if terrain.IsBlocked(cell[0], cell[1]) {
		t.Errorf("PassableNear returned a blocked cell %v (pos %v)", cell, pos)
	}

	// Sanity: an already-passable position is returned unchanged.
	open := NewVec2(100, 100)
	if got := m.PassableNear(open); got != open {
		t.Errorf("PassableNear moved an already-passable position: %v -> %v", open, got)
	}
}

func TestSegmentBlockedDiagonalCorners(t *testing.T) {
	loadTestConfig(t)
	// 10x10 grid, cell size 10. Wall at cell (5,5) only.
	g := NewTerrainGrid(100, 100, nil, 10, 0)
	g.Cells[5][5] = '#'

	// A steep diagonal that passes through cell (5,5) but whose
	// index-interpolated march would sample around it.
	a := NewVec2(51, 40) // cell (5,4)
	b := NewVec2(59, 69) // cell (5,6) — passes through (5,5)
	if !g.SegmentBlocked(a, b) {
		t.Error("segment through blocked cell not detected")
	}

	// A clear segment should not be blocked.
	if g.SegmentBlocked(NewVec2(5, 5), NewVec2(95, 5)) {
		t.Error("clear horizontal segment reported blocked")
	}
}

func TestTeamAssignmentAndDamageRules(t *testing.T) {
	loadTestConfig(t)
	bots := map[string]*BotState{
		"a": {BotID: "a", IsAlive: true},
		"b": {BotID: "b", IsAlive: true},
		"c": {BotID: "c", IsAlive: true},
		"d": {BotID: "d", IsAlive: true},
	}
	AssignTeams(bots, 2)
	counts := map[int]int{}
	for _, b := range bots {
		if b.Team < 1 || b.Team > 2 {
			t.Fatalf("bot %s has invalid team %d", b.BotID, b.Team)
		}
		counts[b.Team]++
	}
	if counts[1] != 2 || counts[2] != 2 {
		t.Errorf("teams unbalanced: %v", counts)
	}

	rules := ModeRules{Mode: ModeTeamBattle, TeamCount: 2, FriendlyFire: false}
	var ally, enemy *BotState
	for _, b := range bots {
		if b.BotID == "a" {
			continue
		}
		if b.Team == bots["a"].Team {
			ally = b
		} else {
			enemy = b
		}
	}
	if rules.CanDamage(bots["a"], ally) {
		t.Error("friendly fire should be blocked")
	}
	if !rules.CanDamage(bots["a"], enemy) {
		t.Error("enemy damage should be allowed")
	}

	// FFA never blocks.
	ffa := ModeRules{Mode: ModeFFA}
	if !ffa.CanDamage(bots["a"], ally) {
		t.Error("FFA should not block damage")
	}
}
