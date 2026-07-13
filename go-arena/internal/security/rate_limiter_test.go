package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"arena-server/internal/config"

	"github.com/redis/go-redis/v9"
)

type recordingRateLimitEvaluator struct {
	calls  int
	script string
	keys   []string
	args   []interface{}
	result []interface{}
	err    error
}

func (e *recordingRateLimitEvaluator) Eval(_ context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	e.calls++
	e.script = script
	e.keys = append([]string(nil), keys...)
	e.args = append([]interface{}(nil), args...)
	return redis.NewCmdResult(e.result, e.err)
}

func TestCheckRateLimitUsesOneAtomicRedisRoundTrip(t *testing.T) {
	evaluator := &recordingRateLimitEvaluator{
		result: []interface{}{int64(2), int64(42_000)},
	}
	now := time.Unix(1_700_000_000, 0)

	allowed, remaining, resetAt, err := checkRateLimitWithEvaluator(
		context.Background(), evaluator, "bot:connect", 5, 60, now,
	)
	if err != nil {
		t.Fatalf("checkRateLimitWithEvaluator: %v", err)
	}
	if !allowed || remaining != 3 {
		t.Fatalf("decision = allowed %t remaining %d, want true/3", allowed, remaining)
	}
	if want := now.Add(42 * time.Second); !resetAt.Equal(want) {
		t.Fatalf("resetAt = %v, want %v", resetAt, want)
	}
	if evaluator.calls != 1 {
		t.Fatalf("Redis calls = %d, want exactly 1", evaluator.calls)
	}
	if len(evaluator.keys) != 1 || evaluator.keys[0] != "ratelimit:bot:connect" {
		t.Fatalf("Redis keys = %#v", evaluator.keys)
	}
	if len(evaluator.args) != 1 || evaluator.args[0] != int64(60_000) {
		t.Fatalf("Redis args = %#v, want 60000ms expiry", evaluator.args)
	}
	if !strings.Contains(evaluator.script, "INCR") || !strings.Contains(evaluator.script, "PTTL") || !strings.Contains(evaluator.script, "PEXPIRE") {
		t.Fatalf("rate-limit script does not atomically increment and repair expiry: %q", evaluator.script)
	}
}

func TestCheckRateLimitRedisIntegration(t *testing.T) {
	addr := strings.TrimSpace(os.Getenv("ARENA_TEST_REDIS_ADDR"))
	if addr == "" {
		t.Skip("set ARENA_TEST_REDIS_ADDR to run Redis integration test")
	}

	client := redis.NewClient(&redis.Options{Addr: addr})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("ping test Redis: %v", err)
	}
	previous := RedisClient
	RedisClient = client
	key := "integration:" + strings.ReplaceAll(t.Name(), "/", ":") + ":" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		_ = client.Del(ctx, "ratelimit:"+key).Err()
		_ = client.Close()
		RedisClient = previous
	})

	for attempt, wantAllowed := range []bool{true, true, false} {
		allowed, remaining, _, err := CheckRateLimit(ctx, key, 2, 1)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
		if allowed != wantAllowed {
			t.Fatalf("attempt %d allowed = %t, want %t (remaining %d)", attempt+1, allowed, wantAllowed, remaining)
		}
	}

	ttl, err := client.PTTL(ctx, "ratelimit:"+key).Result()
	if err != nil || ttl <= 0 || ttl > time.Second {
		t.Fatalf("rate-limit TTL = %v, error %v", ttl, err)
	}
	time.Sleep(1100 * time.Millisecond)
	allowed, remaining, _, err := CheckRateLimit(ctx, key, 2, 1)
	if err != nil || !allowed || remaining != 1 {
		t.Fatalf("post-expiry decision = allowed %t remaining %d error %v", allowed, remaining, err)
	}
}

