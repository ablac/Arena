package game

import (
	"math"
	"math/rand"

	"arena-server/internal/config"
)

// ProcessMovement handles MOVE, MOVE_TO, and DODGE actions for all alive bots.
func ProcessMovement(bots map[string]*BotState, obstacles []Obstacle, grid *SpatialGrid, navGrid *NavGrid, dt float64) {
	for _, bot := range bots {
		if !bot.IsAlive || bot.PendingAction == nil {
			continue
		}

		// Stunned or frozen bots cannot move.
		if bot.StunTicks > 0 || bot.Frozen {
			continue
		}

		switch bot.PendingAction.Type {
		case ActionDodge:
			processDodge(bot, obstacles, grid, dt)

		case ActionMove:
			processMove(bot, obstacles, grid, dt)

		case ActionMoveTo:
			processMoveTo(bot, obstacles, grid, navGrid, dt)
		}
	}

	// Final grid sync: ensure all alive bots have accurate grid positions.
	for id, bot := range bots {
		if bot.IsAlive {
			grid.Update(id, bot.Position)
		}
	}
}

// processDodge executes the dodge action for a single bot.
func processDodge(bot *BotState, obstacles []Obstacle, grid *SpatialGrid, dt float64) {
	if bot.DodgeCooldown > 0 {
		bot.LastActionResult = &ActionResult{
			Action:  "dodge",
			Success: false,
			Message: "dodge on cooldown",
		}
		return
	}

	dir := bot.PendingAction.Direction.Normalized()
	// If direction is zero, pick a random one.
	if dir.Length() < 1e-10 {
		angle := rand.Float64() * 2 * math.Pi
		dir = NewVec2(math.Cos(angle), math.Sin(angle))
	}

	speed := bot.Speed * config.C.DodgeSpeedMult
	newX := bot.Position.X() + dir.X()*speed
	newY := bot.Position.Y() + dir.Y()*speed

	// Slide along obstacles.
	newX, newY = SlideAlongObstacle(bot.Position.X(), bot.Position.Y(), newX, newY, obstacles, config.C.BotRadius)

	// Clamp to arena bounds.
	newX = clampToArena(newX, config.C.BotRadius, config.C.ArenaWidth)
	newY = clampToArena(newY, config.C.BotRadius, config.C.ArenaHeight)

	bot.Position = NewVec2(newX, newY)
	bot.InvulnTicks = config.C.DodgeInvulnTicks
	bot.DodgeCooldown = config.C.DodgeCooldownTicks

	grid.Update(bot.BotID, bot.Position)

	bot.LastActionResult = &ActionResult{
		Action:  "dodge",
		Success: true,
	}
}

// processMove executes a directional move for a single bot.
func processMove(bot *BotState, obstacles []Obstacle, grid *SpatialGrid, dt float64) {
	dir := bot.PendingAction.Direction.Normalized()
	if dir.Length() < 1e-10 {
		return
	}

	effectiveSpeed := bot.Speed

	// Apply speed boost effects.
	for _, eff := range bot.ActiveEffects {
		if eff.Name == "speed_boost" {
			effectiveSpeed *= eff.Value
		}
	}

	oldPos := bot.Position
	newX := bot.Position.X() + dir.X()*effectiveSpeed
	newY := bot.Position.Y() + dir.Y()*effectiveSpeed

	// Slide along obstacles.
	newX, newY = SlideAlongObstacle(bot.Position.X(), bot.Position.Y(), newX, newY, obstacles, config.C.BotRadius)

	// Clamp to arena bounds.
	newX = clampToArena(newX, config.C.BotRadius, config.C.ArenaWidth)
	newY = clampToArena(newY, config.C.BotRadius, config.C.ArenaHeight)

	bot.Position = NewVec2(newX, newY)

	// Track distance traveled.
	dist := oldPos.DistanceTo(bot.Position)
	bot.RoundDistance += dist

	grid.Update(bot.BotID, bot.Position)
}

