package game

import (
	"math/rand"

	"arena-server/internal/config"

	"github.com/google/uuid"
)

// HazardZone represents a pulsing damage zone on the arena floor.
type HazardZone struct {
	ID            string
	Position      Vec2 // center position
	Width         int  // grid tiles
	Height        int  // grid tiles
	DamagePerTick float64
	Active        bool
	PulseOnTicks  int
	PulseOffTicks int
	TickCounter   int
}

// SpawnHazardZones creates hazard zones at valid terrain positions. The
// whole zone rectangle must sit on open terrain — validating only the centre
// let hazard overlays draw on top of wall geometry on carved maps.
func SpawnHazardZones(arena *ArenaMap, count int) []HazardZone {
	c := &config.C
	var zones []HazardZone

	for i := 0; i < count; i++ {
		w := c.HazardMinWidth + rand.Intn(c.HazardMaxWidth-c.HazardMinWidth+1)
		h := c.HazardMinWidth + rand.Intn(c.HazardMaxWidth-c.HazardMinWidth+1)

		var pos Vec2
		placed := false
		for attempt := 0; attempt < 20; attempt++ {
			pos = arena.GetSpawnPoint()
			if ActiveTerrain == nil {
				placed = true
				break
			}
			cell := ActiveTerrain.WorldToGrid(pos)
			pos = ActiveTerrain.GridToWorld(cell)
			if hazardRectOpen(cell, w, h) {
				placed = true
				break
			}
		}
		if !placed {
			// Cramped map: shrink to a single open cell rather than draping
			// the zone over walls.
			w, h = 1, 1
		}

		zones = append(zones, HazardZone{
			ID:            uuid.New().String(),
			Position:      pos,
			Width:         w,
			Height:        h,
			DamagePerTick: c.HazardDamagePerTick,
			Active:        true,
			PulseOnTicks:  c.HazardPulseOnTicks,
			PulseOffTicks: c.HazardPulseOffTicks,
			TickCounter:   0,
		})
	}

	return zones
}

// hazardRectOpen reports whether every grid cell covered by a hazard zone of
// w x h tiles centred on cell is open terrain.
func hazardRectOpen(cell [2]int, w, h int) bool {
	halfW, halfH := w/2, h/2
	for cx := cell[0] - halfW; cx <= cell[0]+halfW; cx++ {
		for cy := cell[1] - halfH; cy <= cell[1]+halfH; cy++ {
			if ActiveTerrain.IsBlocked(cx, cy) {
				return false
			}
		}
	}
	return true
}

// UpdateHazards ticks all hazard zones, toggling their active state based on
// pulse timing, and applies damage to bots standing in active zones.
func UpdateHazards(zones []HazardZone, bots map[string]*BotState, tickCount int, mod RoundModifier) {
	for i := range zones {
		zone := &zones[i]
		onTicks, offTicks, damagePerTick := effectiveHazardProfile(mod, *zone)
		zone.TickCounter++

		if zone.Active {
			if zone.TickCounter >= onTicks {
				zone.Active = false
				zone.TickCounter = 0
			}
		} else {
			if zone.TickCounter >= offTicks {
				zone.Active = true
				zone.TickCounter = 0
			}
		}

		if !zone.Active {
			continue
		}

		// Apply damage to bots in the zone.
		for _, bot := range bots {
			if !bot.IsAlive {
				continue
			}
			if bot.TeleportHazardGraceTicks > 0 {
				continue
			}
			if hasEffectByName(bot.ActiveEffects, "hazard_key") {
				continue
			}
			if bot.InvulnTicks > 0 {
				continue
			}
			if botMovementIntersectsHazard(bot, zone) {
				dmg := damagePerTick * SuddenDeathDamageMultiplier()
				bot.HP -= dmg
				bot.RoundDamageTaken += dmg
			}
		}
	}
}

func botMovementIntersectsHazard(bot *BotState, zone *HazardZone) bool {
	if bot == nil {
		return false
	}
	for _, entered := range bot.MovementTrace {
		if isBotInHazardZone(entered, zone) {
			return true
		}
	}
	return isBotInHazardZone(bot.Position, zone)
}

// isBotInHazardZone checks if a bot position is within the rectangular hazard zone.
func isBotInHazardZone(pos Vec2, zone *HazardZone) bool {
	if ActiveTerrain == nil {
		return false
	}
	botCell := ActiveTerrain.WorldToGrid(pos)
	zoneCell := ActiveTerrain.WorldToGrid(zone.Position)

	// Zone is centered at zoneCell, extending Width/2 and Height/2 in each direction.
	halfW := zone.Width / 2
	halfH := zone.Height / 2

	return botCell[0] >= zoneCell[0]-halfW && botCell[0] <= zoneCell[0]+halfW &&
		botCell[1] >= zoneCell[1]-halfH && botCell[1] <= zoneCell[1]+halfH
}

// BuildHazardZoneView creates a protocol-compatible view of a hazard zone.
func BuildHazardZoneView(zone HazardZone, useGridPos bool, mod RoundModifier) map[string]interface{} {
	onTicks, offTicks, damagePerTick := effectiveHazardProfile(mod, zone)
	view := map[string]interface{}{
		"type": "hazard_zone",
		"id":   zone.ID,
		// isBotInHazardZone/hazardRectOpen both extend zone.Width/2 (integer
		// division) tiles in each direction from center, i.e. a span of
		// 2*(Width/2)+1 tiles - one tile wider than Width itself whenever
		// Width is even. Report the true span so a bot computing a safe
		// standing distance from these fields doesn't still take damage
		// just outside what it was told is the zone.
		"width":           2*(zone.Width/2) + 1,
		"height":          2*(zone.Height/2) + 1,
		"active":          zone.Active,
		"on_ticks":        onTicks,
		"off_ticks":       offTicks,
		"tick_counter":    zone.TickCounter,
		"damage_per_tick": damagePerTick,
	}
	if useGridPos {
		gridPos := posToGrid(zone.Position)
		view["position"] = [2]int{gridPos[0], gridPos[1]}
	} else {
		view["position"] = zone.Position
	}
	return view
}
