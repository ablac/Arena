'use strict';

/**
 * Interior-obstacle cluster unification (issue #190).
 *
 * Obstacle rects arrive grid-snapped from the server (VisualObstacles), so
 * neighbors routinely share edges or overlap after the bot-radius expansion.
 * The old per-rect build gave each rect its own +1.5-oversized translucent
 * trim box; wherever rects touched, a trim intersected the neighbor's body —
 * bright patches and seam lines. This module groups rects into
 * touch/overlap clusters, recovers the EXACT rectilinear union outline of a
 * cluster by rasterizing its rects onto their gcd grid and reusing the
 * tested marching-squares contour trace from map-walls.js (holes included —
 * a ring of rects yields an outline plus its courtyard), and emits the
 * geometry for ONE prism per cluster with a SINGLE continuous trim along
 * the union outline. No interior faces, no overlapping trims; the footprint
 * is byte-identical to the collision rects. Isolated rects keep the cheap
 * box path in obstacles.js, and everything here is render-only.
 *
 * The pure helpers are window-free and exported so
 * scripts/test-obstacle-clusters.mjs can unit-test them in node.
 * @module renderer/obstacle-clusters
 */

import { rasterizeMask, extractContours, mergeCollinear, groupContours, contourNormals } from './map-walls.js?v=20260718g';

/** Half the old +1.5 trim-box oversize: how far the unified trim ring
 *  overhangs each cluster face (the old boxes grew width/depth by 1.5
 *  TOTAL, i.e. 0.75 per side). */
export const TRIM_OVERHANG = 0.75;
/** Old top trim box: height 2, centered 0.8 above the roof. */
export const TRIM_TOP_HEIGHT = 2;
export const TRIM_TOP_LIFT = 0.8;
/** Old base trim box: height 1.5, sitting on the floor. */
export const TRIM_BASE_HEIGHT = 1.5;
/** Rasterization guard: clusters bigger than this many cells on their gcd
 *  grid fall back to the per-rect box path. */
export const MAX_CLUSTER_CELLS = 262144;

function gcdInt(a, b) {
  while (b) { const t = a % b; a = b; b = t; }
  return a;
}

/**
 * Group rects into connected clusters by touch/overlap (closed-interval
 * AABB test, so shared edges AND shared corners connect). Union-find keeps
 * it deterministic: clusters come out ordered by their smallest member
 * index, members ascending — rebuilding the same layout always yields the
 * same grouping (rooftop detailing hashes depend on it).
 * @param {Array<{x:number,y:number,width:number,height:number}>} rects
 * @returns {number[][]} arrays of indices into `rects`
 */
export function clusterObstacles(rects, eps = 1e-6) {
  const n = rects.length;
  const parent = Array.from({ length: n }, (_, i) => i);
  const find = (i) => {
    while (parent[i] !== i) { parent[i] = parent[parent[i]]; i = parent[i]; }
    return i;
  };
  const touches = (a, b) =>
    a.x <= b.x + b.width + eps && b.x <= a.x + a.width + eps &&
    a.y <= b.y + b.height + eps && b.y <= a.y + a.height + eps;
  for (let i = 0; i < n; i++) {
    for (let j = i + 1; j < n; j++) {
      if (touches(rects[i], rects[j])) {
        const a = find(i);
        const b = find(j);
        if (a !== b) parent[Math.max(a, b)] = Math.min(a, b);
      }
    }
  }
  const byRoot = new Map();
  for (let i = 0; i < n; i++) {
    const root = find(i);
    let list = byRoot.get(root);
    if (!list) { list = []; byRoot.set(root, list); }
    list.push(i);
  }
  return [...byRoot.values()];
}

/**
 * Exact rectilinear union outline(s) of one cluster's rects: rasterize on
 * the gcd grid of all rect coordinates (every value is divisible by the gcd
 * by construction, so the raster reproduces the union exactly), trace with
 * the map-walls marching squares, merge collinear runs, and pair holes with
 * their outers. NO smoothing — the prism must match collision exactly.
 * Returns null when the rects are not integer-aligned (a pre-grid-snap
 * server) or the raster would be unreasonably large; the caller then keeps
 * the per-rect box path for those rects.
 * @param {Array<{x:number,y:number,width:number,height:number}>} rects
 * @returns {{cellSize:number,groups:Array<{outer:Array<[number,number]>,holes:Array<Array<[number,number]>>}>}|null}
 */
export function computeClusterOutline(rects) {
  if (!rects || !rects.length) return null;
  let g = 0;
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const r of rects) {
    for (const v of [r.x, r.y, r.width, r.height]) {
      if (!Number.isFinite(v) || Math.abs(v - Math.round(v)) > 1e-6) return null;
      const n = Math.round(Math.abs(v));
      if (n) g = g ? gcdInt(g, n) : n;
    }
    minX = Math.min(minX, r.x);
    minY = Math.min(minY, r.y);
    maxX = Math.max(maxX, r.x + r.width);
    maxY = Math.max(maxY, r.y + r.height);
  }
  if (!g) return null;
  if (((maxX - minX) / g) * ((maxY - minY) / g) > MAX_CLUSTER_CELLS) return null;
  const mask = rasterizeMask(rects, g);
  const world = extractContours(mask).map((c) =>
    mergeCollinear(c).map(([x, y]) => [x * g + mask.originX, y * g + mask.originY]));
  const groups = groupContours(world);
  return groups.length ? { cellSize: g, groups } : null;
}

