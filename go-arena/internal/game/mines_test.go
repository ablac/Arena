package game

import "testing"

func TestMineIgnoresAlliesButTriggersAndDamagesEnemies(t *testing.T) {
	previousMode := ActiveModeRules
	previousTerrain := ActiveTerrain
	ActiveModeRules = ModeRules{
		Mode:         ModeTeamBattle,
		TeamCount:    2,
		FriendlyFire: false,
	}
	ActiveTerrain = nil
	t.Cleanup(func() {
		ActiveModeRules = previousMode
		ActiveTerrain = previousTerrain
	})

	owner := &BotState{
		BotID:     "owner",
		Team:      1,
		HP:        100,
		IsAlive:   true,
		MineCount: 1,
		Position:  NewVec2(0, 0),
	}
	ally := &BotState{BotID: "ally", Team: 1, HP: 100, IsAlive: true, Position: NewVec2(1, 0)}
	enemy := &BotState{BotID: "enemy", Team: 2, HP: 100, IsAlive: true, Position: NewVec2(1000, 0)}
	bots := map[string]*BotState{owner.BotID: owner, ally.BotID: ally, enemy.BotID: enemy}
	mines := []Landmine{{
		ID:          "mine-1",
		OwnerID:     owner.BotID,
		Position:    NewVec2(0, 0),
		Damage:      30,
		BlastRadius: 2,
		Armed:       true,
	}}

	if events := UpdateMines(&mines, bots, 10); len(events) != 0 {
		t.Fatalf("ally triggered mine: events = %+v", events)
	}
	if len(mines) != 1 || ally.HP != 100 {
		t.Fatalf("ally changed armed mine: mines=%d ally_hp=%v", len(mines), ally.HP)
	}

	enemy.Position = NewVec2(0, 0)
	if !ActiveModeRules.CanDamage(owner, enemy) {
		t.Fatal("enemy unexpectedly protected by team damage rules")
	}
	if !IsInRange(enemy.Position, mines[0].Position, mines[0].BlastRadius) {
		t.Fatalf("enemy position %v not in mine range (radius=%d)", enemy.Position, mines[0].BlastRadius)
	}
	if events := UpdateMines(&mines, bots, 11); len(events) != 1 {
		t.Fatalf("enemy detonation events = %d, want 1", len(events))
	}
	if len(mines) != 0 {
		t.Fatalf("detonated mine count = %d, want 0", len(mines))
	}
	if ally.HP != 100 || ally.LastDamagedBy != "" {
		t.Fatalf("ally took friendly mine damage: hp=%v attacker=%q", ally.HP, ally.LastDamagedBy)
	}
	if enemy.HP != 70 || enemy.LastDamagedBy != owner.BotID || enemy.LastDamageSource != "landmine" || enemy.LastDamageAmount != 30 {
		t.Fatalf("enemy mine result: hp=%v attacker=%q source=%q damage=%v",
			enemy.HP, enemy.LastDamagedBy, enemy.LastDamageSource, enemy.LastDamageAmount)
	}
	if owner.RoundDamageDealt != 30 || owner.MineCount != 0 {
		t.Fatalf("owner mine stats: damage=%v mines=%d", owner.RoundDamageDealt, owner.MineCount)
	}
}
