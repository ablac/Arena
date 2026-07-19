import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const moduleUrl = new URL('../frontend/js/safe-viewport.js', import.meta.url);
const { computeSafeViewport } = await import(moduleUrl);

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

const cameraSource = readFileSync(new URL('../frontend/js/renderer/camera.js', import.meta.url), 'utf8');
assert.match(cameraSource, /export function frameWorldTarget/);
assert.match(cameraSource, /setSafeViewport\(viewport\)/);
assert.match(cameraSource, /this\._framedTarget\(bot\.position\[0\], bot\.position\[1\]\)/);

const engineSource = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
assert.match(engineSource, /this\._safeViewport/);
assert.match(engineSource, /this\.camera\.setSafeViewport\(this\._safeViewport\)/);

for (const path of ['../frontend/js/app.js', '../frontend/m/mobile.js']) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8');
  assert.match(source, /observeArenaSafeViewport/);
  assert.match(source, /setSafeViewport/);
}

console.log('safe viewport regression checks passed');
