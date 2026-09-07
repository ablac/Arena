import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

function dataModule(source) {
  return `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
}

const bodyFormRosterURL = new URL(
  '../frontend/js/renderer/body-form-roster.js?cosmetics-forge-mounts-test',
  import.meta.url,
).href;

class FakeColor3 {
  constructor(r = 0, g = 0, b = 0) { Object.assign(this, {r, g, b}); }
  clone() { return new FakeColor3(this.r, this.g, this.b); }
  scale(value) { return new FakeColor3(this.r * value, this.g * value, this.b * value); }
  static White() { return new FakeColor3(1, 1, 1); }
}

class FakeMaterial {
  constructor(name) {
    this.name = name;
    this.diffuseColor = new FakeColor3(0.2, 0.2, 0.2);
    this.emissiveColor = new FakeColor3();
    this.specularColor = new FakeColor3();
    this.disposed = false;
  }
  clone(name) {
    const clone = new FakeMaterial(name);
    clone.diffuseColor = this.diffuseColor.clone();
    clone.emissiveColor = this.emissiveColor.clone();
    clone.specularColor = this.specularColor.clone();
    return clone;
  }
  freeze() {}
  unfreeze() {}
  dispose() { this.disposed = true; }
}

class FakeVector3 {
  constructor(x = 0, y = 0, z = 0) { Object.assign(this, {x, y, z}); }
  set(x, y, z) { Object.assign(this, {x, y, z}); return this; }
}

const nodes = [];
class FakeNode {
  constructor(name) {
    this.name = name;
    this.position = new FakeVector3();
    this.rotation = new FakeVector3();
    this.scaling = new FakeVector3(1, 1, 1);
    this.parent = null;
    this.material = null;
    this.disposed = false;
    nodes.push(this);
  }
  isDisposed() { return this.disposed; }
  dispose() {
    this.disposed = true;
    for (const child of nodes.filter(node => node.parent === this)) child.dispose();
  }
  setEnabled(enabled) { this.enabled = enabled; }
}

// Keep real mech geometry active: a box-only mock would miss malformed
// authored vertices while still satisfying every semantic mount assertion.
class FakeVertexData {
  applyToMesh(mesh) {
    mesh.vertexData = this;
    const positions = this.positions;
    const extent = axis => {
      const values = positions.filter((_, index) => index % 3 === axis);
      return Math.max(...values) - Math.min(...values);
    };
    mesh.geometryOptions = {width: extent(0), height: extent(1), depth: extent(2)};
  }
}

const MeshBuilder = new Proxy({}, {
  get: (_, method) => method.startsWith('Create')
    ? ((name, options) => {
        const node = new FakeNode(name);
        node.geometryOptions = options;
        return node;
      })
    : undefined,
});

function proceduralTheme(key) {
  const [, slot, kind = 'core'] = /^test-(skin|attachment|weapon)-(.+)$/.exec(key) || [];
  if (!slot) return null;
  return {
    key,
    palette: {primary: '#44ccff', secondary: '#ffd166', accent: '#dd88ff'},
    skin: {pattern: slot === 'skin' ? kind : 'core', layers: 2, angle: 0.08},
    weapon: {finish: 'ion', emissive: 0.7},
    attachment: {kind: slot === 'attachment' ? kind : 'reactor', variant: 1},
  };
}

globalThis.window = {
  BABYLON: {Color3: FakeColor3, TransformNode: FakeNode, Mesh: FakeNode, VertexData: FakeVertexData, MeshBuilder},
  ArenaCosmeticThemes: {themeFor: proceduralTheme},
  FakeMaterial,
};

let source = readFileSync(new URL('../frontend/js/renderer/cosmetics.js', import.meta.url), 'utf8');
source = source
  .replace("from './mech-geometry.js';",
    `from '${new URL('../frontend/js/renderer/mech-geometry.js', import.meta.url).href}';`)
  .replace("from './forge-surfaces.js';",
    `from '${new URL('../frontend/js/renderer/forge-surfaces.js', import.meta.url).href}';`)
  .replace(/import \{ isEnabled \} from '[^']+';\r?\n/, 'const isEnabled = () => true;\n')
  .replace(/import \{ makeMat, parseColor \} from '[^']+';\r?\n/, `
    const parseColor = value => {
      const hex = String(value || '#000000').replace('#', '');
      return new window.BABYLON.Color3(
        parseInt(hex.slice(0, 2), 16) / 255,
        parseInt(hex.slice(2, 4), 16) / 255,
        parseInt(hex.slice(4, 6), 16) / 255,
      );
    };
    const makeMat = name => new window.FakeMaterial(name);
  `)
  .replace(
    /from '\.\/body-form-roster\.js[^']*';/,
    `from '${bodyFormRosterURL}';`,
  );
const cosmetics = await import(dataModule(source));

function disposeChecked(entry) {
  const state = entry._cosmeticState;
  const belongsToState = node => {
    for (let parent = node.parent; parent; parent = parent.parent) {
      if (state.groups.includes(parent)) return true;
    }
    return false;
  };
  const meshes = nodes.filter(node => !node.disposed && node.vertexData && belongsToState(node));
  assert.ok(state.groups.length <= 6, 'one cosmetic loadout must retain a bounded mount group count');
  assert.ok(state.materials.length <= 9, 'one cosmetic loadout must share a bounded material palette');
  assert.ok(meshes.length <= 32, 'cosmetic detail must stay within the per-bot mesh budget');
  for (const mesh of meshes) {
    const {positions, normals, indices, uvs} = mesh.vertexData;
    assert.ok(positions.length >= 24 && positions.every(Number.isFinite), `${mesh.name} needs finite authored vertices`);
    assert.equal(normals.length, positions.length, `${mesh.name} needs one normal per vertex`);
    assert.ok(normals.every(Number.isFinite));
    assert.equal(uvs.length, positions.length / 3 * 2, `${mesh.name} needs complete surface UVs`);
    assert.ok(indices.every(index => Number.isInteger(index) && index >= 0 && index < positions.length / 3));
  }
  cosmetics.disposeBotCosmetics(entry);
  assert.ok(meshes.every(mesh => mesh.disposed), 'disposing cosmetic groups must dispose all authored child meshes');
  assert.ok(state.materials.every(material => material.disposed), 'all owned cosmetic materials must be disposed');
  assert.ok(state.weaponSwaps.every(swap => swap.clone.disposed), 'weapon finish clones must be disposed');
}

const METRICS = {
  daggers: {
    bodyY: 9.15, torsoWidth: 5.74, torsoHeight: 6.36, torsoDepth: 3.68,
    shoulderX: 2.98, shoulderY: 6.08, headWidth: 3.74, headHeight: 3.05,
    headDepth: 3.14, headY: 9.19,
  },
  shield: {
    bodyY: 9.86, torsoWidth: 8.96, torsoHeight: 9.13, torsoDepth: 3.85,
    shoulderX: 4.66, shoulderY: 8.24, headWidth: 4.44, headHeight: 3.62,
    headDepth: 3.72, headY: 12.28,
  },
};

function createEntry(metrics = METRICS.daggers, forge = true) {
  const mounts = Object.fromEntries(
    ['chest', 'head', 'back', 'shoulderL', 'shoulderR', 'cosmeticRoot']
      .map(name => [name, new FakeNode(`mount-${name}`)]),
  );
  const leftDagger = new FakeNode('left-dagger');
  const rightDagger = new FakeNode('right-dagger');
  leftDagger.material = new FakeMaterial('dagger-left-original');
  rightDagger.material = new FakeMaterial('dagger-right-original');
  const entry = {
    root: new FakeNode('bot-root'),
    mounts,
    mountMetrics: metrics,
    isForgeCharacter: forge,
    weapon: {
      _forgeMeshes: [leftDagger, rightDagger],
      getChildMeshes: () => [],
    },
    lodRefreshes: 0,
    setLOD() { this.lodRefreshes += 1; },
  };
  return {entry, leftDagger, rightDagger};
}

function botWith(loadout) {
  return {
    bot_id: `forge-${Math.random()}`,
    avatar_color: '#22ccff',
    cosmetics: {
      bot_skin: 'standard', weapon_skin: 'standard', attachment: 'none', ...loadout,
    },
  };
}

function mesh(namePart) {
  const found = nodes.find(node => !node.disposed && node.name.includes(namePart));
  assert.ok(found, `expected a live mesh containing ${namePart}`);
  return found;
}

function assertMounted(namePart, mount, message) {
  const rendered = mesh(namePart);
  assert.equal(rendered.parent?.parent, mount, message);
  return rendered;
}

for (const pattern of ['bands', 'plates', 'chevrons', 'core']) {
  const {entry} = createEntry();
  cosmetics.applyBotCosmetics(entry, botWith({bot_skin: `test-skin-${pattern}`}), {}, {forceEnabled: true});
  if (pattern === 'bands') {
    assertMounted('set-radiator-frame', entry.mounts.chest,
      'procedural radiator armor must scale around the semantic chest mount');
  } else if (pattern === 'plates') {
    assertMounted('set-pauldron', entry.mounts.shoulderL,
      'the left procedural plate must follow the articulated left shoulder');
    const plates = nodes.filter(node => !node.disposed && node.name.includes('set-pauldron'));
    assert.ok(plates.some(node => node.parent?.parent === entry.mounts.shoulderR),
      'the right procedural plate must follow the articulated right shoulder');
  } else {
    assertMounted(`set-${pattern === 'core' ? 'power-socket' : 'harness-coupler'}`, entry.mounts.chest,
      `${pattern} skin geometry must use chest-local coordinates`);
  }
  assert.equal(entry.lodRefreshes, 1, 'cosmetic refresh must reapply the current Forge LOD');
  disposeChecked(entry);
}

{
  const {entry} = createEntry();
  cosmetics.applyBotCosmetics(entry, botWith({bot_skin: 'neon_grid'}), {}, {forceEnabled: true});
  assertMounted('set-radiator-frame', entry.mounts.chest, 'Forge neon radiator armor must use chest-local coordinates');
  disposeChecked(entry);
}

{
  const {entry} = createEntry();
  cosmetics.applyBotCosmetics(entry, botWith({bot_skin: 'carbon_armor'}), {}, {forceEnabled: true});
  assertMounted('set-cuirass-', entry.mounts.chest, 'Forge carbon armor must attach to the chest mount');
  const shoulders = nodes.filter(node => !node.disposed && node.name.includes('set-pauldron-'));
  assert.ok(shoulders.some(node => node.parent?.parent === entry.mounts.shoulderL));
  assert.ok(shoulders.some(node => node.parent?.parent === entry.mounts.shoulderR));
  disposeChecked(entry);
}

for (const kind of ['halo', 'antenna', 'crown', 'orbitals']) {
  const {entry} = createEntry();
  cosmetics.applyBotCosmetics(entry, botWith({attachment: `test-attachment-${kind}`}), {}, {forceEnabled: true});
  const part = {halo: 'set-halo-segment', antenna: 'set-scanner-mast', crown: 'set-crest-vane', orbitals: 'set-nav-pod'}[kind];
  assertMounted(part, entry.mounts.head, `${kind} must follow the semantic head mount`);
  disposeChecked(entry);
}

{
  const {entry} = createEntry();
  entry.mounts.headTop = new FakeNode('mount-headTop');
  cosmetics.applyBotCosmetics(entry, botWith({attachment: 'test-attachment-crown'}), {}, {forceEnabled: true});
  assertMounted('set-crest-vane', entry.mounts.headTop,
    'a silhouette head-top anchor must take precedence over the raw head mount');
  disposeChecked(entry);
}

for (const [asset, part] of [
  ['signal_antenna', 'set-scanner-mast'],
  ['orbital_halo', 'set-halo-segment'],
]) {
  const {entry} = createEntry();
  cosmetics.applyBotCosmetics(entry, botWith({attachment: asset}), {}, {forceEnabled: true});
  assertMounted(part, entry.mounts.head, `legacy ${asset} must follow the semantic head mount on Forge`);
  disposeChecked(entry);
}

{
  const {entry} = createEntry();
  cosmetics.applyBotCosmetics(entry, botWith({attachment: 'test-attachment-fins'}), {}, {forceEnabled: true});
  const fins = nodes.filter(node => !node.disposed && node.name.includes('set-radiator-wing-'));
  assert.ok(fins.some(node => node.parent?.parent === entry.mounts.shoulderL));
  assert.ok(fins.some(node => node.parent?.parent === entry.mounts.shoulderR));
  disposeChecked(entry);
}

{
  const {entry} = createEntry();
  cosmetics.applyBotCosmetics(entry, botWith({attachment: 'test-attachment-reactor'}), {}, {forceEnabled: true});
  assertMounted('set-reactor-casing-', entry.mounts.back, 'reactor must use the semantic back mount');
  disposeChecked(entry);
}

{
  const {entry, leftDagger, rightDagger} = createEntry();
  const leftOriginal = leftDagger.material;
  const rightOriginal = rightDagger.material;
  cosmetics.applyBotCosmetics(entry, botWith({weapon_skin: 'test-weapon-ion'}), {}, {forceEnabled: true});
  assert.notEqual(leftDagger.material, leftOriginal, 'detached left dagger must receive the finish');
  assert.notEqual(rightDagger.material, rightOriginal, 'detached right dagger must receive the finish');
  disposeChecked(entry);
  assert.equal(leftDagger.material, leftOriginal);
  assert.equal(rightDagger.material, rightOriginal);
}

{
  const dagger = createEntry(METRICS.daggers);
  cosmetics.applyBotCosmetics(dagger.entry, botWith({bot_skin: 'carbon_armor'}), {}, {forceEnabled: true});
  const daggerChest = mesh('set-cuirass-');
  const daggerWidth = daggerChest.geometryOptions.width;
  disposeChecked(dagger.entry);

  const shield = createEntry(METRICS.shield);
  cosmetics.applyBotCosmetics(shield.entry, botWith({bot_skin: 'carbon_armor'}), {}, {forceEnabled: true});
  const shieldChest = mesh('set-cuirass-');
  assert.ok(shieldChest.geometryOptions.width > daggerWidth * 1.4,
    'carbon armor geometry must scale from each chassis profile instead of one world-space size');
  disposeChecked(shield.entry);
}

{
  const {entry} = createEntry(METRICS.daggers, false);
  cosmetics.applyBotCosmetics(entry, botWith({bot_skin: 'carbon_armor'}), {}, {forceEnabled: true});
  const legacyChest = assertMounted('set-cuirass-', entry.mounts.cosmeticRoot,
    'non-Forge cosmetics must retain the compatibility coordinate root');
  assert.ok(legacyChest.position.y > 0 && legacyChest.position.z < 0,
    'the rebuilt compatibility armor must remain above the floor and forward of the torso');
  const cuirass = nodes.filter(node => !node.disposed && node.name.includes('set-cuirass-'));
  assert.equal(cuirass.length, 2, 'the compatibility root must carry both halves of the split cuirass');
  assert.equal(cuirass[0].position.x, -cuirass[1].position.x,
    'the compatibility armor must preserve symmetric coverage around the torso');
  assert.ok(cuirass.every(node => node.parent.parent === entry.mounts.cosmeticRoot),
    'both compatibility armor halves must stay on the compatibility root');
  disposeChecked(entry);
}

console.log('Forge cosmetics use authored bounded geometry and semantic mounts, tint detached daggers, and preserve compatibility roots');
