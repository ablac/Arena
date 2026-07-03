---
id: TASK-1
title: 'Fix: kill streak gold ring does not follow bot smoothly'
status: To Do
assignee: []
created_date: '2026-07-03 06:34'
labels:
  - bug
dependencies: []
priority: high
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
GitHub issue #13. The gold ring visual lags or jumps instead of tracking the bot mesh smoothly in the spectator renderer. Suspect the ring position updates on tick instead of lerping with the bot render position (frontend/js/renderer/bots.js or gameplay.js). Done when the ring tracks as smoothly as the bot itself at all camera distances.
<!-- SECTION:DESCRIPTION:END -->