// processMoveTo executes pathfinding-based movement for a single bot.
func processMoveTo(bot *BotState, obstacles []Obstacle, grid *SpatialGrid, navGrid *NavGrid, dt float64) {
	action := bot.PendingAction

	// Determine the goal position.
	var goal Vec2
	if action.TargetPosition != nil {
		goal = *action.TargetPosition
	} else {
		// No target position specified; nothing to do.
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
			// No nav grid available; move directly.
			bot.CurrentPath = []Vec2{goal}
		}
		goalCopy := goal
		bot.PathTarget = &goalCopy
	}

	if len(bot.CurrentPath) == 0 {
		return
	}

	// Follow the first waypoint.
	for len(bot.CurrentPath) > 1 {
		wp := bot.CurrentPath[0]
		if bot.Position.DistanceTo(wp) < 1.0 {
			bot.CurrentPath = bot.CurrentPath[1:]
		} else {
			break
		}
	}

	if len(bot.CurrentPath) == 0 {
		return
	}

	waypoint := bot.CurrentPath[0]
	dir := waypoint.Sub(bot.Position).Normalized()
	if dir.Length() < 1e-10 {
		// Already at waypoint.
		bot.CurrentPath = bot.CurrentPath[1:]
		return
	}

	effectiveSpeed := bot.Speed

	// Apply speed boost effects.
	for _, eff := range bot.ActiveEffects {
		if eff.Name == "speed_boost" {
			effectiveSpeed *= eff.Value
		}
	}

	oldPos := bot.Position
	newX := bot.Position.X() + dir.X()*effectiveSpeed
	newY := bot.Position.Y() + dir.Y()*effectiveSpeed

	// Slide along obstacles.
	newX, newY = SlideAlongObstacle(bot.Position.X(), bot.Position.Y(), newX, newY, obstacles, config.C.BotRadius)

	// Clamp to arena bounds.
	newX = clampToArena(newX, config.C.BotRadius, config.C.ArenaWidth)
	newY = clampToArena(newY, config.C.BotRadius, config.C.ArenaHeight)

	bot.Position = NewVec2(newX, newY)

	// Track distance traveled.
	dist := oldPos.DistanceTo(bot.Position)
	bot.RoundDistance += dist

	grid.Update(bot.BotID, bot.Position)

	// If we reached the current waypoint, advance.
	if bot.Position.DistanceTo(waypoint) < 1.0 {
		bot.CurrentPath = bot.CurrentPath[1:]
	}
}

// SeparateBots pushes overlapping bots apart over 2 iterations.
func SeparateBots(bots map[string]*BotState, obstacles []Obstacle, grid *SpatialGrid) {
	queryRadius := config.C.BotSeparationDist + 5.0
	minDist := 2.0 * config.C.BotRadius

	for iter := 0; iter < 2; iter++ {
		for id, bot := range bots {
			if !bot.IsAlive {
				continue
			}

			nearby := grid.QueryRadius(bot.Position, queryRadius)
			for _, otherID := range nearby {
				if otherID == id {
					continue
				}
				other, ok := bots[otherID]
				if !ok || !other.IsAlive {
					continue
				}

				dist := bot.Position.DistanceTo(other.Position)
				if dist >= minDist {
					continue
				}

				// Compute separation vector.
				sep := bot.Position.Sub(other.Position)
				if sep.Length() < 1e-10 {
					// Bots are exactly on top of each other; push in a random direction.
					angle := rand.Float64() * 2 * math.Pi
					sep = NewVec2(math.Cos(angle), math.Sin(angle))
				}
				sep = sep.Normalized()

				push := config.C.BotSeparationFactor * (minDist - dist) * 0.5

				// Push this bot.
				botMoved := false
				newX := bot.Position.X() + sep.X()*push
				newY := bot.Position.Y() + sep.Y()*push

				if CollidesWithObstacle(newX, newY, obstacles, config.C.BotRadius) == nil {
					newX = clampToArena(newX, config.C.BotRadius, config.C.ArenaWidth)
					newY = clampToArena(newY, config.C.BotRadius, config.C.ArenaHeight)
					bot.Position = NewVec2(newX, newY)
					grid.Update(id, bot.Position)
					botMoved = true
				}

				// Push the other bot in the opposite direction.
				otherMoved := false
				otherNewX := other.Position.X() - sep.X()*push
				otherNewY := other.Position.Y() - sep.Y()*push

				if CollidesWithObstacle(otherNewX, otherNewY, obstacles, config.C.BotRadius) == nil {
					otherNewX = clampToArena(otherNewX, config.C.BotRadius, config.C.ArenaWidth)
					otherNewY = clampToArena(otherNewY, config.C.BotRadius, config.C.ArenaHeight)
					other.Position = NewVec2(otherNewX, otherNewY)
					grid.Update(otherID, other.Position)
					otherMoved = true
				}

				// If both pushes failed, try perpendicular nudge to unstick.
				if !botMoved && !otherMoved {
					perp := NewVec2(-sep.Y(), sep.X())
					px := bot.Position.X() + perp.X()*push
					py := bot.Position.Y() + perp.Y()*push
					if CollidesWithObstacle(px, py, obstacles, config.C.BotRadius) == nil {
						px = clampToArena(px, config.C.BotRadius, config.C.ArenaWidth)
						py = clampToArena(py, config.C.BotRadius, config.C.ArenaHeight)
						bot.Position = NewVec2(px, py)
						grid.Update(id, bot.Position)
					}
					opx := other.Position.X() - perp.X()*push
					opy := other.Position.Y() - perp.Y()*push
					if CollidesWithObstacle(opx, opy, obstacles, config.C.BotRadius) == nil {
						opx = clampToArena(opx, config.C.BotRadius, config.C.ArenaWidth)
						opy = clampToArena(opy, config.C.BotRadius, config.C.ArenaHeight)
						other.Position = NewVec2(opx, opy)
						grid.Update(otherID, other.Position)
					}
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
