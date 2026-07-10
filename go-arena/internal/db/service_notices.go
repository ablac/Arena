package db

import (
	"context"
	"fmt"
	"time"
)

const (
	ServiceNoticeSlotBroadcast   = "broadcast"
	ServiceNoticeSlotMaintenance = "maintenance"
)

// ServiceNoticeEvent is one append-only state transition for either the
// operator broadcast or scheduled-maintenance slot. A clear is represented by
// an inactive event, so an expired or cleared message can never reveal an
// older message underneath it.
type ServiceNoticeEvent struct {
	ID                       int64      `json:"id"`
	Slot                     string     `json:"slot"`
	Active                   bool       `json:"active"`
	Severity                 string     `json:"severity"`
	Message                  string     `json:"message"`
	Phase                    string     `json:"phase,omitempty"`
	TargetCommit             string     `json:"target_commit,omitempty"`
	EstimatedDowntimeSeconds int        `json:"estimated_downtime_seconds,omitempty"`
	RetryAfterSeconds        int        `json:"retry_after_seconds,omitempty"`
	ExpiresAt                *time.Time `json:"expires_at,omitempty"`
	Source                   string     `json:"source"`
	CreatedAt                time.Time  `json:"created_at"`
}

// EnsureServiceNoticeEventsTable creates the durable event stream used by the
// public status snapshot and admin broadcast controls.
func EnsureServiceNoticeEventsTable(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS service_notice_events (
			id BIGSERIAL PRIMARY KEY,
			slot TEXT NOT NULL CHECK (slot IN ('broadcast', 'maintenance')),
			active BOOLEAN NOT NULL,
			severity TEXT NOT NULL DEFAULT 'info' CHECK (severity IN ('info', 'warning', 'critical')),
			message TEXT NOT NULL DEFAULT '',
			phase TEXT NOT NULL DEFAULT '',
			target_commit TEXT NOT NULL DEFAULT '',
			estimated_downtime_seconds INT NOT NULL DEFAULT 0 CHECK (estimated_downtime_seconds >= 0 AND estimated_downtime_seconds <= 3600),
			retry_after_seconds INT NOT NULL DEFAULT 0 CHECK (retry_after_seconds >= 0 AND retry_after_seconds <= 3600),
			expires_at TIMESTAMPTZ,
			source TEXT NOT NULL DEFAULT 'admin',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_service_notice_events_slot_id
			ON service_notice_events (slot, id DESC);
		CREATE INDEX IF NOT EXISTS idx_service_notice_events_created_at
			ON service_notice_events (created_at DESC);
	`)
	if err != nil {
		return fmt.Errorf("ensure service_notice_events: %w", err)
	}
	return nil
}

// AppendServiceNoticeEvent records a publish, phase change, or clear and
// returns the database-assigned revision and timestamp.
func AppendServiceNoticeEvent(ctx context.Context, evt ServiceNoticeEvent) (ServiceNoticeEvent, error) {
	if Pool == nil {
		return ServiceNoticeEvent{}, ErrNoDatabase
	}
	err := Pool.QueryRow(ctx, `
		INSERT INTO service_notice_events (
			slot, active, severity, message, phase, target_commit,
			estimated_downtime_seconds, retry_after_seconds, expires_at, source
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, created_at`,
		evt.Slot, evt.Active, evt.Severity, evt.Message, evt.Phase,
		evt.TargetCommit, evt.EstimatedDowntimeSeconds,
		evt.RetryAfterSeconds, evt.ExpiresAt, evt.Source,
	).Scan(&evt.ID, &evt.CreatedAt)
	if err != nil {
		return ServiceNoticeEvent{}, fmt.Errorf("append service notice event: %w", err)
	}
	return evt, nil
}

// CurrentServiceNoticeEvents returns the newest event in each slot. Expiry is
// deliberately evaluated by the service after this query; filtering expired
// rows here would resurrect the previous event for the slot.
func CurrentServiceNoticeEvents(ctx context.Context) ([]ServiceNoticeEvent, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `
		SELECT DISTINCT ON (slot)
			id, slot, active, severity, message, phase, target_commit,
			estimated_downtime_seconds, retry_after_seconds, expires_at,
			source, created_at
		FROM service_notice_events
		ORDER BY slot, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query current service notices: %w", err)
	}
	defer rows.Close()

	events := make([]ServiceNoticeEvent, 0, 2)
	for rows.Next() {
		var evt ServiceNoticeEvent
		if err := rows.Scan(
			&evt.ID, &evt.Slot, &evt.Active, &evt.Severity, &evt.Message,
			&evt.Phase, &evt.TargetCommit, &evt.EstimatedDowntimeSeconds,
			&evt.RetryAfterSeconds, &evt.ExpiresAt, &evt.Source,
			&evt.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan current service notice: %w", err)
		}
		events = append(events, evt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current service notices: %w", err)
	}
	return events, nil
}

// ListServiceNoticeEvents returns the newest events for admin audit history.
func ListServiceNoticeEvents(ctx context.Context, limit int) ([]ServiceNoticeEvent, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := Pool.Query(ctx, `
		SELECT id, slot, active, severity, message, phase, target_commit,
			estimated_downtime_seconds, retry_after_seconds, expires_at,
			source, created_at
		FROM service_notice_events
		ORDER BY id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list service notice events: %w", err)
	}
	defer rows.Close()

	events := make([]ServiceNoticeEvent, 0, limit)
	for rows.Next() {
		var evt ServiceNoticeEvent
		if err := rows.Scan(
			&evt.ID, &evt.Slot, &evt.Active, &evt.Severity, &evt.Message,
			&evt.Phase, &evt.TargetCommit, &evt.EstimatedDowntimeSeconds,
			&evt.RetryAfterSeconds, &evt.ExpiresAt, &evt.Source,
			&evt.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan service notice history: %w", err)
		}
		events = append(events, evt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service notice history: %w", err)
	}
	return events, nil
}
