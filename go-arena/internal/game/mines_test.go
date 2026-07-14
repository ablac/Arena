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

// TestMineDetonationDamageRespectsShieldAbsorb guards against mine damage
// bypassing the shared ApplyDamage pipeline: a Shield Bubble (or capture-pad
// shield bonus) must soak a mine blast the same way it soaks any other
// damage source.
func TestMineDetonationDamageRespectsShieldAbsorb(t *testing.T) {
	previousMode := ActiveModeRules
	previousTerrain := ActiveTerrain
	ActiveModeRules = ModeRules{Mode: ModeFFA}
	ActiveTerrain = nil
	t.Cleanup(func() {
		ActiveModeRules = previousMode
		ActiveTerrain = previousTerrain
	})

	owner := &BotState{BotID: "owner", HP: 100, IsAlive: true, MineCount: 1, Position: NewVec2(0, 0)}
	enemy := &BotState{BotID: "enemy", HP: 100, IsAlive: true, ShieldAbsorb: 20, Position: NewVec2(0, 0)}
	bots := map[string]*BotState{owner.BotID: owner, enemy.BotID: enemy}
	mines := []Landmine{{
		ID: "mine-1", OwnerID: owner.BotID, Position: NewVec2(0, 0),
		Damage: 30, BlastRadius: 2, Armed: true,
	}}

	if events := UpdateMines(&mines, bots, 1); len(events) != 1 {
		t.Fatalf("detonation events = %d, want 1", len(events))
	}
	// 30 damage against a 20-point shield pool: 20 absorbed, 10 to HP.
	if enemy.ShieldAbsorb != 0 {
		t.Fatalf("enemy.ShieldAbsorb = %v, want 0 (mine damage must consume the shield pool)", enemy.ShieldAbsorb)
	}
	if enemy.HP != 90 {
		t.Fatalf("enemy.HP = %v, want 90 (30 damage - 20 absorbed by ShieldAbsorb)", enemy.HP)
	}
}

// TestInvulnerableBotCannotTriggerEnemyMine guards against a dodging bot
// setting off an enemy's mine at all - matching hazard zones, which skip
// invulnerable bots in the outer interaction check rather than only
// exempting them from the resulting damage. Without this, an invulnerable
// bot alone within blast radius still detonates and consumes the mine, even
// though ApplyDamage would exempt it from the resulting damage anyway - the
// bug is the wasted/inappropriate trigger itself, not the damage.
func TestInvulnerableBotCannotTriggerEnemyMine(t *testing.T) {
	previousMode := ActiveModeRules
	previousTerrain := ActiveTerrain
	ActiveModeRules = ModeRules{Mode: ModeFFA}
	ActiveTerrain = nil
	t.Cleanup(func() {
		ActiveModeRules = previousMode
		ActiveTerrain = previousTerrain
	})

	owner := &BotState{BotID: "owner", HP: 100, IsAlive: true, MineCount: 1, Position: NewVec2(0, 0)}
	dodging := &BotState{BotID: "dodging", HP: 100, IsAlive: true, InvulnTicks: 5, Position: NewVec2(0, 0)}
	bots := map[string]*BotState{owner.BotID: owner, dodging.BotID: dodging}
	mines := []Landmine{{
		ID: "mine-1", OwnerID: owner.BotID, Position: NewVec2(0, 0),
		Damage: 30, BlastRadius: 2, Armed: true,
	}}

	if events := UpdateMines(&mines, bots, 1); len(events) != 0 {
		t.Fatalf("invulnerable bot alone triggered the mine: events = %+v", events)
	}
	if len(mines) != 1 {
		t.Fatalf("mine consumed by an invulnerable bot: mines = %d, want 1", len(mines))
	}
}
