package ws

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/game"
)

func TestWebSocketAdmissionMetricsRollingWindowAndLatency(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	metrics := newWebSocketAdmissionMetrics(func() time.Time { return now })

	success := metrics.begin(websocketEndpointBot)
	now = now.Add(25 * time.Millisecond)
	success.upgraded()
	now = now.Add(15 * time.Millisecond)
	success.admitted()
	success.finish()

	authFailure := metrics.begin(websocketEndpointBot)
	authFailure.fail(websocketFailureAuth)
	authFailure.finish()

	upgradeFailure := metrics.begin(websocketEndpointBot)
	upgradeFailure.fail(websocketFailureUpgrade)
	upgradeFailure.finish()

	rateFailure := metrics.begin(websocketEndpointChat)
	rateFailure.fail(websocketFailureRateLimit)
	rateFailure.finish()

	capacityFailure := metrics.begin(websocketEndpointSpectator)
	capacityFailure.upgraded()
	capacityFailure.fail(websocketFailureCapacity)
	capacityFailure.finish()

	unclassifiedFailure := metrics.begin(websocketEndpointChat)
	unclassifiedFailure.finish()

	snapshot := metrics.snapshot()
	if snapshot.WindowSeconds != 60 {
		t.Fatalf("window_seconds = %d, want 60", snapshot.WindowSeconds)
	}
	if got := snapshot.Bot.Totals; got.Attempts != 3 || got.Upgrades != 1 || got.Admissions != 1 || got.Failures.Auth != 1 || got.Failures.Upgrade != 1 {
		t.Fatalf("bot totals = %#v, want 3 attempts, 1 upgrade/admission, 1 auth and 1 upgrade failure", got)
	}
	if got := snapshot.Bot.Rolling1m; got.Attempts != 3 || got.Upgrades != 1 || got.Admissions != 1 {
		t.Fatalf("bot rolling metrics = %#v, want current-minute totals", got)
	}
	if got := snapshot.Bot.Rolling1m.AttemptsPerSecond; got != float64(3)/60 {
		t.Fatalf("bot attempts_per_second = %v, want %v", got, float64(3)/60)
	}
	if got := snapshot.Bot.Rolling1m; got.PeakAttemptsPerSecond != 3 || got.PeakUpgradesPerSecond != 1 || got.PeakAdmissionsPerSecond != 1 {
		t.Fatalf("bot rolling peaks = %#v, want attempts=3 upgrades=1 admissions=1", got)
	}
	if got := snapshot.Bot.AdmissionLatencyMS; got.Samples != 1 || got.Average != 40 || got.Max != 40 {
		t.Fatalf("bot admission latency = %#v, want one 40ms sample", got)
	}
	if got := snapshot.Bot.Rolling1m.AdmissionLatencyMS; got.Samples != 1 || got.Average != 40 || got.Max != 40 {
		t.Fatalf("rolling bot admission latency = %#v, want one 40ms sample", got)
	}
	if got := snapshot.Chat.Totals.Failures; got.RateLimit != 1 || got.Other != 1 {
		t.Fatalf("chat failures = %#v, want one rate-limit and one fallback failure", got)
	}
	if got := snapshot.Spectator.Totals; got.Attempts != 1 || got.Upgrades != 1 || got.Failures.Capacity != 1 {
		t.Fatalf("spectator totals = %#v, want upgraded capacity rejection", got)
	}

	now = now.Add(61 * time.Second)
	expired := metrics.snapshot()
	if expired.Bot.Rolling1m.Attempts != 0 || expired.Bot.Rolling1m.Admissions != 0 || expired.Chat.Rolling1m.Attempts != 0 {
		t.Fatalf("expired rolling window retained events: bot=%#v chat=%#v", expired.Bot.Rolling1m, expired.Chat.Rolling1m)
	}
	if expired.Bot.Totals.Attempts != 3 || expired.Bot.Totals.Admissions != 1 {
		t.Fatalf("cumulative totals changed after rolling expiry: %#v", expired.Bot.Totals)
	}
}

