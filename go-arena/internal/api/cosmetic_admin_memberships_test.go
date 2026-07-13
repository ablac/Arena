package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"arena-server/internal/db"
	"arena-server/internal/game"

	"github.com/go-chi/chi/v5"
)

func adminMembershipTestRouter(store *fakeCosmeticsStore) http.Handler {
	router := chi.NewRouter()
	registerCosmeticsAdminRoutes(router, newCosmeticsHandlerWithStore(store, nil))
	return router
}

func TestAdminCosmeticAccessLooksUpNormalizedEmail(t *testing.T) {
	store := &fakeCosmeticsStore{adminAccess: &db.CosmeticAdminAccess{
		Account:     db.CustomerAccount{ID: "account-1", Email: "owner@example.com"},
		Memberships: []db.CosmeticAdminMembership{{ID: "membership-1", Status: "active"}},
	}}
	recorder := httptest.NewRecorder()
	adminMembershipTestRouter(store).ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, "/cosmetics/access?email="+url.QueryEscape(" Owner@Example.com "), nil,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.lastEmail != "owner@example.com" {
		t.Fatalf("store email=%q, want normalized email", store.lastEmail)
	}
	var response db.CosmeticAdminAccess
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode access: %v", err)
	}
	if response.Account.Email != "owner@example.com" || len(response.Memberships) != 1 {
		t.Fatalf("access response=%+v", response)
	}
}

