# Bug: Disabled chat remains visible

> Status: FIXED
> Mode: default
> Severity: functional
> Author: Keith
> Last updated: 2026-07-14

## Symptom

When an admin disables chat while the panel is open, the chat surface remains visible with a status message. Anonymous visitors can also see a contradictory sign-in watermark during the transition.

## Expected

Disabling chat closes the panel, hides the sign-in watermark, and leaves a visibly struck, disabled launcher labeled `Chat disabled`. Re-enabling chat restores the launcher.

## Reproduction

- Command: `node scripts/test-chat-disabled-ui.mjs`
- Test location: `scripts/test-chat-disabled-ui.mjs`
- Reproduction stability: 3/3 reliably failed before the implementation because the availability transition did not exist.

## Hypotheses and diagnosis

| # | Hypothesis | Verdict | Evidence |
|---|---|---|---|
| H1 | The live `chat_settings` handler updates only status/composer state and never closes or disables the outer UI. | confirmed (root cause) | The handler changed `runtimeEnabled`, status text, watermark state, and composer controls, but did not remove the overlay's `open` class or disable either launcher. |
| H2 | A separate desktop or mobile CSS rule forces the panel to remain visible after JavaScript closes it. | eliminated | Both layouts use the same `open` class as their visibility gate; removing it and setting `aria-hidden=true` closes each layout in the regression test. |

## Root cause

Runtime availability was modeled only at the message composer layer. The launcher and overlay remained interactive after the admin kill switch changed state, so the user could keep seeing the chat surface even though posting was disabled.

## Fix

- `frontend/js/chat-panel.js`: added one availability transition shared by desktop and mobile, called for initial config, WebSocket status, and live admin settings.
- `frontend/css/chat.css`: added disabled launcher styling and a diagonal strike through the chat icon.
- `frontend/index.html` and `frontend/m/index.html`: bumped chat asset versions so the fix is not hidden by an old browser cache entry.
- `.github/workflows/ci.yml`: runs the new regression test in the existing frontend check.

## Verification

- V-1: failing test to GREEN: `node scripts/test-chat-disabled-ui.mjs` passed.
- V-2: implementation stashed while the test remained, then the test returned RED; implementation restored and the test returned GREEN.
- V-3: 55 frontend/SDK JavaScript files passed syntax checks, all 45 UI script checks passed, 20 updater tests passed, and 8 Node SDK tests passed.
- V-4: `git diff --check` passed.

## Regression test

- Path: `scripts/test-chat-disabled-ui.mjs`
- Behavior: disabled chat closes the panel, hides the sign-in watermark, disables and relabels the launcher, renders the visual strike, can be re-enabled, and does not close an already-open panel on a normal enabled status.

## Pattern analysis

| Search | Hits | Same-class risk |
|---|---:|---|
| `git grep -n "case 'chat_settings'" -- frontend` | 1 | No. The only live chat availability handler is now routed through the shared transition. |

## Open questions / Follow-ups

None.
