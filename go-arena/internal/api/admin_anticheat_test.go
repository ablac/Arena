package api

import "testing"

func TestDefenseReductionOutOfRangeAllowsRoundedDetailValues(t *testing.T) {
	if defenseReductionOutOfRange(0.10, 0.06) {
		t.Fatal("0.06 rounded to 0.1 should not be treated as out of range")
	}
	if !defenseReductionOutOfRange(0.20, 0.06) {
		t.Fatal("large defense reduction should be treated as out of range")
	}
}

func TestDamagePerHitOutOfRangeRequiresEnoughSamples(t *testing.T) {
	if out, _ := damagePerHitOutOfRange(3, 999, 50); out {
		t.Fatal("small hit samples should not create damage-per-hit flags")
	}
	if out, severity := damagePerHitOutOfRange(8, 90, 50); !out || severity != "high" {
		t.Fatalf("expected high review signal, got out=%v severity=%q", out, severity)
	}
	if out, severity := damagePerHitOutOfRange(8, 120, 50); !out || severity != "critical" {
		t.Fatalf("expected critical review signal, got out=%v severity=%q", out, severity)
	}
}

func TestAnticheatRiskScoreAndConfidence(t *testing.T) {
	flags := []acFlag{
		{Severity: "high"},
		{Severity: "medium"},
		{Severity: "low"},
	}
	risk := anticheatRiskScore(flags)
	if risk != 29 {
		t.Fatalf("risk = %d, want 29", risk)
	}
	if got := anticheatConfidence(risk, 0, len(flags)); got != "medium" {
		t.Fatalf("confidence = %q, want medium", got)
	}
	if got := anticheatConfidence(20, 1, 1); got != "high" {
		t.Fatalf("hard stat flag confidence = %q, want high", got)
	}
}
