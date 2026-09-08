// The blank arena that no GPU-error ladder could ever catch.
//
// scripts/test-gpu-error-escalation.mjs contains the rungs that fire when the
// device REJECTS work. This file covers the case where the device is never
// asked to do any, which produces the identical symptom and is invisible to all
// of it: a black canvas at a healthy frame rate, a live HUD, a live kill feed,
// and nothing in the log.
//
// frontend/js/renderer/environment.js authors the skybox and the energy floor as
// GLSL ShaderMaterials. Babylon ships WGSL for its own materials, but a GLSL
// ShaderMaterial on WebGPU has to be transpiled, and Babylon fetches glslang and
// twgsl for that from cdn.babylonjs.com at RUNTIME — which is why that host sits
// in the production CSP's script-src and connect-src.
//
// In the vendored bundle the fetch is lazy, unbounded and unrejectable:
//
//     prepareGlslangAndTintAsync() {
//       return this._workingGlslangAndTintPromise || (
//         this._workingGlslangAndTintPromise = new Promise((resolve) => {
//           this._initGlslangAsync(...).then((g) => {
//             ...initTwgsl(...).then(() => { ...; resolve(); })
//           })
//         }))
//     }
//
// The executor takes `resolve` only: no `reject`, no `.catch`. A blocked,
// filtered, throttled or down CDN leaves that promise PENDING FOR THE LIFE OF
// THE PAGE. It is awaited inside _preparePipelineContextAsync, so the skybox and
// floor effects never become ready and never draw, without one error anywhere.
//
// It also explains the oldest clue in this bug: ?webgpu=0 has always looked like
// a cure, because WebGL consumes that GLSL directly and asks the CDN for nothing.
//
// The fix pulls the fetch into startup and puts a clock on it, so the failure
// lands in the WebGL fallback that already exists instead of hanging forever.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
const isolated = source.replace(/import[\s\S]*?from '[^']+';\r?\n/g, '');
const moduleURL = `data:text/javascript;base64,${Buffer.from(isolated).toString('base64')}`;
const { prepareGLSLTranspilerWithin } = await import(moduleURL);

assert.equal(typeof prepareGLSLTranspilerWithin, 'function',
  'the renderer must expose a bounded GLSL-transpiler preparation step');

// 1. The production failure: a promise that never settles, exactly as the
//    vendored bundle produces when the CDN cannot be reached. This is the whole
//    bug — before the fix this await simply never returned.
const hangingEngine = { prepareGlslangAndTintAsync: () => new Promise(() => {}) };
const startedAt = performance.now();
await assert.rejects(
  prepareGLSLTranspilerWithin(hangingEngine, 20),
  /GLSL transpiler unavailable/,
  'a transpiler fetch that never settles must fail, not hang the arena forever',
);
const elapsed = performance.now() - startedAt;
assert.ok(elapsed < 500, `the bound is not being enforced (${elapsed.toFixed(1)}ms)`);

// 2. A healthy client must be untouched. If this rung could fire on a working
//    device it would demote every spectator to WebGL.
await prepareGLSLTranspilerWithin(
  { prepareGlslangAndTintAsync: () => Promise.resolve() },
  20,
);

// 3. WebGL has no such method, and neither would a Babylon that stopped needing
//    a transpiler. Both mean there is nothing to wait for, not a failure.
await prepareGLSLTranspilerWithin({}, 20);
await prepareGLSLTranspilerWithin(null, 20);

// 4. A build that rejects rather than hanging must still reach the fallback.
await assert.rejects(
  prepareGLSLTranspilerWithin(
    { prepareGlslangAndTintAsync: () => Promise.reject(new Error('blocked by client')) },
    1000,
  ),
  /blocked by client/,
  'a rejected transpiler fetch must propagate to the WebGL fallback',
);

// 5. A synchronous throw is the third way this can fail and must not escape as
//    something other than a rejection.
await assert.rejects(
  prepareGLSLTranspilerWithin(
    { prepareGlslangAndTintAsync() { throw new Error('no network'); } },
    1000,
  ),
  /no network/,
);

// 6. Placement is the fix, and it is a NON-blocking placement. Awaiting this in
//    init() would hold the whole scene behind ~2.7MB of third-party WASM on
//    every WebGPU client; dropping the call restores the hang outright. Pin
//    both: init() arms the watch, and does not await the transpiler itself.
const initSrc = source.slice(source.indexOf('  async init() {'), source.indexOf('  _watchGLSLTranspiler('));
assert.ok(initSrc.length > 0, 'init() must stay discoverable');
assert.match(initSrc, /this\._watchGLSLTranspiler\(engine\);/,
  'init() must arm the transpiler watch');
assert.doesNotMatch(initSrc, /await prepareGLSLTranspilerWithin/,
  'init() must NOT block startup on a 2.7MB third-party download');
assert.ok(
  initSrc.indexOf('this.ready = true;') < initSrc.indexOf('this._watchGLSLTranspiler'),
  'the watch may tear the scene down, so it must arm only once the scene is up',
);

// The failure must land in the backend rung that already exists, under the same
// latch, so this and the GPU error channel cannot both tear down one engine.
const watchSrc = source.slice(
  source.indexOf('  _watchGLSLTranspiler('),
  source.indexOf('  /** @private */\n  _addLights()'),
);
assert.match(watchSrc, /prepareGLSLTranspilerWithin\(engine\)\.catch\(/,
  'the watch must react to the rejection, not await it');
assert.match(watchSrc, /this\._webGPUFallbackPending/,
  'the watch must share the backend rung latch');
assert.match(watchSrc, /this\.engine !== engine/,
  'a verdict about a replaced engine must be discarded');
assert.match(watchSrc, /this\._fallBackToWebGL\(\)/,
  'the remedy is the WebGL fallback that ?webgpu=0 already proves works');
assert.match(watchSrc, /__arenaReportError\?\.\('gpu-transpiler'/,
  'a fallback nobody can see in the log is how this bug survived this long');

// 7. The reason this step is needed at all. If these ever become WGSL, or move
//    off ShaderMaterial, the WebGPU path stops needing a third-party CDN and
//    this whole mechanism — and cdn.babylonjs.com in the CSP — can be deleted.
//    Until then, silently losing them would silently restore the outage.
const environment = readFileSync(
  new URL('../frontend/js/renderer/environment.js', import.meta.url), 'utf8',
);
assert.match(environment, /ShadersStore\['spaceVertexShader'\]/,
  'the skybox is the GLSL ShaderMaterial this bound exists for');
assert.match(environment, /ShadersStore\['energyFloorVertexShader'\]/,
  'the energy floor is the GLSL ShaderMaterial this bound exists for');

console.log('GLSL transpiler preparation is bounded and falls back to WebGL when the CDN is unreachable');
