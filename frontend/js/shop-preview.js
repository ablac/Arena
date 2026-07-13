'use strict';

import { createBotEntry, disposeBotEntry } from './renderer/bot-body.js?v=20260713b';
import { applyBotCosmetics, disposeBotCosmetics } from './renderer/cosmetics.js?v=20260712c';
import { updateForgeCharacter } from './renderer/character-anims.js?v=20260712c';
import { TrailRenderer } from './renderer/trails.js?v=20260713b';

const DEFAULT_ALPHA = -Math.PI / 2;
const DEFAULT_BETA = 1.12;
const DEFAULT_RADIUS = 42;
const SHOP_FRAME_INTERVAL_MS = 1000 / 30;
const DEFAULT_LOADOUT = Object.freeze({
  bot_skin: 'standard',
  weapon_skin: 'standard',
  attachment: 'none',
  trail: 'standard',
});
const PREVIEW_WEAPONS = new Set(['sword', 'bow', 'spear', 'daggers', 'staff', 'shield', 'grapple']);

function assetKey(value, fallback) {
  if (typeof value !== 'string') return fallback;
  const normalized = value.trim();
  return normalized && normalized.length <= 96 ? normalized : fallback;
}

/** One isolated, presentation-only Babylon scene for the cosmetics shop. */
export class CosmeticShopPreview {
  constructor(canvas, options = {}) {
    if (!canvas) throw new TypeError('CosmeticShopPreview requires a canvas');
    this.canvas = canvas;
    this.options = options;
    this.autoRotate = options.autoRotate !== false;
    this.rotationSpeed = Number.isFinite(options.rotationSpeed) ? options.rotationSpeed : 0.22;
    this.loadout = {...DEFAULT_LOADOUT};
    this.engine = null;
    this.scene = null;
    this.camera = null;
    this.turntable = null;
    this.entry = null;
    this.trailRenderer = null;
    this._trailEntries = new Map();
    this.bot = null;
    this.ready = false;
    this._disposed = false;
    this._canvasVisible = true;
    this._reducedMotion = false;
    this._pageSuspended = false;
    this._renderRequested = true;
    this._lastRenderedAt = 0;
  }

  init() {
    if (this._disposed) throw new Error('CosmeticShopPreview has been disposed');
    if (this.ready) return this;
    try {
      return this._initialize();
    } catch (error) {
      this.dispose();
      throw error;
    }
  }

