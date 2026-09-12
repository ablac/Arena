import assert from 'node:assert/strict';
import { chooseCombatFrame, combatFrameRadius } from '../frontend/js/renderer/camera-framing.js';
import { CameraController, frameWorldTarget } from '../frontend/js/renderer/camera.js';
import { ARENA_GRADE, applyArenaGrade } from '../frontend/js/renderer/scene-look.js';

const bot = (id, x, z, extra = {}) => ({ bot_id: id, position: [x, z], is_alive: true, ...extra });
const field = [bot('a', 100, 100), bot('b', 120, 100), bot('c', 1900, 1900), bot('d', 1940, 1900)];
let focus = chooseCombatFrame(field, null, 0);
assert.deepEqual(focus.ids, ['a', 'b']);
assert.equal(focus.x, 110, 'watch the actual nearby fight, not empty map-center');
assert.equal(focus.z, 100);
assert.deepEqual(chooseCombatFrame([...field].reverse(), null, 0), focus, 'input reordering cannot switch the shot');
const switched = [bot('a', 100, 100), bot('b', 180, 100), bot('c', 1900, 1900), bot('d', 1910, 1900)];
assert.deepEqual(chooseCombatFrame(switched, focus, 2).ids, ['a', 'b'], 'hold the shot for the minimum dwell');
const smallDifference = [bot('a', 100, 100), bot('b', 120, 100), bot('c', 1900, 1900), bot('d', 1918, 1900)];
assert.deepEqual(chooseCombatFrame(smallDifference, focus, 4).ids, ['a', 'b'], 'small distance changes do not cause flicker');
assert.deepEqual(chooseCombatFrame(switched, focus, 4).ids, ['c', 'd'], 'change to substantially closer action after dwell');
assert.deepEqual(chooseCombatFrame([bot('a', 100, 100, { is_alive: false }), ...switched.slice(1)], focus, 1).ids,
  ['c', 'd'], 'a dead subject releases the shot immediately');
assert.equal(chooseCombatFrame([null, bot('bad', NaN, 2), bot('dead', 1, 2, { is_alive: false })]), null);
assert.equal(chooseCombatFrame([bot('solo', 50, 60)]).x, 50);
assert.deepEqual(chooseCombatFrame([bot('a', 0, 0, {team: 1}), bot('b', 1, 0, {team: 1}),
  bot('enemy', 30, 0, {team: 2})]).ids, ['b', 'enemy'], 'team play frames opponents');

const wide = { width: 1000, height: 600, canvasHeight: 800 };
const narrow = { width: 260, height: 600, canvasHeight: 800 };
assert.ok(combatFrameRadius(180, narrow) > combatFrameRadius(180, wide), 'narrow phone layout needs more framing room');
assert.ok(combatFrameRadius(300, wide) > combatFrameRadius(30, wide));
for (const span of [0, 20, 5000, NaN, Infinity]) {
  assert.ok(Number.isFinite(combatFrameRadius(span, wide, NaN)));
}
// At a vertical field of view of 90 degrees and 1000px canvas height, a
// 500-unit radius makes one horizontal pixel equal one ground unit.
const projected = frameWorldTarget(1000, 1000, 100, 0, 500, -Math.PI / 2,
  {canvasHeight: 1000, fov: Math.PI / 2, beta: Math.PI / 4});
assert.ok(Math.abs(projected.x - 900) < 1e-8);
assert.ok(Math.abs(projected.z - 1000) < 1e-8);
const taller = frameWorldTarget(1000, 1000, 100, 0, 500, -Math.PI / 2,
  {canvasHeight: 2000, fov: Math.PI / 2, beta: Math.PI / 4});