func setTrustedProxyCIDRs(t *testing.T, cidrs string) {
	t.Helper()
	previous := config.C.TrustedProxyCIDRs
	config.C.TrustedProxyCIDRs = cidrs
	t.Cleanup(func() { config.C.TrustedProxyCIDRs = previous })
}

func setTrustedCloudflareProxyCIDRs(t *testing.T, cidrs string) {
	t.Helper()
	previous := config.C.TrustedCloudflareProxyCIDRs
	config.C.TrustedCloudflareProxyCIDRs = cidrs
	t.Cleanup(func() { config.C.TrustedCloudflareProxyCIDRs = previous })
}

func TestExtractClientIPIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	setTrustedProxyCIDRs(t, "127.0.0.1/32,::1/128")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.RemoteAddr = "198.51.100.40:4242"
	request.Header.Set("CF-Connecting-IP", "203.0.113.10")
	request.Header.Set("X-Forwarded-For", "203.0.113.11")

	if got := ExtractClientIP(request); got != "198.51.100.40" {
		t.Fatalf("ExtractClientIP() = %q, want immediate untrusted peer", got)
	}
}

func TestExtractClientIPHonorsForwardedHeadersFromTrustedProxy(t *testing.T) {
	setTrustedProxyCIDRs(t, "10.0.0.0/8,2001:db8:100::/48")

	t.Run("Cloudflare header", func(t *testing.T) {
		setTrustedCloudflareProxyCIDRs(t, "10.20.30.40/32")
		request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		request.RemoteAddr = "10.20.30.40:4242"
		request.Header.Set("CF-Connecting-IP", "203.0.113.10")
		request.Header.Set("X-Forwarded-For", "203.0.113.11")

		if got := ExtractClientIP(request); got != "203.0.113.10" {
			t.Fatalf("ExtractClientIP() = %q, want validated CF client", got)
		}
	})

	t.Run("forwarded chain", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		request.RemoteAddr = "10.20.30.40:4242"
		request.Header.Set("X-Forwarded-For", "198.51.100.8, 10.10.10.10")

		if got := ExtractClientIP(request); got != "198.51.100.8" {
			t.Fatalf("ExtractClientIP() = %q, want rightmost untrusted client", got)
		}
	})

	t.Run("IPv6 proxy", func(t *testing.T) {
		setTrustedCloudflareProxyCIDRs(t, "2001:db8:100::7/128")
		request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		request.RemoteAddr = "[2001:db8:100::7]:4242"
		request.Header.Set("CF-Connecting-IP", "2001:db8:200::9")

		if got := ExtractClientIP(request); got != "2001:db8:200::9" {
			t.Fatalf("ExtractClientIP() = %q, want validated IPv6 client", got)
		}
	})
}

func TestExtractClientIPDoesNotTrustCloudflareHeaderFromGenericProxy(t *testing.T) {
	setTrustedProxyCIDRs(t, "10.0.0.0/8")
	setTrustedCloudflareProxyCIDRs(t, "")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.RemoteAddr = "10.20.30.40:4242"
	request.Header.Set("CF-Connecting-IP", "203.0.113.99")
	request.Header.Set("X-Forwarded-For", "198.51.100.8, 10.10.10.10")

	if got := ExtractClientIP(request); got != "198.51.100.8" {
		t.Fatalf("ExtractClientIP() = %q, want XFF client; generic proxy must not authorize CF-Connecting-IP", got)
	}
}

func TestExtractClientIPUsesRightmostUntrustedForwardedHop(t *testing.T) {
	setTrustedProxyCIDRs(t, "10.0.0.0/8")

	t.Run("nearest untrusted hop", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		request.RemoteAddr = "10.20.30.40:4242"
		request.Header.Set("X-Forwarded-For", "192.0.2.50, 198.51.100.20, 10.10.10.10")

		if got := ExtractClientIP(request); got != "198.51.100.20" {
			t.Fatalf("ExtractClientIP() = %q, want nearest untrusted hop", got)
		}
	})

	t.Run("all claimed hops trusted", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		request.RemoteAddr = "10.20.30.40:4242"
		request.Header.Set("X-Forwarded-For", "10.1.1.1, 10.10.10.10")

		if got := ExtractClientIP(request); got != "10.20.30.40" {
			t.Fatalf("ExtractClientIP() = %q, want immediate proxy when no untrusted hop exists", got)
		}
	})
}