  _initialize() {
    const B = window.BABYLON;
    if (!B) throw new Error('Babylon.js is required for the cosmetics preview');

    this.engine = new B.Engine(this.canvas, false, {
      preserveDrawingBuffer: false,
      stencil: false,
    });
    // Match Arena's default 1x cap so a large CSS canvas does not supersample
    // on high-DPI phones and laptops.
    this.engine.setHardwareScalingLevel(1);

    this.scene = new B.Scene(this.engine);
    this.scene.clearColor = new B.Color4(0.018, 0.027, 0.05, 1);
    this.scene.skipPointerMovePicking = true;

    this.camera = new B.ArcRotateCamera(
      'cosmetic-shop-camera',
      DEFAULT_ALPHA,
      DEFAULT_BETA,
      DEFAULT_RADIUS,
      new B.Vector3(0, 10, 0),
      this.scene,
    );
    this.camera.lowerRadiusLimit = 28;
    this.camera.upperRadiusLimit = 70;
    this.camera.lowerBetaLimit = 0.45;
    this.camera.upperBetaLimit = Math.PI / 2.05;
    this.camera.panningSensibility = 0;
    this.camera.wheelPrecision = 55;
    this.camera.minZ = 0.1;
    this.camera.maxZ = 200;
    this.camera.attachControl(this.canvas, true);
    // ArcRotateCamera assigns a positive tabindex for keyboard controls. The
    // Shop exposes equivalent labeled buttons, so keep the pointer canvas out
    // of the normal tab order and preserve the skip link as the first stop.
    this.canvas.tabIndex = -1;

    const hemi = new B.HemisphericLight(
      'cosmetic-shop-hemi',
      new B.Vector3(0, 1, 0),
      this.scene,
    );
    hemi.intensity = 0.78;
    hemi.diffuse = new B.Color3(0.72, 0.82, 1);
    hemi.groundColor = new B.Color3(0.08, 0.1, 0.16);

    const key = new B.DirectionalLight(
      'cosmetic-shop-key',
      new B.Vector3(-0.5, -1, 0.45),
      this.scene,
    );
    key.position = new B.Vector3(18, 34, -24);
    key.intensity = 0.9;

    this.turntable = new B.TransformNode('cosmetic-shop-turntable', this.scene);
    this.bot = {
      bot_id: 'cosmetic-shop-preview',
      name: 'Preview',
      avatar_color: assetKey(this.options.avatarColor, '#5edfff'),
      weapon: assetKey(this.options.weapon, 'sword'),
      cosmetics: {...this.loadout},
    };
    this.entry = createBotEntry(this.bot, this.scene, {presentationOnly: true});
    this.entry.root.parent = this.turntable;
    this.entry.isAlive = true;
    this.entry._interpReady = true;
    this.entry.botData = this.bot;
    updateForgeCharacter(this.entry, 0, true);
    applyBotCosmetics(this.entry, this.bot, this.scene, {forceEnabled: true});
    this._trailEntries.set(this.bot.bot_id, this.entry);
    this.trailRenderer = new TrailRenderer(this.scene, {
      forceEnabled: true,
      showStandard: false,
      staticPreview: true,
    });

    this._motionQuery = typeof window.matchMedia === 'function'
      ? window.matchMedia('(prefers-reduced-motion: reduce)')
      : null;
    this._reducedMotion = this._motionQuery?.matches === true;
    this._motionHandler = (event) => {
      this._reducedMotion = event.matches === true;
      this._renderRequested = true;
    };
    if (this._motionQuery?.addEventListener) {
      this._motionQuery.addEventListener('change', this._motionHandler);
    } else if (this._motionQuery?.addListener) {
      this._motionQuery.addListener(this._motionHandler);
    }

    if (typeof ResizeObserver === 'function') {
      this._resizeObserver = new ResizeObserver(() => this.resize());
      this._resizeObserver.observe(this.canvas);
    } else {
      this._resizeHandler = () => this.resize();
      window.addEventListener('resize', this._resizeHandler);
    }

    if (typeof IntersectionObserver === 'function') {
      this._visibilityObserver = new IntersectionObserver((entries) => {
        for (const observed of entries) {
          this._canvasVisible = observed.isIntersecting;
          if (observed.isIntersecting) {
            this._lastFrame = performance.now();
            this._renderRequested = true;
          }
        }
      }, {threshold: 0});
      this._visibilityObserver.observe(this.canvas);
    }

    this._interactionHandler = () => { this._renderRequested = true; };
    for (const type of ['pointerdown', 'pointermove', 'wheel', 'keydown']) {
      this.canvas.addEventListener?.(type, this._interactionHandler, {passive: type === 'wheel'});
    }
    this._visibilityHandler = () => {
      if (!document.hidden) {
        this._lastFrame = performance.now();
        this._renderRequested = true;
      }
    };
    document.addEventListener?.('visibilitychange', this._visibilityHandler);

    this._pageHideHandler = (event) => {
      if (event?.persisted) {
        this._pageSuspended = true;
        return;
      }
      this.dispose();
    };
    this._pageShowHandler = (event) => {
      if (!event?.persisted || this._disposed) return;
      this._pageSuspended = false;
      this._lastFrame = performance.now();
      this._renderRequested = true;
      this.resize();
    };
    window.addEventListener('pagehide', this._pageHideHandler);
    window.addEventListener('pageshow', this._pageShowHandler);

    this._lastFrame = performance.now();
    this._renderFrame = () => {
      if (!this.ready || this._pageSuspended || document.hidden || this._canvasVisible === false) return;
      const now = performance.now();
      const onDemand = this._reducedMotion || !this.autoRotate;
      if (onDemand && !this._renderRequested) return;
      if (!this._renderRequested && this._lastRenderedAt &&
          now - this._lastRenderedAt < SHOP_FRAME_INTERVAL_MS) return;
      const dt = Math.min((now - this._lastFrame) / 1000, 0.1);
      this._lastFrame = now;
      if (this.autoRotate && !this._reducedMotion && this.turntable) {
        this.turntable.rotation.y += dt * this.rotationSpeed;
      }
      if (this.entry) {
        updateForgeCharacter(this.entry, dt, this._reducedMotion);
      }
      if (this.trailRenderer) this.trailRenderer.render(this._trailEntries, dt);
      this.scene.render();
      this._lastRenderedAt = now;
      this._renderRequested = false;
    };

    this.resize();
    this.ready = true;
    this.engine.runRenderLoop(this._renderFrame);
    return this;
  }

