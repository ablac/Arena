# Arena Admin Ops Console Implementation Plan

> **Status:** Superseded historical plan. The approved design in
> [`../specs/2026-07-07-admin-ops-console-design.md`](../specs/2026-07-07-admin-ops-console-design.md)
> removed public website editing from this pass and is authoritative where the
> documents differ. The unchecked boxes below record the original proposal; they
> are not an active implementation queue.
>
> **Historical note:** The original execution workflow used checkbox (`- [ ]`)
> syntax for task tracking. These checkboxes no longer authorize implementation.

**Goal:** Build the approved Arena Admin Ops Console with PR #65 fixes, typed game controls, demo bot builder, map workshop, and editable public content blocks.

**Architecture:** Keep the Go/admin-auth/static-frontend stack. Add backend registries for content, demo bot templates, and map templates, then present them through a redesigned admin page. The public site reads validated content blocks by key instead of letting admins write arbitrary files.

**Tech Stack:** Go, chi, pgx/Postgres, vanilla HTML/CSS/JS, Canvas 2D, GitHub Actions CI.

---

### Task 1: PR #65 Update Fixes

**Files:**
- Modify: `go-arena/internal/api/update.go`
- Modify: `go-arena/internal/api/update_test.go`

- [ ] Add a test that `triggerUpdate` returns HTTP 202 for accepted sidecar work.
- [ ] Add a test that `updateStatus` builds a valid `/status` URL when `ARENA_UPDATER_URL` ends in `/update/`.
- [ ] Add a dedicated sidecar HTTP client and helper for updater URLs.
- [ ] Run `go test ./internal/api`.

### Task 2: Backend Registries

**Files:**
- Modify: `go-arena/internal/db/queries.go`
- Modify: `go-arena/internal/demobots/config.go`
- Modify: `go-arena/internal/demobots/manager.go`
- Modify: `go-arena/internal/game/map_shapes.go`
- Modify: `go-arena/internal/config/config.go`
- Modify: `go-arena/internal/api/admin.go`
- Create: `go-arena/internal/api/admin_registry.go`
- Create: `go-arena/internal/api/admin_registry_test.go`

- [ ] Add idempotent schema for admin content blocks, demo bot templates, and custom map templates.
- [ ] Add validation helpers for demo bot templates, map names, map pools, and content blocks.
- [ ] Add admin endpoints for content blocks, demo bot templates, spawning by template, map settings, map previews, and custom maps.
- [ ] Teach random map selection to use an enabled pool and custom templates.
- [ ] Run focused Go tests.

### Task 3: Admin Ops Console UI

**Files:**
- Modify: `frontend/admin/index.html`

- [ ] Replace the dense controls panel with an ops-console dashboard section, typed config groups, demo bot builder, map workshop, and content editor.
- [ ] Add Arena-themed visual polish and animations while preserving existing panels and actions.
- [ ] Wire new endpoints into the Admin page with clear save/apply feedback.
- [ ] Run JS syntax checks.

### Task 4: Public Content Runtime

**Files:**
- Modify: `frontend/index.html`
- Create: `frontend/js/content-blocks.js`
- Modify: `go-arena/internal/api/router.go`

- [ ] Add `data-content-key` hooks to editable public site text.
- [ ] Add a public read endpoint for published content blocks.
- [ ] Add frontend fallback behavior so static text remains if content fetch fails.
- [ ] Run JS syntax checks.

### Task 5: Verification And PR

**Files:**
- All changed files.

- [ ] Run `go test ./...`.
- [ ] Run JavaScript syntax checks.
- [ ] Run `git diff --check`.
- [ ] Browser-check the Admin page at desktop and mobile widths.
- [ ] Commit, push, and open the follow-up PR.
