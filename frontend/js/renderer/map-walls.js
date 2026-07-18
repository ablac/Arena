'use strict';

/**
 * Smooth map-boundary walls (issue #186).
 *
 * Non-square map shapes ship their carved boundary as per-cell rectangles
 * (`mask_rects`, keyframe-gated exactly like `obstacles`). Rendering each
 * rect as its own trim-boxed pillar paints stripe seams at every cell seam
 * (the +1.5-oversized translucent trims overlap their neighbors) and turns
 * curved shapes into staircases. This module instead rasterizes the rects
 * back onto the terrain cell grid, extracts the region outline with a
 * marching-squares boundary trace (outer contours AND holes — the blocked
 * apron around the playfield has the playfield outline as its inner hole),
 * merges collinear runs, smooths the staircase, and extrudes one continuous
 * wall ribbon with a single unbroken glow trim along the top edge.
 *
 * Smoothing (issue #190, replacing the #186 Chaikin passes whose 1-cell
 * max-cut turned every staircase step into a periodic scallop): the contour
 * is resampled at uniform ~0.5-cell arc-length spacing, corners are
 * detected on a Gaussian-smoothed probe copy (windowed net-turn angle, so
 * staircase zigzags read as straight/curved runs while diamond tips,
 * rectilinear corners, and spiral arm tips read as genuinely sharp), the
 * corner samples are pinned in place, and everything between them gets a
 * windowed Gaussian blur of the vertex positions. Long staircase runs
 * converge onto their midline — a rasterized circle comes out visually
 * round — while pinned corners keep their exact staircase position. Every
 * vertex stays within MAX_SMOOTH_DEVIATION (half a cell) of the collision
 * staircase, so bots still read as stopping at the wall.
 *
 * Geometry conventions: world is y-up, the arena lies in the x/z plane, and
 * rect coords map to world directly (rect x → world x, rect y → world z),
 * matching how obstacles.js positions its boxes. Contours are traced with
 * the blocked region on the LEFT of the travel direction, so the empty
 * (playfield) side is always to the RIGHT — outer contours have positive
 * shoelace area, holes negative, and side faces get outward normals without
 * any per-contour winding fixups.
 *
 * The pure helpers below are window-free and exported so
 * scripts/test-map-walls-geometry.mjs can unit-test them in node.
 * @module renderer/map-walls
 */

/** Matches PILLAR_HEIGHT in obstacles.js — boundary boxes used that height. */
export const WALL_HEIGHT = 30;

/* ------------------------------------------------------------------------ */
/* Pure geometry helpers                                                     */
/* ------------------------------------------------------------------------ */

function gcdInt(a, b) {
  while (b) { const t = a % b; a = b; b = t; }
  return a;
}

/**
 * Infer the terrain cell size from the mask rects themselves: every rect the
 * server emits is an exact cell multiple in x/y/width/height (MaskToRects),
 * so the gcd of all values recovers the grid. Clamped to a sane [10, 40]
 * band with the config default (20) as the fallback for degenerate input.
 * @param {Array<{x:number,y:number,width:number,height:number}>} rects
 * @returns {number}
 */
export function inferCellSize(rects) {
  let g = 0;
  for (const r of rects) {
    for (const v of [r.x, r.y, r.width, r.height]) {
      const n = Math.round(Math.abs(v));
      if (n === 0) continue;
      g = g === 0 ? n : gcdInt(g, n);
    }
  }
  if (!g) return 20;
  // A gcd that came out as a whole multiple of the real cell (e.g. every
  // value even) still rasterizes exactly; halving keeps that property while
  // bringing the smoothing radius back into the tuned band.
  while (g > 40 && g % 2 === 0) g /= 2;
  if (g < 10 || g > 40) return 20;
  return g;
}

/**
 * Rasterize rects into a boolean cell grid.
 * @returns {{grid:Uint8Array,cols:number,rows:number,cellSize:number,originX:number,originY:number}}
 */
