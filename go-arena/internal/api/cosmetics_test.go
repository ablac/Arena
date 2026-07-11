package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/go-chi/chi/v5"
)

type fakeCosmeticsStore struct {
	publicCatalog *db.CosmeticCatalog
	adminCatalog  *db.CosmeticCatalog
	audit         []db.CosmeticCatalogAudit
	items         []db.BotCosmeticItem
	equipped      map[string]string
	equipItem     *db.CosmeticItem
	equipErr      error
	grantCreated  bool
	grantErr      error
	revoked       bool
	revokeErr     error
	inventory     *db.CustomerCosmeticsInventory
	linkBot       *db.AccountBot
	assignment    *db.CosmeticAssignmentChange
	license       *db.CosmeticLicense
	lastBotID     string
	lastAccount   string
	lastLicense   string
	lastSlot      string
	lastCosmetic  string
	lastActor     string
	lastLimit     int
}

func (f *fakeCosmeticsStore) PublicCatalog(context.Context) (*db.CosmeticCatalog, error) {
	return f.publicCatalog, f.grantErr
}
func (f *fakeCosmeticsStore) AdminCatalog(context.Context) (*db.CosmeticCatalog, error) {
	return f.adminCatalog, f.grantErr
}
func (f *fakeCosmeticsStore) UpsertCategory(_ context.Context, category db.CosmeticCategory, actor string) (*db.CosmeticCategory, error) {
	f.lastActor = actor
	return &category, f.grantErr
}
func (f *fakeCosmeticsStore) DeleteCategory(_ context.Context, id, actor string) (bool, error) {
	f.lastCosmetic, f.lastActor = id, actor
	return f.revoked, f.revokeErr
}
func (f *fakeCosmeticsStore) UpsertItem(_ context.Context, item db.CosmeticItem, actor string) (*db.CosmeticItem, error) {
	f.lastCosmetic, f.lastActor = item.ID, actor
	return &item, f.grantErr
}
func (f *fakeCosmeticsStore) DeleteItem(_ context.Context, id, actor string) (bool, error) {
	f.lastCosmetic, f.lastActor = id, actor
	return f.revoked, f.revokeErr
}
func (f *fakeCosmeticsStore) UpsertPack(_ context.Context, pack db.CosmeticPack, actor string) (*db.CosmeticPack, error) {
	f.lastCosmetic, f.lastActor = pack.ID, actor
	return &pack, f.grantErr
}
func (f *fakeCosmeticsStore) DeletePack(_ context.Context, id, actor string) (bool, error) {
	f.lastCosmetic, f.lastActor = id, actor
	return f.revoked, f.revokeErr
}
func (f *fakeCosmeticsStore) ListAudit(_ context.Context, limit int) ([]db.CosmeticCatalogAudit, error) {
	f.lastLimit = limit
	return f.audit, f.grantErr
}
func (f *fakeCosmeticsStore) ListForBot(context.Context, string) ([]db.BotCosmeticItem, error) {
	return f.items, nil
}
func (f *fakeCosmeticsStore) Equipped(context.Context, string) (map[string]string, error) {
	return f.equipped, nil
}
func (f *fakeCosmeticsStore) Equip(_ context.Context, botID, slot, cosmeticID string) (*db.CosmeticItem, error) {
	f.lastBotID, f.lastSlot, f.lastCosmetic = botID, slot, cosmeticID
	return f.equipItem, f.equipErr
}
func (f *fakeCosmeticsStore) AccountInventory(context.Context, string) (*db.CustomerCosmeticsInventory, error) {
	if f.inventory == nil {
		f.inventory = &db.CustomerCosmeticsInventory{}
	}
	return f.inventory, nil
}
func (f *fakeCosmeticsStore) LinkBot(_ context.Context, accountID, botID string) (*db.AccountBot, error) {
	f.lastAccount, f.lastBotID = accountID, botID
	return f.linkBot, f.grantErr
}
func (f *fakeCosmeticsStore) UnlinkBot(_ context.Context, accountID, botID string) (bool, error) {
	f.lastAccount, f.lastBotID = accountID, botID
	return f.revoked, f.revokeErr
}
func (f *fakeCosmeticsStore) AssignLicense(_ context.Context, accountID, licenseID string, botID *string) (*db.CosmeticAssignmentChange, error) {
	f.lastAccount, f.lastLicense = accountID, licenseID
	if botID != nil {
		f.lastBotID = *botID
	}
	return f.assignment, f.grantErr
}
func (f *fakeCosmeticsStore) EquipLicense(_ context.Context, accountID, botID, licenseID string) (*db.CosmeticLicense, error) {
	f.lastAccount, f.lastBotID, f.lastLicense = accountID, botID, licenseID
	return f.license, f.equipErr
}
func (f *fakeCosmeticsStore) GrantLicense(_ context.Context, email, cosmeticID, _, _ string) (*db.CosmeticLicense, bool, error) {
	f.lastAccount, f.lastCosmetic = email, cosmeticID
	return f.license, f.grantCreated, f.grantErr
}
func (f *fakeCosmeticsStore) RevokeLicense(_ context.Context, licenseID string) (*db.CosmeticAssignmentChange, bool, error) {
	f.lastLicense = licenseID
	return f.assignment, f.revoked, f.revokeErr
}

