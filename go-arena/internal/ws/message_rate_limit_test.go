package ws

import (
	"testing"
	"time"
)

func TestBotMessageLimiterAllowsTransientBacklogWithoutPunishment(t *testing.T) {
	limiter := newBotMessageLimiter(25)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	allowed := 0
	denied := 0
	notices := 0
	punishments := 0
	for i := 0; i < 30; i++ {
		decision := limiter.Check(now)
		if decision.Allowed {
			allowed++
		} else {
			denied++
		}
		if decision.Notify {
			notices++
		}
		if decision.Punish {
			punishments++
		}
	}

	if allowed != 25 || denied != 5 {
		t.Fatalf("transient burst allowed=%d denied=%d, want 25/5", allowed, denied)
	}
	if notices != 1 || punishments != 0 {
		t.Fatalf("transient burst notices=%d punishments=%d, want 1/0", notices, punishments)
	}
}

func TestBotMessageLimiterEscalatesSustainedFlood(t *testing.T) {
	limiter := newBotMessageLimiter(25)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	punishments := 0
	for i := 0; i < 175; i++ {
		if limiter.Check(now).Punish {
			punishments++
		}
	}
	if punishments != 3 {
		t.Fatalf("sustained flood punishments=%d, want 3 thresholds", punishments)
	}
}

func TestBotMessageLimiterResetsAfterQuietWindow(t *testing.T) {
	limiter := newBotMessageLimiter(2)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	limiter.Check(now)
	limiter.Check(now)
	if decision := limiter.Check(now); decision.Allowed || !decision.Notify {
		t.Fatalf("first overflow decision=%+v, want one notice", decision)
	}

	decision := limiter.Check(now.Add(time.Second + time.Millisecond))
	if !decision.Allowed || decision.Notify || decision.Punish {
		t.Fatalf("post-quiet decision=%+v, want fresh allowed window", decision)
	}
}