export function rasterizeMask(rects, cellSize) {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const r of rects) {
    minX = Math.min(minX, r.x);
    minY = Math.min(minY, r.y);
    maxX = Math.max(maxX, r.x + r.width);
    maxY = Math.max(maxY, r.y + r.height);
  }
  const originX = Math.floor(minX / cellSize) * cellSize;
  const originY = Math.floor(minY / cellSize) * cellSize;
  const cols = Math.max(1, Math.round((maxX - originX) / cellSize));
  const rows = Math.max(1, Math.round((maxY - originY) / cellSize));
  const grid = new Uint8Array(cols * rows);
  for (const r of rects) {
    const x0 = Math.max(0, Math.round((r.x - originX) / cellSize));
    const y0 = Math.max(0, Math.round((r.y - originY) / cellSize));
    const x1 = Math.min(cols, x0 + Math.max(1, Math.round(r.width / cellSize)));
    const y1 = Math.min(rows, y0 + Math.max(1, Math.round(r.height / cellSize)));
    for (let y = y0; y < y1; y++) {
      for (let x = x0; x < x1; x++) grid[y * cols + x] = 1;
    }
  }
  return { grid, cols, rows, cellSize, originX, originY };
}

/**
 * Marching-squares boundary trace over the cell grid. Emits closed contours
 * in cell-corner coordinates with the blocked region on the LEFT of travel:
 * outer region outlines wind with positive shoelace area, enclosed holes
 * (open pockets — e.g. the playfield inside the apron) with negative area.
 * The classic diagonal-adjacency ambiguity is resolved by always taking the
 * left-most turn, which keeps diagonally-touching cells on separate
 * contours instead of pinching them into a figure-eight.
 * @param {{grid:Uint8Array,cols:number,rows:number}} mask
 * @returns {Array<Array<[number,number]>>}
 */
export function extractContours(mask) {
  const { grid, cols, rows } = mask;
  const blocked = (x, y) => x >= 0 && y >= 0 && x < cols && y < rows && grid[y * cols + x] === 1;

  // Directed unit edges between blocked and open cells, keyed by start corner.
  const edges = new Map();
  const addEdge = (sx, sy, ex, ey) => {
    const key = sx + ',' + sy;
    let list = edges.get(key);
    if (!list) { list = []; edges.set(key, list); }
    list.push({ sx, sy, ex, ey, used: false });
  };
  for (let y = 0; y < rows; y++) {
    for (let x = 0; x < cols; x++) {
      if (!blocked(x, y)) continue;
      if (!blocked(x, y - 1)) addEdge(x, y, x + 1, y);
      if (!blocked(x + 1, y)) addEdge(x + 1, y, x + 1, y + 1);
      if (!blocked(x, y + 1)) addEdge(x + 1, y + 1, x, y + 1);
      if (!blocked(x - 1, y)) addEdge(x, y + 1, x, y);
    }
  }

  const contours = [];
  for (const list of edges.values()) {
    for (const first of list) {
      if (first.used) continue;
      const contour = [];
      let edge = first;
      while (!edge.used) {
        edge.used = true;
        contour.push([edge.sx, edge.sy]);
        const prevDx = edge.ex - edge.sx;
        const prevDy = edge.ey - edge.sy;
        const nextList = edges.get(edge.ex + ',' + edge.ey);
        let next = null;
        let bestTurn = -Infinity;
        if (nextList) {
          for (const cand of nextList) {
            if (cand.used) continue;
            const dx = cand.ex - cand.sx;
            const dy = cand.ey - cand.sy;
            const turn = Math.atan2(prevDx * dy - prevDy * dx, prevDx * dx + prevDy * dy);
            if (turn > bestTurn) { bestTurn = turn; next = cand; }
          }
        }
        if (!next) break; // back at the start — every corner has balanced degree
        edge = next;
      }
      if (contour.length >= 4) contours.push(contour);
    }
  }
  return contours;
}

/**
 * Drop points that lie on a straight run of a closed polygon.
 * @param {Array<[number,number]>} points
 * @returns {Array<[number,number]>}
 */
export function mergeCollinear(points) {
  const n = points.length;
  if (n < 3) return points.slice();
  const out = [];
  for (let i = 0; i < n; i++) {
    const prev = points[(i + n - 1) % n];
    const cur = points[i];
    const next = points[(i + 1) % n];
    const cross = (cur[0] - prev[0]) * (next[1] - cur[1]) - (cur[1] - prev[1]) * (next[0] - cur[0]);
    if (Math.abs(cross) > 1e-9) out.push(cur);
  }
  return out.length >= 3 ? out : points.slice();
}

