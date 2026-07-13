package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arena-server/internal/config"
)

// TestSecurityHeadersMiddleware_AllowsSameOriginFraming is the regression
// test for the 2026-07 Toolkit/Dashboard outage: frontend/index.html embeds
// /dashboard/?view=public and /dashboard/?view=private in same-origin
// <iframe>s (the Toolkit and Dashboard nav overlays). X-Frame-Options: DENY
// and CSP frame-ancestors 'none' block ALL framing, including same-origin,
// so Chrome refused to render the iframe response (net::ERR_BLOCKED_BY_RESPONSE)
// and both overlays rendered as an empty drawer. The fix must still block
// third-party (cross-origin) framing to preserve the original clickjacking
// protection.
func TestSecurityHeadersMiddleware_AllowsSameOriginFraming(t *testing.T) {
	config.C.SecurityHeadersEnabled = true

	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/dashboard/?view=public", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if xfo := rec.Header().Get("X-Frame-Options"); xfo != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want SAMEORIGIN (DENY blocks the same-origin dashboard iframe)", xfo)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP contains frame-ancestors 'none', which blocks the same-origin dashboard iframe: %q", csp)
	} else if !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Errorf("CSP missing frame-ancestors 'self': %q", csp)
	} else if !strings.Contains(csp, "frame-src 'self'") {
		t.Errorf("CSP frame-src must allow the same-origin dashboard iframe: %q", csp)
	}
}

func TestSecurityHeadersMiddleware_AllowsStripeEmbeddedCheckout(t *testing.T) {
	config.C.SecurityHeadersEnabled = true
	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	for _, directive := range []string{
		"script-src 'self' https://cdn.babylonjs.com https://js.stripe.com https://*.js.stripe.com https://checkout.stripe.com",
		"frame-src 'self' https://checkout.stripe.com https://js.stripe.com https://*.js.stripe.com https://hooks.stripe.com https://link.com https://*.link.com",
		"connect-src 'self' ws: wss: https://cdn.babylonjs.com https://api.stripe.com https://checkout.stripe.com https://link.com https://*.link.com",
		"img-src 'self' data: blob: https://*.stripe.com https://*.link.com",
	} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP missing Stripe Embedded Checkout directive %q: %q", directive, csp)
		}
	}
	if policy := rec.Header().Get("Permissions-Policy"); !strings.Contains(policy, `payment=(self "https://checkout.stripe.com"`) {
		t.Errorf("Permissions-Policy blocks Stripe wallet payments: %q", policy)
	}
}
