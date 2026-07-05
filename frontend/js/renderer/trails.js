'use strict';

/**
 * Movement trail system — smooth fading ribbons behind moving bots.
 * Samples bot visual positions and updates reusable ribbon meshes in place.
 * Uses updatable ribbons to avoid per-frame mesh allocation (memory leak fix).
 * @module renderer/trails
 */

const MAX_HISTORY = 20;
const SAMPLE_INTERVAL = 0.03;
const TRAIL_WIDTH = 6;
const TRAIL_Y = 0.4;

export class TrailRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, Object>} */
    this.trails = new Map();
  }

  /**
   * Called every render frame with the bot renderer's entries map.
   * @param {Map<string, Object>|null} botEntries
   * @param {number} dt
   */
  render(botEntries, dt) {
    if (!botEntries) return;
    const B = window.BABYLON;
    const seen = new Set();

    for (const [botId, entry] of botEntries) {
      if (!entry.isAlive || !entry._interpReady) continue;
      seen.add(botId);

      const x = entry.root.position.x;
      const z = entry.root.position.z;

      let trail = this.trails.get(botId);
      if (!trail) {
        const color = entry.bodyMat ? entry.bodyMat.diffuseColor : new B.Color3(0.5, 0.5, 0.5);
        const mat = new B.StandardMaterial(`tmat-${botId}`, this.scene);
        // Additive + brightened so overlapping trails brighten instead of
        // muddying, and every moving bot drags a visible light streak (with
        // bloom this is the core "alive between battles" read on a big screen).
        // Depth-write off keeps additive ribbons from occluding ground decals.
        mat.emissiveColor = new B.Color3(
          Math.min(1, color.r * 1.5),
          Math.min(1, color.g * 1.5),
          Math.min(1, color.b * 1.5)
        );
        mat.diffuseColor = color.clone();
        mat.disableLighting = true;
        mat.backFaceCulling = false;
        mat.alpha = 1;
        mat.useVertexAlpha = true;
        mat.alphaMode = B.Engine.ALPHA_ADD;
        mat.disableDepthWrite = true;

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
        if (dx * dx + dz * dz > 0.5) {
          trail.history.push({ x, z });
          if (trail.history.length > MAX_HISTORY) {
            trail.history.shift();
          }
          trail.dirty = true;
        }
      }

      if (trail.history.length < 2) continue;
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
          ribbon.alwaysSelectAsActiveMesh = true;
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

        // Update vertex colors in the pre-allocated buffer
        const c = trail.mat.emissiveColor;
        const vc = trail.mesh.getTotalVertices();
        const pps = MAX_HISTORY; // points per side
        for (let v = 0; v < vc; v++) {
          const idx = v % pps;
          // 0.5 peak alpha (was 0.3): with additive blending the ground shows
          // through, so the brighter ribbon does not occlude.
          const a = idx < n ? (idx / (n - 1)) * 0.5 : 0;
          trail.colors[v * 4] = c.r;
          trail.colors[v * 4 + 1] = c.g;
          trail.colors[v * 4 + 2] = c.b;
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
