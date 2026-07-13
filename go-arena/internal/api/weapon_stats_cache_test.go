package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type doneObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *doneObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestWeaponStatsCacheCollapsesConcurrentColdLoads(t *testing.T) {
	const requestCount = 32

	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	var loaderCalls atomic.Int32
	loaderStarted := make(chan struct{})
	releaseLoader := make(chan struct{})
	var startOnce sync.Once
	cache := newWeaponStatsCache(15*time.Second, func() time.Time { return now }, func(context.Context) (WeaponStatsResponse, error) {
		loaderCalls.Add(1)
		startOnce.Do(func() { close(loaderStarted) })
		<-releaseLoader
		return testWeaponStatsResponse(now), nil
	})

	startRequests := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, requestCount)
	var requests sync.WaitGroup
	requests.Add(requestCount)
	for range requestCount {
		go func() {
			defer requests.Done()
			<-startRequests
			recorder := httptest.NewRecorder()
			cache.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/weapon-stats", nil))
			responses <- recorder
		}()
	}

	close(startRequests)
	<-loaderStarted
	close(releaseLoader)
	requests.Wait()
	close(responses)

	if got := loaderCalls.Load(); got != 1 {
		t.Fatalf("loader calls = %d, want 1", got)
	}
	for recorder := range responses {
		if recorder.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		var response WeaponStatsResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Errorf("decode response: %v", err)
			continue
		}
		if len(response.Entries) != 1 || response.Entries[0].Weapon != "railgun" {
			t.Errorf("unexpected response entries: %#v", response.Entries)
		}
	}
}

func TestWeaponStatsCacheLeaderCancellationDoesNotCancelSharedLoad(t *testing.T) {
	type contextKey string
	const requestValueKey contextKey = "request-value"

	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	loaderStarted := make(chan struct{})
	releaseLoader := make(chan struct{})
	loaderContext := make(chan context.Context, 1)
	var loaderCalls atomic.Int32
	cache := newWeaponStatsCache(15*time.Second, func() time.Time { return now }, func(ctx context.Context) (WeaponStatsResponse, error) {
		loaderCalls.Add(1)
		loaderContext <- ctx
		close(loaderStarted)
		select {
		case <-releaseLoader:
			return testWeaponStatsResponse(now), nil
		case <-ctx.Done():
			return WeaponStatsResponse{}, ctx.Err()
		}
	})

	leaderContext, cancelLeader := context.WithCancel(context.WithValue(context.Background(), requestValueKey, "leader"))
	leaderDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/weapon-stats", nil).WithContext(leaderContext)
		recorder := httptest.NewRecorder()
		cache.ServeHTTP(recorder, request)
		leaderDone <- recorder
	}()

	<-loaderStarted
	loadContext := <-loaderContext
	if got := loadContext.Value(requestValueKey); got != "leader" {
		t.Fatalf("loader request value = %v, want leader", got)
	}
	if _, ok := loadContext.Deadline(); !ok {
		t.Fatal("loader context has no bounded deadline")
	}

	waiterContext := &doneObservedContext{Context: context.Background(), observed: make(chan struct{})}
	waiterDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/weapon-stats", nil).WithContext(waiterContext)
		recorder := httptest.NewRecorder()
		cache.ServeHTTP(recorder, request)
		waiterDone <- recorder
	}()
	select {
	case <-waiterContext.observed:
	case <-time.After(time.Second):
		t.Fatal("healthy waiter did not join the in-flight load")
	}

	cancelLeader()
	select {
	case leader := <-leaderDone:
		if leader.Code != http.StatusInternalServerError {
			t.Fatalf("canceled leader status = %d, want %d", leader.Code, http.StatusInternalServerError)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled leader remained blocked on the shared load")
	}

	close(releaseLoader)

	select {
	case waiter := <-waiterDone:
		if waiter.Code != http.StatusOK {
			t.Fatalf("healthy waiter status = %d, body = %s", waiter.Code, waiter.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("healthy waiter did not receive the shared load result")
	}
	if got := loaderCalls.Load(); got != 1 {
		t.Fatalf("loader calls = %d, want one shared load", got)
	}
}

func TestWeaponStatsCacheExpiresSuccessfulResponses(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	loaderCalls := 0
	cache := newWeaponStatsCache(15*time.Second, func() time.Time { return now }, func(context.Context) (WeaponStatsResponse, error) {
		loaderCalls++
		response := testWeaponStatsResponse(now)
		response.Entries[0].Kills = loaderCalls
		return response, nil
	})

	first := serveWeaponStatsCache(cache, "")
	now = now.Add(14 * time.Second)
	second := serveWeaponStatsCache(cache, "")
	if loaderCalls != 1 {
		t.Fatalf("loader calls before expiry = %d, want 1", loaderCalls)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatal("cached response body changed before expiry")
	}

	now = now.Add(time.Second)
	third := serveWeaponStatsCache(cache, "")
	if loaderCalls != 2 {
		t.Fatalf("loader calls after expiry = %d, want 2", loaderCalls)
	}
	if first.Body.String() == third.Body.String() {
		t.Fatal("expired response body was reused")
	}
}

func TestWeaponStatsCacheDoesNotCacheFailures(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	loaderCalls := 0
	cache := newWeaponStatsCache(15*time.Second, func() time.Time { return now }, func(context.Context) (WeaponStatsResponse, error) {
		loaderCalls++
		if loaderCalls == 1 {
			return WeaponStatsResponse{}, errors.New("database unavailable")
		}
		return testWeaponStatsResponse(now), nil
	})

	failed := serveWeaponStatsCache(cache, "")
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed status = %d, want %d", failed.Code, http.StatusInternalServerError)
	}
	if got := failed.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("failed Cache-Control = %q, want no-store", got)
	}
	if got := failed.Header().Get("ETag"); got != "" {
		t.Errorf("failed ETag = %q, want empty", got)
	}

	succeeded := serveWeaponStatsCache(cache, "")
	if succeeded.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want %d", succeeded.Code, http.StatusOK)
	}
	if loaderCalls != 2 {
		t.Fatalf("loader calls after retry = %d, want 2", loaderCalls)
	}

	_ = serveWeaponStatsCache(cache, "")
	if loaderCalls != 2 {
		t.Fatalf("loader calls after cached success = %d, want 2", loaderCalls)
	}
}

