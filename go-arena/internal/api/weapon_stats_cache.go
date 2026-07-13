package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The public standings UI polls every 15 seconds. Keep the server-side result
// across several polls so one viewer cannot retrigger the three aggregate
// database queries on every refresh; one minute is still shorter than a
// normal balance/round feedback cycle.
const (
	weaponStatsCacheTTL    = time.Minute
	weaponStatsLoadTimeout = 10 * time.Second
)

type weaponStatsLoader func(context.Context) (WeaponStatsResponse, error)

type weaponStatsCacheResult struct {
	body []byte
	etag string
	err  error
}

type weaponStatsFlight struct {
	done   chan struct{}
	result weaponStatsCacheResult
}

type weaponStatsCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	now       func() time.Time
	load      weaponStatsLoader
	body      []byte
	etag      string
	expiresAt time.Time
	flight    *weaponStatsFlight
}

var defaultWeaponStatsCache = newWeaponStatsCache(weaponStatsCacheTTL, time.Now, loadWeaponStats)

func newWeaponStatsCache(ttl time.Duration, now func() time.Time, load weaponStatsLoader) *weaponStatsCache {
	return &weaponStatsCache{
		ttl:  ttl,
		now:  now,
		load: load,
	}
}

func (c *weaponStatsCache) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	result := c.get(r.Context())
	if result.err != nil {
		w.Header().Set("Cache-Control", "no-store")
		writeError(w, http.StatusInternalServerError, weaponStatsPublicError(result.err))
		return
	}

	w.Header().Set("Cache-Control", c.cacheControl())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", result.etag)
	if ifNoneMatch(r.Header.Get("If-None-Match"), result.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(result.body); err != nil {
		slog.Error("failed to write cached weapon stats response", "error", err)
	}
}

func (c *weaponStatsCache) get(ctx context.Context) weaponStatsCacheResult {
	now := c.now()
	c.mu.Lock()
	if len(c.body) > 0 && now.Before(c.expiresAt) {
		result := weaponStatsCacheResult{body: c.body, etag: c.etag}
		c.mu.Unlock()
		return result
	}
	if c.flight != nil {
		flight := c.flight
		c.mu.Unlock()
		return waitForWeaponStatsFlight(ctx, flight)
	}

	flight := &weaponStatsFlight{done: make(chan struct{})}
	c.flight = flight
	c.mu.Unlock()
	// Preserve request-scoped values needed by the loader, but let the shared
	// load outlive any one caller while retaining a hard upper bound.
	loadContext, cancelLoad := context.WithTimeout(context.WithoutCancel(ctx), weaponStatsLoadTimeout)
	go c.loadFlight(flight, loadContext, cancelLoad)

	return waitForWeaponStatsFlight(ctx, flight)
}

func waitForWeaponStatsFlight(ctx context.Context, flight *weaponStatsFlight) weaponStatsCacheResult {
	select {
	case <-flight.done:
		return flight.result
	case <-ctx.Done():
		return weaponStatsCacheResult{err: ctx.Err()}
	}
}

func (c *weaponStatsCache) loadFlight(flight *weaponStatsFlight, ctx context.Context, cancel context.CancelFunc) {
	defer cancel()

	response, err := c.load(ctx)
	result := weaponStatsCacheResult{err: err}
	if err == nil {
		result.body, result.etag, result.err = encodeWeaponStatsResponse(response)
	}

	c.mu.Lock()
	if result.err == nil {
		c.body = result.body
		c.etag = result.etag
		c.expiresAt = c.now().Add(c.ttl)
	}
	flight.result = result
	c.flight = nil
	close(flight.done)
	c.mu.Unlock()
}

func encodeWeaponStatsResponse(response WeaponStatsResponse) ([]byte, string, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(response); err != nil {
		return nil, "", err
	}
	encoded := body.Bytes()
	hash := sha256.Sum256(encoded)
	return encoded, fmt.Sprintf("\"%x\"", hash), nil
}

func (c *weaponStatsCache) cacheControl() string {
	maxAge := c.ttl / time.Second
	if maxAge < 0 {
		maxAge = 0
	}
	return fmt.Sprintf("public, max-age=%d", maxAge)
}

func ifNoneMatch(headerValue, etag string) bool {
	if headerValue == "" || etag == "" {
		return false
	}
	wanted := strings.TrimPrefix(etag, "W/")
	for _, candidate := range strings.Split(headerValue, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == wanted {
			return true
		}
	}
	return false
}

type weaponStatsLoadError struct {
	publicMessage string
	err           error
}

func newWeaponStatsLoadError(publicMessage string, err error) error {
	return &weaponStatsLoadError{publicMessage: publicMessage, err: err}
}

func (e *weaponStatsLoadError) Error() string {
	return fmt.Sprintf("%s: %v", e.publicMessage, e.err)
}

func (e *weaponStatsLoadError) Unwrap() error {
	return e.err
}

func weaponStatsPublicError(err error) string {
	var loadErr *weaponStatsLoadError
	if errors.As(err, &loadErr) {
		return loadErr.publicMessage
	}
	return "failed to get weapon stats"
}
