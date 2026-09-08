# Arena Overall Polish Implementation Plan

> For agentic workers: use isolated task workspaces and bounded independent ownership. Coordinator performs integration and final verification.

**Goal:** Make the glass-space Arena feel coherent, legible and materially convincing in normal play and close-up inspection.

**Architecture:** Refine existing environment, material, animation and spectator seams. Three independent visual work items feed a coordinator-owned lighting and camera pass, followed by one integrated verification stage.

**Tech Stack:** Vanilla JavaScript, Babylon.js, CSS, existing Node/Go/Playwright checks.

**Spec:** `docs/superpowers/specs/2026-09-07-overall-polish-design.md`

## Global constraints

- Assemble all production changes before test execution, as the user requested.
- Preserve gameplay, contact timing, protocol, catalog IDs, cosmetic mounts, source of identity and quality settings.
- Keep settings imports bare and shared textures scene-owned with safe clone disposal.
- Preserve WebGPU failure containment, same-canvas recovery and shader fallback.
- No publication, deployment, direct protected-branch writes, dependency upgrades or unrelated work.
- Writers use separate managed workspaces; only the coordinator integrates.

### Task 1: Make the orbital stage read as glass and metal

**Files:** `frontend/js/renderer/environment.js`, `glass-deck.js`, `obstacles.js`, `map-walls.js`; focused existing/new environment checks.

**Interfaces:** retain `EnvironmentRenderer`, map palettes, `GlassDeck`, obstacle layout/cluster contracts and existing quality toggles. Engine owns all scene lights.

- [ ] Inspect the floor blend/depth/shader paths and current cover/frame materials.
- [ ] Increase stable base visibility while retaining transparency; add subtle surface panel seams and broad reflected highlights on the glass.
- [ ] Give cover and frame readable bevels/edges/material contrast using batched geometry and stable scene resources.
- [ ] Refine planet/cloud/noise scales and atmosphere to remove the repetitive mottled appearance. Keep the background quiet around the stage.
- [ ] Retain quality gates and lifecycle disposal. Write regression assertions for finite deck parts, material readiness, map transitions and no resource growth, without executing yet.
- [ ] Commit this isolated surface and return its exact head and review notes.

### Task 2: Refine chassis, equipment and motion

**Files:** `frontend/js/renderer/forge-surfaces.js`, `character-rig.js`, `character-anims.js`, `forge-weapons.js`, `cosmetics.js`, `body-form-geometry.js`; focused character checks.

**Interfaces:** preserve rigs, anchors, body profile IDs, material ownership, near/far mesh contracts, attack contact timing and state transitions. Root owns `bots.js` and lights.

- [ ] Trace shared material creation and clone adoption before changing finish maps.
- [ ] Author distinct brushed steel, matte graphite and painted armor response; reduce broad self-illumination while preserving visible colored accents and legacy lighting-off behavior.
- [ ] Refine torso/shoulder/foot stance and weight transfer in existing motion functions; keep exact attack contact poses and no accumulating transforms.
- [ ] Keep all 25 forms and cosmetics compatible; avoid increasing mesh counts without a visible purpose.
- [ ] Update meaningful material/motion regressions for the new requirements without executing yet.
- [ ] Commit and return exact head, changed seams and verification that remains pending.

### Task 3: Refine the spectator shell

**Files:** `frontend/css/site-shell.css`, `frontend/css/arena.css`, `frontend/m/mobile.css` and existing HUD CSS only; no JS or HTML edits unless coordinated.

**Interfaces:** retain all IDs, accessibility semantics, safe-viewport measurements, panel modes and viewport breakpoints.

- [ ] Inspect live wide/close screenshots and current CSS cascade.
- [ ] Increase low-contrast text clarity, simplify visual framing, reduce heavy panel treatment and emphasize controls needed while spectating.
- [ ] Keep touch targets and keyboard focus visible; preserve drawer, consent, chat and error surfaces.
- [ ] Preserve reduced-motion overrides and avoid new animation or decorative assets.
- [ ] Commit and return exact head and any needed coordinator markup changes.

### Task 4: Integrate lighting, camera and action hierarchy

**Files:** `frontend/js/renderer/engine.js`, `camera.js`, `bot-body.js`/`bots.js`/`world-hud.js` as needed, settings, cache-tag entry points and corresponding tests.

- [ ] Establish shared exposure/contrast/vignette bases used both at initialization and after dynamic grading resets.
- [ ] Tune existing key/fill/rim directions and colors for readable front/side surfaces without adding a shadow pass.
- [ ] Implement stable combat framing: choose a nearby living pair, retain focus for a dwell interval, use hysteresis before switching and restore manual pan/zoom control immediately.
- [ ] Reduce the oversized selected-bot marker while retaining a clear ring and readable name/health display.
- [ ] Integrate the three isolated commits and inspect their combined differences.
- [ ] Update only necessary asset tags through the full imported module closure; retain bare settings imports.

### Task 5: Verify and deliver

- [ ] Mark assembly complete before any test execution.
- [ ] Run focused environment, camera, character and settings regressions, syntax and Babylon runtime validation.
- [ ] Run all CI frontend/SDK checks and Go vet/tests; diagnose rather than suppress failures.
- [ ] Run real browser scenes for phone/desktop maps, quality off/on, combat camera/manual override, all chassis/forms and cosmetic clone lifecycle. Compare screenshots with the captured baseline.
- [ ] Obtain independent exact-head review and resolve material findings.
- [ ] Commit final docs and source, run the protected local integration controller with matching review evidence, verify its receipt and cleanup.
- [ ] Update the existing Arena unreleased changelog and finish task logs. Return visible comparison and concise outcome with honest limitations.
