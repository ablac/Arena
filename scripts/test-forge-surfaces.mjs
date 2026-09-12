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
const {forgeSurface, applyForgeSurface, syncForgeSurface, inheritForgeSurface} = await import('../frontend/js/renderer/forge-surfaces.js');
const {setEffect, isEnabled} = await import('../frontend/js/settings.js');
assert.equal(isEnabled('rendering', 'surfaceDetail'), true, 'Surface detail ships enabled');
const scene = {onDisposeObservable: {addOnce: callback => { scene.dispose = callback; }}};
const graphite = forgeSurface(scene, 'graphite');
assert.equal(graphite, forgeSurface(scene, 'graphite'), 'Bots share one texture per surface and scene');
assert.equal(textures.length, 1);
assert.equal(graphite.size, 128);
assert.equal(graphite.uploads, 1, 'Surface uploads once, with no frame updates');
const secondScene = {};
assert.deepEqual(forgeSurface(secondScene, 'graphite').pixels, graphite.pixels, 'Surface is deterministic across scenes');
assert.notDeepEqual(forgeSurface(scene, 'steel').pixels, graphite.pixels);
const material = {isFrozen: false, unfreeze() { this.isFrozen = false; }, freeze() { this.isFrozen = true; },
  disableLighting: true, emissiveColor: {r: 0.18, g: 0.24, b: 0.36}};
applyForgeSurface(material, scene, 'graphite');
const staticMaterial = {...material, isFrozen: true};
applyForgeSurface(staticMaterial, scene, 'steel');
assert.equal(material.diffuseTexture, graphite);
assert.ok(material.emissiveTexture === null, 'Grayscale detail must not add white emission');
assert.deepEqual(material.emissiveColor, {r: 0.18, g: 0.24, b: 0.36}, 'Authored dark-sector color stays intact');
material.disableLighting = false;
syncForgeSurface(material);
assert.ok(material.diffuseTexture === graphite, 'Lit mode keeps diffuse grain');
assert.ok(material.emissiveTexture === null, 'Lit mode must not add white emission');
setEffect('rendering', 'surfaceDetail', false);
assert.equal(material.isFrozen, false, 'A settings toggle must keep status-feedback materials mutable');
assert.equal(staticMaterial.isFrozen, true, 'Shared static surfaces retain their frozen render optimization');
assert.ok(material.diffuseTexture === null, 'Detail disables diffuse map');
assert.ok(material.emissiveTexture === null, 'Detail disables emissive map');
setEffect('rendering', 'surfaceDetail', true);
assert.equal(material.diffuseTexture, graphite, 'Live toggle restores shared texture');
// A cloned material may own duplicate DynamicTextures. Dispose duplicates
// once and ensure frequently replaced cosmetics leave the subscriber set.
let orphanDisposals = 0;
const orphan = {dispose() { orphanDisposals++; }};
let cloneUpdates = 0;
const clone = {
  diffuseTexture: orphan, emissiveTexture: orphan,
  unfreeze() { cloneUpdates++; }, freeze() {},
  onDisposeObservable: {addOnce(callback) { clone.dispose = callback; }},
};
inheritForgeSurface(clone, material, scene);
assert.equal(orphanDisposals, 1);
assert.ok(clone.diffuseTexture === graphite);
assert.ok(clone.emissiveTexture === null);
setEffect('rendering', 'surfaceDetail', false);
assert.ok(clone.diffuseTexture === null, 'Clones follow live detail setting');
setEffect('rendering', 'surfaceDetail', true);
assert.ok(clone.diffuseTexture === graphite);
clone.dispose();
const updateCountAtDisposal = cloneUpdates;
setEffect('rendering', 'surfaceDetail', false);
assert.equal(cloneUpdates, updateCountAtDisposal, 'Disposed clones leave settings subscriber set');
setEffect('rendering', 'surfaceDetail', true);
let sharedDisposals = 0;
graphite.dispose = () => { sharedDisposals++; };
const sharingClone = {diffuseTexture: graphite, emissiveTexture: graphite, unfreeze() {}, freeze() {}};
inheritForgeSurface(sharingClone, material, scene);
assert.equal(sharedDisposals, 0, 'Inherited maps never dispose original/shared texture');
scene.dispose();
setEffect('rendering', 'surfaceDetail', false);
assert.equal(material.diffuseTexture, graphite, 'Scene disposal removes settings subscriber');
setEffect('rendering', 'surfaceDetail', true);
console.log('Forge textures are deterministic, scene-shared, bounded and live-toggleable');
