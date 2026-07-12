import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const source = readFileSync(new URL('../frontend/js/renderer/projectiles.js', import.meta.url), 'utf8');
const engineSource = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
assert.doesNotMatch(source, /data:image\//, 'projectile trails must not depend on an embedded image decoder');
assert.match(engineSource, /projectiles\.js\?v=20260711a/, 'renderer entrypoint should invalidate the broken trail module cache');

const created = [];
class FakeDynamicTexture {
  constructor(name, size, scene, generateMipMaps, samplingMode) {
    Object.assign(this, {name, size, scene, generateMipMaps, samplingMode, disposed:false, stops:[]});
    created.push(this);
    this.context = {
      clearRect: (...args) => { this.clearArgs = args; },
      createRadialGradient: (...args) => ({
        addColorStop: (offset, color) => this.stops.push([offset, color]),
      }),
      set fillStyle(value) { this._fillStyle = value; },
      get fillStyle() { return this._fillStyle; },
      fillRect: (...args) => { this.fillArgs = args; },
    };
  }
  getContext() { return this.context; }
  update(invertY) { this.updated = true; this.invertY = invertY; }
  isDisposed() { return this.disposed; }
  dispose() { this.disposed = true; }
}

globalThis.window = {
  BABYLON: {
    DynamicTexture: FakeDynamicTexture,
    Texture: {BILINEAR_SAMPLINGMODE:7, CLAMP_ADDRESSMODE:0},
  },
};
const {ProjectileRenderer} = await import('../frontend/js/renderer/projectiles.js?procedural-trail-test');
const renderer = new ProjectileRenderer({id:'scene'});
const first = renderer._getTrailTexture();
assert.equal(first, renderer._getTrailTexture(), 'all projectiles should share one trail texture');
assert.equal(created.length, 1);
assert.equal(first.hasAlpha, true);
assert.equal(first.updated, true);
assert.equal(first.invertY, false);
assert.deepEqual(first.stops, [
  [0, 'rgba(255,255,255,1)'],
  [0.32, 'rgba(255,255,255,.9)'],
  [1, 'rgba(255,255,255,0)'],
]);
first.dispose();
assert.notEqual(renderer._getTrailTexture(), first, 'a disposed shared texture should be recreated');
assert.equal(created.length, 2);

console.log('projectile trails use one reusable procedural alpha texture');
