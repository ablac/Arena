package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBotSetupDescribesPublicIssuanceThenVerifiedAccountClaim(t *testing.T) {
	rec := httptest.NewRecorder()
	BotSetup().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bot-setup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/api/v1/keys/generate") || !strings.Contains(strings.ToLower(body), "no account") {
		t.Fatalf("bot setup does not describe public database-backed key generation: %s", body)
	}
	if !strings.Contains(body, "/dashboard/") || !strings.Contains(body, "/api/v1/account/bots") {
		t.Fatalf("bot setup does not describe later verified-account claim: %s", body)
	}
	if !strings.Contains(body, `print(api_key)`) {
		t.Fatalf("copyable example loses the one-time generated credential: %s", body)
	}
}
