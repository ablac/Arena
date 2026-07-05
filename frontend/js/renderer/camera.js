'use strict';

/**
 * 3D perspective camera — ArcRotateCamera with WASD pan, zoom, orbit, and follow.
 * RTS-style angled view looking down at the arena.
 * @module renderer/camera
 */

const DEFAULT_ALPHA = -Math.PI / 2;
const DEFAULT_BETA = 1.0;
const BASE_RADIUS = 800;
const MIN_BETA = 0.01;   // nearly straight up from below
const MAX_BETA = Math.PI; // full orbit — can go under the arena
const PAN_SPEED = 8;

export class CameraController {
  constructor(scene, canvas, w, h) {
    const B = window.BABYLON;
    this.scene = scene;
    this.arenaWidth = w;
    this.arenaHeight = h;
    this.zoom = 1.0;
    this.followId = null;
    this.autoPan = false;
    this.targetX = w / 2;
    this.targetZ = h / 2;
    this.bots = [];
    this.onZoomChange = null;

    // Idle-attract: after 8s without input the camera drifts into a slow
    // cinematic orbit so the arena never sits on a dead static angle.
    // Honors prefers-reduced-motion (checked once; kiosk operators can
    // still orbit manually).
    this._idleTime = 0;
    this._idleBaseBeta = null;
    this._reducedMotion = typeof window.matchMedia === 'function'
      && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    // Smoothed zoom: setZoom() writes the target, _tick() lerps toward it.
    this._targetRadius = BASE_RADIUS;

    // Track held keys
    this._keys = new Set();

    this.camera = new B.ArcRotateCamera('cam',
      DEFAULT_ALPHA, DEFAULT_BETA, BASE_RADIUS,
      new B.Vector3(w / 2, 0, h / 2), scene
    );
    this.camera.lowerRadiusLimit = 80;
    this.camera.upperRadiusLimit = 1800; // limit zoom-out to prevent flying into void
    this.camera.maxZ = 60000; // far clip — must see skybox (50k) and space objects
    this.camera.lowerBetaLimit = MIN_BETA;
    this.camera.upperBetaLimit = MAX_BETA;
    this.camera.panningSensibility = 0;
    this.camera.attachControl(canvas, true);

    // Remove default keyboard input so WASD doesn't conflict
    this.camera.inputs.removeByType('ArcRotateCameraKeyboardMoveInput');
    this.camera.inputs.removeByType('ArcRotateCameraMouseWheelInput');

    this._setupInput(canvas);
    scene.registerBeforeRender(() => this._tick());
  }

  _setupInput(canvas) {
    let dragging = false;
    let lastX = 0, lastY = 0;

    canvas.addEventListener('pointerdown', (e) => {
      // Any pointer interaction (including left-drag orbit handled by
      // attachControl) cancels the idle-attract drift.
      this._idleTime = 0;
      this._idleBaseBeta = null;
      if (e.button === 1 || e.button === 2) {
        dragging = true;
        lastX = e.clientX;
        lastY = e.clientY;
        this.followId = null;
        e.preventDefault();
      }
    });
    canvas.addEventListener('pointermove', (e) => {
      if (!dragging) return;
      const scale = this.camera.radius / 500;
      const dx = (e.clientX - lastX) * scale;
      const dy = (e.clientY - lastY) * scale;
      const cosA = Math.cos(this.camera.alpha);
      const sinA = Math.sin(this.camera.alpha);
      this.targetX += dx * cosA - dy * sinA;
      this.targetZ += dx * sinA + dy * cosA;
      lastX = e.clientX;
      lastY = e.clientY;
    });
    canvas.addEventListener('pointerup', () => { dragging = false; });
    canvas.addEventListener('pointerleave', () => { dragging = false; });
    canvas.addEventListener('contextmenu', (e) => e.preventDefault());

    canvas.addEventListener('wheel', (e) => {
      e.preventDefault();
      this._idleTime = 0;
      this._idleBaseBeta = null;
      const delta = e.deltaY > 0 ? -0.15 : 0.15;
      this.setZoom(this.zoom + delta);
    }, { passive: false });

    // WASD / arrow key panning
    window.addEventListener('keydown', (e) => {
      const key = e.key.toLowerCase();
      if (['w', 'a', 's', 'd', 'arrowup', 'arrowdown', 'arrowleft', 'arrowright'].includes(key)) {
        this._idleTime = 0;
        this._idleBaseBeta = null;
        this._keys.add(key);
        // Stop following/auto-pan when manually panning
        if (this.followId || this.autoPan) {
          this.followId = null;
          this.autoPan = false;
        }
      }
    });
    window.addEventListener('keyup', (e) => {
      this._keys.delete(e.key.toLowerCase());
    });
  }

