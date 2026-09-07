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
    node.vertexData = this.vertexData;
    node.mergedPieces = this.mergedPieces;
    node.enabled = this.enabled;
    return node;
  }
  setEnabled(enabled) { this.enabled = enabled; }
  dispose() { this.disposed = true; }
  isDisposed() { return this.disposed; }
}

const createdMaterials = [];
class FakeMaterial {
  constructor(name, scene) {
    Object.assign(this, {name, scene, disposed: false, frozen: false});
    createdMaterials.push(this);
  }
  freeze() { this.frozen = true; }
  dispose() { this.disposed = true; }
}

let primitiveBuilderCalls = 0;
class FakeMesh extends FakeNode {
  static MergeMeshes(pieces, disposeSource) {
    assert.ok(pieces.length > 0);
    const mesh = new FakeMesh('merged', pieces[0].scene);
    mesh.mergedPieces = [...pieces];
    if (disposeSource) for (const piece of pieces) piece.dispose();
    return mesh;
  }
}

// Match Babylon's documented left-handed normal convention using actual
// triangle arithmetic. A no-op normal mock would miss inside-out sculptures.
class FakeVertexData {
  static ComputeNormals(positions, indices, normals) {
    normals.length = positions.length;
    normals.fill(0);
    for (let i = 0; i < indices.length; i += 3) {
      const [a, b, c] = indices.slice(i, i + 3).map(index => index * 3);
      const u = [0, 1, 2].map(axis => positions[a + axis] - positions[b + axis]);
      const v = [0, 1, 2].map(axis => positions[c + axis] - positions[b + axis]);
      const face = [u[1] * v[2] - u[2] * v[1], u[2] * v[0] - u[0] * v[2], u[0] * v[1] - u[1] * v[0]];
      const length = Math.hypot(...face) || 1;
      for (const vertex of [a, b, c]) for (let axis = 0; axis < 3; axis += 1) normals[vertex + axis] += face[axis] / length;
    }
    for (let i = 0; i < normals.length; i += 3) {
      const length = Math.hypot(...normals.slice(i, i + 3)) || 1;
      for (let axis = 0; axis < 3; axis += 1) normals[i + axis] /= length;
    }
  }
  applyToMesh(mesh) {
    mesh.vertexData = {
      positions: [...this.positions], indices: [...this.indices],
      normals: [...this.normals], uvs: [...this.uvs],
    };
  }
}

