# Bug: Delayed spectator combat renders after round end

> Status: FIXED
> Mode: default
> Severity: functional
> Author: Keith
> Last updated: 2026-07-18

## Symptom

After a round ends and bots despawn, combat flashes, damage or death effects, and occasional landmine-like explosions can still render around invisible entities.

## Expected

The final arena frame must be presented before `round_end`, and a same-round frame received after transition ownership begins must not create any gameplay presentation.

## Reproduction

- Commands: `go test ./internal/ws -run '^TestSpectatorWriterPreservesGameplayStateOrderAcrossRoundEnd$' -count=1` and `node scripts/test-round-transition-rendering.mjs`
- Test locations: `go-arena/internal/ws/spectator_test.go` and `scripts/test-round-transition-rendering.mjs`
- Reproduction stability: both tests failed 3/3 before the fix and failed again when only the production fix was temporarily removed.
- The server test observed `[round_end arena_state lobby_state]`; the client test observed stale combat-event and death/damage-effect presentation while transition ownership remained active.

## Hypotheses & diagnosis

| # | Hypothesis | Verdict | Evidence |
|---|---|---|---|
| H1 | The spectator writer bypasses the five-second gameplay delay for `round_end`, reordering it ahead of the final arena frame. | confirmed | The deterministic writer test received `round_end` before the delayed final `arena_state`, despite the tick outbox staging the arena state first. |
| H2 | The server continues simulating combat during intermission. | eliminated | `tickLocked` dispatches only `tickIntermission` after `endRound` changes the phase; combat, hazards, and landmines run only inside the final active tick. |
| H3 | Delaying `round_end` can lose it when high tick rates saturate the bounded delayed queue. | confirmed | With more than 128 delayed arena frames, the old drop-newest policy delivered zero `round_end` messages and discarded the newest/final arena tick. |
| H4 | The upstream 32-slot spectator handoff can drop the final state and `round_end` before the delayed writer sees them. | confirmed | Exercising `BroadcastToSpectators` at the real capacity delivered zero terminal messages under the old non-blocking drop-on-full policy. |

## Root cause

`isDelayedSpectatorState` classified `arena_state` and `lobby_state` as delayed gameplay but omitted the typed `round_end` message. The browser therefore began its intermission five seconds before the final arena frame arrived, and `ArenaEngine.setState` continued applying combat events and HP deltas from that late same-round frame even though bot presentation was already owned by the intermission director.

The initial ordering fix also exposed a bounded-queue edge case: the previous 128-entry drop-newest policy could fill during a five-second window at the supported 60 Hz maximum, dropping both the final snapshot and the newly delayed `round_end` lifecycle boundary.

The engine-to-writer handoff had the same defect earlier in the pipeline: its real 32-slot channel silently dropped every full-capacity broadcast without distinguishing replaceable snapshots from lifecycle boundaries.

## Fix

- Classify `round_end` as part of the ordered delayed spectator presentation stream.
- Size the bounded delayed queue for the supported 60 Hz window, reserve its terminal slot, and coalesce the oldest arena snapshot on overflow so the newest/final snapshot remains directly before `round_end` with its full release delay.
- Keep the real non-blocking 32-slot lobby/control handoff bounded, replace arena snapshots through a one-slot latest-state channel, and transfer the newest final state plus `round_end` as one replaceable lifecycle batch. The writer applies that batch in order before later lobby backlog.
- Return from `ArenaEngine.setState` when a same-round arena frame arrives while round-transition ownership is still active.
- Keep service-status messages and heartbeats immediate.

## Verification

- V-1: both focused regressions are green in 3 consecutive runs.
- V-2: temporarily removing only the production fix makes both regressions red; restoring it returns both to green.
- V-3: `go test ./...` is green.
- V-4: `go test -race ./internal/ws` is green.
- V-5: relevant JavaScript syntax and round-transition, intermission, renderer-suspension, rendering-safety, and spectator-client tests are green.
- V-6: `git diff --check` and `python .github/agent-contract/check.py` are green.
- V-7: the desktop Playwright smoke did not complete within the local 300-second SwiftShader cap; CI remains the browser authority.
- V-8: saturation regression failed consistently before the queue policy fix, then passed 3/3; focused ordering tests, `go test ./...`, and `go test -race ./internal/ws` remain green.
- V-9: real-capacity `BroadcastToSpectators` regression failed 3/3 before the handoff fix, passed 3/3 after it, and failed again when the old drop-on-full branch was temporarily restored; full Go and race tests for game and WebSocket packages are green.

## Regression test

- Path: `go-arena/internal/ws/spectator_test.go`
- Assertion: final arena state, `round_end`, and lobby state are released after the same delay and in that order.
- Assertion: an intentionally saturated delayed queue keeps service status immediate, remains bounded, retains exactly one delayed `round_end`, and places the newest arena tick immediately before it.
- Path: `go-arena/internal/game/spectator_broadcast_test.go`
- Assertion: the real 32-slot production control handoff stays bounded while a separate one-slot lifecycle batch retains the newest final arena tick directly before exactly one `round_end`, even after later lobby broadcasts saturate the control channel.
- Path: `scripts/test-round-transition-rendering.mjs`
- Assertion: a stale same-round arena frame cannot invoke combat, HP/death, pickup, gameplay, or camera presentation after `round_end`.

## Pattern analysis

| Search method | Hits | Same latent defect |
|---|---:|---|
| `rg -n "isDelayedSpectatorState|round_end" go-arena/internal/ws go-arena/internal/game/outbox.go` | 11 | One classifier controls the delayed stream; the outbox already documents the required final-state-before-round-end order. |
| `rg -n "_roundTransitionActive" frontend/js/renderer scripts/test-round-transition-rendering.mjs` | 17 | Normal interpolation and bounty animation already honor transition ownership; the missing `setState` presentation guard was the remaining stale-frame path. |

## Open questions / Follow-ups

- Re-run the existing Playwright spectator smoke in CI or on a host where Chromium is not stalled under SwiftShader.
