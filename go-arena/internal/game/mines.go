package game

import (
	"arena-server/internal/config"

	"github.com/google/uuid"
)

// Landmine represents a player-placed mine on the arena floor.
type Landmine struct {
	ID        string
	OwnerID   string
	Position  Vec2
	Damage    float64
	Armed     bool // becomes true after arm delay
	ArmTick   int  // tick at which it becomes armed
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
		ID:       uuid.New().String(),
		OwnerID:  bot.BotID,
		Position: bot.Position,
		Damage:   c.MineDamage,
		Armed:    false,
		ArmTick:  tickCount + c.MineArmDelayTicks,
	}
	*mines = append(*mines, mine)
	bot.MineCount++
	return &mine
}

// UpdateMines arms mines that have passed their delay, and detonates mines
// when an enemy bot walks within blast radius. Returns IDs of detonated mines.
func UpdateMines(mines *[]Landmine, bots map[string]*BotState, tickCount int) []string {
	c := &config.C
	var detonated []string

	active := (*mines)[:0]
	for i := range *mines {
		mine := &(*mines)[i]

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
			if !bot.IsAlive || bot.BotID == mine.OwnerID {
				continue
			}
			if IsInRange(bot.Position, mine.Position, c.MineBlastRadius) {
				// Detonate: deal damage to all bots in blast radius (except owner).
				for _, target := range bots {
					if !target.IsAlive || target.BotID == mine.OwnerID {
						continue
					}
					if IsInRange(target.Position, mine.Position, c.MineBlastRadius) {
						target.HP -= mine.Damage
						target.RoundDamageTaken += mine.Damage

						// Attribute damage to mine owner.
						target.LastDamagedBy = mine.OwnerID
						target.LastDamageTick = tickCount
						target.HitsReceived = append(target.HitsReceived, HitRecord{
							AttackerID: mine.OwnerID,
							Damage:     mine.Damage,
							Weapon:     "landmine",
						})

						// Track damage dealt for owner.
						if owner, ok := bots[mine.OwnerID]; ok {
							owner.RoundDamageDealt += mine.Damage
						}
					}
				}
				triggered = true
				detonated = append(detonated, mine.ID)

				// Decrement owner's mine count.
				if owner, ok := bots[mine.OwnerID]; ok {
					owner.MineCount--
					if owner.MineCount < 0 {
						owner.MineCount = 0
					}
				}
				break
			}
		}

		if !triggered {
			active = append(active, *mine)
		}
	}

	*mines = active
	return detonated
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
