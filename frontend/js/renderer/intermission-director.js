'use strict';

/**
 * Intermission show director (issue #189).
 *
 * Turns the between-round window into a staged spectator show driven by the
 * server's typed `round_end` broadcast:
 *
 *   Phase A (~2.5s) — winner banner overlay + hero light pillar on the
 *     winner, then an ascend/shrink despawn; every other lingering bot entry
 *     teleports out. Bots are removed through BotRenderer's normal entry
 *     disposal path so the next round's first snapshot rebuilds them cleanly.
 *   Phase B (~2s)  — the current obstacle merged meshes and boundary wall
 *     sink into the floor with a few pooled dust bursts.
 *   Phase C        — the next map (pre-announced in `round_end.next_map`)
 *     builds itself: the smooth boundary wall rises while its glow trim
 *     reveals segment by segment around the contour, and obstacles rise as
 *     staggered transient clones. On completion the transient geometry is
 *     disposed and the REAL renderers receive the exact keyframe-shaped
 *     data, so the swap to the live round is a no-op.
 *
 * Robustness: the first arena_state of the NEXT round (or a stale show,
 * >60s) fast-forwards everything — overlays dismissed, transient meshes
 * disposed, teardown transforms restored. No `round_end` message (old
 * server) means the feature is completely inert.
 *
 * Every phase is individually gated by the `intermissionShow` settings
 * section. Settings are cached via onSettingsChange; update(dt) runs from
 * the engine render loop and performs no isEnabled() reads (issue #180
 * discipline, pinned by scripts/test-renderer-suspension.mjs).
 *
 * The timeline/scheduling logic below is window-free and exported so
 * scripts/test-intermission-director.mjs can unit-test it in node.
 * @module renderer/intermission-director
 */

import { buildBoundaryContours, buildWallGeometry, contourNormals, resolveEarcut, WALL_HEIGHT } from './map-walls.js?v=20260718d';
import { parseColor } from './utils.js';
import { isEnabled, onSettingsChange } from '../settings.js';

/* ------------------------------------------------------------------------ */
/* Pure timeline logic                                                       */
/* ------------------------------------------------------------------------ */

export const WINNER_PHASE_SECS = 2.5;
export const TEARDOWN_PHASE_SECS = 2.0;
export const MIN_CONSTRUCTION_SECS = 2.5;
export const TITLE_CARD_SECS = 2.0;
/** A show with no new-round state after this long is stale — snap it away. */
export const STALE_SHOW_SECS = 60;
/** Per-box transient clone budget; above it the stagger runs merged groups. */
export const MAX_OBSTACLE_CLONES = 120;
export const OBSTACLE_CLONE_GROUPS = 10;
/** Max transient trim-reveal meshes for the boundary contour. */
export const MAX_TRIM_STRIPS = 24;

/** Winner-despawn choreography beats inside phase A (seconds from show start). */
export const DESPAWN_OTHERS_AT = 0.35;
export const WINNER_ASCEND_START = 1.0;
export const WINNER_ASCEND_SECS = 1.4;

/**
 * Normalize/validate a raw round_end message. Returns null when the message
 * is not a usable round_end envelope (feature stays inert). Winner is null
 * on a draw; nextMap is null when the server did not attach a usable map.
 */
export function summarizeRoundEnd(msg) {
  if (!msg || msg.type !== 'round_end') return null;
  const roundNumber = Number(msg.round_number);
  if (!Number.isFinite(roundNumber)) return null;
  const secs = Number(msg.intermission_secs);
  const winner = msg.winner && typeof msg.winner === 'object' && msg.winner.id
    ? {
        id: String(msg.winner.id),
        name: String(msg.winner.name || ''),
        color: typeof msg.winner.color === 'string' ? msg.winner.color : '',
      }
    : null;
  const nm = msg.next_map;
  const nextMap = nm && typeof nm === 'object' &&
    Array.isArray(nm.obstacles) &&
    Array.isArray(nm.arena_size) && nm.arena_size.length === 2
    ? {
        shape: typeof nm.shape === 'string' && nm.shape ? nm.shape : 'square',
        arenaSize: [Number(nm.arena_size[0]) || 0, Number(nm.arena_size[1]) || 0],
        obstacles: nm.obstacles,
        maskRects: Array.isArray(nm.mask_rects) ? nm.mask_rects : [],
      }
    : null;
  return {
    roundNumber,
    intermissionSecs: Number.isFinite(secs) && secs > 0 ? secs : 10,
    winner,
    nextMap,
  };
}

/**
 * Phase schedule for a show. Disabled phases collapse to zero so the next
 * phase starts immediately; construction gets whatever remains of the
 * intermission (with a floor so short intermissions still read as a build).
 * @param {number} intermissionSecs
 * @param {{winnerBanner?:boolean,botDespawn?:boolean,mapTeardown?:boolean,mapConstruction?:boolean}} flags
 */
export function planPhases(intermissionSecs, flags = {}) {
  const secs = Number.isFinite(intermissionSecs) && intermissionSecs > 0 ? intermissionSecs : 10;
  const winner = (flags.winnerBanner || flags.botDespawn) ? WINNER_PHASE_SECS : 0;
  const teardown = flags.mapTeardown ? TEARDOWN_PHASE_SECS : 0;
  const constructionStart = winner + teardown;
  const construction = flags.mapConstruction
    ? Math.max(MIN_CONSTRUCTION_SECS, secs - constructionStart - 0.5)
    : 0;
  return { winner, teardown, constructionStart, constructionEnd: constructionStart + construction };
}

