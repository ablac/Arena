package api

import (
	"testing"

	"arena-server/internal/db"
)

func TestDemoLoadoutBotSkinOverridesPackSkin(t *testing.T) {
	selections, err := cosmeticSelectionsForDemoLoadout(
		db.DefaultCosmeticCatalogData(),
		"neon-signal-pack",
		"skin-body-spider-drone",
		"trail-ember-sparks",
	)
	if err != nil {
		t.Fatalf("resolve demo loadout: %v", err)
	}

	bySlot := make(map[string]string, len(selections))
	for _, selection := range selections {
		if previous := bySlot[selection.Slot]; previous != "" {
			t.Fatalf("slot %q selected twice: %q and %q", selection.Slot, previous, selection.CosmeticID)
		}
		bySlot[selection.Slot] = selection.CosmeticID
	}

	if got := bySlot[db.CosmeticSlotBotSkin]; got != "skin-body-spider-drone" {
		t.Fatalf("bot skin = %q, want Spider Drone", got)
	}
	if len(selections) != 4 {
		t.Fatalf("selection count = %d, want pack slots plus trail with one bot skin", len(selections))
	}
}