/* --- smoothing contract constants (issue #190) — all in CELL units ------- */

/** Arc-length spacing of the resampled contour. On integer lattice contours
 *  the perimeter is an integer, so the effective spacing is EXACTLY 0.5 and
 *  every lattice corner (and every cell-edge midpoint) lands on a sample —
 *  detected corners pin at their true staircase position. */
export const RESAMPLE_SPACING = 0.5;
/** Gaussian sigma of the position blur. One cell kills the half-cell
 *  staircase zigzag (arc period 2 cells → amplitude attenuated ~140x) while
 *  a curve of radius R cells only shrinks by ~sigma^2/2R. */
export const SMOOTH_SIGMA = 1.0;
/** Blur window half-width, in sigmas. */
export const SMOOTH_WINDOW_SIGMAS = 2.5;
/** Corner probe: net turn is measured between the chords to the samples
 *  this many cells of arc behind/ahead. */
export const CORNER_PROBE_CELLS = 2;
/** Net turn (degrees) at or above which a sample is pinned as a corner.
 *  Sits between the sharpest curve that must STAY smooth (a 5-cell-radius
 *  circle turns ~46 deg across the probe; spiral corridors are gentler)
 *  and the shallowest genuine corner that must stay crisp (hexagon
 *  vertices, 60 deg nominal, measure ~57 after the probe blur;
 *  diamond/rectilinear corners measure ~86; spiral arm tips ~180). */
export const CORNER_TURN_DEG = 55;
/** Hard clamp on how far any smoothed vertex may move from its resampled
 *  position on the collision staircase. */
export const MAX_SMOOTH_DEVIATION = 0.5;

/**
 * Uniform arc-length resampling of a CLOSED polygon. Sample count is
 * round(perimeter / spacing), so on lattice contours (integer perimeter)
 * the spacing is exact and samples hit every lattice corner.
 * @param {Array<[number,number]>} points
 * @param {number} [spacing]
 * @returns {Array<[number,number]>}
 */
export function resampleClosed(points, spacing = RESAMPLE_SPACING) {
  const n = points.length;
  const segLen = [];
  let perimeter = 0;
  for (let i = 0; i < n; i++) {
    const [x0, y0] = points[i];
    const [x1, y1] = points[(i + 1) % n];
    const len = Math.hypot(x1 - x0, y1 - y0);
    segLen.push(len);
    perimeter += len;
  }
  if (!(perimeter > spacing * 4)) return points.map((p) => p.slice());
  const count = Math.max(8, Math.round(perimeter / spacing));
  const step = perimeter / count;
  const out = [];
  let seg = 0;
  let segStart = 0;
  for (let k = 0; k < count; k++) {
    const target = k * step;
    while (seg < n - 1 && segStart + segLen[seg] <= target - 1e-9) {
      segStart += segLen[seg];
      seg++;
    }
    const t = segLen[seg] > 1e-12 ? (target - segStart) / segLen[seg] : 0;
    const [x0, y0] = points[seg];
    const [x1, y1] = points[(seg + 1) % n];
    out.push([x0 + (x1 - x0) * t, y0 + (y1 - y0) * t]);
  }
  return out;
}

/**
 * Windowed Gaussian blur of a CLOSED resampled polyline's vertex positions.
 * Samples whose index is in `pinned` keep their exact position, and the
 * averaging window truncates at pinned samples (inclusive), so smoothing
 * never bleeds around a corner. Every output vertex is clamped to move at
 * most `maxDev` from its input position.
 * @param {Array<[number,number]>} samples uniformly spaced points
 * @param {Iterable<number>} [pinnedIdx] corner sample indices to pin
 * @returns {Array<[number,number]>}
 */
