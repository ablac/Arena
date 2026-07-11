package db

import (
	"context"
	"errors"
	"testing"
)

func TestPostgresCosmeticCatalogAdministrationAndPublicProjection(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	category := CosmeticCategory{ID: "event", Name: "Event", Description: "Event cosmetics", IsActive: true, SortOrder: 50}
	if _, err := UpsertCosmeticCategory(ctx, category, "integration-admin"); err != nil {
		t.Fatalf("UpsertCosmeticCategory: %v", err)
	}
	item := CosmeticItem{
		ID: "attachment-event-crown", Name: "Event Crown", Description: "Presentation only.",
		CategoryID: category.ID, Slot: CosmeticSlotAttachment, AssetKey: "event_crown", Rarity: "rare",
		PriceCents: 99, Currency: "USD", IsPurchasable: true, IsActive: true, SortOrder: 10,
	}
	if _, err := UpsertCosmeticCatalogItem(ctx, item, "integration-admin"); err != nil {
		t.Fatalf("UpsertCosmeticCatalogItem: %v", err)
	}
	pack := CosmeticPack{
		ID: "event-pack", CategoryID: category.ID, Name: "Event Pack", Description: "One event cosmetic.",
		PriceCents: 99, Currency: "USD", IsPurchasable: true, IsActive: true, SortOrder: 50,
		ItemIDs: []string{item.ID},
	}
	if _, err := UpsertCosmeticPack(ctx, pack, "integration-admin"); err != nil {
		t.Fatalf("UpsertCosmeticPack: %v", err)
	}

	publicCatalog, err := GetPublicCosmeticCatalog(ctx)
	if err != nil {
		t.Fatalf("GetPublicCosmeticCatalog: %v", err)
	}
	gotPack := cosmeticPackByID(publicCatalog.Packs, pack.ID)
	if gotPack == nil || len(gotPack.Items) != 1 || gotPack.Items[0].ID != item.ID {
		t.Fatalf("public pack = %+v, want embedded item %q", gotPack, item.ID)
	}

	pack.PriceCents = 129
	if _, err := UpsertCosmeticPack(ctx, pack, "integration-admin"); err != nil {
		t.Fatalf("update pack price: %v", err)
	}
	audit, err := ListCosmeticCatalogAudit(ctx, 20)
	if err != nil {
		t.Fatalf("ListCosmeticCatalogAudit: %v", err)
	}
	if len(audit) < 4 || audit[0].Actor != "integration-admin" || audit[0].EntityID != pack.ID || len(audit[0].Before) == 0 || len(audit[0].After) == 0 {
		t.Fatalf("catalog audit did not capture update before/after: %+v", audit)
	}

	category.IsActive = false
	if _, err := UpsertCosmeticCategory(ctx, category, "integration-admin"); err != nil {
		t.Fatalf("deactivate category: %v", err)
	}
	publicCatalog, err = GetPublicCosmeticCatalog(ctx)
	if err != nil {
		t.Fatalf("GetPublicCosmeticCatalog after deactivate: %v", err)
	}
	if cosmeticPackByID(publicCatalog.Packs, pack.ID) != nil || cosmeticItemByID(publicCatalog.Items, item.ID) != nil {
		t.Fatal("public catalog exposed entries in an inactive category")
	}
	adminCatalog, err := GetAdminCosmeticCatalog(ctx)
	if err != nil {
		t.Fatalf("GetAdminCosmeticCatalog: %v", err)
	}
	if cosmeticPackByID(adminCatalog.Packs, pack.ID) == nil || cosmeticItemByID(adminCatalog.Items, item.ID) == nil {
		t.Fatal("admin catalog omitted inactive entries")
	}
}

func TestPostgresCosmeticPackUpdatesAreAtomicUnderConcurrency(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	packA := CosmeticPack{
		ID: "concurrent-pack", CategoryID: "starter-packs", Name: "Concurrent Pack A",
		PriceCents: 101, Currency: "USD", IsPurchasable: true, IsActive: true, SortOrder: 90,
		ItemIDs: []string{"skin-neon-grid"},
	}
	packB := packA
	packB.Name = "Concurrent Pack B"
	packB.PriceCents = 202
	packB.ItemIDs = []string{"weapon-void-edge"}

	results := make(chan error, 2)
	go func() { _, err := UpsertCosmeticPack(context.Background(), packA, "writer-a"); results <- err }()
	go func() { _, err := UpsertCosmeticPack(context.Background(), packB, "writer-b"); results <- err }()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent UpsertCosmeticPack: %v", err)
		}
	}

	catalog, err := GetAdminCosmeticCatalog(ctx)
	if err != nil {
		t.Fatalf("GetAdminCosmeticCatalog: %v", err)
	}
	got := cosmeticPackByID(catalog.Packs, packA.ID)
	if got == nil {
		t.Fatal("concurrent pack missing")
	}
	isA := got.Name == packA.Name && got.PriceCents == packA.PriceCents && len(got.ItemIDs) == 1 && got.ItemIDs[0] == packA.ItemIDs[0]
	isB := got.Name == packB.Name && got.PriceCents == packB.PriceCents && len(got.ItemIDs) == 1 && got.ItemIDs[0] == packB.ItemIDs[0]
	if !isA && !isB {
		t.Fatalf("concurrent pack contains torn metadata/membership: %+v", got)
	}
}