  setLoadout(loadout = {}) {
    this.loadout = {
      bot_skin: assetKey(loadout.bot_skin, DEFAULT_LOADOUT.bot_skin),
      weapon_skin: assetKey(loadout.weapon_skin, DEFAULT_LOADOUT.weapon_skin),
      attachment: assetKey(loadout.attachment, DEFAULT_LOADOUT.attachment),
      trail: assetKey(loadout.trail, DEFAULT_LOADOUT.trail),
    };
    if (this.ready && this.entry) {
      this.bot.cosmetics = {...this.loadout};
      this.entry.botData = this.bot;
      applyBotCosmetics(this.entry, this.bot, this.scene, {forceEnabled: true});
      this._renderRequested = true;
    }
    return this;
  }

  setCharacter(character = {}) {
    const requested = assetKey(character.weapon, this.bot?.weapon || this.options.weapon || 'sword');
    const weapon = PREVIEW_WEAPONS.has(requested) ? requested : 'sword';
    const avatarColor = assetKey(
      character.avatarColor,
      this.bot?.avatar_color || this.options.avatarColor || '#5edfff',
    );
    this.options.weapon = weapon;
    this.options.avatarColor = avatarColor;
    if (!this.ready || !this.entry || !this.bot) return this;
    if (this.bot.weapon === weapon && this.bot.avatar_color === avatarColor) return this;

    // Weapon finishes own material clones, so always tear cosmetic state down
    // before disposing the old chassis hierarchy.
    disposeBotCosmetics(this.entry);
    disposeBotEntry(this.entry);
    this.bot = {
      ...this.bot,
      weapon,
      avatar_color: avatarColor,
      cosmetics: {...this.loadout},
    };
    this.entry = createBotEntry(this.bot, this.scene, {presentationOnly: true});
    this.entry.root.parent = this.turntable;
    this.entry.isAlive = true;
    this.entry._interpReady = true;
    this.entry.botData = this.bot;
    this._trailEntries.set(this.bot.bot_id, this.entry);
    updateForgeCharacter(this.entry, 0, true);
    applyBotCosmetics(this.entry, this.bot, this.scene, {forceEnabled: true});
    this._lastFrame = performance.now();
    this._renderRequested = true;
    return this;
  }

  rotateBy(radians) {
    if (this.turntable && Number.isFinite(radians)) {
      this.turntable.rotation.y += radians;
      this._renderRequested = true;
    }
    return this;
  }

  resetRotation() {
    if (this.turntable) this.turntable.rotation.y = 0;
    if (this.camera) {
      this.camera.alpha = DEFAULT_ALPHA;
      this.camera.beta = DEFAULT_BETA;
      this.camera.radius = DEFAULT_RADIUS;
    }
    this._renderRequested = true;
    return this;
  }

  resize() {
    if (this.engine) this.engine.resize();
    this._renderRequested = true;
    return this;
  }

  dispose() {
    if (this._disposed) return;
    this._disposed = true;
    this.ready = false;

    if (this._resizeObserver) this._resizeObserver.disconnect();
    if (this._visibilityObserver) this._visibilityObserver.disconnect();
    if (this._resizeHandler) window.removeEventListener('resize', this._resizeHandler);
    if (this._motionQuery?.removeEventListener) {
      this._motionQuery.removeEventListener('change', this._motionHandler);
    } else if (this._motionQuery?.removeListener) {
      this._motionQuery.removeListener(this._motionHandler);
    }
    if (this._pageHideHandler) window.removeEventListener('pagehide', this._pageHideHandler);
    if (this._pageShowHandler) window.removeEventListener('pageshow', this._pageShowHandler);
    document.removeEventListener?.('visibilitychange', this._visibilityHandler);
    for (const type of ['pointerdown', 'pointermove', 'wheel', 'keydown']) {
      this.canvas.removeEventListener?.(type, this._interactionHandler, {passive: type === 'wheel'});
    }

    // Weapon finishes clone shared materials, so restore and dispose cosmetic
    // state before the underlying weapon/model hierarchy is destroyed.
    if (this.entry) {
      disposeBotCosmetics(this.entry);
      disposeBotEntry(this.entry);
      this.entry = null;
    }
    if (this.trailRenderer) {
      this.trailRenderer.dispose();
      this.trailRenderer = null;
    }
    this._trailEntries.clear();
    if (this.camera) this.camera.detachControl(this.canvas);
    if (this.engine && this._renderFrame) this.engine.stopRenderLoop(this._renderFrame);
    if (this.scene) this.scene.dispose();
    if (this.engine) this.engine.dispose();

    this.turntable = null;
    this.camera = null;
    this.scene = null;
    this.engine = null;
  }
}
