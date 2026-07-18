import assert from 'node:assert/strict';

class FakeVector3 {
  constructor(x = 0, y = 0, z = 0) { Object.assign(this, {x, y, z}); }
  set(x, y, z) { Object.assign(this, {x, y, z}); return this; }
  setAll(value) { return this.set(value, value, value); }
  copyFrom(value) { return this.set(value.x, value.y, value.z); }
  clone() { return new FakeVector3(this.x, this.y, this.z); }
}

class FakeColor3 extends FakeVector3 {
  clone() { return new FakeColor3(this.x, this.y, this.z); }
  scale(value) { return new FakeColor3(this.x * value, this.y * value, this.z * value); }
  get r() { return this.x; }
  set r(value) { this.x = value; }
  get g() { return this.y; }
  set g(value) { this.y = value; }
  get b() { return this.z; }
  set b(value) { this.z = value; }
  static Black() { return new FakeColor3(0, 0, 0); }
}

class FakeMaterial {
  constructor(name, scene) {
    this.name = name;
    this.scene = scene;
    this.diffuseColor = FakeColor3.Black();
    this.emissiveColor = FakeColor3.Black();
    this.specularColor = FakeColor3.Black();
    this.alpha = 1;
    this.disableLighting = false;
  }
  freeze() {}
  unfreeze() {}
  dispose() { this.disposed = true; }
}

const nodes = [];
class FakeNode {
  constructor(name, scene) {
    this.name = name;
    this.scene = scene;
    this.position = new FakeVector3();
    this.rotation = new FakeVector3();
    this.scaling = new FakeVector3(1, 1, 1);
    this.parent = null;
    this.material = null;
    this.enabled = true;
    nodes.push(this);
  }
  createInstance(name) {
    const instance = new FakeNode(name, this.scene);
    instance.material = this.material;
    instance.sourceMesh = this;
    return instance;
  }
  clone(name) {
    const clone = new FakeNode(name, this.scene);
    clone.material = this.material;
    clone.geometrySource = this;
    return clone;
  }
  getScene() { return this.scene; }
  getChildMeshes() { return nodes.filter(node => node.parent === this); }
  setEnabled(enabled) { this.enabled = enabled; }
  isEnabled() { return this.enabled; }
  isDisposed() { return this.disposed === true; }
  dispose() { this.disposed = true; }
}

const MeshBuilder = new Proxy({}, {
  get: (_, key) => key.startsWith('Create')
    ? ((name, options, scene) => {
        const node = new FakeNode(name, scene);
        node.geometryOptions = options;
        return node;
      })
    : undefined,
});

globalThis.window = {
  BABYLON: {
    Color3: FakeColor3,
    StandardMaterial: FakeMaterial,
    TransformNode: FakeNode,
    MeshBuilder,
    Mesh: {CAP_ALL: 3},
    Vector3: FakeVector3,
  },
};

// This suite pins the LIT-mode material contract (shading depth + emissive
// floor). rendering.characterLighting ships default-OFF while the look is
// tuned live, so enable it explicitly — the assertions verify the mechanism,
// not the shipping default.
const {setEffect} = await import(new URL('../frontend/js/settings.js', import.meta.url));
setEffect('rendering', 'characterLighting', true);

const {createForgeCharacter, setForgeCharacterLOD} = await import(
  new URL('../frontend/js/renderer/character-rig.js?visibility-regression', import.meta.url)
);
const {updateForgeCharacter} = await import(
  new URL('../frontend/js/renderer/character-anims.js?visibility-regression', import.meta.url)
);
const {BotRenderer} = await import(
  new URL('../frontend/js/renderer/bots.js?visibility-regression', import.meta.url)
);

const weapons = ['sword', 'bow', 'spear', 'daggers', 'staff', 'shield', 'grapple'];
const avatarColors = ['#ff5c72', '#74f28c', '#ffd166', '#c77dff', '#ff9f45', '#5ce1e6', '#f783ff'];
const scene = {};
const entries = weapons.map((weapon, index) => createForgeCharacter({
  bot_id: `visibility-test-${weapon}`,
  name: `Visibility Test ${weapon}`,
  avatar_color: avatarColors[index],
  weapon,
}, scene));