export function gaussianSmoothClosed(samples, pinnedIdx = [], {
  spacing = RESAMPLE_SPACING,
  sigma = SMOOTH_SIGMA,
  windowSigmas = SMOOTH_WINDOW_SIGMAS,
  maxDev = MAX_SMOOTH_DEVIATION,
} = {}) {
  const n = samples.length;
  const K = Math.max(1, Math.round((windowSigmas * sigma) / spacing));
  if (n < 2 * K + 2) return samples.map((p) => p.slice());
  const pinned = new Set(pinnedIdx);
  const weight = [];
  for (let d = 0; d <= K; d++) {
    weight.push(Math.exp(-((d * spacing) ** 2) / (2 * sigma * sigma)));
  }
  const out = new Array(n);
  for (let i = 0; i < n; i++) {
    if (pinned.has(i)) {
      out[i] = samples[i].slice();
      continue;
    }
    let sx = samples[i][0] * weight[0];
    let sy = samples[i][1] * weight[0];
    let sw = weight[0];
    for (const dir of [1, -1]) {
      for (let d = 1; d <= K; d++) {
        const j = (i + dir * d + n) % n;
        sx += samples[j][0] * weight[d];
        sy += samples[j][1] * weight[d];
        sw += weight[d];
        if (pinned.has(j)) break; // corner: include as endpoint, then stop
      }
    }
    let nx = sx / sw;
    let ny = sy / sw;
    const dx = nx - samples[i][0];
    const dy = ny - samples[i][1];
    const dev = Math.hypot(dx, dy);
    if (maxDev > 0 && dev > maxDev) {
      nx = samples[i][0] + (dx * maxDev) / dev;
      ny = samples[i][1] + (dy * maxDev) / dev;
    }
    out[i] = [nx, ny];
  }
  return out;
}

/**
 * Corner detection on a CLOSED resampled polyline. The turn is measured on
 * a fully Gaussian-smoothed probe copy (raw staircase zigzag would add
 * ~±27 deg of direction noise; on the probe copy the residual is <1 deg)
 * as the net change between the LOCAL tangent directions `probeCells` of
 * arc behind and ahead — local tangents capture ~95% of a blurred corner's
 * angle, where window chords would average it away. A staircase run has
 * ~zero NET turn regardless of its zigzag; a circle of radius R cells
 * turns 2*probe/R radians across the probe (R=5 → ~46 deg, stays smooth);
 * a 90-deg lattice corner measures ~86 deg and a 60-deg hexagon vertex
 * ~57 deg — above the 55-deg threshold, so they pin. Consecutive
 * above-threshold samples collapse to the single peak-turn sample.
 * @param {Array<[number,number]>} samples
 * @returns {number[]} sorted corner sample indices
 */
export function detectCorners(samples, {
  spacing = RESAMPLE_SPACING,
  probeCells = CORNER_PROBE_CELLS,
  thresholdDeg = CORNER_TURN_DEG,
  sigma = SMOOTH_SIGMA,
} = {}) {
  const n = samples.length;
  const w = Math.max(1, Math.round(probeCells / spacing));
  if (n < 2 * (w + 1) + 2) return [];
  const probe = gaussianSmoothClosed(samples, [], { spacing, sigma });
  const turn = new Float64Array(n);
  for (let i = 0; i < n; i++) {
    const a0 = probe[(i - w - 1 + n) % n];
    const a1 = probe[(i - w + 1 + n) % n];
    const c0 = probe[(i + w - 1) % n];
    const c1 = probe[(i + w + 1) % n];
    const ux = a1[0] - a0[0];
    const uy = a1[1] - a0[1];
    const vx = c1[0] - c0[0];
    const vy = c1[1] - c0[1];
    turn[i] = Math.abs(Math.atan2(ux * vy - uy * vx, ux * vx + uy * vy));
  }
  const threshold = (thresholdDeg * Math.PI) / 180;
  let start = -1;
  for (let i = 0; i < n; i++) {
    if (turn[i] < threshold) { start = i; break; }
  }
  if (start < 0) return []; // everything "sharp" — a tiny blob, leave smooth
  const corners = [];
  let runPeak = -1;
  for (let k = 1; k <= n; k++) {
    const i = (start + k) % n;
    if (turn[i] >= threshold) {
      if (runPeak < 0 || turn[i] > turn[runPeak]) runPeak = i;
    } else if (runPeak >= 0) {
      corners.push(runPeak);
      runPeak = -1;
    }
  }
  return corners.sort((a, b) => a - b);
}

