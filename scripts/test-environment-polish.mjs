import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { deckRandom } from '../frontend/js/renderer/glass-deck.js';

// Exercise the actual floor creation/repaint methods with recording canvas and
// material hosts. GPU appearance is verified separately by the browser pass.
const source = readFileSync(new URL('../frontend/js/renderer/environment.js', import.meta.url), 'utf8');
const methods = ['_paintFloor', '_createFloor'].map(name => {
  const match = source.match(new RegExp(`  ${name}\\(\\) \\{[\\s\\S]*?\\n  \\}`));
  assert.ok(match, `find production ${name}`);
  return match[0];
}).join('\n');
const log = [];
function context() {
  return new Proxy({
    createRadialGradient(...args) {
      log.push(['gradient', ...args]);
      return { addColorStop(...stop) { log.push(['stop', ...stop]); } };
    },
    clearRect(...args) { log.push(['clear', ...args]); },
    fillRect(...args) { log.push(['fill', this.fillStyle, ...args]); },
    drawImage() { log.push(['upload']); },
    beginPath() {}, moveTo() {}, lineTo() {}, stroke() {}, strokeRect() {},
    closePath() {}, fill() {},
  }, { set(target, key, value) { target[key] = value; return true; } });
}
class Color3 {
  constructor(...channels) { [this.r, this.g, this.b] = channels; }
}
const materials = [];
class StandardMaterial {
  constructor(name) { this.name = name; materials.push(this); }
}
class DynamicTexture {
  constructor() { this.ctx = context(); this.updates = 0; }
  getContext() { return this.ctx; }
  update() { this.updates++; }
}
class ShaderMaterial extends StandardMaterial {
  setFloat(key, value) { this[key] = value; }
  setColor3(key, value) { this[key] = value; }
}
const meshes = [];
const B = { Color3, StandardMaterial, DynamicTexture, ShaderMaterial,
  Effect: { ShadersStore: {} }, Engine: { ALPHA_ADD: 1 },
  MeshBuilder: { CreateGround(name) {
    const mesh = { name, position: { set() {} }, setEnabled(value) { this.enabled = value; } };
    meshes.push(mesh); return mesh;
  } },
};
const enabled = { floorEnergyGlow: true, contactShadows: true };
const callbacks = [];
const palette = { floorBase: [[10, 20, 38], [6, 12, 22]], wallTrim: [0.2, 0.5, 1], swirl: [0.1, 0.3, 0.6] };
const sandbox = { window: { BABYLON: B },
  document: { createElement: () => ({ getContext: context }) },
  performance: { now: () => 1000 }, deckRandom,
  isEnabled: (_, effect) => enabled[effect],
  GlassDeck: class {}, createGlassPolish: () => ({}), markStaticMesh() {},
};
const Floor = vm.runInNewContext(`(class { ${methods} })`, sandbox);
const floor = new Floor();
Object.assign(floor, { w: 1000, h: 800, scene: { registerBeforeRender: cb => callbacks.push(cb) }, getPalette: () => palette });
floor._createFloor();
assert.equal(meshes.length, 2, 'base and optional overlay only');
assert.ok(floor._groundMat.alpha >= 0.35 && floor._groundMat.alpha <= 0.55,
  'glass needs a readable yet translucent base');
assert.equal(floor._groundMat.emissiveTexture, null, 'finish cannot become a white additive emitter');
assert.ok(floor._groundMat.specularPower >= 48 && floor._groundMat.specularPower <= 128,
  'glass uses broad polished highlights');
assert.equal(floor._groundMat.diffuseTexture, floor._floorTex);
assert.equal(callbacks.length, 1, 'no extra per-frame work for static polish');
callbacks[0]();
assert.equal(floor._floorGlow.enabled, true);
enabled.floorEnergyGlow = false;
callbacks[0]();
assert.equal(floor._floorGlow.enabled, false);
assert.equal(floor._ground.material, floor._groundMat, 'ambience off retains the structural glass');
enabled.floorEnergyGlow = true;
callbacks[0]();
assert.equal(floor._floorGlow.enabled, true);

log.length = 0;
floor._paintFloor();
const first = JSON.stringify(log);
log.length = 0;
floor._paintFloor();
assert.equal(JSON.stringify(log), first, 'repaints cannot accumulate tint or drift');
assert.equal(log[0][0], 'clear');
assert.deepEqual(log.slice(-2).map(item => item[0]), ['clear', 'upload'], 'clear destination before every upload');
assert.ok(log.filter(item => item[0] === 'gradient').length <= 3, 'bounded static reflection bake');
assert.ok(log.some(item => item[0] === 'stop' && /^rgb\(/.test(item[2])), 'stable base tint is explicit');
const ownedTexture = floor._floorTex;
floor._roundObstacles = [{ x: 120, y: 80, width: 60, height: 40 }];
floor._paintFloor();
floor._roundObstacles = [];
floor._paintFloor();
assert.equal(floor._floorTex, ownedTexture, 'map changes reuse the owned texture');
assert.equal(materials.length, 2, 'round paints must not allocate materials');
console.log('Environment polish: transparent base, stable paint, live glow toggle, bounded resources pass.');
