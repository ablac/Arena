package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
)

type fakeServiceNoticeStore struct {
	mu     sync.Mutex
	nextID int64
	events []db.ServiceNoticeEvent
}

func (s *fakeServiceNoticeStore) Append(_ context.Context, evt db.ServiceNoticeEvent) (db.ServiceNoticeEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	evt.ID = s.nextID
	evt.CreatedAt = time.Now().UTC()
	s.events = append(s.events, evt)
	return evt, nil
}

func (s *fakeServiceNoticeStore) Current(_ context.Context) ([]db.ServiceNoticeEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	latest := map[string]db.ServiceNoticeEvent{}
	for _, evt := range s.events {
		if evt.ID > latest[evt.Slot].ID {
			latest[evt.Slot] = evt
		}
	}
	out := make([]db.ServiceNoticeEvent, 0, len(latest))
	for _, evt := range latest {
		out = append(out, evt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out, nil
}

func (s *fakeServiceNoticeStore) List(_ context.Context, limit int) ([]db.ServiceNoticeEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit > len(s.events) {
		limit = len(s.events)
	}
	out := make([]db.ServiceNoticeEvent, 0, limit)
	for i := len(s.events) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.events[i])
	}
	return out, nil
}

func newFakeServiceStatus(t *testing.T) (*game.GameEngine, *ServiceStatusService, *fakeServiceNoticeStore) {
	t.Helper()
	engine := game.NewGameEngine()
	store := &fakeServiceNoticeStore{}
	service := newServiceStatusServiceWithStore(engine, NewEventBus(), store)
	if err := service.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	return engine, service, store
}