function assertSculptGeometry(mesh) {
  const {positions, indices, normals, uvs} = mesh.vertexData;
  const vertices = positions.length / 3;
  assert.ok(Number.isInteger(vertices) && vertices >= 30 && vertices <= 256, `${mesh.name} bounded sculpt vertices`);
  assert.equal(indices.length % 3, 0);
  assert.equal(normals.length, positions.length);
  assert.equal(uvs.length, vertices * 2);
  assert.ok([...positions, ...normals, ...uvs].every(Number.isFinite), `${mesh.name} finite buffers`);
  assert.ok(indices.every(index => Number.isInteger(index) && index >= 0 && index < vertices));
  // Signed volume is independent of the normal mock: clockwise exterior
  // triangles have negative conventional signed volume in Babylon's LH scene.
  let volume = 0;
  for (let i = 0; i < indices.length; i += 3) {
    const [a, b, c] = indices.slice(i, i + 3).map(index => positions.slice(index * 3, index * 3 + 3));
    const cross = [b[1] * c[2] - b[2] * c[1], b[2] * c[0] - b[0] * c[2], b[0] * c[1] - b[1] * c[0]];
    volume += a.reduce((sum, value, axis) => sum + value * cross[axis], 0) / 6;
    const ab = b.map((value, axis) => value - a[axis]);
    const ac = c.map((value, axis) => value - a[axis]);
    assert.ok(Math.hypot(ab[1] * ac[2] - ab[2] * ac[1], ab[2] * ac[0] - ab[0] * ac[2], ab[0] * ac[1] - ab[1] * ac[0]) > 1e-9,
      `${mesh.name} must not contain degenerate triangles`);
  }
  assert.ok(volume < -1e-4, `${mesh.name} must have outward clockwise winding, volume=${volume}`);
  let lowerCap = 0, upperCap = 0, sideVertices = 0;
  const ys = Array.from({length: vertices}, (_, i) => positions[i * 3 + 1]);
  const low = Math.min(...ys), high = Math.max(...ys);
  const rings = new Map();
  for (let i = 0; i < vertices; i += 1) {
    const y = positions[i * 3 + 1];
    const ring = rings.get(y) || [];
    ring.push(i); rings.set(y, ring);
  }
  for (let i = 0; i < vertices; i += 1) {
    const n = normals.slice(i * 3, i * 3 + 3);
    assert.ok(Math.abs(Math.hypot(...n) - 1) < 1e-7, `${mesh.name} unit normal`);
    const y = positions[i * 3 + 1];
    if (y === low && n[1] < -.999) lowerCap += 1;
    else if (y === high && n[1] > .999) upperCap += 1;
    else {
      const ring = rings.get(y);
      const centerX = ring.reduce((sum, vertex) => sum + positions[vertex * 3], 0) / ring.length;
      const centerZ = ring.reduce((sum, vertex) => sum + positions[vertex * 3 + 2], 0) / ring.length;
      const radialDot = n[0] * (positions[i * 3] - centerX) + n[2] * (positions[i * 3 + 2] - centerZ);
      assert.ok(radialDot > 0, `${mesh.name} radial side normal must face outward`);
      sideVertices += 1;
    }
  }
  assert.ok(lowerCap >= 8 && upperCap >= 8, `${mesh.name} independent downward/upward flat caps`);
  assert.ok(sideVertices >= 28, `${mesh.name} continuous sculpted side surface`);
}
const MeshBuilder = new Proxy({}, {
  get: (_, method) => method.startsWith('Create')
    ? ((name, options, scene) => {
        primitiveBuilderCalls += 1;
        const node = new FakeNode(name, scene);
        node.geometryOptions = options;
        return node;
      })
    : undefined,
});

