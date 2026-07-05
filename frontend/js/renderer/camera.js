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
    // Orbit on LEFT button only. With panningSensibility=0 Babylon's pointer
    // input otherwise treats right/middle drags as orbit too, so every drag
    // through the custom pan below ALSO rotated the camera at the same time.
    if (this.camera.inputs.attached.pointers) {
      this.camera.inputs.attached.pointers.buttons = [0];
    }

    this._setupInput(canvas);
    scene.registerBeforeRender(() => this._tick());
  }

  _setupInput(canvas) {
    let dragging = false;
    let lastX = 0, lastY = 0;

    canvas.addEventListener('pointerdown', (e) => {
      if (e.button === 1 || e.button === 2) {
        dragging = true;
        lastX = e.clientX;
        lastY = e.clientY;
        this.followId = null;
        // Auto-pan re-targets every tick and would fight the drag, so a
        // manual drag takes over just like WASD does.
        this.autoPan = false;
        e.preventDefault();
      }
    });
    canvas.addEventListener('pointermove', (e) => {
      if (!dragging) return;
      const scale = this.camera.radius / 500;
      const dx = (e.clientX - lastX) * scale;
      const dy = (e.clientY - lastY) * scale;
      // Grab-style pan: the world follows the cursor. Directions come from
      // the camera's actual view vectors (same approach as the WASD block in
      // _tick), so the mapping stays correct at any orbit angle. The old
      // hand-rolled cos/sin(alpha) mapping was rotated 90 degrees at the
      // default angle and drifted further off as the camera orbited.
      const fwd = this.camera.target.subtract(this.camera.position);
      fwd.y = 0;
      const flen = Math.sqrt(fwd.x * fwd.x + fwd.z * fwd.z);
      if (flen > 1e-6) {
        const fx = fwd.x / flen, fz = fwd.z / flen;
        const rightX = fz, rightZ = -fx;
        // drag right: world moves right on screen, so the target moves left;
        // drag down: world moves toward the viewer, so the target moves forward
        this.targetX += -dx * rightX + dy * fx;
        this.targetZ += -dx * rightZ + dy * fz;
      }
      lastX = e.clientX;
      lastY = e.clientY;
    });
    canvas.addEventListener('pointerup', () => { dragging = false; });
    canvas.addEventListener('pointerleave', () => { dragging = false; });
    canvas.addEventListener('contextmenu', (e) => e.preventDefault());

    canvas.addEventListener('wheel', (e) => {
      e.preventDefault();
      const delta = e.deltaY > 0 ? -0.15 : 0.15;
      this.setZoom(this.zoom + delta);
    }, { passive: false });

    // WASD / arrow key panning
    window.addEventListener('keydown', (e) => {
      const key = e.key.toLowerCase();
      if (['w', 'a', 's', 'd', 'arrowup', 'arrowdown', 'arrowleft', 'arrowright'].includes(key)) {
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
    this.camera.radius = BASE_RADIUS / this.zoom;
    if (this.onZoomChange) this.onZoomChange(this.zoom);
  }

  followBot(botId) { this.followId = botId; }
  setAutoPan(enabled) { this.autoPan = enabled; if (enabled) this.followId = null; }
  updateBotPositions(bots) { this.bots = bots || []; }
}
