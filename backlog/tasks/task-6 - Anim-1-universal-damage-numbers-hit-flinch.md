---
id: TASK-6
title: 'Anim 1: universal damage numbers + hit flinch'
status: In Progress
assignee: []
created_date: '2026-07-03 06:59'
updated_date: '2026-07-03 14:10'
labels:
  - enhancement
dependencies: []
priority: high
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Parent: task-5. Every successful hit pops a pooled floating damage number at the victim (magnitude-scaled, yellow for crits) plus a flinch (white flash + squash + 3-5 unit nudge away from attacker). Phase A client-only via hp-delta detection in bots.js (_lastHp cache) + effects.spawnDamageNumber; Phase B exact via a new server ArenaEvent type hit with Damage field from combat.go. Files: bots.js, effects.js, engine.js (+ state.go, combat.go for B). Effort S/M. Full spec: docs/combat-animation-plan.md section 1 (PR 33).
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Phase A shipped. Damage numbers merged (PR #38, effects.js prevHp hp-delta detection). Hit flinch in review (PR #39, bots.js root.scaling squash-recoil). Deferred to Phase B (needs server hit attribution): the white emissive flash (emissive channel is contended by attack-glow/stun/death anims) and the directional nudge away from attacker (needs attacker position from a server hit event). The squash-recoil is the readable core of the flinch and pairs with the floating numbers.
<!-- SECTION:NOTES:END -->
