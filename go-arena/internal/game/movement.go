package game

import (
	"math"
	"math/rand"

	"arena-server/internal/config"
)

// normalizeActionTargetPosition accepts either grid coordinates or world
// coordinates and returns a snapped world-space position.
func normalizeActionTargetPosition(tp Vec2) Vec2 {
	if ActiveTerrain == nil {
		return tp
	}
	if tp.X() < float64(ActiveTerrain.Width) && tp.Y() < float64(ActiveTerrain.Height) &&
		tp.X() >= 0 && tp.Y() >= 0 {
		cell := [2]int{int(math.Round(tp.X())), int(math.Round(tp.Y()))}
		return ActiveTerrain.GridToWorld(cell)
	}
	cell := ActiveTerrain.WorldToGrid(tp)
	return ActiveTerrain.GridToWorld(cell)
}

// effectiveMoveSpeed returns the bot's configured movement speed with active
// speed effects applied. Keeping this calculation in the movement system
// makes the speed stat authoritative for both directional and path movement.
func effectiveMoveSpeed(bot *BotState) float64 {
	if bot == nil {
		return 0
	}
	speed := bot.Speed
	for _, eff := range bot.ActiveEffects {
		if eff.Name == "speed_boost" {
			speed *= eff.Value
		}
	}
	return speed
}

// terrainMoveCellsPerTick is the authoritative conversion from the continuous
// speed stat to average terrain cells per tick. Movement pacing and projectile
// lead prediction both use it so changing a speed allocation cannot make the
// server aim with stale fixed-speed assumptions.
func terrainMoveCellsPerTick(bot *BotState) float64 {
	referencePoints := float64(config.C.StatBudget) / 4
	referenceSpeed := config.C.StatSpeedBase + referencePoints*config.C.StatSpeedPerPoint
	if math.IsNaN(referenceSpeed) || math.IsInf(referenceSpeed, 0) || referenceSpeed <= 0 {
		referenceSpeed = 1
	}

	speed := effectiveMoveSpeed(bot)
	if math.IsNaN(speed) || math.IsInf(speed, 0) || speed <= 0 {
		speed = referenceSpeed
	}
	basePace := config.C.TerrainMoveCellsPerTick
	if math.IsNaN(basePace) || math.IsInf(basePace, 0) || basePace <= 0 || basePace > config.MaxTerrainMoveCellsPerTick {
		basePace = config.DefaultTerrainMoveCellsPerTick
	}
	pace := basePace * speed / referenceSpeed
	if math.IsNaN(pace) || math.IsInf(pace, 0) || pace <= 0 {
		return config.DefaultTerrainMoveCellsPerTick
	}
	// Eight cells in one tick still covers deliberate high-speed effects while
	// providing a hard work bound if runtime bot state is ever corrupted.
	return math.Min(pace, 8)
}

// gridMoveCellsForTick converts continuous MoveSpeed into grid-cell movement
// credits. A balanced loadout (one quarter of the stat budget in speed) uses
// the configured base cadence, while lower and higher allocations move
// proportionally slower or faster. Fractional credits carry across ticks so
// small stat differences are not rounded away.
func gridMoveCellsForTick(bot *BotState) int {
	bot.MoveProgress += terrainMoveCellsPerTick(bot)
	cells := int(math.Floor(bot.MoveProgress + 1e-9))
	if cells > 0 {
		bot.MoveProgress -= float64(cells)
	}
	return cells
}

func movementSign(v int) int {
	if v > 0 {
		return 1
	}
	if v < 0 {
		return -1
	}
	return 0
}

func recordMovementPosition(bot *BotState, position Vec2) {
	if bot == nil {
		return
	}
	bot.MovementTrace = append(bot.MovementTrace, position)
}

// firstMovementPositionInRange checks every cell entered this tick before the
// final position. Arena effects use this to prevent multi-cell moves from
// jumping over mines, burn fields, and teleport pads.
func firstMovementPositionInRange(bot *BotState, position Vec2, gridRange int) (Vec2, bool) {
	if bot == nil {
		return Vec2{}, false
	}
	for _, entered := range bot.MovementTrace {
		if IsInRange(entered, position, gridRange) {
			return entered, true
		}
	}
	if IsInRange(bot.Position, position, gridRange) {
		return bot.Position, true
	}
	return Vec2{}, false
}