const luminance = color => 0.2126 * color.r + 0.7152 * color.g + 0.0722 * color.b;
const guaranteedLuminance = mesh => luminance(mesh.material.emissiveColor);
const focus = process.argv[2] || 'all';

const anatomicalParts = [
  'forge-torso-', 'forge-chest-plate-', 'forge-head-',
  'forge-left-upper-arm-', 'forge-left-forearm-',
  'forge-right-upper-arm-', 'forge-right-forearm-',
  'forge-left-upper-leg-', 'forge-left-shin-', 'forge-left-foot-',
  'forge-right-upper-leg-', 'forge-right-shin-', 'forge-right-foot-',
];
const requiredWeaponParts = {
  sword: ['forge-sword-grip-'],
  bow: ['forge-bow-string-'],
  spear: ['forge-spear-shaft-'],
  daggers: ['forge-dagger-grip-'],
  staff: ['forge-staff-shaft-'],
  shield: ['forge-shield-shell-'],
  grapple: ['forge-grapple-launcher-', 'forge-grapple-cable-'],
};

if (focus === 'all' || focus === 'body') {
  for (const entry of entries) {
    for (const part of anatomicalParts) {
      const mesh = entry._forgeMeshes.find(candidate => candidate.name.startsWith(part));
      assert.ok(mesh, `${entry.profile.weapon} ${part} must remain part of the high-detail body`);
      assert.ok(guaranteedLuminance(mesh) >= 0.15,
        `${mesh.name} needs enough shared emissive contrast to remain visible in the dark arena`);
      // Issue #181: chassis materials are sun/hemi-lit for depth, but the
      // emissive floor above still guarantees the minimum silhouette, and the
      // legacy self-lit look must remain restorable for the
      // rendering.characterLighting toggle.
      assert.equal(mesh.material.disableLighting, false,
        `${mesh.name} must take arena lighting for shading depth (emissive floor covers the dark sectors)`);
      assert.ok(mesh.material._forgeUnlitEmissive,
        `${mesh.name} must retain its self-lit emissive fallback for the characterLighting toggle`);
    }
  }
  const torsoMaterials = entries.map(entry => entry.joints.torso.material);
  assert.ok(torsoMaterials.every(material => material === torsoMaterials[0]),
    'readable body contrast must still use one scene-owned material across every bot');
}

