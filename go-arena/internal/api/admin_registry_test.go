package api

import (
	"testing"

	"arena-server/internal/config"
)

func TestValidateDemoTemplateRejectsBadStats(t *testing.T) {
	config.Load()
	tpl := demoTemplatePayload{
		Name:     "Broken",
		Weapon:   "bow",
		Strategy: "kite",
		Color:    "#00ff88",
		Stats:    map[string]int{"hp": 10, "speed": 10, "attack": 10, "defense": 10},
	}
	if _, err := validateDemoTemplate(tpl); err == nil {
		t.Fatal("expected stat-budget validation error")
	}
}

func TestValidateDemoTemplateAcceptsBalancedTemplate(t *testing.T) {
	config.Load()
	tpl := demoTemplatePayload{
		Name:     "Night Archer",
		Weapon:   "bow",
		Strategy: "kite",
		Color:    "#00ff88",
		Stats:    map[string]int{"hp": 4, "speed": 6, "attack": 8, "defense": 2},
	}
	got, err := validateDemoTemplate(tpl)
	if err != nil {
		t.Fatalf("expected valid template, got %v", err)
	}
	if got.Name != "Night Archer" || got.Weapon != "bow" || got.Stats["attack"] != 8 {
		t.Fatalf("unexpected validated template: %+v", got)
	}
}

func TestBuildMapPreviewReturnsTerrainAndPlayablePercent(t *testing.T) {
	prev, err := buildMapPreview(mapPreviewRequest{Shape: "circle", Cols: 48, Rows: 32})
	if err != nil {
		t.Fatalf("buildMapPreview returned error: %v", err)
	}
	if prev.Shape != "circle" || len(prev.Terrain) != 32 {
		t.Fatalf("unexpected preview metadata: %+v", prev)
	}
	if len(prev.Terrain[0]) != 48 {
		t.Fatalf("unexpected preview row width: %d", len(prev.Terrain[0]))
	}
	if prev.PlayablePct <= 0 || prev.PlayablePct > 100 {
		t.Fatalf("playable percentage out of range: %.2f", prev.PlayablePct)
	}
}
