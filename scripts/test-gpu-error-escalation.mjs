// The rung above scripts/test-bloom-failure-latch.mjs.
//
// That test proves containment ARMS from the async GPU error channel. This one
// proves it KEEPS GOING when the first remedy does not work, which is the case
// that produced the blank arena on arena.angel-serv.com.
//
// The listener used to open with:
//     if (!this.pipeline || !this.pipeline.bloomEnabled) return;
// and its own remedy sets bloomEnabled false. So it could fire exactly once. If
// the failing pass was not bloom, the first three errors dropped bloom, the
// fault continued, and every later error hit that guard and was discarded. The
// synchronous escalation could not help either: its own comment records that
// _onRenderLoopError is never called a second time, because the steady-state
// fault is an async GPUValidationError that never throws into JS. Nothing was
// left to run, so the canvas stayed black at a healthy frame rate.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const src = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');

const method = (name) => {
  const head = `  ${name}(err) {`;
  const i = src.indexOf(head);
  assert.ok(i > 0, `${name} not found`);
  let depth = 0, j = src.indexOf('{', i);
  for (let k = j; k < src.length; k++) {
    if (src[k] === '{') depth++;
    else if (src[k] === '}') { depth--; if (depth === 0) { j = k; break; } }
  }
  return new Function(`return function ${src.slice(i + `  ${name}`.length, j + 1)}`)();
};

const onGPUError = method('_onUncapturedGPUError');
const contain = method('_containBloomFailure');
const quiet = { error() {}, warn() {}, log() {} };
const realConsole = globalThis.console;

const buildPipeline = (engine) => ({
  bloomEnabled: !engine._bloomBroken,
  disposed: false,
  dispose() { this.disposed = true; },
});

// A device that keeps rejecting work no matter what is switched off: the case
// the old guard could not represent, because it stopped looking after one rung.
const newEngine = () => {
  const e = {
    _onUncapturedGPUError: onGPUError,
    _containBloomFailure: contain,
    fellBack: 0,
    _fallBackToWebGL() { this.fellBack++; },
  };
  e.pipeline = buildPipeline(e);
  return e;
};
const err = () => new Error('No bind group set at group index 1');
// Three errors is one rung's worth; anything less must be treated as noise.
const rung = (e, n = 3) => { for (let i = 0; i < n; i++) e._onUncapturedGPUError(err()); };

globalThis.console = quiet;
try {
  // --- noise must not demote anyone ---
  const noisy = newEngine();
  noisy._onUncapturedGPUError(err());
  noisy._onUncapturedGPUError(err());
  assert.equal(noisy.pipeline.bloomEnabled, true, 'two errors is noise, not a signal');
  assert.equal(noisy._bloomBroken, undefined, 'and must not arm the latch');

  const e = newEngine();
  assert.equal(e.pipeline.bloomEnabled, true, 'bloom starts on for a device with no history');

  // --- rung 1: drop the pass that is known to fail on a real WebGPU client ---
  rung(e);
  assert.equal(e.pipeline.bloomEnabled, false, 'rung 1 must drop the bloom pass');
  assert.equal(e._bloomBroken, true, 'and latch it for the session');
  assert.ok(e.pipeline, 'rung 1 must not take the whole pipeline with it');
  assert.equal(e.fellBack, 0, 'and must not abandon the backend on the first sign of trouble');

  // --- rung 2: the errors continue, so bloom was not the culprit ---
  // THIS is the rung the old guard could never reach: bloomEnabled is false
  // here, and the listener returned early on exactly that condition.
  rung(e);
  assert.equal(e.pipeline, null, 'rung 2 must drop the whole post-process pipeline');
  assert.equal(e.fellBack, 0, 'but still not the backend, which may yet be fine');

  // --- rung 3: nothing is post-processing and the device still rejects work ---
  rung(e);
  assert.equal(e.fellBack, 1, 'rung 3 must leave WebGPU for WebGL');

  // --- and the fallback is latched, not repeated every three errors ---
  rung(e);
  rung(e);
  assert.equal(e.fellBack, 1, 'the backend swap must happen once, not on a loop');

  // --- a healthy device is never touched, because it emits no such events ---
  const healthy = newEngine();
  assert.equal(healthy.pipeline.bloomEnabled, true, 'healthy device keeps bloom');
  assert.equal(healthy.fellBack, 0, 'and keeps WebGPU');
} finally {
  globalThis.console = realConsole;
}

console.log('GPU error containment escalates bloom -> post-process -> backend');
console.log('each rung needs its own three errors, and the backend swap latches');

// The regression this file exists for. If the early-return guard comes back in
// any form, rung 2 is unreachable again and the blank arena returns. Pinned on
// the source rather than the simulation, because the simulation would still
// pass with the guard present in a shape it happens not to exercise.
// Scoped to the method BODY with comments stripped, not to the whole file. The
// first version of this pin searched raw `src` and failed on the JSDoc above
// _onUncapturedGPUError, which quotes the removed guard verbatim to explain why
// it went: the pin matched its own documentation. A pin that fires on prose
// makes the fix undocumentable, which is a worse outcome than the regression.
const methodSrc = (name) => {
  const head = `  ${name}(err) {`;
  const i = src.indexOf(head);
  assert.ok(i > 0, `${name} not found`);
  let depth = 0, j = src.indexOf('{', i);
  for (let k = j; k < src.length; k++) {
    if (src[k] === '{') depth++;
    else if (src[k] === '}') { depth--; if (depth === 0) { j = k; break; } }
  }
  return src.slice(i, j + 1);
};
const ladderCode = methodSrc('_onUncapturedGPUError')
  .replace(/\/\*[\s\S]*?\*\//g, '')
  .replace(/^\s*\/\/.*$/gm, '');
// Positive control: the stripper must leave the executable ladder intact, or an
// over-eager strip would make this pin vacuous by deleting what it inspects.
assert.match(ladderCode, /_fallBackToWebGL\(\)/,
  'comment stripping must not remove the code the pin is about');
assert.ok(
  !/bloomEnabled\)\s*return;/.test(ladderCode),
  'the ladder must not gate on bloomEnabled: its own remedy clears that flag, ' +
  'so gating on it makes every rung after the first unreachable',
);

// The async channel is the only one that sees the steady-state fault, and it
// reported nothing before this change: only the synchronous catch called the
// reporter, so a fault that never throws left no server-side trace and could
// not be triaged from the admin log.
assert.match(src, /__arenaReportError\?\.\('gpu-uncaptured'/,
  'the async GPU channel must report, or the failure stays invisible server-side');

// The swap is what makes the WebGL fallback able to present at all: the canvas
// was claimed by 'webgpu' for life when the failing engine was constructed.
const fallback = src.slice(src.indexOf('  _fallBackToWebGL() {'));
assert.match(fallback.slice(0, fallback.indexOf('\n  }')), /replaceCanvasElement\(this\.canvas\)/,
  'the fallback must swap the canvas, or WebGL renders into a context nothing presents');
assert.match(fallback.slice(0, fallback.indexOf('\n  }')), /this\._webGPUUnavailable = true;/,
  'and mark WebGPU unavailable, or the next rebuild goes straight back to it');
console.log('the guard that made rung 2 unreachable is gone, and the fallback swaps the canvas');
