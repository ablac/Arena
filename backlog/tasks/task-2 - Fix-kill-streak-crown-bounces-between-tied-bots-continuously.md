---
id: TASK-2
title: 'Fix: kill streak crown bounces between tied bots continuously'
status: To Do
assignee: []
created_date: '2026-07-03 06:34'
labels:
  - bug
dependencies: []
priority: high
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
GitHub issue #14. When two bots are tied on kill streak the crown reassigns every evaluation tick, bouncing between them. Needs a stable tiebreak (keep current holder until strictly beaten, or tiebreak on earliest-reached). Done when a tie leaves the crown stable on one bot.
<!-- SECTION:DESCRIPTION:END -->
