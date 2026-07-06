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
	}
}
