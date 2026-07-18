package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNoCacheStaticHandler_HTMLDocuments is part of the regression for the
// 2026-07 Toolkit/Dashboard outage: frontend/index.html embeds /dashboard/
// in a same-origin <iframe>. Directory-style routes like "/dashboard/" and
// "/" serve dynamically-redeployed HTML (via http.FileServer's implicit
// index.html) but previously only .js/.css got no-cache headers. A browser
// that cached a bad response (e.g. while a blocking header was live) kept
// serving it from cache after the server was fixed, because there was
// nothing telling it to revalidate.
func TestNoCacheStaticHandler_HTMLDocuments(t *testing.T) {
	cases := []struct {
		name             string
		path             string
		wantCacheControl string
	}{
		{"root", "/", "no-cache, no-store, must-revalidate"},
		{"dashboard directory", "/dashboard/", "no-cache, no-store, must-revalidate"},
		{"admin directory", "/admin/", "no-cache, no-store, must-revalidate"},
		{"mobile directory", "/m/", "no-cache, no-store, must-revalidate"},
		{"shop directory", "/shop/", "no-cache, no-store, must-revalidate"},
		{"explicit html file", "/dashboard/index.html", "no-cache, no-store, must-revalidate"},
		// JS/CSS get no-cache WITHOUT no-store: the browser must revalidate
		// every load (so a deploy is picked up immediately) but may store the
		// body, enabling If-Modified-Since/304 revalidation instead of full
		// re-downloads.
		{"js asset", "/js/app.js", "no-cache"},
		{"css asset", "/css/arena.css", "no-cache"},
		{"texture asset", "/textures/skybox/px.jpg", ""},
		{"favicon", "/favicon.ico", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := noCacheStaticHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got := rec.Header().Get("Cache-Control"); got != tc.wantCacheControl {
				t.Errorf("path %q: Cache-Control = %q, want %q", tc.path, got, tc.wantCacheControl)
			}
		})
	}
}
