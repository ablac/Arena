import assert from 'node:assert/strict';

class FakeColor3 {
  constructor(r, g, b) {
    this.r = r;
    this.g = g;
    this.b = b;
  }

  clone() { return new FakeColor3(this.r, this.g, this.b); }
  scale(value) { return new FakeColor3(this.r * value, this.g * value, this.b * value); }
  static Black() { return new FakeColor3(0, 0, 0); }
}

class FakeScene {
  constructor(name) {
    this.name = name;
    this.isDisposed = false;
    this.materials = [];
  }

  dispose() {
    this.isDisposed = true;
    for (const material of this.materials) material.dispose();
  }
}

class FakeMaterial {
  constructor(name, scene) {
    this.name = name;
    this.scene = scene;
    this._isDisposed = false;
    this.isFrozen = false;
    scene.materials.push(this);
  }

  getScene() { return this.scene; }
  freeze() { this.isFrozen = true; }
  isDisposed() { return this._isDisposed; }
  dispose() { this._isDisposed = true; }
}

globalThis.window = {
  BABYLON: {
    Color3: FakeColor3,
    StandardMaterial: FakeMaterial,
  },
};

const { getSwordsmanMaterials } = await import(
  new URL('../frontend/js/renderer/swordsman-materials.js?lifecycle-test', import.meta.url)
);

const slots = ['blade', 'guard', 'grip', 'pommel'];

function assertRenderable(materials, scene, label) {
  assert.deepEqual(Object.keys(materials).sort(), [...slots].sort(), `${label}: complete material set`);
  for (const slot of slots) {
    const material = materials[slot];
    assert.equal(material.getScene(), scene, `${label}: ${slot} belongs to current scene`);
    assert.equal(material.isDisposed(), false, `${label}: ${slot} is live`);
    assert.equal(material.isFrozen, true, `${label}: ${slot} is frozen after creation`);
  }
}

const sceneA = new FakeScene('scene-a');
const first = getSwordsmanMaterials(sceneA);
assertRenderable(first, sceneA, 'initial scene');
assert.equal(getSwordsmanMaterials(sceneA), first, 'same live scene reuses one shared set');

const completeFirst = { ...first };
delete first.guard;
const completed = getSwordsmanMaterials(sceneA);
assert.notEqual(completed, first, 'one missing member replaces the cache object');
assertRenderable(completed, sceneA, 'missing member repair');
for (const slot of slots) {
  assert.notEqual(completed[slot], completeFirst[slot], `missing member repair recreates ${slot} atomically`);
}

// A partially disposed set must be replaced as one unit, not mixed with the
// three surviving objects.
completed.blade.dispose();
const repaired = getSwordsmanMaterials(sceneA);
assert.notEqual(repaired, completed, 'one disposed member replaces the cache object');
assertRenderable(repaired, sceneA, 'partially disposed set repair');
for (const slot of slots) {
  assert.notEqual(repaired[slot], completed[slot], `disposed member repair recreates ${slot} atomically`);
}

// This mirrors ArenaEngine._rebuildForArenaSize: disposing scene A disposes
// its registered materials before sword meshes are built in scene B.
sceneA.dispose();
const sceneB = new FakeScene('scene-b');
const second = getSwordsmanMaterials(sceneB);
assertRenderable(second, sceneB, 'first rebuild');
for (const slot of slots) {
  assert.notEqual(second[slot], repaired[slot], `first rebuild replaces stale ${slot}`);
}

sceneB.dispose();
const sceneC = new FakeScene('scene-c');
const third = getSwordsmanMaterials(sceneC);
assertRenderable(third, sceneC, 'second rebuild');
for (const slot of slots) {
  assert.notEqual(third[slot], second[slot], `second rebuild replaces stale ${slot}`);
}

console.log('swordsman materials remain scene-owned across two dispose/rebuild cycles');
