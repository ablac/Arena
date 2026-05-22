package game

import (
	"math/rand"

	"arena-server/internal/config"

	"github.com/google/uuid"
)

// CapturePad is a neutral round objective that can be captured by standing on
// it uncontested for a short duration.
type CapturePad struct {
	ID                   string
	Position             Vec2
	Radius               int
	CaptureTicksRequired int
	ProgressTicks        int
	CapturingBotID       string
	LastCapturedBy       string
	CooldownUntilTick    int
}

// SpawnCapturePads creates objective pads at valid terrain positions.
func SpawnCapturePads(arena *ArenaMap, count int) []CapturePad {
	if count <= 0 {
		return nil
	}
	pads := make([]CapturePad, 0, count)
	for i := 0; i < count; i++ {
		pos := findValidCapturePadPosition(arena, pads)
		pads = append(pads, CapturePad{
			ID:                   uuid.New().String(),
			Position:             pos,
			Radius:               config.C.CapturePadRadius,
			CaptureTicksRequired: config.C.CapturePadCaptureTicks,
		})
	}
	return pads
}

func findValidCapturePadPosition(arena *ArenaMap, existing []CapturePad) Vec2 {
	for i := 0; i < 120; i++ {
		pos := arena.GetSpawnPoint()
		if ActiveTerrain != nil {
			cell := ActiveTerrain.WorldToGrid(pos)
			if ActiveTerrain.IsBlocked(cell[0], cell[1]) {
				continue
			}
			pos = ActiveTerrain.GridToWorld(cell)
		}
		tooClose := false
		for _, pad := range existing {
			if pos.DistanceTo(pad.Position) < 260 {
				tooClose = true
				break
			}
		}
		if tooClose {
			continue
		}
		return pos
	}
	return arena.ClampToArena(arena.ZoneCenter.Add(NewVec2(rand.Float64()*120-60, rand.Float64()*120-60)))
}

// UpdateCapturePads advances objective state and returns spectator events for
// successful captures.
func UpdateCapturePads(pads []CapturePad, bots map[string]*BotState, tickCount int) []ArenaEvent {
	if len(pads) == 0 {
		return nil
	}

	var events []ArenaEvent
	for i := range pads {
		pad := &pads[i]

		if pad.CooldownUntilTick > tickCount {
			continue
		}
		if pad.CooldownUntilTick > 0 && pad.CooldownUntilTick <= tickCount {
			pad.CooldownUntilTick = 0
			pad.ProgressTicks = 0
			pad.CapturingBotID = ""
			pad.LastCapturedBy = ""
		}

		contenders := capturePadContenders(*pad, bots)
		switch len(contenders) {
		case 0:
			if pad.ProgressTicks > 0 {
				pad.ProgressTicks--
				if pad.ProgressTicks == 0 {
					pad.CapturingBotID = ""
				}
			}
			continue
		case 1:
			contender := contenders[0]
			if pad.CapturingBotID == "" || pad.CapturingBotID == contender.BotID {
				pad.CapturingBotID = contender.BotID
				pad.ProgressTicks++
			} else {
				pad.ProgressTicks--
				if pad.ProgressTicks <= 0 {
					pad.ProgressTicks = 1
					pad.CapturingBotID = contender.BotID
				}
			}
			if pad.ProgressTicks >= pad.CaptureTicksRequired {
				pad.ProgressTicks = pad.CaptureTicksRequired
				pad.LastCapturedBy = contender.BotID
				pad.CooldownUntilTick = tickCount + config.C.CapturePadCooldownTicks
				applyCapturePadReward(contender)
				events = append(events, buildCapturePadCaptureEvent(*pad, contender, tickCount))
			}
		default:
			// Contested pads do not progress.
			continue
		}
	}

	return events
}

func capturePadContenders(pad CapturePad, bots map[string]*BotState) []*BotState {
	contenders := make([]*BotState, 0, 3)
	for _, bot := range bots {
		if !bot.IsAlive {
			continue
		}
		if IsInRange(bot.Position, pad.Position, pad.Radius) {
			contenders = append(contenders, bot)
		}
	}
	return contenders
}

func applyCapturePadReward(bot *BotState) {
	if bot == nil {
		return
	}
	bot.Elo += config.C.CapturePadScoreBonus
	bot.ShieldAbsorb += config.C.CapturePadShieldBonus
	bot.ActiveEffects = removeEffectByName(bot.ActiveEffects, "capture_pad_power")
	bot.ActiveEffects = append(bot.ActiveEffects, Effect{
		Name:           "capture_pad_power",
		RemainingTicks: config.C.CapturePadEffectTicks,
		Value:          config.C.CapturePadDamageBoostMult,
	})
}

// BuildCapturePadView creates a protocol-compatible view of a capture pad.
func BuildCapturePadView(pad CapturePad, tickCount int, useGridPos bool) map[string]interface{} {
	remaining := 0
	if pad.CooldownUntilTick > tickCount {
		remaining = pad.CooldownUntilTick - tickCount
	}
	view := map[string]interface{}{
		"type":                     "capture_pad",
		"id":                       pad.ID,
		"radius":                   pad.Radius,
		"capture_ticks":            pad.CaptureTicksRequired,
		"progress_ticks":           pad.ProgressTicks,
		"capturing_bot_id":         pad.CapturingBotID,
		"owner_id":                 pad.LastCapturedBy,
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
