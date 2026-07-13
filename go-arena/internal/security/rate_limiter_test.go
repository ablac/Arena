package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arena-server/internal/config"

	"github.com/redis/go-redis/v9"
)

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
