import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

function dataModule(source) {
  return `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
}

const bodyFormRosterURL = new URL(
  '../frontend/js/renderer/body-form-roster.js?cosmetics-renderer-test',
  import.meta.url,
).href;

class FakeColor3 {
  constructor(r, g, b) { this.r = r; this.g = g; this.b = b; }
  clone() { return new FakeColor3(this.r, this.g, this.b); }
  scale(value) { return new FakeColor3(this.r * value, this.g * value, this.b * value); }
  static White() { return new FakeColor3(1, 1, 1); }
}

class FakeMaterial {
  constructor(name) {
    this.name = name;
    this.diffuseColor = new FakeColor3(0.2, 0.2, 0.2);
    this.emissiveColor = new FakeColor3(0, 0, 0);
    this.specularColor = new FakeColor3(0, 0, 0);
    this.disposed = false;
  }
  clone(name) { return Object.assign(new FakeMaterial(name), {
    diffuseColor: this.diffuseColor.clone(), emissiveColor: this.emissiveColor.clone(), specularColor: this.specularColor.clone(),
  }); }
  freeze() {}
  unfreeze() {}
  dispose() { this.disposed = true; }
}

class FakeVector {
  constructor() { this.x = 0; this.y = 0; this.z = 0; }
  set(x, y, z) { this.x = x; this.y = y; this.z = z; }
}

class FakeNode {
  constructor(name) {
    this.name = name;
    this.position = new FakeVector();
    this.rotation = new FakeVector();
    this.scaling = new FakeVector();
    this.disposed = false;
    this.material = null;
  }
  isDisposed() { return this.disposed; }
  dispose() { this.disposed = true; }
}

const MeshBuilder = new Proxy({}, {
  get: (_, name) => name.startsWith('Create') ? (meshName => new FakeNode(meshName)) : undefined,
});

globalThis.window = {
  BABYLON: {Color3: FakeColor3, TransformNode: FakeNode, MeshBuilder},
  FakeMaterial,
};

const themeSource = readFileSync(new URL('../frontend/js/cosmetic-themes.js', import.meta.url), 'utf8');
vm.runInThisContext(themeSource, {filename: 'cosmetic-themes.js'});
window.ArenaCosmeticThemes = globalThis.ArenaCosmeticThemes;

let rendererSource = readFileSync(new URL('../frontend/js/renderer/cosmetics.js', import.meta.url), 'utf8');
rendererSource = rendererSource
  .replace(/import \{ isEnabled \} from '[^']+';\r?\n/, "const isEnabled = () => true;\n")
  .replace(/import \{ makeMat, parseColor \} from '[^']+';\r?\n/, `
    const parseColor = value => {
      const hex = String(value || '#000000').replace('#', '');
      return new window.BABYLON.Color3(parseInt(hex.slice(0,2),16)/255, parseInt(hex.slice(2,4),16)/255, parseInt(hex.slice(4,6),16)/255);
    };
    const makeMat = name => new window.FakeMaterial(name);
  `)
  .replace(
    /from '\.\/body-form-roster\.js[^']*';/,
    `from '${bodyFormRosterURL}';`,
  );
const renderer = await import(dataModule(rendererSource));

assert.equal(renderer.resolveCosmeticAsset('bot_skin', 'neon_grid').key, 'neon_grid', 'legacy visuals must stay supported');
assert.equal(renderer.resolveCosmeticAsset('weapon_skin', 'arena_set_003_ember_signal').kind, 'procedural');
assert.equal(renderer.resolveCosmeticAsset('attachment', 'arena_set_003_BAD').key, 'none', 'malformed keys must fall back safely');

const originalWeaponMaterial = new FakeMaterial('weapon-original');
const weaponMesh = new FakeNode('blade');
weaponMesh.material = originalWeaponMaterial;
const entry = {
  root: new FakeNode('bot-root'),
  weapon: {getChildMeshes: () => [weaponMesh]},
};
const bot = {
  bot_id: 'bot-1',
  avatar_color: '#22ccff',
  cosmetics: {
    bot_skin: 'arena_set_003_ember_signal',
    weapon_skin: 'arena_set_003_ember_signal',
    attachment: 'arena_set_003_ember_signal',
  },
};

renderer.applyBotCosmetics(entry, bot, {});
assert.match(entry._cosmeticSignature, /arena_set_003_ember_signal/);
assert.ok(entry._cosmeticState.groups.length >= 2, 'procedural skin and attachment should create bounded groups');
assert.ok(entry._cosmeticState.materials.length >= 2, 'procedural visuals should use disposable local materials');
assert.notEqual(weaponMesh.material, originalWeaponMaterial, 'procedural weapon finish should be visible');

const state = entry._cosmeticState;
renderer.disposeBotCosmetics(entry);
assert.equal(entry._cosmeticState, null);
assert.equal(weaponMesh.material, originalWeaponMaterial, 'cleanup must restore the shared weapon material');
assert.ok(state.groups.every(group => group.disposed), 'all cosmetic groups must be disposed');
assert.ok(state.materials.every(material => material.disposed), 'all cosmetic materials must be disposed');
assert.ok(state.weaponSwaps.every(swap => swap.clone.disposed), 'all cloned weapon materials must be disposed');

delete window.ArenaCosmeticThemes;
assert.equal(renderer.resolveCosmeticAsset('bot_skin', 'arena_set_004_void_orbit').key, 'standard', 'helper absence must fail closed to a legacy default');

console.log('renderer preserves legacy cosmetics, safely resolves procedural keys, and cleans up resources');