/**
 * Offset a closed contour outward (toward the empty side) by `dist` with
 * exact miters: the per-vertex offset satisfies m·n̂ = dist against BOTH
 * adjacent segment normals, so a rectilinear outline offsets to the same
 * shape the old oversized trim boxes traced. 180-degree spikes fall back to
 * the single segment normal.
 * @param {Array<[number,number]>} contour
 * @param {number} dist
 * @returns {Array<[number,number]>}
 */
export function offsetContour(contour, dist) {
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
    const mx = a[0] + b[0];
    const my = a[1] + b[1];
    const len2 = mx * mx + my * my;
    if (len2 < 1e-12) {
      out.push([contour[i][0] + b[0] * dist, contour[i][1] + b[1] * dist]);
      continue;
    }
    const s = (2 * dist) / len2; // m = 2d(a+b)/|a+b|^2 → m·a = m·b = d
    out.push([contour[i][0] + mx * s, contour[i][1] + my * s]);
  }
  return out;
}

/** Concatenate one indexed-triangle geometry onto another in place. */
export function appendGeometry(target, source) {
  const base = target.positions.length / 3;
  for (const v of source.positions) target.positions.push(v);
  for (const v of source.normals) target.normals.push(v);
  for (const idx of source.indices) target.indices.push(base + idx);
}

/** @private Vertical ring between y0..y1 along a closed contour, with
 *  smooth outward shading normals. */
function appendRing(geo, contour, y0, y1) {
  const n = contour.length;
  const norms = contourNormals(contour);
  const base = geo.positions.length / 3;
  for (let i = 0; i < n; i++) {
    const [x, z] = contour[i];
    const [nx, nz] = norms[i];
    geo.positions.push(x, y0, z, x, y1, z);
    geo.normals.push(nx, 0, nz, nx, 0, nz);
  }
  for (let i = 0; i < n; i++) {
    const a = base + i * 2;
    const b = base + ((i + 1) % n) * 2;
    geo.indices.push(a, b, a + 1, b, b + 1, a + 1);
  }
}

/** @private Flat cap over outer-with-holes at height y (earcut). */
function appendCap(geo, outer, holes, y, earcutFn) {
  const flat = [];
  const holeIdx = [];
  for (const [x, z] of outer) flat.push(x, z);
  for (const hole of holes) {
    holeIdx.push(flat.length / 2);
    for (const [x, z] of hole) flat.push(x, z);
  }
  const tri = earcutFn(flat, holeIdx.length ? holeIdx : null, 2);
  const base = geo.positions.length / 3;
  for (let i = 0; i < flat.length; i += 2) {
    geo.positions.push(flat[i], y, flat[i + 1]);
    geo.normals.push(0, 1, 0);
  }
  for (const idx of tri) geo.indices.push(base + idx);
}

/**
 * The SINGLE continuous trim for a cluster: for each contour group, the
 * union outline offset outward by TRIM_OVERHANG, extruded into two bands
 * that reproduce the old per-rect trim boxes — a 2-high band wrapping the
 * roof edge (bottom at height-0.2, translucent film cap over the roof at
 * height+1.8) and a 1.5-high base ring on the floor. Rendered with the
 * culling-disabled trim clone (like the map-wall trim ribbon), so bottom
 * caps are unnecessary: the top caps' backfaces cover the underside view.
 * @param {Array<{outer:Array<[number,number]>,holes:Array<Array<[number,number]>>}>} groups
 * @param {{height:number,earcutFn:Function}} opts
 * @returns {{positions:number[],normals:number[],indices:number[]}}
 */
export function buildClusterTrimGeometry(groups, { height, earcutFn }) {
  const geo = { positions: [], normals: [], indices: [] };
  const yTop0 = height + TRIM_TOP_LIFT - TRIM_TOP_HEIGHT / 2;
  const yTop1 = height + TRIM_TOP_LIFT + TRIM_TOP_HEIGHT / 2;
  for (const group of groups) {
    const outer = offsetContour(group.outer, TRIM_OVERHANG);
    const holes = group.holes.map((h) => offsetContour(h, TRIM_OVERHANG));
    for (const contour of [outer, ...holes]) {
      appendRing(geo, contour, yTop0, yTop1);
      appendRing(geo, contour, 0, TRIM_BASE_HEIGHT);
    }
    if (earcutFn) {
      appendCap(geo, outer, holes, yTop1, earcutFn);
      appendCap(geo, outer, holes, TRIM_BASE_HEIGHT, earcutFn);
    }
  }
  return geo;
}
