'use strict';

/**
 * Babylon.js engine — scene setup, render loop, module orchestration.
 * @module renderer/engine
 */

import { CameraController } from './camera.js';
import { BotRenderer } from './bots.js';
import { EnvironmentRenderer } from './environment.js';
import { ObstacleRenderer } from './obstacles.js';
import { PickupRenderer } from './pickups.js';
import { EffectRenderer } from './effects.js';
import { TrailRenderer } from './trails.js';
import { ProjectileRenderer } from './projectiles.js';

// Bot positions are smoothed via exponential lerp each frame,
// so no tick-interval-based alpha is needed.

export class ArenaEngine {
  /** @param {HTMLCanvasElement} canvas @param {Object} opts */
  constructor(canvas, opts = {}) {
    this.canvas = canvas;
    this.arenaWidth = opts.arenaWidth || 2000;
    this.arenaHeight = opts.arenaHeight || 2000;
    this.engine = null;
    this.scene = null;
    this.camera = null;
    this.botRenderer = null;
    this.envRenderer = null;
    this.obstacleRenderer = null;
    this.pickupRenderer = null;
    this.effectRenderer = null;
    this.trailRenderer = null;
    this.projectileRenderer = null;
    this.state = null;
    this.ready = false;
  }

  /** Initialize Babylon engine. */
  async init() {
    const B = window.BABYLON;
    let engine;
    try {
      engine = new B.WebGPUEngine(this.canvas, { antialias: false, powerPreference: 'high-performance' });
      await engine.initAsync();
      console.log('[Arena] WebGPU');
    } catch {
      engine = new B.Engine(this.canvas, false, {
        preserveDrawingBuffer: false,
        stencil: false,
        powerPreference: 'high-performance',
      });
    }
    // Cap at 1x device pixel ratio to prevent supersampling on HiDPI
    engine.setHardwareScalingLevel(1.0 / Math.min(window.devicePixelRatio, 1));
    this.engine = engine;
    const scene = new B.Scene(engine);
    this.scene = scene;
    scene.clearColor = new B.Color4(0.03, 0.03, 0.05, 1);
    scene.fogMode = B.Scene.FOGMODE_EXP2;
    scene.fogDensity = 0.00008;
    scene.fogColor = new B.Color3(0.03, 0.03, 0.03);
    scene.skipPointerMovePicking = true;
    scene.autoClear = false;
    scene.autoClearDepthAndStencil = true;
    scene.blockMaterialDirtyMechanism = true;
    scene.useGeometryIdsMap = true;
    scene.useMaterialMeshMap = true;

    this.camera = new CameraController(scene, this.canvas, this.arenaWidth, this.arenaHeight);
    this.envRenderer = new EnvironmentRenderer(scene, this.arenaWidth, this.arenaHeight);
    this.obstacleRenderer = new ObstacleRenderer(scene);
    this.botRenderer = new BotRenderer(scene);
    this.pickupRenderer = new PickupRenderer(scene);
    this.effectRenderer = new EffectRenderer(scene);
    this.effectRenderer.camera = this.camera;
    this.trailRenderer = new TrailRenderer(scene);
    this.projectileRenderer = new ProjectileRenderer(scene);

    // Wire up attack → per-weapon hit effects + projectiles for ranged
    this.botRenderer.onAttack = (ax, az, tx, tz, color, weapon) => {
      if (weapon === 'bow' || weapon === 'staff') {
        // Ranged: projectile travels to target, hit sparks on impact
        this.projectileRenderer.spawn(ax, az, tx, tz, weapon, color, () => {
          this.effectRenderer.spawnHitSparks(tx, tz, color, weapon);
        });
      } else {
        // Melee: immediate hit sparks at target
        this.effectRenderer.spawnHitSparks(tx, tz, color, weapon);
      }
    };

    // Wire up dodge → afterimage shimmer
    this.botRenderer.onDodge = (x, z, color) => {
      this.effectRenderer.spawnDodgeEffect(x, z, color);
    };

    this._addLights();

    // Subtle glow on emissive surfaces (low-res for performance)
    const gl = new B.GlowLayer('glow', this.scene, {
      mainTextureFixedSize: 128,
    });
    gl.intensity = 0.2;

    const self = this;
    let _lastFrame = performance.now();
    engine.runRenderLoop(() => {
      const now = performance.now();
      const dt = Math.min((now - _lastFrame) / 1000, 0.1);
      _lastFrame = now;
      if (self.botRenderer) {
        self.botRenderer.interpolate();
      }
      if (self.projectileRenderer) {
        self.projectileRenderer.update(dt);
      }
      scene.render();
    });
    window.addEventListener('resize', () => engine.resize());
    this.ready = true;
  }

  /** @private */
  _addLights() {
    const B = window.BABYLON;
    const dir = new B.DirectionalLight('sun', new B.Vector3(-0.4, -1, 0.3), this.scene);
    dir.position = new B.Vector3(0, 80, -40);
    dir.intensity = 0.7;
    dir.diffuse = new B.Color3(1, 0.95, 0.85);
    dir.specular = new B.Color3(0.3, 0.3, 0.3);
    this.sunLight = dir;

    const hemi = new B.HemisphericLight('hemi', new B.Vector3(0, 1, 0), this.scene);
    hemi.intensity = 0.4;
    hemi.diffuse = new B.Color3(0.6, 0.65, 0.8);
    hemi.specular = B.Color3.Black();
    hemi.groundColor = new B.Color3(0.15, 0.12, 0.1);
  }

  /**
   * Feed arena state from spectator WS.
   * @param {Object} state
   */
  setState(state) {
    if (!this.ready || state.type !== 'arena_state') return;
    this.state = state;
    this.obstacleRenderer.update(state.obstacles);
    this.envRenderer.update(state.safe_zone);
    this.botRenderer.update(state.bots);
    this.trailRenderer.update(state.bots);
    this.pickupRenderer.update(state.pickups || []);
    this.effectRenderer.update(state.bots);
    this.camera.updateBotPositions(state.bots);
  }

  setZoom(z) { if (this.camera) this.camera.setZoom(z); }
  followBot(id) { if (this.camera) this.camera.followBot(id); }
  setAutoPan(on) { if (this.camera) this.camera.setAutoPan(on); }
  getState() { return this.state; }

  dispose() {
    if (this.projectileRenderer) this.projectileRenderer.dispose();
    if (this.trailRenderer) this.trailRenderer.dispose();
    if (this.engine) {
      this.engine.stopRenderLoop();
      this.scene.dispose();
      this.engine.dispose();
    }
  }
}
