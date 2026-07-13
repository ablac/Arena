package demobots

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"arena-server/internal/db"
)

func TestDemoBotReconnectBackoffResetsAfterEstablishedSession(t *testing.T) {
	b := newDemoBot(DemoConfigs[0], "http://arena.test")
	outcomes := []demoBotSessionOutcome{
		{Err: errors.New("dial failed")},
		{Err: errors.New("dial failed")},
		{Err: errors.New("dial failed")},
		{Established: true, Err: errors.New("connection dropped")},
		{Err: errors.New("dial failed")},
	}
	nextOutcome := 0
	waits := make([]time.Duration, 0, len(outcomes))

	b.runSessions(context.Background(), func(context.Context) demoBotSessionOutcome {
		outcome := outcomes[nextOutcome]
		nextOutcome++
		return outcome
	}, func(_ context.Context, delay time.Duration) bool {
		waits = append(waits, delay)
		return len(waits) < len(outcomes)
	})

	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, time.Second, 2 * time.Second}
	if len(waits) != len(want) {
		t.Fatalf("reconnect waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("reconnect waits = %v, want %v; an established session must restore the initial retry inside the reconnect grace", waits, want)
		}
	}
}

func TestDemoBotRegistrationProvisionsCredentialInProcess(t *testing.T) {
	originalPool := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = originalPool })

	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		if r.URL.Path == "/api/v1/keys/generate" {
			t.Error("demo bot called the public self-service key-generation route")
			http.Error(w, "unexpected public key generation", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bot_id":"demo-bot-id"}`))
	}))
	t.Cleanup(server.Close)

	bot := newDemoBot(DemoConfigs[0], server.URL)
	provisionCalls := 0
	bot.credentialProvisioner = func(context.Context, BotConfig) (string, error) {
		provisionCalls++
		return "arena_demo_test_credential", nil
	}

	if err := bot.register(context.Background()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if provisionCalls != 1 {
		t.Fatalf("provision calls = %d, want 1", provisionCalls)
	}
	select {
	case request := <-requests:
		if request != "PUT /api/v1/bot/config" {
			t.Fatalf("unexpected HTTP request: %s", request)
		}
	default:
		t.Fatal("demo bot did not configure its in-process credential")
	}
}

func TestDemoBotActsOnlyOnAliveTick(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  map[string]interface{}
		want bool
	}{
		{
			name: "alive",
			msg:  map[string]interface{}{"your_state": map[string]interface{}{"is_alive": true}},
			want: true,
		},
		{
			name: "dead",
			msg:  map[string]interface{}{"your_state": map[string]interface{}{"is_alive": false}},
			want: false,
		},
		{name: "missing state", msg: map[string]interface{}{}, want: false},
		{name: "malformed state", msg: map[string]interface{}{"your_state": "alive"}, want: false},
		{
			name: "missing alive flag",
			msg:  map[string]interface{}{"your_state": map[string]interface{}{}},
			want: false,
		},
		{
			name: "malformed alive flag",
			msg:  map[string]interface{}{"your_state": map[string]interface{}{"is_alive": 1}},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldActOnTick(tc.msg); got != tc.want {
				t.Fatalf("shouldActOnTick() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDemoBotRegistrationProvisionsConfiguredCosmeticPack(t *testing.T) {
	originalPool := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = originalPool })

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Arena-Key") != "arena_demo_cosmetic_test" {
			t.Errorf("%s %s missing demo bot authentication", r.Method, r.URL.Path)
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.URL.Path != "/api/v1/bot/config" || r.Method != http.MethodPut {
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bot_id":"registered-demo-bot-id"}`))
	}))
	t.Cleanup(server.Close)

	cfg := DemoConfigs[0]
	bot := newDemoBot(cfg, server.URL)
	bot.credentialProvisioner = func(context.Context, BotConfig) (string, error) {
		return "arena_demo_cosmetic_test", nil
	}
	provisionCalls := 0
	bot.cosmeticProvisioner = func(_ context.Context, botID string, got BotConfig) ([]cosmeticSelection, error) {
		provisionCalls++
		if botID != "registered-demo-bot-id" {
			t.Errorf("cosmetic provision bot ID = %q", botID)
		}
		if got.Name != cfg.Name || got.CosmeticPackID != cfg.CosmeticPackID || got.CosmeticTrailID != cfg.CosmeticTrailID {
			t.Errorf("cosmetic provision config = %+v, want %+v", got, cfg)
		}
		return configuredAllCosmeticSelections(got), nil
	}

	if err := bot.register(context.Background()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if provisionCalls != 1 {
		t.Fatalf("cosmetic provision calls = %d, want 1", provisionCalls)
	}

	if len(requests) != 1 || requests[0] != "PUT /api/v1/bot/config" {
		t.Errorf("demo bot made unexpected registration requests: %v", requests)
	}
}
