package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arena-server/internal/config"
)

func withDemobotsConfig(t *testing.T, controlURL, adminToken, updaterURL, updaterSecret string) {
	t.Helper()
	prevControl := config.C.DemobotsControlURL
	prevToken := config.C.AdminToken
	prevUpdater := config.C.UpdaterURL
	prevSecret := config.C.UpdaterSharedSecret
	config.C.DemobotsControlURL = controlURL
	config.C.AdminToken = adminToken
	config.C.UpdaterURL = updaterURL
	config.C.UpdaterSharedSecret = updaterSecret
	t.Cleanup(func() {
		config.C.DemobotsControlURL = prevControl
		config.C.AdminToken = prevToken
		config.C.UpdaterURL = prevUpdater
		config.C.UpdaterSharedSecret = prevSecret
	})
}

func TestDemobotsStatusUnconfigured(t *testing.T) {
	withDemobotsConfig(t, "", "", "", "")
	h := &AdminHandler{}
	rec := httptest.NewRecorder()
	h.demobotsStatus(rec, httptest.NewRequest(http.MethodGet, "/demobots/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if configured, _ := payload["configured"].(bool); configured {
		t.Fatalf("expected configured=false, got %v", payload)
	}
}

func TestDemobotsStatusProxiesWithAdminToken(t *testing.T) {
	var gotToken string
	fleet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Admin-Token")
		if r.URL.Path != "/control/status" {
			t.Errorf("fleet got path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"commit":"abc1234","count":3,"bots":[]}`))
	}))
	defer fleet.Close()
	withDemobotsConfig(t, fleet.URL, "secret-token", "", "")

	h := &AdminHandler{}
	rec := httptest.NewRecorder()
	h.demobotsStatus(rec, httptest.NewRequest(http.MethodGet, "/demobots/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if gotToken != "secret-token" {
		t.Fatalf("fleet did not receive the admin token (got %q)", gotToken)
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if payload["reachable"] != true || payload["configured"] != true || payload["count"] != float64(3) {
		t.Fatalf("unexpected payload: %v", payload)
	}
}

func TestDemobotsStatusReportsUnreachableFleet(t *testing.T) {
	withDemobotsConfig(t, "http://127.0.0.1:1", "secret-token", "", "")
	h := &AdminHandler{}
	rec := httptest.NewRecorder()
	h.demobotsStatus(rec, httptest.NewRequest(http.MethodGet, "/demobots/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	if payload["configured"] != true || payload["reachable"] != false {
		t.Fatalf("unexpected payload: %v", payload)
	}
}

func TestDemobotsSetCountValidatesBody(t *testing.T) {
	withDemobotsConfig(t, "http://127.0.0.1:1", "secret-token", "", "")
	h := &AdminHandler{}
	for _, body := range []string{"", "{}", `{"count":-1}`, `{"count":101}`} {
		rec := httptest.NewRecorder()
		h.demobotsSetCount(rec, httptest.NewRequest(http.MethodPut, "/demobots/count", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: got %d, want 400", body, rec.Code)
		}
	}
}

func TestDemobotsSetCountForwardsCount(t *testing.T) {
	var gotBody string
	fleet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, 256)
		n, _ := r.Body.Read(raw)
		gotBody = string(raw[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"count":5}`))
	}))
	defer fleet.Close()
	withDemobotsConfig(t, fleet.URL, "secret-token", "", "")

	h := &AdminHandler{}
	rec := httptest.NewRecorder()
	h.demobotsSetCount(rec, httptest.NewRequest(http.MethodPut, "/demobots/count", strings.NewReader(`{"count":5}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(gotBody, `"count":5`) {
		t.Fatalf("fleet received body %q", gotBody)
	}
}

func TestDemobotsTriggerUpdateRequiresUpdater(t *testing.T) {
	withDemobotsConfig(t, "http://127.0.0.1:1", "secret-token", "", "")
	h := &AdminHandler{}
	rec := httptest.NewRecorder()
	h.demobotsTriggerUpdate(rec, httptest.NewRequest(http.MethodPost, "/demobots/update", strings.NewReader("{}")))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDemobotsTriggerUpdateForwardsToUpdater(t *testing.T) {
	var gotPath, gotAuth string
	updater := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true,"accepted":true,"commitSha":"0123456789abcdef0123456789abcdef01234567"}`))
	}))
	defer updater.Close()
	withDemobotsConfig(t, "http://127.0.0.1:1", "secret-token", updater.URL+"/update", "sidecar-secret")

	h := &AdminHandler{}
	rec := httptest.NewRecorder()
	h.demobotsTriggerUpdate(rec, httptest.NewRequest(http.MethodPost, "/demobots/update", strings.NewReader("{}")))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	if gotPath != "/demobots/update" {
		t.Fatalf("updater got path %q, want /demobots/update", gotPath)
	}
	if gotAuth != "Bearer sidecar-secret" {
		t.Fatalf("updater got auth %q", gotAuth)
	}
}

func TestDemobotsTriggerUpdateRejectsBadSha(t *testing.T) {
	withDemobotsConfig(t, "http://127.0.0.1:1", "secret-token", "http://127.0.0.1:1/update", "sidecar-secret")
	h := &AdminHandler{}
	rec := httptest.NewRecorder()
	h.demobotsTriggerUpdate(rec, httptest.NewRequest(http.MethodPost, "/demobots/update", strings.NewReader(`{"commitSha":"short"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUpdaterEndpointURLRebasesUpdatePath(t *testing.T) {
	withDemobotsConfig(t, "", "", "http://arena-updater:8090/update", "s")
	got, err := updaterEndpointURL("/demobots/latest")
	if err != nil || got != "http://arena-updater:8090/demobots/latest" {
		t.Fatalf("got %q err=%v", got, err)
	}
	config.C.UpdaterURL = "http://arena-updater:8090"
	got, err = updaterEndpointURL("/demobots/update")
	if err != nil || got != "http://arena-updater:8090/demobots/update" {
		t.Fatalf("got %q err=%v", got, err)
	}
}
