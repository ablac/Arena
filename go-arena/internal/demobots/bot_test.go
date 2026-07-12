package demobots

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"arena-server/internal/db"
)

func TestDemoBotRegistrationProvisionsCredentialInProcess(t *testing.T) {
	originalPool := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = originalPool })

	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		if r.URL.Path == "/api/v1/keys/generate" {
			t.Error("demo bot called retired public key-generation route")
			http.Error(w, "retired", http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusOK)
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
