package db

import (
	"testing"
	"time"
)

func TestServiceNoticeEventsLastEventWinsWithoutExpiryResurrection(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureServiceNoticeEventsTable(ctx); err != nil {
		t.Fatalf("ensure table: %v", err)
	}
	if err := EnsureServiceNoticeEventsTable(ctx); err != nil {
		t.Fatalf("repeat ensure table: %v", err)
	}
	old, err := AppendServiceNoticeEvent(ctx, ServiceNoticeEvent{
		Slot: ServiceNoticeSlotBroadcast, Active: true, Severity: "info", Message: "old", Source: "test",
	})
	if err != nil {
		t.Fatalf("append old: %v", err)
	}
	expired := time.Now().Add(-time.Minute)
	newest, err := AppendServiceNoticeEvent(ctx, ServiceNoticeEvent{
		Slot: ServiceNoticeSlotBroadcast, Active: true, Severity: "warning", Message: "new", ExpiresAt: &expired, Source: "test",
	})
	if err != nil {
		t.Fatalf("append expired: %v", err)
	}
	current, err := CurrentServiceNoticeEvents(ctx)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if len(current) != 1 || current[0].ID != newest.ID || current[0].ID <= old.ID {
		t.Fatalf("current events = %#v", current)
	}

	clear, err := AppendServiceNoticeEvent(ctx, ServiceNoticeEvent{
		Slot: ServiceNoticeSlotBroadcast, Active: false, Severity: "info", Source: "test",
	})
	if err != nil {
		t.Fatalf("append clear: %v", err)
	}
	current, err = CurrentServiceNoticeEvents(ctx)
	if err != nil {
		t.Fatalf("current after clear: %v", err)
	}
	if len(current) != 1 || current[0].ID != clear.ID || current[0].Active {
		t.Fatalf("clear was not last-event-wins: %#v", current)
	}
}
