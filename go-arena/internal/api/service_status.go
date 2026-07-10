package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/version"

	"github.com/go-chi/chi/v5"
)

const (
	maxBroadcastRunes       = 500
	maxBroadcastExpiry      = 7 * 24 * time.Hour
	minBroadcastExpiry      = time.Minute
	maintenanceFallbackTTL  = 30 * time.Minute
	maintenanceRetrySeconds = 60
)

var (
	errNoCurrentBroadcast = errors.New("no active broadcast")
	errStaleBroadcast     = errors.New("broadcast is no longer current")
	errStaleMaintenance   = errors.New("maintenance update is no longer current")
)

type serviceNoticeStore interface {
	Append(context.Context, db.ServiceNoticeEvent) (db.ServiceNoticeEvent, error)
	Current(context.Context) ([]db.ServiceNoticeEvent, error)
	List(context.Context, int) ([]db.ServiceNoticeEvent, error)
}

type postgresServiceNoticeStore struct{}

func (postgresServiceNoticeStore) Append(ctx context.Context, evt db.ServiceNoticeEvent) (db.ServiceNoticeEvent, error) {
	return db.AppendServiceNoticeEvent(ctx, evt)
}

func (postgresServiceNoticeStore) Current(ctx context.Context) ([]db.ServiceNoticeEvent, error) {
	return db.CurrentServiceNoticeEvents(ctx)
}

func (postgresServiceNoticeStore) List(ctx context.Context, limit int) ([]db.ServiceNoticeEvent, error) {
	return db.ListServiceNoticeEvents(ctx, limit)
}

// ServiceStatusService serializes state transitions so an older concurrent
// request cannot overwrite a newer database revision in the in-memory hub.
type ServiceStatusService struct {
	mu                 sync.Mutex
	engine             *game.GameEngine
	bus                *EventBus
	store              serviceNoticeStore
	broadcastEventID   int64
	maintenanceEventID int64
}

func NewServiceStatusService(engine *game.GameEngine, bus *EventBus) *ServiceStatusService {
	return &ServiceStatusService{
		engine: engine,
		bus:    bus,
		store:  postgresServiceNoticeStore{},
	}
}

func newServiceStatusServiceWithStore(engine *game.GameEngine, bus *EventBus, store serviceNoticeStore) *ServiceStatusService {
	return &ServiceStatusService{engine: engine, bus: bus, store: store}
}

// Load restores the last event in each slot. If this process is already the
// target build of a scheduled update, it appends the durable completion
// tombstone immediately; this also supports the older updater sidecar that did
// not yet send phase callbacks.
func (s *ServiceStatusService) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	events, err := s.store.Current(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	status := statusFromCurrentEvents(events, now)
	s.engine.RestoreServiceStatus(status)
	for _, evt := range events {
		s.rememberCurrentEventLocked(evt)
		if !evt.Active || evt.ExpiresAt == nil {
			continue
		}
		if !evt.ExpiresAt.After(now) {
			clear, err := s.store.Append(ctx, db.ServiceNoticeEvent{
				Slot:         evt.Slot,
				Active:       false,
				Severity:     "info",
				TargetCommit: evt.TargetCommit,
				Source:       "expired",
			})
			if err != nil {
				return fmt.Errorf("persist expired %s notice: %w", evt.Slot, err)
			}
			s.applyEventLocked(clear, false)
			continue
		}
		s.scheduleExpiryLocked(evt)
	}

	status = s.engine.GetServiceStatus()
	running := version.ResolvedCommit()
	if status.Maintenance != nil && (status.Maintenance.Source == "admin-restart" ||
		(running != "" && running != "unknown" && status.Maintenance.TargetCommit == running)) {
		clear, err := s.store.Append(ctx, db.ServiceNoticeEvent{
			Slot:         db.ServiceNoticeSlotMaintenance,
			Active:       false,
			Severity:     "info",
			Source:       "startup-reconcile",
			TargetCommit: status.Maintenance.TargetCommit,
		})
		if err != nil {
			return fmt.Errorf("clear completed maintenance: %w", err)
		}
		s.applyEventLocked(clear, false)
	}
	return nil
}

func (s *ServiceStatusService) rememberCurrentEventLocked(evt db.ServiceNoticeEvent) {
	switch evt.Slot {
	case db.ServiceNoticeSlotBroadcast:
		s.broadcastEventID = evt.ID
	case db.ServiceNoticeSlotMaintenance:
		s.maintenanceEventID = evt.ID
	}
}

