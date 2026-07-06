# Graphics & Animation Settings System

The site has a user-facing Settings panel (gear icon, right side of the
spectator page) that lets a visitor turn off individual visual/animation
effects. Every effect added since (2026-07) should be wired through this
system rather than shipped as an unconditional, non-toggleable effect.

Everything routes through one file: **`frontend/js/settings.js`**. It is the
single source of truth for the schema, the persisted state (`localStorage`,
per-browser, no account system), and the `isEnabled(section, effect)` check
every effect site calls.

## The model

```js
SETTINGS_SCHEMA = {
  <sectionKey>: {
    label: 'Human-readable section name',
    description: 'One sentence a viewer would understand.',
    effects: {
      <effectKey>: { label: 'Human-readable effect name' },
      ...
    },
  },
  ...
}
```

`isEnabled(sectionKey, effectKey)` returns `sectionMaster && effectFlag` - an
AND, not a synced/indeterminate checkbox. Turning a section's master off
silences everything in it without touching each effect's own flag, so
flipping the master back on restores whatever was individually set before.

**All defaults are `true`.** Nothing in `buildDefaults()` should ever default
to `false` - shipping a new toggle must not change the site's current look
for anyone who has never opened Settings. If you want a *new* effect to ship
disabled-by-default, that's a product decision to raise explicitly, not
something to slip in via this system.

The settings panel UI (`frontend/js/settings-panel.js`) renders itself
entirely from `SETTINGS_SCHEMA` - adding an entry to the schema is enough to
get a working checkbox in the panel. You do not hand-edit HTML for a new
toggle.

## Adding a toggle for a new effect

1. **Add the schema entry.** Pick an existing section if the new effect
   belongs with effects a viewer would already lump together (e.g. another
   weapon-impact VFX goes in `weaponImpactVfx`). Only add a new top-level
   section if the effect doesn't fit any existing one conceptually - don't
   create a section for a single effect if a close-enough home exists.

2. **Import `isEnabled`** at the top of whatever file spawns/animates the
   effect:
   ```js
   import { isEnabled } from '../settings.js';   // from frontend/js/renderer/*.js
   import { isEnabled } from './settings.js';    // from frontend/js/*.js
   ```
   Every import site across the whole codebase uses the **exact same path,
   no `?v=` query string** on `settings.js` itself. ES modules are cached per
   exact specifier string including the query - if one file imported
   `../settings.js?v=1` and another imported `../settings.js`, the browser
   would load *two separate module instances* with two separate `state`
   objects, and toggling a setting in one would silently do nothing for
   effects wired through the other. Keep it bare, everywhere.

3. **Gate the effect at the right point** - this depends on what kind of
   effect it is:

   - **One-shot, event-triggered** (a hit spark, a death burst, a floating
     damage number - anything spawned once per game event): add
     `if (!isEnabled('section', 'effect')) return;` as the very first line
     of the spawn function. Check first whether the function does anything
     *besides* the visual spawn (e.g. stashes hit-direction data another
     system reads) - if so, only gate the visual portion and leave the
     bookkeeping unconditional. Gating a function that has non-visual side
     effects other code depends on is the most common way to introduce a
     regression here.

   - **Continuous / per-frame** (particle systems that keep emitting,
     looping animations, materials that get re-set every tick, post-process
     pipeline flags): check `isEnabled(...)` **inside the existing per-frame
     update hook** (a `registerBeforeRender`, `onBeforeRenderObservable`, or
     whatever per-tick method already drives the effect), not only at
     one-time creation. This is what makes a toggle take effect live, while
     the user is watching, with no page reload. For particle systems this
     usually means holding a `BASE_EMIT_RATE` constant and setting
     `system.emitRate = isEnabled(...) ? BASE_EMIT_RATE : 0` every frame;
     for meshes, `mesh.setEnabled(isEnabled(...))`; for pipeline booleans, a
     direct assignment. If the state driving the effect (position, target
     selection, animation phase) needs to keep advancing even while hidden
     so re-enabling doesn't show a stale/jumped result, only gate the
     *visual* half (alpha/emitRate/setEnabled), not the update math.

   - **Pure CSS animation with no JS trigger point** (a continuously
     looping `@keyframes` animation with nothing in JS ever adding the class
     that starts it): there's no function call to gate. Instead:
     - Add an entry to `CSS_ONLY_EFFECT_CLASSES` in `frontend/js/app.js`
       (`[sectionKey, effectKey, 'no-fx-<effectKey>']`).
       `syncCssOnlyEffectClasses()` toggles that class on `<body>` on boot
       and on every settings change.
     - Add the matching CSS override near the existing ones in
       `frontend/css/sections.css` (search for `no-fx-` to find them):
       `body.no-fx-<effectKey> <selector> { animation: none; }`.
     - **Watch for one class driving more than one animation.** `killfeed-new`
       drives both the kill-feed entry's slide-in appearance *and* its red
       glow flash - only the glow is the "Kill Feed Glow" toggle, so the
       override targets `.killfeed-entry.killfeed-new::before` specifically,
       not the bare class, so disabling the glow doesn't also kill the
       slide-in. If you're adding a toggle for an effect that shares a class
       with something else, scope the CSS override to the most specific
       selector that isolates just your effect.

4. **Test the default path first.** With the toggle left untouched (its
   default `true`), the effect must look and behave exactly as it did before
   your change - this is a refactor-in-place, not a behavior change. Then
   flip it off in the Settings panel and confirm the effect actually stops;
   flip it back on and confirm it resumes (live, if you followed the
   per-frame pattern above).

5. **Syntax-check before you consider it done**: this frontend has no
   bundler and no JS test framework, so `node --check <file>.js` on every
   file you touched is the fast pre-flight - it won't catch logic bugs, but
   it catches the broken-import/unbalanced-brace class of mistake before you
   ever load a browser.

## Where things live

| Piece | File |
|---|---|
| Schema, state, persistence, `isEnabled()` | `frontend/js/settings.js` |
| Settings panel UI (auto-generated from schema) | `frontend/js/settings-panel.js` |
| Gear icon + overlay markup | `frontend/index.html` (`#settings-gear`, `#settings-overlay`) |
| CSS-only effect body-class overrides | `frontend/css/sections.css`, search `no-fx-` |
| Kill Flash / Site Motion gating | `frontend/js/app.js`, `frontend/js/leaderboard.js` |
| Combat/weapon VFX gating | `frontend/js/renderer/effects.js` |
| Bot body state (death/impact/wounded/corpse) | `frontend/js/renderer/bots.js`, `animations.js` |
| Ambient scene (skybox/particles/pylons/etc.) | `frontend/js/renderer/environment.js` |
| Objective/zone indicators, movement trails, bloom/vignette | `frontend/js/renderer/gameplay.js`, `trails.js`, `engine.js` |
| Idle weapon animations | `frontend/js/renderer/weapons.js` |
