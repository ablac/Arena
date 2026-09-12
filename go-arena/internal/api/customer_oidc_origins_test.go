package api

import (
	"arena-server/internal/config"
	"github.com/coreos/go-oidc/v3/oidc"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCustomerOIDCOrigins(t *testing.T) {
	h := newTestCustomerOIDCHandler()
	h.redirectURIs = []string{h.oauth2Config.RedirectURL, "https://arena.angel-gaming.com/account/callback"}
	for _, origin := range []string{"https://arena.example", "https://arena.angel-gaming.com"} {
		rec := httptest.NewRecorder()
		h.LoginHandler(rec, httptest.NewRequest("GET", origin+"/account/login", nil))
		if rec.Code != 302 {
			t.Fatal(rec.Code)
		}
		u, _ := url.Parse(rec.Header().Get("Location"))
		txn := h.states[u.Query().Get("state")]
		if u.Query().Get("redirect_uri") != origin+"/account/callback" || txn.RedirectURI != origin+"/account/callback" {
			t.Fatal("origin not bound")
		}
		if c, ok := h.callbackOAuthConfig(httptest.NewRequest("GET", origin+"/account/callback", nil), txn); !ok || c.RedirectURL != txn.RedirectURI {
			t.Fatal("correct callback rejected")
		}
		if _, ok := h.callbackOAuthConfig(httptest.NewRequest("GET", "https://wrong.example/account/callback", nil), txn); ok {
			t.Fatal("wrong host accepted")
		}
	}
	if len(h.states) != 2 || h.oauth2Config.RedirectURL != "https://arena.example/account/callback" {
		t.Fatal("concurrent flow changed shared config")
	}
}
func TestCustomerOIDCOriginAttacksAndLegacy(t *testing.T) {
	old := config.C.TrustedProxyCIDRs
	t.Cleanup(func() { config.C.TrustedProxyCIDRs = old })
	config.C.TrustedProxyCIDRs = "172.30.1.2/32"
	h := newTestCustomerOIDCHandler()
	h.redirectURIs = []string{h.oauth2Config.RedirectURL, "https://arena.angel-gaming.com/account/callback"}
	for _, address := range []string{"https://evil.example/account/login", "https://arena.example.evil.test/account/login", "http://arena.example/account/login", "https://arena.example:444/account/login"} {
		req := httptest.NewRequest("GET", address, nil)
		req.Header.Set("X-Forwarded-Host", "arena.example")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.RemoteAddr = "203.0.113.1:42"
		rec := httptest.NewRecorder()
		h.LoginHandler(rec, req)
		if rec.Code != 400 || len(rec.Result().Cookies()) != 0 {
			t.Fatalf("untrusted %s accepted", address)
		}
	}
	req := httptest.NewRequest("GET", "http://arena.angel-gaming.com/account/login", nil)
	req.RemoteAddr = "172.30.1.2:42"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.LoginHandler(rec, req)
	if rec.Code != 302 {
		t.Fatal("trusted proxy rejected")
	}
	for _, bad := range []string{"https://evil.example/account/callback", "https://arena.example/account/callback?redirect_uri=https://evil.example", "https://arena.example/other"} {
		if _, ok := h.callbackOAuthConfig(httptest.NewRequest("GET", "https://arena.example/account/callback", nil), customerOIDCTransaction{RedirectURI: bad}); ok {
			t.Fatal("unconfigured stored URI accepted")
		}
	}
	if c, ok := h.callbackOAuthConfig(httptest.NewRequest("GET", "https://arena.example/account/callback", nil), customerOIDCTransaction{}); !ok || c.RedirectURL != h.oauth2Config.RedirectURL {
		t.Fatal("legacy rejected")
	}
	if _, ok := h.callbackOAuthConfig(httptest.NewRequest("GET", "https://arena.angel-gaming.com/account/callback", nil), customerOIDCTransaction{}); ok {
		t.Fatal("legacy transferred origin")
	}
}

func TestCustomerOIDCBoundURIAtTokenExchangeAndReplay(t *testing.T) {
	calls := 0
	expected := ""
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = r.ParseForm()
		if r.Form.Get("redirect_uri") != expected {
			t.Errorf("exchange redirect = %q, want %q", r.Form.Get("redirect_uri"), expected)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer provider.Close()
	h := newTestCustomerOIDCHandler()
	h.oauth2Config.Endpoint.TokenURL = provider.URL
	h.verifier = oidc.NewVerifier(h.issuer, nil, &oidc.Config{ClientID: "customer-client"})
	h.redirectURIs = []string{h.oauth2Config.RedirectURL, "https://arena.angel-gaming.com/account/callback"}
	type flow struct {
		uri, state string
		cookie     *http.Cookie
	}
	flows := []flow{}
	for _, uri := range h.redirectURIs {
		u, _ := url.Parse(uri)
		rec := httptest.NewRecorder()
		h.LoginHandler(rec, httptest.NewRequest("GET", u.Scheme+"://"+u.Host+"/account/login", nil))
		auth, _ := url.Parse(rec.Header().Get("Location"))
		flows = append(flows, flow{uri, auth.Query().Get("state"), rec.Result().Cookies()[0]})
	}
	for _, f := range flows {
		expected = f.uri
		req := httptest.NewRequest("GET", f.uri+"?state="+f.state+"&code=test", nil)
		req.AddCookie(f.cookie)
		rec := httptest.NewRecorder()
		h.CallbackHandler(rec, req)
		if rec.Code != 502 || calls == 0 {
			t.Fatalf("did not reach token exchange: %d", rec.Code)
		}
		before := calls
		rec = httptest.NewRecorder()
		h.CallbackHandler(rec, req)
		if rec.Code != 400 || calls != before {
			t.Fatal("replay reached provider")
		}
	}
}
