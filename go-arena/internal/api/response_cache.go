package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// responseCache is a keyed TTL + ETag + single-flight cache for public GET
// endpoints that rebuild identical bodies per request (leaderboard, cosmetics
// catalog, bot-setup). It mirrors the weaponStatsCache pattern but is
// reusable: one concurrent load per key, cached encoded body with a sha256
// ETag, and 304 responses via If-None-Match. Callers must keep the key space
// bounded (normalize query params before keying).
type responseCache struct {
	mu          sync.Mutex
	ttl         time.Duration
	loadTimeout time.Duration
	now         func() time.Time
	entries     map[string]*responseCacheEntry
}

type responseCacheEntry struct {
	body      []byte
	etag      string
	expiresAt time.Time
	flight    *responseCacheFlight
}

type responseCacheFlight struct {
	done chan struct{}
	body []byte
	etag string
	err  error
}

func newResponseCache(ttl, loadTimeout time.Duration, now func() time.Time) *responseCache {
	return &responseCache{
		ttl:         ttl,
		loadTimeout: loadTimeout,
		now:         now,
		entries:     make(map[string]*responseCacheEntry),
	}
}

// Serve responds with the cached body for key, loading it at most once per
// TTL expiry regardless of concurrent callers. load must return the fully
// encoded response body. Load errors are never cached; the requester that
// observes one gets errorStatus with publicError.
func (c *responseCache) Serve(w http.ResponseWriter, r *http.Request, key string, load func(context.Context) ([]byte, error), publicError string, errorStatus int) {
	body, etag, err := c.get(r.Context(), key, load)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		writeError(w, errorStatus, publicError)
		return
	}

	maxAge := int(c.ttl / time.Second)
	if maxAge < 0 {
		maxAge = 0
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", maxAge))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag)
	if ifNoneMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		slog.Error("failed to write cached response", "key", key, "error", err)
	}
}

func (c *responseCache) get(ctx context.Context, key string, load func(context.Context) ([]byte, error)) ([]byte, string, error) {
	now := c.now()
	c.mu.Lock()
	entry := c.entries[key]
	if entry == nil {
		entry = &responseCacheEntry{}
		c.entries[key] = entry
	}
	if len(entry.body) > 0 && now.Before(entry.expiresAt) {
		body, etag := entry.body, entry.etag
		c.mu.Unlock()
		return body, etag, nil
	}
	if entry.flight != nil {
		flight := entry.flight
		c.mu.Unlock()
		return waitResponseCacheFlight(ctx, flight)
	}

	flight := &responseCacheFlight{done: make(chan struct{})}
	entry.flight = flight
	c.mu.Unlock()

	// Let the shared load outlive any single caller, with a hard upper bound.
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.loadTimeout)
	go func() {
		defer cancel()
		body, err := load(loadCtx)
		flight.err = err
		if err == nil {
			flight.body = body
			hash := sha256.Sum256(body)
			flight.etag = fmt.Sprintf("\"%x\"", hash)
		}
		c.mu.Lock()
		if err == nil {
			entry.body = flight.body
			entry.etag = flight.etag
			entry.expiresAt = c.now().Add(c.ttl)
		}
		entry.flight = nil
		close(flight.done)
		c.mu.Unlock()
	}()

	return waitResponseCacheFlight(ctx, flight)
}

func waitResponseCacheFlight(ctx context.Context, flight *responseCacheFlight) ([]byte, string, error) {
	select {
	case <-flight.done:
		return flight.body, flight.etag, flight.err
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
}
