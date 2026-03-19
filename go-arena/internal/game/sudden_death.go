package game

import (
	"math/rand"

	"arena-server/internal/config"
)

// SuddenDeathSystem tracks void tiles that appear after the zone reaches minimum.
type SuddenDeathSystem struct {
	Active    bool
	VoidTiles map[[2]int]bool // grid cells marked as void
}

// NewSuddenDeathSystem creates an inactive sudden death system.
func NewSuddenDeathSystem() *SuddenDeathSystem {
	return &SuddenDeathSystem{
		VoidTiles: make(map[[2]int]bool),
	}
}

// Clear resets the system for a new round.
func (sd *SuddenDeathSystem) Clear() {
	sd.Active = false
	sd.VoidTiles = make(map[[2]int]bool)
}

// CheckActivation checks if sudden death should activate (zone at min radius).
func (sd *SuddenDeathSystem) CheckActivation(arena *ArenaMap) {
	if sd.Active {
		return
	}
	if arena.ZoneRadius <= arena.MinRadius+1 {
		sd.Active = true
	}
}

// Update removes random floor tiles and damages bots on void tiles.
// Returns newly created void tiles for broadcast.
func (sd *SuddenDeathSystem) Update(bots map[string]*BotState, arena *ArenaMap) [][2]int {
	if !sd.Active {
		return nil
	}

	c := &config.C
	terrain := ActiveTerrain
	if terrain == nil {
		return nil
	}

	// Remove random floor tiles
	tilesPerTick := c.SuddenDeathTilesPerTick
	var newVoids [][2]int

	for i := 0; i < tilesPerTick; i++ {
		// Pick a random cell within the zone
		for attempt := 0; attempt < 20; attempt++ {
			col := rand.Intn(terrain.Width)
			row := rand.Intn(terrain.Height)
			cell := [2]int{col, row}

			// Skip if already void/wall
			if terrain.IsBlocked(col, row) || sd.VoidTiles[cell] {
				continue
			}

			// Only remove tiles within zone radius
			worldPos := terrain.GridToWorld(cell)
			if !arena.IsInZone(worldPos) {
				continue
			}

			sd.VoidTiles[cell] = true
			newVoids = append(newVoids, cell)
			break
		}
	}

	// Damage bots standing on void tiles
	for _, bot := range bots {
		if !bot.IsAlive {
			continue
		}
		cell := terrain.WorldToGrid(bot.Position)
		if sd.VoidTiles[cell] {
			bot.HP -= c.SuddenDeathDamage
			bot.RoundDamageTaken += c.SuddenDeathDamage
		}
	}

	return newVoids
}

// IsVoidTile returns true if the cell has been destroyed by sudden death.
func (sd *SuddenDeathSystem) IsVoidTile(col, row int) bool {
	return sd.VoidTiles[[2]int{col, row}]
}

// GetAllVoidTiles returns all void tile positions.
func (sd *SuddenDeathSystem) GetAllVoidTiles() [][2]int {
	tiles := make([][2]int, 0, len(sd.VoidTiles))
	for cell := range sd.VoidTiles {
		tiles = append(tiles, cell)
	}
	return tiles
}
