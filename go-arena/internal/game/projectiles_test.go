package game

import (
	"testing"

	"arena-server/internal/config"
)

// TestUpdateProjectilesAppliesVictimDefenseReduction guards against the bow
// (and any other projectile weapon) bypassing the victim's Defense stat.
// proj.Damage is computed at fire time as weaponDmg*attackMult, before the
// eventual victim is known; UpdateProjectiles must apply that victim's real
// DefenseReduction at the moment of impact, the same way every melee/AoE
// damage path applies target.DefenseReduction right before ApplyDamage.
func TestUpdateProjectilesAppliesVictimDefenseReduction(t *testing.T) {
	previousTerrain := ActiveTerrain
	ActiveTerrain = nil
	t.Cleanup(func() { ActiveTerrain = previousTerrain })

	previousRules := ActiveModeRules
	ActiveModeRules = ModeRules{Mode: ModeFFA}
	t.Cleanup(func() { ActiveModeRules = previousRules })

	previousSuddenDeath := ActiveSuddenDeath
	ActiveSuddenDeath = nil // avoid leaking another test's sudden-death multiplier
	t.Cleanup(func() { ActiveSuddenDeath = previousSuddenDeath })

	oldBotRadius := config.C.BotRadius
	oldHitRadius := config.C.ProjectileHitRadius
	oldArenaWidth := config.C.ArenaWidth
	oldArenaHeight := config.C.ArenaHeight
	config.C.BotRadius = 5
	config.C.ProjectileHitRadius = 2
	// Pin a large arena so the post-hit knockback (ApplyAttributedGridKnockback)
	// can't push the victim into a wall and add unrelated wall-slam damage on
	// top of the projectile hit this test is actually checking - it must not
	// depend on whatever ArenaWidth/Height another test left behind.
	config.C.ArenaWidth = 2000
	config.C.ArenaHeight = 2000
	t.Cleanup(func() {
		config.C.BotRadius = oldBotRadius
		config.C.ProjectileHitRadius = oldHitRadius
		config.C.ArenaWidth = oldArenaWidth
		config.C.ArenaHeight = oldArenaHeight
	})

	owner := &BotState{BotID: "archer", IsAlive: true, HP: 100, MaxHP: 100, Position: NewVec2(400, 500)}
	victim := &BotState{BotID: "target", IsAlive: true, HP: 100, MaxHP: 100, DefenseReduction: 0.5, Position: NewVec2(500, 500)}
	bots := map[string]*BotState{owner.BotID: owner, victim.BotID: victim}

	projectiles := []Projectile{{
		ID:        "arrow-1",
		OwnerID:   owner.BotID,
		Position:  victim.Position, // already overlapping the victim this tick
		Direction: NewVec2(1, 0),
		Speed:     0, // stationary: stays on the victim after the move step
		HitRadius: config.C.ProjectileHitRadius,
		Damage:    20, // weaponDmg*attackMult baked in at fire time, defense NOT yet applied
		Weapon:    "bow",
		MaxAge:    10,
	}}

	var events []ArenaEvent
	UpdateProjectiles(&projectiles, bots, nil, &events, 1, 0.1)

	if victim.HP != 90 {
		t.Fatalf("victim.HP = %v, want 90 (20 base damage reduced by 50%% defense)", victim.HP)
	}
	if len(projectiles) != 0 {
		t.Fatalf("projectile survived a confirmed hit: %#v", projectiles)
	}
}