/**
 * Full corner-preserving smoother for one closed lattice contour (issue
 * #190): resample → detect corners → pin them → Gaussian-smooth between
 * them. Replaces the Chaikin passes whose clamped cuts scalloped every
 * staircase step.
 * @param {Array<[number,number]>} points collinear-merged lattice contour
 * @returns {Array<[number,number]>}
 */
export function smoothContour(points, opts = {}) {
  const samples = resampleClosed(points, opts.spacing || RESAMPLE_SPACING);
  const corners = detectCorners(samples, opts);
  return gaussianSmoothClosed(samples, corners, opts);
}

/**
 * Signed shoelace area of a closed polygon. Positive = blocked-region outer
 * outline, negative = hole, per the extraction orientation.
 * @param {Array<[number,number]>} points
 * @returns {number}
 */
export function contourArea(points) {
  let sum = 0;
  const n = points.length;
  for (let i = 0; i < n; i++) {
    const [x0, y0] = points[i];
    const [x1, y1] = points[(i + 1) % n];
    sum += x0 * y1 - x1 * y0;
  }
  return sum / 2;
}

/**
 * Ray-cast point-in-polygon test.
 * @param {[number,number]} pt
 * @param {Array<[number,number]>} poly
 * @returns {boolean}
 */
export function pointInPolygon(pt, poly) {
  const [px, py] = pt;
  let inside = false;
  const n = poly.length;
  for (let i = 0, j = n - 1; i < n; j = i++) {
    const [xi, yi] = poly[i];
    const [xj, yj] = poly[j];
    if ((yi > py) !== (yj > py) && px < ((xj - xi) * (py - yi)) / (yj - yi) + xi) {
      inside = !inside;
    }
  }
  return inside;
}

/**
 * Pair each hole with the smallest outer contour containing it.
 * @param {Array<Array<[number,number]>>} contours
 * @returns {Array<{outer:Array<[number,number]>,holes:Array<Array<[number,number]>>}>}
 */
export function groupContours(contours) {
  const groups = [];
  const holes = [];
  for (const c of contours) {
    if (contourArea(c) > 0) groups.push({ outer: c, holes: [] });
    else holes.push(c);
  }
  groups.sort((a, b) => Math.abs(contourArea(a.outer)) - Math.abs(contourArea(b.outer)));
  for (const hole of holes) {
    // Probe with an edge midpoint: lattice corners of a hole can touch the
    // outer contour at a diagonal pinch, where corner-exact containment
    // tests are ambiguous.
    const probe = [(hole[0][0] + hole[1][0]) / 2, (hole[0][1] + hole[1][1]) / 2];
    const target = groups.find((g) => pointInPolygon(probe, g.outer));
    if (target) target.holes.push(hole);
  }
  return groups;
}

/**
 * Full pipeline: rects → cell grid → contours → collinear merge →
 * corner-preserving resample + Gaussian smoothing → world coordinates →
 * outer/hole grouping. Contours with fewer than 8 corners (plain
 * rectangles, single-cell features) skip smoothing so they stay crisp and
 * are never shaved smaller — this also keeps the apron's outer rectangle
 * hugging the arena edge exactly.
 * @param {Array<{x:number,y:number,width:number,height:number}>} rects
 * @returns {{cellSize:number,groups:Array<{outer:Array<[number,number]>,holes:Array<Array<[number,number]>>}>}}
 */
export function buildBoundaryContours(rects) {
  const cellSize = inferCellSize(rects);
  const mask = rasterizeMask(rects, cellSize);
  const contours = extractContours(mask);
  const world = [];
  for (const contour of contours) {
    const merged = mergeCollinear(contour);
    const pts = merged.length >= 8 ? smoothContour(merged) : merged;
    world.push(pts.map(([x, y]) => [x * cellSize + mask.originX, y * cellSize + mask.originY]));
  }
  return { cellSize, groups: groupContours(world) };
}

/** Per-point outward (empty-side) 2D normals, averaged across the two
 *  adjacent segments for smooth shading along curved walls. */