assert.ok(Math.abs(taller.x - 950) < 1e-8, 'viewport height controls world units per pixel');
// Independent forward projection: express the subject in the camera's basis,
// then apply the perspective divide. This checks the inverse ground-ray fit
// for vertical offsets and camera orbits, including views below the deck.
for (const alpha of [-Math.PI / 2, 0.3, 2]) {
  for (const beta of [0.3, 0.8, 1.2, 2.1]) {
    const radius = 900, height = 812, fov = 0.8, ox = -43, oy = -12;
    const target = frameWorldTarget(500, 700, ox, oy, radius, alpha, {canvasHeight:height, fov, beta});
    const ca = Math.cos(alpha), sa = Math.sin(alpha), cb = Math.cos(beta), sb = Math.sin(beta);
    const subject = [500 - target.x - radius * ca * sb, -radius * cb, 700 - target.z - radius * sa * sb];
    const dot = (a, b) => a.reduce((sum, value, i) => sum + value * b[i], 0);
    const depth = dot(subject, [-ca * sb, -cb, -sa * sb]);
    const right = dot(subject, [-sa, 0, ca]);
    const up = dot(subject, [-ca * cb, sb, -sa * cb]);
    const pixelsPerUnit = height / (2 * Math.tan(fov / 2) * depth);
    assert.ok(Math.abs(right * pixelsPerUnit - ox) < 1e-8);
    assert.ok(Math.abs(-up * pixelsPerUnit - oy) < 1e-8);
  }
}
assert.deepEqual(frameWorldTarget(500, 700, 50, 0, 900, 0,
  {canvasHeight:812, fov:0.8, beta:Math.PI / 2}), {x:500, z:700}, 'horizon view stays finite and centered');

// Exercise the public controller and actual input handlers with only the
// browser/3D host substituted. Focus policy itself is imported above.
const handlers = new Map();
const canvas = { addEventListener(name, fn) { handlers.set(name, fn); }, removeEventListener() {}, setPointerCapture() {} };
class Vector3 { constructor(x, y, z) { Object.assign(this, {x,y,z}); } }
class Camera { constructor(name, alpha, beta, radius, target) { Object.assign(this, {name,alpha,beta,radius,target}); this.fov = 0.8; } }
globalThis.window = { BABYLON: {Vector3, ArcRotateCamera: Camera}, matchMedia: () => ({matches:false}),
  addEventListener() {}, removeEventListener() {} };
const scene = { registerBeforeRender() {}, getEngine: () => ({getDeltaTime: () => 16}) };
const controller = new CameraController(scene, canvas, 2000, 2000);
controller.updateBotPositions(field);
for (let i = 0; i < 90; i++) controller._tick();
assert.ok(controller.camera.radius < 500, 'automatic framing brings the selected contest into view');
controller.setZoom(4);
controller._tick();
assert.equal(controller.camera.radius, 200, 'manual zoom is not overwritten by automatic framing');
controller.followBot('a');
handlers.get('pointerdown')({pointerId:1, pointerType:'mouse', button:0, clientX:10, clientY:10});
assert.equal(controller.followId, null, 'manual orbit immediately releases follow');
assert.equal(controller.autoPan, false);
const state = controller.getNavigationState();
const replacement = new CameraController(scene, canvas, 1800, 1800);
replacement.restoreNavigationState(state);
assert.deepEqual(replacement.getNavigationState(), state, 'scene rebuild preserves deliberate camera state');
replacement.setZoom(NaN);
assert.equal(replacement.zoom, state.zoom);
controller.dispose(); replacement.dispose();
window.matchMedia = () => ({matches:true});
const reduced = new CameraController(scene, canvas, 2000, 2000);
reduced.updateBotPositions(field);
reduced._tick();
assert.equal(reduced.autoPan, false, 'reduced motion starts with a stationary camera');
assert.equal(reduced.camera.radius, 800);
reduced.dispose();
delete globalThis.window;

const ip = {exposure:0.5,contrast:2,vignetteWeight:4,vignetteColor:{r:1,g:0,b:0}};
applyArenaGrade(ip);
assert.equal(ip.exposure, ARENA_GRADE.exposure);
assert.equal(ip.contrast, ARENA_GRADE.contrast);
assert.equal(ip.vignetteWeight, ARENA_GRADE.vignetteWeight);
assert.ok(ip.contrast < 1.1 && ip.exposure > 1, 'the authored base reveals shaded material detail');
console.log('camera framing, manual control and scene grade checks passed');
