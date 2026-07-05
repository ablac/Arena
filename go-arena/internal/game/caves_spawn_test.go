package game

import (
	"fmt"
	"testing"

	"arena-server/internal/config"
)

// TestCavesRoundStartSpawnStress replicates startRound's exact spawn sequence
// on caves maps many times and asserts no bot is ever placed in a blocked
// cell. This is the regression test for "bots stuck in the ground when the
// map changes".
func TestCavesRoundStartSpawnStress(t *testing.T) {
	loadTestConfig(t)
	prevTerrain, prevShape := ActiveTerrain, ActiveMapShape
	defer func() { ActiveTerrain, ActiveMapShape = prevTerrain, prevShape }()
	config.C.MapShape = "caves"

	grid := NewSpatialGrid(config.C.SpatialCellSize)
	const rounds = 300
	const botCount = 14
	violations := 0

	for round := 0; round < rounds; round++ {
		obstacles, _, terrain, shape, maskRects := generateRoundTerrain(botCount)
		ActiveTerrain = terrain
		ActiveMapShape = shape
		m := NewArenaMap()
		m.Reset(obstacles)
		m.MaskRects = maskRects

		points := m.GetSpawnPoints(botCount)
		for i, p := range points {
			bot := &BotState{BotID: fmt.Sprintf("b%d", i), MaxHP: 100}
			SpawnBotAt(bot, p, grid, 0)
			cell := terrain.WorldToGrid(bot.Position)
			if terrain.IsBlocked(cell[0], cell[1]) {
				violations++
				t.Logf("round %d: bot %d spawned in wall cell %v (requested %v, got %v)",
					round, i, cell, p, bot.Position)
			}
		}
		grid.Clear()
	}
	if violations > 0 {
		t.Errorf("%d of %d caves spawns landed inside walls", violations, rounds*botCount)
	}
}

// TestCavesTeamSpawnStress does the same for team-mode arc spawns.
func TestCavesTeamSpawnStress(t *testing.T) {
	loadTestConfig(t)
	prevTerrain, prevShape := ActiveTerrain, ActiveMapShape
	defer func() { ActiveTerrain, ActiveMapShape = prevTerrain, prevShape }()
	config.C.MapShape = "caves"

	grid := NewSpatialGrid(config.C.SpatialCellSize)
	const rounds = 200
	const teamCount = 2
	const teamSize = 7
	violations := 0

	for round := 0; round < rounds; round++ {
		obstacles, _, terrain, shape, maskRects := generateRoundTerrain(teamCount * teamSize)
		ActiveTerrain = terrain
		ActiveMapShape = shape
		m := NewArenaMap()
		m.Reset(obstacles)
		m.MaskRects = maskRects

		for team := 1; team <= teamCount; team++ {
			for member := 0; member < teamSize; member++ {
				p := m.TeamSpawnPoint(team, member, teamCount, teamSize)
				bot := &BotState{BotID: fmt.Sprintf("t%dm%d", team, member), MaxHP: 100}
				SpawnBotAt(bot, p, grid, 0)
				cell := terrain.WorldToGrid(bot.Position)
				if terrain.IsBlocked(cell[0], cell[1]) {
					violations++
					t.Logf("round %d: team %d member %d spawned in wall cell %v", round, team, member, cell)
				}
			}
		}
		grid.Clear()
	}
	if violations > 0 {
		t.Errorf("%d of %d caves team spawns landed inside walls", violations, rounds*teamCount*teamSize)
	}
}

// TestSpawnBotAtDeepWallRescue feeds SpawnBotAt a position deep inside a
// caves wall (deeper than its spiral radius) and asserts the bot still ends
// up on an open cell. Guards the reconnect / worst-case-fallback path.
func TestSpawnBotAtDeepWallRescue(t *testing.T) {
	loadTestConfig(t)
	prevTerrain := ActiveTerrain
	defer func() { ActiveTerrain = prevTerrain }()

	grid := NewSpatialGrid(config.C.SpatialCellSize)
	failures := 0
	const trials = 100

	for trial := 0; trial < trials; trial++ {
		terrain := NewTerrainGrid(2000, 2000, nil, 20, 5)
		if mask := GenerateShapeMask(ShapeCaves, terrain.Width, terrain.Height); mask != nil {
			terrain.ApplyMask(mask)
		}
		ActiveTerrain = terrain

		// The map corner is always deep inside the caves border wall.
		bot := &BotState{BotID: "corner", MaxHP: 100}
		SpawnBotAt(bot, NewVec2(10, 10), grid, 0)
		cell := terrain.WorldToGrid(bot.Position)
		if terrain.IsBlocked(cell[0], cell[1]) {
			failures++
			t.Logf("trial %d: corner spawn left bot in wall cell %v", trial, cell)
		}
		grid.Clear()
	}
	if failures > 0 {
		t.Errorf("%d of %d deep-wall spawns left the bot inside a wall", failures, trials)
	}
}