export function contourNormals(contour) {
  const n = contour.length;
  const seg = [];
  for (let i = 0; i < n; i++) {
    const [x0, y0] = contour[i];
    const [x1, y1] = contour[(i + 1) % n];
    const dx = x1 - x0;
    const dy = y1 - y0;
    const len = Math.hypot(dx, dy) || 1;
    seg.push([dy / len, -dx / len]); // right of travel = empty side
  }
  const out = [];
  for (let i = 0; i < n; i++) {
    const a = seg[(i + n - 1) % n];
    const b = seg[i];
    let nx = a[0] + b[0];
    let ny = a[1] + b[1];
    const len = Math.hypot(nx, ny);
    if (len < 1e-6) { nx = b[0]; ny = b[1]; } else { nx /= len; ny /= len; }
    out.push([nx, ny]);
  }
  return out;
}

function appendSideRibbon(contour, height, positions, normals, indices) {
  const n = contour.length;
  const norms = contourNormals(contour);
  const base = positions.length / 3;
  for (let i = 0; i < n; i++) {
    const [x, z] = contour[i];
    const [nx, nz] = norms[i];
    positions.push(x, 0, z, x, height, z);
    normals.push(nx, 0, nz, nx, 0, nz);
  }
  for (let i = 0; i < n; i++) {
    const a = base + i * 2;
    const b = base + ((i + 1) % n) * 2;
    indices.push(a, a + 1, b, b, a + 1, b + 1);
  }
}

function isConvex(contour) {
  // Positive-area orientation: every turn must be non-right (cross >= -eps).
  const n = contour.length;
  for (let i = 0; i < n; i++) {
    const [x0, y0] = contour[i];
    const [x1, y1] = contour[(i + 1) % n];
    const [x2, y2] = contour[(i + 2) % n];
    if ((x1 - x0) * (y2 - y1) - (y1 - y0) * (x2 - x1) < -1e-6) return false;
  }
  return true;
}

function appendConvexFanCap(contour, height, positions, normals, indices) {
  const n = contour.length;
  let cx = 0, cz = 0;
  for (const [x, z] of contour) { cx += x; cz += z; }
  cx /= n; cz /= n;
  const base = positions.length / 3;
  positions.push(cx, height, cz);
  normals.push(0, 1, 0);
  for (const [x, z] of contour) {
    positions.push(x, height, z);
    normals.push(0, 1, 0);
  }
  for (let i = 0; i < n; i++) {
    indices.push(base, base + 1 + i, base + 1 + ((i + 1) % n));
  }
}

function appendCapRing(contour, height, capWidth, positions, normals, indices) {
  const n = contour.length;
  const norms = contourNormals(contour);
  const base = positions.length / 3;
  for (let i = 0; i < n; i++) {
    const [x, z] = contour[i];
    // Inward = away from the empty side. Overlap on thin features is
    // harmless: coplanar same-material triangles shade identically.
    positions.push(x, height, z, x - norms[i][0] * capWidth, height, z - norms[i][1] * capWidth);
    normals.push(0, 1, 0, 0, 1, 0);
  }
  for (let i = 0; i < n; i++) {
    const a = base + i * 2;
    const b = base + ((i + 1) % n) * 2;
    indices.push(a, a + 1, b, b, a + 1, b + 1);
  }
}

function appendFullCap(group, height, earcutFn, positions, normals, indices) {
  const flat = [];
  const holeIdx = [];
  for (const [x, z] of group.outer) flat.push(x, z);
  for (const hole of group.holes) {
    holeIdx.push(flat.length / 2);
    for (const [x, z] of hole) flat.push(x, z);
  }
  const tri = earcutFn(flat, holeIdx.length ? holeIdx : null, 2);
  const base = positions.length / 3;
  for (let i = 0; i < flat.length; i += 2) {
    positions.push(flat[i], height, flat[i + 1]);
    normals.push(0, 1, 0);
  }
  for (const idx of tri) indices.push(base + idx);
}

/**
 * Wall body geometry for all groups: a vertical side ribbon per contour plus
 * a top cap. With an earcut function the cap is a full triangulation of the
 * region (contour with holes); without one it falls back to an inward ring
 * strip ~2 cells wide (hole-free convex contours — circle/donut cores — get
 * an exact centroid fan instead). The deep interior of the blocked apron is
 * far from the camera focus and the dark body material hides it.
 * @param {Array<{outer:Array<[number,number]>,holes:Array<Array<[number,number]>>}>} groups
 * @param {{height?:number,capWidth?:number,earcutFn?:Function|null}} [opts]
 * @returns {{positions:number[],normals:number[],indices:number[]}}
 */