if (focus === 'all' || focus === 'far') {
  for (const entry of entries) {
    setForgeCharacterLOD(entry, true);
    assert.equal(entry.lowDetail.isEnabled(), true, 'far LOD must enable its shared proxy');
    assert.ok(guaranteedLuminance(entry.lowDetail) >= 0.55,
      'far LOD needs a high-contrast shared material so zooming out cannot erase bots');
    assert.equal(entry.lowDetail.material.disableLighting, true,
      'far LOD visibility must not depend on arena lighting');
    assert.ok(entry.lowDetail.scaling.x >= Math.max(
      entry.mountMetrics.torsoWidth,
      entry.mountMetrics.pelvisWidth,
      entry.mountMetrics.headWidth,
    ) * 1.15, 'far LOD must widen the proxy enough to remain legible at minimum zoom');
    assert.ok(entry.lowDetail.scaling.y >= entry.lowDetail.position.y * 2 * 1.05,
      'far LOD must slightly enlarge the proxy height at minimum zoom');
    assert.ok(entry.lowDetail.scaling.z >= Math.max(
      entry.mountMetrics.torsoDepth,
      entry.mountMetrics.headDepth,
    ) * 1.10, 'far LOD must deepen the proxy enough to retain a solid silhouette');
  }
  const farMaterials = entries.map(entry => entry.lowDetail.material);
  assert.equal(new Set(farMaterials).size, entries.length,
    'far LOD must keep per-bot avatar materials instead of painting every bot the same blue');
  assert.equal(new Set(farMaterials.map(material => [
    material.diffuseColor.r.toFixed(4),
    material.diffuseColor.g.toFixed(4),
    material.diffuseColor.b.toFixed(4),
  ].join(','))).size, entries.length,
  'far LOD colors must preserve each bot identity at overview distance');

  const statusEntry = entries[0];
  const farMaterial = statusEntry.lowDetail.material;
  const farEmissive = farMaterial.emissiveColor.clone();
  BotRenderer.prototype._updateStatusEffects.call({}, statusEntry, {
    is_alive: true, is_dodging: true, is_stunned: true, hp: 12, max_hp: 100,
  }, 100);
  assert.equal(statusEntry.lowDetail.visibility, undefined,
    'status effects must not write unsupported visibility values to instances');
  assert.ok(statusEntry._forgeMeshes.every(mesh => mesh.visibility === undefined),
    'status effects must not write unsupported visibility values to body instances');
  assert.equal(statusEntry.bodyMat.alpha, 0.5,
    'dodge feedback must use the bot-owned accent material');
  assert.equal(statusEntry.headMat.alpha, 0.5,
    'dodge feedback must use the bot-owned core material');
  assert.deepEqual(farMaterial.emissiveColor, farEmissive,
    'stun and wounded status must not mutate the bot identity far-visibility floor');
  BotRenderer.prototype._updateStatusEffects.call({}, statusEntry, {
    is_alive: true, is_dodging: false, is_stunned: false, hp: 100, max_hp: 100,
  }, 200);
  assert.equal(statusEntry.lowDetail.visibility, undefined,
    'the far proxy must stay opaque instead of accepting unsupported visibility writes');
  assert.equal(statusEntry.bodyMat.alpha, 1,
    'the accent material must restore full opacity after dodge');
  assert.equal(statusEntry.headMat.alpha, 1,
    'the core material must restore full opacity after dodge');
  assert.deepEqual(farMaterial.emissiveColor, farEmissive);

  statusEntry.isAlive = false;
  updateForgeCharacter(statusEntry, 0.1, false, false);
  assert.ok(statusEntry.anim.deathTimer >= 0,
    'far characters must keep advancing death choreography without high-detail joint writes');
  assert.equal(statusEntry.lowDetail.isEnabled(), true,
    'the far proxy must remain enabled for the visible death window');
}

if (focus === 'all' || focus === 'weapon') {
  for (const entry of entries) {
    for (const part of requiredWeaponParts[entry.profile.weapon]) {
      const meshes = entry.weapon._forgeMeshes.filter(candidate => candidate.name.startsWith(part));
      assert.ok(meshes.length > 0, `${entry.profile.weapon} requires visible geometry for ${part}`);
      for (const mesh of meshes) {
        assert.ok(guaranteedLuminance(mesh) >= 0.12,
          `${mesh.name} must not disappear and leave detached weapon pieces`);
        if (mesh.material.name.startsWith('forge-weapon-')) {
          // Issue #181: weapon silhouettes are lit like the chassis; the
          // emissive floor asserted above keeps the dark-sector guarantee.
          assert.equal(mesh.material.disableLighting, false,
            `${mesh.name} must take arena lighting for shading depth (emissive floor covers the dark sectors)`);
          assert.ok(mesh.material._forgeUnlitEmissive,
            `${mesh.name} must retain its self-lit emissive fallback for the characterLighting toggle`);
        }
      }
    }
  }
  for (const materialName of ['forge-weapon-dark', 'forge-weapon-cable']) {
    const meshes = entries.flatMap(entry => entry.weapon._forgeMeshes)
      .filter(mesh => mesh.material.name === materialName);
    assert.ok(meshes.length > 1, `${materialName} must be exercised across multiple chassis`);
    assert.ok(meshes.every(mesh => mesh.material === meshes[0].material),
      `${materialName} must stay scene-owned and shared across bots`);
  }
}

console.log('Forge body parts and far LOD retain a readable silhouette in the dark arena');
