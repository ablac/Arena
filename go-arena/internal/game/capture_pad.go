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
	NextControlPulseTick int
	ContenderCount       int
	Contested            bool
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
		if pad.CooldownUntilTick > 0 && pad.CooldownUntilTick <= tickCount {
			pad.CooldownUntilTick = 0
			pad.ProgressTicks = 0
			pad.CapturingBotID = ""
			pad.LastCapturedBy = ""
			pad.NextControlPulseTick = 0
		}

		contenders := capturePadContenders(*pad, bots)
		pad.ContenderCount = len(contenders)
		pad.Contested = len(contenders) > 1
		if pad.CooldownUntilTick > tickCount {
			maybeApplyCapturePadControlPulse(pad, contenders, tickCount)
			continue
		}
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
			progressStep := capturePadProgressPerTick(contender)
			if progressStep < 1 {
				progressStep = 1
			}
			if pad.CapturingBotID == "" || pad.CapturingBotID == contender.BotID {
				pad.CapturingBotID = contender.BotID
				pad.ProgressTicks += progressStep
			} else {
				pad.ProgressTicks -= progressStep
				if pad.ProgressTicks <= 0 {
					pad.ProgressTicks = progressStep
					pad.CapturingBotID = contender.BotID
				}
			}
			if pad.ProgressTicks >= pad.CaptureTicksRequired {
				pad.ProgressTicks = pad.CaptureTicksRequired
				pad.LastCapturedBy = contender.BotID
				pad.CooldownUntilTick = tickCount + config.C.CapturePadCooldownTicks
				pad.NextControlPulseTick = tickCount + config.C.CapturePadControlPulseTicks
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

func capturePadProgressPerTick(bot *BotState) int {
	if bot == nil {
		return 1
	}
	progress := 1
	if hasEffectByName(bot.ActiveEffects, "hazard_key") {
		progress *= 2
	}
	if bonus := effectValueByName(bot.ActiveEffects, "relay_battery"); bonus > 0 {
		progress += int(bonus)
	}
	if progress < 1 {
		return 1
	}
	return progress
}

func applyCapturePadReward(bot *BotState) {
	if bot == nil {
		return
	}
	bot.Elo = ClampElo(bot.Elo + config.C.CapturePadScoreBonus)
	bot.ShieldAbsorb += config.C.CapturePadShieldBonus
	bot.ActiveEffects = removeEffectByName(bot.ActiveEffects, "capture_pad_power")
	bot.ActiveEffects = append(bot.ActiveEffects, Effect{
		Name:           "capture_pad_power",
		RemainingTicks: config.C.CapturePadEffectTicks,
		Value:          config.C.CapturePadDamageBoostMult,
	})
}

func maybeApplyCapturePadControlPulse(pad *CapturePad, contenders []*BotState, tickCount int) {
	if pad == nil || pad.LastCapturedBy == "" || pad.NextControlPulseTick <= 0 || tickCount < pad.NextControlPulseTick {
		return
	}
	if len(contenders) != 1 {
		return
	}
	holder := contenders[0]
	if holder == nil || !holder.IsAlive || holder.BotID != pad.LastCapturedBy {
		return
	}
	holder.Elo = ClampElo(holder.Elo + config.C.CapturePadControlPulseScore)
	holder.ShieldAbsorb += config.C.CapturePadControlPulseShield
	pad.NextControlPulseTick = tickCount + config.C.CapturePadControlPulseTicks
}

// CapturePadView is the typed protocol view of a capture pad. Position is
// grid coordinates ([2]int) for bots/REST and world coordinates (Vec2) for
// spectators, matching the useGridPos flag of BuildCapturePadView.
type CapturePadView struct {
	Type                   string `json:"type"`
	ID                     string `json:"id"`
	Radius                 int    `json:"radius"`
	CaptureTicks           int    `json:"capture_ticks"`
	ProgressTicks          int    `json:"progress_ticks"`
	CapturingBotID         string `json:"capturing_bot_id"`
	OwnerID                string `json:"owner_id"`
	ContenderCount         int    `json:"contender_count"`
	IsContested            bool   `json:"is_contested"`
	IsReady                bool   `json:"is_ready"`
	CooldownRemainingTicks int    `json:"cooldown_remaining_ticks"`
	NextControlPulseTicks  int    `json:"next_control_pulse_ticks"`
	Position               any    `json:"position"`
}

// BuildCapturePadView creates a protocol-compatible view of a capture pad.
func BuildCapturePadView(pad CapturePad, tickCount int, useGridPos bool) CapturePadView {
	remaining := 0
	if pad.CooldownUntilTick > tickCount {
		remaining = pad.CooldownUntilTick - tickCount
	}
	view := CapturePadView{
		Type:                   "capture_pad",
		ID:                     pad.ID,
		Radius:                 pad.Radius,
		CaptureTicks:           pad.CaptureTicksRequired,
		ProgressTicks:          pad.ProgressTicks,
		CapturingBotID:         pad.CapturingBotID,
		OwnerID:                pad.LastCapturedBy,
		ContenderCount:         pad.ContenderCount,
		IsContested:            pad.Contested,
		IsReady:                remaining == 0,
		CooldownRemainingTicks: remaining,
		NextControlPulseTicks:  max(0, pad.NextControlPulseTick-tickCount),
	}
	if useGridPos {
		view.Position = posToGrid(pad.Position)
	} else {
		view.Position = pad.Position
	}
	return view
}