/**
 * The pure show clock: phase resolution, monotonic construction progress,
 * re-pacing against the observed lobby countdown, staleness, fast-forward.
 * Owns no Babylon/DOM state — the director maps its outputs onto the scene.
 */
export class IntermissionTimeline {
  constructor(intermissionSecs, flags = {}) {
    this.flags = { ...flags };
    this.plan = planPhases(intermissionSecs, flags);
    this.elapsed = 0;
    this.constructionEnd = this.plan.constructionEnd;
    this.constructionP = 0;
    this.finished = false;
  }

  /** Advance the clock. Construction progress is monotonic: a re-paced
   *  (extended) deadline pauses it rather than running it backwards. */
  advance(dt) {
    this.elapsed += Math.max(0, Number(dt) || 0);
    if (this.flags.mapConstruction && !this.finished &&
        this.elapsed >= this.plan.constructionStart) {
      const span = Math.max(0.001, this.constructionEnd - this.plan.constructionStart);
      const p = (this.elapsed - this.plan.constructionStart) / span;
      this.constructionP = Math.min(1, Math.max(this.constructionP, p));
    }
  }

  /** Lobby countdown observed: the round starts in `secs` — re-pace the
   *  construction to land just before it. */
  noteRoundCountdown(secs) {
    if (!Number.isFinite(secs) || secs <= 0 || this.constructionP >= 1) return;
    const target = this.elapsed + Math.max(0.75, secs - 0.5);
    this.constructionEnd = Math.max(this.plan.constructionStart + 0.5, target);
  }

  /** Snap everything complete (new round arrived / show dismissed). */
  fastForward() {
    this.finished = true;
    if (this.flags.mapConstruction) this.constructionP = 1;
  }

  get stale() { return this.elapsed > STALE_SHOW_SECS; }

  phase() {
    if (this.finished) return 'done';
    if (this.elapsed < this.plan.winner) return 'winner';
    if (this.elapsed < this.plan.winner + this.plan.teardown) return 'teardown';
    if (this.flags.mapConstruction && this.constructionP < 1) return 'construction';
    return 'waiting';
  }

  /** Teardown-phase progress 0..1 (0 before, 1 after). */
  teardownT() {
    if (!this.plan.teardown) return this.elapsed >= this.plan.winner ? 1 : 0;
    return clamp01((this.elapsed - this.plan.winner) / this.plan.teardown);
  }

  /** Whether the "ROUND N — SHAPE" title card should be on screen. */
  titleCardVisible() {
    if (!this.flags.mapConstruction || this.finished) return false;
    return this.elapsed >= this.constructionEnd - TITLE_CARD_SECS &&
      this.elapsed <= this.constructionEnd + 1.0;
  }
}

/**
 * Stagger schedule for the obstacle rise: boxes sorted by distance from the
 * arena centre, each rising over a normalized-progress window, consecutive
 * windows overlapping ~60% so the wave reads as one motion. All rises finish
 * by p=0.9, leaving the tail of the construction for the trim to close.
 * @param {Array<{x:number,y:number,width:number,height:number}>} boxes
 * @returns {Array<{index:number,start:number,dur:number}>} in input order of `boxes` indices
 */
export function obstacleRiseSchedule(boxes, arenaW, arenaH) {
  const n = boxes.length;
  if (!n) return [];
  const cx = (Number(arenaW) || 0) / 2;
  const cz = (Number(arenaH) || 0) / 2;
  const order = boxes
    .map((o, i) => ({ i, d: Math.hypot(o.x + o.width / 2 - cx, o.y + o.height / 2 - cz) }))
    .sort((a, b) => a.d - b.d || a.i - b.i);
  // Solving span = dur * (1 + 0.4*(n-1)) = 0.9 gives exactly 60% overlap
  // between consecutive windows; the clamp keeps tiny/huge counts sane.
  const dur = Math.min(0.6, Math.max(0.08, 0.9 / (1 + 0.4 * Math.max(0, n - 1))));
  const step = dur * 0.4;
  return order.map((e, k) => ({
    index: e.i,
    start: Math.min(k * step, 0.9 - dur),
    dur,
  }));
}

/**
 * Split the boundary contours (outer + holes across all groups) into at most
 * ~maxStrips contiguous point runs for the progressive trim reveal. Each
 * strip carries its points and matching outward normals; consecutive strips
 * share their boundary point so the revealed band stays gap-free. The strip
 * count is bounded by maxStrips plus one per tiny contour (a contour never
 * splits below one strip).
 * @param {Array<{outer:Array<[number,number]>,holes:Array<Array<[number,number]>>}>} groups
 */