// ProcessMovement handles MOVE, MOVE_TO, and DODGE actions for all alive bots.
// Terrain movement uses fractional cell credits derived from the speed stat.
func ProcessMovement(bots map[string]*BotState, obstacles []Obstacle, grid *SpatialGrid, navGrid *NavGrid, dt float64) {
	for _, bot := range bots {
		bot.MovementTrace = bot.MovementTrace[:0]
		if !bot.IsAlive || bot.PendingAction == nil {
			continue
		}

		// Stunned or frozen bots cannot move.
		if bot.StunTicks > 0 || bot.Frozen {
			continue
		}

		// The legacy non-terrain movement path keeps its every-other-tick
		// cadence. Terrain movement is paced by gridMoveCellsForTick instead.
		// Dodge always goes through immediately (it's a combat ability).
		if ActiveTerrain == nil && bot.PendingAction.Type != ActionDodge {
			if bot.MoveCooldown > 0 {
				bot.MoveCooldown--
				continue
			}
		}

		switch bot.PendingAction.Type {
		case ActionDodge:
			processDodge(bot, obstacles, grid, dt)

		case ActionMove:
			cells := 1
			if ActiveTerrain != nil {
				cells = gridMoveCellsForTick(bot)
				if cells == 0 {
					continue
				}
			}
			processMove(bot, obstacles, grid, dt, cells)
			if ActiveTerrain == nil {
				bot.MoveCooldown = 1 // skip next tick
			}

		case ActionMoveTo:
			cells := 1
			if ActiveTerrain != nil {
				cells = gridMoveCellsForTick(bot)
				if cells == 0 {
					continue
				}
			}
			processMoveTo(bot, obstacles, grid, navGrid, dt, cells)
			if ActiveTerrain == nil {
				bot.MoveCooldown = 1 // skip next tick
			}
		}
	}

	// Final grid sync: ensure all alive bots have accurate grid positions.
	for id, bot := range bots {
		if bot.IsAlive {
			grid.Update(id, bot.Position)
		}
	}
}

// processMove executes grid-based directional movement for a single bot.
func processMove(bot *BotState, obstacles []Obstacle, grid *SpatialGrid, dt float64, maxCells int) {
	if ActiveTerrain == nil {
		return
	}

	dir := bot.PendingAction.Direction
	dx := SnapDirection(dir.X())
	dy := SnapDirection(dir.Y())
	if dx == 0 && dy == 0 {
		return
	}

	currentCell := ActiveTerrain.WorldToGrid(bot.Position)
	oldPos := bot.Position

	for step := 0; step < maxCells; step++ {
		// Hard wall check: if the target cell is blocked, STOP. No sliding.
		if ActiveTerrain.IsMoveBlocked(currentCell[0], currentCell[1], dx, dy) {
			break
		}
		currentCell = [2]int{currentCell[0] + dx, currentCell[1] + dy}
		recordMovementPosition(bot, ActiveTerrain.GridToWorld(currentCell))
	}

	newPos := ActiveTerrain.GridToWorld(currentCell)

	// Final validation: never place bot in a blocked cell.
	if ActiveTerrain.IsBlocked(currentCell[0], currentCell[1]) {
		return // reject move entirely
	}

	bot.Position = newPos
	bot.LastValidPosition = newPos
	bot.Facing = Vec2{float64(dx), float64(dy)}.Normalized()

	// Track distance traveled.
	dist := oldPos.DistanceTo(bot.Position)
	bot.RoundDistance += dist

	grid.Update(bot.BotID, bot.Position)
}

