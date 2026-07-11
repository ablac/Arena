package ws

import "time"

type botMessageRateDecision struct {
	Allowed      bool
	Notify       bool
	Punish       bool
	CurrentCount int
	DroppedCount int
}

type botMessageLimiter struct {
	maxPerSecond int
	timestamps   []time.Time
	incident     bool
	dropped      int
}

func newBotMessageLimiter(maxPerSecond int) *botMessageLimiter {
	return &botMessageLimiter{maxPerSecond: maxPerSecond}
}

// Check permits a bounded one-second message rate, reports the first overflow
// to the client without a protocol strike, and escalates only sustained floods.
// The server already accepts at most one action per authoritative tick, so a
// short client backlog cannot create a gameplay advantage and should not lock
// an otherwise healthy key.
func (l *botMessageLimiter) Check(now time.Time) botMessageRateDecision {
	if l.maxPerSecond <= 0 {
		return botMessageRateDecision{Allowed: true}
	}

	cutoff := now.Add(-time.Second)
	kept := l.timestamps[:0]
	for _, timestamp := range l.timestamps {
		if timestamp.After(cutoff) {
			kept = append(kept, timestamp)
		}
	}
	l.timestamps = kept
	if len(l.timestamps) < l.maxPerSecond && l.incident {
		l.incident = false
		l.dropped = 0
	}

	if len(l.timestamps) < l.maxPerSecond {
		l.timestamps = append(l.timestamps, now)
		return botMessageRateDecision{
			Allowed:      true,
			CurrentCount: len(l.timestamps),
		}
	}

	l.dropped++
	notify := !l.incident
	l.incident = true
	punishEvery := l.maxPerSecond * 2
	return botMessageRateDecision{
		Notify:       notify,
		Punish:       punishEvery > 0 && l.dropped%punishEvery == 0,
		CurrentCount: len(l.timestamps),
		DroppedCount: l.dropped,
	}
}
