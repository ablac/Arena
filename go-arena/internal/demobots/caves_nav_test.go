package demobots

import (
	"math/rand"
	"testing"

	"arena-server/internal/game"
)

// buildShapeTerrain generates a real map mask and installs it as the demo
// bots' cached terrain, returning it for direct inspection.
func buildShapeTerrain(t *testing.T, shape game.MapShape, cols, rows int) *botTerrain {
	t.Helper()
	mask := game.GenerateShapeMask(shape, cols, rows)
	if mask == nil {
		t.Fatalf("expected a %s mask", shape)
	}
	cells := make([][]byte, cols)
	for x := range cells {
		cells[x] = make([]byte, rows)
		for y := range cells[x] {
			if mask[x][y] {
				cells[x][y] = '.'
			} else {
				cells[x][y] = '#'
			}
		}
	}
	terrain := &botTerrain{Width: cols, Height: rows, CellSize: 20, Cells: cells}
	terrainMu.Lock()
	cachedTerrain = terrain
	terrainMu.Unlock()
	return terrain
}

// shortestPathLen is an unbounded reference BFS returning the true shortest
// path length in steps (8-directional, corner-cutting rules matching
// isMoveBlocked), or -1 if unreachable.
func shortestPathLen(t *botTerrain, sc, sr, gc, gr int) int {
	if sc == gc && sr == gr {
		return 0
	}
	type node struct{ c, r, d int }
	visited := make([]bool, t.Width*t.Height)
	queue := []node{{sc, sr, 0}}
	visited[sc*t.Height+sr] = true
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for dc := -1; dc <= 1; dc++ {
			for dr := -1; dr <= 1; dr++ {
				if dc == 0 && dr == 0 {
					continue
				}
				if t.isMoveBlocked(n.c, n.r, dc, dr) {
					continue
				}
				nc, nr := n.c+dc, n.r+dr
				idx := nc*t.Height + nr
				if visited[idx] {
					continue
				}
				if nc == gc && nr == gr {
					return n.d + 1
				}
				visited[idx] = true
				queue = append(queue, node{nc, nr, n.d + 1})
			}
		}
	}
	return -1
}

func randomOpenCell(rng *rand.Rand, t *botTerrain) [2]int {
	for {
		c, r := rng.Intn(t.Width), rng.Intn(t.Height)
		if !t.isBlocked(c, r) {
			return [2]int{c, r}
		}
	}
}

// TestCavesNavigationWalk simulates a demo bot walking with bfsStep across
// caves maps. For each start/goal pair with a known path, the bot gets a
// generous step allowance (4x the true shortest path + 20); failing to reach
// the goal means the pathfinder wandered or oscillated — exactly the
// "demo bots struggle on caves" behavior.
func TestShapeNavigationWalk(t *testing.T) {
	shapes := []game.MapShape{
		game.ShapeCaves,
		game.ShapeDonut,
		game.ShapeIslands,
		game.ShapeRooms,
		game.ShapeSpiral,
	}
	for _, shape := range shapes {
		t.Run(string(shape), func(t *testing.T) {
			rng := rand.New(rand.NewSource(42))
			const maps = 6
			const pairsPerMap = 30

			attempts, reached := 0, 0
			for m := 0; m < maps; m++ {
				terrain := buildShapeTerrain(t, shape, 100, 100)
				for p := 0; p < pairsPerMap; p++ {
					start := randomOpenCell(rng, terrain)
					goal := randomOpenCell(rng, terrain)
					best := shortestPathLen(terrain, start[0], start[1], goal[0], goal[1])
					if best <= 0 {
						continue // same cell or unreachable — not a navigation test
					}
					attempts++
					budget := best*4 + 20
					cur := start
					ok := false
					for step := 0; step < budget; step++ {
						d := bfsStep(cur[0], cur[1], goal[0], goal[1], nil)
						if d[0] == 0 && d[1] == 0 {
							break // pathfinder gave up
						}
						cur = [2]int{cur[0] + d[0], cur[1] + d[1]}
						if cur == goal {
							ok = true
							break
						}
					}
					if ok {
						reached++
					} else {
						t.Logf("map %d: failed %v -> %v (shortest %d steps)", m, start, goal, best)
					}
				}
			}

			if attempts == 0 {
				t.Fatal("shape generated no reachable navigation pairs")
			}
			rate := float64(reached) / float64(attempts)
			t.Logf("%s navigation success: %d/%d (%.1f%%)", shape, reached, attempts, rate*100)
			if rate < 0.95 {
				t.Errorf("caves navigation success rate %.1f%% — demo bots cannot reliably cross caves maps", rate*100)
			}
		})
	}
}

// TestTerrainBlockedGroundCells: ground ('.') cells must not be reported as
// blocked by terrainBlocked (used by nearImpactSurface for grapple slams).
func TestTerrainBlockedGroundCells(t *testing.T) {
	cells := make([][]byte, 3)
	for x := range cells {
		cells[x] = make([]byte, 3)
		for y := range cells[x] {
			cells[x][y] = '.'
		}
	}
	cells[2][2] = '#'
	terrainMu.Lock()
	cachedTerrain = &botTerrain{Width: 3, Height: 3, CellSize: 20, Cells: cells}
	terrainMu.Unlock()

	if terrainBlocked(1, 1) {
		t.Error("terrainBlocked reported an open ground cell as blocked")
	}
	if !terrainBlocked(2, 2) {
		t.Error("terrainBlocked failed to report a wall cell as blocked")
	}
	if !terrainBlocked(-1, 0) {
		t.Error("terrainBlocked failed to report out-of-bounds as blocked")
	}
}
