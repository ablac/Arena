import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const moduleUrl = new URL('../frontend/js/safe-viewport.js', import.meta.url);
const { computeSafeViewport, MOBILE_SAFE_VIEWPORT_REGIONS } = await import(moduleUrl);
const { frameWorldTarget } = await import('../frontend/js/renderer/camera.js');

const canvas = { left: 100, top: 50, right: 1100, bottom: 650, width: 1000, height: 600 };

const clear = computeSafeViewport(canvas, []);
assert.deepEqual(
  { left: clear.left, top: clear.top, right: clear.right, bottom: clear.bottom },
  { left: 0, top: 0, right: 0, bottom: 0 },
  'an uncovered canvas must retain its full safe viewport',
);

const covered = computeSafeViewport(canvas, [
  { side: 'top', rect: { left: 120, top: 50, right: 1080, bottom: 130 } },
  { side: 'right', rect: { left: 980, top: 160, right: 1100, bottom: 570 } },
  { side: 'bottom', rect: { left: 100, top: 520, right: 1100, bottom: 650 } },
]);
assert.equal(covered.top, 92, 'top overlay plus breathing room should be excluded');
assert.equal(covered.right, 132, 'right overlay plus breathing room should be excluded');
assert.equal(covered.bottom, 142, 'bottom overlay plus breathing room should be excluded');
assert.equal(covered.focalOffsetX, -66, 'safe focal point should move away from a right overlay');
assert.equal(covered.focalOffsetY, -25, 'opposing vertical overlays should produce their net focal shift');

const squeezed = computeSafeViewport(canvas, [
  { side: 'left', rect: { left: 100, top: 50, right: 900, bottom: 650 } },
  { side: 'right', rect: { left: 300, top: 50, right: 1100, bottom: 650 } },
  { side: 'top', rect: { left: 100, top: 50, right: 1100, bottom: 590 } },
]);
assert.ok(squeezed.width >= 350, 'horizontal overlays must leave at least 35% of the canvas visible');
assert.ok(squeezed.height >= 228, 'vertical overlays must leave at least 38% of the canvas visible');

const minimapRegion = MOBILE_SAFE_VIEWPORT_REGIONS.find(region => region.selector === '#minimap-box');
assert.equal(minimapRegion?.side, 'left', 'the mobile minimap is anchored on the left edge');
const mobileMinimap = computeSafeViewport(
  { left: 0, top: 0, right: 390, bottom: 844, width: 390, height: 844 },
  [{ side: minimapRegion.side, rect: { left: 10, top: 76, right: 160, bottom: 226 } }],
);
assert.equal(mobileMinimap.left, 172);
assert.equal(mobileMinimap.right, 0);
assert.ok(mobileMinimap.focalOffsetX > 0,
  'opening the left minimap must move the camera focal point right, away from it');

const rightFramed = frameWorldTarget(1000, 1000, 100, 0, 500, -Math.PI / 2);
assert.ok(rightFramed.x < 1000,
  'at the authored camera alpha, rightward screen framing moves the target toward world -X');
const leftFramed = frameWorldTarget(1000, 1000, -100, 0, 500, -Math.PI / 2);
assert.ok(leftFramed.x > 1000,
  'leftward screen framing must invert symmetrically');

const cameraSource = readFileSync(new URL('../frontend/js/renderer/camera.js', import.meta.url), 'utf8');
assert.match(cameraSource, /export function frameWorldTarget/);
assert.match(cameraSource, /setSafeViewport\(viewport\)/);
assert.match(cameraSource, /this\._framedTarget\(bot\.position\[0\], bot\.position\[1\]\)/);

const engineSource = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
assert.match(engineSource, /this\._safeViewport/);
assert.match(engineSource, /this\.camera\.setSafeViewport\(this\._safeViewport\)/);

const safeViewportSource = readFileSync(moduleUrl, 'utf8');
assert.match(safeViewportSource, /addEventListener\('transitionend', schedule\)/,
  'transform-driven sheets and menus must remeasure after their transition settles');
assert.match(safeViewportSource, /removeEventListener\('transitionend', schedule\)/,
  'safe viewport teardown must remove transition listeners');

for (const path of ['../frontend/js/app.js', '../frontend/m/mobile.js']) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8');
  assert.match(source, /observeArenaSafeViewport/);
  assert.match(source, /setSafeViewport/);
}

console.log('safe viewport regression checks passed');