  _tick() {
    // WASD / arrow key movement relative to camera facing
    if (this._keys.size > 0) {
      // Scale by dt (relative to a 60fps baseline) so pan speed doesn't
      // depend on the display's refresh rate.
      const dtFrames = Math.min(this.scene.getEngine().getDeltaTime(), 100) / (1000 / 60);
      const speed = PAN_SPEED * (this.camera.radius / BASE_RADIUS) * dtFrames;
      // Get camera's actual forward direction projected onto XZ plane
      const cam = this.camera;
      const forward = cam.target.subtract(cam.position);
      forward.y = 0;
      forward.normalize();
      // Right is perpendicular on XZ plane
      const rightX = forward.z;
      const rightZ = -forward.x;

      let dx = 0, dz = 0;

      if (this._keys.has('w') || this._keys.has('arrowup'))    { dx += forward.x; dz += forward.z; }
      if (this._keys.has('s') || this._keys.has('arrowdown'))  { dx -= forward.x; dz -= forward.z; }
      if (this._keys.has('a') || this._keys.has('arrowleft'))  { dx -= rightX; dz -= rightZ; }
      if (this._keys.has('d') || this._keys.has('arrowright')) { dx += rightX; dz += rightZ; }

      if (dx !== 0 || dz !== 0) {
        const len = Math.sqrt(dx * dx + dz * dz);
        this.targetX += (dx / len) * speed;
        this.targetZ += (dz / len) * speed;
      }
    }

    // Follow / auto-pan
    if (this.followId && this.bots.length > 0) {
      const bot = this.bots.find(b => b.bot_id === this.followId);
      if (bot && bot.position) {
        this.targetX = bot.position[0];
        this.targetZ = bot.position[1];
      }
    } else if (this.autoPan && this.bots.length > 0) {
      this._autoPanToAction();
    }

    // Clamp target to stay near the arena (with some margin)
    const margin = 400;
    this.targetX = Math.max(-margin, Math.min(this.arenaWidth + margin, this.targetX));
    this.targetZ = Math.max(-margin, Math.min(this.arenaHeight + margin, this.targetZ));

    // dt-based smoothing so follow/pan speed is framerate-independent.
    const dt = this.scene.getEngine().getDeltaTime() / 1000;
    const lerp = 1 - Math.exp(-5 * Math.min(dt, 0.1));
    const t = this.camera.target;
    t.x += (this.targetX - t.x) * lerp;
    t.z += (this.targetZ - t.z) * lerp;
    t.y = 0;

    // Idle-attract cinematic drift: slow orbit (~2 min per revolution) with a
    // gentle beta breathe around wherever the camera was left, plus a slight
    // radius push-in/out. Kicks in after 8s of no input. Reduced-motion
    // suppresses the ambient version, but an EXPLICIT Auto-Pan toggle
    // overrides it (deliberate operator intent, e.g. a showcase kiosk whose
    // OS has animations off) with a shorter 2s idle gate so it still never
    // fights active dragging. All scalar math, zero allocations.
    this._idleTime += Math.min(dt, 0.1);
    const driftActive = this.autoPan
      ? this._idleTime > 2
      : (!this._reducedMotion && this._idleTime > 8);
    if (driftActive) {
      if (this._idleBaseBeta === null) this._idleBaseBeta = this.camera.beta;
      this.camera.alpha += dt * 0.05;
      this.camera.beta = this._idleBaseBeta + Math.sin(this._idleTime * 0.13) * 0.07;
    }

    // Smoothed zoom toward the target radius, with a slow oscillation while
    // drifting so the shot breathes. Reuses the same dt-lerp shape as the target.
    let radiusGoal = this._targetRadius;
    if (driftActive) {
      radiusGoal = this._targetRadius * (1 + Math.sin(this._idleTime * 0.07) * 0.06);
    }
    this.camera.radius += (radiusGoal - this.camera.radius) * lerp;
  }

  _autoPanToAction() {
    const alive = this.bots.filter(b => b.is_alive);
    if (alive.length === 0) return;
    let ax = 0, az = 0;
    alive.forEach(b => { ax += b.position[0]; az += b.position[1]; });
    this.targetX = ax / alive.length;
    this.targetZ = az / alive.length;
  }

  setZoom(zoom) {
    this.zoom = Math.max(0.3, Math.min(6.0, zoom));
    // _tick() lerps camera.radius toward this each frame (buttery zoom
    // instead of a hard snap).
    this._targetRadius = BASE_RADIUS / this.zoom;
    if (this.onZoomChange) this.onZoomChange(this.zoom);
  }

  /**
   * Trigger a screen shake effect (e.g. on kill).
   * @param {number} [intensity=8] - max displacement in world units
   */
  shake(intensity = 8) {
    const start = performance.now();
    const duration = 300; // ms
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const elapsed = performance.now() - start;
      if (elapsed >= duration) {
        this.scene.onBeforeRenderObservable.remove(obs);
        return;
      }
      const decay = Math.exp(-elapsed / (duration * 0.3));
      const ox = (Math.random() * 2 - 1) * intensity * decay;
      const oz = (Math.random() * 2 - 1) * intensity * decay;
      this.camera.target.x += ox;
      this.camera.target.z += oz;
    });
  }

  followBot(botId) { this.followId = botId; }
  setAutoPan(enabled) { this.autoPan = enabled; if (enabled) this.followId = null; }
  updateBotPositions(bots) { this.bots = bots || []; }
}
