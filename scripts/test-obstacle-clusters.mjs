import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  TRIM_OVERHANG,
  TRIM_TOP_HEIGHT,
  TRIM_TOP_LIFT,
  TRIM_BASE_HEIGHT,
  clusterObstacles,
  computeClusterOutline,
  offsetContour,
  appendGeometry,
  buildClusterTrimGeometry,
} from '../frontend/js/renderer/obstacle-clusters.js';
import { contourArea } from '../frontend/js/renderer/map-walls.js';

// ---------------------------------------------------------------------------
// Module identity / wiring pins
// ---------------------------------------------------------------------------

const obstaclesSource = readFileSync(new URL('../frontend/js/renderer/obstacles.js', import.meta.url), 'utf8');
const ciYml = readFileSync(new URL('../.github/workflows/ci.yml', import.meta.url), 'utf8');
assert.match(obstaclesSource, /obstacle-clusters\.js\?v=20260718g/,
  'obstacles.js must import the stamped cluster module');
assert.match(ciYml, /test-obstacle-clusters\.mjs/, 'this suite must be wired into CI');
assert.equal(TRIM_OVERHANG * 2, 1.5,
  'the unified trim must overhang exactly like the old +1.5-oversized trim boxes');
assert.equal(TRIM_TOP_HEIGHT, 2);
assert.equal(TRIM_TOP_LIFT, 0.8);
assert.equal(TRIM_BASE_HEIGHT, 1.5);

// ---------------------------------------------------------------------------
// Clustering (touch/overlap union-find)
// ---------------------------------------------------------------------------

{
  const rects = [
    { x: 0, y: 0, width: 40, height: 40 },    // 0: touches 1 on its right edge
    { x: 40, y: 0, width: 40, height: 40 },   // 1
    { x: 200, y: 200, width: 20, height: 20 },// 2: isolated
    { x: 60, y: 30, width: 40, height: 40 },  // 3: overlaps 1
  ];
  const clusters = clusterObstacles(rects);
  assert.deepEqual(clusters, [[0, 1, 3], [2]],
    'edge-touching and overlapping rects must group; isolated rects stay alone');
}
{
  // Corner-touching rects connect (their oversized trims used to overlap
  // at the shared corner too).
  const clusters = clusterObstacles([
    { x: 0, y: 0, width: 20, height: 20 },
    { x: 20, y: 20, width: 20, height: 20 },
  ]);
  assert.deepEqual(clusters, [[0, 1]]);
}
{
  // A one-cell gap keeps rects separate.
  const clusters = clusterObstacles([
    { x: 0, y: 0, width: 20, height: 20 },
    { x: 40, y: 0, width: 20, height: 20 },
  ]);
  assert.deepEqual(clusters, [[0], [1]]);
}

// ---------------------------------------------------------------------------
// Exact rectilinear union outlines
// ---------------------------------------------------------------------------

