package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestDefaultCosmeticCatalogIntegrity(t *testing.T) {
	items := DefaultCosmeticCatalog()
	if len(items) != 303 {
		t.Fatalf("launch catalog has %d items, want 303", len(items))
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
		if !IsSupportedCosmeticAsset(item.Slot, item.AssetKey) {
			t.Fatalf("catalog item %q has unsupported %s asset %q", item.ID, item.Slot, item.AssetKey)
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

func TestLaunchCosmeticCatalogHasOneHundredCompleteSets(t *testing.T) {
	catalog := DefaultCosmeticCatalogData()
	if len(catalog.Items) != 303 {
		t.Fatalf("launch items = %d, want 303", len(catalog.Items))
	}
	if len(catalog.Packs) != 100 {
		t.Fatalf("launch packs = %d, want 100", len(catalog.Packs))
	}

	defaultIDs := map[string]bool{
		"skin-standard": true, "weapon-standard": true, "attachment-none": true,
	}
	itemsByID := make(map[string]CosmeticItem, len(catalog.Items))
	customMemberships := make(map[string]int, 300)
	for _, item := range catalog.Items {
		if _, exists := itemsByID[item.ID]; exists {
			t.Fatalf("duplicate launch item id %q", item.ID)
		}
		itemsByID[item.ID] = item
		if !defaultIDs[item.ID] {
			customMemberships[item.ID] = 0
		}
	}
	if len(customMemberships) != 300 {
		t.Fatalf("custom cosmetics = %d, want 300", len(customMemberships))
	}

	packIDs := make(map[string]bool, len(catalog.Packs))
	for index, pack := range catalog.Packs {
		if packIDs[pack.ID] {
			t.Fatalf("duplicate launch pack id %q", pack.ID)
		}
		packIDs[pack.ID] = true
		if len(pack.ItemIDs) != 3 || len(pack.Items) != 3 {
			t.Fatalf("pack %q has ids=%d items=%d, want three of each", pack.ID, len(pack.ItemIDs), len(pack.Items))
		}
		if !pack.IsActive || !pack.IsPurchasable || pack.IsFree || pack.PriceCents <= 0 || pack.Currency != "USD" {
			t.Fatalf("pack %q is not sale-ready: %+v", pack.ID, pack)
		}
		slots := make(map[string]bool, 3)
		assetKeys := make(map[string]bool, 3)
		for _, itemID := range pack.ItemIDs {
			item, ok := itemsByID[itemID]
			if !ok {
				t.Fatalf("pack %q references missing item %q", pack.ID, itemID)
			}
			if defaultIDs[itemID] {
				t.Fatalf("pack %q contains default item %q", pack.ID, itemID)
			}
			if item.IsPurchasable {
				t.Fatalf("pack %q piece %q is directly purchasable", pack.ID, itemID)
			}
			slots[item.Slot] = true
			assetKeys[item.AssetKey] = true
			customMemberships[itemID]++
		}
		for _, slot := range []string{CosmeticSlotBotSkin, CosmeticSlotWeaponSkin, CosmeticSlotAttachment} {
			if !slots[slot] {
				t.Fatalf("pack %q is missing slot %q", pack.ID, slot)
			}
		}
		if index >= 2 && len(assetKeys) != 1 {
			t.Fatalf("generated pack %q uses %d asset keys, want one shared set key", pack.ID, len(assetKeys))
		}
	}
	for itemID, memberships := range customMemberships {
		if memberships != 1 {
			t.Errorf("custom cosmetic %q belongs to %d packs, want exactly one", itemID, memberships)
		}
	}
}

func TestLaunchCosmeticLegacyIDsRemainStable(t *testing.T) {
	catalog := DefaultCosmeticCatalogData()
	legacyItems := []string{
		"skin-standard", "skin-neon-grid", "skin-carbon-armor",
		"weapon-standard", "weapon-solar-flare", "weapon-void-edge",
		"attachment-none", "attachment-signal-antenna", "attachment-orbital-halo",
	}
	for _, itemID := range legacyItems {
		if cosmeticItemByID(catalog.Items, itemID) == nil {
			t.Errorf("legacy cosmetic id %q changed or disappeared", itemID)
		}
	}
	wantPacks := map[string][]string{
		"neon-signal-pack": {"skin-neon-grid", "weapon-solar-flare", "attachment-signal-antenna"},
		"void-orbit-pack":  {"skin-carbon-armor", "weapon-void-edge", "attachment-orbital-halo"},
	}
	for packID, wantItems := range wantPacks {
		pack := cosmeticPackByID(catalog.Packs, packID)
		if pack == nil {
			t.Errorf("legacy pack id %q changed or disappeared", packID)
			continue
		}
		if fmt.Sprint(pack.ItemIDs) != fmt.Sprint(wantItems) {
			t.Errorf("legacy pack %q items = %v, want %v", packID, pack.ItemIDs, wantItems)
		}
	}
}

func TestDefaultCosmeticCatalogEntriesAreBuiltin(t *testing.T) {
	catalog := DefaultCosmeticCatalogData()
	for _, category := range catalog.Categories {
		if !category.IsBuiltin {
			t.Errorf("seed category %q is not built-in", category.ID)
		}
	}
	for _, item := range catalog.Items {
		if !item.IsBuiltin {
			t.Errorf("seed item %q is not built-in", item.ID)
		}
	}
	for _, pack := range catalog.Packs {
		if !pack.IsBuiltin {
			t.Errorf("seed pack %q is not built-in", pack.ID)
		}
	}
}

func TestSupportedCosmeticAssetValidation(t *testing.T) {
	valid := []struct{ slot, key string }{
		{CosmeticSlotBotSkin, "standard"},
		{CosmeticSlotBotSkin, "neon_grid"},
		{CosmeticSlotWeaponSkin, "solar_flare"},
		{CosmeticSlotAttachment, "orbital_halo"},
		{CosmeticSlotBotSkin, "arena_set_001_alpha"},
		{CosmeticSlotWeaponSkin, "arena_set_100_omega_paragon"},
		{CosmeticSlotAttachment, "arena_set_999_last_set"},
	}
	for _, test := range valid {
		if !IsSupportedCosmeticAsset(test.slot, test.key) {
			t.Errorf("supported asset rejected: %s/%s", test.slot, test.key)
		}
	}
	invalid := []struct{ slot, key string }{
		{CosmeticSlotAttachment, "standard"},
		{CosmeticSlotBotSkin, "unknown_asset"},
		{CosmeticSlotBotSkin, "arena_set_000_alpha"},
		{CosmeticSlotBotSkin, "arena_set_1000_alpha"},
		{CosmeticSlotBotSkin, "arena_set_003_"},
		{CosmeticSlotBotSkin, "arena_set_003_two__words"},
		{CosmeticSlotBotSkin, "arena_set_003_Two_words"},
		{"power", "arena_set_003_alpha"},
	}
	for _, test := range invalid {
		if IsSupportedCosmeticAsset(test.slot, test.key) {
			t.Errorf("unsupported asset accepted: %s/%s", test.slot, test.key)
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

func TestDefaultCosmeticCatalogDataIncludesOrderedSaleReadyPacks(t *testing.T) {
	catalog := DefaultCosmeticCatalogData()
	if len(catalog.Categories) < 4 {
		t.Fatalf("starter categories = %d, want at least 4", len(catalog.Categories))
	}
	if len(catalog.Packs) != 100 {
		t.Fatalf("launch packs = %d, want 100", len(catalog.Packs))
	}

	for index, pack := range catalog.Packs {
		if pack.PriceCents <= 0 || pack.Currency != "USD" {
			t.Errorf("pack %q price = %d %s, want positive USD minor units", pack.ID, pack.PriceCents, pack.Currency)
		}
		if !pack.IsActive || !pack.IsPurchasable || pack.IsFree {
			t.Errorf("pack %q sale metadata is inconsistent: %+v", pack.ID, pack)
		}
		if len(pack.Items) != 3 {
			t.Errorf("pack %q has %d items, want 3", pack.ID, len(pack.Items))
		}
		if index > 0 && catalog.Packs[index-1].CategoryID == pack.CategoryID && catalog.Packs[index-1].SortOrder > pack.SortOrder {
			t.Errorf("packs in category %q are not ordered by sort_order", pack.CategoryID)
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
		AssetKey: "arena_set_901_test_attachment", Rarity: "common", CategoryID: "attachments",
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
		{ID: "Bad ID", Name: "Bad", CategoryID: "attachments", Slot: CosmeticSlotAttachment, AssetKey: "arena_set_902_bad", Rarity: "common", Currency: "USD"},
		{ID: "bad-slot", Name: "Bad", CategoryID: "attachments", Slot: "power", AssetKey: "arena_set_902_bad", Rarity: "common", Currency: "USD"},
		{ID: "bad-free", Name: "Bad", CategoryID: "attachments", Slot: CosmeticSlotAttachment, AssetKey: "arena_set_902_bad", Rarity: "common", PriceCents: 1, Currency: "USD", IsFree: true},
		{ID: "bad-price", Name: "Bad", CategoryID: "attachments", Slot: CosmeticSlotAttachment, AssetKey: "arena_set_902_bad", Rarity: "common", Currency: "USD", IsPurchasable: true},
		{ID: "bad-currency", Name: "Bad", CategoryID: "attachments", Slot: CosmeticSlotAttachment, AssetKey: "arena_set_902_bad", Rarity: "common", PriceCents: 99, Currency: "usd", IsPurchasable: true},
		{ID: "bad-currency-exponent", Name: "Bad", CategoryID: "attachments", Slot: CosmeticSlotAttachment, AssetKey: "arena_set_902_bad", Rarity: "common", PriceCents: 99, Currency: "JPY", IsPurchasable: true},
		{ID: "bad-asset", Name: "Bad", CategoryID: "attachments", Slot: CosmeticSlotAttachment, AssetKey: "syntactically_valid_but_unknown", Rarity: "common", Currency: "USD"},
	}
	for _, item := range invalidItems {
		if err := ValidateCosmeticItem(item); err == nil {
			t.Errorf("invalid item accepted: %+v", item)
		}
	}

	invalidPacks := []CosmeticPack{
		{ID: "empty-pack", Name: "Empty", PriceCents: 99, Currency: "USD", IsPurchasable: true, IsActive: true},
		{ID: "duplicate-pack", Name: "Duplicate", PriceCents: 99, Currency: "USD", IsPurchasable: true, IsActive: true, ItemIDs: []string{"one", "one"}},
		{ID: "non-usd-pack", Name: "Non USD", CategoryID: "starter-packs", PriceCents: 99, Currency: "JPY", IsPurchasable: true, IsActive: true, ItemIDs: []string{"one"}},
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
