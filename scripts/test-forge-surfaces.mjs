import assert from 'node:assert/strict';

const textures = [];
class Texture {
  constructor(name, size, scene, mipmaps) {
    Object.assign(this, {name, size, scene, mipmaps});
    textures.push(this);
  }
  getContext() {
    return {
      createImageData: (w, h) => ({data: new Uint8ClampedArray(w * h * 4)}),
      putImageData: pixels => { this.pixels = pixels.data; },
    };
  }
  update() { this.uploads = (this.uploads || 0) + 1; }
}
globalThis.window = {BABYLON: {DynamicTexture: Texture, Texture: {WRAP_ADDRESSMODE: 1}}};
const {forgeSurface, applyForgeSurface} = await import('../frontend/js/renderer/forge-surfaces.js');
const {setEffect, getState} = await import('../frontend/js/settings.js');
// The coordinator adds the schema entry during integration.
getState().rendering.effects.surfaceDetail = true;
const scene = {onDisposeObservable: {addOnce: callback => { scene.dispose = callback; }}};
const graphite = forgeSurface(scene, 'graphite');
assert.equal(graphite, forgeSurface(scene, 'graphite'), 'Bots share one texture per surface and scene');
assert.equal(textures.length, 1);
assert.equal(graphite.size, 128);
assert.equal(graphite.uploads, 1, 'Surface uploads once, with no frame updates');
const secondScene = {};
assert.deepEqual(forgeSurface(secondScene, 'graphite').pixels, graphite.pixels, 'Surface is deterministic across scenes');
assert.notDeepEqual(forgeSurface(scene, 'steel').pixels, graphite.pixels);
const material = {unfreeze() {}, freeze() {}};
applyForgeSurface(material, scene, 'graphite');
assert.equal(material.diffuseTexture, graphite);
assert.equal(material.emissiveTexture, graphite, 'Default unlit chassis still receive surface detail');
setEffect('rendering', 'surfaceDetail', false);
assert.ok(material.diffuseTexture === null, 'Detail disables diffuse map');
assert.ok(material.emissiveTexture === null, 'Detail disables emissive map');
setEffect('rendering', 'surfaceDetail', true);
assert.equal(material.diffuseTexture, graphite, 'Live toggle restores shared texture');
scene.dispose();
setEffect('rendering', 'surfaceDetail', false);
assert.equal(material.diffuseTexture, graphite, 'Scene disposal removes settings subscriber');
setEffect('rendering', 'surfaceDetail', true);
console.log('Forge textures are deterministic, scene-shared, bounded and live-toggleable');
