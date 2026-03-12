'use strict';

/**
 * Babylon.js engine setup with WebGPU fallback to WebGL2.
 * Creates the scene and orchestrates rendering sub-modules.
 * @module renderer/engine
 */

import { CameraController } from './camera.js';
import { BotRenderer } from './bots.js';
import { EnvironmentRenderer } from './environment.js';
import { PickupRenderer } from './pickups.js';
import { EffectRenderer } from './effects.js';

export class ArenaEngine {
  /**
   * @param {HTMLCanvasElement} canvas
   * @param {Object} options - { arenaWidth, arenaHeight }
   */
  constructor(canvas, options = {}) {
    this.canvas = canvas;
    this.arenaWidth = options.arenaWidth || 2000;
    this.arenaHeight = options.arenaHeight || 2000;
    /** @type {BABYLON.Engine|null} */
    this.engine = null;
    /** @type {BABYLON.Scene|null} */
    this.scene = null;
    this.camera = null;
    this.botRenderer = null;
    this.envRenderer = null;
    this.pickupRenderer = null;
    this.effectRenderer = null;
    this.state = null;
    this.ready = false;
  }

  /** Initialize Babylon engine — try WebGPU first, fall back to WebGL2. */
  async init() {
    const BABYLON = window.BABYLON;
    let engine;
    try {
      engine = new BABYLON.WebGPUEngine(this.canvas, { antialias: true });
      await engine.initAsync();
      console.log('[Arena] Using WebGPU');
    } catch (err) {
      console.warn('[Arena] WebGPU unavailable, falling back to WebGL2:', err.message);
      engine = new BABYLON.Engine(this.canvas, true, { preserveDrawingBuffer: true });
    }
    this.engine = engine;
    this.scene = new BABYLON.Scene(engine);
    this.scene.clearColor = new BABYLON.Color4(0.02, 0.03, 0.06, 1);

    this.camera = new CameraController(this.scene, this.canvas, this.arenaWidth, this.arenaHeight);
    this.envRenderer = new EnvironmentRenderer(this.scene, this.arenaWidth, this.arenaHeight);
    this.botRenderer = new BotRenderer(this.scene);
    this.pickupRenderer = new PickupRenderer(this.scene);
    this.effectRenderer = new EffectRenderer(this.scene);

    this._addLight();
    engine.runRenderLoop(() => this.scene.render());
    window.addEventListener('resize', () => engine.resize());
    this.ready = true;
  }

  /** @private */
  _addLight() {
    const BABYLON = window.BABYLON;
    const light = new BABYLON.HemisphericLight('light', new BABYLON.Vector3(0, 1, 0), this.scene);
    light.intensity = 1.0;
  }

  /**
   * Update arena state from spectator WebSocket.
   * @param {Object} state - Arena state message
   */
  setState(state) {
    if (!this.ready || state.type !== 'arena_state') return;
    this.state = state;
    this.envRenderer.update(state.safe_zone, state.obstacles);
    this.botRenderer.update(state.bots);
    this.pickupRenderer.update(state.pickups || []);
    this.effectRenderer.update(state.bots);
    this.camera.updateBotPositions(state.bots);
  }

  /** Set camera zoom level. */
  setZoom(zoom) { if (this.camera) this.camera.setZoom(zoom); }

  /** Follow a specific bot by ID. */
  followBot(botId) { if (this.camera) this.camera.followBot(botId); }

  /** Toggle auto-pan to action. */
  setAutoPan(enabled) { if (this.camera) this.camera.setAutoPan(enabled); }

  /** Get current state for HUD/minimap rendering. */
  getState() { return this.state; }

  /** Clean up resources. */
  dispose() {
    if (this.engine) {
      this.engine.stopRenderLoop();
      this.scene.dispose();
      this.engine.dispose();
    }
  }
}
