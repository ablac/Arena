'use strict';

/**
 * Movement trail system — smooth fading ribbons behind moving bots.
 * Samples bot visual positions and updates reusable ribbon meshes in place.
 * Uses updatable ribbons to avoid per-frame mesh allocation (memory leak fix).
 * @module renderer/trails
 */

import { isEnabled } from '../settings.js';

const MAX_HISTORY = 12;
// Spectator positions arrive at 10 Hz. Sampling three times faster rebuilt the
// same ribbon geometry from interpolated points without adding useful truth.
const SAMPLE_INTERVAL = 0.1;
const MAX_RENDERED_TRAILS = 48;
const TRAIL_WIDTH = 6;
const TRAIL_Y = 0.4;

export class TrailRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, Object>} */
    this.trails = new Map();
  }

  /** Break every existing ribbon at the latest snapped bot position. */
  reset(botEntries) {
    for (const [botId, trail] of this.trails) {
      const entry = botEntries?.get(botId);
      trail.timer = 0;
      trail.history.length = 0;
      if (entry?._interpReady) {
        trail.history.push({x: entry.root.position.x, z: entry.root.position.z});
      }
      trail.dirty = false;
      if (trail.mesh) trail.mesh.setEnabled(false);
    }
  }

  /**
   * Called every render frame with the bot renderer's entries map.
   * @param {Map<string, Object>|null} botEntries
   * @param {number} dt
   */
  render(botEntries, dt) {
    if (!botEntries) return;

    if (!isEnabled('movementTrails', 'botTrails')) {
      // Meshes are reused/updated in place across frames rather than
      // recreated, so a bare early-return here would leave the last-built
      // ribbon frozen and visible. Hide any already-built trail meshes and
      // bail before doing any sampling/geometry work.
      for (const [, trail] of this.trails) {
        if (trail.mesh) trail.mesh.setEnabled(false);
      }
      return;
    }

    const B = window.BABYLON;
    const seen = new Set();
    let renderedTrails = 0;

    for (const [botId, entry] of botEntries) {
      if (!entry.isAlive || !entry._interpReady) continue;
      if (renderedTrails >= MAX_RENDERED_TRAILS) continue;
      renderedTrails += 1;
      seen.add(botId);

      const x = entry.root.position.x;
      const z = entry.root.position.z;

      let trail = this.trails.get(botId);
      if (!trail) {
        const color = entry.bodyMat ? entry.bodyMat.diffuseColor : new B.Color3(0.5, 0.5, 0.5);
        const mat = new B.StandardMaterial(`tmat-${botId}`, this.scene);
        mat.emissiveColor = color.clone();
        mat.diffuseColor = color.clone();
        mat.disableLighting = true;
        mat.backFaceCulling = false;
        mat.alpha = 1;
        mat.useVertexAlpha = true;

        // Pre-allocate reusable path arrays (2 sides × MAX_HISTORY points)
        const left = [];
        const right = [];
        for (let i = 0; i < MAX_HISTORY; i++) {
          left.push(new B.Vector3(x, TRAIL_Y, z));
          right.push(new B.Vector3(x, TRAIL_Y, z));
        }

        trail = {
          history: [{ x, z }],
          mesh: null,
          mat,
          timer: 0,
          left,
          right,
          colors: null, // allocated once when mesh is created
          dirty: false,
        };
        this.trails.set(botId, trail);
      }

      // Sample position at fixed interval
      trail.timer += dt;
      if (trail.timer >= SAMPLE_INTERVAL) {
        trail.timer = 0;
        const last = trail.history[trail.history.length - 1];
        const dx = x - last.x;
        const dz = z - last.z;
        if (dx * dx + dz * dz > 150 * 150) {
          // Teleport (respawn/round reset): break the ribbon instead of
          // painting a full-width comet across the arena.
          trail.history.length = 0;
          trail.history.push({ x, z });
          trail.dirty = true;
        } else if (dx * dx + dz * dz > 0.5) {
          trail.history.push({ x, z });
          if (trail.history.length > MAX_HISTORY) {
            trail.history.shift();
          }
          trail.dirty = true;
        }
      }

      if (trail.history.length < 2) {
        if (trail.mesh) trail.mesh.setEnabled(false);
        continue;
      }
      // A disabled/reset trail becomes visible only after fresh geometry is
      // available; this prevents the old ribbon flashing on resume.
      if (trail.mesh && !trail.mesh.isEnabled()) trail.mesh.setEnabled(true);
      if (!trail.dirty && trail.mesh) continue; // no change, skip update
      trail.dirty = false;

      const hist = trail.history;
      const n = hist.length;

      // Update pre-allocated path arrays in place (no new Vector3 allocations)
      for (let i = 0; i < n; i++) {
        let nx, nz;
        if (i < n - 1) {
          nx = hist[i + 1].x - hist[i].x;
          nz = hist[i + 1].z - hist[i].z;
        } else {
          nx = hist[i].x - hist[i - 1].x;
          nz = hist[i].z - hist[i - 1].z;
        }
        const len = Math.sqrt(nx * nx + nz * nz) || 1;
        const px = -nz / len;
        const pz = nx / len;
        const alpha = i / (n - 1);
        const w = TRAIL_WIDTH * alpha;

        trail.left[i].set(hist[i].x + px * w, TRAIL_Y, hist[i].z + pz * w);
        trail.right[i].set(hist[i].x - px * w, TRAIL_Y, hist[i].z - pz * w);
      }
      // Collapse unused tail points to the last used position (avoids stale geometry)
      for (let i = n; i < MAX_HISTORY; i++) {
        trail.left[i].copyFrom(trail.left[n - 1]);
        trail.right[i].copyFrom(trail.right[n - 1]);
      }

      try {
        if (!trail.mesh) {
          // First creation — updatable ribbon
          const ribbon = B.MeshBuilder.CreateRibbon(`trail-${botId}`, {
            pathArray: [trail.left, trail.right],
            updatable: true,
            sideOrientation: B.Mesh.DOUBLESIDE,
          }, this.scene);
          ribbon.material = trail.mat;
          ribbon.isPickable = false;
          ribbon.hasVertexAlpha = true;
          trail.mesh = ribbon;

          // Allocate vertex color buffer once
          const vc = ribbon.getTotalVertices();
          trail.colors = new Float32Array(vc * 4);
        } else {
          // Update in place — no new mesh allocation
          B.MeshBuilder.CreateRibbon(null, {
            pathArray: [trail.left, trail.right],
            instance: trail.mesh,
          });
        }

        // Update vertex colors in the pre-allocated buffer. Brightness toggle
        // checked here (not just at material-creation time) so it applies
        // live on the next geometry update rather than needing a reload.
        const c = trail.mat.emissiveColor;
        const bright = isEnabled('movementTrails', 'trailBrightness');
        const cr = bright ? c.r : c.r * 0.55;
        const cg = bright ? c.g : c.g * 0.55;
        const cb = bright ? c.b : c.b * 0.55;
        const vc = trail.mesh.getTotalVertices();
        const pps = MAX_HISTORY; // points per side
        for (let v = 0; v < vc; v++) {
          const idx = v % pps;
          const a = idx < n ? (idx / (n - 1)) * 0.3 : 0;
          trail.colors[v * 4] = cr;
          trail.colors[v * 4 + 1] = cg;
          trail.colors[v * 4 + 2] = cb;
          trail.colors[v * 4 + 3] = a;
        }
        trail.mesh.setVerticesData(B.VertexBuffer.ColorKind, trail.colors, true);
      } catch {
        // degenerate geometry — ignore
      }
    }

    // Cleanup trails for disconnected bots
    for (const [botId, trail] of this.trails) {
      if (!seen.has(botId)) {
        if (trail.mesh) trail.mesh.dispose();
        trail.mat.dispose();
        this.trails.delete(botId);
      }
    }
  }

  dispose() {
    for (const [, trail] of this.trails) {
      if (trail.mesh) trail.mesh.dispose();
      trail.mat.dispose();
    }
    this.trails.clear();
  }
}
