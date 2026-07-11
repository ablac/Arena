package game

import (
	"errors"
	"fmt"
	"sort"

	"arena-server/internal/config"
)

// Bot taunts: cosmetic emotes a bot can emit over its /ws/bot connection,
// rendered as speech bubbles in the spectator view. Taunts ride the
// ArenaEvent transient channel, which is spectator-only and sits behind the
// 5s anti-radar delay, so a taunt can never act as a real-time signal
// between bots. Free text is not accepted anywhere on this path: the emote
// key must be in tauntEmotes, and the display text always comes from this
// server-side table.

// tauntEmotes maps the wire emote key to the display text spectators see.
var tauntEmotes = map[string]string{
	"gg":        "GG!",
	"glhf":      "GLHF!",
	"nice":      "Nice one!",
	"ouch":      "Ouch!",
	"oops":      "Oops...",
	"bring_it":  "Bring it!",
	"too_easy":  "Too easy.",
	"help":      "A little help here?!",
	"revenge":   "That was personal.",
	"sneaky":    "Sneaky sneaky.",
	"heartbeat": "Still alive!",
	"salute":    "o7",
}

// TauntEmoteKeys returns the sorted list of valid emote keys, for the
// public bot-setup documentation.
func TauntEmoteKeys() []string {
	keys := make([]string, 0, len(tauntEmotes))
	for k := range tauntEmotes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TauntText returns the display text for an emote key.
func TauntText(emote string) (string, bool) {
	text, ok := tauntEmotes[emote]
	return text, ok
}

var (
	// ErrTauntInvalidEmote is the only taunt error surfaced to the bot (a
	// typo deserves feedback). Every other rejection is a silent drop:
	// taunts are cosmetic, and a strike or error loop over a cooldown would
	// punish harmless enthusiasm.
	ErrTauntInvalidEmote = errors.New("unknown taunt emote")

	errTauntDropped = errors.New("taunt dropped")
)

// AddTauntForSession validates and buffers a taunt from a bot's reader
// goroutine. On success the taunt becomes an ArenaEvent on the next
// spectator broadcast. Rejections other than an invalid emote return
// errTauntDropped, which callers treat as a silent no-op.
func (e *GameEngine) AddTauntForSession(botID string, expected *BotState, emote string) error {
	text, ok := tauntEmotes[emote]
	if !ok {
		return ErrTauntInvalidEmote
	}
	if !config.C.TauntsEnabled {
		return errTauntDropped
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	bot, ok := e.Bots[botID]
	if !ok {
		return errTauntDropped
	}
	// A replaced socket may still have a read in flight; a stale session
	// must not taunt through the new one (same guard as action submission).
	if expected != nil && bot != expected {
		return errTauntDropped
	}
	if e.Round.Phase != PhaseActive || !bot.IsAlive {
		return errTauntDropped
	}

	cooldownTicks := int(config.C.TauntCooldownSecs * float64(config.C.TickRate))
	if cooldownTicks < 1 {
		cooldownTicks = 1
	}
	if bot.LastTauntTick > 0 && e.TickCount-bot.LastTauntTick < cooldownTicks {
		return errTauntDropped
	}
	bot.LastTauntTick = e.TickCount

	e.appendArenaEvents(ArenaEvent{
		ID:       fmt.Sprintf("taunt:%s:%d", botID, e.TickCount),
		Type:     "taunt",
		Tick:     e.TickCount,
		Position: bot.Position,
		OwnerID:  botID,
		Emote:    emote,
		Text:     text,
	})
	return nil
}

// IsTauntDropped reports whether a taunt error is the silent-drop sentinel.
func IsTauntDropped(err error) bool {
	return errors.Is(err, errTauntDropped)
}