func TestExtractClientIPRejectsMalformedForwardedAddresses(t *testing.T) {
	setTrustedProxyCIDRs(t, "10.0.0.0/8")

	tests := []struct {
		name string
		cf   string
		xff  string
	}{
		{name: "Cloudflare value with port", cf: "203.0.113.10:443"},
		{name: "Cloudflare list", cf: "203.0.113.10, 203.0.113.11"},
		{name: "forwarded chain with invalid hop", xff: "203.0.113.10, not-an-ip"},
		{name: "forwarded value with port", xff: "203.0.113.10:443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			request.RemoteAddr = "10.20.30.40:4242"
			request.Header.Set("CF-Connecting-IP", tt.cf)
			request.Header.Set("X-Forwarded-For", tt.xff)

			if got := ExtractClientIP(request); got != "10.20.30.40" {
				t.Fatalf("ExtractClientIP() = %q, want immediate proxy for malformed header", got)
			}
		})
	}
}

func TestExtractClientIPPreservesDirectRequestBehavior(t *testing.T) {
	setTrustedProxyCIDRs(t, "127.0.0.1/32,::1/128")

	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{name: "IPv4 socket", remoteAddr: "198.51.100.40:4242", want: "198.51.100.40"},
		{name: "IPv6 socket", remoteAddr: "[2001:db8::40]:4242", want: "2001:db8::40"},
		{name: "bare IP", remoteAddr: "198.51.100.41", want: "198.51.100.41"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			request.RemoteAddr = tt.remoteAddr

			if got := ExtractClientIP(request); got != tt.want {
				t.Fatalf("ExtractClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFailClosedRateLimitMiddlewareRejectsSideEffectsWhenRedisIsUnavailable(t *testing.T) {
	previous := RedisClient
	RedisClient = nil
	t.Cleanup(func() { RedisClient = previous })

	for _, path := range []string{
		"/api/v1/account/email/start",
		"/api/v1/account/cosmetics/checkout",
		"/arena/api/v1/account/cosmetics/checkout",
	} {
		t.Run(path, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"pilot@example.com"}`))

			FailClosedRateLimitMiddleware(3)(next).ServeHTTP(recorder, request)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if called {
				t.Fatal("protected handler ran without a working rate limiter")
			}
			if recorder.Header().Get("Retry-After") != "60" {
				t.Fatalf("Retry-After = %q", recorder.Header().Get("Retry-After"))
			}
			if !strings.Contains(recorder.Body.String(), `"code":"RATE_LIMIT_UNAVAILABLE"`) {
				t.Fatalf("body = %s", recorder.Body.String())
			}
		})
	}
}

func TestFailClosedRateLimitMiddlewareRejectsRedisErrors(t *testing.T) {
	previous := RedisClient
	RedisClient = redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  10 * time.Millisecond,
		ReadTimeout:  10 * time.Millisecond,
		WriteTimeout: 10 * time.Millisecond,
		MaxRetries:   -1,
	})
	t.Cleanup(func() {
		_ = RedisClient.Close()
		RedisClient = previous
	})

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/account/email/start", nil)

	FailClosedRateLimitMiddleware(3)(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable || called {
		t.Fatalf("status = %d, called = %t, body = %s", recorder.Code, called, recorder.Body.String())
	}
}

func TestDefaultRateLimitMiddlewareStillDegradesGracefully(t *testing.T) {
	previous := RedisClient
	RedisClient = nil
	t.Cleanup(func() { RedisClient = previous })

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)

	RateLimitMiddleware(3)(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
