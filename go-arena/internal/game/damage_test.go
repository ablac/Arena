package game

import (
	"testing"

	"arena-server/internal/config"
)

func newTestBot(id string, hp float64) *BotState {
	config.Load()
	return &BotState{
		BotID:            id,
		Name:             id,
		HP:               hp,
		MaxHP:            hp,
		IsAlive:          true,
		Weapon:           "sword",
		AttackMultiplier: 1.0,
		DefenseReduction: 0.0,
		Position:         NewVec2(100, 100),
		LastValidPosition: NewVec2(100, 100),
	}
}

func TestApplyDamageBasic(t *testing.T) {
	attacker := newTestBot("atk", 100)
	target := newTestBot("tgt", 100)

	dealt := ApplyDamage(target, attacker, 30, "sword", 1)
	if dealt != 30 {
		t.Errorf("dealt=%v, want 30", dealt)
	}
	if target.HP != 70 {
		t.Errorf("target HP=%v, want 70", target.HP)
	}
	if attacker.RoundDamageDealt != 30 {
		t.Errorf("attacker RoundDamageDealt=%v, want 30", attacker.RoundDamageDealt)
	}
	if target.RoundDamageTaken != 30 {
		t.Errorf("target RoundDamageTaken=%v, want 30", target.RoundDamageTaken)
	}
}

func TestApplyDamageInvulnerable(t *testing.T) {
	attacker := newTestBot("atk", 100)
	target := newTestBot("tgt", 100)
	target.InvulnTicks = 5

	dealt := ApplyDamage(target, attacker, 50, "sword", 1)
	if dealt != 0 {
		t.Errorf("dealt=%v on invuln target, want 0", dealt)
	}
	if target.HP != 100 {
		t.Errorf("invuln target HP changed to %v", target.HP)
	}
}

func TestApplyDamageShieldWeapon(t *testing.T) {
	attacker := newTestBot("atk", 100)
	target := newTestBot("tgt", 100)
	target.Weapon = "shield"

	shieldCfg := GetWeaponConfig("shield")
	expected := 50.0 * (1 - shieldCfg.Param)

	dealt := ApplyDamage(target, attacker, 50, "sword", 1)
	if dealt != expected {
		t.Errorf("shield weapon: dealt=%v, want %v", dealt, expected)
	}
}

func TestApplyDamageShieldAbsorb(t *testing.T) {
	attacker := newTestBot("atk", 100)
	target := newTestBot("tgt", 100)
	target.ShieldAbsorb = 20

	dealt := ApplyDamage(target, attacker, 30, "bow", 1)
	// 20 absorbed, 10 reaches HP
	if dealt != 10 {
		t.Errorf("shield absorb: dealt=%v, want 10", dealt)
	}
	if target.HP != 90 {
		t.Errorf("target HP=%v, want 90", target.HP)
	}
	if target.ShieldAbsorb != 0 {
		t.Errorf("shield absorb=%v, want 0", target.ShieldAbsorb)
	}
}

func TestApplyDamageShieldAbsorbPartial(t *testing.T) {
	attacker := newTestBot("atk", 100)
	target := newTestBot("tgt", 100)
	target.ShieldAbsorb = 50 // more than damage

	dealt := ApplyDamage(target, attacker, 30, "bow", 1)
	if dealt != 0 {
		t.Errorf("full absorb: dealt=%v, want 0", dealt)
	}
	if target.HP != 100 {
		t.Errorf("target HP changed: %v", target.HP)
	}
	if target.ShieldAbsorb != 20 {
		t.Errorf("shield absorb=%v, want 20", target.ShieldAbsorb)
	}
}

