package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

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
