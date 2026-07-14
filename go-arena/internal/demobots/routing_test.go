package demobots

import (
	"math"
	"math/rand"
	"testing"

	"arena-server/internal/game"
)

func routeTick(pos [2]float64) map[string]interface{} {
	return map[string]interface{}{
		"type": "tick",
		"tick": float64(100),
		"your_state": map[string]interface{}{
			"position": []interface{}{pos[0], pos[1]},
			"hp":       float64(100),
			"max_hp":   float64(100),
			"speed":    float64(5),
			"is_alive": true,
		},
	}
}

// moveIntent mirrors what the tactics layer emits for travel: a bfs-guided
// single step that remembers its destination for the router.
func moveIntent(pos, dst [2]float64) actionResult {
	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	danger.reset()
	return moveTo(pos, dst, danger)
}

// buildSeededShapeTerrain is buildShapeTerrain with a deterministic mask so
// routed-travel results cannot depend on test execution order.
func buildSeededShapeTerrain(t *testing.T, shape game.MapShape, cols, rows int, seed int64) *botTerrain {
	t.Helper()
	mask := game.GenerateShapeMaskWithSeed(shape, cols, rows, seed)
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

func openTerrain(t *testing.T, size int) {
	t.Helper()
	cells := make([][]byte, size)
	for x := range cells {
		cells[x] = make([]byte, size)
		for y := range cells[x] {
			cells[x][y] = '.'
		}
	}
	terrainMu.Lock()
	cachedTerrain = &botTerrain{Width: size, Height: size, CellSize: 20, Cells: cells}
	terrainMu.Unlock()
	t.Cleanup(clearTerrain)
}

// A long-range move on a clean route must delegate wholly to the server's
// authoritative move_to pathfinding, preserving the original destination.
func TestRouteDelegatesCleanRoutesToServer(t *testing.T) {
	openTerrain(t, 30)
	router := &movementRouter{}
	pos := [2]float64{2, 2}
	dst := [2]float64{25, 25}

	got := router.route(routeTick(pos), moveIntent(pos, dst), "route-bot")
	if got.Action != "move_to" {
		t.Fatalf("expected move_to delegation, got %q", got.Action)
	}
	if got.TargetPosition == nil || *got.TargetPosition != dst {
		t.Fatalf("expected target %v, got %v", dst, got.TargetPosition)
	}
}

// Close-quarters movement keeps the legacy single-step behavior so combat
// spacing stays reactive.
func TestRouteKeepsShortHopsAsSteps(t *testing.T) {
	openTerrain(t, 30)
	router := &movementRouter{}
	pos := [2]float64{5, 5}
	dst := [2]float64{6, 6}

	got := router.route(routeTick(pos), moveIntent(pos, dst), "route-bot")
	if got.Action != "move" {
		t.Fatalf("expected legacy move step for adjacent goal, got %q", got.Action)
	}
}

// Non-movement actions and direction-only strafes pass through untouched.
func TestRoutePassesThroughNonDestinationActions(t *testing.T) {
	openTerrain(t, 30)
	router := &movementRouter{}
	attack := actionResult{Action: "attack", Target: "enemy"}
	if got := router.route(routeTick([2]float64{5, 5}), attack, "route-bot"); got.Action != "attack" {
		t.Fatalf("attack was rewritten to %q", got.Action)
	}
	direction := [2]float64{1, 0}
	strafe := actionResult{Action: "move", Direction: &direction}
	if got := router.route(routeTick([2]float64{5, 5}), strafe, "route-bot"); got.TargetPosition != nil {
		t.Fatalf("direction-only strafe gained a target position: %v", got.TargetPosition)
	}
}

// When lethal danger sits on the shortest route, the emitted move_to segment
// must be straight-line clear of walls AND danger, so the server's pacing
// cannot drag the bot through the hazard.
func TestRouteSkirtsDangerWithClearSegments(t *testing.T) {
	openTerrain(t, 30)
	router := &movementRouter{}
	pos := [2]float64{2, 15}
	dst := [2]float64{27, 15}

	// A vertical hazard wall across the direct route, with a gap at row 3.
	msg := routeTick(pos)
	hazards := make([]interface{}, 0, 20)
	for row := 6; row <= 28; row++ {
		hazards = append(hazards, map[string]interface{}{
			"id": "hz", "type": "hazard_zone", "is_active": true,
			"position": []interface{}{float64(15), float64(row)},
			"width":    float64(1), "height": float64(1),
		})
	}
	msg["hazard_zones"] = hazards

	got := router.route(msg, moveIntent(pos, dst), "route-bot")
	if got.Action != "move_to" || got.TargetPosition == nil {
		t.Fatalf("expected a move_to segment, got %+v", got)
	}
	ts := parseTick(msg)
	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	buildDangerSet(danger, &ts, "route-bot")
	start := [2]int{int(pos[0]), int(pos[1])}
	segment := [2]int{int(math.Round(got.TargetPosition[0])), int(math.Round(got.TargetPosition[1]))}
	if danger.has(segment[0], segment[1]) {
		t.Fatalf("segment target %v is inside danger", segment)
	}
	if lineCrossesDanger(start, segment, danger) {
		t.Fatalf("segment %v -> %v crosses danger", start, segment)
	}
}

// simulateServerMoveTo mirrors the server's move_to execution closely enough
// for navigation testing: an authoritative wall-aware path toward the target,
// followed a bounded number of cells per tick.
func simulateServerMoveTo(pos [2]int, target [2]int, cellsPerTick int) [2]int {
	path := planGridPath(pos, target, nil)
	if path == nil {
		// The real server would idle; approach as far as the closest
		// reachable cell the same way processMoveTo stalls.
		return pos
	}
	steps := cellsPerTick
	if steps > len(path) {
		steps = len(path)
	}
	if steps == 0 {
		return pos
	}
	return path[steps-1]
}

// The headline regression: demo bots must reliably cross every carved map
// shape end to end using the routed action stream (move_to delegation plus
// legacy steps), the way a live bot follows its tactics-layer destinations.
func TestShapeRoutedTravel(t *testing.T) {
	shapes := []game.MapShape{
		game.ShapeCaves,
		game.ShapeDonut,
		game.ShapeIslands,
		game.ShapeRooms,
		game.ShapeSpiral,
	}
	for _, shape := range shapes {
		t.Run(string(shape), func(t *testing.T) {
			rng := rand.New(rand.NewSource(1234))
			const maps = 4
			const pairsPerMap = 20

			attempts, reached := 0, 0
			for m := 0; m < maps; m++ {
				terrain := buildSeededShapeTerrain(t, shape, 100, 100, int64(9000+m))
				for p := 0; p < pairsPerMap; p++ {
					start := randomOpenCell(rng, terrain)
					goal := randomOpenCell(rng, terrain)
					best := shortestPathLen(terrain, start[0], start[1], goal[0], goal[1])
					if best <= 0 {
						continue
					}
					attempts++
					budget := best*3 + 20
					router := &movementRouter{}
					cur := start
					ok := false
					for tick := 0; tick < budget; tick++ {
						pos := [2]float64{float64(cur[0]), float64(cur[1])}
						dst := [2]float64{float64(goal[0]), float64(goal[1])}
						action := router.route(routeTick(pos), moveIntent(pos, dst), "route-bot")
						switch {
						case action.Action == "move_to" && action.TargetPosition != nil:
							target := [2]int{
								int(math.Round(action.TargetPosition[0])),
								int(math.Round(action.TargetPosition[1])),
							}
							cur = simulateServerMoveTo(cur, target, 2)
						case action.Action == "move" && action.Direction != nil:
							next := [2]int{cur[0] + int(action.Direction[0]), cur[1] + int(action.Direction[1])}
							if !terrain.isBlocked(next[0], next[1]) {
								cur = next
							}
						default:
							// idle: no movement this tick
						}
						if cur == goal {
							ok = true
							break
						}
					}
					if ok {
						reached++
					} else {
						t.Logf("map %d: routed travel failed %v -> %v (shortest %d)", m, start, goal, best)
					}
				}
			}

			if attempts == 0 {
				t.Fatal("shape generated no reachable pairs")
			}
			rate := float64(reached) / float64(attempts)
			t.Logf("%s routed travel success: %d/%d (%.1f%%)", shape, reached, attempts, rate*100)
			if rate < 0.99 {
				t.Errorf("routed travel success %.1f%% on %s — bots would still get stuck", rate*100, shape)
			}
		})
	}
}

// Goal hysteresis: a destination wobbling across a cell boundary must not
// thrash the emitted move_to target every tick.
func TestRouteGoalHysteresis(t *testing.T) {
	openTerrain(t, 40)
	router := &movementRouter{}
	pos := [2]float64{2, 2}

	first := router.route(routeTick(pos), moveIntent(pos, [2]float64{30, 30}), "route-bot")
	second := router.route(routeTick(pos), moveIntent(pos, [2]float64{31, 30}), "route-bot")
	if first.TargetPosition == nil || second.TargetPosition == nil {
		t.Fatal("expected move_to for both ticks")
	}
	if *second.TargetPosition != *first.TargetPosition {
		t.Fatalf("wobbling goal re-targeted: %v then %v", *first.TargetPosition, *second.TargetPosition)
	}
}
