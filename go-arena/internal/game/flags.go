package game

import (
	"fmt"
	"math"

	"arena-server/internal/config"
)

// FlagStatus describes where a CTF flag currently is.
type FlagStatus string

const (
	FlagAtBase  FlagStatus = "at_base"
	FlagCarried FlagStatus = "carried"
	FlagDropped FlagStatus = "dropped"
)

// CTFFlag is one team's flag in capture-the-flag mode.
type CTFFlag struct {
	ID           string
	Team         int
	BasePosition Vec2
	Position     Vec2
	Status       FlagStatus
	CarrierID    string
	DroppedTick  int
}

// SpawnCTFFlags places one flag (and base) per team. Bases sit on the spawn
// arc centre of each team so flags start near their owners.
func SpawnCTFFlags(arena *ArenaMap, teamCount int) []*CTFFlag {
	if teamCount < 2 {
		return nil
	}
	flags := make([]*CTFFlag, 0, teamCount)
	baseRadius := math.Min(arena.ZoneRadius*0.7, arena.maxRadiusInsideArena()*0.8)
	arc := 2 * math.Pi / float64(teamCount)

	for team := 1; team <= teamCount; team++ {
		angle := arc*float64(team-1) + arc/2
		pos := arena.ClampToArena(NewVec2(
			arena.ZoneCenter.X()+baseRadius*math.Cos(angle),
			arena.ZoneCenter.Y()+baseRadius*math.Sin(angle),
		))
		// Nudge off obstacles/blocked terrain if needed.
		for nudge := 0; nudge < 16; nudge++ {
			if CollidesWithObstacle(pos.X(), pos.Y(), arena.Obstacles, config.C.BotRadius*2) == nil && !terrainBlockedAt(pos) {
				break
			}
			a := angle + float64(nudge+1)*0.15
			pos = arena.ClampToArena(NewVec2(
				arena.ZoneCenter.X()+baseRadius*math.Cos(a),
				arena.ZoneCenter.Y()+baseRadius*math.Sin(a),
			))
		}
		flags = append(flags, &CTFFlag{
			ID:           fmt.Sprintf("flag_%d", team),
			Team:         team,
			BasePosition: pos,
			Position:     pos,
			Status:       FlagAtBase,
		})
	}
	return flags
}

// UpdateCTFFlags advances flag state for one tick: pickups, drops, returns,
// and captures. teamScores is mutated when a capture happens. Returns arena
// events for spectator feedback.
func UpdateCTFFlags(flags []*CTFFlag, bots map[string]*BotState, teamScores map[int]int, tickCount int) []ArenaEvent {
	if len(flags) == 0 {
		return nil
	}
	c := &config.C
	pickupR := c.CTFFlagPickupRadius
	returnTicks := int(c.CTFFlagReturnSecs * float64(c.TickRate))
	var events []ArenaEvent

	// Index of each team's own flag, for capture checks.
	flagByTeam := make(map[int]*CTFFlag, len(flags))
	for _, f := range flags {
		flagByTeam[f.Team] = f
	}

	for _, f := range flags {
		switch f.Status {
		case FlagCarried:
			carrier, ok := bots[f.CarrierID]
			if !ok || !carrier.IsAlive {
				// Carrier died or disconnected: drop the flag where they fell.
				if ok {
					f.Position = carrier.Position
				}
				f.Status = FlagDropped
				f.CarrierID = ""
				f.DroppedTick = tickCount
				events = append(events, ArenaEvent{
					ID: fmt.Sprintf("flagdrop_%s_%d", f.ID, tickCount), Type: "flag_dropped",
					Tick: tickCount, Position: f.Position,
				})
				continue
			}
			// Flag follows the carrier.
			f.Position = carrier.Position

			// Capture: carrier reaches their own base while their own flag is home.
			ownFlag := flagByTeam[carrier.Team]
			if ownFlag != nil && ownFlag.Status == FlagAtBase &&
				carrier.Position.DistanceTo(ownFlag.BasePosition) <= pickupR {
				teamScores[carrier.Team]++
				carrier.RoundFlagCaptures++
				f.Status = FlagAtBase
				f.Position = f.BasePosition
				f.CarrierID = ""
				events = append(events, ArenaEvent{
					ID: fmt.Sprintf("flagcap_%s_%d", f.ID, tickCount), Type: "flag_captured",
					Tick: tickCount, Position: ownFlag.BasePosition, OwnerID: carrier.BotID,
				})
			}

		case FlagDropped:
			// Auto-return after the timeout.
			if returnTicks > 0 && tickCount-f.DroppedTick >= returnTicks {
				f.Status = FlagAtBase
				f.Position = f.BasePosition
				events = append(events, ArenaEvent{
					ID: fmt.Sprintf("flagret_%s_%d", f.ID, tickCount), Type: "flag_returned",
					Tick: tickCount, Position: f.BasePosition,
				})
				continue
			}
			fallthrough

		case FlagAtBase:
			// Touch interactions: enemies steal, owners return dropped flags.
			for _, bot := range bots {
				if !bot.IsAlive || bot.Team == 0 {
					continue
				}
				if bot.Position.DistanceTo(f.Position) > pickupR {
					continue
				}
				if bot.Team == f.Team {
					// Owner touch: return a dropped flag home.
					if f.Status == FlagDropped {
						f.Status = FlagAtBase
						f.Position = f.BasePosition
						events = append(events, ArenaEvent{
							ID: fmt.Sprintf("flagret_%s_%d", f.ID, tickCount), Type: "flag_returned",
							Tick: tickCount, Position: f.BasePosition, OwnerID: bot.BotID,
						})
						break
					}
					continue
				}
				// Enemy touch: steal the flag. One flag per carrier.
				if carriesFlag(flags, bot.BotID) {
					continue
				}
				f.Status = FlagCarried
				f.CarrierID = bot.BotID
				f.Position = bot.Position
				events = append(events, ArenaEvent{
					ID: fmt.Sprintf("flagtake_%s_%d", f.ID, tickCount), Type: "flag_taken",
					Tick: tickCount, Position: bot.Position, OwnerID: bot.BotID,
				})
				break
			}
		}
	}

	return events
}

// carriesFlag reports whether the bot already carries any flag.
func carriesFlag(flags []*CTFFlag, botID string) bool {
	for _, f := range flags {
		if f.Status == FlagCarried && f.CarrierID == botID {
			return true
		}
	}
	return false
}

// CTFWinningTeam returns the team that reached the capture target, or 0.
func CTFWinningTeam(teamScores map[int]int) int {
	target := config.C.CTFCapturesToWin
	if target <= 0 {
		return 0
	}
	for team, score := range teamScores {
		if score >= target {
			return team
		}
	}
	return 0
}

// BuildFlagView serialises a flag for spectator clients.
func BuildFlagView(f *CTFFlag) map[string]interface{} {
	return map[string]interface{}{
		"id":            f.ID,
		"team":          f.Team,
		"position":      Vec2{round1(f.Position[0]), round1(f.Position[1])},
		"base_position": Vec2{round1(f.BasePosition[0]), round1(f.BasePosition[1])},
		"status":        string(f.Status),
		"carrier_id":    f.CarrierID,
	}
}