func TestWebSocketAdmissionMetricsConcurrentRecording(t *testing.T) {
	fixedNow := time.Unix(1_700_000_000, 0)
	metrics := newWebSocketAdmissionMetrics(func() time.Time { return fixedNow })

	const attempts = 1000
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			defer wg.Done()
			attempt := metrics.begin(websocketEndpointBot)
			if i%2 == 0 {
				attempt.upgraded()
				attempt.admitted()
			} else {
				attempt.fail(websocketFailureAuth)
			}
			attempt.finish()
		}(i)
	}
	wg.Wait()

	snapshot := metrics.snapshot().Bot
	if snapshot.Totals.Attempts != attempts || snapshot.Totals.Upgrades != attempts/2 || snapshot.Totals.Admissions != attempts/2 || snapshot.Totals.Failures.Auth != attempts/2 {
		t.Fatalf("concurrent totals = %#v, want %d attempts split evenly between admission and auth failure", snapshot.Totals, attempts)
	}
	if snapshot.Rolling1m.Attempts != attempts || snapshot.Rolling1m.Admissions != attempts/2 {
		t.Fatalf("concurrent rolling totals = %#v", snapshot.Rolling1m)
	}
}

func TestWebSocketHandlersRecordUpgradeFailures(t *testing.T) {
	originalMetrics := defaultWebSocketAdmissionMetrics
	defaultWebSocketAdmissionMetrics = newWebSocketAdmissionMetrics(func() time.Time {
		return time.Unix(1_700_000_000, 0)
	})
	t.Cleanup(func() { defaultWebSocketAdmissionMetrics = originalMetrics })

	originalChatEnabled := config.C.ChatEnabled
	originalConnectLimit := config.C.WSConnectRatePerMin
	originalMaxSpectators := config.C.MaxSpectators
	config.C.ChatEnabled = true
	config.C.WSConnectRatePerMin = 0
	config.C.MaxSpectators = 500
	t.Cleanup(func() {
		config.C.ChatEnabled = originalChatEnabled
		config.C.WSConnectRatePerMin = originalConnectLimit
		config.C.MaxSpectators = originalMaxSpectators
	})

	engine := game.NewGameEngine()
	handlers := []http.HandlerFunc{
		BotHandler(engine),
		SpectatorHandler(engine),
		ChatHandler(engine, NewChatHub(engine), nil),
	}
	paths := []string{"/ws/bot", "/ws/spectator", "/ws/chat"}
	for i, handler := range handlers {
		request := httptest.NewRequest(http.MethodGet, paths[i], nil)
		request.RemoteAddr = "127.0.0.1:12345"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
	}

	snapshot := defaultWebSocketAdmissionMetrics.snapshot()
	for name, endpoint := range map[string]WebSocketEndpointMetrics{
		"bot":       snapshot.Bot,
		"spectator": snapshot.Spectator,
		"chat":      snapshot.Chat,
	} {
		if endpoint.Totals.Attempts != 1 || endpoint.Totals.Failures.Upgrade != 1 {
			t.Errorf("%s metrics = %#v, want one attempt and one upgrade failure", name, endpoint.Totals)
		}
	}
}

func TestPublicWebSocketUpgradersBoundHandshakeResources(t *testing.T) {
	for name, candidate := range map[string]struct {
		timeout time.Duration
		pool    interface{}
	}{
		"chat": {
			timeout: chatUpgrader.HandshakeTimeout,
			pool:    chatUpgrader.WriteBufferPool,
		},
		"spectator": {
			timeout: spectatorUpgrader.HandshakeTimeout,
			pool:    spectatorUpgrader.WriteBufferPool,
		},
	} {
		if candidate.timeout != 5*time.Second {
			t.Errorf("%s HandshakeTimeout = %v, want 5s", name, candidate.timeout)
		}
		if candidate.pool == nil {
			t.Errorf("%s upgrader has no shared write-buffer pool", name)
		}
	}
}

func BenchmarkWebSocketAdmissionMetricsParallelSuccessfulAdmission(b *testing.B) {
	metrics := newWebSocketAdmissionMetrics(time.Now)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			attempt := metrics.begin(websocketEndpointBot)
			attempt.upgraded()
			attempt.admitted()
		}
	})
}
