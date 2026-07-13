package ws

import (
	"sync"
	"sync/atomic"
	"time"
)

const websocketAdmissionWindowSeconds int64 = 60

type websocketEndpoint uint8

const (
	websocketEndpointBot websocketEndpoint = iota
	websocketEndpointSpectator
	websocketEndpointChat
	websocketEndpointCount
)

type websocketFailure uint8

const (
	websocketFailureUpgrade websocketFailure = iota
	websocketFailureAuth
	websocketFailureRateLimit
	websocketFailureCapacity
	websocketFailureOther
)

type websocketAdmissionEvent uint8

const (
	websocketEventAttempt websocketAdmissionEvent = iota
	websocketEventUpgrade
	websocketEventAdmission
	websocketEventFailure
)

// WebSocketFailureMetrics contains terminal admission failures by cause.
type WebSocketFailureMetrics struct {
	Upgrade   uint64 `json:"upgrade"`
	Auth      uint64 `json:"auth"`
	RateLimit uint64 `json:"rate_limit"`
	Capacity  uint64 `json:"capacity"`
	Other     uint64 `json:"other"`
}

// WebSocketAdmissionCounts contains cumulative or rolling admission events.
type WebSocketAdmissionCounts struct {
	Attempts   uint64                  `json:"attempts"`
	Upgrades   uint64                  `json:"upgrades"`
	Admissions uint64                  `json:"admissions"`
	Failures   WebSocketFailureMetrics `json:"failures"`
}

// WebSocketLatencyMetrics reports successful end-to-end admission latency.
// Bot latency includes client authentication and loadout negotiation time.
type WebSocketLatencyMetrics struct {
	Samples uint64  `json:"samples"`
	Average float64 `json:"average_ms"`
	Max     float64 `json:"max_ms"`
}

// WebSocketRollingAdmissionMetrics is the trailing one-minute endpoint view.
type WebSocketRollingAdmissionMetrics struct {
	WebSocketAdmissionCounts
	AttemptsPerSecond       float64                 `json:"attempts_per_second"`
	UpgradesPerSecond       float64                 `json:"upgrades_per_second"`
	AdmissionsPerSecond     float64                 `json:"admissions_per_second"`
	PeakAttemptsPerSecond   uint64                  `json:"peak_attempts_per_second"`
	PeakUpgradesPerSecond   uint64                  `json:"peak_upgrades_per_second"`
	PeakAdmissionsPerSecond uint64                  `json:"peak_admissions_per_second"`
	AdmissionLatencyMS      WebSocketLatencyMetrics `json:"admission_latency_ms"`
}

// WebSocketEndpointMetrics contains lifetime and trailing-window metrics.
type WebSocketEndpointMetrics struct {
	Totals             WebSocketAdmissionCounts         `json:"totals"`
	Rolling1m          WebSocketRollingAdmissionMetrics `json:"rolling_1m"`
	AdmissionLatencyMS WebSocketLatencyMetrics          `json:"admission_latency_ms"`
}

// WebSocketAdmissionMetricsSnapshot is exposed through admin debug metrics.
type WebSocketAdmissionMetricsSnapshot struct {
	WindowSeconds int                      `json:"window_seconds"`
	Bot           WebSocketEndpointMetrics `json:"bot"`
	Spectator     WebSocketEndpointMetrics `json:"spectator"`
	Chat          WebSocketEndpointMetrics `json:"chat"`
}

type atomicAdmissionCounters struct {
	attempts   atomic.Uint64
	upgrades   atomic.Uint64
	admissions atomic.Uint64

	failureUpgrade   atomic.Uint64
	failureAuth      atomic.Uint64
	failureRateLimit atomic.Uint64
	failureCapacity  atomic.Uint64
	failureOther     atomic.Uint64

	latencySamples atomic.Uint64
	latencyNanos   atomic.Uint64
	latencyMax     atomic.Uint64
}

type admissionCounterSnapshot struct {
	counts         WebSocketAdmissionCounts
	latencySamples uint64
	latencyNanos   uint64
	latencyMax     uint64
}

func (c *atomicAdmissionCounters) add(event websocketAdmissionEvent, failure websocketFailure, latency time.Duration) {
	switch event {
	case websocketEventAttempt:
		c.attempts.Add(1)
	case websocketEventUpgrade:
		c.upgrades.Add(1)
	case websocketEventAdmission:
		c.admissions.Add(1)
		if latency < 0 {
			latency = 0
		}
		nanos := uint64(latency)
		c.latencySamples.Add(1)
		c.latencyNanos.Add(nanos)
		atomicStoreMax(&c.latencyMax, nanos)
	case websocketEventFailure:
		switch failure {
		case websocketFailureUpgrade:
			c.failureUpgrade.Add(1)
		case websocketFailureAuth:
			c.failureAuth.Add(1)
		case websocketFailureRateLimit:
			c.failureRateLimit.Add(1)
		case websocketFailureCapacity:
			c.failureCapacity.Add(1)
		default:
			c.failureOther.Add(1)
		}
	}
}