func TestBroadcastPublishClearAndStaleProtection(t *testing.T) {
	engine, service, _ := newFakeServiceStatus(t)
	status, err := service.PublishBroadcast(context.Background(), "Plain <b>text</b>", "warning", nil)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if status.Broadcast == nil || status.Broadcast.Message != "Plain <b>text</b>" || status.Broadcast.ID == 0 {
		t.Fatalf("published status = %#v", status)
	}
	if _, err := service.ClearBroadcast(context.Background(), status.Broadcast.ID+1); err != errStaleBroadcast {
		t.Fatalf("stale clear error = %v, want %v", err, errStaleBroadcast)
	}
	if engine.GetServiceStatus().Broadcast == nil {
		t.Fatal("stale clear removed current broadcast")
	}
	if _, err := service.ClearBroadcast(context.Background(), status.Broadcast.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if engine.GetServiceStatus().Broadcast != nil {
		t.Fatal("broadcast remained after clear")
	}
}

func TestExpiredLatestBroadcastDoesNotRevealOlderEvent(t *testing.T) {
	engine := game.NewGameEngine()
	store := &fakeServiceNoticeStore{}
	old, _ := store.Append(context.Background(), db.ServiceNoticeEvent{Slot: db.ServiceNoticeSlotBroadcast, Active: true, Severity: "info", Message: "old"})
	expired := time.Now().Add(-time.Second)
	newest, _ := store.Append(context.Background(), db.ServiceNoticeEvent{Slot: db.ServiceNoticeSlotBroadcast, Active: true, Severity: "warning", Message: "new", ExpiresAt: &expired})
	if newest.ID <= old.ID {
		t.Fatal("fixture revisions are not monotonic")
	}
	service := newServiceStatusServiceWithStore(engine, nil, store)
	if err := service.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	status := engine.GetServiceStatus()
	if status.Broadcast != nil {
		t.Fatalf("expired newest event revealed a broadcast: %#v", status.Broadcast)
	}
	if status.Revision <= newest.ID {
		t.Fatalf("revision = %d, want durable tombstone after %d", status.Revision, newest.ID)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if len(current) != 1 || current[0].Active || current[0].Source != "expired" {
		t.Fatalf("current event = %#v, want expired tombstone", current)
	}
	reloadedEngine := game.NewGameEngine()
	reloadedService := newServiceStatusServiceWithStore(reloadedEngine, nil, store)
	if err := reloadedService.Load(context.Background()); err != nil {
		t.Fatalf("reload tombstone: %v", err)
	}
	if reloaded := reloadedEngine.GetServiceStatus(); reloaded.Broadcast != nil || reloaded.Revision != status.Revision {
		t.Fatalf("reloaded status = %#v, want stable clear revision %d", reloaded, status.Revision)
	}
}

func TestBroadcastExpiryAppendsMonotonicTombstone(t *testing.T) {
	engine, service, store := newFakeServiceStatus(t)
	expires := time.Now().UTC().Add(30 * time.Millisecond)
	published, err := service.PublishBroadcast(context.Background(), "short lived", "info", &expires)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := engine.GetServiceStatus()
		if status.Broadcast == nil && status.Revision > published.Revision {
			current, currentErr := store.Current(context.Background())
			if currentErr != nil {
				t.Fatalf("current: %v", currentErr)
			}
			if len(current) != 1 || current[0].Active || current[0].Source != "expired" {
				t.Fatalf("current event = %#v, want expired tombstone", current)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expiry did not append a newer clear event; status=%#v", engine.GetServiceStatus())
}

func TestOldExpiryTimerCannotClearReplacementBroadcast(t *testing.T) {
	engine, service, store := newFakeServiceStatus(t)
	expires := time.Now().UTC().Add(40 * time.Millisecond)
	if _, err := service.PublishBroadcast(context.Background(), "old", "warning", &expires); err != nil {
		t.Fatalf("publish old: %v", err)
	}
	replacement, err := service.PublishBroadcast(context.Background(), "replacement", "info", nil)
	if err != nil {
		t.Fatalf("publish replacement: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	status := engine.GetServiceStatus()
	if status.Broadcast == nil || status.Broadcast.ID != replacement.Broadcast.ID || status.Broadcast.Message != "replacement" {
		t.Fatalf("replacement was cleared by stale timer: %#v", status.Broadcast)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if len(current) != 1 || !current[0].Active || current[0].ID != replacement.Broadcast.ID {
		t.Fatalf("current event = %#v, want replacement", current)
	}
}

func TestManualRestartIsClearedDurablyOnStartup(t *testing.T) {
	_, service, store := newFakeServiceStatus(t)
	status, err := service.SetManualRestart(context.Background())
	if err != nil {
		t.Fatalf("set manual restart: %v", err)
	}
	if status.Maintenance == nil || status.Maintenance.Source != "admin-restart" {
		t.Fatalf("maintenance = %#v", status.Maintenance)
	}

	restartedEngine := game.NewGameEngine()
	restartedService := newServiceStatusServiceWithStore(restartedEngine, nil, store)
	if err := restartedService.Load(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	restarted := restartedEngine.GetServiceStatus()
	if restarted.Maintenance != nil || restarted.Revision <= status.Revision {
		t.Fatalf("restarted status = %#v, want newer durable clear", restarted)
	}
}

func TestValidateBroadcastInput(t *testing.T) {
	if _, _, _, err := validateBroadcastInput("", "info", nil); err == nil {
		t.Fatal("empty message accepted")
	}
	if _, _, _, err := validateBroadcastInput("hello", "urgent", nil); err == nil {
		t.Fatal("unknown severity accepted")
	}
	tooLong := strings.Repeat("x", maxBroadcastRunes+1)
	if _, _, _, err := validateBroadcastInput(tooLong, "info", nil); err == nil {
		t.Fatal("oversized message accepted")
	}
	if _, _, _, err := validateBroadcastInput("bad\x00message", "info", nil); err == nil {
		t.Fatal("control character accepted")
	}
	seconds := 59
	if _, _, _, err := validateBroadcastInput("hello", "info", &seconds); err == nil {
		t.Fatal("too-short expiry accepted")
	}
}

func TestPublicServiceStatusIsNoStore(t *testing.T) {
	engine, service, _ := newFakeServiceStatus(t)
	engine.RestoreServiceStatus(game.ServiceStatus{Revision: 12})
	rec := httptest.NewRecorder()
	service.publicStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/service-status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	var status game.ServiceStatus
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.Type != "service_status" || status.Revision != 12 {
		t.Fatalf("response = %#v", status)
	}
}

func TestUpdaterCallbackRequiresSecretAndMatchingTarget(t *testing.T) {
	_, service, _ := newFakeServiceStatus(t)
	previous := config.C.UpdaterSharedSecret
	config.C.UpdaterSharedSecret = "callback-secret"
	t.Cleanup(func() { config.C.UpdaterSharedSecret = previous })
	target := strings.Repeat("a", 40)
	if _, err := service.SetMaintenance(context.Background(), target, "preparing", "updating", "test"); err != nil {
		t.Fatalf("set maintenance: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/internal/updater/status", strings.NewReader(`{"phase":"restarting","target_commit":"`+target+`"}`))
	rec := httptest.NewRecorder()
	service.updaterStatusCallback(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing secret status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/internal/updater/status", strings.NewReader(`{"phase":"done","target_commit":"`+strings.Repeat("b", 40)+`"}`))
	req.Header.Set("Authorization", "Bearer callback-secret")
	rec = httptest.NewRecorder()
	service.updaterStatusCallback(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale target status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminUpdatePublishesMaintenanceBeforeCallingSidecar(t *testing.T) {
	engine, service, _ := newFakeServiceStatus(t)
	target := strings.Repeat("c", 40)
	observed := make(chan bool, 1)
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			maintenance := engine.GetServiceStatus().Maintenance
			observed <- maintenance != nil && maintenance.TargetCommit == target && maintenance.Phase == "preparing"
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"inProgress": true, "phase": "building"})
	}))
	defer sidecar.Close()
	withUpdaterConfig(t, sidecar.URL, "secret")

	h := &AdminHandler{Engine: engine, ServiceStatus: service}
	req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"commitSha":"`+target+`"}`))
	rec := httptest.NewRecorder()
	h.triggerUpdate(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case ok := <-observed:
		if !ok {
			t.Fatal("sidecar was called before preparing maintenance became visible")
		}
	case <-time.After(time.Second):
		t.Fatal("sidecar did not receive update request")
	}
}

func TestAdminUpdateClearsMaintenanceWhenSidecarRejects(t *testing.T) {
	engine, service, _ := newFakeServiceStatus(t)
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusConflict)
	}))
	defer sidecar.Close()
	withUpdaterConfig(t, sidecar.URL, "secret")

	target := strings.Repeat("d", 40)
	h := &AdminHandler{Engine: engine, ServiceStatus: service}
	req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"commitSha":"`+target+`"}`))
	rec := httptest.NewRecorder()
	h.triggerUpdate(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if engine.GetServiceStatus().Maintenance != nil {
		t.Fatal("rejected update left maintenance active")
	}
}

func TestConcurrentAdminUpdateReturnsConflictWithoutReplacingFirstNotice(t *testing.T) {
	engine, service, _ := newFakeServiceStatus(t)
	firstTarget := strings.Repeat("e", 40)
	secondTarget := strings.Repeat("f", 40)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls int
	var callsMu sync.Mutex
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusOK, map[string]interface{}{"inProgress": true, "targetCommit": firstTarget, "phase": "building"})
			return
		}
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		once.Do(func() { close(entered) })
		<-release
		w.WriteHeader(http.StatusAccepted)
	}))
	defer sidecar.Close()
	withUpdaterConfig(t, sidecar.URL, "secret")

	h := &AdminHandler{Engine: engine, ServiceStatus: service}
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"commitSha":"`+firstTarget+`"}`))
		h.triggerUpdate(rec, req)
		firstDone <- rec
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first request did not reach sidecar")
	}

	secondStarted := make(chan struct{})
	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		close(secondStarted)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"commitSha":"`+secondTarget+`"}`))
		h.triggerUpdate(rec, req)
		secondDone <- rec
	}()
	<-secondStarted
	close(release)

	first := <-firstDone
	second := <-secondDone
	if first.Code != http.StatusAccepted || second.Code != http.StatusConflict {
		t.Fatalf("responses = first %d (%s), second %d (%s)", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	maintenance := engine.GetServiceStatus().Maintenance
	if maintenance == nil || maintenance.TargetCommit != firstTarget {
		t.Fatalf("maintenance = %#v, want first target", maintenance)
	}
	callsMu.Lock()
	gotCalls := calls
	callsMu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("sidecar POST calls = %d, want 1", gotCalls)
	}
	_, _ = service.ClearMaintenance(context.Background(), firstTarget, "test-cleanup")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func withUpdaterHTTPClient(t *testing.T, fn roundTripFunc) {
	t.Helper()
	previous := updaterHTTPClient
	updaterHTTPClient = &http.Client{Transport: fn}
	t.Cleanup(func() { updaterHTTPClient = previous })
}

func updaterStatusResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestAmbiguousUpdateTransportErrorReconcilesAcceptedTarget(t *testing.T) {
	engine, service, _ := newFakeServiceStatus(t)
	target := strings.Repeat("1", 40)
	withUpdaterConfig(t, "http://updater.test/update", "secret")
	withUpdaterHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return nil, errors.New("response connection reset")
		}
		if got := req.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("status authorization = %q", got)
		}
		return updaterStatusResponse(`{"inProgress":true,"targetCommit":"` + target + `","phase":"building"}`), nil
	})

	h := &AdminHandler{Engine: engine, ServiceStatus: service}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"commitSha":"`+target+`"}`))
	h.triggerUpdate(rec, req)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"acceptance":"reconciled"`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if current := engine.GetServiceStatus().Maintenance; current == nil || current.TargetCommit != target {
		t.Fatalf("accepted ambiguous update lost notice: %#v", current)
	}
	_, _ = service.ClearMaintenance(context.Background(), target, "test-cleanup")
}

