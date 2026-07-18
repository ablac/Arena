package game

import (
	"arena-server/internal/config"

	"github.com/google/uuid"
)

// Landmine represents a player-placed mine on the arena floor.
type Landmine struct {
	ID          string
	OwnerID     string
	Position    Vec2
	Damage      float64
	BlastRadius int
	Armed       bool // becomes true after arm delay
	ArmTick     int  // tick at which it becomes armed
}

// PlaceMine creates a new landmine at the bot's current position.
// Returns nil if the bot has reached max mines.
func PlaceMine(bot *BotState, mines *[]Landmine, tickCount int) *Landmine {
	c := &config.C

	// Count existing mines for this bot.
	count := 0
	for _, m := range *mines {
		if m.OwnerID == bot.BotID {
			count++
		}
	}
	if count >= c.MineMaxPerBot {
		return nil
	}

	mine := Landmine{
		ID:          uuid.New().String(),
		OwnerID:     bot.BotID,
		Position:    bot.Position,
		Damage:      c.MineDamage,
		BlastRadius: c.MineBlastRadius,
		Armed:       false,
		ArmTick:     tickCount + c.MineArmDelayTicks,
	}
	*mines = append(*mines, mine)
	bot.MineCount++
	return &mine
}

// UpdateMines arms mines that have passed their delay, and detonates mines
// when an enemy bot walks within blast radius. Returns spectator events for
// detonations so the client can render explicit blast feedback.
func UpdateMines(mines *[]Landmine, bots map[string]*BotState, tickCount int) []ArenaEvent {
	var events []ArenaEvent

	active := (*mines)[:0]
	for i := range *mines {
		mine := &(*mines)[i]
		owner, ownerPresent := bots[mine.OwnerID]
		if !ownerPresent {
			// Mines cannot apply team rules or award legitimate attribution once
			// their owner has left the match, so discard orphaned mines.
			continue
		}

		// Arm the mine after delay.
		if !mine.Armed && tickCount >= mine.ArmTick {
			mine.Armed = true
		}

		if !mine.Armed {
			active = append(active, *mine)
			continue
		}

		// Check if any enemy bot is in blast radius. An invulnerable
		// (dodging) bot must not be able to trigger an enemy's mine at all -
		// matching hazard zones, which skip invulnerable bots entirely
		// rather than merely exempting them from damage - otherwise a
		// dodging bot could walk through a mine, set it off, and splash
		// damage onto a bystander while taking none itself.
		triggered := false
		for _, bot := range bots {
			if !bot.IsAlive || bot.BotID == mine.OwnerID || bot.InvulnTicks > 0 || !ActiveModeRules.CanDamage(owner, bot) {
				continue
			}
			if _, enteredRange := firstMovementPositionInRange(bot, mine.Position, mine.BlastRadius); enteredRange {
				// Detonate: deal damage to all bots in blast radius (except
				// owner). Routed through ApplyDamage (not a manual HP
				// subtraction) so a mine blast is soaked by ShieldAbsorb and
				// the shield weapon's passive reduction the same as every
				// other damage source - it already handles the sudden-death
				// multiplier, invulnerability, and attribution bookkeeping.
				for _, target := range bots {
					if !target.IsAlive || target.BotID == mine.OwnerID || !ActiveModeRules.CanDamage(owner, target) {
						continue
					}
					if _, enteredRange := firstMovementPositionInRange(target, mine.Position, mine.BlastRadius); enteredRange {
						ApplyDamage(target, owner, mine.Damage, "landmine", tickCount)
					}
				}
				triggered = true
				events = append(events, buildMineDetonationEvent(*mine, tickCount))

				// Decrement owner's mine count.
				owner.MineCount--
				if owner.MineCount < 0 {
					owner.MineCount = 0
				}
				break
			}
		}

		if !triggered {
			active = append(active, *mine)
		}
	}

	*mines = active
	return events
}

// MineView is the typed protocol view of a landmine. Position is grid
// coordinates ([2]int) for bots and world coordinates (Vec2) for spectators,
// matching the useGridPos flag of BuildMineView.
type MineView struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	OwnerID  string `json:"owner_id"`
	Armed    bool   `json:"armed"`
	Position any    `json:"position"`
}

// BuildMineView creates a protocol-compatible view of a landmine.
// For spectators, all mines are visible. For bots, only their own.
func BuildMineView(mine Landmine, useGridPos bool) MineView {
	view := MineView{
		Type:    "landmine",
		ID:      mine.ID,
		OwnerID: mine.OwnerID,
		Armed:   mine.Armed,
	}
	if useGridPos {
		view.Position = posToGrid(mine.Position)
	} else {
		view.Position = mine.Position
	}
	return view
}
