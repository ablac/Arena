'use strict';

/**
 * Babylon.js engine — scene setup, render loop, module orchestration.
 * @module renderer/engine
 */

import { CameraController } from './camera.js?v=20260705c';
import { BotRenderer } from './bots.js?v=20260706a';
import { EnvironmentRenderer } from './environment.js?v=20260705c';
import { ObstacleRenderer } from './obstacles.js?v=20260521h';
import { PickupRenderer } from './pickups.js?v=20260521m';
import { EffectRenderer } from './effects.js?v=20260705c';
import { TrailRenderer } from './trails.js?v=20260705d';
import { ProjectileRenderer } from './projectiles.js?v=20260521l';
import { GameplayRenderer } from './gameplay.js?v=20260705a';

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
    this.gameplayRenderer = null;
    this.state = null;
    this.ready = false;
    this._seenArenaEvents = new Set();
  }

  /** Initialize Babylon engine. */
  async init() {
    const B = window.BABYLON;
    let engine;
    try {
      const webGPUSupported = await B.WebGPUEngine.IsSupportedAsync;
      if (webGPUSupported) {
        engine = new B.WebGPUEngine(this.canvas, { antialias: false });
        await engine.initAsync();
        console.log('[Arena] WebGPU');
      } else {
        throw new Error('WebGPU not supported');
      }
    } catch {
      engine = new B.Engine(this.canvas, false, {
        preserveDrawingBuffer: false,
        stencil: false,
      });
      console.log('[Arena] WebGL');
    }
    // Cap at 1x device pixel ratio to prevent supersampling on HiDPI
    engine.setHardwareScalingLevel(1.0 / Math.min(window.devicePixelRatio, 1));
    this.engine = engine;
    const scene = new B.Scene(engine);
    this.scene = scene;
    scene.clearColor = new B.Color4(0, 0, 0.02, 1); // near-black to match starfield skybox
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
    this.obstacleRenderer = new ObstacleRenderer(scene, this.envRenderer);
    this.botRenderer = new BotRenderer(scene);
    this.pickupRenderer = new PickupRenderer(scene);
    this.effectRenderer = new EffectRenderer(scene);
    this.effectRenderer.camera = this.camera;
    this.trailRenderer = new TrailRenderer(scene);
    this.projectileRenderer = new ProjectileRenderer(scene);
    this.gameplayRenderer = new GameplayRenderer(scene);
    this.gameplayRenderer.onStaffImpactCreated = (impact) => {
      const owner = (this.state?.bots || []).find((bot) => (bot.bot_id || bot.id) === impact.ownerId);
      if (!owner || !impact?.position) return;
      this.projectileRenderer.spawn(
        owner.position[0],
        owner.position[1],
        impact.position[0],
        impact.position[1],
        'staff',
        owner.avatar_color || '#8d4dff',
        undefined,
        { travelTime: Math.max(0.16, (impact.ticksLeft || 1) / 10) }
      );
    };
    this.botRenderer.onSelectionChange = (botId) => {
      if (this.onSelectBot) this.onSelectBot(botId);
    };
    scene.onPointerObservable.add((pointerInfo) => {
      const B = window.BABYLON;
      if (pointerInfo.type !== B.PointerEventTypes.POINTERDOWN) return;
      const pickedMesh = pointerInfo.pickInfo?.pickedMesh || null;
      if (!this.botRenderer.handlePick(pickedMesh)) {
        this.botRenderer.clearSelection();
      }
    });

    // Wire up attack → direct combat effects for non-event-driven weapons.
    // Effects are delayed to the CONTACT moment of the swing (opts.contactDelay,
    // computed from the weapon's windup+active phases) so sparks, the strike
    // read, and the victim's reaction land when the blow visually connects
    // instead of at swing start. setTimeout at event rate is fine; per-frame
    // work stays allocation-free.
    this.botRenderer.onAttack = (ax, az, tx, tz, color, weapon, opts) => {
      if (weapon === 'bow' || weapon === 'staff') {
        return;
      }
      const delayMs = Math.max(0, (opts?.contactDelay || 0) * 1000);
      const targetId = opts?.targetId || null;
      // Capture the scene live at schedule time. The between-round
      // _rebuildForArenaSize disposes and re-inits the scene, so a delayed
      // contact callback could otherwise fire against a disposed scene (final
      // teardown) or spawn a stale strike on the fresh scene (rebuild). Bail
      // unless the exact scene that owned this swing is still the live one.
      const swingScene = this.scene;
      setTimeout(() => {
        if (!this.effectRenderer || this.scene !== swingScene || swingScene.isDisposed) return;
        this.effectRenderer.spawnWeaponStrike(ax, az, tx, tz, color, weapon);
        this.effectRenderer.spawnHitSparks(tx, tz, color, weapon);
        if (targetId && this.botRenderer) {
          const victim = this.botRenderer.entries.get(targetId);
          if (victim && victim.isAlive) {
            this.botRenderer.playImpactReaction(targetId, ax, az);
          }
        }
      }, delayMs);
    };

    // Wire up dodge → afterimage shimmer
    this.botRenderer.onDodge = (x, z, color) => {
      this.effectRenderer.spawnDodgeEffect(x, z, color);
    };

    // Wire up shove → shockwave blast effect
    this.botRenderer.onShove = (ax, az, tx, tz, color) => {
      this.effectRenderer.spawnShoveEffect(ax, az, tx, tz, color);
    };

    this._addLights();
    this.envRenderer.setupShadows(this.sunLight);

    // DefaultRenderingPipeline: stable FXAA + tone mapping, light sharpen only.
    const pipeline = new B.DefaultRenderingPipeline('defaultPipeline', true, this.scene, [this.camera.camera]);
    if (pipeline.isSupported) {
      pipeline.fxaaEnabled = true;
      pipeline.sharpenEnabled = true;
      pipeline.sharpen.edgeAmount = 0.15;
      pipeline.sharpen.colorAmount = 1.0;
      pipeline.imageProcessingEnabled = true;
      pipeline.imageProcessing.toneMappingEnabled = true;
      pipeline.imageProcessing.toneMappingType = B.ImageProcessingConfiguration.TONEMAPPING_ACES;
      pipeline.imageProcessing.exposure = 1.0;
      pipeline.imageProcessing.contrast = 1.1;
      // Bloom: the scene is built almost entirely from emissive materials and
      // additive particles (trims, rings, trails, explosions) but nothing glowed.
      // High threshold so only genuine highlights bloom; ACES keeps them controlled.
      // bloomScale 0.5 halves the post-pass cost for projector laptops.
      pipeline.bloomEnabled = true;
      pipeline.bloomThreshold = 0.75;
      pipeline.bloomWeight = 0.3;
      pipeline.bloomKernel = 48;
      pipeline.bloomScale = 0.5;
      // Subtle vignette frames the arena on a big screen.
      pipeline.imageProcessing.vignetteEnabled = true;
      pipeline.imageProcessing.vignetteWeight = 1.6;
      pipeline.imageProcessing.vignetteColor = new B.Color4(0, 0, 0.05, 0);
    }

    const self = this;
    let _lastFrame = performance.now();
    engine.runRenderLoop(() => {
      const now = performance.now();
      const dt = Math.min((now - _lastFrame) / 1000, 0.1);
      _lastFrame = now;
      if (self.botRenderer) {
        self.botRenderer.interpolate();
      }
      if (self.trailRenderer) {
        self.trailRenderer.render(self.botRenderer ? self.botRenderer.entries : null, dt);
      }
      if (self.projectileRenderer) {
        self.projectileRenderer.update(dt);
      }
      if (self.gameplayRenderer) {
        self.gameplayRenderer.animate(self.botRenderer ? self.botRenderer.entries : null, dt);
      }
      scene.render();
    });
    this._resizeHandler = () => engine.resize();
    window.addEventListener('resize', this._resizeHandler);
    this.ready = true;
  }

  /** @private */
  _addLights() {
    const B = window.BABYLON;
    const dir = new B.DirectionalLight('sun', new B.Vector3(-0.4, -1, 0.3), this.scene);
    dir.position = new B.Vector3(0, 80, -40);
    dir.intensity = 0.82;
    dir.diffuse = new B.Color3(1, 0.95, 0.85);
    dir.specular = new B.Color3(0.34, 0.34, 0.34);
    this.sunLight = dir;

    const hemi = new B.HemisphericLight('hemi', new B.Vector3(0, 1, 0), this.scene);
    hemi.intensity = 0.46;
    hemi.diffuse = new B.Color3(0.66, 0.72, 0.88);
    hemi.specular = B.Color3.Black();
    hemi.groundColor = new B.Color3(0.09, 0.1, 0.12);
  }

  /**
   * Feed arena state from spectator WS.
   * @param {Object} state
   */
  setState(state) {
    if (!this.ready || state.type !== 'arena_state') return;

    // Dynamic arena sizing: the map can change dimensions between rounds
    // (it grows with bot count). Keyframe states carry arena_size; when it
    // differs from the scene we built, rebuild the whole scene at the new
    // size. This only ever happens at round boundaries.
    const size = state.arena_size;
    if (size && size.length === 2 && !this._resizing &&
        (size[0] !== this.arenaWidth || size[1] !== this.arenaHeight)) {
      this._rebuildForArenaSize(size[0], size[1], state);
      return;
    }

    this.state = state;
    this._playArenaEvents(state.events || [], state);
    this.obstacleRenderer.update(state.obstacles);
    this.envRenderer.update(state.safe_zone);
    this.botRenderer.update(state.bots);
    this.pickupRenderer.update(state.pickups || []);
    this.effectRenderer.update(state.bots);
    this.gameplayRenderer.update(state);
    this.camera.updateBotPositions(state.bots);
  }

  /**
   * @private Tear down and rebuild the whole scene at new arena dimensions.
   * Every renderer has a dispose path and module-level caches are
   * scene-aware, so a full rebuild is safe; external wiring (controls, HUD
   * callbacks) stays valid because the ArenaEngine instance survives.
   */
  async _rebuildForArenaSize(w, h, state) {
    this._resizing = true;
    console.log(`[Arena] arena size changed to ${w}x${h} — rebuilding scene`);
    const prevFollow = this.camera ? this.camera.followId : null;
    const prevZoom = this.camera ? this.camera.zoom : null;
    this.ready = false;
    try {
      this.dispose();
      this.arenaWidth = w;
      this.arenaHeight = h;
      this.state = null;
      await this.init();
      if (prevZoom) this.setZoom(prevZoom);
      if (prevFollow) this.followBot(prevFollow);
    } catch (err) {
      console.error('[Arena] scene rebuild failed:', err);
    } finally {
      this._resizing = false;
    }
    // Apply the keyframe that triggered the rebuild so the new scene
    // populates immediately instead of waiting for the next broadcast.
    if (this.ready && state) this.setState(state);
  }

  _playArenaEvents(events, state) {
    for (const ev of events) {
      if (!ev || !ev.id || this._seenArenaEvents.has(ev.id)) continue;
      this._seenArenaEvents.add(ev.id);

      if (this._seenArenaEvents.size > 256) {
        const first = this._seenArenaEvents.values().next();
        if (!first.done) this._seenArenaEvents.delete(first.value);
      }

      if (ev.type === 'teleport' && ev.from_position && ev.to_position) {
        this.effectRenderer.spawnTeleportBurst(
          ev.from_position[0], ev.from_position[1],
          ev.to_position[0], ev.to_position[1],
          ev.color || '#00ffff'
        );
      } else if (ev.type === 'bow_fired' && ev.from_position && ev.to_position) {
        this.projectileRenderer.spawn(
          ev.from_position[0], ev.from_position[1],
          ev.to_position[0], ev.to_position[1],
          'bow',
          ev.color || '#f0e6c9',
          undefined,
          { intensity: ev.intensity || 1 },
        );
      } else if (ev.type === 'bow_impact' && ev.position) {
        this.effectRenderer.spawnBowImpact(
          ev.position[0], ev.position[1],
          ev.color || '#f0e6c9',
          !!ev.target_id,
          ev.intensity || 1
        );
        if (ev.target_id && this.botRenderer) {
          this.botRenderer.playImpactReaction(ev.target_id);
        }
      } else if (ev.type === 'spear_brace' && ev.from_position && ev.position) {
        this.effectRenderer.spawnSpearBrace(
          ev.from_position[0], ev.from_position[1],
          ev.position[0], ev.position[1],
          ev.color || '#ffe38a'
        );
        if (ev.target_id && this.botRenderer) {
          this.botRenderer.playImpactReaction(ev.target_id);
        }
      } else if (ev.type === 'shield_bash' && ev.from_position && ev.position) {
        this.effectRenderer.spawnShieldBash(
          ev.from_position[0], ev.from_position[1],
          ev.position[0], ev.position[1],
          ev.color || '#bfe3ff'
        );
        if (ev.target_id && this.botRenderer) {
          this.botRenderer.playImpactReaction(ev.target_id);
        }
      } else if (ev.type === 'backstab' && ev.from_position && ev.position) {
        this.effectRenderer.spawnBackstab(
          ev.from_position[0], ev.from_position[1],
          ev.position[0], ev.position[1],
          ev.color || '#ff8f47'
        );
        if (ev.target_id && this.botRenderer) {
          this.botRenderer.playImpactReaction(ev.target_id);
        }
      } else if ((ev.type === 'grapple_pull' || ev.type === 'grapple_anchor') && ev.from_position && ev.to_position) {
        const owner = (state?.bots || []).find((b) => b.bot_id === ev.owner_id || b.id === ev.owner_id);
        const anchor = ev.position || ev.to_position;
        if (ev.type === 'grapple_pull' && owner) {
          this.effectRenderer.spawnGrappleEffect(
            owner.position[0], owner.position[1],
            ev.from_position[0], ev.from_position[1],
            { mode: 'pull', endX: ev.to_position[0], endZ: ev.to_position[1], color: ev.color || '#59f1ff' }
          );
        } else {
          this.effectRenderer.spawnGrappleEffect(
            ev.from_position[0], ev.from_position[1],
            anchor[0], anchor[1],
            { mode: 'anchor', endX: ev.to_position[0], endZ: ev.to_position[1], color: ev.color || '#59f1ff' }
          );
        }
      } else if (ev.type === 'grapple_slam' && ev.from_position && ev.position) {
        this.effectRenderer.spawnGrappleSlam(
          ev.from_position[0], ev.from_position[1],
          ev.position[0], ev.position[1],
          ev.color || '#59f1ff'
        );
        if (ev.target_id && this.botRenderer) {
          this.botRenderer.playImpactReaction(ev.target_id);
        }
      } else if (ev.type === 'flag_captured' && ev.position) {
        // CTF capture: celebratory burst at the base.
        this.effectRenderer.spawnMineExplosion(ev.position[0], ev.position[1], 30);
        this.effectRenderer.spawnHitSparks(ev.position[0], ev.position[1], '#ffd700', 'sword');
      } else if ((ev.type === 'flag_taken' || ev.type === 'flag_returned' || ev.type === 'flag_dropped') && ev.position) {
        this.effectRenderer.spawnHitSparks(
          ev.position[0], ev.position[1],
          ev.type === 'flag_taken' ? '#ff5a4d' : '#7ef7ff',
          'sword'
        );
      } else if (ev.type === 'mine_detonated' && ev.position) {
        this.effectRenderer.spawnMineExplosion(
          ev.position[0], ev.position[1],
          (ev.radius || 1) * 20
        );
      } else if (ev.type === 'staff_detonated' && ev.position) {
        this.effectRenderer.spawnStaffExplosion(
          ev.position[0], ev.position[1],
          (ev.radius || 1) * 20,
          ev.color || '#8d4dff'
        );
      } else if (ev.type === 'capture_pad_captured' && ev.position) {
        this.effectRenderer.spawnCapturePadPulse(
          ev.position[0], ev.position[1],
          (ev.radius || 2) * 20,
          ev.color || '#7ef7ff'
        );
      }
    }
  }

  setZoom(z) { if (this.camera) this.camera.setZoom(z); }
  followBot(id) { if (this.camera) this.camera.followBot(id); }
  setAutoPan(on) { if (this.camera) this.camera.setAutoPan(on); }
  getState() { return this.state; }
  selectBot(id) { if (this.botRenderer) this.botRenderer.selectBot(id); }

  dispose() {
    if (this._resizeHandler) {
      window.removeEventListener('resize', this._resizeHandler);
    }
    if (this.projectileRenderer) this.projectileRenderer.dispose();
    if (this.trailRenderer) this.trailRenderer.dispose();
    if (this.envRenderer && this.envRenderer.dispose) this.envRenderer.dispose();
    if (this.engine) {
      this.engine.stopRenderLoop();
      this.scene.dispose();
      this.engine.dispose();
    }
  }
}