{
  // L-shape: two touching rects → one 6-corner outline, exact union area.
  const outline = computeClusterOutline([
    { x: 0, y: 0, width: 40, height: 40 },
    { x: 40, y: 0, width: 40, height: 80 },
  ]);
  assert.ok(outline, 'grid-aligned rects must produce an outline');
  assert.equal(outline.groups.length, 1);
  const [group] = outline.groups;
  assert.equal(group.holes.length, 0);
  assert.equal(group.outer.length, 6, 'an L union must merge to 6 corners');
  assert.equal(contourArea(group.outer), 40 * 40 + 40 * 80,
    'the outline area must equal the exact union of the collision rects');
}
{
  // Ring of 4 rects → one outline with the courtyard as a hole.
  const outline = computeClusterOutline([
    { x: 0, y: 0, width: 100, height: 20 },
    { x: 0, y: 80, width: 100, height: 20 },
    { x: 0, y: 20, width: 20, height: 60 },
    { x: 80, y: 20, width: 20, height: 60 },
  ]);
  assert.ok(outline);
  assert.equal(outline.groups.length, 1, 'a ring of rects must stay one region');
  const [group] = outline.groups;
  assert.equal(group.outer.length, 4, 'the ring outline is the bounding rectangle');
  assert.equal(group.holes.length, 1, 'the courtyard must come out as a hole');
  assert.equal(contourArea(group.holes[0]), -(60 * 60),
    'the hole must wind negative with the courtyard area');
}
{
  // Corner-touching pair: clustered, but the union is two separate
  // outlines (the marching squares keeps diagonal contacts apart).
  const outline = computeClusterOutline([
    { x: 0, y: 0, width: 20, height: 20 },
    { x: 20, y: 20, width: 20, height: 20 },
  ]);
  assert.ok(outline);
  assert.equal(outline.groups.length, 2, 'diagonal contact yields two prisms');
}
{
  // Overlapping rects: the union outline covers the merged footprint once.
  const outline = computeClusterOutline([
    { x: 0, y: 0, width: 60, height: 40 },
    { x: 40, y: 0, width: 60, height: 40 },
  ]);
  assert.equal(contourArea(outline.groups[0].outer), 100 * 40,
    'overlap must not double-count footprint');
}
{
  // Non-grid-aligned rects (pre-snap server) must decline → box fallback.
  assert.equal(computeClusterOutline([
    { x: 0.5, y: 0, width: 40, height: 40 },
    { x: 40.5, y: 0, width: 40, height: 40 },
  ]), null);
  assert.equal(computeClusterOutline([]), null);
}

// ---------------------------------------------------------------------------
// Trim geometry: exact offset, band heights, single continuous ring
// ---------------------------------------------------------------------------

{
  // Exact miter offset of a CCW square: every corner moves diagonally out
  // by exactly (dist, dist).
  const off = offsetContour([[0, 0], [40, 0], [40, 40], [0, 40]], 0.75);
  assert.deepEqual(off.map((p) => p.map((v) => +v.toFixed(6))), [
    [-0.75, -0.75], [40.75, -0.75], [40.75, 40.75], [-0.75, 40.75],
  ]);
}
{
  const outline = computeClusterOutline([
    { x: 0, y: 0, width: 40, height: 40 },
    { x: 40, y: 0, width: 40, height: 80 },
  ]);
  const earcutStub = (flat) => (flat.length >= 6 ? [0, 1, 2] : []);
  const geo = buildClusterTrimGeometry(outline.groups, { height: 30, earcutFn: earcutStub });
  assert.equal(geo.positions.length % 3, 0);
  assert.equal(geo.positions.length, geo.normals.length);
  assert.equal(geo.indices.length % 3, 0);
  const vertexCount = geo.positions.length / 3;
  for (const idx of geo.indices) {
    assert.ok(Number.isInteger(idx) && idx >= 0 && idx < vertexCount);
  }
  // Every vertex sits on one of the four trim planes the old boxes used:
  // floor, base-ring top, roof-band bottom (30-0.2), roof-band top (30+1.8).
  const allowedY = new Set([0, TRIM_BASE_HEIGHT, 30 + TRIM_TOP_LIFT - TRIM_TOP_HEIGHT / 2,
    30 + TRIM_TOP_LIFT + TRIM_TOP_HEIGHT / 2]);
  for (let i = 1; i < geo.positions.length; i += 3) {
    assert.ok(allowedY.has(geo.positions[i]),
      `trim vertices must sit on the legacy box planes (got ${geo.positions[i]})`);
  }
  // Every x/z vertex lies on the offset outline (single continuous ring, no
  // per-rect trim rectangles crossing the interior).
  const off = offsetContour(outline.groups[0].outer, TRIM_OVERHANG);
  const onOutline = ([px, pz]) => {
    const n = off.length;
    for (let i = 0; i < n; i++) {
      const [x0, y0] = off[i];
      const [x1, y1] = off[(i + 1) % n];
      const dx = x1 - x0;
      const dy = y1 - y0;
      const t = Math.max(0, Math.min(1, ((px - x0) * dx + (pz - y0) * dy) / (dx * dx + dy * dy || 1)));
      if (Math.hypot(px - (x0 + t * dx), pz - (y0 + t * dy)) < 1e-9) return true;
    }
    return false;
  };
  let onCount = 0;
  for (let i = 0; i < geo.positions.length; i += 3) {
    if (onOutline([geo.positions[i], geo.positions[i + 2]])) onCount++;
  }
  assert.equal(onCount, vertexCount,
    'every trim vertex must lie on the single offset union outline');
}
{
  // appendGeometry re-bases indices.
  const target = { positions: [0, 0, 0], normals: [0, 1, 0], indices: [0, 0, 0] };
  appendGeometry(target, { positions: [1, 1, 1, 2, 2, 2, 3, 3, 3], normals: [0, 1, 0, 0, 1, 0, 0, 1, 0], indices: [0, 1, 2] });
  assert.deepEqual(target.indices, [0, 0, 0, 1, 2, 3]);
}

