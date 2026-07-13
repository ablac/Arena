package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestDefaultCosmeticCatalogIntegrity(t *testing.T) {
	items := DefaultCosmeticCatalog()
	wantItems := 328 + len(bodyFormCosmeticSeeds)
	if len(items) != wantItems {
		t.Fatalf("launch catalog has %d items, want %d", len(items), wantItems)
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

	for _, slot := range []string{CosmeticSlotBotSkin, CosmeticSlotWeaponSkin, CosmeticSlotAttachment, CosmeticSlotTrail} {
		if !freeBySlot[slot] {
			t.Errorf("slot %q has no free fallback item", slot)
		}
	}
}

func TestLaunchCosmeticCatalogHasOneHundredCompleteSets(t *testing.T) {
	catalog := DefaultCosmeticCatalogData()
	wantItems := 328 + len(bodyFormCosmeticSeeds)
	wantPacks := 124 + len(bodyFormCosmeticSeeds)
	if len(catalog.Items) != wantItems {
		t.Fatalf("launch items = %d, want %d", len(catalog.Items), wantItems)
	}
	if len(catalog.Packs) != wantPacks {
		t.Fatalf("launch packs = %d, want %d", len(catalog.Packs), wantPacks)
	}

	defaultIDs := map[string]bool{
		"skin-standard": true, "weapon-standard": true, "attachment-none": true, "trail-standard": true,
	}
	itemsByID := make(map[string]CosmeticItem, len(catalog.Items))
	customMemberships := make(map[string]int, 300)
	for _, item := range catalog.Items {
		if _, exists := itemsByID[item.ID]; exists {
			t.Fatalf("duplicate launch item id %q", item.ID)
		}
		itemsByID[item.ID] = item
		if !defaultIDs[item.ID] && item.Slot != CosmeticSlotTrail && item.CategoryID != CosmeticBodyFormCategoryID {
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
		if pack.CategoryID == CosmeticTrailCategoryID {
			continue
		}
		if pack.CategoryID == CosmeticBodyFormCategoryID {
			continue
		}
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

func TestBodyFormCatalogHasOriginalSingletonSkinsAtSetPrice(t *testing.T) {
	catalog := DefaultCosmeticCatalogData()
	itemsByID := make(map[string]CosmeticItem, len(catalog.Items))
	for _, item := range catalog.Items {
		itemsByID[item.ID] = item
	}

	seen := make(map[string]bool, len(bodyFormCosmeticSeeds))
	for _, pack := range catalog.Packs {
		if pack.CategoryID != CosmeticBodyFormCategoryID {
			continue
		}
		if len(pack.ItemIDs) != 1 || len(pack.Items) != 1 {
			t.Fatalf("body-form pack %q must contain one independent license", pack.ID)
		}
		if !pack.IsActive || !pack.IsPurchasable || pack.IsFree || pack.PriceCents != CosmeticPackPriceCents || pack.Currency != "USD" {
			t.Fatalf("body-form pack %q is not sale-ready at $1.99: %+v", pack.ID, pack)
		}
		item := itemsByID[pack.ItemIDs[0]]
		if item.CategoryID != CosmeticBodyFormCategoryID || item.Slot != CosmeticSlotBotSkin || item.IsPurchasable || item.IsFree {
			t.Fatalf("body-form item %q has invalid ownership metadata: %+v", item.ID, item)
		}
		if !IsSupportedCosmeticAsset(item.Slot, item.AssetKey) || seen[item.AssetKey] {
			t.Fatalf("body-form asset %q is unsupported or duplicated", item.AssetKey)
		}
		seen[item.AssetKey] = true
	}
	if len(seen) != len(bodyFormCosmeticSeeds) || len(seen) < 18 {
		t.Fatalf("body-form packs = %d, want %d", len(seen), len(bodyFormCosmeticSeeds))
	}
}

func TestLaunchCatalogHasTwentyFourIndividuallyPurchasableTrails(t *testing.T) {
	catalog := DefaultCosmeticCatalogData()
	itemsByID := make(map[string]CosmeticItem, len(catalog.Items))
	for _, item := range catalog.Items {
		itemsByID[item.ID] = item
	}

	trailPacks := 0
	assetKeys := make(map[string]bool)
	for _, pack := range catalog.Packs {
		if pack.CategoryID != CosmeticTrailCategoryID {
			continue
		}
		trailPacks++
		if !pack.IsActive || !pack.IsPurchasable || pack.IsFree || pack.PriceCents != CosmeticTrailPriceCents || pack.Currency != "USD" {
			t.Errorf("trail product %q is not a sale-ready $0.99 USD pack: %+v", pack.ID, pack)
		}
		if len(pack.ItemIDs) != 1 || len(pack.Items) != 1 {
			t.Fatalf("trail product %q has ids=%d items=%d, want one of each", pack.ID, len(pack.ItemIDs), len(pack.Items))
		}
		item := itemsByID[pack.ItemIDs[0]]
		if item.Slot != CosmeticSlotTrail || item.IsFree || item.PriceCents != CosmeticTrailPriceCents {
			t.Errorf("trail product %q item is not a paid trail: %+v", pack.ID, item)
		}
		if assetKeys[item.AssetKey] {
			t.Errorf("duplicate paid trail asset %q", item.AssetKey)
		}
		assetKeys[item.AssetKey] = true
	}
	if trailPacks != 24 || len(assetKeys) != 24 {
		t.Fatalf("paid trail products/assets = %d/%d, want 24/24", trailPacks, len(assetKeys))
	}
	fallback := cosmeticItemByID(catalog.Items, "trail-standard")
	if fallback == nil || fallback.Slot != CosmeticSlotTrail || fallback.AssetKey != "standard" || !fallback.IsFree || fallback.PriceCents != 0 {
		t.Fatalf("trail fallback = %+v, want free Standard Wake", fallback)
	}
}

func TestPurchasableCosmeticSetsAndTrailsUseFixedPrices(t *testing.T) {
	catalog := DefaultCosmeticCatalogData()
	for _, pack := range catalog.Packs {
		if !pack.IsPurchasable || pack.IsFree {
			continue
		}
		want := CosmeticPackPriceCents
		if pack.CategoryID == CosmeticTrailCategoryID {
			want = CosmeticTrailPriceCents
		}
		if pack.PriceCents != want || pack.Currency != "USD" {
			t.Errorf("purchasable pack %q price = %d %s, want %d USD", pack.ID, pack.PriceCents, pack.Currency, want)
		}
	}

	nonStandard := CosmeticPack{
		ID: "future-set", CategoryID: "starter-packs", Name: "Future Set",
		PriceCents: 299, Currency: "USD", IsPurchasable: true, IsActive: true,
		ItemIDs: []string{"skin-neon-grid"},
	}
	if err := ValidateCosmeticPack(nonStandard); !errors.Is(err, ErrCosmeticCatalogInvalid) {
		t.Fatalf("future purchasable set with non-standard price error = %v, want ErrCosmeticCatalogInvalid", err)
	}

	trail := CosmeticPack{
		ID: "future-trail", CategoryID: CosmeticTrailCategoryID, Name: "Future Trail",
		PriceCents: CosmeticTrailPriceCents, Currency: "USD", IsPurchasable: true, IsActive: true,
		ItemIDs: []string{"trail-future"},
	}
	if err := ValidateCosmeticPack(trail); err != nil {
		t.Fatalf("$0.99 trail product rejected: %v", err)
	}
	trail.PriceCents = CosmeticPackPriceCents
	if err := ValidateCosmeticPack(trail); !errors.Is(err, ErrCosmeticCatalogInvalid) {
		t.Fatalf("trail with set price error = %v, want ErrCosmeticCatalogInvalid", err)
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
		{CosmeticSlotTrail, "standard"},
		{CosmeticSlotTrail, "ember_sparks"},
		{CosmeticSlotTrail, "phantom_smoke"},
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
		{CosmeticSlotTrail, "unknown_trail"},
		{CosmeticSlotBotSkin, "ember_sparks"},
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
	for _, slot := range []string{CosmeticSlotBotSkin, CosmeticSlotWeaponSkin, CosmeticSlotAttachment, CosmeticSlotTrail, " BOT_SKIN "} {
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
	wantPacks := 124 + len(bodyFormCosmeticSeeds)
	if len(catalog.Packs) != wantPacks {
		t.Fatalf("launch packs = %d, want %d", len(catalog.Packs), wantPacks)
	}

	for index, pack := range catalog.Packs {
		if pack.PriceCents <= 0 || pack.Currency != "USD" {
			t.Errorf("pack %q price = %d %s, want positive USD minor units", pack.ID, pack.PriceCents, pack.Currency)
		}
		if !pack.IsActive || !pack.IsPurchasable || pack.IsFree {
			t.Errorf("pack %q sale metadata is inconsistent: %+v", pack.ID, pack)
		}
		wantItems := 3
		if pack.CategoryID == CosmeticTrailCategoryID || pack.CategoryID == CosmeticBodyFormCategoryID {
			wantItems = 1
		}
		if len(pack.Items) != wantItems {
			t.Errorf("pack %q has %d items, want %d", pack.ID, len(pack.Items), wantItems)
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
		PriceCents: CosmeticPackPriceCents, Currency: "USD", IsPurchasable: true, IsActive: true,
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
		{ID: "trail-wrong-category", Name: "Wrong Trail", CategoryID: "attachments", Slot: CosmeticSlotTrail, AssetKey: "ember_sparks", Rarity: "common", Currency: "USD"},
		{ID: "nontrail-wrong-category", Name: "Wrong Non-Trail", CategoryID: CosmeticTrailCategoryID, Slot: CosmeticSlotAttachment, AssetKey: "arena_set_902_bad", Rarity: "common", Currency: "USD"},
		{ID: "trail-wrong-price", Name: "Wrong Trail Price", CategoryID: CosmeticTrailCategoryID, Slot: CosmeticSlotTrail, AssetKey: "ember_sparks", Rarity: "common", PriceCents: 199, Currency: "USD"},
	}
	for _, item := range invalidItems {
		if err := ValidateCosmeticItem(item); err == nil {
			t.Errorf("invalid item accepted: %+v", item)
		}
	}

	invalidPacks := []CosmeticPack{
		{ID: "empty-pack", Name: "Empty", PriceCents: CosmeticPackPriceCents, Currency: "USD", IsPurchasable: true, IsActive: true},
		{ID: "duplicate-pack", Name: "Duplicate", PriceCents: CosmeticPackPriceCents, Currency: "USD", IsPurchasable: true, IsActive: true, ItemIDs: []string{"one", "one"}},
		{ID: "non-usd-pack", Name: "Non USD", CategoryID: "starter-packs", PriceCents: CosmeticPackPriceCents, Currency: "JPY", IsPurchasable: true, IsActive: true, ItemIDs: []string{"one"}},
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
