package api

import (
	"arena-server/internal/db"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGamingProfileAuthBodyAndQuota(t *testing.T) {
	key := strings.Repeat("a", 64)
	calls := 0
	h := newGamingProfileHandler(key, func(_ context.Context, issuer, subject string) (*db.GamingProfile, error) {
		calls++
		if issuer != "https://accounts.angel-serv.com" || subject != "subject-one" {
			t.Fatal("unexpected identity")
		}
		return &db.GamingProfile{Bots: []db.GamingBot{}}, nil
	})
	valid := `{"issuer":"https://accounts.angel-serv.com","subject":"subject-one"}`
	for _, tc := range []struct {
		token, body string
		status      int
	}{
		{"", valid, 401}, {"Bearer wrong", valid, 401}, {"Bearer " + key, `{"issuer":"https://evil.example","subject":"subject-one"}`, 400},
		{"Bearer " + key, valid + ` {}`, 400}, {"Bearer " + key, `{"issuer":"https://accounts.angel-serv.com","subject":"subject-one","email":"x"}`, 400},
		{"Bearer " + key, strings.Repeat("x", 1100), 400}, {"Bearer " + key, valid, 200},
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/gaming/profile", strings.NewReader(tc.body))
		r.Header.Set("Authorization", tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("status %d want %d", w.Code, tc.status)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("profile response cacheable")
		}
	}
	if calls != 1 {
		t.Fatalf("unauthorized lookup count %d", calls)
	}
	for i := 0; i < 60; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/gaming/profile", strings.NewReader(valid))
		r.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i == 59 && w.Code != 429 {
			t.Fatalf("quota not enforced: %d", w.Code)
		}
	}
}
