import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  WALL_HEIGHT,
  inferCellSize,
  rasterizeMask,
  extractContours,
  mergeCollinear,
  chaikinSmooth,
  contourArea,
  buildBoundaryContours,
  buildWallGeometry,
  buildTrimGeometry,
  MapWallsRenderer,
} from '../frontend/js/renderer/map-walls.js';

// --- module identity: the wall height must match the pillar height the
// boundary boxes used, and the importer chain must carry the new stamp.
const obstaclesSource = readFileSync(new URL('../frontend/js/renderer/obstacles.js', import.meta.url), 'utf8');
const engineSource = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
const settingsSource = readFileSync(new URL('../frontend/js/settings.js', import.meta.url), 'utf8');
const pillarMatch = obstaclesSource.match(/const PILLAR_HEIGHT = (\d+);/);
assert.ok(pillarMatch, 'obstacles.js must declare PILLAR_HEIGHT');
assert.equal(WALL_HEIGHT, Number(pillarMatch[1]),
  'smooth boundary walls must match the boundary-box pillar height');
assert.match(obstaclesSource, /map-walls\.js\?v=20260718d/,
  'obstacles.js must import the stamped map-walls module');
assert.match(engineSource, /obstacles\.js\?v=20260718d/,
  'engine.js must invalidate the cached pre-#186 obstacle renderer');
assert.match(engineSource, /state\.mask_rects/,
  'engine.js must feed mask_rects from spectator keyframes');
assert.match(settingsSource, /smoothMapWalls/,
  'the smoothMapWalls toggle must exist in SETTINGS_SCHEMA');

// --- cell size inference: gcd of grid-aligned rects, clamped, defaulted.
assert.equal(inferCellSize([{ x: 40, y: 60, width: 20, height: 100 }]), 20);
assert.equal(inferCellSize([{ x: 80, y: 0, width: 40, height: 120 }]), 40);
assert.equal(inferCellSize([{ x: 0, y: 0, width: 7, height: 13 }]), 20,
  'non-grid rects must fall back to the config default cell size');
assert.equal(inferCellSize([]), 20);

// --- single rect: rasterize + trace yields one rectangle contour.
{
  const mask = rasterizeMask([{ x: 0, y: 0, width: 60, height: 40 }], 20);
  assert.equal(mask.cols, 3);
  assert.equal(mask.rows, 2);
  const contours = extractContours(mask);
  assert.equal(contours.length, 1, 'one rect must trace to one closed contour');
  const merged = mergeCollinear(contours[0]);
  assert.equal(merged.length, 4, 'a rectangle must merge to 4 corners');
  assert.ok(contourArea(merged) > 0, 'blocked-region outlines must have positive area');
  assert.equal(contourArea(merged), 6, 'shoelace area must equal the cell count');
}

// --- collinear merge drops redundant lattice points only.
{
  const square = [[0, 0], [1, 0], [2, 0], [2, 1], [2, 2], [1, 2], [0, 2], [0, 1]];
  assert.deepEqual(mergeCollinear(square), [[0, 0], [2, 0], [2, 2], [0, 2]]);
}

// --- Chaikin: point counts double per pass, output stays inside the input
// bounds, and the clamp caps the cut distance on long runs.
{
  const square = [[0, 0], [10, 0], [10, 10], [0, 10]];
  const once = chaikinSmooth(square, 1, 1);
  assert.equal(once.length, 8);
  // maxCut=1 on a 10-long edge: cut points sit exactly 1 unit from corners.
  assert.deepEqual(once[0], [1, 0]);
  assert.deepEqual(once[1], [9, 0]);
  const twice = chaikinSmooth(square, 2, 1);
  assert.equal(twice.length, 16);
  for (const [x, y] of twice) {
    assert.ok(x >= 0 && x <= 10 && y >= 0 && y <= 10, 'smoothed points must stay in bounds');
  }
}

