package game

import (
	"math/rand"

	"arena-server/internal/config"
)

// SuddenDeathSystem tracks void tiles that appear after the zone reaches
// minimum, plus the overtime pressure that forces stalled rounds to resolve:
// all damage is multiplied while active, and if no bot deals damage for a
// configured window every living bot starts taking ramping stall damage.
type SuddenDeathSystem struct {
	Active    bool
	VoidTiles map[[2]int]bool // grid cells marked as void

	// StallActive is true while the no-combat window has been exceeded and
	// rapid stall damage is being applied. It switches back off as soon as
	// any bot deals damage again.
	StallActive bool

	stallTicks       int     // ticks since a bot last dealt damage
	lastCombatDamage float64 // sum of RoundDamageDealt at the last stall check
	combatDamageSeen bool    // lastCombatDamage has been initialised
}

// ActiveSuddenDeath mirrors the engine's sudden-death system so package-level
// damage helpers can read the active damage multiplier without an engine
// reference (same pattern as ActiveTerrain / ActiveModeRules).
var ActiveSuddenDeath *SuddenDeathSystem

// SuddenDeathDamageMultiplier returns the global damage multiplier: the
// configured sudden-death factor while sudden death is active, 1 otherwise.
func SuddenDeathDamageMultiplier() float64 {
	if ActiveSuddenDeath != nil && ActiveSuddenDeath.Active {
		if m := config.C.SuddenDeathDamageMult; m > 0 {
			return m
		}
	}
	return 1
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
	sd.StallActive = false
	sd.stallTicks = 0
	sd.lastCombatDamage = 0
	sd.combatDamageSeen = false
}

// CheckActivation checks if sudden death should activate. It fires when the
// zone reaches minimum radius, or when the round clock expires while the fight
// is unresolved (on default-size maps the shrink schedule needs slightly
// longer than the round duration, so without the clock trigger sudden death
// would almost never happen). Returns true on the tick it first activates.
func (sd *SuddenDeathSystem) CheckActivation(arena *ArenaMap, elapsedTicks, roundDurationTicks int) bool {
	if sd.Active {
		return false
	}
	if arena.ZoneRadius <= arena.MinRadius+1 || (roundDurationTicks > 0 && elapsedTicks >= roundDurationTicks) {
		sd.Active = true
		return true
	}
	return false
}

// UpdateStall drives the anti-stall pressure while sudden death is active.
// Combat is detected through the total damage dealt by bots (RoundDamageDealt
// only grows from attacker-attributed hits, so zone/stall/void damage never
// resets the window). After SuddenDeathStallSeconds without combat, every
// living bot takes SuddenDeathStallDamage per tick, ramping up by another
// step every SuddenDeathStallRampSeconds until someone lands a hit.
func (sd *SuddenDeathSystem) UpdateStall(bots map[string]*BotState) {
	if !sd.Active {
		return
	}
	c := &config.C

	total := 0.0
	for _, bot := range bots {
		total += bot.RoundDamageDealt
	}
	if !sd.combatDamageSeen || total > sd.lastCombatDamage {
		sd.lastCombatDamage = total
		sd.combatDamageSeen = true
		sd.stallTicks = 0
		sd.StallActive = false
		return
	}
	if total < sd.lastCombatDamage {
		// A bot disconnected and took its dealt-damage total with it.
		// Re-baseline without resetting the stall window, otherwise later
		// combat could go unnoticed until the sum catches back up.
		sd.lastCombatDamage = total
	}

	sd.stallTicks++
	stallThreshold := int(c.SuddenDeathStallSeconds * float64(c.TickRate))
	if stallThreshold <= 0 || sd.stallTicks < stallThreshold {
		return
	}
	sd.StallActive = true

	// Linear ramp: one extra damage step per ramp interval of continued stall.
	damage := c.SuddenDeathStallDamage
	if rampTicks := int(c.SuddenDeathStallRampSeconds * float64(c.TickRate)); rampTicks > 0 {
		steps := (sd.stallTicks - stallThreshold) / rampTicks
		damage += c.SuddenDeathStallDamage * float64(steps)
	}
	if damage <= 0 {
		return
	}

	// Environmental pressure, like zone damage: ignores invulnerability so
	// dodge spam cannot stall the round indefinitely.
	for _, bot := range bots {
		if !bot.IsAlive {
			continue
		}
		bot.HP -= damage
		bot.RoundDamageTaken += damage
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

	// Sample within the zone's grid-space bounding box. Sampling the whole
	// grid makes the hit rate collapse once the zone is small, silently
	// destroying far fewer tiles per tick than configured.
	minCell := terrain.WorldToGrid(NewVec2(arena.ZoneCenter.X()-arena.ZoneRadius, arena.ZoneCenter.Y()-arena.ZoneRadius))
	maxCell := terrain.WorldToGrid(NewVec2(arena.ZoneCenter.X()+arena.ZoneRadius, arena.ZoneCenter.Y()+arena.ZoneRadius))
	spanX := maxCell[0] - minCell[0] + 1
	spanY := maxCell[1] - minCell[1] + 1

	for i := 0; i < tilesPerTick; i++ {
		// Pick a random cell within the zone
		for attempt := 0; attempt < 20; attempt++ {
			col := minCell[0] + rand.Intn(spanX)
			row := minCell[1] + rand.Intn(spanY)
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
		if !bot.IsAlive || bot.InvulnTicks > 0 {
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

// VoidTilesNear returns void tiles within radius (Chebyshev grid distance) of
// the given cell. Used to keep per-bot tick payloads small: bots only receive
// void tiles inside their fog radius.
func (sd *SuddenDeathSystem) VoidTilesNear(cell [2]int, radius int) [][2]int {
	tiles := make([][2]int, 0, 16)
	for c := range sd.VoidTiles {
		if GridDistance(cell, c) <= radius {
			tiles = append(tiles, c)
		}
	}
	return tiles
}