func TestPostgresCosmeticCatalogRejectsBrokenReferencesAndImmutableRenderIdentity(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	invalidPack := CosmeticPack{
		ID: "broken-pack", CategoryID: "starter-packs", Name: "Broken Pack",
		PriceCents: 99, Currency: "USD", IsPurchasable: true, IsActive: true,
		ItemIDs: []string{"missing-item"},
	}
	if _, err := UpsertCosmeticPack(ctx, invalidPack, "integration-admin"); !errors.Is(err, ErrCosmeticCatalogInvalid) {
		t.Fatalf("missing pack item error = %v, want ErrCosmeticCatalogInvalid", err)
	}

	item := DefaultCosmeticCatalog()[0]
	item.AssetKey = "changed_identity"
	if _, err := UpsertCosmeticCatalogItem(ctx, item, "integration-admin"); !errors.Is(err, ErrCosmeticCatalogConflict) {
		t.Fatalf("render identity change error = %v, want ErrCosmeticCatalogConflict", err)
	}

	if deleted, err := DeleteCosmeticCategory(ctx, "chassis", "integration-admin"); deleted || !errors.Is(err, ErrCosmeticCatalogConflict) {
		t.Fatalf("referenced category delete = (%v, %v), want conflict", deleted, err)
	}

	chassis := CosmeticCategory{ID: "chassis", Name: "Chassis", IsActive: false, SortOrder: 10}
	if _, err := UpsertCosmeticCategory(ctx, chassis, "integration-admin"); !errors.Is(err, ErrCosmeticCatalogConflict) {
		t.Fatalf("deactivating the free chassis fallback category error = %v, want conflict", err)
	}
}

func TestPostgresCosmeticStarterPackAdminEditsSurviveSchemaRepair(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	pack := starterCosmeticPacks[0]
	pack.ItemIDs = []string{"skin-neon-grid"}
	if _, err := UpsertCosmeticPack(ctx, pack, "integration-admin"); err != nil {
		t.Fatalf("edit starter pack: %v", err)
	}
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("schema repair after starter pack edit: %v", err)
	}
	catalog, err := GetAdminCosmeticCatalog(ctx)
	if err != nil {
		t.Fatalf("GetAdminCosmeticCatalog: %v", err)
	}
	got := cosmeticPackByID(catalog.Packs, pack.ID)
	if got == nil || len(got.ItemIDs) != 1 || got.ItemIDs[0] != "skin-neon-grid" {
		t.Fatalf("schema repair overwrote admin starter pack membership: %+v", got)
	}
}

func TestPostgresConcurrentFallbackChangesCannotRemoveLastFreeSlotItem(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	second := CosmeticItem{
		ID: "skin-second-free", Name: "Second Free Chassis", CategoryID: "chassis",
		Slot: CosmeticSlotBotSkin, AssetKey: "second_free", Rarity: "common",
		Currency: "USD", IsFree: true, IsActive: true, SortOrder: 99,
	}
	if _, err := UpsertCosmeticCatalogItem(ctx, second, "integration-admin"); err != nil {
		t.Fatalf("create second free fallback: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		CREATE FUNCTION delay_cosmetic_fallback_update() RETURNS trigger AS $$
		BEGIN
			IF OLD.slot = 'bot_skin' AND OLD.is_active = true AND NEW.is_active = false THEN
				PERFORM pg_sleep(0.2);
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`); err != nil {
		t.Fatalf("install fallback concurrency function: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		CREATE TRIGGER delay_cosmetic_fallback_update
			BEFORE UPDATE ON cosmetic_items
			FOR EACH ROW EXECUTE FUNCTION delay_cosmetic_fallback_update()`); err != nil {
		t.Fatalf("install fallback concurrency trigger: %v", err)
	}

	first := DefaultCosmeticCatalog()[0]
	first.IsActive = false
	second.IsActive = false
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, item := range []CosmeticItem{first, second} {
		item := item
		go func() {
			<-start
			_, err := UpsertCosmeticCatalogItem(context.Background(), item, "concurrent-admin")
			results <- err
		}()
	}
	close(start)
	conflicts := 0
	for range 2 {
		err := <-results
		if errors.Is(err, ErrCosmeticCatalogConflict) {
			conflicts++
		} else if err != nil {
			t.Fatalf("concurrent fallback update: %v", err)
		}
	}
	if conflicts != 1 {
		t.Fatalf("fallback conflicts = %d, want exactly one rejected update", conflicts)
	}
	var activeFree int
	if err := Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM cosmetic_items item
		JOIN cosmetic_categories category ON category.id = item.category_id
		WHERE item.slot = 'bot_skin' AND item.is_active = true AND item.is_free = true
		  AND category.is_active = true`).Scan(&activeFree); err != nil {
		t.Fatalf("count active free chassis: %v", err)
	}
	if activeFree != 1 {
		t.Fatalf("active free chassis = %d, want 1", activeFree)
	}
}

func cosmeticPackByID(packs []CosmeticPack, id string) *CosmeticPack {
	for index := range packs {
		if packs[index].ID == id {
			return &packs[index]
		}
	}
	return nil
}

func cosmeticItemByID(items []CosmeticItem, id string) *CosmeticItem {
	for index := range items {
		if items[index].ID == id {
			return &items[index]
		}
	}
	return nil
}
