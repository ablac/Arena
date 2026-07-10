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

    // All pointer/wheel/keyboard input is handled by _setupInput below —
    // Babylon's own inputs are never attached. Its pointer input mapped
    // touch to orbit-only (pan lived exclusively on right/middle mouse and
    // WASD), which made phones spin in place with no way to move around.
    this._setupInput(canvas);
    scene.registerBeforeRender(() => this._tick());
  }

  /** @private Grab-pan: move the target opposite the drag on the XZ plane.
   * Camera forward on the ground plane is F = (-cosA, -sinA), camera right is
   * R = (-sinA, cosA); move the target opposite the drag along R and along F
   * for vertical drags. */
  _panByPixels(dxPx, dyPx) {
    const scale = this.camera.radius / 500;
    const dx = dxPx * scale;
    const dy = dyPx * scale;
    const cosA = Math.cos(this.camera.alpha);
    const sinA = Math.sin(this.camera.alpha);
    this.targetX += dx * sinA - dy * cosA;
    this.targetZ += -dx * cosA - dy * sinA;
  }

  /** @private Orbit matching Babylon's drag direction and rough sensitivity. */
  _orbitByPixels(dxPx, dyPx) {
    this.camera.alpha -= dxPx * 0.008;
    this.camera.beta = Math.max(
      MIN_BETA,
      Math.min(MAX_BETA, this.camera.beta - dyPx * 0.008)
    );
  }

  /** @private Manual navigation takes over from follow and auto-pan. */
  _takeManualControl() {
    this.followId = null;
    this.autoPan = false;
  }

  _setupInput(canvas) {
    this._canvas = canvas;
    /** @type {Map<number, {x: number, y: number, type: string}>} */
    const active = new Map();
    let mouseButton = -1;   // which mouse button drives the current drag
    let pinchDist = 0;      // last two-finger distance (0 = not pinching)

    const touches = () => [...active.values()].filter((p) => p.type === 'touch');

    const H = {};
    H.pointerdown = (e) => {
      active.set(e.pointerId, { x: e.clientX, y: e.clientY, type: e.pointerType });
      try { canvas.setPointerCapture(e.pointerId); } catch { /* detached */ }

      if (e.pointerType === 'touch') {
        const t = touches();
        if (t.length === 1) {
          // One finger = grab-pan (the thing phones couldn't do at all).
          this._takeManualControl();
        } else if (t.length === 2) {
          pinchDist = Math.hypot(t[0].x - t[1].x, t[0].y - t[1].y);
        }
        e.preventDefault();
        return;
      }

      if (e.button === 0 || e.button === 1 || e.button === 2) {
        mouseButton = e.button;
        if (e.button !== 0) {
          // Right/middle-drag pan; otherwise auto-pan rewrites the target
          // every frame mid-drag (matching WASD behavior).
          this._takeManualControl();
          e.preventDefault();
        }
      }
    };

    H.pointermove = (e) => {
      const prev = active.get(e.pointerId);
      if (!prev) return;
      const dx = e.clientX - prev.x;
      const dy = e.clientY - prev.y;

      if (e.pointerType === 'touch') {
        const t = touches();
        if (t.length === 1) {
          this._panByPixels(dx, dy);
        } else if (t.length === 2) {
          // Pinch = zoom; the pair's average drift = orbit. Update this
          // pointer first so distance/midpoint use both fresh positions.
          prev.x = e.clientX;
          prev.y = e.clientY;
          const [a, b] = touches();
          const dist = Math.hypot(a.x - b.x, a.y - b.y);
          if (pinchDist > 0 && dist > 0) {
            this.setZoom(this.zoom * (dist / pinchDist));
          }
          pinchDist = dist;
          this._orbitByPixels(dx / 2, dy / 2);
          return;
        }
      } else if (mouseButton === 0) {
        this._orbitByPixels(dx, dy);
      } else if (mouseButton === 1 || mouseButton === 2) {
        this._panByPixels(dx, dy);
      }

      prev.x = e.clientX;
      prev.y = e.clientY;
    };

    H.pointerup = (e) => {
      active.delete(e.pointerId);
      if (e.pointerType === 'touch') {
        pinchDist = 0;
      } else {
        mouseButton = -1;
      }
    };
    H.pointercancel = H.pointerup;
    H.pointerleave = H.pointerup;

    H.contextmenu = (e) => e.preventDefault();

    H.wheel = (e) => {
      e.preventDefault();
      const delta = e.deltaY > 0 ? -0.15 : 0.15;
      this.setZoom(this.zoom + delta);
    };

    // WASD / arrow key panning
    H.keydown = (e) => {
      const key = e.key.toLowerCase();
      if (['w', 'a', 's', 'd', 'arrowup', 'arrowdown', 'arrowleft', 'arrowright'].includes(key)) {
        this._keys.add(key);
        // Stop following/auto-pan when manually panning
        if (this.followId || this.autoPan) {
          this._takeManualControl();
        }
      }
    };
    H.keyup = (e) => {
      this._keys.delete(e.key.toLowerCase());
    };

    canvas.addEventListener('pointerdown', H.pointerdown);
    canvas.addEventListener('pointermove', H.pointermove);
    canvas.addEventListener('pointerup', H.pointerup);
    canvas.addEventListener('pointercancel', H.pointercancel);
    canvas.addEventListener('pointerleave', H.pointerleave);
    canvas.addEventListener('contextmenu', H.contextmenu);
    canvas.addEventListener('wheel', H.wheel, { passive: false });
    window.addEventListener('keydown', H.keydown);
    window.addEventListener('keyup', H.keyup);
    this._handlers = H;
  }

  /** Detach every listener. The between-round arena-size rebuild constructs a
   * fresh controller on the same canvas; without this, each rebuild stacked
   * another full set of listeners (and retained the dead controller). */
  dispose() {
    const H = this._handlers;
    if (!H) return;
    const c = this._canvas;
    c.removeEventListener('pointerdown', H.pointerdown);
    c.removeEventListener('pointermove', H.pointermove);
    c.removeEventListener('pointerup', H.pointerup);
    c.removeEventListener('pointercancel', H.pointercancel);
    c.removeEventListener('pointerleave', H.pointerleave);
    c.removeEventListener('contextmenu', H.contextmenu);
    c.removeEventListener('wheel', H.wheel);
    window.removeEventListener('keydown', H.keydown);
    window.removeEventListener('keyup', H.keyup);
    this._handlers = null;
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