func atomicStoreMax(target *atomic.Uint64, value uint64) {
	for current := target.Load(); value > current; current = target.Load() {
		if target.CompareAndSwap(current, value) {
			return
		}
	}
}

func (c *atomicAdmissionCounters) reset() {
	c.attempts.Store(0)
	c.upgrades.Store(0)
	c.admissions.Store(0)
	c.failureUpgrade.Store(0)
	c.failureAuth.Store(0)
	c.failureRateLimit.Store(0)
	c.failureCapacity.Store(0)
	c.failureOther.Store(0)
	c.latencySamples.Store(0)
	c.latencyNanos.Store(0)
	c.latencyMax.Store(0)
}

func (c *atomicAdmissionCounters) snapshot() admissionCounterSnapshot {
	return admissionCounterSnapshot{
		counts: WebSocketAdmissionCounts{
			Attempts:   c.attempts.Load(),
			Upgrades:   c.upgrades.Load(),
			Admissions: c.admissions.Load(),
			Failures: WebSocketFailureMetrics{
				Upgrade:   c.failureUpgrade.Load(),
				Auth:      c.failureAuth.Load(),
				RateLimit: c.failureRateLimit.Load(),
				Capacity:  c.failureCapacity.Load(),
				Other:     c.failureOther.Load(),
			},
		},
		latencySamples: c.latencySamples.Load(),
		latencyNanos:   c.latencyNanos.Load(),
		latencyMax:     c.latencyMax.Load(),
	}
}

func (s *admissionCounterSnapshot) merge(other admissionCounterSnapshot) {
	s.counts.Attempts += other.counts.Attempts
	s.counts.Upgrades += other.counts.Upgrades
	s.counts.Admissions += other.counts.Admissions
	s.counts.Failures.Upgrade += other.counts.Failures.Upgrade
	s.counts.Failures.Auth += other.counts.Failures.Auth
	s.counts.Failures.RateLimit += other.counts.Failures.RateLimit
	s.counts.Failures.Capacity += other.counts.Failures.Capacity
	s.counts.Failures.Other += other.counts.Failures.Other
	s.latencySamples += other.latencySamples
	s.latencyNanos += other.latencyNanos
	if other.latencyMax > s.latencyMax {
		s.latencyMax = other.latencyMax
	}
}

type admissionMetricBucket struct {
	// Rotations happen once per minute for a given slot. All ordinary records
	// take the atomic fast path and never contend on this mutex.
	rotateMu sync.Mutex
	// second stores Unix second + 1 so zero is an uninitialized sentinel.
	second atomic.Int64
	counts atomicAdmissionCounters
}

func (b *admissionMetricBucket) prepare(second int64) bool {
	encodedSecond := second + 1
	if b.second.Load() == encodedSecond {
		return true
	}

	b.rotateMu.Lock()
	defer b.rotateMu.Unlock()
	current := b.second.Load()
	if current == encodedSecond {
		return true
	}
	// Ignore a delayed record if this slot has already advanced beyond it.
	if current != 0 && current > encodedSecond {
		return false
	}
	b.second.Store(0)
	b.counts.reset()
	b.second.Store(encodedSecond)
	return true
}

type websocketEndpointCollector struct {
	totals  atomicAdmissionCounters
	buckets [websocketAdmissionWindowSeconds]admissionMetricBucket
}

type websocketAdmissionMetrics struct {
	now       func() time.Time
	endpoints [websocketEndpointCount]websocketEndpointCollector
}

func newWebSocketAdmissionMetrics(now func() time.Time) *websocketAdmissionMetrics {
	if now == nil {
		now = time.Now
	}
	return &websocketAdmissionMetrics{now: now}
}

func (m *websocketAdmissionMetrics) record(endpoint websocketEndpoint, event websocketAdmissionEvent, failure websocketFailure, at time.Time, latency time.Duration) {
	if endpoint >= websocketEndpointCount {
		return
	}
	collector := &m.endpoints[endpoint]
	collector.totals.add(event, failure, latency)

	second := at.Unix()
	index := second % websocketAdmissionWindowSeconds
	if index < 0 {
		index += websocketAdmissionWindowSeconds
	}
	bucket := &collector.buckets[index]
	if bucket.prepare(second) {
		bucket.counts.add(event, failure, latency)
	}
}

func (m *websocketAdmissionMetrics) begin(endpoint websocketEndpoint) websocketAdmissionAttempt {
	now := m.now()
	m.record(endpoint, websocketEventAttempt, websocketFailureOther, now, 0)
	return websocketAdmissionAttempt{metrics: m, endpoint: endpoint, started: now}
}

