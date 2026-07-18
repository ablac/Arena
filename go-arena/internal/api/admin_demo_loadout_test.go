package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arena-server/internal/db"
)

type fakeDemoCatalogAuthority struct {
	catalog *db.CosmeticCatalog
	called  bool
}

func (f *fakeDemoCatalogAuthority) PublicCatalog(context.Context) (*db.CosmeticCatalog, error) {
	f.called = true
	return f.catalog, nil
}

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

func TestDemoLoadoutReadsCatalogThroughPlatformAuthority(t *testing.T) {
	authority := &fakeDemoCatalogAuthority{catalog: &db.CosmeticCatalog{}}
	handler := NewAdminHandler(nil)
	handler.platformCatalog = authority
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/bots/demo-loadout", strings.NewReader(`{
		"bot_id":"demo-bot",
		"pack_id":"missing-pack"
	}`))

	handler.applyDemoLoadout(recorder, request)

	if !authority.called {
		t.Fatal("platform authority was not asked for the catalog")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