func (s *ServiceStatusService) currentEventIDLocked(slot string) int64 {
	if slot == db.ServiceNoticeSlotBroadcast {
		return s.broadcastEventID
	}
	if slot == db.ServiceNoticeSlotMaintenance {
		return s.maintenanceEventID
	}
	return 0
}

// scheduleExpiryLocked turns wall-clock expiry into an append-only state
// transition. The expected event ID prevents an old timer from clearing a
// newer notice published into the same slot.
func (s *ServiceStatusService) scheduleExpiryLocked(evt db.ServiceNoticeEvent) {
	if !evt.Active || evt.ExpiresAt == nil {
		return
	}
	delay := time.Until(*evt.ExpiresAt)
	if delay < 0 {
		delay = 0
	}
	go func(slot string, expectedID int64, targetCommit string, wait time.Duration) {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		<-timer.C
		s.expireIfCurrent(slot, expectedID, targetCommit)
	}(evt.Slot, evt.ID, evt.TargetCommit, delay)
}

func (s *ServiceStatusService) expireIfCurrent(slot string, expectedID int64, targetCommit string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentEventIDLocked(slot) != expectedID {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clear, err := s.store.Append(ctx, db.ServiceNoticeEvent{
		Slot:         slot,
		Active:       false,
		Severity:     "info",
		TargetCommit: targetCommit,
		Source:       "expired",
	})
	if err != nil {
		// GetServiceStatus already hides the expired value. Broadcast that
		// semantic clear now, then retry persistence so a restart cannot revive it.
		slog.Warn("failed to persist expired service notice", "slot", slot, "event_id", expectedID, "error", err)
		s.engine.BroadcastServiceStatus()
		go func() {
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			<-timer.C
			s.expireIfCurrent(slot, expectedID, targetCommit)
		}()
		return
	}
	status := s.applyEventLocked(clear, true)
	s.emitChanged(status, slot, "expired")
}

func statusFromCurrentEvents(events []db.ServiceNoticeEvent, now time.Time) game.ServiceStatus {
	status := game.ServiceStatus{Type: "service_status", ServerTime: now}
	for _, evt := range events {
		if evt.ID > status.Revision {
			status.Revision = evt.ID
		}
		if !evt.Active || (evt.ExpiresAt != nil && !evt.ExpiresAt.After(now)) {
			continue
		}
		notice := noticeFromEvent(evt)
		switch evt.Slot {
		case db.ServiceNoticeSlotBroadcast:
			status.Broadcast = notice
		case db.ServiceNoticeSlotMaintenance:
			status.Maintenance = notice
		}
	}
	return status
}

func noticeFromEvent(evt db.ServiceNoticeEvent) *game.ServiceNotice {
	return &game.ServiceNotice{
		ID:                       evt.ID,
		Severity:                 evt.Severity,
		Message:                  evt.Message,
		Phase:                    evt.Phase,
		TargetCommit:             evt.TargetCommit,
		EstimatedDowntimeSeconds: evt.EstimatedDowntimeSeconds,
		RetryAfterSeconds:        evt.RetryAfterSeconds,
		ExpiresAt:                evt.ExpiresAt,
		Source:                   evt.Source,
		PublishedAt:              evt.CreatedAt,
	}
}

func (s *ServiceStatusService) applyEventLocked(evt db.ServiceNoticeEvent, broadcast bool) game.ServiceStatus {
	s.rememberCurrentEventLocked(evt)
	status := s.engine.GetServiceStatus()
	if evt.ID > status.Revision {
		status.Revision = evt.ID
	}
	now := time.Now().UTC()
	var notice *game.ServiceNotice
	if evt.Active && (evt.ExpiresAt == nil || evt.ExpiresAt.After(now)) {
		notice = noticeFromEvent(evt)
	}
	switch evt.Slot {
	case db.ServiceNoticeSlotBroadcast:
		status.Broadcast = notice
	case db.ServiceNoticeSlotMaintenance:
		status.Maintenance = notice
	}
	if broadcast {
		s.engine.SetServiceStatus(status)
	} else {
		s.engine.RestoreServiceStatus(status)
	}
	return s.engine.GetServiceStatus()
}

func (s *ServiceStatusService) emitChanged(status game.ServiceStatus, slot, action string) {
	EmitGameEvent(s.bus, "service_status_changed", map[string]interface{}{
		"revision": status.Revision,
		"slot":     slot,
		"action":   action,
	})
}

func (s *ServiceStatusService) PublishBroadcast(ctx context.Context, message, severity string, expiresAt *time.Time) (game.ServiceStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	evt, err := s.store.Append(ctx, db.ServiceNoticeEvent{
		Slot:      db.ServiceNoticeSlotBroadcast,
		Active:    true,
		Severity:  severity,
		Message:   message,
		ExpiresAt: expiresAt,
		Source:    "admin",
	})
	if err != nil {
		return game.ServiceStatus{}, err
	}
	status := s.applyEventLocked(evt, true)
	s.scheduleExpiryLocked(evt)
	s.emitChanged(status, evt.Slot, "published")
	return status, nil
}

func (s *ServiceStatusService) ClearBroadcast(ctx context.Context, expectedID int64) (game.ServiceStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.engine.GetServiceStatus().Broadcast
	if current == nil {
		return game.ServiceStatus{}, errNoCurrentBroadcast
	}
	if current.ID != expectedID {
		return game.ServiceStatus{}, errStaleBroadcast
	}
	evt, err := s.store.Append(ctx, db.ServiceNoticeEvent{
		Slot:     db.ServiceNoticeSlotBroadcast,
		Active:   false,
		Severity: "info",
		Source:   "admin",
	})
	if err != nil {
		return game.ServiceStatus{}, err
	}
	status := s.applyEventLocked(evt, true)
	s.emitChanged(status, evt.Slot, "cleared")
	return status, nil
}

func (s *ServiceStatusService) SetMaintenance(ctx context.Context, targetCommit, phase, message, source string) (game.ServiceStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	expires := time.Now().UTC().Add(maintenanceFallbackTTL)
	evt, err := s.store.Append(ctx, db.ServiceNoticeEvent{
		Slot:                     db.ServiceNoticeSlotMaintenance,
		Active:                   true,
		Severity:                 "warning",
		Message:                  message,
		Phase:                    phase,
		TargetCommit:             targetCommit,
		EstimatedDowntimeSeconds: maintenanceRetrySeconds,
		RetryAfterSeconds:        maintenanceRetrySeconds,
		ExpiresAt:                &expires,
		Source:                   source,
	})
	if err != nil {
		return game.ServiceStatus{}, err
	}
	status := s.applyEventLocked(evt, true)
	s.scheduleExpiryLocked(evt)
	s.emitChanged(status, evt.Slot, phase)
	return status, nil
}

// SetManualRestart durably announces an operator-initiated restart. The next
// process clears this source on startup, even when it runs the same commit.
func (s *ServiceStatusService) SetManualRestart(ctx context.Context) (game.ServiceStatus, error) {
	return s.SetMaintenance(
		ctx,
		version.ResolvedCommit(),
		"restarting",
		"Arena is restarting. Connections will return automatically.",
		"admin-restart",
	)
}

func (s *ServiceStatusService) UpdateMaintenancePhase(ctx context.Context, targetCommit, phase, message string) (game.ServiceStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.engine.GetServiceStatus().Maintenance
	if current == nil || current.TargetCommit != targetCommit {
		return game.ServiceStatus{}, errStaleMaintenance
	}
	expires := time.Now().UTC().Add(maintenanceFallbackTTL)
	evt, err := s.store.Append(ctx, db.ServiceNoticeEvent{
		Slot:                     db.ServiceNoticeSlotMaintenance,
		Active:                   true,
		Severity:                 current.Severity,
		Message:                  message,
		Phase:                    phase,
		TargetCommit:             targetCommit,
		EstimatedDowntimeSeconds: maintenanceRetrySeconds,
		RetryAfterSeconds:        maintenanceRetrySeconds,
		ExpiresAt:                &expires,
		Source:                   "updater",
	})
	if err != nil {
		return game.ServiceStatus{}, err
	}
	status := s.applyEventLocked(evt, true)
	s.scheduleExpiryLocked(evt)
	s.emitChanged(status, evt.Slot, phase)
	return status, nil
}

func (s *ServiceStatusService) ClearMaintenance(ctx context.Context, targetCommit, source string) (game.ServiceStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.engine.GetServiceStatus().Maintenance
	if current == nil {
		return s.engine.GetServiceStatus(), nil
	}
	if current.TargetCommit != targetCommit {
		return game.ServiceStatus{}, errStaleMaintenance
	}
	evt, err := s.store.Append(ctx, db.ServiceNoticeEvent{
		Slot:         db.ServiceNoticeSlotMaintenance,
		Active:       false,
		Severity:     "info",
		TargetCommit: targetCommit,
		Source:       source,
	})
	if err != nil {
		return game.ServiceStatus{}, err
	}
	status := s.applyEventLocked(evt, true)
	s.emitChanged(status, evt.Slot, "cleared")
	return status, nil
}

func (s *ServiceStatusService) publicStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.engine.GetServiceStatus())
}

