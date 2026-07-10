package db

import (
	"context"
	"errors"
	"testing"
)

func TestDefaultCosmeticCatalogIntegrity(t *testing.T) {
	items := DefaultCosmeticCatalog()
	if len(items) < 6 {
		t.Fatalf("starter catalog has %d items, want at least 6", len(items))
	}

	ids := make(map[string]bool)
	assets := make(map[string]bool)
	freeBySlot := make(map[string]bool)
	for _, item := range items {
		if item.ID == "" || item.Name == "" || item.AssetKey == "" {
			t.Fatalf("catalog item has empty identity field: %+v", item)
		}
		if !IsValidCosmeticSlot(item.Slot) {
			t.Fatalf("catalog item %q has invalid slot %q", item.ID, item.Slot)
		}
		if ids[item.ID] {
			t.Fatalf("duplicate cosmetic id %q", item.ID)
		}
		ids[item.ID] = true
		assetIdentity := item.Slot + ":" + item.AssetKey
		if assets[assetIdentity] {
			t.Fatalf("duplicate cosmetic asset %q", assetIdentity)
		}
		assets[assetIdentity] = true
		if item.PriceCents < 0 {
			t.Fatalf("catalog item %q has negative price", item.ID)
		}
		if item.IsFree {
			freeBySlot[item.Slot] = true
			if item.PriceCents != 0 {
				t.Fatalf("free item %q has a nonzero price", item.ID)
			}
		}
	}

	for _, slot := range []string{CosmeticSlotBotSkin, CosmeticSlotWeaponSkin, CosmeticSlotAttachment} {
		if !freeBySlot[slot] {
			t.Errorf("slot %q has no free fallback item", slot)
		}
	}
}

func TestDefaultCosmeticCatalogReturnsCopy(t *testing.T) {
	first := DefaultCosmeticCatalog()
	first[0].Name = "mutated"
	second := DefaultCosmeticCatalog()
	if second[0].Name == "mutated" {
		t.Fatal("DefaultCosmeticCatalog exposed mutable shared state")
	}
}

func TestCosmeticQueriesNilPoolReturnError(t *testing.T) {
	original := Pool
	Pool = nil
	t.Cleanup(func() { Pool = original })

	ctx := context.Background()
	if _, err := ListCosmeticCatalog(ctx); !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("ListCosmeticCatalog error = %v, want ErrNoDatabase", err)
	}
	if _, err := ListBotCosmetics(ctx, "bot"); !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("ListBotCosmetics error = %v, want ErrNoDatabase", err)
	}
	if _, err := GetEquippedCosmetics(ctx, "bot"); !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("GetEquippedCosmetics error = %v, want ErrNoDatabase", err)
	}
	if _, err := EquipCosmetic(ctx, "bot", CosmeticSlotBotSkin, "skin-standard"); !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("EquipCosmetic error = %v, want ErrNoDatabase", err)
	}
}

func TestCosmeticSlotValidation(t *testing.T) {
	for _, slot := range []string{CosmeticSlotBotSkin, CosmeticSlotWeaponSkin, CosmeticSlotAttachment, " BOT_SKIN "} {
		if !IsValidCosmeticSlot(slot) {
			t.Errorf("slot %q should be valid", slot)
		}
	}
	for _, slot := range []string{"", "power", "weapon", "bot_skin; DROP TABLE bots"} {
		if IsValidCosmeticSlot(slot) {
			t.Errorf("slot %q should be invalid", slot)
		}
	}
}
