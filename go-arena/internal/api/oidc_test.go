package api

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"arena-server/internal/config"
	"arena-server/internal/game"

	"golang.org/x/oauth2"
)

func newTestAdminOIDCHandler() *OIDCHandler {
	return &OIDCHandler{
		oauth2Config: &oauth2.Config{
			ClientID:    "admin-client",
			RedirectURL: "https://arena.example/admin/callback",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://identity.example/authorize",
				TokenURL: "https://identity.example/token",
			},
		},
		sessions:      make(map[string]*OIDCSession),
		states:        make(map[string]adminOIDCTransaction),
		allowedEmails: parseAdminEmailAllowlist("Owner@Example.com, operator@example.com"),
	}
}

func TestAdminOIDCLoginUsesBrowserBindingNonceAndPKCE(t *testing.T) {
	handler := newTestAdminOIDCHandler()
	req := httptest.NewRequest(http.MethodGet, "https://arena.example/admin/login", nil)
	rec := httptest.NewRecorder()
	handler.LoginHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	query := location.Query()
	state := query.Get("state")
	if state == "" || query.Get("nonce") == "" || query.Get("code_challenge") == "" {
		t.Fatalf("authorization URL missing state/nonce/PKCE: %s", location.String())
	}
	if query.Get("code_challenge_method") != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", query.Get("code_challenge_method"))
	}
	var bindingCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == adminStateCookieName {
			bindingCookie = cookie
			break
		}
	}
	if bindingCookie == nil || bindingCookie.Value == "" || bindingCookie.Value == state {
		t.Fatalf("invalid browser-binding cookie: %+v", bindingCookie)
	}
	txn, ok := handler.states[state]
	if !ok {
		t.Fatal("server-side admin OIDC transaction was not recorded")
	}
	digest := sha256.Sum256([]byte(bindingCookie.Value))
	if digest != txn.BrowserBindingDigest || query.Get("nonce") != txn.Nonce ||
		query.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(txn.PKCEVerifier) {
		t.Fatal("admin OIDC transaction does not match browser binding, nonce, and PKCE challenge")
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("admin login response must not be cached")
	}
}

func TestAdminOIDCWrongBrowserCannotConsumeState(t *testing.T) {
	handler := newTestAdminOIDCHandler()
	login := httptest.NewRecorder()
	handler.LoginHandler(login, httptest.NewRequest(http.MethodGet, "https://arena.example/admin/login", nil))
	location, _ := url.Parse(login.Header().Get("Location"))
	state := location.Query().Get("state")

	req := httptest.NewRequest(http.MethodGet, "https://arena.example/admin/callback?state="+url.QueryEscape(state)+"&code=fake", nil)
	req.AddCookie(&http.Cookie{Name: adminStateCookieName, Value: "wrong-browser"})
	rec := httptest.NewRecorder()
	handler.CallbackHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong-browser callback status = %d, want 400", rec.Code)
	}
	if _, ok := handler.states[state]; !ok {
		t.Fatal("wrong browser consumed the real browser's one-use state")
	}
}

func TestAdminOIDCEmailAuthorizationIsExplicitAndCaseInsensitive(t *testing.T) {
	handler := newTestAdminOIDCHandler()
	if !handler.isAllowedAdminEmail(" owner@EXAMPLE.com ") {
		t.Fatal("configured verified admin email was not allowed")
	}
	if handler.isAllowedAdminEmail("customer@example.com") {
		t.Fatal("unconfigured customer email was allowed to mint an admin session")
	}
	if got := len(parseAdminEmailAllowlist(" ,not-an-email,")); got != 0 {
		t.Fatalf("invalid admin allowlist entries = %d, want 0", got)
	}
}

func TestAdminOIDCConfigurationFailsClosedWithoutAllowlist(t *testing.T) {
	previous := config.C
	t.Cleanup(func() { config.C = previous })
	config.C.OIDCEnabled = true
	config.C.OIDCIssuer = "https://identity.example"
	config.C.OIDCClientID = "admin-client"
	config.C.OIDCClientSecret = "secret"
	config.C.OIDCRedirectURI = "https://arena.example/admin/callback"
	config.C.OIDCAdminEmails = ""
	if handler := NewOIDCHandler(); handler != nil {
		t.Fatal("admin OIDC initialized without an explicit verified-email allowlist")
	}
}

func TestAdminSessionRoutesQuietlyReportOIDCDisabled(t *testing.T) {
	previous := config.C
	t.Cleanup(func() { config.C = previous })
	config.C.OIDCEnabled = false
	config.C.CustomerOIDCEnabled = false
	router := NewRouter(game.NewGameEngine())

	for _, path := range []string{"/api/v1/admin/session", "/arena/api/v1/admin/session"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s, want 200", path, recorder.Code, recorder.Body.String())
		}
		var payload struct {
			Authenticated bool `json:"authenticated"`
			LoginEnabled  bool `json:"login_enabled"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("GET %s response: %v", path, err)
		}
		if payload.Authenticated || payload.LoginEnabled {
			t.Fatalf("GET %s payload=%+v, want disabled unauthenticated session", path, payload)
		}
	}
}
