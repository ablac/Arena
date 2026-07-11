package ws

import (
	"encoding/json"
	"testing"
	"time"

	"arena-server/internal/game"
)

func TestActionTickDuplicateAndStaleRejectionsAreNonPunitive(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{name: "duplicate", err: game.ErrActionTickDuplicate, code: "DUPLICATE_ACTION_TICK"},
		{name: "server_tick_used", err: game.ErrActionServerTickUsed, code: "SERVER_TICK_ACTION_LOCKED"},
		{name: "stale", err: game.ErrActionTickStale, code: "STALE_ACTION_TICK"},
		{name: "target_not_visible", err: game.ErrActionTargetNotVisible, code: "TARGET_NOT_VISIBLE"},
		{name: "round_not_active", err: game.ErrActionRoundNotActive, code: "ROUND_NOT_ACTIVE"},
		{name: "bot_not_alive", err: game.ErrActionBotNotAlive, code: "BOT_NOT_ALIVE"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tracker := installActionStrikeTestTracker(t)
			bot := &game.BotState{
				APIKeyID: "key-" + tc.name,
				SendChan: make(chan []byte, 4),
			}

			for attempt := 0; attempt < 3; attempt++ {
				if locked := sendActionSubmissionError(nil, bot, 42, tc.err); locked {
					t.Fatalf("%s rejection locked the API key on attempt %d", tc.name, attempt+1)
				}
				message := readActionStrikeTestError(t, bot.SendChan)
				if message.Code != tc.code {
					t.Fatalf("%s rejection code = %q, want %q", tc.name, message.Code, tc.code)
				}
				if _, present := message.Details["strikes"]; present {
					t.Fatalf("%s rejection exposed punitive strike details: %#v", tc.name, message.Details)
				}
			}

			if entry := tracker.entries[bot.APIKeyID]; entry != nil {
				t.Fatalf("%s rejection created a violation entry: %+v", tc.name, entry)
			}
			if remaining, locked := tracker.IsLocked(bot.APIKeyID); locked || remaining != 0 {
				t.Fatalf("%s rejection lock state = remaining %v locked %v", tc.name, remaining, locked)
			}
		})
	}
}

func TestActionTickFutureRejectionStillStrikesAndLocks(t *testing.T) {
	tracker := installActionStrikeTestTracker(t)
	bot := &game.BotState{
		APIKeyID: "key-future",
		SendChan: make(chan []byte, 4),
	}

	for attempt := 1; attempt <= 3; attempt++ {
		locked := sendActionSubmissionError(nil, bot, 999, game.ErrActionTickFuture)
		message := readActionStrikeTestError(t, bot.SendChan)
		if attempt < 3 {
			if locked {
				t.Fatalf("future rejection locked on attempt %d", attempt)
			}
			if message.Code != "FUTURE_ACTION_TICK" {
				t.Fatalf("future rejection code on attempt %d = %q", attempt, message.Code)
			}
			if got := int(message.Details["strikes"].(float64)); got != attempt {
				t.Fatalf("future rejection strikes on attempt %d = %d", attempt, got)
			}
			continue
		}

		if !locked {
			t.Fatal("third future rejection did not lock the API key")
		}
		if message.Code != "API_KEY_TEMP_LOCKED" {
			t.Fatalf("third future rejection code = %q, want API_KEY_TEMP_LOCKED", message.Code)
		}
	}

	if remaining, locked := tracker.IsLocked(bot.APIKeyID); !locked || remaining != 30*time.Second {
		t.Fatalf("future rejection lock state = remaining %v locked %v", remaining, locked)
	}
}

func TestProtocolLockIsRecheckedAtAdmissionTime(t *testing.T) {
	tracker := installActionStrikeTestTracker(t)
	keyID := "key-admission-race"
	if retry, locked := temporaryProtocolLockRetrySeconds(keyID); locked || retry != 0 {
		t.Fatalf("initial lock state = retry %d locked %v", retry, locked)
	}
	for i := 0; i < 3; i++ {
		tracker.Record(keyID)
	}
	if retry, locked := temporaryProtocolLockRetrySeconds(keyID); !locked || retry != 30 {
		t.Fatalf("admission-time lock state = retry %d locked %v, want 30/true", retry, locked)
	}
}

type actionStrikeTestError struct {
	Code    string                 `json:"code"`
	Details map[string]interface{} `json:"details"`
}

func installActionStrikeTestTracker(t *testing.T) *violationTracker {
	t.Helper()
	previous := botViolationTracker
	tracker := newViolationTracker(3, time.Minute, 30*time.Second)
	tracker.now = func() time.Time { return time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC) }
	botViolationTracker = tracker
	t.Cleanup(func() { botViolationTracker = previous })
	return tracker
}

func readActionStrikeTestError(t *testing.T, messages <-chan []byte) actionStrikeTestError {
	t.Helper()
	data := <-messages
	var message actionStrikeTestError
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatalf("decode structured error: %v", err)
	}
	return message
}
