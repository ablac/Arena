import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const rig = readFileSync(new URL('../frontend/js/renderer/character-rig.js', import.meta.url), 'utf8');
const bots = readFileSync(new URL('../frontend/js/renderer/bots.js', import.meta.url), 'utf8');
const hudSource = readFileSync(new URL('../frontend/js/renderer/world-hud.js', import.meta.url), 'utf8');
const { healthBandForRatio, worldHudScaleForRadius } = await import('../frontend/js/renderer/world-hud.js');

assert.doesNotMatch(
  rig,
  /new GUI\.(?:TextBlock|Rectangle)\(`forge-(?:label|hp)/,
  'always-visible bot labels and health bars must not use fullscreen GUI controls',
);
assert.doesNotMatch(
  bots,
  /new GUI\.(?:TextBlock|Rectangle)\('taunt-/,
  'taunt bubbles must not dirty the fullscreen GUI texture while bots move',
);
assert.match(hudSource, /new B\.DynamicTexture\(/,
  'world labels must draw into small event-driven textures');
assert.match(hudSource, /B\.MeshBuilder\.CreatePlane\(/,
  'world labels and health bars must render as billboard planes');
assert.match(hudSource, /const HUD_RENDERING_GROUP\s*=\s*3[\s\S]*renderingGroupId\s*=\s*HUD_RENDERING_GROUP/,
  'bot HUD planes must render in the overlay group');
assert.match(hudSource, /isPickable\s*=\s*false/,
  'bot HUD planes must never intercept arena selection');
assert.match(hudSource, /nameLabel\.scaling\.y\s*=\s*-1[\s\S]*plane\.scaling\.y\s*=\s*-1/,
  'world label and taunt textures must be corrected for billboard orientation');

assert.equal(worldHudScaleForRadius(800), 1);
assert.equal(worldHudScaleForRadius(1600), 2);
assert.equal(healthBandForRatio(0.8), 'healthy');
assert.equal(healthBandForRatio(0.5), 'wounded');
assert.equal(healthBandForRatio(0.2), 'critical');

console.log('bot labels, HP bars, and taunts use bounded world-space HUD resources');
