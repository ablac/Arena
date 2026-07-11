package game

import (
	"math"
	"testing"
)

func TestGravityWellSkipsAlliesAndInvulnerableEnemies(t *testing.T) {
	withIntegrityTestRules(t)
	ActiveTerrain = openMovementTerrain(12, 6)
	ActiveModeRules = ModeRules{Mode: ModeTeamBattle, TeamCount: 2, FriendlyFire: false}

	owner := &BotState{BotID: "owner", Team: 1, IsAlive: true, Position: ActiveTerrain.GridToWorld([2]int{1, 1})}
	ally := &BotState{BotID: "ally", Team: 1, IsAlive: true, Position: ActiveTerrain.GridToWorld([2]int{2, 1})}
	dodging := &BotState{BotID: "dodging", Team: 2, IsAlive: true, InvulnTicks: 1, Position: ActiveTerrain.GridToWorld([2]int{2, 2})}
	enemy := &BotState{BotID: "enemy", Team: 2, IsAlive: true, Position: ActiveTerrain.GridToWorld([2]int{2, 3})}
	bots := map[string]*BotState{owner.BotID: owner, ally.BotID: ally, dodging.BotID: dodging, enemy.BotID: enemy}
	grid := NewSpatialGrid(20)
	for _, bot := range bots {
		grid.Insert(bot.BotID, bot.Position)
	}
	wells := []GravityWell{{
		OwnerID:        owner.BotID,
		Position:       ActiveTerrain.GridToWorld([2]int{6, 2}),
		RemainingTicks: 10,
		PullRadius:     10,
		PullForce:      1,
	}}
	allyStart := ally.Position
	dodgingStart := dodging.Position
	enemyStart := enemy.Position

	UpdateGravityWells(&wells, bots, grid)

	if ally.Position != allyStart {
		t.Fatalf("gravity well pulled a friendly-fire-disabled ally: got %v want %v", ally.Position, allyStart)
	}
	if dodging.Position != dodgingStart {
		t.Fatalf("gravity well pulled an invulnerable enemy: got %v want %v", dodging.Position, dodgingStart)
	}
	if enemy.Position == enemyStart {
		t.Fatal("gravity well did not pull a vulnerable enemy")
	}
}

func TestTerrainGravityWellHonorsFractionalPullForceOverTime(t *testing.T) {
	withIntegrityTestRules(t)
	ActiveTerrain = openMovementTerrain(12, 4)

	owner := &BotState{BotID: "owner", IsAlive: true, Position: ActiveTerrain.GridToWorld([2]int{1, 1})}
	enemy := &BotState{BotID: "enemy", IsAlive: true, Position: ActiveTerrain.GridToWorld([2]int{2, 1})}
	bots := map[string]*BotState{owner.BotID: owner, enemy.BotID: enemy}
	grid := NewSpatialGrid(20)
	grid.Insert(owner.BotID, owner.Position)
	grid.Insert(enemy.BotID, enemy.Position)
	wells := []GravityWell{{
		OwnerID:        owner.BotID,
		Position:       ActiveTerrain.GridToWorld([2]int{8, 1}),
		RemainingTicks: 10,
		PullRadius:     10,
		PullForce:      0.5,
	}}
	start := enemy.Position

	UpdateGravityWells(&wells, bots, grid)
	UpdateGravityWells(&wells, bots, grid)

	if got, want := start.DistanceTo(enemy.Position), ActiveTerrain.CellSize; math.Abs(got-want) > 1e-9 {
		t.Fatalf("two ticks at 0.5 cells/tick moved %.1f, want %.1f", got, want)
	}
}
