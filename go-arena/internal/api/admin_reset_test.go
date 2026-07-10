package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arena-server/internal/db"

	"github.com/go-chi/chi/v5"
)

func TestAdminResetLeaderboard_RouteClearsBothLeaderboardSources(t *testing.T) {
	calls := 0
	h := &AdminHandler{
		resetLeaderboardData: func(context.Context) error {
			calls++
			return nil
		},
	}
	router := chi.NewRouter()
	h.Routes(router)

	req := httptest.NewRequest(http.MethodPost, "/db/reset-leaderboard", strings.NewReader(`{"confirm":"RESET_ALL_STATS"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if calls != 1 {
		t.Fatalf("reset calls = %d, want 1", calls)
	}
	var body struct {
		Message        string   `json:"message"`
		ClearedSources []string `json:"cleared_sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Message != "leaderboard reset successfully" {
		t.Errorf("message = %q", body.Message)
	}
	if len(body.ClearedSources) != 2 || body.ClearedSources[0] != "all_time" || body.ClearedSources[1] != "time_windows" {
		t.Errorf("cleared_sources = %#v, want both all-time and time-window sources", body.ClearedSources)
	}
}

func TestAdminResetLeaderboard_RequiresExactConfirmation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `not-json`},
		{name: "missing confirmation", body: `{}`},
		{name: "wrong confirmation", body: `{"confirm":"reset"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			h := &AdminHandler{resetLeaderboardData: func(context.Context) error {
				called = true
				return nil
			}}
			req := httptest.NewRequest(http.MethodPost, "/db/reset-leaderboard", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			h.resetLeaderboard(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if called {
				t.Fatal("reset was called without the exact confirmation phrase")
			}
		})
	}
}

func TestAdminResetLeaderboard_MapsDatabaseFailures(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantError  string
	}{
		{name: "database unavailable", err: db.ErrNoDatabase, wantStatus: http.StatusServiceUnavailable, wantError: "database not available"},
		{name: "database operation failed", err: errors.New("truncate failed"), wantStatus: http.StatusInternalServerError, wantError: "failed to reset leaderboard"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &AdminHandler{resetLeaderboardData: func(context.Context) error { return tt.err }}
			req := httptest.NewRequest(http.MethodPost, "/db/reset-leaderboard", strings.NewReader(`{"confirm":"RESET_ALL_STATS"}`))
			rec := httptest.NewRecorder()

			h.resetLeaderboard(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body["error"] != tt.wantError {
				t.Errorf("error = %q, want %q", body["error"], tt.wantError)
			}
		})
	}
}