export function splitContoursIntoStrips(groups, maxStrips = MAX_TRIM_STRIPS) {
  const contours = [];
  let totalPts = 0;
  for (const g of groups) {
    for (const c of [g.outer, ...g.holes]) {
      if (c && c.length >= 2) {
        contours.push(c);
        totalPts += c.length;
      }
    }
  }
  if (!totalPts) return [];
  const budget = Math.max(1, maxStrips);
  const strips = [];
  for (const c of contours) {
    const share = Math.max(1, Math.floor((c.length / totalPts) * budget));
    const per = Math.ceil(c.length / share); // ceil(len/ceil(len/share)) <= share strips
    const norms = contourNormals(c);
    for (let s = 0; s < c.length; s += per) {
      const end = Math.min(c.length, s + per);
      const pts = [];
      const ns = [];
      for (let i = s; i <= end; i++) { // include the wrap point to close gaps
        const idx = i % c.length;
        pts.push(c[idx]);
        ns.push(norms[idx]);
      }
      strips.push({ points: pts, normals: ns });
    }
  }
  return strips;
}

/**
 * Trim band + outer skirt geometry for one OPEN strip of contour points —
 * the open-run variant of map-walls' buildTrimGeometry (same ±1.5 band,
 * +0.8 lift, 2.4 skirt constants so the reveal matches the final trim).
 */
export function buildTrimStripGeometry(points, normals, { height = WALL_HEIGHT } = {}) {
  const OUT = 1.5;
  const INN = 1.5;
  const LIFT = 0.8;
  const SKIRT = 2.4;
  const positions = [];
  const norms = [];
  const indices = [];
  const n = points.length;
  const y = height + LIFT;
  const quadStrip = (count, base) => {
    for (let i = 0; i < count - 1; i++) {
      const a = base + i * 2;
      const b = base + (i + 1) * 2;
      indices.push(a, a + 1, b, b, a + 1, b + 1);
    }
  };
  let base = 0;
  for (let i = 0; i < n; i++) {
    const [x, z] = points[i];
    const [nx, nz] = normals[i];
    positions.push(x + nx * OUT, y, z + nz * OUT, x - nx * INN, y, z - nz * INN);
    norms.push(0, 1, 0, 0, 1, 0);
  }
  quadStrip(n, base);
  base = positions.length / 3;
  for (let i = 0; i < n; i++) {
    const [x, z] = points[i];
    const [nx, nz] = normals[i];
    positions.push(x + nx * OUT, y + 1.0, z + nz * OUT, x + nx * OUT, y - SKIRT, z + nz * OUT);
    norms.push(nx, 0, nz, nx, 0, nz);
  }
  quadStrip(n, base);
  return { positions, normals: norms, indices };
}

function clamp01(v) { return v < 0 ? 0 : (v > 1 ? 1 : v); }
function easeOutCubic(t) { const u = 1 - clamp01(t); return 1 - u * u * u; }
function easeInQuad(t) { const u = clamp01(t); return u * u; }

/* ------------------------------------------------------------------------ */
/* Babylon/DOM-facing director                                               */
/* ------------------------------------------------------------------------ */

export class IntermissionDirector {
  /** @param {import('./engine.js').ArenaEngine} engine */
  constructor(engine) {
    this.engine = engine;
    /** @type {Object|null} live show state; null = inert */
    this._show = null;
    this._overlayRoot = null;
    this._banner = null;
    this._titleCard = null;
    this._trimMat = null;
    this._motionQuery = typeof window !== 'undefined' && typeof window.matchMedia === 'function'
      ? window.matchMedia('(prefers-reduced-motion: reduce)')
      : null;
    // Settings are cached here and refreshed on change so update(dt) — which
    // runs on the render loop — never reads isEnabled() itself.
    this._flags = this._readFlags();
    this._unsubSettings = onSettingsChange(() => { this._flags = this._readFlags(); });
  }

  _readFlags() {
    return {
      winnerBanner: isEnabled('intermissionShow', 'winnerBanner'),
      botDespawn: isEnabled('intermissionShow', 'botDespawn'),
      mapTeardown: isEnabled('intermissionShow', 'mapTeardown'),
      mapConstruction: isEnabled('intermissionShow', 'mapConstruction'),
      smoothWalls: isEnabled('arenaAmbience', 'smoothMapWalls'),
    };
  }

  get active() { return !!this._show; }

  /** While true the engine must not feed BotRenderer.update — the director
   *  owns bot visuals (they've been despawned for the show). */
  holdsBots() { return !!(this._show && this._show.flags.botDespawn); }

  /** While true the engine must not feed ObstacleRenderer.update — stale
   *  intermission keyframes still carry the ENDED round's layout and would
   *  rebuild the map the teardown just sank (or clobber the construction
   *  handoff). */
  holdsWorld() { return !!(this._show && (this._show.flags.mapTeardown || this._show.flags.mapConstruction)); }

  /** Entry point for the server's round_end broadcast. */
  handleRoundEnd(msg) {
    const summary = summarizeRoundEnd(msg);
    if (!summary) return;
    if (this._show) this.fastForward();
    const flags = { ...this._flags };
    if (!flags.winnerBanner && !flags.botDespawn && !flags.mapTeardown && !flags.mapConstruction) {
      return; // every phase toggled off — show fully inert
    }
    this._show = {
      summary,
      flags,
      timeline: new IntermissionTimeline(summary.intermissionSecs, flags),
      lastPhase: 'idle',
      bannerShown: false,
      bannerDismissed: false,
      othersDespawned: false,
      heroSpawned: false,
      ascend: null,
      teardown: null,
      construction: null,
      titleShown: false,
      cameraPushed: false,
      fx: [],
    };
    // Gentle camera push toward the winner ONLY while auto-pan is driving
    // the camera (a manual or following camera is never fought), and never
    // under the OS reduced-motion preference. Restored on fast-forward.
    const cam = this.engine.camera;
    if (summary.winner && cam && cam.autoPan &&
        !(this._motionQuery && this._motionQuery.matches)) {
      cam.followBot(summary.winner.id);
      this._show.cameraPushed = true;
    }
  }

