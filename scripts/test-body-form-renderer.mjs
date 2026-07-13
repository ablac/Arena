import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

class FakeColor3 {
  constructor(r = 0, g = 0, b = 0) { Object.assign(this, {r, g, b}); }
  clone() { return new FakeColor3(this.r, this.g, this.b); }
  scale(value) { return new FakeColor3(this.r * value, this.g * value, this.b * value); }
  static Black() { return new FakeColor3(0, 0, 0); }
}

class FakeVector3 {
  constructor(x = 0, y = 0, z = 0) { Object.assign(this, {x, y, z}); }
  set(x, y, z) { Object.assign(this, {x, y, z}); return this; }
}

const created = [];
class FakeNode {
  constructor(name, scene) {
    Object.assign(this, {
      name, scene, position: new FakeVector3(), rotation: new FakeVector3(),
      scaling: new FakeVector3(1, 1, 1), parent: null, material: null,
      enabled: true, disposed: false,
    });
    created.push(this);
  }
  clone(name) {
    const node = new FakeNode(name, this.scene);
    node.material = this.material;
    node.geometrySource = this;
    node.enabled = this.enabled;
    return node;
  }
  setEnabled(enabled) { this.enabled = enabled; }
  dispose() { this.disposed = true; }
  isDisposed() { return this.disposed; }
}

class FakeMaterial {
  constructor(name, scene) { Object.assign(this, {name, scene, disposed: false, frozen: false}); }
  freeze() { this.frozen = true; }
  dispose() { this.disposed = true; }
}

let nearBuilderCalls = 0;
const MeshBuilder = new Proxy({}, {
  get: (_, method) => method.startsWith('Create')
    ? ((name, options, scene) => {
        if (name.startsWith('body-form-near-template-')) nearBuilderCalls += 1;
        const node = new FakeNode(name, scene);
        node.geometryOptions = options;
        return node;
      })
    : undefined,
});

globalThis.window = {
  BABYLON: {Color3: FakeColor3, StandardMaterial: FakeMaterial, MeshBuilder},
};

const {BODY_FORM_SPECS} = await import(
  new URL('../frontend/js/renderer/body-form-roster.js?body-form-renderer-roster', import.meta.url)
);
const {buildBodyFormGeometry, createBodyFormFarProxy} = await import(
  new URL('../frontend/js/renderer/body-form-geometry.js?body-form-renderer-test', import.meta.url)
);

const scene = {};
const joint = name => new FakeNode(name, scene);
const joints = {
  body: joint('body'), head: joint('head'), leftArm: joint('leftArm'), rightArm: joint('rightArm'),
  leftElbow: joint('leftElbow'), rightElbow: joint('rightElbow'),
  leftLeg: joint('leftLeg'), rightLeg: joint('rightLeg'), leftKnee: joint('leftKnee'), rightKnee: joint('rightKnee'),
};
const metrics = {
  bodyY: 10, torsoWidth: 7, torsoHeight: 8.15, torsoDepth: 3.75,
  headWidth: 4.35, headHeight: 3.55, headDepth: 3.65,
  upperArmLength: 4.25, forearmLength: 3.65, upperLegLength: 5.25, shinLength: 4.85,
};
const allowedParents = new Set(Object.values(joints));

for (const form of BODY_FORM_SPECS) {
  const before = created.length;
  const result = buildBodyFormGeometry(form, {scene, id: `test-${form.key}`, joints, metrics});
  assert.ok(result.meshes.length >= 3, `${form.key} needs a complete readable body`);
  assert.ok(result.meshes.length <= form.nearMeshBudget,
    `${form.key} rendered ${result.meshes.length} meshes above its declared ${form.nearMeshBudget} budget`);
  assert.equal(result.materials.length, 3, `${form.key} should own exactly three bounded materials`);
  assert.ok(result.materials.every(material => material.frozen === false),
    `${form.key} materials must remain mutable for dodge, stun, hit, and death feedback`);
  assert.ok(result.body && result.head, `${form.key} needs canonical body/head pick meshes`);
  for (const mesh of result.meshes) {
    assert.ok(allowedParents.has(mesh.parent), `${form.key} mesh ${mesh.name} bypassed an animated joint`);
    assert.equal(mesh.isPickable, false);
    assert.equal(mesh.enabled, true,
      `${form.key} near meshes must not inherit the disabled shared-template state`);
  }
  assert.ok(created.length - before <= result.meshes.length + 5,
    `${form.key} may only add its clones plus the five shared primitive templates`);
  assert.ok(result.meshes.every(mesh => mesh.geometrySource?.name.startsWith('body-form-near-template-')),
    `${form.key} must clone shared primitive geometry instead of rebuilding vertex buffers per bot`);

  const modelRoot = joint(`model-${form.key}`);
  const far = createBodyFormFarProxy(form, scene, modelRoot, {height: 24, width: 10, depth: 6});
  assert.equal(far.parent, modelRoot);
  assert.equal(far.enabled, false);
  assert.match(far.name, new RegExp(form.key));
  assert.notEqual(far.material?.name, 'forge-far-silhouette-shared',
    `${form.key} must not turn into the generic blue proxy when zoomed out`);
}

assert.ok(nearBuilderCalls <= 5,
  'all near body forms must share at most five primitive geometry sources per scene');

const rigSource = readFileSync(new URL('../frontend/js/renderer/character-rig.js', import.meta.url), 'utf8');
const botSource = readFileSync(new URL('../frontend/js/renderer/bots.js', import.meta.url), 'utf8');
const previewSource = readFileSync(new URL('../frontend/js/shop-preview.js', import.meta.url), 'utf8');
const cosmeticSource = readFileSync(new URL('../frontend/js/renderer/cosmetics.js', import.meta.url), 'utf8');
assert.match(rigSource, /bodyFormForAsset/);
assert.match(rigSource, /buildBodyFormGeometry/);
assert.match(rigSource, /createBodyFormFarProxy/,
  'full body forms must retain authored silhouettes at distant Arena LOD');
assert.match(rigSource, /bodyMat\.dispose\(\)/,
  'discarding the standard shell must immediately release its now-unused accent material');
assert.match(rigSource, /_forgeFarMeshes:\s*bodyForm\s*\?\s*\[\]\s*:/,
  'body-form far proxies must replace near weapons and cores instead of retaining extra crowd submissions');
assert.match(botSource, /bodyFormKeyForBot/);
assert.match(botSource, /entry\?\.bodyFormKey\s*!==\s*bodyFormKey/,
  'live bots must rebuild only when the equipped full body form changes');
assert.match(previewSource, /bodyFormKeyForBot/);
assert.match(previewSource, /_rebuildEntry/,
  'Shop and Dashboard previews must rebuild the same production rig when a form changes');
assert.match(cosmeticSource, /asset\.kind === 'body-form'/,
  'the overlay renderer must recognize construction-time full-body skins');

console.log('all 18 body forms are articulated, mesh-bounded, and retain form-specific far proxies');
