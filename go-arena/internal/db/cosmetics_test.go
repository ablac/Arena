package db

import (
	"context"
	"errors"
	"fmt"
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

func TestDefaultCosmeticCatalogDataIncludesOrderedNinetyNineCentPacks(t *testing.T) {
	catalog := DefaultCosmeticCatalogData()
	if len(catalog.Categories) < 4 {
		t.Fatalf("starter categories = %d, want at least 4", len(catalog.Categories))
	}
	if len(catalog.Packs) < 2 {
		t.Fatalf("starter packs = %d, want at least 2", len(catalog.Packs))
	}

	for index, pack := range catalog.Packs {
		if pack.PriceCents != 99 || pack.Currency != "USD" {
			t.Errorf("pack %q price = %d %s, want 99 USD", pack.ID, pack.PriceCents, pack.Currency)
		}
		if !pack.IsActive || !pack.IsPurchasable || pack.IsFree {
			t.Errorf("pack %q sale metadata is inconsistent: %+v", pack.ID, pack)
		}
		if len(pack.Items) < 2 {
			t.Errorf("pack %q has %d items, want at least 2", pack.ID, len(pack.Items))
		}
		if index > 0 && catalog.Packs[index-1].SortOrder > pack.SortOrder {
			t.Errorf("starter packs are not ordered by sort_order: %+v", catalog.Packs)
		}
	}
}

func TestCosmeticCatalogMetadataValidation(t *testing.T) {
	validCategory := CosmeticCategory{ID: "starter-packs", Name: "Starter Packs", IsActive: true}
	if err := ValidateCosmeticCategory(validCategory); err != nil {
		t.Fatalf("valid category rejected: %v", err)
	}

	validItem := CosmeticItem{
		ID: "attachment-test", Name: "Test Attachment", Slot: CosmeticSlotAttachment,
		AssetKey: "test_attachment", Rarity: "common", CategoryID: "attachments",
		PriceCents: 99, Currency: "USD", IsPurchasable: true, IsActive: true,
	}
	if err := ValidateCosmeticItem(validItem); err != nil {
		t.Fatalf("valid item rejected: %v", err)
	}

	validPack := CosmeticPack{
		ID: "test-pack", Name: "Test Pack", CategoryID: "starter-packs",
		PriceCents: 99, Currency: "USD", IsPurchasable: true, IsActive: true,
		ItemIDs: []string{"attachment-test"},
	}
	if err := ValidateCosmeticPack(validPack); err != nil {
		t.Fatalf("valid pack rejected: %v", err)
	}

	invalidItems := []CosmeticItem{
		{ID: "Bad ID", Name: "Bad", Slot: CosmeticSlotAttachment, AssetKey: "bad", Currency: "USD"},
		{ID: "bad-slot", Name: "Bad", Slot: "power", AssetKey: "bad", Currency: "USD"},
		{ID: "bad-free", Name: "Bad", Slot: CosmeticSlotAttachment, AssetKey: "bad", PriceCents: 1, Currency: "USD", IsFree: true},
		{ID: "bad-price", Name: "Bad", Slot: CosmeticSlotAttachment, AssetKey: "bad", Currency: "USD", IsPurchasable: true},
		{ID: "bad-currency", Name: "Bad", Slot: CosmeticSlotAttachment, AssetKey: "bad", PriceCents: 99, Currency: "usd", IsPurchasable: true},
	}
	for _, item := range invalidItems {
		if err := ValidateCosmeticItem(item); err == nil {
			t.Errorf("invalid item accepted: %+v", item)
		}
	}

	invalidPacks := []CosmeticPack{
		{ID: "empty-pack", Name: "Empty", PriceCents: 99, Currency: "USD", IsPurchasable: true, IsActive: true},
		{ID: "duplicate-pack", Name: "Duplicate", PriceCents: 99, Currency: "USD", IsPurchasable: true, IsActive: true, ItemIDs: []string{"one", "one"}},
	}
	for _, pack := range invalidPacks {
		if err := ValidateCosmeticPack(pack); err == nil {
			t.Errorf("invalid pack accepted: %+v", pack)
		}
	}

	tooManyItems := validPack
	tooManyItems.ItemIDs = make([]string, 501)
	for index := range tooManyItems.ItemIDs {
		tooManyItems.ItemIDs[index] = fmt.Sprintf("item-%d", index)
	}
	if err := ValidateCosmeticPack(tooManyItems); err == nil {
		t.Fatal("pack with more than 500 items was accepted")
	}
}
