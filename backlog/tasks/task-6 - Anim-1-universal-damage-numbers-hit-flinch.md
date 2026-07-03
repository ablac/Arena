---
id: TASK-6
title: 'Anim 1: universal damage numbers + hit flinch'
status: To Do
assignee: []
created_date: '2026-07-03 06:59'
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