export function buildWallGeometry(groups, { height = WALL_HEIGHT, capWidth = 40, earcutFn = null } = {}) {
  const positions = [];
  const normals = [];
  const indices = [];
  for (const group of groups) {
    const contours = [group.outer, ...group.holes];
    for (const contour of contours) {
      appendSideRibbon(contour, height, positions, normals, indices);
    }
    if (earcutFn) {
      appendFullCap(group, height, earcutFn, positions, normals, indices);
    } else if (!group.holes.length && isConvex(group.outer)) {
      appendConvexFanCap(group.outer, height, positions, normals, indices);
    } else {
      for (const contour of contours) {
        appendCapRing(contour, height, capWidth, positions, normals, indices);
      }
    }
  }
  return { positions, normals, indices };
}

/**
 * One continuous glow-trim ribbon per contour: a flat band straddling the
 * top edge (±1.5, matching the old +1.5-oversized trim boxes) plus a short
 * outer skirt so the trim still reads from low camera angles.
 * @param {Array<{outer:Array<[number,number]>,holes:Array<Array<[number,number]>>}>} groups
 * @param {{height?:number}} [opts]
 * @returns {{positions:number[],normals:number[],indices:number[]}}
 */
export function buildTrimGeometry(groups, { height = WALL_HEIGHT } = {}) {
  const OUT = 1.5;   // band reach past the wall face (old trim oversize)
  const INN = 1.5;   // band reach onto the cap
  const LIFT = 0.8;  // band height above the cap (old trim center)
  const SKIRT = 2.4; // outer skirt drop below the band
  const positions = [];
  const normals = [];
  const indices = [];
  const quadStrip = (n, base) => {
    for (let i = 0; i < n; i++) {
      const a = base + i * 2;
      const b = base + ((i + 1) % n) * 2;
      indices.push(a, a + 1, b, b, a + 1, b + 1);
    }
  };
  for (const group of groups) {
    for (const contour of [group.outer, ...group.holes]) {
      const n = contour.length;
      const norms = contourNormals(contour);
      const y = height + LIFT;
      let base = positions.length / 3;
      for (let i = 0; i < n; i++) {
        const [x, z] = contour[i];
        const [nx, nz] = norms[i];
        positions.push(x + nx * OUT, y, z + nz * OUT, x - nx * INN, y, z - nz * INN);
        normals.push(0, 1, 0, 0, 1, 0);
      }
      quadStrip(n, base);
      base = positions.length / 3;
      for (let i = 0; i < n; i++) {
        const [x, z] = contour[i];
        const [nx, nz] = norms[i];
        positions.push(x + nx * OUT, y + 1.0, z + nz * OUT, x + nx * OUT, y - SKIRT, z + nz * OUT);
        normals.push(nx, 0, nz, nx, 0, nz);
      }
      quadStrip(n, base);
    }
  }
  return { positions, normals, indices };
}

/**
 * Locate an earcut triangulator. Babylon's UMD bundle references a bare
 * `earcut` global for PolygonMeshBuilder but does NOT ship the
 * implementation (verified against babylonjs@9.14.0 — default-argument
 * references only). Both site shells load earcut@2.2.4 via a CDN script
 * tag, so in production this resolves the global and full top caps are the
 * live path; the ring-cap fallback only covers a failed CDN fetch.
 * @returns {Function|null}
 */
export function resolveEarcut() {
  if (typeof earcut === 'function') return earcut;
  const g = typeof globalThis !== 'undefined'
    ? globalThis
    : (typeof window !== 'undefined' ? window : null);
  if (g && typeof g.earcut === 'function') return g.earcut;
  if (g && g.BABYLON && typeof g.BABYLON.earcut === 'function') return g.BABYLON.earcut;
  return null;
}

/* ------------------------------------------------------------------------ */
/* Babylon-facing renderer                                                   */
/* ------------------------------------------------------------------------ */

