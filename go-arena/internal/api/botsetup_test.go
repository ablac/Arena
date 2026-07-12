package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBotSetupDirectsKeyCreationToAuthenticatedDashboard(t *testing.T) {
	rec := httptest.NewRecorder()
	BotSetup().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bot-setup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if strings.Contains(body, "/api/v1/keys/generate") || strings.Contains(strings.ToLower(body), "no auth required") {
		t.Fatalf("bot setup advertises retired anonymous key generation: %s", body)
	}
	if !strings.Contains(body, "/dashboard/") || !strings.Contains(body, "/api/v1/account/keys") {
		t.Fatalf("bot setup does not describe authenticated account key management: %s", body)
	}
}
