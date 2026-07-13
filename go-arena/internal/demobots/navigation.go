package demobots

import (
	"math"

	"arena-server/internal/config"
)

const (
	navigationOscillationLimit = 2
	navigationRecoveryRadius   = 7
	navigationRecoveryTicks    = 16
)

// navigationState keeps the small amount of per-bot history that cannot live
// in PickAction's pure, single-tick decision model. It detects movement intent
// that makes no progress and two-cell loops, then briefly routes toward a
// reachable side waypoint instead of repeating the same wall-facing action.
type navigationState struct {
	hasPrevious        bool
	previousCell       [2]int
	hasLastDistinct    bool
	hasTwoDistinct     bool
	lastDistinctCell   [2]int
	twoDistinctAgoCell [2]int
	lastWasMovement    bool
	stalledTicks       int
	oscillationHits    int
	recovery           *navigationRecovery
}

type navigationRecovery struct {
	waypoint             [2]float64
	avoidCell            [2]int
	requestedDirection   [2]int
	directionChangeGrace int
	ticksLeft            int
}

func (n *navigationState) reset() {
	*n = navigationState{}
}

func (n *navigationState) clearMotionEvidence(cell [2]int) {
	n.stalledTicks = 0
	n.oscillationHits = 0
	n.lastDistinctCell = cell
	n.hasLastDistinct = true
	n.hasTwoDistinct = false
}

func (n *navigationState) stabilize(msg map[string]interface{}, requested actionResult, botID string) actionResult {
	position, speed, ok := navigationPosition(msg)
	if !ok {
		return requested
	}
	cell := [2]int{int(math.Round(position[0])), int(math.Round(position[1]))}

	if n.hasPrevious && n.lastWasMovement {
		if cell == n.previousCell {
			n.stalledTicks++
		} else {
			n.stalledTicks = 0
		}
	}

	// Movement is applied at a lower cadence than spectator ticks for some
	// speed profiles, producing A,A,B,B,A,A rather than literal A,B,A,B. Track
	// the last two distinct cells so fractional hold frames cannot erase the
	// evidence of a real two-cell loop.
	if !n.hasPrevious || !n.lastWasMovement {
		n.lastDistinctCell = cell
		n.hasLastDistinct = true
		n.hasTwoDistinct = false
	} else if cell != n.lastDistinctCell {
		if n.hasTwoDistinct && cell == n.twoDistinctAgoCell {
			n.oscillationHits++
		} else {
			n.oscillationHits = 0
		}
		n.twoDistinctAgoCell = n.lastDistinctCell
		n.hasTwoDistinct = true
		n.lastDistinctCell = cell
	}

	previous := n.previousCell
	n.previousCell = cell
	n.hasPrevious = true
	if !isMovementAction(requested) {
		// A tactical action or deliberate hold ends the previous navigation
		// attempt. Do not let its stale stall/loop evidence arm recovery for the
		// next, potentially unrelated movement objective.
		n.clearMotionEvidence(cell)
	}

	result := requested
	if n.recovery != nil {
		if !canContinueNavigationRecovery(requested) {
			n.recovery = nil
			n.clearMotionEvidence(cell)
		} else {
			requestedDirection := actionGridDirection(position, requested)
			directionChanged := requestedDirection != [2]int{} && requestedDirection != n.recovery.requestedDirection
			if directionChanged && n.recovery.directionChangeGrace <= 0 {
				// Recovery is only a correction for the movement that got stuck.
				// A new flee, pickup, zone, or combat objective must take effect now.
				n.recovery = nil
				n.clearMotionEvidence(cell)
			} else if cell == [2]int{int(math.Round(n.recovery.waypoint[0])), int(math.Round(n.recovery.waypoint[1]))} || n.recovery.ticksLeft <= 0 {
				n.recovery = nil
				n.clearMotionEvidence(cell)
			} else {
				recovery := navigationRecoveryAction(msg, botID, *n.recovery)
				n.recovery.ticksLeft--
				if recovery.Action == "idle" {
					// Dynamic hazards and newly ready teleporters can invalidate a
					// previously reachable waypoint. Drop it immediately so this
					// correction cannot become an infinite idle retry.
					n.recovery = nil
					n.clearMotionEvidence(cell)
				} else {
					if directionChanged {
						n.recovery.directionChangeGrace--
					}
					result = recovery
				}
			}
		}
	}

	if n.recovery == nil && isMovementAction(requested) &&
		(n.stalledTicks >= navigationStallLimit(speed) || n.oscillationHits >= navigationOscillationLimit) {
		oscillationRecovery := n.oscillationHits >= navigationOscillationLimit
		failedDirection := actionGridDirection(position, requested)
		avoidCell := previous
		if cell == previous {
			avoidCell = [2]int{cell[0] + failedDirection[0], cell[1] + failedDirection[1]}
		}
		if waypoint, found := navigationRecoveryWaypoint(msg, botID, cell, avoidCell, failedDirection); found {
			n.recovery = &navigationRecovery{
				waypoint:           waypoint,
				avoidCell:          avoidCell,
				requestedDirection: failedDirection,
				ticksLeft:          navigationRecoveryTicks,
			}
			// An A-B-A-B loop naturally alternates its requested direction. Give
			// that recovery one opposite-direction tick so it can make multiple
			// corrective steps, while still yielding quickly to a real retarget.
			if oscillationRecovery {
				n.recovery.directionChangeGrace = 1
			}
			result = navigationRecoveryAction(msg, botID, *n.recovery)
			n.clearMotionEvidence(cell)
		}
	}

	n.lastWasMovement = isMovementAction(result)
	return result
}