func validateBroadcastInput(message, severity string, expiresInSeconds *int) (string, string, *time.Time, error) {
	message = strings.TrimSpace(message)
	severity = strings.ToLower(strings.TrimSpace(severity))
	if severity == "" {
		severity = "info"
	}
	if message == "" {
		return "", "", nil, errors.New("message is required")
	}
	if !utf8.ValidString(message) || utf8.RuneCountInString(message) > maxBroadcastRunes {
		return "", "", nil, fmt.Errorf("message must be at most %d characters", maxBroadcastRunes)
	}
	for _, r := range message {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return "", "", nil, errors.New("message contains unsupported control characters")
		}
	}
	if severity != "info" && severity != "warning" && severity != "critical" {
		return "", "", nil, errors.New("severity must be info, warning, or critical")
	}
	var expiresAt *time.Time
	if expiresInSeconds != nil {
		duration := time.Duration(*expiresInSeconds) * time.Second
		if duration < minBroadcastExpiry || duration > maxBroadcastExpiry {
			return "", "", nil, errors.New("expiry must be between 60 and 604800 seconds")
		}
		expires := time.Now().UTC().Add(duration)
		expiresAt = &expires
	}
	return message, severity, expiresAt, nil
}

func (h *AdminHandler) listBroadcasts(w http.ResponseWriter, r *http.Request) {
	if h.ServiceStatus == nil {
		writeError(w, http.StatusServiceUnavailable, "service status unavailable")
		return
	}
	events, err := h.ServiceStatus.store.List(r.Context(), 50)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "broadcast history unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"current": h.Engine.GetServiceStatus(),
		"events":  events,
	})
}