// --- diamond apron: an 8x8 grid blocked outside a diamond. The pipeline
// must produce one group whose outer contour is the crisp arena rectangle
// (4 points, unsmoothed) and whose single hole is the smoothed playfield
// outline, staying within the blocked staircase by a modest margin.
{
  const cell = 20;
  const N = 8;
  const inDiamond = (x, y) => Math.abs(x - 3.5) + Math.abs(y - 3.5) <= 3.5;
  const rects = [];
  let openCells = 0;
  for (let y = 0; y < N; y++) {
    let x = 0;
    while (x < N) {
      if (inDiamond(x, y)) { openCells++; x++; continue; }
      const x0 = x;
      while (x < N && !inDiamond(x, y)) x++;
      rects.push({ x: x0 * cell, y: y * cell, width: (x - x0) * cell, height: cell });
    }
  }
  const { cellSize, groups } = buildBoundaryContours(rects);
  assert.equal(cellSize, 20);
  assert.equal(groups.length, 1, 'the apron must form exactly one region');
  const [group] = groups;
  assert.equal(group.outer.length, 4, 'the outer rectangle must stay crisp (unsmoothed)');
  const xs = group.outer.map((p) => p[0]);
  const zs = group.outer.map((p) => p[1]);
  assert.equal(Math.min(...xs), 0);
  assert.equal(Math.max(...xs), N * cell);
  assert.equal(Math.min(...zs), 0);
  assert.equal(Math.max(...zs), N * cell);
  assert.equal(group.holes.length, 1, 'the playfield outline must be the inner hole');
  const hole = group.holes[0];
  assert.ok(hole.length >= 16, 'the staircase hole must have been Chaikin-subdivided');
  assert.ok(contourArea(hole) < 0, 'holes must wind opposite to outers');
  const holeArea = Math.abs(contourArea(hole));
  const openArea = openCells * cell * cell;
  assert.ok(holeArea <= openArea + 1e-6,
    `smoothing must not grow the playfield outline (${holeArea} > ${openArea})`);
  assert.ok(holeArea >= 0.7 * openArea,
    `smoothing must not over-shrink the playfield outline (${holeArea} < 0.7 * ${openArea})`);
  for (const [x, z] of hole) {
    assert.ok(x >= cell - 1e-6 && x <= (N - 1) * cell + 1e-6 &&
      z >= cell - 1e-6 && z <= (N - 1) * cell + 1e-6,
      'the smoothed outline must stay within the staircase envelope');
  }

  // Wall geometry (ring-cap fallback — production path: earcut is absent
  // from the babylonjs UMD bundle): valid indexed triangles, every vertex at
  // floor height, wall height, or the trim band around it.
  const wall = buildWallGeometry(groups, { height: WALL_HEIGHT, capWidth: cellSize * 2, earcutFn: null });
  assert.equal(wall.positions.length % 3, 0);
  assert.equal(wall.positions.length, wall.normals.length);
  assert.equal(wall.indices.length % 3, 0);
  const vertexCount = wall.positions.length / 3;
  for (const idx of wall.indices) {
    assert.ok(Number.isInteger(idx) && idx >= 0 && idx < vertexCount, 'indices must address real vertices');
  }
  for (let i = 1; i < wall.positions.length; i += 3) {
    const y = wall.positions[i];
    assert.ok(y === 0 || y === WALL_HEIGHT, `wall vertices must sit at floor or cap height (got ${y})`);
  }

  // Full-cap path with an earcut stand-in still emits valid geometry:
  // 2 side vertices per contour point plus 1 flat-cap vertex per point.
  const earcutStub = (flat) => (flat.length >= 6 ? [0, 1, 2] : []);
  const capped = buildWallGeometry(groups, { height: WALL_HEIGHT, earcutFn: earcutStub });
  assert.ok(capped.indices.length > 0);
  const totalPts = group.outer.length + hole.length;
  assert.equal(capped.positions.length / 3, totalPts * 3,
    'the earcut path must add one cap vertex per contour point');

  const trim = buildTrimGeometry(groups, { height: WALL_HEIGHT });
  assert.equal(trim.positions.length % 3, 0);
  assert.equal(trim.positions.length, trim.normals.length);
  const trimVertexCount = trim.positions.length / 3;
  for (const idx of trim.indices) {
    assert.ok(Number.isInteger(idx) && idx >= 0 && idx < trimVertexCount);
  }
  for (let i = 1; i < trim.positions.length; i += 3) {
    const y = trim.positions[i];
    assert.ok(y > WALL_HEIGHT - 3 && y < WALL_HEIGHT + 3,
      `trim vertices must hug the wall top edge (got ${y})`);
  }

  // Renderer contract with a stubbed BABYLON: the whole boundary is exactly
  // two meshes (body + trim), the body is a shadow caster excluded from the
  // glow layer, and a rebuild disposes the previous pair.
  const created = [];
  class FakeMesh {
    constructor(name) { this.name = name; this.disposed = false; created.push(this); }
    dispose() { this.disposed = true; }
    freezeWorldMatrix() { this.frozen = true; }
  }
  class FakeVertexData {
    applyToMesh(mesh) { mesh.vertexData = this; }
  }
  globalThis.window = { BABYLON: { Mesh: FakeMesh, VertexData: FakeVertexData } };
  const casters = [];
  const env = { addShadowCaster: (m) => casters.push(m), refreshShadows: () => {} };
  const excluded = new Set();
  const glow = {
    addExcludedMesh: (m) => excluded.add(m),
    removeExcludedMesh: (m) => excluded.delete(m),
  };
  const trimSource = {
    emissiveColor: { copyFrom() {} },
    clone: () => ({ backFaceCulling: true, freeze() {}, unfreeze() {}, emissiveColor: { copyFrom() {} } }),
  };
  const walls = new MapWallsRenderer({}, env);
  walls.setGlowLayer(glow);
  walls.build(rects, { name: 'bodyMat' }, trimSource);
  assert.equal(created.length, 2, 'the boundary must build exactly two meshes (body + trim)');
  const [bodyMesh, trimMesh] = created;
  assert.equal(bodyMesh.name, 'mapWallBody');
  assert.equal(trimMesh.name, 'mapWallTrim');
  assert.ok(bodyMesh.frozen && trimMesh.frozen, 'wall meshes must freeze their world matrices');
  assert.deepEqual(casters, [bodyMesh], 'only the wall body casts shadows');
  assert.ok(excluded.has(bodyMesh), 'the wall body must not feed the glow pass');
  assert.equal(trimMesh.material.backFaceCulling, false, 'the trim ribbon must render both faces');
  walls.build(rects, { name: 'bodyMat' }, trimSource);
  assert.ok(bodyMesh.disposed && trimMesh.disposed, 'a rebuild must dispose the previous meshes');
  assert.ok(!excluded.has(bodyMesh), 'disposing the body must drop its glow exclusion');
  assert.equal(created.length, 4);
  walls.clear();
  assert.ok(created[2].disposed && created[3].disposed);
  delete globalThis.window;
}

console.log('map-walls geometry: contours, Chaikin bounds, ring/earcut caps, and the 2-mesh boundary contract hold');