  /** Lobby countdown observations re-pace the construction deadline. */
  handleLobbyState(state) {
    if (!this._show) return;
    const secs = Number(state && state.countdown);
    if (Number.isFinite(secs) && secs > 0) this._show.timeline.noteRoundCountdown(secs);
  }

  /** Every arena_state passes through here; the first one belonging to the
   *  NEXT round snap-completes the show before normal processing. */
  handleArenaState(state) {
    if (!this._show) return;
    const rn = Number(state && state.round_number);
    if (Number.isFinite(rn) && rn > this._show.summary.roundNumber) this.fastForward();
  }

  /**
   * Per-frame hook, called from the engine render loop after the entity
   * renderers. Reads only cached settings flags — no isEnabled() here.
   */
  update(dt) {
    const show = this._show;
    if (!show) return;
    const tl = show.timeline;
    tl.advance(dt);
    if (tl.stale) {
      this.fastForward();
      return;
    }
    const phase = tl.phase();
    if (phase !== show.lastPhase) {
      this._enterPhase(show, phase);
      show.lastPhase = phase;
    }
    this._updateWinnerChoreo(show);
    this._updateTeardown(show);
    this._updateConstruction(show);
    this._updateTitleCard(show);
    this._updateTransientFx(show);
  }

  /**
   * Snap the show complete: dismiss overlays, dispose transient geometry,
   * restore teardown transforms, release pooled effects, restore the camera.
   * Called on the new round's first arena_state, staleness, disposal, or a
   * round_end arriving over a live show.
   */
  fastForward() {
    const show = this._show;
    if (!show) return;
    this._show = null;
    show.timeline.fastForward();
    this._dismissBanner();
    this._dismissTitleCard();
    for (const fx of show.fx) this._releaseFx(fx);
    show.fx.length = 0;
    // Winner mid-ascend: drop the entry through the normal disposal path so
    // its lifted/shrunk root can never leak into the next round.
    if (show.ascend && this.engine.botRenderer) {
      this.engine.botRenderer.despawnEntry(show.ascend.id);
      show.ascend = null;
    }
    this._restoreTeardownMeshes(show);
    this._disposeConstructionArtifacts(show);
    if (show.cameraPushed && this.engine.camera) this.engine.camera.setAutoPan(true);
  }

  dispose() {
    this.fastForward();
    if (this._unsubSettings) {
      this._unsubSettings();
      this._unsubSettings = null;
    }
    if (this._overlayRoot) {
      this._overlayRoot.remove();
      this._overlayRoot = null;
    }
    this._banner = null;
    this._titleCard = null;
    if (this._trimMat) {
      this._trimMat.dispose();
      this._trimMat = null;
    }
  }

  /* ---------------------------- phase entries --------------------------- */

  _enterPhase(show, phase) {
    if (phase === 'winner') {
      if (show.flags.winnerBanner && !show.bannerShown) {
        show.bannerShown = true;
        this._showBanner(show.summary.winner);
      }
      return;
    }
    // Any later phase implies the winner beat is over (it may have been
    // skipped entirely when both of its toggles are off).
    this._finishWinnerPhase(show);
    if (phase === 'teardown' && show.flags.mapTeardown && !show.teardown) {
      this._beginTeardown(show);
    }
    if (phase === 'construction' && show.flags.mapConstruction && !show.construction) {
      // A null build (arena resize next round / unusable map data) degrades
      // the phase to the title card alone; the empty artifact lists keep the
      // dispose/fast-forward paths uniform.
      show.construction = this._beginConstruction(show) ||
        { ok: false, handoffDone: true, wallMesh: null, strips: [], clones: [] };
    }
  }

  _finishWinnerPhase(show) {
    if (show.flags.winnerBanner && show.bannerShown && !show.bannerDismissed) {
      show.bannerDismissed = true;
      this._dismissBanner();
    }
  }

  /* ------------------------- phase A: winner/despawn --------------------- */