func TestApplyDamageHitRecord(t *testing.T) {
	attacker := newTestBot("atk", 100)
	target := newTestBot("tgt", 100)

	ApplyDamage(target, attacker, 15, "sword", 5)

	if len(target.HitsReceived) != 1 {
		t.Fatalf("expected 1 hit record, got %d", len(target.HitsReceived))
	}
	hr := target.HitsReceived[0]
	if hr.AttackerID != "atk" {
		t.Errorf("AttackerID=%v, want atk", hr.AttackerID)
	}
	if hr.Damage != 15 {
		t.Errorf("Damage=%v, want 15", hr.Damage)
	}
	if hr.Weapon != "sword" {
		t.Errorf("Weapon=%v, want sword", hr.Weapon)
	}
}

func TestApplyDamageAttribution(t *testing.T) {
	attacker := newTestBot("atk", 100)
	target := newTestBot("tgt", 100)

	ApplyDamage(target, attacker, 10, "sword", 7)
	if target.LastDamagedBy != "atk" {
		t.Errorf("LastDamagedBy=%v", target.LastDamagedBy)
	}
	if target.LastDamageTick != 7 {
		t.Errorf("LastDamageTick=%v", target.LastDamageTick)
	}
}

func TestTickTimers(t *testing.T) {
	config.Load()
	bot := newTestBot("b", 100)
	bot.CooldownRemaining = 1.0
	bot.StunTicks = 3
	bot.InvulnTicks = 2
	bot.DodgeCooldown = 5
	bot.ShoveCooldown = 2.0
	bot.GrappleCooldown = 1.5

	dt := 0.1
	TickTimers(bot, dt)

	if bot.CooldownRemaining != 0.9 {
		t.Errorf("CooldownRemaining=%v, want 0.9", bot.CooldownRemaining)
	}
	if bot.StunTicks != 2 {
		t.Errorf("StunTicks=%v, want 2", bot.StunTicks)
	}
	if bot.InvulnTicks != 1 {
		t.Errorf("InvulnTicks=%v, want 1", bot.InvulnTicks)
	}
	if bot.DodgeCooldown != 4 {
		t.Errorf("DodgeCooldown=%v, want 4", bot.DodgeCooldown)
	}
}

func TestTickTimersClampsToZero(t *testing.T) {
	config.Load()
	bot := newTestBot("b", 100)
	bot.CooldownRemaining = 0.05
	bot.ShoveCooldown = 0.05
	bot.GrappleCooldown = 0.05

	TickTimers(bot, 1.0)

	if bot.CooldownRemaining != 0 {
		t.Errorf("CooldownRemaining should be 0, got %v", bot.CooldownRemaining)
	}
	if bot.ShoveCooldown != 0 {
		t.Errorf("ShoveCooldown should be 0, got %v", bot.ShoveCooldown)
	}
	if bot.GrappleCooldown != 0 {
		t.Errorf("GrappleCooldown should be 0, got %v", bot.GrappleCooldown)
	}
}

func TestTickEffects(t *testing.T) {
	config.Load()
	bot := newTestBot("b", 100)
	bot.ActiveEffects = []Effect{
		{Name: "speed_boost", RemainingTicks: 2, Value: 2.0},
		{Name: "damage_boost", RemainingTicks: 1, Value: 1.5},
	}

	TickEffects(bot)

	if len(bot.ActiveEffects) != 1 {
		t.Errorf("expected 1 effect after tick, got %d", len(bot.ActiveEffects))
	}
	if bot.ActiveEffects[0].Name != "speed_boost" {
		t.Errorf("wrong surviving effect: %v", bot.ActiveEffects[0].Name)
	}
	if bot.ActiveEffects[0].RemainingTicks != 1 {
		t.Errorf("RemainingTicks=%v, want 1", bot.ActiveEffects[0].RemainingTicks)
	}
}

func TestTickEffectsAllExpire(t *testing.T) {
	config.Load()
	bot := newTestBot("b", 100)
	bot.ActiveEffects = []Effect{
		{Name: "speed_boost", RemainingTicks: 1, Value: 2.0},
	}
	TickEffects(bot)
	if len(bot.ActiveEffects) != 0 {
		t.Errorf("expected 0 effects after expiry, got %d", len(bot.ActiveEffects))
	}
}
