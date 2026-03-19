package game

import (
	"arena-server/internal/config"

	"github.com/google/uuid"
)

// GravityWell is a deployable vortex that pulls nearby bots toward its center.
type GravityWell struct {
	ID             string
	OwnerID        string
	Position       Vec2
	RemainingTicks int
	PullRadius     int     // grid tiles
	PullForce      float64 // cells per tick to pull
}

// CreateGravityWell creates a new gravity well at the target position.
func CreateGravityWell(ownerID string, position Vec2) *GravityWell {
	c := &config.C
	return &GravityWell{
		ID:             uuid.New().String(),
		OwnerID:        ownerID,
		Position:       position,
		RemainingTicks: c.GravityWellDurationTicks,
		PullRadius:     c.GravityWellPullRadius,
		PullForce:      c.GravityWellPullForce,
	}
}

// UpdateGravityWells ticks all active gravity wells: pulls nearby bots toward
// center and removes expired wells.
func UpdateGravityWells(wells *[]GravityWell, bots map[string]*BotState, grid *SpatialGrid) {
	active := (*wells)[:0]

	for i := range *wells {
		well := &(*wells)[i]
		well.RemainingTicks--

		if well.RemainingTicks <= 0 {
			continue // expired
		}

		// Pull bots within radius toward center (except owner).
		for _, bot := range bots {
			if !bot.IsAlive || bot.BotID == well.OwnerID {
				continue
			}

			if !IsInRange(bot.Position, well.Position, well.PullRadius) {
				continue
			}

			// Pull toward center by PullForce cells.
			if ActiveTerrain != nil {
				botCell := ActiveTerrain.WorldToGrid(bot.Position)
				wellCell := ActiveTerrain.WorldToGrid(well.Position)

				dx := 0
				if wellCell[0] > botCell[0] {
					dx = 1
				} else if wellCell[0] < botCell[0] {
					dx = -1
				}
				dy := 0
				if wellCell[1] > botCell[1] {
					dy = 1
				} else if wellCell[1] < botCell[1] {
					dy = -1
				}

				if dx == 0 && dy == 0 {
					continue // already at center
				}

				// Only move if destination isn't blocked.
				if !ActiveTerrain.IsMoveBlocked(botCell[0], botCell[1], dx, dy) {
					newCell := [2]int{botCell[0] + dx, botCell[1] + dy}
					bot.Position = ActiveTerrain.GridToWorld(newCell)
					bot.LastValidPosition = bot.Position
					grid.Update(bot.BotID, bot.Position)
				}
			} else {
				// Float-based fallback: pull toward center.
				dir := well.Position.Sub(bot.Position).Normalized()
				pullDist := well.PullForce * config.C.PathfindingCellSize
				bot.Position = bot.Position.Add(dir.Scale(pullDist))
				bot.LastValidPosition = bot.Position
				grid.Update(bot.BotID, bot.Position)
			}
		}

		active = append(active, *well)
	}

	*wells = active
}

// BuildGravityWellView creates a protocol-compatible view of a gravity well.
func BuildGravityWellView(well GravityWell, useGridPos bool) map[string]interface{} {
	view := map[string]interface{}{
		"type":            "gravity_well",
		"id":              well.ID,
		"owner_id":        well.OwnerID,
		"remaining_ticks": well.RemainingTicks,
		"pull_radius":     well.PullRadius,
	}
	if useGridPos {
		gridPos := posToGrid(well.Position)
		view["position"] = [2]int{gridPos[0], gridPos[1]}
	} else {
		view["position"] = well.Position
	}
	return view
}