  _updateWinnerChoreo(show) {
    const tl = show.timeline;
    const bots = this.engine.botRenderer;
    if (!show.flags.botDespawn || !bots) {
      // Banner-only shows still need the banner to auto-dismiss on schedule.
      if (tl.elapsed >= tl.plan.winner) this._finishWinnerPhase(show);
      return;
    }
    const winner = show.summary.winner;

    // Beat 1: every lingering non-winner bot teleports out.
    if (!show.othersDespawned && tl.elapsed >= DESPAWN_OTHERS_AT) {
      show.othersDespawned = true;
      const fx = this.engine.effectRenderer;
      const canSpawn = fx && this.engine.shouldSpawnEffects && this.engine.shouldSpawnEffects();
      for (const [id, entry] of [...bots.entries]) {
        if (winner && id === winner.id) continue;
        const root = entry.root;
        const visible = root && (typeof root.isEnabled !== 'function' || root.isEnabled());
        if (visible && canSpawn && root.position) {
          const color = (entry.botData && entry.botData.avatar_color) || '#59f1ff';
          fx.spawnTeleportBurst(root.position.x, root.position.z, root.position.x, root.position.z, color);
        }
        bots.despawnEntry(id);
      }
      // Hero moment on the winner: gold light pillar + ember burst.
      if (winner && !show.heroSpawned) {
        show.heroSpawned = true;
        const entry = bots.entries.get(winner.id);
        if (entry && entry.root) {
          this._spawnWinnerHero(entry.root.position.x, entry.root.position.z, winner.color);
          if (entry.nameLabel) entry.nameLabel.isVisible = false;
          if (entry.hpContainer) entry.hpContainer.isVisible = false;
        }
      }
    }

    // Beat 2: winner ascends and dissolves away.
    if (winner && !show.ascend && show.othersDespawned && tl.elapsed >= WINNER_ASCEND_START) {
      const entry = bots.entries.get(winner.id);
      if (entry && entry.root) {
        show.ascend = { id: winner.id, entry };
      }
    }
    if (show.ascend) {
      const k = clamp01((tl.elapsed - WINNER_ASCEND_START) / WINNER_ASCEND_SECS);
      const e = easeInQuad(k);
      const root = show.ascend.entry.root;
      if (root) {
        root.position.y = 46 * e;
        const s = Math.max(0.12, 1 - 0.85 * e);
        root.scaling.set(s, s, s);
      }
      if (k >= 1) {
        bots.despawnEntry(show.ascend.id);
        show.ascend = null;
      }
    }
    if (tl.elapsed >= tl.plan.winner) this._finishWinnerPhase(show);
  }

  /**
   * Winner hero moment: a gold-blended light pillar (deathBurst-pillar
   * style) plus a brief ember burst, both from the EffectRenderer pools —
   * no new particle systems or one-off meshes are constructed per show.
   */
  _spawnWinnerHero(x, z, colorHex) {
    const fx = this.engine.effectRenderer;
    if (!fx || !(this.engine.shouldSpawnEffects && this.engine.shouldSpawnEffects())) return;
    const B = window.BABYLON;
    if (!B) return;
    const c = parseColor(colorHex || '#ffce54');
    // Blend the avatar color toward gold so every winner reads as a victory.
    const col = {
      r: Math.min(1, c.r * 0.55 + 1.0 * 0.45),
      g: Math.min(1, c.g * 0.55 + 0.81 * 0.45),
      b: Math.min(1, c.b * 0.55 + 0.33 * 0.45),
    };
    const entry = fx._acquireFxMesh('intermission-pillar', () => B.MeshBuilder.CreateCylinder(
      'intermission-pillar', { height: 1, diameter: 1, tessellation: 12 }, fx.scene));
    entry.mat.emissiveColor.set(col.r, col.g, col.b);
    entry.mat.alpha = 0.3;
    entry.mesh.position.set(x, 8, z);
    entry.mesh.scaling.set(7, 16, 7);
    this._show.fx.push({ kind: 'pillar', entry, start: this._show.timeline.elapsed, dur: 1.7, x, z });

    const ps = fx._acquirePS();
    ps.emitter.set(x, 10, z);
    ps.direction1.set(-0.5, 0.6, -0.5);
    ps.direction2.set(0.5, 1.2, 0.5);
    ps.color1.set(col.r, col.g, col.b, 0.95);
    ps.color2.set(1, 0.92, 0.6, 0.85);
    ps.colorDead.set(col.r * 0.25, col.g * 0.25, col.b * 0.2, 0);
    ps.minSize = 1.2; ps.maxSize = 3;
    ps.minLifeTime = 0.4; ps.maxLifeTime = 1.0;
    ps.emitRate = 120;
    ps.minEmitPower = 10; ps.maxEmitPower = 26;
    ps.gravity.set(0, 6, 0); // embers drift UP with the ascension
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.2;
    fx._launch(ps);
  }

  /* --------------------------- phase B: teardown ------------------------- */

  _beginTeardown(show) {
    const obstacles = this.engine.obstacleRenderer;
    if (!obstacles || !obstacles.collectTeardownMeshes) return;
    const meshes = obstacles.collectTeardownMeshes();
    for (const mesh of meshes) {
      if (mesh.unfreezeWorldMatrix) mesh.unfreezeWorldMatrix();
    }
    const layout = obstacles.getLastLayout ? obstacles.getLastLayout() : { obstacles: [] };
    show.teardown = { meshes, dustMarks: [0.15, 0.45, 0.78], layout };
  }

  _updateTeardown(show) {
    const td = show.teardown;
    if (!td) return;
    const t = show.timeline.teardownT();
    const e = easeInQuad(t);
    for (const mesh of td.meshes) {
      if (mesh.isDisposed && mesh.isDisposed()) continue;
      mesh.scaling.y = Math.max(0.02, 1 - 0.98 * e);
      mesh.position.y = -1.4 * e;
    }
    while (td.dustMarks.length && t >= td.dustMarks[0]) {
      td.dustMarks.shift();
      const rects = td.layout.obstacles || [];
      if (rects.length) {
        const r = rects[Math.floor(Math.random() * rects.length)];
        this._spawnDust(r.x + r.width / 2, r.y + r.height / 2);
      }
    }
  }

