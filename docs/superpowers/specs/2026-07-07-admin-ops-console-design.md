# Arena Admin Ops Console Design

## Goal

Rebuild the Arena Admin Panel into a polished operations console that matches the energy of `arena.angel-serv.com`, fixes the PR #65 self-update follow-ups, and gives operators practical controls for demo bots, gameplay tuning, maps, and editable public website content.

## Product Direction

The Admin Panel should feel like a live arena control room, not a dense debug page. The first impression should be a dark, high-contrast operations surface with animated telemetry, clear status cards, fast navigation, and controls that show whether a change applies immediately or at the next round.

The current panel has useful functionality but too much of it lives in a single terminal-like page with small controls. Game configuration is hard to edit safely, demo bot controls only support a count-based start/stop flow, and public site/document copy still requires code changes. The redesign should preserve existing admin capabilities while adding focused builder surfaces.

## Architecture

Keep the existing Go backend, admin auth, and static frontend model. Add focused backend endpoints for admin-managed registries:

- Demo bot templates
- Map controls, map preview, and generated custom map templates
- Editable public content blocks

Do not allow arbitrary filesystem edits from the Admin Panel. Public-site editing should work through validated content blocks that the frontend can fetch and apply by key. This gives operators no-PR copy updates without exposing a general file writer.

The frontend can remain static HTML/CSS/JS for this PR, but the Admin page should be reorganized into clear visual sections with reusable JavaScript helpers. A later PR can split the single file further if desired.

## Admin UX

Use the approved mockup direction:

- Left rail navigation grouped by Operate, Tune, Publish, and System.
- Animated status cards for health, bots, tick rate, version, and draft changes.
- Larger panels with clear visual hierarchy and 8px radius controls.
- Arena-themed motion: low-key pulse effects, animated metric sheen, map-preview draw animation, and progress rings.
- No decorative orb backgrounds. Use the Arena site’s energetic dark interface, grid/holographic accents, cyan/green/amber/red state colors, and compact operational density.

## Gameplay Controls

Game configuration should become typed and grouped:

- Core pacing: tick rate, max bots, max spectators, round duration, intermission, lobby countdown.
- Modes: game mode, team count, friendly fire.
- Zone: damage, shrink percent, shrink interval, min radius, shrink delay.
- Movement/stats: stat budget, stat multipliers, dodge tuning, projectile speed, AFK timeout.
- Arena scale: base width/height and dynamic map sizing values when supported.

Each control should declare validation, allowed ranges, and whether changes are live or next-round. Save responses should show accepted and rejected fields instead of simply saying “saved”.

## Demo Bot Builder

Add a builder for demo bots:

- Show built-in and custom templates.
- Show running/stopped state for each active demo bot instance.
- Allow spawning one or many bots from a selected template.
- Allow stopping one bot or all bots.
- Allow creating/updating custom templates with name, weapon, strategy, color, and four stats.
- Enforce the existing stat budget and supported weapon/strategy lists.

Built-in templates are read-only. Custom templates are admin-managed and persisted when the database is available.

## Map Workshop

Add a map workshop for actual gameplay:

- Enable/disable map types used by random map selection.
- Keep direct map selection for next round.
- Preview built-in map shapes before applying.
- Generate custom maps from a base shape and seed.
- Save custom map templates and include them in the random pool when enabled.
- Show playable percentage, grid preview, and next-round impact.

This PR should support generated custom map templates through validated map metadata, not freehand geometry editing. The game engine should treat saved custom templates as first-class map shape options.

## Website And Docs Editing

Add a content manager for public text blocks:

- Homepage hero title/subtitle/CTA labels.
- Site announcement/banner text.
- Bot guide notice text.
- Rules or onboarding snippet.

Admin edits should write content records, not source files. The public frontend should fetch published content blocks and apply them to elements with `data-content-key`. If the fetch fails, the static source text remains the fallback.

## PR #65 Review Fixes

The next PR must include:

- `POST /api/v1/admin/update` returns `202 Accepted` when the sidecar accepts async update work.
- Status URL construction handles updater URLs with or without trailing slashes.
- Sidecar calls use a dedicated HTTP client instead of the GitHub API client.

## Testing

Backend tests should cover:

- PR #65 update handler behavior.
- Map random pool validation and fallback.
- Map preview and custom map registration.
- Demo bot template validation.
- Content block validation and fallback defaults.

Frontend checks should cover JavaScript syntax. Browser verification should load the Admin page and inspect the new control surfaces at desktop and mobile widths.

## 2026-07-07 Control Center V2 Follow-Up

The production failure on the Demo Bot Builder and Public Website Content panels was caused by missing admin registry tables. The admin endpoints should not hard-fail those operator surfaces when a registry table is missing or temporarily unavailable; read paths now fall back to built-in demo templates/content blocks, while writes still require the database.

The second pass expands the approved visual direction beyond the Operations Console tab:

- Live Arena becomes a tactical overhead map desk with live metric chips, zone pressure, pickup/weapon summaries, improved canvas rendering, and click-to-act behavior preserved.
- Live Bots becomes a card roster plus dense table. Cards show health, weapon, K/D, Elo, damage, uptime, pinning, profiling, healing, freezing, and kill actions while bulk actions remain table-checkbox based.
- Tick Inspector becomes a readiness dashboard instead of raw counters, translating tick rate, heap, goroutines, projectiles, and spectators into operator-readable health signals.
- Anti-Cheat becomes a review queue. Signals are scored by risk and confidence, shared IPs are review-only, and kick/ban buttons are reserved for high-confidence findings.
- Match configuration is split into Match Flow, Capacity, Match Rules, Zone Rules, Movement and Combat, and Stat Curves. Map pools, terrain generation, demo templates, and public website content stay in their own workspaces.

Browser verification should confirm the rebuilt tabs render without console errors or horizontal overflow:

- `?tab=controls`
- `?tab=minimap`
- `?tab=bots`
- `?tab=ticks`
- `?tab=anticheat`
