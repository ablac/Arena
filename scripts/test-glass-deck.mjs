import assert from 'node:assert/strict';
import { glassDeckParts, deckRandom, GlassDeck } from '../frontend/js/renderer/glass-deck.js';

// Geometry cannot intrude into gameplay, even after non-integer arena resizing.
for (const [width, depth] of [[1000, 1000], [2111.111111, 2111.111111], [400, 900]]) {
  const parts = glassDeckParts(width, depth);
  assert.equal(parts.length, 28, 'retain the authored open-deck geometry');
  assert.equal(parts.filter(p => p.kind === 'glass').length, 4);
  for (const part of parts) {
    assert.ok(part.y + part.height / 2 <= 0, 'all structure must remain below the playable plane');
    assert.ok(part.width > 0 && part.height > 0 && part.depth > 0);
    // A transparent viewing aperture must remain at the arena center.
    assert.ok(Math.abs(part.x - width / 2) >= part.width / 2 ||
      Math.abs(part.z - depth / 2) >= part.depth / 2,
    'no part may form an opaque bottom plate below the center');
  }
}
const a = deckRandom(), b = deckRandom();
for (let i = 0; i < 2000; i++) {
  const value = a();
  assert.equal(value, b(), 'repainting the floor must keep the same texture');
  assert.ok(value >= 0 && value < 1);
}

// Exercise ownership and teardown without requiring WebGL.
class Color3 {
  constructor(r, g, b) { this.set(r, g, b); }
  set(r, g, b) { this.r = r; this.g = g; this.b = b; }
  scale(v) { return new Color3(this.r * v, this.g * v, this.b * v); }
  static Black() { return new Color3(0, 0, 0); }
}
const resources = [];
class Resource {
  constructor() { resources.push(this); this.disposed = 0; }
  dispose() { this.disposed++; }
  freeze() {}
  unfreeze() {}
}
globalThis.window = { BABYLON: {
  Color3,
  StandardMaterial: Resource,
  Mesh: { MergeMeshes(source, disposeSource) {
    assert.equal(disposeSource, true, 'temporary source meshes must be released');
    assert.ok(source.every(mesh => mesh.material === source[0].material),
      'each batch must share one material to avoid hidden submesh draws');
    for (const mesh of source) mesh.dispose();
    const merged = new Resource();
    merged.freezeWorldMatrix = () => {};
    return merged;
  } },
  MeshBuilder: { CreateBox() {
    const mesh = new Resource();
    mesh.position = { set() {} };
    mesh.freezeWorldMatrix = () => {};
    return mesh;
  } },
} };
const deck = new GlassDeck({}, 1000, 1000, [0.24, 0.72, 1]);
assert.equal(deck.meshes.length, 3, 'structure uses one render submission per material');
assert.deepEqual(deck.meshes.map(mesh => mesh.name), ['deck-glass', 'deck-metal', 'deck-trim']);
assert.ok(deck.meshes.every(m => m.isPickable === false), 'decoration cannot steal input');
deck.setAccent([1, 0.8, 0.3]);
assert.equal(deck.trim.emissiveColor.r, 0.55);
deck.dispose();
deck.dispose();
assert.ok(resources.every(r => r.disposed === 1), 'every owned material and mesh disposed exactly once');
delete window.BABYLON.Mesh;
const fallback = new GlassDeck({}, 1000, 1000, [0.24, 0.72, 1]);
assert.equal(fallback.meshes.length, 28, 'minimal runtimes retain all source geometry');
fallback.dispose();
assert.ok(resources.every(r => r.disposed === 1), 'fallback owns and disposes its sources');
console.log('Glass deck: below-surface geometry, open aperture, stable polish, bounded resources, disposal pass.');