// processDodge executes a grid-based dodge: moves 2 cells + grants invulnerability.
func processDodge(bot *BotState, obstacles []Obstacle, grid *SpatialGrid, dt float64) {
	if bot.DodgeCooldown > 0 {
		bot.LastActionResult = &ActionResult{
			Action:  "dodge",
			Success: false,
			Message: "dodge on cooldown",
		}
		return
	}

	if ActiveTerrain == nil {
		return
	}

	dir := bot.PendingAction.Direction.Normalized()
	dx := SnapDirection(dir.X())
	dy := SnapDirection(dir.Y())

	// If direction is zero, pick a random one.
	if dx == 0 && dy == 0 {
		angle := rand.Float64() * 2 * math.Pi
		dx = SnapDirection(math.Cos(angle))
		dy = SnapDirection(math.Sin(angle))
		if dx == 0 && dy == 0 {
			dx = 1
		}
	}

	currentCell := ActiveTerrain.WorldToGrid(bot.Position)

	// Walk cell by cell (up to 2); stop at the first wall or diagonal
	// corner so we never teleport through a blocked cell.
	destCell := currentCell
	placed := false
	prev := currentCell
	for step := 1; step <= 2; step++ {
		next := [2]int{currentCell[0] + dx*step, currentCell[1] + dy*step}
		if ActiveTerrain.IsMoveBlocked(prev[0], prev[1], dx, dy) {
			break
		}
		prev = next
		destCell = next
		recordMovementPosition(bot, ActiveTerrain.GridToWorld(next))
		placed = true
	}

	if !placed {
		bot.LastActionResult = &ActionResult{
			Action:  "dodge",
			Success: false,
			Message: "no valid dodge destination",
		}
		return
	}

	// Final validation: never place bot in a blocked cell.
	if ActiveTerrain.IsBlocked(destCell[0], destCell[1]) {
		bot.LastActionResult = &ActionResult{
			Action:  "dodge",
			Success: false,
			Message: "no valid dodge destination",
		}
		return
	}

	bot.Position = ActiveTerrain.GridToWorld(destCell)
	bot.LastValidPosition = bot.Position
	bot.Facing = Vec2{float64(dx), float64(dy)}.Normalized()
	bot.InvulnTicks = config.C.DodgeInvulnTicks
	bot.DodgeCooldown = scaledCooldownTicks(config.C.DodgeCooldownTicks, effectCooldownMultiplier(bot))

	grid.Update(bot.BotID, bot.Position)

	bot.LastActionResult = &ActionResult{
		Action:  "dodge",
		Success: true,
	}
}

// processMoveTo executes pathfinding-based movement for a single bot.
func processMoveTo(bot *BotState, obstacles []Obstacle, grid *SpatialGrid, navGrid *NavGrid, dt float64, maxCells int) {
	action := bot.PendingAction

	// Determine the goal position.
	var goal Vec2
	if action.TargetPosition != nil {
		goal = normalizeActionTargetPosition(*action.TargetPosition)
	} else {
		return
	}

	// Compute a new path if we have none or if the target changed.
	needNewPath := len(bot.CurrentPath) == 0 ||
		bot.PathTarget == nil ||
		bot.PathTarget.DistanceTo(goal) > 1.0

	if needNewPath {
		if navGrid != nil {
			bot.CurrentPath = FindPath(bot.Position, goal, navGrid)
		} else {
			bot.CurrentPath = []Vec2{goal}
		}
		goalCopy := goal
		bot.PathTarget = &goalCopy
	}

	if len(bot.CurrentPath) == 0 {
		return
	}

	// Follow the first waypoint: advance past any already-reached waypoints.
	if ActiveTerrain != nil {
		currentCell := ActiveTerrain.WorldToGrid(bot.Position)
		for len(bot.CurrentPath) > 1 {
			wpCell := ActiveTerrain.WorldToGrid(bot.CurrentPath[0])
			if wpCell == currentCell {
				bot.CurrentPath = bot.CurrentPath[1:]
			} else {
				break
			}
		}
	} else {
		for len(bot.CurrentPath) > 1 {
			wp := bot.CurrentPath[0]
			if bot.Position.DistanceTo(wp) < 1.0 {
				bot.CurrentPath = bot.CurrentPath[1:]
			} else {
				break
			}
		}
	}

	if len(bot.CurrentPath) == 0 {
		return
	}

	if ActiveTerrain != nil {
		// Spend movement credits one cell at a time. Checking every traversed
		// edge prevents a high-speed bot from tunnelling through a wall.
		for step := 0; step < maxCells && len(bot.CurrentPath) > 0; step++ {
			currentCell := ActiveTerrain.WorldToGrid(bot.Position)
			for len(bot.CurrentPath) > 0 && ActiveTerrain.WorldToGrid(bot.CurrentPath[0]) == currentCell {
				bot.CurrentPath = bot.CurrentPath[1:]
			}
			if len(bot.CurrentPath) == 0 {
				break
			}

			wpCell := ActiveTerrain.WorldToGrid(bot.CurrentPath[0])
			dx := movementSign(wpCell[0] - currentCell[0])
			dy := movementSign(wpCell[1] - currentCell[1])
			if dx == 0 && dy == 0 {
				bot.CurrentPath = bot.CurrentPath[1:]
				step--
				continue
			}

			if ActiveTerrain.IsMoveBlocked(currentCell[0], currentCell[1], dx, dy) {
				break
			}
			targetCell := [2]int{currentCell[0] + dx, currentCell[1] + dy}
			if ActiveTerrain.IsBlocked(targetCell[0], targetCell[1]) {
				break
			}

			oldPos := bot.Position
			bot.Position = ActiveTerrain.GridToWorld(targetCell)
			recordMovementPosition(bot, bot.Position)
			bot.RoundDistance += oldPos.DistanceTo(bot.Position)
			bot.LastValidPosition = bot.Position
			bot.Facing = Vec2{float64(dx), float64(dy)}.Normalized()
			if targetCell == wpCell {
				bot.CurrentPath = bot.CurrentPath[1:]
			}
		}
	} else {
		// Fallback: float-based movement.
		waypoint := bot.CurrentPath[0]
		dir := waypoint.Sub(bot.Position).Normalized()
		if dir.Length() < 1e-10 {
			bot.CurrentPath = bot.CurrentPath[1:]
			return
		}

		effectiveSpeed := bot.Speed
		for _, eff := range bot.ActiveEffects {
			if eff.Name == "speed_boost" {
				effectiveSpeed *= eff.Value
			}
		}

		oldPos := bot.Position
		newX := bot.Position.X() + dir.X()*effectiveSpeed
		newY := bot.Position.Y() + dir.Y()*effectiveSpeed

		newX, newY = SlideAlongObstacle(bot.Position.X(), bot.Position.Y(), newX, newY, obstacles, config.C.BotRadius)
		newX = clampToArena(newX, config.C.BotRadius, config.C.ArenaWidth)
		newY = clampToArena(newY, config.C.BotRadius, config.C.ArenaHeight)

		bot.Position = NewVec2(newX, newY)
		recordMovementPosition(bot, bot.Position)
		bot.RoundDistance += oldPos.DistanceTo(bot.Position)
		bot.Facing = dir

		if bot.Position.DistanceTo(waypoint) < 1.0 {
			bot.CurrentPath = bot.CurrentPath[1:]
		}
	}

	grid.Update(bot.BotID, bot.Position)
}

