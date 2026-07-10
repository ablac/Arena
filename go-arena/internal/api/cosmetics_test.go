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
	catalog      []db.CosmeticItem
	items        []db.BotCosmeticItem
	equipped     map[string]string
	equipItem    *db.CosmeticItem
	equipErr     error
	grantCreated bool
	grantErr     error
	revoked      bool
	revokeErr    error
	inventory    *db.CustomerCosmeticsInventory
	linkBot      *db.AccountBot
	assignment   *db.CosmeticAssignmentChange
	license      *db.CosmeticLicense
	lastBotID    string
	lastAccount  string
	lastLicense  string
	lastSlot     string
	lastCosmetic string
}

func (f *fakeCosmeticsStore) ListCatalog(context.Context) ([]db.CosmeticItem, error) {
	return f.catalog, nil
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

func TestCosmeticsCatalogDisclosesCheckoutState(t *testing.T) {
	store := &fakeCosmeticsStore{catalog: []db.CosmeticItem{
		{ID: "free", IsFree: true, IsActive: true},
		{ID: "paid", PriceCents: 299, IsPurchasable: true, IsActive: true},
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
		CheckoutEnabled bool              `json:"checkout_enabled"`
		Items           []db.CosmeticItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CheckoutEnabled || len(response.Items) != 2 {
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
