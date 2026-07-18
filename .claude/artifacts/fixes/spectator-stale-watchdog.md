# Bug: Browser stale watchdog disconnects healthy spectators

> Status: FIXED
> Mode: default
> Severity: functional
> Author: Keith
> Last updated: 2026-07-18

## Symptom

Spectators periodically disconnect and reconnect even while the Arena server remains reachable.

## Expected

A healthy spectator transport should remain connected. Server WebSocket ping/pong and application heartbeats should own liveness; a suspended browser timer must not close an otherwise healthy socket.

## Reproduction

- Command: `node scripts/test-spectator-client.mjs`
- Test location: `scripts/test-spectator-client.mjs`
- Reproduction stability: 3/3 reliably failed before the fix and failed again with the production fix stashed.
- The test simulates a background-tab resume where the stale timer runs before queued WebSocket message callbacks.

## Hypotheses & diagnosis

| # | Hypothesis | Verdict | Evidence |
|---|---|---|---|
| H1 | The browser-side 45-second message-silence timer closes a healthy socket after timer or main-thread suspension. | confirmed | The deterministic resume test invoked the pending watchdog and observed one client `close()` call. Live server logs showed clean client-initiated disconnects, including one roughly 55 seconds after admission. |
| H2 | The server heartbeat lane is failing and dropping spectators. | eliminated | A live 65-second probe received arena state about every 100 ms and heartbeats every 10 seconds, with no server close across the observed failure window. |
| H3 | Bot and spectator sockets share the same disconnect failure. | eliminated | Bot and spectator heartbeat implementations are separate. Live status showed 14 connected bots and recent server logs contained no bot disconnects while spectator sessions cycled. |

## Root cause

The spectator client used JavaScript callback silence as a transport-health signal. Browsers can suspend timers and message callbacks independently of the WebSocket transport; on resume, the stale timer can run before queued messages and close a connection that the server is actively pinging and feeding.

## Fix

- Removed the browser-side stale-message close timer from `frontend/js/spectator-ws.js`.
- Kept exponential reconnect behavior for real `close` events and retained the existing keepalive compatibility ping.
- Server ping/pong timeout and 10-second application heartbeats remain the authoritative liveness mechanism.

## Verification

- V-1: focused spectator-client regression test is green.
- V-2: stashing only the production fix makes the regression test red; restoring it returns green.
- V-3: `node --check frontend/js/spectator-ws.js` and `node scripts/test-spectator-client.mjs` are green.
- V-4: `cd go-arena && go test ./...`, `git diff --check`, and `python .github/agent-contract/check.py` are green.
- V-5: live read-only probe stayed connected for 65 seconds and observed heartbeats at 10-second intervals.

## Regression test

- Path: `scripts/test-spectator-client.mjs`
- Assertion: resuming long browser timers cannot trigger a client-driven close on a server-heartbeated spectator socket.

## Pattern analysis

| Search method | Hits | Same latent defect |
|---|---:|---|
| `rg -n "_staleTimer|_staleTimeout|No messages received|forcing reconnect|message-silence" frontend scripts` | 7 | One related implementation remains in `frontend/js/chat-panel.js`; it is a separate chat transport and was not changed in this spectator fix. |

## Open questions / Follow-ups

- Review the chat panel's equivalent stale watchdog separately if users report chat reconnect flapping.