// ---------------------------------------------------------------------------
// ObstacleRenderer integration with stubbed BABYLON: cluster meshes, box
// fallbacks, teardown collection, contact-shadow routing
// ---------------------------------------------------------------------------

function vec3() {
  return { x: 0, y: 0, z: 0, set(x, y, z) { this.x = x; this.y = y; this.z = z; } };
}

const createdBoxes = [];
const createdMeshes = [];

class FakeColor3 {
  constructor(r = 0, g = 0, b = 0) { this.r = r; this.g = g; this.b = b; }
  set(r, g, b) { this.r = r; this.g = g; this.b = b; }
  copyFrom(c) { this.r = c.r; this.g = c.g; this.b = c.b; }
  static Black() { return new FakeColor3(0, 0, 0); }
}

class FakeStandardMaterial {
  constructor(name) {
    this.name = name;
    this.diffuseColor = new FakeColor3();
    this.emissiveColor = new FakeColor3();
    this.specularColor = new FakeColor3();
    this.backFaceCulling = true;
  }
  freeze() { this.frozen = true; }
  unfreeze() { this.frozen = false; }
  clone(name) {
    const m = new FakeStandardMaterial(name);
    m.alpha = this.alpha;
    m.disableLighting = this.disableLighting;
    m.frozen = this.frozen;
    return m;
  }
}

function fakeMesh(name) {
  return {
    name,
    position: vec3(),
    scaling: vec3(),
    material: null,
    isPickable: true,
    disposed: false,
    dispose() { this.disposed = true; },
    freezeWorldMatrix() { this.frozen = true; },
  };
}

class FakeMesh {
  constructor(name) {
    Object.assign(this, fakeMesh(name));
    createdMeshes.push(this);
  }
  static MergeMeshes(meshes) {
    meshes.forEach((m) => m.dispose());
    const merged = fakeMesh('merged');
    merged.sourceNames = meshes.map((m) => m.name);
    createdMeshes.push(merged);
    return merged;
  }
}

globalThis.window = {
  BABYLON: {
    Color3: FakeColor3,
    StandardMaterial: FakeStandardMaterial,
    Mesh: FakeMesh,
    VertexData: class { applyToMesh(mesh) { mesh.vertexData = this; } },
    MeshBuilder: {
      CreateBox: (name) => { const m = fakeMesh(name); createdBoxes.push(m); return m; },
    },
  },
};

const { ObstacleRenderer } = await import('../frontend/js/renderer/obstacles.js');

function makeEnv() {
  return {
    casters: [],
    shadowRefreshes: 0,
    roundCalls: [],
    addShadowCaster(m) { this.casters.push(m); },
    refreshShadows() { this.shadowRefreshes++; },
    setRoundObstacles(isolated, outlines) { this.roundCalls.push({ isolated, outlines }); },
  };
}

const LAYOUT = [
  { x: 0, y: 0, width: 40, height: 40 },   // cluster with next
  { x: 40, y: 0, width: 40, height: 80 },
  { x: 200, y: 200, width: 20, height: 20 }, // isolated
];