/**
 * Owns the boundary wall meshes. Driven by ObstacleRenderer on the same
 * fingerprint/round-boundary rebuild trigger as the merged pillars, so the
 * whole boundary is exactly two draw calls: body (sides + cap, shares the
 * palette-tinted obstacle body material) and trim (double-sided clone of the
 * obstacle trim material, so map palettes from #182 apply). The trim glows
 * via the engine GlowLayer like every other emissive trim; the body is
 * excluded (issue #181 pattern for wall bodies).
 */
export class MapWallsRenderer {
  /** @param {BABYLON.Scene} scene @param {EnvironmentRenderer} [envRenderer] */
  constructor(scene, envRenderer) {
    this.scene = scene;
    this._env = envRenderer || null;
    this._glowLayer = null;
    /** @type {BABYLON.Mesh|null} side ribbons + top cap (one draw call) */
    this._bodyMesh = null;
    /** @type {BABYLON.Mesh|null} glow trim band + skirt (one draw call) */
    this._trimMesh = null;
    this._trimMat = null;
  }

  /** Wire the engine's GlowLayer so wall bodies can be excluded from glow. */
  setGlowLayer(glow) {
    this._glowLayer = glow || null;
    if (this._glowLayer && this._bodyMesh) {
      this._glowLayer.addExcludedMesh(this._bodyMesh);
    }
  }

  /**
   * Rebuild the boundary walls. Pass null/empty to clear (setting off,
   * square map, old server). bodyMat is the shared palette-tinted obstacle
   * body material; trimSourceMat supplies the palette-tinted trim emissive
   * copied onto this module's double-sided trim clone.
   */
  build(maskRects, bodyMat, trimSourceMat) {
    this.clear();
    if (!maskRects || !maskRects.length) return;
    const B = window.BABYLON;
    const { cellSize, groups } = buildBoundaryContours(maskRects);
    if (!groups.length) return;

    const wall = buildWallGeometry(groups, {
      height: WALL_HEIGHT,
      capWidth: cellSize * 2,
      earcutFn: resolveEarcut(),
    });
    const body = new B.Mesh('mapWallBody', this.scene);
    const bodyData = new B.VertexData();
    bodyData.positions = wall.positions;
    bodyData.normals = wall.normals;
    bodyData.indices = wall.indices;
    bodyData.applyToMesh(body);
    body.material = bodyMat;
    body.isPickable = false;
    body.freezeWorldMatrix();
    if (this._env && this._env.addShadowCaster) this._env.addShadowCaster(body);
    if (this._glowLayer) this._glowLayer.addExcludedMesh(body);
    this._bodyMesh = body;

    const trimGeo = buildTrimGeometry(groups, { height: WALL_HEIGHT });
    const trim = new B.Mesh('mapWallTrim', this.scene);
    const trimData = new B.VertexData();
    trimData.positions = trimGeo.positions;
    trimData.normals = trimGeo.normals;
    trimData.indices = trimGeo.indices;
    trimData.applyToMesh(trim);
    trim.material = this._syncTrimMaterial(trimSourceMat);
    trim.isPickable = false;
    trim.freezeWorldMatrix();
    this._trimMesh = trim;
  }

  /** @private Clone-or-retint the trim material from the obstacle trim. The
   *  ribbon is single-layer open geometry, so the clone disables backface
   *  culling (the shared box-trim material culls). */
  _syncTrimMaterial(source) {
    if (!this._trimMat) {
      // The clone inherits the source's frozen state — unfreeze before
      // flipping culling so the change isn't swallowed by the freeze cache.
      this._trimMat = source.clone('mapWallTrimMat');
      this._trimMat.unfreeze();
      this._trimMat.backFaceCulling = false;
    } else {
      this._trimMat.unfreeze();
      this._trimMat.emissiveColor.copyFrom(source.emissiveColor);
    }
    this._trimMat.freeze();
    return this._trimMat;
  }

  /** Dispose the wall meshes (round reset / rebuild / setting off). */
  clear() {
    if (this._bodyMesh) {
      if (this._glowLayer && this._glowLayer.removeExcludedMesh) {
        this._glowLayer.removeExcludedMesh(this._bodyMesh);
      }
      this._bodyMesh.dispose();
      this._bodyMesh = null;
    }
    if (this._trimMesh) {
      this._trimMesh.dispose();
      this._trimMesh = null;
    }
  }
}