func requestWithBot(method, target string, body []byte, bot *db.Bot) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if bot != nil {
		req = req.WithContext(security.WithBotContext(req.Context(), bot))
	}
	return req
}

func requestWithCustomerParam(method, target string, body []byte, param, value string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add(param, value)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeContext)
	ctx = withCustomerSession(ctx, &CustomerSession{AccountID: "account-1"})
	return req.WithContext(ctx)
}

func requestWithRouteParam(method, target string, body []byte, param, value string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add(param, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
}

func TestCosmeticsCatalogDisclosesCheckoutState(t *testing.T) {
	store := &fakeCosmeticsStore{publicCatalog: &db.CosmeticCatalog{
		Categories: []db.CosmeticCategory{{ID: "starter-packs", Name: "Starter Packs", IsActive: true}},
		Items:      []db.CosmeticItem{{ID: "free", IsFree: true, IsActive: true}},
		Packs: []db.CosmeticPack{{
			ID: "neon-pack", CategoryID: "starter-packs", PriceCents: 99, Currency: "USD",
			IsPurchasable: true, IsActive: true, ItemIDs: []string{"free"},
		}},
	}}
	handler := newCosmeticsHandlerWithStore(store, nil)
	// Staging a sale flag must not advertise working checkout until a payment
	// provider has been explicitly enabled on the handler.
	rec := httptest.NewRecorder()
	handler.Catalog(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cosmetics/catalog", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		CheckoutEnabled bool                  `json:"checkout_enabled"`
		Categories      []db.CosmeticCategory `json:"categories"`
		Packs           []db.CosmeticPack     `json:"packs"`
		Items           []db.CosmeticItem     `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CheckoutEnabled || len(response.Categories) != 1 || len(response.Packs) != 1 || len(response.Items) != 1 {
		t.Fatalf("unexpected catalog response: %+v", response)
	}

	handler.checkoutEnabled = true
	enabled := httptest.NewRecorder()
	handler.Catalog(enabled, httptest.NewRequest(http.MethodGet, "/api/v1/cosmetics/catalog", nil))
	if err := json.Unmarshal(enabled.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.CheckoutEnabled {
		t.Fatal("configured checkout with a purchasable item was not disclosed")
	}
}

func TestAdminCosmeticsCatalogIncludesInactiveEntriesWithoutEnablingCheckout(t *testing.T) {
	store := &fakeCosmeticsStore{adminCatalog: &db.CosmeticCatalog{
		Categories: []db.CosmeticCategory{{ID: "drafts", Name: "Drafts", IsActive: false}},
		Items:      []db.CosmeticItem{{ID: "draft-item", CategoryID: "drafts", IsActive: false}},
		Packs:      []db.CosmeticPack{{ID: "draft-pack", CategoryID: "drafts", PriceCents: 99, Currency: "USD", IsPurchasable: true, IsActive: false}},
	}}
	handler := newCosmeticsHandlerWithStore(store, nil)
	rec := httptest.NewRecorder()
	handler.AdminCatalog(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/cosmetics/catalog", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		CheckoutEnabled bool                  `json:"checkout_enabled"`
		Categories      []db.CosmeticCategory `json:"categories"`
		Packs           []db.CosmeticPack     `json:"packs"`
		Items           []db.CosmeticItem     `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CheckoutEnabled || len(response.Categories) != 1 || len(response.Packs) != 1 || len(response.Items) != 1 {
		t.Fatalf("unexpected admin catalog response: %+v", response)
	}
}

func TestAdminCosmeticCatalogMutationsUsePathIdentityAndSafeAuditActor(t *testing.T) {
	store := &fakeCosmeticsStore{revoked: true}
	handler := newCosmeticsHandlerWithStore(store, nil)

	category := httptest.NewRecorder()
	categoryReq := requestWithRouteParam(http.MethodPut, "/api/v1/admin/cosmetics/categories/event", []byte(`{
		"name":"Event", "description":"Limited event cosmetics", "is_active":true, "sort_order":50
	}`), "category_id", "event")
	categoryReq.Header.Set("X-Admin-Token", "never-store-this-secret")
	handler.UpsertAdminCategory(category, categoryReq)
	if category.Code != http.StatusOK || store.lastActor != "admin-token" {
		t.Fatalf("category mutation status=%d actor=%q body=%s", category.Code, store.lastActor, category.Body.String())
	}

	item := httptest.NewRecorder()
	handler.UpsertAdminItem(item, requestWithRouteParam(http.MethodPut, "/api/v1/admin/cosmetics/items/attachment-event", []byte(`{
		"name":"Event Crown", "description":"Presentation only", "category_id":"event",
		"slot":"attachment", "asset_key":"event_crown", "rarity":"rare", "price_cents":99,
		"currency":"USD", "is_free":false, "is_purchasable":true, "is_active":true, "sort_order":10
	}`), "item_id", "attachment-event"))
	if item.Code != http.StatusOK || store.lastCosmetic != "attachment-event" {
		t.Fatalf("item mutation status=%d id=%q body=%s", item.Code, store.lastCosmetic, item.Body.String())
	}

	pack := httptest.NewRecorder()
	handler.UpsertAdminPack(pack, requestWithRouteParam(http.MethodPut, "/api/v1/admin/cosmetics/packs/event-pack", []byte(`{
		"name":"Event Pack", "description":"A tiny pack", "category_id":"event", "price_cents":99,
		"currency":"USD", "is_free":false, "is_purchasable":true, "is_active":true,
		"sort_order":20, "item_ids":["attachment-event"]
	}`), "pack_id", "event-pack"))
	if pack.Code != http.StatusOK || store.lastCosmetic != "event-pack" {
		t.Fatalf("pack mutation status=%d id=%q body=%s", pack.Code, store.lastCosmetic, pack.Body.String())
	}

	deleted := httptest.NewRecorder()
	handler.DeleteAdminPack(deleted, requestWithRouteParam(http.MethodDelete, "/api/v1/admin/cosmetics/packs/event-pack", nil, "pack_id", "event-pack"))
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"deleted":true`) {
		t.Fatalf("pack delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if strings.Contains(category.Body.String(), "never-store-this-secret") {
		t.Fatal("admin token leaked into mutation response")
	}
}

func TestAdminCosmeticCatalogMutationsValidateAndMapConflicts(t *testing.T) {
	badJSON := httptest.NewRecorder()
	newCosmeticsHandlerWithStore(&fakeCosmeticsStore{}, nil).UpsertAdminCategory(badJSON,
		requestWithRouteParam(http.MethodPut, "/api/v1/admin/cosmetics/categories/event", []byte(`{"name":"Event","unknown":true}`), "category_id", "event"))
	if badJSON.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want 400", badJSON.Code)
	}

	conflictStore := &fakeCosmeticsStore{grantErr: db.ErrCosmeticCatalogConflict}
	conflict := httptest.NewRecorder()
	newCosmeticsHandlerWithStore(conflictStore, nil).UpsertAdminItem(conflict,
		requestWithRouteParam(http.MethodPut, "/api/v1/admin/cosmetics/items/item", []byte(`{
			"name":"Item", "category_id":"event", "slot":"attachment", "asset_key":"item", "rarity":"common",
			"currency":"USD", "is_free":true, "is_active":true
		}`), "item_id", "item"))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("catalog conflict status = %d, want 409; body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestAdminCosmeticAuditCapsLimit(t *testing.T) {
	store := &fakeCosmeticsStore{audit: []db.CosmeticCatalogAudit{{ID: 1, Actor: "admin-token"}}}
	handler := newCosmeticsHandlerWithStore(store, nil)
	rec := httptest.NewRecorder()
	handler.AdminAudit(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/cosmetics/audit?limit=9999", nil))
	if rec.Code != http.StatusOK || store.lastLimit != 200 {
		t.Fatalf("audit status=%d limit=%d body=%s", rec.Code, store.lastLimit, rec.Body.String())
	}
}

func TestRegisterCosmeticsAdminRoutes(t *testing.T) {
	store := &fakeCosmeticsStore{
		adminCatalog: &db.CosmeticCatalog{},
		revoked:      true,
	}
	handler := newCosmeticsHandlerWithStore(store, nil)
	router := chi.NewRouter()
	registerCosmeticsAdminRoutes(router, handler)

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/cosmetics/catalog", ""},
		{http.MethodGet, "/cosmetics/audit", ""},
		{http.MethodPut, "/cosmetics/categories/event", `{"name":"Event","is_active":true}`},
		{http.MethodDelete, "/cosmetics/categories/event", ""},
		{http.MethodPut, "/cosmetics/items/event-item", `{
			"name":"Event Item","category_id":"event","slot":"attachment","asset_key":"event_item",
			"rarity":"common","currency":"USD","is_free":true,"is_active":true
		}`},
		{http.MethodDelete, "/cosmetics/items/event-item", ""},
		{http.MethodPut, "/cosmetics/packs/event-pack", `{
			"name":"Event Pack","category_id":"event","price_cents":99,"currency":"USD",
			"is_purchasable":true,"is_active":true,"item_ids":["event-item"]
		}`},
		{http.MethodDelete, "/cosmetics/packs/event-pack", ""},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestEquipCosmeticRequiresOwnershipAndAuth(t *testing.T) {
	store := &fakeCosmeticsStore{equipErr: db.ErrCosmeticNotOwned}
	handler := newCosmeticsHandlerWithStore(store, nil)
	body := []byte(`{"slot":"weapon_skin","cosmetic_id":"weapon-solar-flare"}`)

	unauth := httptest.NewRecorder()
	handler.Equip(unauth, requestWithBot(http.MethodPut, "/api/v1/bot/cosmetics", body, nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauth.Code)
	}

	forbidden := httptest.NewRecorder()
	handler.Equip(forbidden, requestWithBot(http.MethodPut, "/api/v1/bot/cosmetics", body, &db.Bot{ID: "bot-1"}))
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("unowned status = %d, want 403; body=%s", forbidden.Code, forbidden.Body.String())
	}
}

func TestEquipCosmeticRefreshesConnectedBotVisuals(t *testing.T) {
	store := &fakeCosmeticsStore{
		equipItem: &db.CosmeticItem{ID: "attachment-signal-antenna", Slot: db.CosmeticSlotAttachment, AssetKey: "signal_antenna"},
		equipped:  map[string]string{db.CosmeticSlotAttachment: "signal_antenna"},
	}
	engine := game.NewGameEngine()
	engine.Bots["bot-1"] = &game.BotState{BotID: "bot-1"}
	handler := newCosmeticsHandlerWithStore(store, engine)
	body := []byte(`{"slot":"attachment","cosmetic_id":"attachment-signal-antenna"}`)

	rec := httptest.NewRecorder()
	handler.Equip(rec, requestWithBot(http.MethodPut, "/api/v1/bot/cosmetics", body, &db.Bot{ID: "bot-1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := engine.Bots["bot-1"].Cosmetics[db.CosmeticSlotAttachment]; got != "signal_antenna" {
		t.Fatalf("live cosmetic = %q, want signal_antenna", got)
	}
}

func TestGrantCosmeticIsIdempotentAndValidated(t *testing.T) {
	store := &fakeCosmeticsStore{grantCreated: false}
	handler := newCosmeticsHandlerWithStore(store, nil)

	bad := httptest.NewRecorder()
	handler.Grant(bad, httptest.NewRequest(http.MethodPost, "/api/v1/admin/cosmetics/grants",
		bytes.NewBufferString(`{"email":"owner@example.com","cosmetic_id":"skin-neon-grid","source":"INVALID SOURCE"}`)))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid source status = %d, want 400", bad.Code)
	}

	good := httptest.NewRecorder()
	handler.Grant(good, httptest.NewRequest(http.MethodPost, "/api/v1/admin/cosmetics/grants",
		bytes.NewBufferString(`{"email":"owner@example.com","cosmetic_id":"skin-neon-grid","source":"stripe","external_reference":"evt_123"}`)))
	if good.Code != http.StatusOK {
		t.Fatalf("idempotent grant status = %d, body=%s", good.Code, good.Body.String())
	}
}

func TestEquipCosmeticMapsStorageFailure(t *testing.T) {
	store := &fakeCosmeticsStore{equipErr: errors.New("boom")}
	handler := newCosmeticsHandlerWithStore(store, nil)
	rec := httptest.NewRecorder()
	handler.Equip(rec, requestWithBot(http.MethodPut, "/api/v1/bot/cosmetics",
		[]byte(`{"slot":"bot_skin","cosmetic_id":"skin-standard"}`), &db.Bot{ID: "bot-1"}))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestCustomerCosmeticMutationsRejectInactiveBotKey(t *testing.T) {
	t.Run("assignment", func(t *testing.T) {
		store := &fakeCosmeticsStore{grantErr: db.ErrCustomerBotKeyInactive}
		handler := newCosmeticsHandlerWithStore(store, nil)
		rec := httptest.NewRecorder()
		handler.AssignAccountLicense(rec, requestWithCustomerParam(
			http.MethodPut,
			"/api/v1/account/cosmetic-licenses/license-1/assignment",
			[]byte(`{"bot_id":"bot-inactive"}`),
			"license_id", "license-1",
		))
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("equip", func(t *testing.T) {
		store := &fakeCosmeticsStore{equipErr: db.ErrCustomerBotKeyInactive}
		handler := newCosmeticsHandlerWithStore(store, nil)
		rec := httptest.NewRecorder()
		handler.EquipAccountLicense(rec, requestWithCustomerParam(
			http.MethodPut,
			"/api/v1/account/bots/bot-inactive/cosmetics",
			[]byte(`{"license_id":"license-1"}`),
			"bot_id", "bot-inactive",
		))
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestCustomerCosmeticMutationsHideForeignLicense(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*CosmeticsHandler, *httptest.ResponseRecorder)
	}{
		{
			name: "assignment",
			invoke: func(handler *CosmeticsHandler, rec *httptest.ResponseRecorder) {
				handler.AssignAccountLicense(rec, requestWithCustomerParam(
					http.MethodPut,
					"/api/v1/account/cosmetic-licenses/foreign-license/assignment",
					[]byte(`{"bot_id":"bot-1"}`),
					"license_id", "foreign-license",
				))
			},
		},
		{
			name: "equip",
			invoke: func(handler *CosmeticsHandler, rec *httptest.ResponseRecorder) {
				handler.EquipAccountLicense(rec, requestWithCustomerParam(
					http.MethodPut,
					"/api/v1/account/bots/bot-1/cosmetics",
					[]byte(`{"license_id":"foreign-license"}`),
					"bot_id", "bot-1",
				))
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeCosmeticsStore{grantErr: db.ErrCosmeticLicenseNotOwned, equipErr: db.ErrCosmeticLicenseNotOwned}
			handler := newCosmeticsHandlerWithStore(store, nil)
			rec := httptest.NewRecorder()
			tc.invoke(handler, rec)
			if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "not owned") {
				t.Fatalf("foreign license response status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