func TestAmbiguousUpdateTransportErrorPreservesNoticeWhenStatusUnavailable(t *testing.T) {
	engine, service, _ := newFakeServiceStatus(t)
	target := strings.Repeat("2", 40)
	withUpdaterConfig(t, "http://updater.test/update", "secret")
	withUpdaterHTTPClient(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset")
	})

	h := &AdminHandler{Engine: engine, ServiceStatus: service}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"commitSha":"`+target+`"}`))
	h.triggerUpdate(rec, req)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"acceptance":"unknown"`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if current := engine.GetServiceStatus().Maintenance; current == nil || current.TargetCommit != target {
		t.Fatalf("unreconciled update lost notice: %#v", current)
	}
	_, _ = service.ClearMaintenance(context.Background(), target, "test-cleanup")
}

func TestAmbiguousUpdateTransportErrorClearsOnlyAfterDefinitiveRejection(t *testing.T) {
	engine, service, _ := newFakeServiceStatus(t)
	target := strings.Repeat("3", 40)
	withUpdaterConfig(t, "http://updater.test/update", "secret")
	withUpdaterHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return nil, errors.New("response connection reset")
		}
		return updaterStatusResponse(`{"inProgress":false,"targetCommit":null,"lastCompletedCommit":null,"phase":null}`), nil
	})

	h := &AdminHandler{Engine: engine, ServiceStatus: service}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"commitSha":"`+target+`"}`))
	h.triggerUpdate(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if current := engine.GetServiceStatus().Maintenance; current != nil {
		t.Fatalf("definitively rejected update left notice: %#v", current)
	}
}
