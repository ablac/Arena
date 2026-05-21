package game

import (
	"math/rand"

	"arena-server/internal/config"

	"github.com/google/uuid"
)

// TeleportPad represents one end of a linked teleport pad pair.
type TeleportPad struct {
	ID                string
	Position          Vec2
	LinkedPadID       string
	Color             string // paired pads share a color
	CooldownUntilTick int
}

// SpawnTeleportPads creates linked pairs of teleport pads at valid terrain positions.
func SpawnTeleportPads(arena *ArenaMap, count int) []TeleportPad {
	colors := []string{"#00ffff", "#ff00ff", "#ffff00", "#00ff00", "#ff8800", "#8800ff"}
	var pads []TeleportPad

	for i := 0; i < count; i++ {
		// Spawn two pads for each pair.
		posA := findValidPadPosition(arena)
		posB := findValidPadPosition(arena)

		// Ensure pads aren't too close to each other.
		for attempts := 0; attempts < 20 && posA.DistanceTo(posB) < 200; attempts++ {
			posB = findValidPadPosition(arena)
		}

		idA := uuid.New().String()
		idB := uuid.New().String()
		color := colors[i%len(colors)]

		pads = append(pads, TeleportPad{
			ID:          idA,
			Position:    posA,
			LinkedPadID: idB,
			Color:       color,
		})
		pads = append(pads, TeleportPad{
			ID:          idB,
			Position:    posB,
			LinkedPadID: idA,
			Color:       color,
		})
	}

	return pads
}

// findValidPadPosition finds a random position inside the arena that isn't blocked.
func findValidPadPosition(arena *ArenaMap) Vec2 {
	for i := 0; i < 100; i++ {
		pos := arena.GetSpawnPoint()
		if ActiveTerrain != nil {
			cell := ActiveTerrain.WorldToGrid(pos)
			if !ActiveTerrain.IsBlocked(cell[0], cell[1]) {
				return ActiveTerrain.GridToWorld(cell)
			}
		} else {
			return pos
		}
	}
	return arena.ClampToArena(arena.ZoneCenter)
}

// ProcessTeleports checks if any alive bot is standing on a teleport pad and
// teleports them to the linked pad. Respects per-bot cooldowns and returns
// transient spectator events for visible teleport bursts.
func ProcessTeleports(bots map[string]*BotState, pads []TeleportPad, grid *SpatialGrid, tickCount int) []ArenaEvent {
	if len(pads) == 0 {
		return nil
	}

	collectRadius := config.C.TeleportCollectRadius
	cooldownTicks := config.C.TeleportCooldownTicks
	lockTicks := config.C.TeleportPadLockTicks
	var events []ArenaEvent

	// Build a lookup of pad ID -> pad.
	padMap := make(map[string]*TeleportPad, len(pads))
	for i := range pads {
		padMap[pads[i].ID] = &pads[i]
	}

	for _, bot := range bots {
		if !bot.IsAlive {
			continue
		}
		if bot.TeleportTouchedPads == nil {
			bot.TeleportTouchedPads = make(map[string]bool)
		}
		touchedNow := make(map[string]bool)
		teleported := false

		for i := range pads {
			pad := &pads[i]
			if !IsInRange(bot.Position, pad.Position, collectRadius) {
				continue
			}
			touchedNow[pad.ID] = true
			if bot.TeleportTouchedPads[pad.ID] {
				continue
			}
			if pad.CooldownUntilTick > tickCount {
				continue
			}

			// Check per-bot cooldown for this pad.
			if bot.TeleportCooldowns != nil {
				if expiry, ok := bot.TeleportCooldowns[pad.ID]; ok && tickCount < expiry {
					continue
				}
			}

			// Find the linked pad.
			linked, ok := padMap[pad.LinkedPadID]
			if !ok {
				continue
			}
			if linked.CooldownUntilTick > tickCount {
				continue
			}

			// Teleport the bot.
			from := bot.Position
			grid.Remove(bot.BotID)
			bot.Position = linked.Position
			bot.LastValidPosition = linked.Position
			bot.TeleportHazardGraceTicks = config.C.TeleportHazardGraceTicks
			grid.Insert(bot.BotID, bot.Position)
			events = append(events, buildTeleportEvent(bot, *pad, from, linked.Position, tickCount))

			// Set cooldown on BOTH pads for this bot.
			if bot.TeleportCooldowns == nil {
				bot.TeleportCooldowns = make(map[string]int)
			}
			bot.TeleportCooldowns[pad.ID] = tickCount + cooldownTicks
			bot.TeleportCooldowns[linked.ID] = tickCount + cooldownTicks
			pad.CooldownUntilTick = tickCount + lockTicks
			linked.CooldownUntilTick = tickCount + lockTicks
			touchedNow[pad.ID] = true
			touchedNow[linked.ID] = true

			// Only teleport once per tick per bot.
			teleported = true
			break
		}

		if teleported {
			bot.TeleportTouchedPads = touchedNow
			continue
		}
		bot.TeleportTouchedPads = touchedNow
	}
	return events
}

// BuildTeleportPadView creates a protocol-compatible view of a teleport pad.
func BuildTeleportPadView(pad TeleportPad, tickCount int, useGridPos bool) map[string]interface{} {
	remaining := 0
	if pad.CooldownUntilTick > tickCount {
		remaining = pad.CooldownUntilTick - tickCount
	}
	view := map[string]interface{}{
		"type":                     "teleport_pad",
		"id":                       pad.ID,
		"linked_pad_id":            pad.LinkedPadID,
		"color":                    pad.Color,
		"is_ready":                 remaining == 0,
		"cooldown_remaining_ticks": remaining,
	}
	if useGridPos {
		gridPos := posToGrid(pad.Position)
		view["position"] = [2]int{gridPos[0], gridPos[1]}
	} else {
		view["position"] = pad.Position
	}
	return view
}

// ShufflePads randomizes pad slice order (for deterministic iteration avoidance).
func init() {
	_ = rand.Int // ensure rand is used
}
