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
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/go-chi/chi/v5"
)

type fakeCosmeticsStore struct {
	publicCatalog      *db.CosmeticCatalog
	adminCatalog       *db.CosmeticCatalog
	audit              []db.CosmeticCatalogAudit
	items              []db.BotCosmeticItem
	equipped           map[string]string
	equippedErr        error
	equippedBotIDs     []string
	equipItem          *db.CosmeticItem
	equipErr           error
	grantCreated       bool
	grantErr           error
	revoked            bool
	revokeErr          error
	inventory          *db.CustomerCosmeticsInventory
	linkBot            *db.AccountBot
	assignment         *db.CosmeticAssignmentChange
	license            *db.CosmeticLicense
	lastBotID          string
	lastAccount        string
	lastLicense        string
	lastSlot           string
	lastCosmetic       string
	lastActor          string
	lastLimit          int
	adminAccess        *db.CosmeticAdminAccess
	membership         *db.CosmeticAdminMembership
	licensesMade       int
	affectedBots       []string
	lastEmail          string
	lastNote           string
	lastReason         string
	lastMembership     string
	lastExpiry         time.Time
	lastSource         string
	lastReference      string
	expiredMemberships int
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
func (f *fakeCosmeticsStore) Equipped(_ context.Context, botID string) (map[string]string, error) {
	f.equippedBotIDs = append(f.equippedBotIDs, botID)
	return f.equipped, f.equippedErr
}
func (f *fakeCosmeticsStore) Equip(_ context.Context, botID, slot, cosmeticID string) (*db.CosmeticItem, error) {
	f.lastBotID, f.lastSlot, f.lastCosmetic = botID, slot, cosmeticID
	return f.equipItem, f.equipErr
}

func (f *fakeCosmeticsStore) AccountInventory(_ context.Context, accountID string) (*db.CustomerCosmeticsInventory, error) {
	f.lastAccount = accountID
	if f.inventory == nil {
		f.inventory = &db.CustomerCosmeticsInventory{}
	}
	return f.inventory, nil
}

func TestAccountCosmeticsInventoryAccountQuotaStopsStoreWork(t *testing.T) {
	store := &fakeCosmeticsStore{}
	handler := newCosmeticsHandlerWithStore(store, nil)
	var quotaAccount string
	var quotaLimit int
	handler.checkAccountInventoryQuota = func(_ context.Context, accountID string, limit int) (bool, error) {
		quotaAccount, quotaLimit = accountID, limit
		return false, nil
	}
	previous := config.C
	t.Cleanup(func() { config.C = previous })
	config.C.CosmeticsAccountReadRPM = 60
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/account/cosmetics", nil)
	request = request.WithContext(withCustomerSession(request.Context(), &CustomerSession{AccountID: "account-quota"}))
	handler.AccountInventory(recorder, request)
	if recorder.Code != http.StatusTooManyRequests || quotaAccount != "account-quota" || quotaLimit != 60 || store.lastAccount != "" {
		t.Fatalf("inventory quota status=%d quota=%q/%d store=%q body=%s",
			recorder.Code, quotaAccount, quotaLimit, store.lastAccount, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"ACCOUNT_COSMETICS_RATE_LIMIT"`) {
		t.Fatalf("inventory quota response=%s", recorder.Body.String())
	}
}

func TestAccountCosmeticsInventoryReconcilesTimedMembershipBeforeReadback(t *testing.T) {
	store := &fakeCosmeticsStore{
		inventory:          &db.CustomerCosmeticsInventory{},
		expiredMemberships: 1,
	}
	handler := newCosmeticsHandlerWithStore(store, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/account/cosmetics", nil)
	request = request.WithContext(withCustomerSession(request.Context(), &CustomerSession{
		AccountID: "account-expiry",
		Email:     "Member@Example.com",
	}))
	handler.AccountInventory(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.lastEmail != "Member@Example.com" || store.lastAccount != "account-expiry" {
		t.Fatalf("expiry/readback order email=%q account=%q", store.lastEmail, store.lastAccount)
	}
}

func TestAccountCosmeticsInventoryExpiryRefreshIsScopedAndNonBlocking(t *testing.T) {
	store := &fakeCosmeticsStore{
		inventory:          &db.CustomerCosmeticsInventory{},
		expiredMemberships: 1,
		affectedBots:       []string{"affected-bot"},
		equippedErr:        errors.New("temporary visual cache read failure"),
	}
	engine := game.NewGameEngine()
	engine.Bots["affected-bot"] = &game.BotState{BotID: "affected-bot"}
	engine.Bots["unrelated-bot"] = &game.BotState{BotID: "unrelated-bot"}
	handler := newCosmeticsHandlerWithStore(store, engine)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/account/cosmetics", nil)
	request = request.WithContext(withCustomerSession(request.Context(), &CustomerSession{
		AccountID: "account-expiry",
		Email:     "member@example.com",
	}))

	handler.AccountInventory(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := strings.Join(store.equippedBotIDs, ","); got != "affected-bot" {
		t.Fatalf("visual refresh bots=%q, want only affected-bot", got)
	}
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
func (f *fakeCosmeticsStore) GrantLicense(_ context.Context, email, cosmeticID, source, reference string) (*db.CosmeticLicense, bool, error) {
	f.lastAccount, f.lastCosmetic, f.lastSource, f.lastReference = email, cosmeticID, source, reference
	return f.license, f.grantCreated, f.grantErr
}
func (f *fakeCosmeticsStore) RevokeLicense(_ context.Context, licenseID string) (*db.CosmeticAssignmentChange, bool, error) {
	f.lastLicense = licenseID
	return f.assignment, f.revoked, f.revokeErr
}

func (f *fakeCosmeticsStore) AdminAccess(_ context.Context, email string) (*db.CosmeticAdminAccess, error) {
	f.lastEmail = email
	return f.adminAccess, f.grantErr
}

func (f *fakeCosmeticsStore) CreateAdminMembership(
	_ context.Context, email string, expiresAt time.Time, note, actor string,
) (*db.CosmeticAdminMembership, int, error) {
	f.lastEmail, f.lastExpiry, f.lastNote, f.lastActor = email, expiresAt, note, actor
	return f.membership, f.licensesMade, f.grantErr
}

func (f *fakeCosmeticsStore) RevokeAdminMembership(
	_ context.Context, membershipID, actor, reason string,
) (*db.CosmeticAdminMembership, []string, bool, error) {
	f.lastMembership, f.lastActor, f.lastReason = membershipID, actor, reason
	return f.membership, f.affectedBots, f.revoked, f.revokeErr
}

func (f *fakeCosmeticsStore) ExpireAdminMembershipsForEmail(
	_ context.Context, email string, _ time.Time,
) (int, []string, error) {
	f.lastEmail = email
	return f.expiredMemberships, f.affectedBots, f.revokeErr
}

func requestWithBot(method, target string, body []byte, bot *db.Bot) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if bot != nil {
		req = req.WithContext(security.WithBotContext(req.Context(), bot))
	}
	return req
}

func TestCosmeticAdminActorUsesAuthenticatedPrincipal(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/cosmetics/catalog", nil)
	request = request.WithContext(withAdminPrincipal(request.Context(), "oidc:operator@example.com"))
	if actor := cosmeticAdminActor(request); actor != "oidc:operator@example.com" {
		t.Fatalf("actor=%q", actor)
	}
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

func TestLinkAccountBotMapsDurableKeyOwnershipConflicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "owned by another account", err: db.ErrCustomerAPIKeyAlreadyOwned},
		{name: "active key limit", err: db.ErrCustomerAPIKeyLimit},
		{name: "lifetime history limit", err: db.ErrCustomerAPIKeyHistoryLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeCosmeticsStore{grantErr: tc.err}
			handler := newCosmeticsHandlerWithStore(store, nil)
			handler.verifyAPIKey = func(context.Context, string) (*db.Bot, error) {
				return &db.Bot{ID: "bot-1", APIKeyID: "key-1"}, nil
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/account/bots", strings.NewReader(`{"api_key":"arena_valid"}`))
			req = req.WithContext(withCustomerSession(req.Context(), &CustomerSession{AccountID: "account-1"}))
			rec := httptest.NewRecorder()

			handler.LinkAccountBot(rec, req)

			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusConflict, rec.Body.String())
			}
		})
	}
}

func TestLinkAccountBotRejectsOversizedBodyBeforeQuotaOrBcrypt(t *testing.T) {
	handler := newCosmeticsHandlerWithStore(&fakeCosmeticsStore{}, nil)
	quotaCalls, verifyCalls := 0, 0
	handler.consumeAccountKeyQuota = func(context.Context, string, db.AccountAPIKeyQuotaAction, int) (bool, int, error) {
		quotaCalls++
		return true, 1, nil
	}
	handler.verifyAPIKey = func(context.Context, string) (*db.Bot, error) {
		verifyCalls++
		return nil, errors.New("should not run")
	}
	body := `{"api_key":"` + strings.Repeat("x", 8<<10) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/bots", strings.NewReader(body))
	req = req.WithContext(withCustomerSession(req.Context(), &CustomerSession{AccountID: "account-1"}))
	recorder := httptest.NewRecorder()

	handler.LinkAccountBot(recorder, req)

	if recorder.Code != http.StatusBadRequest || quotaCalls != 0 || verifyCalls != 0 {
		t.Fatalf("oversized link = status %d quota=%d verify=%d body=%s", recorder.Code, quotaCalls, verifyCalls, recorder.Body.String())
	}
}

func TestLinkAccountBotQuotaIsPerAccountAcrossSourceIPsAndRunsBeforeBcrypt(t *testing.T) {
	previous := config.C.CustomerBotLinkPerHour
	config.C.CustomerBotLinkPerHour = 1
	t.Cleanup(func() { config.C.CustomerBotLinkPerHour = previous })

	handler := newCosmeticsHandlerWithStore(&fakeCosmeticsStore{linkBot: &db.AccountBot{BotID: "bot-1"}}, nil)
	quotaCount, verifyCalls := 0, 0
	handler.consumeAccountKeyQuota = func(_ context.Context, accountID string, action db.AccountAPIKeyQuotaAction, limit int) (bool, int, error) {
		if accountID != "account-1" || action != db.AccountAPIKeyQuotaLink || limit != 1 {
			t.Fatalf("quota input account=%q action=%q limit=%d", accountID, action, limit)
		}
		if quotaCount >= limit {
			return false, 0, nil
		}
		quotaCount++
		return true, 0, nil
	}
	handler.verifyAPIKey = func(context.Context, string) (*db.Bot, error) {
		verifyCalls++
		return &db.Bot{ID: "bot-1", APIKeyID: "key-1"}, nil
	}

	for index, remote := range []string{"198.51.100.10:1000", "203.0.113.20:2000"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/account/bots", strings.NewReader(`{"api_key":"arena_valid"}`))
		req.RemoteAddr = remote
		req = req.WithContext(withCustomerSession(req.Context(), &CustomerSession{AccountID: "account-1"}))
		recorder := httptest.NewRecorder()
		handler.LinkAccountBot(recorder, req)
		if index == 0 && recorder.Code != http.StatusOK {
			t.Fatalf("first link = %d %s", recorder.Code, recorder.Body.String())
		}
		if index == 1 && (recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "ACCOUNT_BOT_LINK_RATE_LIMIT")) {
			t.Fatalf("second link = %d %s", recorder.Code, recorder.Body.String())
		}
	}
	if verifyCalls != 1 {
		t.Fatalf("bcrypt verification calls = %d, want 1", verifyCalls)
	}
}

func TestCosmeticsCatalogDisclosesCheckoutState(t *testing.T) {
	store := &fakeCosmeticsStore{publicCatalog: &db.CosmeticCatalog{
		Categories: []db.CosmeticCategory{{ID: "starter-packs", Name: "Starter Packs", IsActive: true}},
		Items:      []db.CosmeticItem{{ID: "free", CategoryID: "starter-packs", Slot: db.CosmeticSlotAttachment, IsFree: true, IsActive: true}},
		Packs: []db.CosmeticPack{{
			ID: "neon-pack", CategoryID: "starter-packs", PriceCents: db.CosmeticPackPriceCents, Currency: "USD",
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
		CheckoutEnabled   bool                         `json:"checkout_enabled"`
		SubscriptionOffer db.CosmeticSubscriptionOffer `json:"subscription_offer"`
		Categories        []db.CosmeticCategory        `json:"categories"`
		Packs             []db.CosmeticPack            `json:"packs"`
		Items             []db.CosmeticItem            `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CheckoutEnabled || response.SubscriptionOffer.Enabled || response.SubscriptionOffer.PriceCents != 1999 ||
		response.SubscriptionOffer.Currency != "USD" || response.SubscriptionOffer.Interval != "month" ||
		!response.SubscriptionOffer.IncludesFutureSets || response.SubscriptionOffer.MaxAPIKeys != 5 ||
		len(response.Categories) != 1 || len(response.Packs) != 1 || len(response.Items) != 1 {
		t.Fatalf("unexpected catalog response: %+v", response)
	}

	handler.checkoutEnabled = true
	enabled := httptest.NewRecorder()
	handler.Catalog(enabled, httptest.NewRequest(http.MethodGet, "/api/v1/cosmetics/catalog", nil))
	if err := json.Unmarshal(enabled.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.CheckoutEnabled || !response.SubscriptionOffer.Enabled {
		t.Fatal("configured checkout with a purchasable pack was not disclosed")
	}

	store.publicCatalog.Packs[0].PriceCents = 299
	corruptPrice := httptest.NewRecorder()
	handler.Catalog(corruptPrice, httptest.NewRequest(http.MethodGet, "/api/v1/cosmetics/catalog", nil))
	if err := json.Unmarshal(corruptPrice.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CheckoutEnabled {
		t.Fatal("catalog advertised checkout for a stale non-$1.99 pack price")
	}
	store.publicCatalog.Packs[0].PriceCents = db.CosmeticPackPriceCents

	// Launch checkout sells packs only. A stray item-level sale flag must not
	// advertise an open shop when no pack can actually be checked out.
	store.publicCatalog.Packs = nil
	store.publicCatalog.Items[0].IsPurchasable = true
	itemOnly := httptest.NewRecorder()
	handler.Catalog(itemOnly, httptest.NewRequest(http.MethodGet, "/api/v1/cosmetics/catalog", nil))
	if err := json.Unmarshal(itemOnly.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CheckoutEnabled {
		t.Fatal("item-only sale flag enabled pack checkout without a purchasable pack")
	}
}

func TestCosmeticsCatalogDisclosesNinetyNineCentTrailCheckout(t *testing.T) {
	store := &fakeCosmeticsStore{publicCatalog: &db.CosmeticCatalog{
		Categories: []db.CosmeticCategory{{ID: db.CosmeticTrailCategoryID, Name: "Trails", IsActive: true}},
		Items: []db.CosmeticItem{{
			ID: "trail-ember-sparks", CategoryID: db.CosmeticTrailCategoryID, Slot: db.CosmeticSlotTrail,
			AssetKey: "ember_sparks", IsActive: true,
		}},
		Packs: []db.CosmeticPack{{
			ID: "trail-ember-sparks-pack", CategoryID: db.CosmeticTrailCategoryID,
			PriceCents: db.CosmeticTrailPriceCents, Currency: "USD", IsPurchasable: true, IsActive: true,
			ItemIDs: []string{"trail-ember-sparks"},
		}},
	}}
	handler := newCosmeticsHandlerWithStore(store, nil)
	handler.checkoutEnabled = true
	recorder := httptest.NewRecorder()
	handler.Catalog(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/cosmetics/catalog", nil))
	var response struct {
		CheckoutEnabled bool `json:"checkout_enabled"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.CheckoutEnabled {
		t.Fatal("catalog hid checkout for a valid one-item $0.99 trail product")
	}

	store.publicCatalog.Items[0].Slot = db.CosmeticSlotAttachment
	malformed := httptest.NewRecorder()
	handler.Catalog(malformed, httptest.NewRequest(http.MethodGet, "/api/v1/cosmetics/catalog", nil))
	if err := json.Unmarshal(malformed.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CheckoutEnabled {
		t.Fatal("malformed Trails product advertised checkout for a non-trail item")
	}
	store.publicCatalog.Items[0].Slot = db.CosmeticSlotTrail
	store.publicCatalog.Packs[0].CategoryID = "starter-packs"
	store.publicCatalog.Categories = append(store.publicCatalog.Categories,
		db.CosmeticCategory{ID: "starter-packs", Name: "Starter Packs", IsActive: true})
	store.publicCatalog.Packs[0].PriceCents = db.CosmeticPackPriceCents
	malformedSet := httptest.NewRecorder()
	handler.Catalog(malformedSet, httptest.NewRequest(http.MethodGet, "/api/v1/cosmetics/catalog", nil))
	if err := json.Unmarshal(malformedSet.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CheckoutEnabled {
		t.Fatal("non-trail product advertised checkout while containing a trail item")
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
		"slot":"attachment", "asset_key":"signal_antenna", "rarity":"rare", "price_cents":99,
		"currency":"USD", "is_free":false, "is_purchasable":true, "is_active":true, "sort_order":10
	}`), "item_id", "attachment-event"))
	if item.Code != http.StatusOK || store.lastCosmetic != "attachment-event" {
		t.Fatalf("item mutation status=%d id=%q body=%s", item.Code, store.lastCosmetic, item.Body.String())
	}

	pack := httptest.NewRecorder()
	handler.UpsertAdminPack(pack, requestWithRouteParam(http.MethodPut, "/api/v1/admin/cosmetics/packs/event-pack", []byte(`{
		"name":"Event Pack", "description":"A tiny pack", "category_id":"event", "price_cents":199,
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

func TestAdminCosmeticItemMutationRefreshesConnectedBotVisuals(t *testing.T) {
	engine := game.NewGameEngine()
	engine.Bots["bot-active"] = &game.BotState{
		BotID: "bot-active", Cosmetics: map[string]string{db.CosmeticSlotBotSkin: "neon_grid"},
	}
	engine.WaitingBots["bot-waiting"] = &game.BotState{
		BotID: "bot-waiting", Cosmetics: map[string]string{db.CosmeticSlotBotSkin: "neon_grid"},
	}
	// This mirrors the DB projection after the item is deactivated: the
	// previously equipped asset is no longer resolved for either live bot.
	store := &fakeCosmeticsStore{equipped: map[string]string{}}
	handler := newCosmeticsHandlerWithStore(store, engine)

	recorder := httptest.NewRecorder()
	handler.UpsertAdminItem(recorder, requestWithRouteParam(
		http.MethodPut,
		"/api/v1/admin/cosmetics/items/skin-neon-grid",
		[]byte(`{
			"name":"Neon Grid Chassis", "description":"Presentation only", "category_id":"chassis",
			"slot":"bot_skin", "asset_key":"neon_grid", "rarity":"rare", "price_cents":499,
			"currency":"USD", "is_free":false, "is_purchasable":false, "is_active":false, "sort_order":20
		}`),
		"item_id",
		"skin-neon-grid",
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	for _, bot := range []*game.BotState{engine.Bots["bot-active"], engine.WaitingBots["bot-waiting"]} {
		if len(bot.Cosmetics) != 0 {
			t.Fatalf("bot %s retained stale cosmetics after admin mutation: %+v", bot.BotID, bot.Cosmetics)
		}
	}
}

func TestAdminCosmeticItemDeletionRefreshesConnectedBotVisuals(t *testing.T) {
	engine := game.NewGameEngine()
	engine.Bots["bot-active"] = &game.BotState{
		BotID: "bot-active", Cosmetics: map[string]string{db.CosmeticSlotAttachment: "arena_set_003_ember_vanguard"},
	}
	store := &fakeCosmeticsStore{revoked: true, equipped: map[string]string{}}
	handler := newCosmeticsHandlerWithStore(store, engine)
	recorder := httptest.NewRecorder()
	handler.DeleteAdminItem(
		recorder,
		requestWithRouteParam(http.MethodDelete, "/api/v1/admin/cosmetics/items/custom", nil, "item_id", "custom"),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(engine.Bots["bot-active"].Cosmetics) != 0 {
		t.Fatalf("connected bot retained deleted cosmetic: %+v", engine.Bots["bot-active"].Cosmetics)
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
			"name":"Item", "category_id":"event", "slot":"attachment", "asset_key":"signal_antenna", "rarity":"common",
			"currency":"USD", "is_free":true, "is_active":true
		}`), "item_id", "item"))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("catalog conflict status = %d, want 409; body=%s", conflict.Code, conflict.Body.String())
	}

	builtinStore := &fakeCosmeticsStore{revokeErr: db.ErrCosmeticCatalogBuiltin}
	builtin := httptest.NewRecorder()
	newCosmeticsHandlerWithStore(builtinStore, nil).DeleteAdminItem(
		builtin,
		requestWithRouteParam(http.MethodDelete, "/api/v1/admin/cosmetics/items/skin-standard", nil, "item_id", "skin-standard"),
	)
	if builtin.Code != http.StatusConflict || !strings.Contains(builtin.Body.String(), "deactivate") {
		t.Fatalf("built-in delete status=%d body=%s", builtin.Code, builtin.Body.String())
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
			"name":"Event Item","category_id":"event","slot":"attachment","asset_key":"signal_antenna",
			"rarity":"common","currency":"USD","is_free":true,"is_active":true
		}`},
		{http.MethodDelete, "/cosmetics/items/event-item", ""},
		{http.MethodPut, "/cosmetics/packs/event-pack", `{
			"name":"Event Pack","category_id":"event","price_cents":199,"currency":"USD",
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
		bytes.NewBufferString(`{"email":"owner@example.com","cosmetic_id":"skin-neon-grid","source":"manual","external_reference":"evt_123"}`)))
	if good.Code != http.StatusOK {
		t.Fatalf("idempotent grant status = %d, body=%s", good.Code, good.Body.String())
	}

	manualStore := &fakeCosmeticsStore{grantCreated: true, license: &db.CosmeticLicense{ID: "manual-license"}}
	manual := httptest.NewRecorder()
	newCosmeticsHandlerWithStore(manualStore, nil).Grant(manual, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/cosmetics/grants",
		bytes.NewBufferString(`{"email":"owner@example.com","cosmetic_id":"skin-neon-grid","external_reference":"support-123"}`),
	))
	if manual.Code != http.StatusCreated || manualStore.lastSource != "manual" || manualStore.lastReference != "support-123" {
		t.Fatalf("default manual grant status=%d source=%q reference=%q body=%s",
			manual.Code, manualStore.lastSource, manualStore.lastReference, manual.Body.String())
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