func (m *websocketAdmissionMetrics) snapshot() WebSocketAdmissionMetricsSnapshot {
	nowSecond := m.now().Unix()
	return WebSocketAdmissionMetricsSnapshot{
		WindowSeconds: int(websocketAdmissionWindowSeconds),
		Bot:           m.endpointSnapshot(websocketEndpointBot, nowSecond),
		Spectator:     m.endpointSnapshot(websocketEndpointSpectator, nowSecond),
		Chat:          m.endpointSnapshot(websocketEndpointChat, nowSecond),
	}
}

func (m *websocketAdmissionMetrics) endpointSnapshot(endpoint websocketEndpoint, nowSecond int64) WebSocketEndpointMetrics {
	collector := &m.endpoints[endpoint]
	totals := collector.totals.snapshot()
	rolling := admissionCounterSnapshot{}
	peak := WebSocketAdmissionCounts{}
	cutoff := nowSecond - websocketAdmissionWindowSeconds + 1
	for i := range collector.buckets {
		bucket := &collector.buckets[i]
		encodedSecond := bucket.second.Load()
		if encodedSecond == 0 {
			continue
		}
		second := encodedSecond - 1
		if second < cutoff || second > nowSecond {
			continue
		}
		bucketSnapshot := bucket.counts.snapshot()
		// A concurrent once-per-minute rotation invalidates this sample; the
		// next admin scrape observes the new bucket instead of mixed counters.
		if bucket.second.Load() != encodedSecond {
			continue
		}
		rolling.merge(bucketSnapshot)
		if bucketSnapshot.counts.Attempts > peak.Attempts {
			peak.Attempts = bucketSnapshot.counts.Attempts
		}
		if bucketSnapshot.counts.Upgrades > peak.Upgrades {
			peak.Upgrades = bucketSnapshot.counts.Upgrades
		}
		if bucketSnapshot.counts.Admissions > peak.Admissions {
			peak.Admissions = bucketSnapshot.counts.Admissions
		}
	}

	window := float64(websocketAdmissionWindowSeconds)
	return WebSocketEndpointMetrics{
		Totals: totals.counts,
		Rolling1m: WebSocketRollingAdmissionMetrics{
			WebSocketAdmissionCounts: rolling.counts,
			AttemptsPerSecond:        float64(rolling.counts.Attempts) / window,
			UpgradesPerSecond:        float64(rolling.counts.Upgrades) / window,
			AdmissionsPerSecond:      float64(rolling.counts.Admissions) / window,
			PeakAttemptsPerSecond:    peak.Attempts,
			PeakUpgradesPerSecond:    peak.Upgrades,
			PeakAdmissionsPerSecond:  peak.Admissions,
			AdmissionLatencyMS:       admissionLatencySnapshot(rolling),
		},
		AdmissionLatencyMS: admissionLatencySnapshot(totals),
	}
}

func admissionLatencySnapshot(snapshot admissionCounterSnapshot) WebSocketLatencyMetrics {
	latency := WebSocketLatencyMetrics{Samples: snapshot.latencySamples}
	if snapshot.latencySamples == 0 {
		return latency
	}
	latency.Average = float64(snapshot.latencyNanos) / float64(snapshot.latencySamples) / float64(time.Millisecond)
	latency.Max = float64(snapshot.latencyMax) / float64(time.Millisecond)
	return latency
}

type websocketAdmissionAttempt struct {
	metrics      *websocketAdmissionMetrics
	endpoint     websocketEndpoint
	started      time.Time
	upgradedOnce bool
	terminal     bool
}

func (a *websocketAdmissionAttempt) upgraded() {
	if a == nil || a.terminal || a.upgradedOnce {
		return
	}
	a.upgradedOnce = true
	a.metrics.record(a.endpoint, websocketEventUpgrade, websocketFailureOther, a.metrics.now(), 0)
}

func (a *websocketAdmissionAttempt) admitted() {
	if a == nil || a.terminal {
		return
	}
	a.terminal = true
	now := a.metrics.now()
	a.metrics.record(a.endpoint, websocketEventAdmission, websocketFailureOther, now, now.Sub(a.started))
}

func (a *websocketAdmissionAttempt) fail(failure websocketFailure) {
	if a == nil || a.terminal {
		return
	}
	a.terminal = true
	a.metrics.record(a.endpoint, websocketEventFailure, failure, a.metrics.now(), 0)
}

func (a *websocketAdmissionAttempt) finish() {
	if a == nil || a.terminal {
		return
	}
	a.fail(websocketFailureOther)
}

var defaultWebSocketAdmissionMetrics = newWebSocketAdmissionMetrics(time.Now)

func beginWebSocketAdmission(endpoint websocketEndpoint) websocketAdmissionAttempt {
	return defaultWebSocketAdmissionMetrics.begin(endpoint)
}

// GetWebSocketAdmissionMetrics returns a lock-free snapshot for admin
// observability. Handler hot paths update atomics; bucket mutexes are touched
// only when a once-per-minute ring slot rotates.
func GetWebSocketAdmissionMetrics() WebSocketAdmissionMetricsSnapshot {
	return defaultWebSocketAdmissionMetrics.snapshot()
}
