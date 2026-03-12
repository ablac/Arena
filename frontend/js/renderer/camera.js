'use strict';

/**
 * Orthographic top-down camera with pan, zoom, and follow.
 * @module renderer/camera
 */

export class CameraController {
  /**
   * @param {BABYLON.Scene} scene
   * @param {HTMLCanvasElement} canvas
   * @param {number} arenaWidth
   * @param {number} arenaHeight
   */
  constructor(scene, canvas, arenaWidth, arenaHeight) {
    const BABYLON = window.BABYLON;
    this.scene = scene;
    this.arenaWidth = arenaWidth;
    this.arenaHeight = arenaHeight;
    this.zoom = 1.0;
    this.followId = null;
    this.autoPan = false;
    this.targetX = arenaWidth / 2;
    this.targetY = arenaHeight / 2;
    this.bots = [];

    // Orthographic camera looking straight down
    this.camera = new BABYLON.FreeCamera('cam', new BABYLON.Vector3(
      arenaWidth / 2, 500, arenaHeight / 2
    ), scene);
    this.camera.setTarget(new BABYLON.Vector3(arenaWidth / 2, 0, arenaHeight / 2));
    this.camera.mode = BABYLON.Camera.ORTHOGRAPHIC_CAMERA;
    this._updateOrtho();

    // Disable default camera controls — we handle input ourselves
    this.camera.inputs.clear();
    this._setupInput(canvas);
    scene.registerBeforeRender(() => this._tick());
  }

  /** @private Set up mouse/touch pan and zoom input. */
  _setupInput(canvas) {
    let dragging = false;
    let lastX = 0, lastY = 0;

    canvas.addEventListener('pointerdown', (e) => {
      dragging = true; lastX = e.clientX; lastY = e.clientY;
      this.followId = null;
    });
    canvas.addEventListener('pointermove', (e) => {
      if (!dragging) return;
      const dx = (e.clientX - lastX) / this.zoom;
      const dy = (e.clientY - lastY) / this.zoom;
      this.targetX -= dx;
      this.targetY -= dy;
      lastX = e.clientX; lastY = e.clientY;
    });
    canvas.addEventListener('pointerup', () => { dragging = false; });
    canvas.addEventListener('pointerleave', () => { dragging = false; });

    canvas.addEventListener('wheel', (e) => {
      e.preventDefault();
      const delta = e.deltaY > 0 ? -0.1 : 0.1;
      this.zoom = Math.max(0.5, Math.min(3.0, this.zoom + delta));
      this._updateOrtho();
    }, { passive: false });
  }

  /** @private Update orthographic bounds based on zoom. */
  _updateOrtho() {
    const aspect = this.scene.getEngine().getRenderWidth() / this.scene.getEngine().getRenderHeight();
    const halfW = (this.arenaWidth / 2) / this.zoom;
    const halfH = (this.arenaHeight / 2) / this.zoom;
    this.camera.orthoLeft = -halfW * aspect;
    this.camera.orthoRight = halfW * aspect;
    this.camera.orthoTop = halfH;
    this.camera.orthoBottom = -halfH;
  }

  /** @private Called each frame — smooth interpolation toward target. */
  _tick() {
    if (this.followId && this.bots.length > 0) {
      const bot = this.bots.find(b => b.bot_id === this.followId);
      if (bot && bot.position) {
        this.targetX = bot.position[0];
        this.targetY = bot.position[1];
      }
    } else if (this.autoPan && this.bots.length > 0) {
      this._autoPanToAction();
    }
    const lerp = 0.08;
    const pos = this.camera.position;
    pos.x += (this.targetX - pos.x) * lerp;
    pos.z += (this.targetY - pos.z) * lerp;
    const target = this.camera.getTarget();
    target.x = pos.x; target.z = pos.z;
    this.camera.setTarget(target);
    this._updateOrtho();
  }

  /** @private Pan toward area with most alive bots. */
  _autoPanToAction() {
    const alive = this.bots.filter(b => b.is_alive);
    if (alive.length === 0) return;
    let avgX = 0, avgY = 0;
    alive.forEach(b => { avgX += b.position[0]; avgY += b.position[1]; });
    this.targetX = avgX / alive.length;
    this.targetY = avgY / alive.length;
  }

  /** @param {number} zoom */
  setZoom(zoom) {
    this.zoom = Math.max(0.5, Math.min(3.0, zoom));
    this._updateOrtho();
  }

  /** @param {string|null} botId */
  followBot(botId) { this.followId = botId; }

  /** @param {boolean} enabled */
  setAutoPan(enabled) { this.autoPan = enabled; if (enabled) this.followId = null; }

  /** @param {Array} bots - Bot array from arena state. */
  updateBotPositions(bots) { this.bots = bots || []; }
}