func TestWeaponStatsCachePreservesLoaderErrorMessage(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	cache := newWeaponStatsCache(15*time.Second, func() time.Time { return now }, func(context.Context) (WeaponStatsResponse, error) {
		return WeaponStatsResponse{}, newWeaponStatsLoadError("failed to get recent weapon performance", errors.New("database unavailable"))
	})

	response := serveWeaponStatsCache(cache, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	var body ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error != "failed to get recent weapon performance" {
		t.Errorf("error = %q, want preserved loader message", body.Error)
	}
}

func TestWeaponStatsCacheSupportsConditionalRequests(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	loaderCalls := 0
	cache := newWeaponStatsCache(15*time.Second, func() time.Time { return now }, func(context.Context) (WeaponStatsResponse, error) {
		loaderCalls++
		return testWeaponStatsResponse(now), nil
	})

	first := serveWeaponStatsCache(cache, "")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusOK)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("successful response is missing ETag")
	}
	cacheControl := first.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "public") || !strings.Contains(cacheControl, "max-age=15") {
		t.Errorf("Cache-Control = %q, want public max-age=15", cacheControl)
	}

	conditional := serveWeaponStatsCache(cache, etag)
	if conditional.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want %d", conditional.Code, http.StatusNotModified)
	}
	if conditional.Body.Len() != 0 {
		t.Errorf("conditional body length = %d, want 0", conditional.Body.Len())
	}
	if got := conditional.Header().Get("ETag"); got != etag {
		t.Errorf("conditional ETag = %q, want %q", got, etag)
	}

	weakConditional := serveWeaponStatsCache(cache, "W/"+etag)
	if weakConditional.Code != http.StatusNotModified {
		t.Fatalf("weak conditional status = %d, want %d", weakConditional.Code, http.StatusNotModified)
	}
	if loaderCalls != 1 {
		t.Fatalf("loader calls = %d, want 1", loaderCalls)
	}
}

func BenchmarkWeaponStatsCacheWarm(b *testing.B) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	var loaderCalls atomic.Int32
	cache := newWeaponStatsCache(15*time.Second, func() time.Time { return now }, func(context.Context) (WeaponStatsResponse, error) {
		loaderCalls.Add(1)
		return testWeaponStatsResponse(now), nil
	})
	if recorder := serveWeaponStatsCache(cache, ""); recorder.Code != http.StatusOK {
		b.Fatalf("warm-up status = %d, want %d", recorder.Code, http.StatusOK)
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			recorder := httptest.NewRecorder()
			cache.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/weapon-stats", nil))
			if recorder.Code != http.StatusOK {
				b.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
		}
	})
	b.StopTimer()
	if got := loaderCalls.Load(); got != 1 {
		b.Fatalf("loader calls = %d, want 1", got)
	}
}

func serveWeaponStatsCache(cache http.Handler, ifNoneMatch string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/weapon-stats", nil)
	if ifNoneMatch != "" {
		request.Header.Set("If-None-Match", ifNoneMatch)
	}
	recorder := httptest.NewRecorder()
	cache.ServeHTTP(recorder, request)
	return recorder
}

func testWeaponStatsResponse(updatedAt time.Time) WeaponStatsResponse {
	return WeaponStatsResponse{
		Entries: []WeaponStatsEntry{{
			Rank:      1,
			Weapon:    "railgun",
			Tier:      "S",
			MetaScore: 99.5,
		}},
		UpdatedAt: updatedAt,
	}
}