// --- with earcut available: one union prism pair + box path for the rest
{
  globalThis.earcut = (flat) => (flat.length >= 6 ? [0, 1, 2] : []);
  createdBoxes.length = 0;
  createdMeshes.length = 0;
  const env = makeEnv();
  const renderer = new ObstacleRenderer({}, env);
  renderer.update(LAYOUT, null);

  assert.equal(env.roundCalls.length, 1);
  assert.deepEqual(env.roundCalls[0].isolated, [LAYOUT[2]],
    'only isolated rects bake rect contact shadows');
  assert.equal(env.roundCalls[0].outlines.length, 1,
    'the cluster bakes one polygon outline shadow');
  assert.ok(env.roundCalls[0].outlines[0].outer.length === 6,
    'the shadow outline is the exact L union');

  const clusterBody = createdMeshes.find((m) => m.name === 'obstacleClusterBodies');
  const clusterTrim = createdMeshes.find((m) => m.name === 'obstacleClusterTrims');
  assert.ok(clusterBody && clusterBody.vertexData, 'cluster prisms build as one custom mesh');
  assert.ok(clusterTrim && clusterTrim.vertexData, 'cluster trims build as one custom mesh');
  assert.ok(clusterBody.frozen && clusterTrim.frozen);
  assert.equal(clusterBody.isPickable, false);
  assert.equal(clusterTrim.material.backFaceCulling, false,
    'the cluster trim renders both faces (single-layer caps, like the wall trim)');
  assert.ok(env.casters.includes(clusterBody), 'cluster prisms cast shadows');
  assert.ok(!env.casters.includes(clusterTrim), 'trims never cast');

  // Box path: only the isolated rect got pillar + edge + base boxes.
  assert.deepEqual(createdBoxes.map((b) => b.name), ['obs-0', 'obsEdge-0', 'obsBase-0'],
    'clustered rects must not take the box path');

  // 4 meshes total: merged box body + merged box trim + cluster body + trim.
  const teardown = renderer.collectTeardownMeshes();
  assert.equal(teardown.length, 4,
    'teardown must collect box merges AND cluster meshes');
  assert.ok(teardown.includes(clusterBody) && teardown.includes(clusterTrim));

  // Same layout again: fingerprint match, nothing rebuilt.
  const meshCount = createdMeshes.length;
  renderer.update(LAYOUT, null);
  assert.equal(createdMeshes.length, meshCount, 'identical keyframes must not rebuild');

  // New layout: previous cluster meshes dispose.
  renderer.update([{ x: 0, y: 0, width: 20, height: 20 }], null);
  assert.ok(clusterBody.disposed && clusterTrim.disposed,
    'a rebuild must dispose the previous cluster meshes');
  assert.equal(renderer.collectTeardownMeshes().length, 2,
    'a box-only layout leaves just the two merged box meshes');
}

// --- without earcut: everything falls back to the pre-#190 box path
{
  delete globalThis.earcut;
  createdBoxes.length = 0;
  createdMeshes.length = 0;
  const env = makeEnv();
  const renderer = new ObstacleRenderer({}, env);
  renderer.update(LAYOUT, null);
  assert.deepEqual(env.roundCalls[0].isolated, LAYOUT,
    'no earcut → every rect shadows as a rect');
  assert.equal(env.roundCalls[0].outlines.length, 0);
  assert.ok(!createdMeshes.some((m) => m.name === 'obstacleClusterBodies'),
    'no earcut → no union prisms (boxes instead of open-topped prisms)');
  assert.equal(createdBoxes.filter((b) => b.name.startsWith('obs-')).length, LAYOUT.length);
}

// --- rooftop detailing follows the clusters, not the raw rects
{
  globalThis.earcut = (flat) => (flat.length >= 6 ? [0, 1, 2] : []);
  const { setEffect } = await import('../frontend/js/settings.js');
  setEffect('arenaAmbience', 'obstacleDetailing', true);
  createdBoxes.length = 0;
  createdMeshes.length = 0;
  const renderer = new ObstacleRenderer({}, makeEnv());
  renderer.update(LAYOUT, null);
  const panels = createdBoxes.filter((b) => b.name.startsWith('obsTop-'));
  assert.equal(panels.length, 2,
    'one roof feature per structure: 1 isolated + 1 cluster (not one per member rect)');
  setEffect('arenaAmbience', 'obstacleDetailing', false);
  delete globalThis.earcut;
}

delete globalThis.window;

console.log('obstacle clusters: union-find grouping, exact rectilinear unions (holes included), single-trim geometry, and the renderer fallback paths hold');
