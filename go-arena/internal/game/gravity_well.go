package game

import (
	"math"

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
	pullProgress   map[string]float64
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
		pullProgress:   make(map[string]float64),
	}
}

// UpdateGravityWells ticks all active gravity wells: pulls nearby bots toward
// center and removes expired wells.
func UpdateGravityWells(wells *[]GravityWell, bots map[string]*BotState, grid *SpatialGrid) {
	active := (*wells)[:0]

	for i := range *wells {
		well := &(*wells)[i]

		// Check expiry BEFORE decrementing: a well is created with
		// RemainingTicks = GravityWellDurationTicks (documented to bots as
		// "3 seconds", i.e. DurationTicks ticks) in the same tick's
		// processUseItems, which runs before this function. Decrementing
		// first would consume one tick of the well's life before it ever
		// got to pull anything, delivering only DurationTicks-1 ticks of
		// actual pull.
		if well.RemainingTicks <= 0 {
			continue // expired
		}
		well.RemainingTicks--

		owner := bots[well.OwnerID]
		// Pull vulnerable enemies within radius toward center.
		for _, bot := range bots {
			if !bot.IsAlive || bot.BotID == well.OwnerID || bot.InvulnTicks > 0 ||
				owner == nil || !ActiveModeRules.CanDamage(owner, bot) {
				continue
			}

			if !IsInRange(bot.Position, well.Position, well.PullRadius) {
				continue
			}

			if well.PullForce <= 0 {
				continue
			}

			// Pull toward center by PullForce cells. Grid movement keeps a
			// per-target fractional credit so values below one cell still retain
			// their configured average force without placing bots inside walls.
			if ActiveTerrain != nil {
				if well.pullProgress == nil {
					well.pullProgress = make(map[string]float64)
				}
				well.pullProgress[bot.BotID] += well.PullForce
				steps := int(math.Floor(well.pullProgress[bot.BotID]))
				if steps <= 0 {
					continue
				}
				well.pullProgress[bot.BotID] -= float64(steps)

				botCell := ActiveTerrain.WorldToGrid(bot.Position)
				wellCell := ActiveTerrain.WorldToGrid(well.Position)
				startCell := botCell
				for step := 0; step < steps; step++ {
					dx := movementSign(wellCell[0] - botCell[0])
					dy := movementSign(wellCell[1] - botCell[1])
					if dx == 0 && dy == 0 {
						break
					}
					if ActiveTerrain.IsMoveBlocked(botCell[0], botCell[1], dx, dy) {
						break
					}
					botCell = [2]int{botCell[0] + dx, botCell[1] + dy}
				}
				if botCell != startCell {
					bot.Position = ActiveTerrain.GridToWorld(botCell)
					bot.LastValidPosition = bot.Position
					grid.Update(bot.BotID, bot.Position)
				}
			} else {
				// Float-based fallback: pull toward center.
				delta := well.Position.Sub(bot.Position)
				dir := delta.Normalized()
				pullDist := well.PullForce * config.C.PathfindingCellSize
				if pullDist > delta.Length() {
					pullDist = delta.Length()
				}
				bot.Position = bot.Position.Add(dir.Scale(pullDist))
				bot.LastValidPosition = bot.Position
				grid.Update(bot.BotID, bot.Position)
			}
		}

		active = append(active, *well)
	}

	*wells = active
}

// GravityWellView is the typed protocol view of a gravity well. Position is
// grid coordinates ([2]int) for bots and world coordinates (Vec2) for
// spectators, matching the useGridPos flag of BuildGravityWellView.
type GravityWellView struct {
	Type           string `json:"type"`
	ID             string `json:"id"`
	OwnerID        string `json:"owner_id"`
	RemainingTicks int    `json:"remaining_ticks"`
	PullRadius     int    `json:"pull_radius"`
	Position       any    `json:"position"`
}

// BuildGravityWellView creates a protocol-compatible view of a gravity well.
func BuildGravityWellView(well GravityWell, useGridPos bool) GravityWellView {
	view := GravityWellView{
		Type:           "gravity_well",
		ID:             well.ID,
		OwnerID:        well.OwnerID,
		RemainingTicks: well.RemainingTicks,
		PullRadius:     well.PullRadius,
	}
	if useGridPos {
		view.Position = posToGrid(well.Position)
	} else {
		view.Position = well.Position
	}
	return view
}
