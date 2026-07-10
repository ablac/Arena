package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestLethalShoveWallSlamReplacesStaleAttribution(t *testing.T) {
	loadTestConfig(t)
	previousTerrain := ActiveTerrain
	ActiveTerrain = nil
	t.Cleanup(func() { ActiveTerrain = previousTerrain })

	config.C.ArenaWidth = 85
	config.C.ArenaHeight = 200
	config.C.BotRadius = 5
	config.C.PathfindingCellSize = 20
	config.C.KnockbackWallDamage = 15

	shover := &BotState{
		BotID: "shover", Name: "Shove Bot", IsAlive: true, HP: 100,
		Position: NewVec2(60, 100), PendingAction: &Action{Type: ActionShove, TargetID: "victim"},
	}
	victim := &BotState{
		BotID: "victim", Name: "Victim", IsAlive: true, HP: 10,
		Position:      NewVec2(80, 100),
		LastDamagedBy: "stale-attacker", LastDamageTick: 99,
		LastDamageSource: "sword", LastDamageAmount: 99,
	}
	bots := map[string]*BotState{shover.BotID: shover, victim.BotID: victim}

	ProcessShoves(bots, nil, 100)

	if victim.LastDamagedBy != shover.BotID || victim.LastDamageTick != 100 ||
		victim.LastDamageSource != "shove_wall_slam" || victim.LastDamageAmount != 15 {
		t.Fatalf("wall-slam attribution = attacker=%q tick=%d source=%q damage=%v",
			victim.LastDamagedBy, victim.LastDamageTick, victim.LastDamageSource, victim.LastDamageAmount)
	}
	if shover.RoundDamageDealt != 15 || victim.RoundDamageTaken != 15 {
		t.Fatalf("wall-slam stats = dealt=%v taken=%v", shover.RoundDamageDealt, victim.RoundDamageTaken)
	}

	deaths := CheckDeaths(bots, NewSpatialGrid(20), 100)
	if len(deaths) != 1 {
		t.Fatalf("deaths = %d, want 1", len(deaths))
	}
	death := deaths[0]
	if death.KillerID != shover.BotID || death.Weapon != "shove_wall_slam" || death.Damage != 15 {
		t.Fatalf("death attribution = %+v", death)
	}
}
