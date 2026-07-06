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
		name      string
		path      string
		wantNoCache bool
	}{
		{"root", "/", true},
		{"dashboard directory", "/dashboard/", true},
		{"admin directory", "/admin/", true},
		{"mobile directory", "/m/", true},
		{"explicit html file", "/dashboard/index.html", true},
		{"js asset", "/js/app.js", true},
		{"css asset", "/css/arena.css", true},
		{"texture asset", "/textures/skybox/px.jpg", false},
		{"favicon", "/favicon.ico", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := noCacheStaticHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			got := rec.Header().Get("Cache-Control") == "no-cache, no-store, must-revalidate"
			if got != tc.wantNoCache {
				t.Errorf("path %q: no-cache header present = %v, want %v (Cache-Control=%q)", tc.path, got, tc.wantNoCache, rec.Header().Get("Cache-Control"))
			}
		})
	}
}