// SeparateBots pushes overlapping bots apart. With grid-based movement this
// is less critical, but knockback can place two bots in the same cell.
func SeparateBots(bots map[string]*BotState, obstacles []Obstacle, grid *SpatialGrid) {
	if ActiveTerrain == nil {
		return
	}

	// Build occupation map: cell -> list of bot IDs.
	occupied := make(map[[2]int][]string)
	for id, bot := range bots {
		if !bot.IsAlive {
			continue
		}
		cell := ActiveTerrain.WorldToGrid(bot.Position)
		occupied[cell] = append(occupied[cell], id)
	}

	// For cells with multiple bots, push extras to adjacent empty cells.
	for cell, ids := range occupied {
		if len(ids) <= 1 {
			continue
		}

		// First bot stays, others get pushed to adjacent cells.
		for i := 1; i < len(ids); i++ {
			bot := bots[ids[i]]
			placed := false

			// Try all 8 directions (using IsMoveBlocked for diagonal corner-cutting).
			for _, d := range directions {
				nc := [2]int{cell[0] + d.dx, cell[1] + d.dy}
				if !ActiveTerrain.IsMoveBlocked(cell[0], cell[1], d.dx, d.dy) {
					if occs := occupied[nc]; len(occs) == 0 {
						bot.Position = ActiveTerrain.GridToWorld(nc)
						bot.LastValidPosition = bot.Position
						grid.Update(ids[i], bot.Position)
						occupied[nc] = append(occupied[nc], ids[i])
						placed = true
						break
					}
				}
			}

			if !placed {
				// All adjacent cells occupied — try a wider ring (distance 2).
				for _, d1 := range directions {
					nc := [2]int{cell[0] + d1.dx*2, cell[1] + d1.dy*2}
					mid := [2]int{cell[0] + d1.dx, cell[1] + d1.dy}
					if !ActiveTerrain.IsMoveBlocked(cell[0], cell[1], d1.dx, d1.dy) &&
						!ActiveTerrain.IsMoveBlocked(mid[0], mid[1], d1.dx, d1.dy) {
						if occs := occupied[nc]; len(occs) == 0 {
							bot.Position = ActiveTerrain.GridToWorld(nc)
							bot.LastValidPosition = bot.Position
							grid.Update(ids[i], bot.Position)
							occupied[nc] = append(occupied[nc], ids[i])
							placed = true
							break
						}
					}
				}
				// If still not placed, stay in current cell (stacked) rather than
				// phasing through a wall.
				if !placed {
					grid.Update(ids[i], bot.Position)
				}
			}
		}
	}
}

// clampToArena constrains a coordinate to [margin, arenaDim - margin].
func clampToArena(val, margin, arenaDim float64) float64 {
	if val < margin {
		return margin
	}
	if val > arenaDim-margin {
		return arenaDim - margin
	}
	return val
}

// NavGrid and FindPath are defined in pathfinding.go.
