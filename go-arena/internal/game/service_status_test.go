package game

import (
	"encoding/json"
	"testing"
	"time"
)

func TestServiceStatusBroadcastReachesBotsAndSpectators(t *testing.T) {
	engine := NewGameEngine()
	active := &BotState{BotID: "active", SendChan: make(chan []byte, 2)}
	waiting := &BotState{BotID: "waiting", SendChan: make(chan []byte, 2)}
	spectator := &SpectatorConn{SendChan: make(chan []byte, 2), Done: make(chan struct{})}
	engine.Bots[active.BotID] = active
	engine.WaitingBots[waiting.BotID] = waiting
	engine.Spectators = append(engine.Spectators, spectator)

	engine.SetServiceStatus(ServiceStatus{
		Revision:  4,
		Broadcast: &ServiceNotice{ID: 4, Severity: "info", Message: "hello", PublishedAt: time.Now()},
	})

	for name, ch := range map[string]chan []byte{
		"active bot":  active.SendChan,
		"waiting bot": waiting.SendChan,
		"spectator":   spectator.SendChan,
	} {
		select {
		case data := <-ch:
			var status ServiceStatus
			if err := json.Unmarshal(data, &status); err != nil {
				t.Fatalf("%s decode: %v", name, err)
			}
			if status.Type != "service_status" || status.Revision != 4 || status.Broadcast == nil || status.Broadcast.Message != "hello" {
				t.Fatalf("%s status = %#v", name, status)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not receive service status", name)
		}
	}
}

func TestGetServiceStatusHidesExpiredNoticeWithoutLoweringRevision(t *testing.T) {
	engine := NewGameEngine()
	expired := time.Now().Add(-time.Second)
	engine.RestoreServiceStatus(ServiceStatus{
		Revision:  9,
		Broadcast: &ServiceNotice{ID: 9, Message: "old", ExpiresAt: &expired},
	})
	status := engine.GetServiceStatus()
	if status.Revision != 9 {
		t.Fatalf("revision = %d, want 9", status.Revision)
	}
	if status.Broadcast != nil {
		t.Fatalf("expired broadcast remained visible: %#v", status.Broadcast)
	}
}

func TestNotifyServiceRestartBroadcastsSemanticChangeAtCurrentRevision(t *testing.T) {
	engine := NewGameEngine()
	bot := &BotState{BotID: "shutdown-bot", SendChan: make(chan []byte, 1)}
	engine.Bots[bot.BotID] = bot
	engine.RestoreServiceStatus(ServiceStatus{Revision: 9})

	engine.NotifyServiceRestart(45)
	status := engine.GetServiceStatus()
	if status.Revision != 9 || status.Maintenance == nil {
		t.Fatalf("shutdown status = %#v", status)
	}
	if status.Maintenance.Phase != "restarting" || status.Maintenance.RetryAfterSeconds != 45 {
		t.Fatalf("shutdown maintenance = %#v", status.Maintenance)
	}
	select {
	case payload := <-bot.SendChan:
		var delivered ServiceStatus
		if err := json.Unmarshal(payload, &delivered); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if delivered.Revision != 9 || delivered.Maintenance == nil {
			t.Fatalf("delivered shutdown status = %#v", delivered)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown status was not delivered to bot")
	}
}
