import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
const start = source.indexOf('    let _lastFrame = performance.now();');
const end = source.indexOf("    if (typeof IntersectionObserver === 'function'", start);
assert.ok(start >= 0 && end > start, 'render-loop source must remain discoverable');

const renderLoopSource = source.slice(start, end);
const engine = {
  loop: null,
  runRenderLoop(callback) { this.loop = callback; },
};
let clock = 1000;
const fakePerformance = { now: () => clock };
const calls = { interpolate: 0, trail: [], projectile: [], gameplay: [], render: 0 };
const self = {
  _canvasVisible: true,
  botRenderer: {
    entries: new Map(),
    interpolate() { calls.interpolate += 1; },
    resume() { calls.resume = (calls.resume || 0) + 1; },
  },
  trailRenderer: {
    render(_entries, dt) { calls.trail.push(dt); },
    reset() { calls.trailReset = (calls.trailReset || 0) + 1; },
  },
  projectileRenderer: { update(dt) { calls.projectile.push(dt); } },
  gameplayRenderer: { animate(_entries, dt) { calls.gameplay.push(dt); } },
  pipeline: {
    isSupported: true,
    imageProcessing: {},
  },
};
const scene = { render() { calls.render += 1; } };
const isEnabled = () => true;

let visibilityHandler = null;
globalThis.document = {
  hidden: false,
  addEventListener(type, callback) {
    if (type === 'visibilitychange') visibilityHandler = callback;
  },
};
const compileLoop = new Function('engine', 'performance', 'self', 'scene', 'isEnabled',
  `${renderLoopSource}\nreturn engine.loop;`);
const frame = compileLoop(engine, fakePerformance, self, scene, isEnabled);
assert.equal(typeof frame, 'function');
assert.equal(typeof visibilityHandler, 'function', 'visibility changes must reset the frame clock even while rAF is parked');

clock = 4000;
document.hidden = true;
visibilityHandler();
document.hidden = false;
clock = 4016;
frame();
assert.ok(Math.abs(calls.trail[0] - 0.016) < 0.0001,
  `resume after a parked hidden-tab rAF must use a fresh delta (got ${calls.trail[0]})`);
assert.equal(calls.resume, 1, 'resume must snap bot roots to the latest server snapshot');
assert.equal(calls.trailReset, 1, 'resume must discard trail history from before suspension');

calls.interpolate = 0;
calls.trail.length = 0;
calls.projectile.length = 0;
calls.gameplay.length = 0;
calls.render = 0;
calls.resume = 0;
calls.trailReset = 0;

document.hidden = true;
clock = 5000;
frame();
assert.deepEqual(calls, {
  interpolate: 0, trail: [], projectile: [], gameplay: [], render: 0, resume: 0, trailReset: 0,
},
  'a hidden document must suspend all renderer-frame work');

document.hidden = false;
clock = 5016;
frame();
assert.equal(calls.interpolate, 1, 'renderer work should resume when the page becomes visible');
assert.equal(calls.resume, 1);
assert.equal(calls.trailReset, 1);
assert.ok(Math.abs(calls.trail[0] - 0.016) < 0.0001,
  `resume delta must start at the visibility boundary, not include hidden time (got ${calls.trail[0]})`);
assert.equal(calls.render, 1);

self._canvasVisible = false;
clock = 9000;
frame();
assert.equal(calls.interpolate, 1, 'an off-screen canvas must suspend bot interpolation');
assert.equal(calls.trail.length, 1, 'an off-screen canvas must suspend trails');
assert.equal(calls.projectile.length, 1, 'an off-screen canvas must suspend projectiles');
assert.equal(calls.gameplay.length, 1, 'an off-screen canvas must suspend gameplay animation');
assert.equal(calls.render, 1, 'an off-screen canvas must suspend scene rendering');

self._canvasVisible = true;
clock = 9017;
frame();
assert.equal(calls.interpolate, 2);
assert.ok(Math.abs(calls.trail[1] - 0.017) < 0.0001,
  `off-screen resume delta must start at the intersection boundary (got ${calls.trail[1]})`);
assert.equal(calls.render, 2);

const botsSource = readFileSync(new URL('../frontend/js/renderer/bots.js', import.meta.url), 'utf8');
const isolatedBotsSource = botsSource.replace(/import[\s\S]*?from '[^']+';\r?\n/g, '');
const {BotRenderer} = await import(`data:text/javascript;base64,${Buffer.from(isolatedBotsSource).toString('base64')}`);
const botRenderer = Object.create(BotRenderer.prototype);
const resumedRoot = {position: {x: 12, z: 18}};
const resumedEntry = {
  root: resumedRoot,
  currPos: [40, 55],
  prevPos: [10, 15],
  _interpReady: true,
  _poseX: 12,
  _poseZ: 18,
};
botRenderer.entries = new Map([['bot-1', resumedEntry]]);
botRenderer._lastFrame = 0;
botRenderer.resume();
assert.deepEqual([resumedRoot.position.x, resumedRoot.position.z], [40, 55],
  'resume must snap the visible root instead of gliding from its stale offscreen position');
assert.deepEqual(resumedEntry.prevPos, [40, 55]);
assert.equal(resumedEntry._poseX, 40);
assert.equal(resumedEntry._poseZ, 55);

assert.match(source,
  /this\._canvasVisible = entry\.isIntersecting;[\s\S]{0,100}frameSuspended = true;[\s\S]{0,100}resetFrameClock\(\);/,
  'intersection changes must also reset the frame clock before resuming');
assert.match(source,
  /removeEventListener\('visibilitychange', this\._visibilityHandler\)/,
  'visibility listeners must be removed during renderer disposal');

console.log('renderer suspends all frame work while hidden/off-screen and resumes with a fresh delta');
