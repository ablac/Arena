package api

import (
	"arena-server/internal/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublicProfileNeverProjectsPrivateName(t *testing.T) {
	profile := &db.PublicProfile{AccountID: "acct"}
	raw, _ := json.Marshal(profileJSON(profile))
	if !strings.Contains(string(raw), `"public_username":null`) || strings.Contains(string(raw), `dev#`) {
		t.Fatalf("missing alias must be explicit: %s", raw)
	}
}
func TestProfileRejectsLocalIdentityWrites(t *testing.T) {
	for _, field := range []string{"display_name", "name", "username", "public_username"} {
		for _, value := range []string{`"Local alias"`, `null`} {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/account/profile", strings.NewReader(`{"`+field+`":`+value+`,"bio":"not applied"}`))
			req = req.WithContext(withCustomerSession(req.Context(), &CustomerSession{AccountID: "acct"}))
			rec := httptest.NewRecorder()
			UpdateAccountProfileHandler(rec, req)
			if rec.Code != 400 || !strings.Contains(rec.Body.String(), "https://accounts.angel-serv.com/portal/account/details") {
				t.Fatalf("%s=%s: %d %s", field, value, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestVerifiedOIDCUsernameStaysSeparateFromPrivateName(t *testing.T) {
	accounts := newAngelAccounts(t)
	handler, authority := newArenaSignedInWithAngel(t, accounts)
	for _, claim := range []any{"Reef_Pilot", nil, "", "Private Name"} {
		claims := map[string]any{"name": "Private Real Name", "product_admin": true}
		if claim != nil {
			claims["preferred_username"] = claim
		}
		session, _ := signInThroughAngel(t, handler, accounts, claims)
		if authority.displayName != "Private Real Name" {
			t.Fatalf("private name overwritten: %q", authority.displayName)
		}
		want := "Username unavailable"
		if claim == "Reef_Pilot" {
			want = "reef_pilot"
		}
		if db.PublicUsernameLabel(authority.publicUsername) != want || db.PublicUsernameLabel(session.PublicUsername) != want {
			t.Fatalf("claim %v: port=%v session=%v", claim, authority.publicUsername, session.PublicUsername)
		}
		if _, ok := session.platformAdminGrantAt(time.Now()); !ok {
			t.Fatal("username sync dropped signed administrator grant")
		}
	}
}
