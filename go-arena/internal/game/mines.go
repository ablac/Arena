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

		// Check if any enemy bot is in blast radius.
		triggered := false
		for _, bot := range bots {
			if !bot.IsAlive || bot.BotID == mine.OwnerID || !ActiveModeRules.CanDamage(owner, bot) {
				continue
			}
			if _, enteredRange := firstMovementPositionInRange(bot, mine.Position, mine.BlastRadius); enteredRange {
				// Detonate: deal damage to all bots in blast radius (except owner).
				for _, target := range bots {
					if !target.IsAlive || target.BotID == mine.OwnerID || !ActiveModeRules.CanDamage(owner, target) {
						continue
					}
					if _, enteredRange := firstMovementPositionInRange(target, mine.Position, mine.BlastRadius); enteredRange {
						if target.InvulnTicks > 0 {
							continue
						}
						dmg := mine.Damage * SuddenDeathDamageMultiplier()
						target.HP -= dmg
						target.RoundDamageTaken += dmg
						owner.RoundDamageDealt += dmg
						recordAttributedDamage(target, owner, dmg, "landmine", tickCount)
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

// BuildMineView creates a protocol-compatible view of a landmine.
// For spectators, all mines are visible. For bots, only their own.
func BuildMineView(mine Landmine, useGridPos bool) map[string]interface{} {
	view := map[string]interface{}{
		"type":     "landmine",
		"id":       mine.ID,
		"owner_id": mine.OwnerID,
		"armed":    mine.Armed,
	}
	if useGridPos {
		gridPos := posToGrid(mine.Position)
		view["position"] = [2]int{gridPos[0], gridPos[1]}
	} else {
		view["position"] = mine.Position
	}
	return view
}