globalThis.window = {
  BABYLON: {Color3: FakeColor3, StandardMaterial: FakeMaterial, MeshBuilder, Mesh: FakeMesh, VertexData: FakeVertexData},
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
const nearSources = new Set();
const farSources = new Set();
const allResults = [];

for (const form of BODY_FORM_SPECS) {
  const result = buildBodyFormGeometry(form, {scene, id: `test-${form.key}`, joints, metrics});
  assert.ok(result.meshes.length >= 3, `${form.key} needs a complete readable body`);
  assert.ok(result.meshes.length <= form.nearMeshBudget,
    `${form.key} rendered ${result.meshes.length} meshes above its declared ${form.nearMeshBudget} budget`);
  assert.equal(result.materials.length, 3, `${form.key} should own exactly three bounded materials`);
  assert.ok(result.materials.every(material => material.frozen === false),
    `${form.key} materials must remain mutable for dodge, stun, hit, and death feedback`);
  assert.ok(result.meshes.includes(result.body) && result.meshes.includes(result.head), `${form.key} needs canonical body/head pick meshes`);
  assert.ok(Number.isFinite(result.anchors.headTopY));
  assert.ok(result.anchors.backPos.length === 3 && result.anchors.backPos.every(Number.isFinite));
  for (const mesh of result.meshes) {
    assert.ok(allowedParents.has(mesh.parent), `${form.key} mesh ${mesh.name} bypassed an animated joint`);
    assert.equal(mesh.isPickable, false);
    assert.ok(result.materials.includes(mesh.material), `${mesh.name} must use an owned status material`);
    assert.ok([mesh.scaling.x, mesh.scaling.y, mesh.scaling.z].every(value => Number.isFinite(value) && value > 0));
    nearSources.add(mesh.geometrySource);
    assert.equal(mesh.enabled, true,
      `${form.key} near meshes must not inherit the disabled shared-template state`);
  }
  assert.ok(result.meshes.every(mesh => mesh.geometrySource?.name.startsWith('body-form-near-template-')),
    `${form.key} must clone shared sculpted geometry instead of rebuilding vertex buffers per bot`);

  const modelRoot = joint(`model-${form.key}`);
  const far = createBodyFormFarProxy(form, scene, modelRoot, {height: 24, width: 10, depth: 6});
  assert.equal(far.parent, modelRoot);
  assert.equal(far.enabled, false);
  assert.equal(far.isPickable, true);
  assert.equal(far.material.frozen, true);
  assert.ok(far.geometrySource.mergedPieces.length >= 3, `${form.key} far template retains its full authored silhouette`);
  assert.ok(far.geometrySource.mergedPieces.every(piece => piece.disposed), `${form.key} far assembly releases temporary pieces`);
  farSources.add(far.geometrySource);
  allResults.push({form, result, far, modelRoot});
  assert.match(far.name, new RegExp(form.key));
  assert.notEqual(far.material?.name, 'forge-far-silhouette-shared',
    `${form.key} must not turn into the generic blue proxy when zoomed out`);
}

// Repeated characters may allocate clones/materials, never new geometry.
assert.ok(nearSources.size <= 22, 'the complete roster shares at most 22 authored geometry templates');
assert.equal(farSources.size, BODY_FORM_SPECS.length, 'each form has one cached far silhouette');
const sculptSources = [...nearSources].filter(source => source.vertexData);
assert.ok(sculptSources.length >= 12, 'forms must actually use the authored sculpted surfaces');
for (const mesh of sculptSources) assertSculptGeometry(mesh);
for (const source of nearSources) {
  assert.equal(source.enabled, false, `${source.name} templates stay hidden`);
  assert.equal(source.isPickable, false);
  assert.equal(source.disposed, false);
}
const allocationCount = primitiveBuilderCalls;
const sculptCount = created.filter(mesh => mesh.vertexData && !mesh.geometrySource).length;
for (const {form, result, far, modelRoot} of allResults) {
  for (const mesh of result.meshes) mesh.dispose();
  for (const material of result.materials) material.dispose();
  far.dispose();
  const before = created.length;
  const next = buildBodyFormGeometry(form, {scene, id: `replacement-${form.key}`, joints, metrics});
  const nextFar = createBodyFormFarProxy(form, scene, modelRoot);
  assert.equal(created.length - before, next.meshes.length + 1, 'replacement creates only its live mesh clones');
  assert.ok(next.meshes.every(mesh => nearSources.has(mesh.geometrySource) && !mesh.geometrySource.disposed));
  assert.equal(nextFar.geometrySource, far.geometrySource);
  assert.equal(nextFar.material, far.material);
  assert.ok(next.materials.every(material => !material.disposed && !result.materials.includes(material)));
}
assert.equal(primitiveBuilderCalls, allocationCount, 'replacement must reuse primitive buffers');
assert.equal(created.filter(mesh => mesh.vertexData && !mesh.geometrySource).length, sculptCount,
  'replacement must reuse custom sculpt buffers');

// Budget failure cleans up character-owned allocations, leaving shared sources.
const beforeFailure = created.length, materialsBeforeFailure = createdMaterials.length;
assert.throws(() => buildBodyFormGeometry({...BODY_FORM_SPECS[0], nearMeshBudget: 0}, {
  scene, id: 'over-budget', joints, metrics,
}), /exceeded its 0-mesh budget/);
assert.ok(created.slice(beforeFailure).every(mesh => mesh.disposed));
assert.ok(createdMaterials.slice(materialsBeforeFailure).every(material => material.disposed));
assert.ok([...nearSources].every(source => !source.disposed));

// Both legacy knee mounts and current articulated ankles are supported.
const ankleJoints = {...joints, leftFoot: joint('leftFoot'), rightFoot: joint('rightFoot')};
const ankleForm = buildBodyFormGeometry(BODY_FORM_SPECS.find(form => form.key === 'corgi'), {
  scene, id: 'ankle-mounts', joints: ankleJoints, metrics,
});
for (const side of ['left', 'right']) {
  const foot = ankleForm.meshes.find(mesh => mesh.name.includes(`${side}-foot`));
  assert.equal(foot.parent, ankleJoints[`${side}Foot`]);
  const legacyFoot = allResults.find(entry => entry.form.key === 'corgi').result.meshes.find(mesh => mesh.name.includes(`${side}-foot`));
  assert.equal(legacyFoot.parent, joints[`${side}Knee`]);
}

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