func TestAdminCosmeticMembershipAcceptsDurationOrExactExpiryAndRecordsActor(t *testing.T) {
	exactExpiry := time.Now().UTC().Add(180 * 24 * time.Hour).Truncate(time.Second)
	offsetExpiry := exactExpiry.In(time.FixedZone("UTC-05", -5*60*60))
	tests := []struct {
		name       string
		body       string
		wantExpiry func(time.Time) bool
	}{
		{
			name: "duration days",
			body: `{"email":" Member@Example.com ","duration_days":30,"note":"Community prize"}`,
			wantExpiry: func(expiry time.Time) bool {
				remaining := time.Until(expiry)
				return remaining > 30*24*time.Hour-time.Minute && remaining <= 30*24*time.Hour+time.Minute
			},
		},
		{
			name: "exact expiry",
			body: `{"email":"member@example.com","expires_at":"` + exactExpiry.Format(time.RFC3339) + `","note":"Support credit"}`,
			wantExpiry: func(expiry time.Time) bool {
				return expiry.Equal(exactExpiry)
			},
		},
		{
			name: "exact expiry with offset is stored as UTC",
			body: `{"email":"member@example.com","expires_at":"` + offsetExpiry.Format(time.RFC3339) + `","note":"Support credit"}`,
			wantExpiry: func(expiry time.Time) bool {
				return expiry.Equal(offsetExpiry) && expiry.Location() == time.UTC
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeCosmeticsStore{
				membership:   &db.CosmeticAdminMembership{ID: "membership-1", Status: "active"},
				licensesMade: 300,
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/cosmetics/memberships", strings.NewReader(test.body))
			request.Header.Set("X-Admin-Token", "never-persist-this-token")
			adminMembershipTestRouter(store).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusCreated {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if store.lastEmail != "member@example.com" || store.lastActor != "admin-token" || !test.wantExpiry(store.lastExpiry) {
				t.Fatalf("store email=%q actor=%q expiry=%s", store.lastEmail, store.lastActor, store.lastExpiry)
			}
			if strings.Contains(recorder.Body.String(), "never-persist-this-token") ||
				!strings.Contains(recorder.Body.String(), `"licenses_created":300`) {
				t.Fatalf("unsafe or incomplete response: %s", recorder.Body.String())
			}
		})
	}
}

func TestAdminCosmeticMembershipRejectsAmbiguousInvalidOrOverlongExpiry(t *testing.T) {
	future := time.Now().UTC().Add(6 * 365 * 24 * time.Hour).Format(time.RFC3339)
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	tests := []string{
		`{"email":"member@example.com"}`,
		`{"email":"member@example.com","duration_days":0}`,
		`{"email":"member@example.com","duration_days":30,"expires_at":"2027-01-02T03:04:05Z"}`,
		`{"email":"member@example.com","expires_at":"` + past + `"}`,
		`{"email":"member@example.com","expires_at":"` + future + `"}`,
		`{"email":"not-an-email","duration_days":30}`,
		`{"email":"member@example.com","duration_days":30,"unknown":true}`,
	}
	for _, body := range tests {
		store := &fakeCosmeticsStore{}
		recorder := httptest.NewRecorder()
		adminMembershipTestRouter(store).ServeHTTP(recorder, httptest.NewRequest(
			http.MethodPost, "/cosmetics/memberships", strings.NewReader(body),
		))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("body=%s status=%d response=%s", body, recorder.Code, recorder.Body.String())
		}
		if store.lastEmail != "" {
			t.Errorf("invalid request reached store with email %q", store.lastEmail)
		}
	}
}

func TestAdminCosmeticMembershipRevocationUsesPathIdentityReasonAndActor(t *testing.T) {
	store := &fakeCosmeticsStore{
		membership: &db.CosmeticAdminMembership{ID: "membership-1", Status: "revoked"},
		revoked:    true, affectedBots: []string{"bot-1"},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodDelete, "/cosmetics/memberships/membership-1", strings.NewReader(`{"reason":"Prize rescinded"}`),
	)
	request.Header.Set("X-Admin-Token", "never-persist-this-token")
	adminMembershipTestRouter(store).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.lastMembership != "membership-1" || store.lastReason != "Prize rescinded" || store.lastActor != "admin-token" {
		t.Fatalf("revoke store membership=%q reason=%q actor=%q", store.lastMembership, store.lastReason, store.lastActor)
	}
	if !strings.Contains(recorder.Body.String(), `"revoked":true`) || strings.Contains(recorder.Body.String(), "never-persist-this-token") {
		t.Fatalf("unsafe or incomplete response: %s", recorder.Body.String())
	}
}

func TestAdminCosmeticMembershipCollectionRevocationUsesBodyIdentity(t *testing.T) {
	store := &fakeCosmeticsStore{membership: &db.CosmeticAdminMembership{ID: "membership-body"}, revoked: true}
	recorder := httptest.NewRecorder()
	adminMembershipTestRouter(store).ServeHTTP(recorder, httptest.NewRequest(
		http.MethodDelete, "/cosmetics/memberships",
		strings.NewReader(`{"membership_id":"membership-body","reason":"Support correction"}`),
	))
	if recorder.Code != http.StatusOK || store.lastMembership != "membership-body" || store.lastReason != "Support correction" {
		t.Fatalf("collection revoke status=%d membership=%q reason=%q body=%s",
			recorder.Code, store.lastMembership, store.lastReason, recorder.Body.String())
	}
}

func TestAdminRejectsDirectMembershipLicenseRevocation(t *testing.T) {
	store := &fakeCosmeticsStore{revokeErr: db.ErrCosmeticAdminMembershipLicense}
	recorder := httptest.NewRecorder()
	adminMembershipTestRouter(store).ServeHTTP(recorder, httptest.NewRequest(
		http.MethodDelete, "/cosmetics/licenses/membership-license", nil,
	))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminMembershipRevokeCommitsAndQueuesFailedVisualRepair(t *testing.T) {
	store := &fakeCosmeticsStore{
		membership:   &db.CosmeticAdminMembership{ID: "membership-retry", Status: "revoked"},
		revoked:      true,
		affectedBots: []string{"bot-retry"},
		equippedErr:  errors.New("temporary database read failure"),
	}
	engine := game.NewGameEngine()
	engine.Bots["bot-retry"] = &game.BotState{BotID: "bot-retry"}
	router := chi.NewRouter()
	registerCosmeticsAdminRoutes(router, newCosmeticsHandlerWithStore(store, engine))

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(
		http.MethodDelete, "/cosmetics/memberships/membership-retry", nil,
	))
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"live_refresh_failures":1`) {
		t.Fatalf("first revoke status=%d body=%s", first.Code, first.Body.String())
	}
	if got := strings.Join(store.equippedBotIDs, ","); got != "bot-retry" {
		t.Fatalf("visual refresh bots=%q, want only bot-retry", got)
	}
}
