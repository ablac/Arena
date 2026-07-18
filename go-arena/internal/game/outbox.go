package game

import (
	"bytes"
	"log/slog"
)

// tickOutbox collects the network payloads staged during the locked phase of
// a tick. Views are built under e.mu because they read live engine state;
// JSON marshaling and the non-blocking channel handoffs happen in
// flushTickOutbox after the lock is released, still on the single tick
// goroutine, so serialization cost no longer extends the engine lock hold.
//
// Staged entries hold *BotState pointers captured under the lock. The flush
// phase reads only fields that are never reassigned after a session is
// published to the engine (BotID, SendChan, TickChan, Conn) plus the lobby
// dedup markers, which are owned exclusively by the tick goroutine.
type tickOutbox struct {
	botTicks    []outboundBotTick
	spectator   *SpectatorState
	deathSends  []outboundDeathMessage
	killSends   []outboundKillMessage
	lobbyUpdate *outboundLobbyUpdate
	lobbyState  *outboundLobbyState
}

type outboundBotTick struct {
	bot *BotState
	msg *TickMessage
}

type outboundDeathMessage struct {
	bot   *BotState
	event DeathEvent
}

type outboundKillMessage struct {
	bot   *BotState
	event KillEvent
}

// outboundLobbyUpdate stages the lobby frame sent to connected bots during
// the lobby phase (every 2 ticks).
type outboundLobbyUpdate struct {
	connected  int
	minBots    int
	countdown  *int
	players    []map[string]interface{}
	recipients []*BotState
}

// outboundLobbyState stages the lobby_state spectator broadcast plus the
// waiting-room lobby frame served alongside it.
type outboundLobbyState struct {
	state            map[string]interface{}
	waitingConnected int
	waitingMinBots   int
	waitingPlayers   []map[string]interface{}
	waitingBots      []*BotState
}

// snapshotSpectators copies the current spectator list under its own lock so
// broadcasts can run without holding the engine lock.
func (e *GameEngine) snapshotSpectators() []*SpectatorConn {
	e.spectatorsMu.RLock()
	specs := make([]*SpectatorConn, len(e.Spectators))
	copy(specs, e.Spectators)
	e.spectatorsMu.RUnlock()
	return specs
}

// flushTickOutbox marshals and delivers everything staged by the locked tick
// phase. It must run on the tick goroutine, without e.mu held. Delivery order
// mirrors the pre-split behavior: bot ticks, spectator state, death/kill
// notifications, then lobby traffic.
func (e *GameEngine) flushTickOutbox() {
	out := &e.outbox

	// Per-bot tick snapshots (active rounds).
	for i := range out.botTicks {
		SendTickUpdate(out.botTicks[i].bot, out.botTicks[i].msg)
		out.botTicks[i] = outboundBotTick{}
	}
	out.botTicks = out.botTicks[:0]

	// Spectator arena state — one marshal + one PreparedMessage shared by
	// every spectator connection.
	if out.spectator != nil {
		if data, err := marshalJSON(out.spectator); err != nil {
			slog.Error("failed to marshal spectator state", "error", err)
		} else {
			BroadcastToSpectators(e.snapshotSpectators(), data)
		}
		out.spectator = nil
	}

	// Death/kill notifications.
	for i := range out.deathSends {
		SendDeathMessage(out.deathSends[i].bot, out.deathSends[i].event)
		out.deathSends[i] = outboundDeathMessage{}
	}
	out.deathSends = out.deathSends[:0]
	for i := range out.killSends {
		SendKillMessage(out.killSends[i].bot, out.killSends[i].event)
		out.killSends[i] = outboundKillMessage{}
	}
	out.killSends = out.killSends[:0]

	// Lobby frame for connected bots (lobby phase).
	if u := out.lobbyUpdate; u != nil {
		out.lobbyUpdate = nil
		payload, err := marshalLobbyUpdatePayload(u.connected, u.minBots, u.countdown, u.players)
		if err != nil {
			slog.Error("failed to marshal lobby update", "error", err)
		} else {
			// Re-send only when the frame changed for this bot+connection.
			// Content changes at most at 1 Hz (countdown) or on
			// join/leave/loadout, so with a full lobby this drops ~80-90% of
			// lobby-phase outbound bytes. Identity comparison is per-bot: a
			// fresh or resumed connection always gets the current frame.
			// e.lastLobbyPayload and the per-bot markers are touched only by
			// the tick goroutine, so no lock is needed here.
			if !bytes.Equal(payload, e.lastLobbyPayload) {
				e.lastLobbyPayload = payload
			}
			payload = e.lastLobbyPayload
			for _, bot := range u.recipients {
				if bot.lastLobbyConn == bot.Conn && sameByteSlice(bot.lastLobbyPayload, payload) {
					continue
				}
				sendLobbyPayload(bot, payload)
				bot.lastLobbyPayload = payload
				bot.lastLobbyConn = bot.Conn
			}
		}
	}

	// lobby_state broadcast for spectators, plus the waiting-room frame.
	if s := out.lobbyState; s != nil {
		out.lobbyState = nil
		if data, err := marshalJSON(s.state); err != nil {
			slog.Error("failed to marshal lobby state", "error", err)
		} else {
			BroadcastToSpectators(e.snapshotSpectators(), data)
		}
		if len(s.waitingBots) > 0 {
			payload, err := marshalLobbyUpdatePayload(s.waitingConnected, s.waitingMinBots, nil, s.waitingPlayers)
			if err != nil {
				slog.Error("failed to marshal waiting-room lobby update", "error", err)
				return
			}
			for _, bot := range s.waitingBots {
				sendLobbyPayload(bot, payload)
			}
		}
	}
}