func (h *AdminHandler) createBroadcast(w http.ResponseWriter, r *http.Request) {
	if h.ServiceStatus == nil {
		writeError(w, http.StatusServiceUnavailable, "service status unavailable")
		return
	}
	var req struct {
		Message          string `json:"message"`
		Severity         string `json:"severity"`
		ExpiresInSeconds *int   `json:"expires_in_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	message, severity, expiresAt, err := validateBroadcastInput(req.Message, req.Severity, req.ExpiresInSeconds)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, err := h.ServiceStatus.PublishBroadcast(r.Context(), message, severity, expiresAt)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to persist broadcast")
		return
	}
	writeJSON(w, http.StatusCreated, status)
}

func (h *AdminHandler) clearBroadcast(w http.ResponseWriter, r *http.Request) {
	if h.ServiceStatus == nil {
		writeError(w, http.StatusServiceUnavailable, "service status unavailable")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid broadcast id")
		return
	}
	status, err := h.ServiceStatus.ClearBroadcast(r.Context(), id)
	switch {
	case errors.Is(err, errNoCurrentBroadcast):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errStaleBroadcast):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "failed to clear broadcast")
	default:
		writeJSON(w, http.StatusOK, status)
	}
}

func updaterBearerValid(provided, secret string) bool {
	if secret == "" {
		return false
	}
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte("Bearer " + secret))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

// updaterStatusCallback accepts only narrowly validated lifecycle callbacks
// from the internal updater sidecar. The route is not an admin/browser API.
func (s *ServiceStatusService) updaterStatusCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if config.C.UpdaterSharedSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "updater callback is not configured")
		return
	}
	if !updaterBearerValid(r.Header.Get("Authorization"), config.C.UpdaterSharedSecret) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Phase        string `json:"phase"`
		TargetCommit string `json:"target_commit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Phase = strings.ToLower(strings.TrimSpace(req.Phase))
	req.TargetCommit = strings.TrimSpace(req.TargetCommit)
	if !fullSHARe.MatchString(req.TargetCommit) {
		writeError(w, http.StatusBadRequest, "target_commit must be a full lowercase commit SHA")
		return
	}

	var (
		status game.ServiceStatus
		err    error
	)
	switch req.Phase {
	case "restarting":
		status, err = s.UpdateMaintenancePhase(r.Context(), req.TargetCommit, "restarting", "Arena is restarting. Connections will return automatically.")
	case "done", "failed":
		status, err = s.ClearMaintenance(r.Context(), req.TargetCommit, "updater-"+req.Phase)
	default:
		writeError(w, http.StatusBadRequest, "phase must be restarting, done, or failed")
		return
	}
	if errors.Is(err, errStaleMaintenance) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to persist updater status")
		return
	}
	writeJSON(w, http.StatusOK, status)
}
