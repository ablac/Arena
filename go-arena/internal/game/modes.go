package game

import (
	"math"
	"sort"

	"arena-server/internal/config"
)

// GameMode identifies the ruleset a round is played under.
type GameMode string

const (
	// ModeFFA is the classic free-for-all: last bot standing wins.
	ModeFFA GameMode = "ffa"
	// ModeTeamBattle splits bots into teams; last team standing wins.
	ModeTeamBattle GameMode = "team_battle"
	// ModeCTF is capture the flag: teams score by carrying the enemy flag
	// back to their own base.
	ModeCTF GameMode = "ctf"
)

// ModeRules describes how a game mode alters core round behaviour. FFA is
// the baseline; team modes layer team assignment, friendly-fire rules,
// objectives, and alternate win conditions on top of it.
type ModeRules struct {
	Mode            GameMode
	TeamCount       int  // < 2 means no teams (free-for-all)
	FriendlyFire    bool // whether same-team damage is allowed
	TeamElimination bool // round ends when at most one team has alive bots
	UsesFlags       bool // CTF: spawn one flag per team
}

// ActiveModeRules is the ruleset for the current round. It is resolved by the
// engine at round start (same package-global pattern as ActiveTerrain) so
// stateless helpers like ApplyDamage can consult it without an engine handle.
var ActiveModeRules = ModeRulesFor(ModeFFA)

// ModeRulesFor returns the rules for a mode. Unknown modes fall back to FFA
// so a bad config value can never wedge the server.
func ModeRulesFor(mode GameMode) ModeRules {
	c := &config.C
	teamCount := c.TeamCount
	if teamCount < 2 {
		teamCount = 2
	}
	switch mode {
	case ModeTeamBattle:
		return ModeRules{
			Mode:            ModeTeamBattle,
			TeamCount:       teamCount,
			FriendlyFire:    c.FriendlyFire,
			TeamElimination: true,
		}
	case ModeCTF:
		return ModeRules{
			Mode:            ModeCTF,
			TeamCount:       teamCount,
			FriendlyFire:    c.FriendlyFire,
			TeamElimination: false, // CTF rounds run to time/score, not elimination
			UsesFlags:       true,
		}
	default:
		return ModeRules{Mode: ModeFFA}
	}
}

// CurrentModeRules resolves the configured game mode.
func CurrentModeRules() ModeRules {
	return ModeRulesFor(GameMode(config.C.GameModeName))
}

// HasTeams reports whether this ruleset plays with teams.
func (r ModeRules) HasTeams() bool {
	return r.TeamCount >= 2 && r.Mode != ModeFFA
}

// SameTeam reports whether two bots are on the same (real) team. Team 0 means
// unassigned, which never counts as a shared team.
func SameTeam(a, b *BotState) bool {
	return a != nil && b != nil && a.Team > 0 && a.Team == b.Team
}

// CanDamage reports whether attacker is allowed to damage target under this
// ruleset. Self-damage (environmental attribution) is always allowed.
func (r ModeRules) CanDamage(attacker, target *BotState) bool {
	if attacker == nil || target == nil || attacker == target {
		return true
	}
	if r.HasTeams() && !r.FriendlyFire && SameTeam(attacker, target) {
		return false
	}
	return true
}

// AssignTeams distributes bots across teamCount teams as evenly as possible.
// Assignment is deterministic (sorted by bot ID) so reconnecting bots land on
// the same team within a round. Clears teams when teamCount < 2.
func AssignTeams(bots map[string]*BotState, teamCount int) {
	ids := make([]string, 0, len(bots))
	for id := range bots {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	if teamCount < 2 {
		for _, id := range ids {
			bots[id].Team = 0
		}
		return
	}
	for i, id := range ids {
		bots[id].Team = (i % teamCount) + 1
	}
}

// AliveTeams returns the set of team numbers that still have at least one
// alive bot. Bots without a team (0) are ignored.
func AliveTeams(bots map[string]*BotState) map[int]int {
	alive := make(map[int]int)
	for _, bot := range bots {
		if bot.IsAlive && bot.Team > 0 {
			alive[bot.Team]++
		}
	}
	return alive
}

// TeamSpawnPoint returns a spawn position for a team member: teams spawn in
// separate arcs of the spawn ring so allies start together and enemies apart.
// teamIdx is the 1-based team number, memberIdx the index within the team.
func (m *ArenaMap) TeamSpawnPoint(teamIdx, memberIdx, teamCount, teamSize int) Vec2 {
	if teamCount < 1 {
		teamCount = 1
	}
	if teamSize < 1 {
		teamSize = 1
	}
	spawnRadius := math.Min(m.ZoneRadius*0.85, m.maxRadiusInsideArena()*0.9)

	// Each team owns an arc slice of the circle; members spread within it.
	arc := 2 * math.Pi / float64(teamCount)
	base := arc * float64(teamIdx-1)
	// Keep members inside the middle 60% of the arc so teams stay separated.
	span := arc * 0.6
	start := base + (arc-span)/2
	var angle float64
	if teamSize == 1 {
		angle = base + arc/2
	} else {
		angle = start + span*float64(memberIdx)/float64(teamSize-1)
	}

	botR := config.C.BotRadius
	for nudge := 0; nudge < 12; nudge++ {
		sign := float64(1)
		if nudge%2 == 1 {
			sign = -1
		}
		a := angle + sign*float64((nudge+1)/2)*0.08
		x := m.ZoneCenter.X() + spawnRadius*math.Cos(a)
		y := m.ZoneCenter.Y() + spawnRadius*math.Sin(a)
		pos := m.ClampToArena(NewVec2(x, y))
		if m.IsInZone(pos) && CollidesWithObstacle(pos.X(), pos.Y(), m.Obstacles, botR) == nil && !terrainBlockedAt(pos) {
			return pos
		}
	}
	return m.ClampToArena(m.ZoneCenter)
}

// terrainBlockedAt reports whether a world position falls in a blocked
// terrain cell (respects non-square map masks). False when no terrain is
// active yet.
func terrainBlockedAt(pos Vec2) bool {
	if ActiveTerrain == nil {
		return false
	}
	cell := ActiveTerrain.WorldToGrid(pos)
	return ActiveTerrain.IsBlocked(cell[0], cell[1])
}