  _restoreTeardownMeshes(show) {
    const td = show.teardown;
    if (!td) return;
    for (const mesh of td.meshes) {
      if (mesh.isDisposed && mesh.isDisposed()) continue;
      mesh.scaling.y = 1;
      mesh.position.y = 0;
      if (mesh.freezeWorldMatrix) mesh.freezeWorldMatrix();
    }
    show.teardown = null;
  }

  /** Pooled standard-blend dust puff (teardown rubble). */
  _spawnDust(x, z) {
    const fx = this.engine.effectRenderer;
    if (!fx || !(this.engine.shouldSpawnEffects && this.engine.shouldSpawnEffects())) return;
    const B = window.BABYLON;
    if (!B) return;
    const ps = fx._acquirePS();
    ps.emitter.set(x, 2, z);
    ps.direction1.set(-1.4, 0.2, -1.4);
    ps.direction2.set(1.4, 1.1, 1.4);
    ps.color1.set(0.5, 0.55, 0.62, 0.55);
    ps.color2.set(0.32, 0.36, 0.45, 0.4);
    ps.colorDead.set(0.1, 0.11, 0.14, 0);
    ps.minSize = 2; ps.maxSize = 4.5;
    ps.minLifeTime = 0.25; ps.maxLifeTime = 0.6;
    ps.emitRate = 90;
    ps.minEmitPower = 6; ps.maxEmitPower = 16;
    ps.gravity.set(0, -18, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_STANDARD;
    ps.targetStopDuration = 0.18;
    fx._launch(ps);
  }

  /* ------------------------- phase C: construction ----------------------- */

  /**
   * Pre-build the next map's transient rise geometry. Returns null (phase
   * degrades to the title card alone) when the next round changes the arena
   * dimensions — the whole scene is rebuilt at the first keyframe then, so
   * pre-building on a scene about to be disposed would be wasted work.
   */
  _beginConstruction(show) {
    const nm = show.summary.nextMap;
    const engine = this.engine;
    const B = typeof window !== 'undefined' ? window.BABYLON : null;
    if (!nm || !B || !engine.scene || !engine.obstacleRenderer) return null;
    if (nm.arenaSize[0] !== engine.arenaWidth || nm.arenaSize[1] !== engine.arenaHeight) return null;
    const mats = engine.obstacleRenderer.getConstructionMaterials
      ? engine.obstacleRenderer.getConstructionMaterials()
      : null;
    if (!mats || !mats.body || !mats.trim) return null;

    const c = {
      ok: true,
      handoffDone: false,
      wallMesh: null,
      strips: [],
      revealed: 0,
      clones: [],
      dustEvery: 1,
    };

    // Boundary wall + trim reveal (only when the smooth-wall look is on —
    // with it off the boundary renders as plain boxes and simply pops in at
    // the handoff, mask rects are far too many to clone within budget).
    if (nm.maskRects.length && show.flags.smoothWalls) {
      const { cellSize, groups } = buildBoundaryContours(nm.maskRects);
      if (groups.length) {
        const wall = buildWallGeometry(groups, {
          height: WALL_HEIGHT,
          capWidth: cellSize * 2,
          earcutFn: resolveEarcut(),
        });
        const body = new B.Mesh('intermissionWallRise', engine.scene);
        const bodyData = new B.VertexData();
        bodyData.positions = wall.positions;
        bodyData.normals = wall.normals;
        bodyData.indices = wall.indices;
        bodyData.applyToMesh(body);
        body.material = mats.body;
        body.isPickable = false;
        body.scaling.y = 0.02;
        // Same glow hygiene as MapWallsRenderer: the wall BODY never glows.
        if (engine.glowLayer) engine.glowLayer.addExcludedMesh(body);
        c.wallMesh = body;

        const trimMat = this._syncTrimMaterial(mats.trim);
        for (const strip of splitContoursIntoStrips(groups, MAX_TRIM_STRIPS)) {
          const geo = buildTrimStripGeometry(strip.points, strip.normals, { height: WALL_HEIGHT });
          const mesh = new B.Mesh('intermissionTrimStrip', engine.scene);
          const data = new B.VertexData();
          data.positions = geo.positions;
          data.normals = geo.normals;
          data.indices = geo.indices;
          data.applyToMesh(mesh);
          mesh.material = trimMat;
          mesh.isPickable = false;
          mesh.setEnabled(false);
          c.strips.push(mesh);
        }
      }
    }

    // Obstacle rise clones (boundary rects excluded — they're the wall).
    const maskKeys = new Set(nm.maskRects.map((o) => `${o.x},${o.y},${o.width},${o.height}`));
    const boxes = nm.obstacles.filter((o) => !maskKeys.has(`${o.x},${o.y},${o.width},${o.height}`));
    const schedule = obstacleRiseSchedule(boxes, nm.arenaSize[0], nm.arenaSize[1]);
    c.dustEvery = Math.max(1, Math.ceil(schedule.length / 14));
    const makeBox = (o, name) => {
      const mesh = B.MeshBuilder.CreateBox(name, {
        width: o.width, height: WALL_HEIGHT, depth: o.height,
      }, engine.scene);
      mesh.position.set(o.x + o.width / 2, WALL_HEIGHT / 2, o.y + o.height / 2);
      return mesh;
    };
    if (schedule.length > MAX_OBSTACLE_CLONES) {
      // Bound the transient draw calls: batch the stagger order into merged
      // groups animated as units (merged vertices are world-space, so a bare
      // scaling.y rises the whole group from the floor).
      const per = Math.ceil(schedule.length / OBSTACLE_CLONE_GROUPS);
      for (let g = 0; g < schedule.length; g += per) {
        const slice = schedule.slice(g, g + per);
        const groupBoxes = slice.map((s, j) => makeBox(boxes[s.index], `intermissionRiseG-${g}-${j}`));
        const merged = B.Mesh.MergeMeshes(groupBoxes, true, true);
        if (!merged) continue;
        merged.material = mats.body;
        merged.isPickable = false;
        merged.scaling.y = 0.02;
        const start = slice[0].start;
        const end = slice[slice.length - 1].start + slice[slice.length - 1].dur;
        c.clones.push({ mesh: merged, mode: 'merged', start, dur: Math.max(0.05, end - start), landed: false, cx: 0, cz: 0 });
      }
    } else {
      for (const s of schedule) {
        const o = boxes[s.index];
        const mesh = makeBox(o, `intermissionRise-${s.index}`);
        mesh.material = mats.body;
        mesh.isPickable = false;
        mesh.scaling.y = 0.02;
        mesh.position.y = WALL_HEIGHT * 0.01;
        c.clones.push({
          mesh,
          mode: 'box',
          start: s.start,
          dur: s.dur,
          landed: false,
          cx: o.x + o.width / 2,
          cz: o.y + o.height / 2,
          size: Math.max(o.width, o.height),
        });
      }
    }
    return c;
  }

  /** Double-sided clone of the shared obstacle trim material (the strip
   *  band is open geometry) — created once, retinted per show. */
  _syncTrimMaterial(source) {
    if (!this._trimMat) {
      this._trimMat = source.clone('intermissionTrimMat');
      this._trimMat.unfreeze();
      this._trimMat.backFaceCulling = false;
    } else {
      this._trimMat.unfreeze();
      this._trimMat.emissiveColor.copyFrom(source.emissiveColor);
    }
    this._trimMat.freeze();
    return this._trimMat;
  }

  _updateConstruction(show) {
    const c = show.construction;
    if (!c || !c.ok || c.handoffDone) return;
    const p = show.timeline.constructionP;

    if (c.wallMesh && !(c.wallMesh.isDisposed && c.wallMesh.isDisposed())) {
      c.wallMesh.scaling.y = Math.max(0.02, easeOutCubic(p / 0.85));
    }
    const want = Math.min(c.strips.length, Math.floor(p * (c.strips.length + 1) * 1.18));
    while (c.revealed < want) {
      c.strips[c.revealed].setEnabled(true);
      c.revealed++;
    }
    for (const clone of c.clones) {
      const local = clamp01((p - clone.start) / clone.dur);
      const s = 0.02 + 0.98 * easeOutCubic(local);
      const mesh = clone.mesh;
      if (mesh.isDisposed && mesh.isDisposed()) continue;
      mesh.scaling.y = s;
      if (clone.mode === 'box') mesh.position.y = (WALL_HEIGHT * s) / 2;
      if (local >= 1 && !clone.landed) {
        clone.landed = true;
        if (clone.mode === 'box' && c.clones.indexOf(clone) % c.dustEvery === 0) {
          this._spawnLandingDust(clone.cx, clone.cz, clone.size);
        }
      }
    }

    if (p >= 1) this._completeConstruction(show);
  }

  /** Pooled disc flash where a risen obstacle lands. */
  _spawnLandingDust(x, z, size) {
    const fx = this.engine.effectRenderer;
    if (!fx || !(this.engine.shouldSpawnEffects && this.engine.shouldSpawnEffects())) return;
    const entry = fx._acquireDisc();
    entry.mesh.position.set(x, 0.35, z);
    entry.mat.emissiveColor.set(0.42, 0.48, 0.58);
    entry.mat.alpha = 0.35;
    this._show.fx.push({
      kind: 'disc', entry,
      start: this._show.timeline.elapsed,
      dur: 0.35,
      scale: Math.max(8, (size || 20) * 0.75),
    });
  }

  /**
   * Construction finished: drop the transient geometry and hand the REAL
   * builders the next map exactly as the first keyframe will describe it —
   * identical rects mean an identical layout fingerprint, so the keyframe
   * swap costs nothing.
   */
  _completeConstruction(show) {
    const c = show.construction;
    if (!c || c.handoffDone) return;
    c.handoffDone = true;
    this._disposeConstructionArtifacts(show, /*keepState=*/true);
    this._restoreTeardownMeshes(show);
    const nm = show.summary.nextMap;
    if (nm && this.engine.obstacleRenderer) {
      this.engine.obstacleRenderer.update(nm.obstacles, nm.maskRects.length ? nm.maskRects : null);
    }
  }

  _disposeConstructionArtifacts(show, keepState = false) {
    const c = show.construction;
    if (!c) return;
    if (c.wallMesh) {
      if (this.engine.glowLayer && this.engine.glowLayer.removeExcludedMesh) {
        this.engine.glowLayer.removeExcludedMesh(c.wallMesh);
      }
      if (!(c.wallMesh.isDisposed && c.wallMesh.isDisposed())) c.wallMesh.dispose();
      c.wallMesh = null;
    }
    for (const strip of c.strips) {
      if (!(strip.isDisposed && strip.isDisposed())) strip.dispose();
    }
    c.strips.length = 0;
    for (const clone of c.clones) {
      if (!(clone.mesh.isDisposed && clone.mesh.isDisposed())) clone.mesh.dispose();
    }
    c.clones.length = 0;
    if (!keepState) show.construction = null;
  }

  /* ----------------------- transient pooled effects ---------------------- */

  _updateTransientFx(show) {
    const elapsed = show.timeline.elapsed;
    for (let i = show.fx.length - 1; i >= 0; i--) {
      const f = show.fx[i];
      const t = (elapsed - f.start) / f.dur;
      if (t >= 1) {
        this._releaseFx(f);
        show.fx.splice(i, 1);
        continue;
      }
      if (f.kind === 'disc') {
        const s = f.scale * (0.6 + 0.55 * easeOutCubic(t));
        f.entry.mesh.scaling.set(s, s, 1);
        f.entry.mat.alpha = 0.35 * (1 - t);
      } else if (f.kind === 'pillar') {
        const h = 16 + 18 * easeOutCubic(t);
        f.entry.mesh.scaling.set(7, h, 7);
        f.entry.mesh.position.y = h / 2;
        f.entry.mat.alpha = 0.3 * (1 - t);
      }
    }
  }

  _releaseFx(f) {
    const fx = this.engine.effectRenderer;
    if (!fx) return;
    if (f.kind === 'disc' && fx._releaseDisc) fx._releaseDisc(f.entry);
    else if (f.kind === 'pillar' && fx._releaseFxMesh) fx._releaseFxMesh(f.entry);
  }

  /* ------------------------------ overlays ------------------------------- */

  _ensureOverlay() {
    if (this._overlayRoot && this._overlayRoot.isConnected !== false) return this._overlayRoot;
    if (typeof document === 'undefined') return null;
    const host = this.engine.canvas && this.engine.canvas.parentElement;
    if (!host) return null;
    const root = document.createElement('div');
    root.className = 'intermission-overlay';
    root.setAttribute('aria-hidden', 'true');
    host.appendChild(root);
    this._overlayRoot = root;
    return root;
  }

  /** "WINNER — name" (winner accent color) or "ROUND OVER" on a draw. */
  _showBanner(winner) {
    const root = this._ensureOverlay();
    if (!root) return;
    this._removeElement(this._banner);
    const banner = document.createElement('div');
    banner.className = 'intermission-banner';
    const kicker = document.createElement('span');
    kicker.className = 'intermission-banner-kicker';
    kicker.textContent = winner ? 'WINNER' : 'ROUND OVER';
    banner.appendChild(kicker);
    if (winner) {
      const name = document.createElement('span');
      name.className = 'intermission-banner-name';
      name.textContent = winner.name || 'UNKNOWN';
      if (winner.color) name.style.color = winner.color;
      banner.appendChild(name);
    }
    root.appendChild(banner);
    this._banner = banner;
    this._reveal(banner);
  }

  _dismissBanner() {
    this._fadeOutAndRemove(this._banner);
    this._banner = null;
  }

  _updateTitleCard(show) {
    const visible = show.timeline.titleCardVisible() && !!show.summary.nextMap;
    if (visible && !show.titleShown) {
      show.titleShown = true;
      this._showTitleCard(show.summary.roundNumber + 1, show.summary.nextMap.shape);
    } else if (!visible && show.titleShown && this._titleCard) {
      this._dismissTitleCard();
    }
  }

  /** "ROUND {n} — {SHAPE}" card as the construction wraps up. */
  _showTitleCard(roundNumber, shape) {
    const root = this._ensureOverlay();
    if (!root) return;
    this._removeElement(this._titleCard);
    const card = document.createElement('div');
    card.className = 'intermission-title';
    const round = document.createElement('span');
    round.className = 'intermission-title-round';
    round.textContent = `ROUND ${roundNumber}`;
    card.appendChild(round);
    const shapeEl = document.createElement('span');
    shapeEl.className = 'intermission-title-shape';
    shapeEl.textContent = String(shape || 'square').replace(/_/g, ' ').toUpperCase();
    card.appendChild(shapeEl);
    root.appendChild(card);
    this._titleCard = card;
    this._reveal(card);
  }

  _dismissTitleCard() {
    this._fadeOutAndRemove(this._titleCard);
    this._titleCard = null;
  }

  _reveal(el) {
    if (typeof requestAnimationFrame === 'function') {
      requestAnimationFrame(() => el.classList.add('visible'));
    } else {
      el.classList.add('visible');
    }
  }

  _fadeOutAndRemove(el) {
    if (!el) return;
    el.classList.remove('visible');
    setTimeout(() => { if (el.parentNode) el.remove(); }, 450);
  }

  _removeElement(el) {
    if (el && el.parentNode) el.remove();
  }
}
