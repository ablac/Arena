// A total rendering outage must not be silent, to the spectator or to the log.
//
// Both spectator entrypoints used to do this and only this:
//
//     } catch (err) {
//       console.error('[App] Engine init failed:', err);
//     }
//
// So the spectator got a black rectangle under a live HUD and a live kill feed
// with nothing to say what had gone wrong, and — because the throw is CAUGHT —
// window.onerror never fired, so client-errors.js never reported it either.
// installClientErrorReporting() only auto-reports uncaught errors and unhandled
// rejections; an awaited failure inside a try/catch is neither.
//
// The result was a rendering outage with no user-facing message and no
// server-side trace, which is the whole reason "the arena does not render for
// some people" had no cause attached to it. Reproduced on the live site
// 2026-09-07: a browser with no working backend renders the full site chrome,
// the telemetry panel and the kill feed over an empty black arena, in silence.
//
// The mobile entrypoint carried the identical defect and matters more, because
// that is where the weakest GPUs are.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = (rel) => readFileSync(new URL(`../frontend/${rel}`, import.meta.url), 'utf8');

// --- both entrypoints must call it --------------------------------------
for (const [file, source, tag] of [
  ['js/app.js', read('js/app.js'), 'app.arenaEngine.init'],
  ['m/mobile.js', read('m/mobile.js'), 'mobile.arenaEngine.init'],
]) {
  const at = source.indexOf('} catch (err) {', source.indexOf('.init();'));
  assert.ok(at > 0, `${file} must still catch the engine-init failure`);
  const block = source.slice(at, at + 1400);
  assert.match(block, new RegExp(`reportEngineInitFailure\\(err, '${tag}'\\)`),
    `${file}: a caught engine-init failure must report itself; window.onerror will not`);
  assert.match(block, /showArenaRenderFallback\(err\)/,
    `${file}: a caught engine-init failure must tell the spectator something`);
  assert.match(source, /from '\.?\.?\/?(?:\.\.\/)?js\/render-failure\.js\?v=|from '\.\/render-failure\.js\?v=/,
    `${file} must import the shared failure surface, not re-implement it`);
}

// --- the module itself, exercised ---------------------------------------
const nodes = () => ({
  'arena-render-fallback': { hidden: true },
  'arena-render-fallback-retry': { hidden: false },
  'arena-render-fallback-detail': { textContent: '' },
});

let dom = nodes();
let added = [];
let reports = [];
let search = '';
let hasGPU = true;
let webglCtx = null;

globalThis.document = {
  getElementById: (id) => dom[id] ?? null,
  createElement: () => ({ getContext: () => webglCtx }),
  body: { classList: { add: (c) => added.push(c) } },
};
Object.defineProperty(globalThis, 'location', {
  configurable: true,
  get: () => ({ get search() { return search; } }),
});
// Node defines navigator as a getter-only global, so it has to be redefined.
Object.defineProperty(globalThis, 'navigator', {
  configurable: true,
  get: () => ({ get gpu() { return hasGPU ? {} : undefined; } }),
});
globalThis.__arenaReportError = (...a) => reports.push(a);

// Loaded from a data URL, like the other renderer tests: the frontend has no
// package.json of its own, so importing the .js directly makes Node warn about
// an unspecified module type on every run.
const { reportEngineInitFailure, showArenaRenderFallback } = await import(
  `data:text/javascript;base64,${Buffer.from(read('js/render-failure.js')).toString('base64')}`
);

// The reporter carries what triage cannot recover afterwards. A client that
// advertises a backend and still fails is a different bug from one that never
// had it, and only the report can tell them apart.
search = ''; hasGPU = true; webglCtx = null; reports = [];
reportEngineInitFailure(new Error('WebGL not supported'), 'app.arenaEngine.init');
assert.equal(reports.length, 1, 'the failure must reach /client-errors');
let [kind, err, extra] = reports[0];
assert.equal(kind, 'engine-init');
assert.equal(err.message, 'WebGL not supported');
assert.equal(extra.source, 'app.arenaEngine.init');
assert.equal(extra.forcedWebGL, false);
assert.equal(extra.webgpuAdvertised, true);
assert.equal(extra.webglAdvertised, false);

