package security

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"arena-server/internal/config"

	"github.com/redis/go-redis/v9"
)

// RedisClient is the package-level Redis client used for rate limiting.
// It is nil if Redis is unavailable (graceful degradation).
var RedisClient *redis.Client

// InitRedis connects to Redis using the host and port from the loaded config.
// If the connection fails, it logs a warning and leaves RedisClient nil so
// that all rate-limit checks degrade gracefully (allow all requests).
func InitRedis() error {
	addr := fmt.Sprintf("%s:%d", config.C.RedisHost, config.C.RedisPort)

	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		slog.Warn("redis connection failed, rate limiting disabled", "addr", addr, "error", err)
		return nil
	}

	RedisClient = client
	slog.Info("redis connected", "addr", addr)
	return nil
}

// CheckRateLimit checks whether a request identified by key is within the
// allowed rate. It uses the Redis INCR + EXPIRE pattern with a sliding window
// of windowSecs seconds.
//
// Returns:
//   - allowed:   true if the request is within the limit
//   - remaining: how many requests are left in the current window
//   - resetAt:   when the current window expires
//   - err:       any Redis communication error
func CheckRateLimit(ctx context.Context, key string, maxPerWindow int, windowSecs int) (allowed bool, remaining int, resetAt time.Time, err error) {
	if RedisClient == nil {
		// Redis unavailable -- allow everything.
		return true, maxPerWindow, time.Now().Add(time.Duration(windowSecs) * time.Second), nil
	}

	redisKey := "ratelimit:" + key

	count, err := RedisClient.Incr(ctx, redisKey).Result()
	if err != nil {
		return true, maxPerWindow, time.Now().Add(time.Duration(windowSecs) * time.Second), fmt.Errorf("redis INCR failed: %w", err)
	}

	// If this is the first request in the window, set the expiry.
	if count == 1 {
		RedisClient.Expire(ctx, redisKey, time.Duration(windowSecs)*time.Second)
	}

	// Determine when the window resets.
	ttl, err := RedisClient.TTL(ctx, redisKey).Result()
	if err != nil || ttl < 0 {
		// Fallback: assume full window remaining.
		resetAt = time.Now().Add(time.Duration(windowSecs) * time.Second)
	} else {
		resetAt = time.Now().Add(ttl)
	}

	currentCount := int(count)
	if currentCount > maxPerWindow {
		return false, 0, resetAt, nil
	}

	return true, maxPerWindow - currentCount, resetAt, nil
}

// rateLimitResponse is the JSON body returned when a client exceeds the rate limit.
type rateLimitResponse struct {
	Error     string `json:"error"`
	Remaining int    `json:"remaining"`
	ResetAt   int64  `json:"reset_at"`
}

// RateLimitMiddleware returns an HTTP middleware that enforces a per-IP rate
// limit of rpm requests per 60-second window. If Redis is unavailable, all
// requests are allowed through. Each endpoint gets its own rate-limit bucket
// based on the request path, so different endpoints don't share counters.
func RateLimitMiddleware(rpm int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ExtractClientIP(r)
			key := "api:" + r.URL.Path + ":" + ip

			allowed, remaining, resetAt, err := CheckRateLimit(r.Context(), key, rpm, 60)
			if err != nil {
				slog.Warn("rate limit check error, allowing request",
					"error", err, "ip", ip)
			}

			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(time.Until(resetAt).Seconds())+1))
				w.WriteHeader(http.StatusTooManyRequests)

				resp := rateLimitResponse{
					Error:     "rate limit exceeded",
					Remaining: remaining,
					ResetAt:   resetAt.Unix(),
				}
				json.NewEncoder(w).Encode(resp)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ExtractClientIP returns the client's IP address, preferring X-Forwarded-For
// if set (takes the first entry). Falls back to the RemoteAddr from the
// request.
//
// NOTE: This trusts the first X-Forwarded-For entry because Caddy (our reverse
// proxy) always overwrites this header with the real client IP. If the proxy
// configuration changes, this function must be updated to validate the header.
func ExtractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For may contain multiple comma-separated IPs;
		// the first one is the original client.
		parts := strings.SplitN(xff, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}

	// RemoteAddr is "ip:port"; strip the port.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// If SplitHostPort fails, RemoteAddr may already be a bare IP.
		return r.RemoteAddr
	}
	return host
}
