---
id: TASK-3
title: 'Dynamic map size: scale arena with connected bot count'
status: To Do
assignee:
  - joey
created_date: '2026-07-03 06:34'
labels:
  - enhancement
dependencies: []
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
GitHub issue #12. UPDATE 2026-07-03: the core feature LANDED in PR 29 (ApplyDynamicArenaSize in go-arena/internal/game/arena_size.go: linear scale-up 1.0x at ARENA_SIZE_BASE_BOTS up to 2.0x at ARENA_SIZE_MAX_BOTS, round-boundary-only, obstacle density area-scaled, pristine-base capture). joey's retroactive peer review 2026-07-03: RETROACTIVELY APPROVED with non-blocking findings. Remaining scope, per the goal-lead verdict: (1) scale-DOWN delta: ARENA_SIZE_MIN_SCALE knob applied from 2 bots up to BaseBots, DEFAULT 0.6 honoring the issue intent (2-bot duel on a small arena), round-boundary-only, docker-go tests covering the 2-bot floor, the BaseBots boundary, and mid-match count changes deferring to next round; (2) close issue #12 citing PR 29 plus the delta with evidence.
<!-- SECTION:DESCRIPTION:END -->