search = '?webgpu=0'; hasGPU = false; webglCtx = {}; reports = [];
reportEngineInitFailure(new Error('boom'), 'mobile.arenaEngine.init');
[, , extra] = reports[0];
assert.equal(extra.source, 'mobile.arenaEngine.init');
assert.equal(extra.forcedWebGL, true, '?webgpu=0 must be recorded: it changes what the report means');
assert.equal(extra.webgpuAdvertised, false);
assert.equal(extra.webglAdvertised, true);

// A reporter that throws inside a failure handler would take the page with it.
reports = [];
const realLocation = Object.getOwnPropertyDescriptor(globalThis, 'location');
Object.defineProperty(globalThis, 'location', {
  configurable: true, get() { throw new Error('hostile'); },
});
assert.doesNotThrow(() => reportEngineInitFailure(new Error('x')));
assert.equal(reports.length, 0);
Object.defineProperty(globalThis, 'location', realLocation);

// Absent reporting (client-errors.js not installed) must not throw either.
const savedReporter = globalThis.__arenaReportError;
delete globalThis.__arenaReportError;
search = '';
assert.doesNotThrow(() => reportEngineInitFailure(new Error('x')));
globalThis.__arenaReportError = savedReporter;

// --- the spectator-facing state -----------------------------------------
const show = (s, message) => {
  dom = nodes(); added = []; search = s;
  showArenaRenderFallback(new Error(message));
  return dom;
};

let d = show('', 'WebGPU device request failed');
assert.equal(d['arena-render-fallback'].hidden, false, 'the fallback panel must actually be shown');
assert.ok(added.includes('arena-render-failed'));
assert.equal(d['arena-render-fallback-retry'].hidden, false,
  'a WebGPU fault must offer the ?webgpu=0 retry, which is the path that can still work');

// Offering a retry that cannot work is worse than offering none: both of these
// mean WebGL itself is gone.
assert.equal(show('', 'WebGL not supported')['arena-render-fallback-retry'].hidden, true,
  'a WebGL fault must not offer a retry into WebGL');
d = show('?webgpu=0', 'anything');
assert.equal(d['arena-render-fallback-retry'].hidden, true,
  'a retry must not be offered to someone already in compatibility mode');
assert.match(d['arena-render-fallback-detail'].textContent, /cannot draw the 3D view/);

// A page without the panel (any other page importing this) must be a no-op.
dom = {}; added = [];
assert.doesNotThrow(() => showArenaRenderFallback(new Error('x')));
assert.deepEqual(added, [], 'no panel means no body class');

// --- the markup and styling it drives ------------------------------------
for (const [page, cssFile] of [['index.html', 'css/site-shell.css'], ['m/index.html', 'm/mobile.css']]) {
  const html = read(page);
  const css = read(cssFile);
  for (const id of ['arena-render-fallback', 'arena-render-fallback-retry', 'arena-render-fallback-detail']) {
    assert.ok(html.includes(`id="${id}"`), `${page} must carry #${id}`);
  }
  assert.match(html, /<div class="arena-render-fallback" id="arena-render-fallback" hidden>/,
    `${page}: the panel must start hidden so a healthy arena never shows it`);
  assert.match(html, /href="\?webgpu=0"/, `${page}: the retry must point at the compatibility path`);
  // It must sit in the positioned box its `inset: 0` resolves against, next to
  // the canvas it stands in for; anywhere else and it covers the wrong thing.
  const panelAt = html.indexOf('id="arena-render-fallback"');
  const canvasAt = html.indexOf('id="arena-canvas"');
  assert.ok(canvasAt >= 0 && panelAt > canvasAt, `${page}: the panel must follow the canvas`);
  assert.ok(panelAt < html.indexOf('</div>', panelAt + 1) , `${page}: panel must be well formed`);

  assert.match(css, /\.arena-render-fallback \{/, `${cssFile}: the panel must be styled`);
  assert.match(css, /\.arena-render-fallback\[hidden\] \{\s*display: none;/,
    `${cssFile}: hidden must beat the flex display or the panel shows on every healthy load`);
  assert.match(css, /\.arena-render-fallback \{[\s\S]*?pointer-events: none;/,
    `${cssFile}: the overlay must not swallow input aimed at the controls beneath it`);
  assert.match(css, /\.arena-render-fallback-action \{[\s\S]*?pointer-events: auto;/,
    `${cssFile}: the retry link must still be clickable inside that overlay`);
}

console.log('a failed engine init is reported to the server and explained to the spectator, on both entrypoints');