func navigationPosition(msg map[string]interface{}) ([2]float64, float64, bool) {
	state, ok := msg["your_state"].(map[string]interface{})
	if !ok {
		return [2]float64{}, 0, false
	}
	position := parsePos(state["position"])
	speed, _ := state["speed"].(float64)
	return position, speed, true
}

func navigationStallLimit(speed float64) int {
	referencePoints := float64(config.C.StatBudget) / 4
	referenceSpeed := config.C.StatSpeedBase + referencePoints*config.C.StatSpeedPerPoint
	if math.IsNaN(referenceSpeed) || math.IsInf(referenceSpeed, 0) || referenceSpeed <= 0 {
		referenceSpeed = 1
	}
	if math.IsNaN(speed) || math.IsInf(speed, 0) || speed <= 0 {
		speed = referenceSpeed
	}
	basePace := config.C.TerrainMoveCellsPerTick
	if math.IsNaN(basePace) || math.IsInf(basePace, 0) || basePace <= 0 || basePace > config.MaxTerrainMoveCellsPerTick {
		basePace = config.DefaultTerrainMoveCellsPerTick
	}
	pace := basePace * speed / referenceSpeed
	if math.IsNaN(pace) || math.IsInf(pace, 0) || pace <= 0 {
		pace = config.DefaultTerrainMoveCellsPerTick
	}
	// The authoritative movement system accrues fractional cell credits. Wait
	// for two complete server-paced cells before treating unchanged coordinates
	// as a wall stall; otherwise slower loadouts are "recovered" immediately
	// before their first legitimate movement credit becomes usable.
	limit := int(math.Ceil(2 / pace))
	return max(2, limit)
}

func isMovementAction(action actionResult) bool {
	return action.Action == "move" || action.Action == "move_to" ||
		(action.Action == "grapple" && action.TargetPosition != nil)
}

func canContinueNavigationRecovery(action actionResult) bool {
	// An explicit idle can be tactical (stun, spear brace, objective hold).
	// Recovery is only allowed to replace another movement decision; otherwise
	// it must yield immediately just like it does for attacks and dodges.
	return isMovementAction(action)
}

func actionGridDirection(position [2]float64, action actionResult) [2]int {
	if action.Direction != nil {
		return [2]int{int(fsign(action.Direction[0])), int(fsign(action.Direction[1]))}
	}
	if action.TargetPosition != nil {
		return [2]int{
			int(fsign(action.TargetPosition[0] - position[0])),
			int(fsign(action.TargetPosition[1] - position[1])),
		}
	}
	return [2]int{}
}

func navigationRecoveryAction(msg map[string]interface{}, botID string, recovery navigationRecovery) actionResult {
	ts := parseTick(msg)
	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	buildDangerSet(danger, &ts, botID)
	if recovery.avoidCell != [2]int{int(math.Round(ts.Position[0])), int(math.Round(ts.Position[1]))} {
		danger.add(recovery.avoidCell[0], recovery.avoidCell[1])
	}
	return moveTo(ts.Position, recovery.waypoint, danger)
}

func navigationRecoveryWaypoint(msg map[string]interface{}, botID string, start, avoid, failedDirection [2]int) ([2]float64, bool) {
	ts := parseTick(msg)
	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	buildDangerSet(danger, &ts, botID)

	terrain := getTerrain()
	if terrain == nil || terrain.isBlocked(start[0], start[1]) {
		return [2]float64{}, false
	}

	scratch := bfsPool.Get().(*bfsScratch)
	defer bfsPool.Put(scratch)
	scratch.reset(terrain.Width, terrain.Height)
	scratch.visit(start[0], start[1])
	scratch.queue = append(scratch.queue, bfsNode{col: start[0], row: start[1]})

	bestScore := math.Inf(-1)
	best := start
	found := false
	for i := 0; i < len(scratch.queue); i++ {
		node := scratch.queue[i]
		if node.distance >= navigationRecoveryRadius {
			continue
		}
		for dc := -1; dc <= 1; dc++ {
			for dr := -1; dr <= 1; dr++ {
				if dc == 0 && dr == 0 || terrain.isMoveBlocked(node.col, node.row, dc, dr) {
					continue
				}
				next := [2]int{node.col + dc, node.row + dr}
				if next == avoid || danger.has(next[0], next[1]) || !scratch.visit(next[0], next[1]) {
					continue
				}
				distance := node.distance + 1
				scratch.queue = append(scratch.queue, bfsNode{col: next[0], row: next[1], distance: distance})
				if distance < 3 {
					continue
				}
				deltaX, deltaY := next[0]-start[0], next[1]-start[1]
				forward := deltaX*failedDirection[0] + deltaY*failedDirection[1]
				lateral := absInt(deltaX*failedDirection[1] - deltaY*failedDirection[0])
				score := float64(forward*2 + lateral*3 + distance)
				if score > bestScore {
					bestScore = score
					best = next
					found = true
				}
			}
		}
	}
	return [2]float64{float64(best[0]), float64(best[1])}, found
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
