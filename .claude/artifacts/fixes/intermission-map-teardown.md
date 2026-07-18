# Bug: Old map remains visible after intermission teardown

> Status: FIXED
> Mode: default
> Severity: functional
> Author: Keith
> Last updated: 2026-07-18

## Symptom

The old map shrinks during the intermission teardown but remains faintly visible until the next round starts.

## Expected

The old map should be completely hidden when the two-second teardown finishes, while still allowing the renderer to restore it if the show is aborted before the real next-map handoff.

## Reproduction

- Command: `node scripts/test-intermission-director.mjs`
- Test location: `scripts/test-intermission-director.mjs`
- Reproduction stability: 3/3 reliably failed before the fix and failed again with the production fix stashed.

## Hypotheses & diagnosis

| # | Hypothesis | Verdict | Evidence |
|---|---|---|---|
| H1 | The teardown reaches its final scale but never disables the old meshes. | confirmed | At 2.55 seconds every teardown mesh had `scaling.y == 0.02` and `isEnabled() == true`. `_updateTeardown` only changed scale and position. |
| H2 | Stale intermission keyframes rebuild the old map after teardown. | eliminated | `holdsWorld()` remained true and the obstacle renderer received no update before construction. The same original meshes remained enabled. |

## Root cause

The teardown animation treated a 2% vertical scale and a small downward offset as equivalent to disappearance. Those meshes remained enabled until `fastForward()` restored them at the next-round handoff, so their residual geometry stayed visible for the rest of intermission.

## Fix

- Changed `frontend/js/renderer/intermission-director.js` to disable teardown meshes when progress reaches 100%.
- The existing restoration path now re-enables the meshes before a real rebuild or an aborted-show recovery.

## Verification

- V-1: focused regression test is green.
- V-2: stashing only the production fix makes the regression test red; restoring it returns green.
- V-3: `node --check frontend/js/renderer/intermission-director.js` and the full `scripts/test-intermission-director.mjs` suite are green.
- V-4: `cd go-arena && go test ./...`, `git diff --check`, and `python .github/agent-contract/check.py` are green.

## Regression test

- Path: `scripts/test-intermission-director.mjs`
- Assertion: completed teardown meshes are disabled, then re-enabled by the handoff restoration path.

## Pattern analysis

| Search method | Hits | Same latent defect |
|---|---:|---|
| `rg -n "scaling.y = Math.max(0.02|teardownT\(\)|collectTeardownMeshes" frontend/js/renderer scripts` | 10 | No. The construction rise intentionally starts at 2%; only the teardown path required disappearance. |

## Open questions / Follow-ups

None.
