'use strict';

/**
 * Intermission show director (issues #189/#192).
 *
 * Turns the between-round window into a staged spectator show driven by the
 * server's typed `round_end` broadcast, re-anchored (issue #192) to the real
 * match events:
 *
 *   round_end arrives — the winner banner + hero pillar + bot despawns fire
 *     immediately AND the teardown starts immediately (the two overlap): the
 *     current merged meshes and boundary wall sink into the floor over
 *     TEARDOWN_PHASE_SECS, so the old map is completely gone long before the
 *     next round's countdown. The blue safe-zone ring plays the round's
 *     shrink in REVERSE back to its opening radius and hides.
 *   stage resize — when the next round changes the arena dimensions (dynamic
 *     sizing), the whole scene is rebuilt at the new size right after the
 *     teardown, INSIDE the show (the director survives the rebuild), instead
 *     of silently skipping the construction phase.
 *   countdown starts (lobby_state.countdown) — that is the construction
 *     trigger. Assembly takes CONSTRUCTION_SECS (10s) to fully build the
 *     next map (pre-announced in `round_end.next_map`), compressed only when
 *     the countdown is shorter, and holds on the finished map + title card
 *     when it is longer. The build is COLOR-TRUE: clones carry the exact
 *     final layout (isolated boxes + trims + rooftop detailing + cluster
 *     union prisms, issue #190) tinted with the NEXT map's palette, and the
 *     gold target ring glides to the next round's pre-announced placement.
 *     If countdown info never arrives, a fallback keeps the old internal
 *     pacing so the show still completes before the new round's first state.
 *
 * Strict ordering: teardown always completes (and any stage resize settles)
 * before construction begins, and construction never begins before the
 * countdown has been observed (fallback aside).
 *
 * Robustness: the first arena_state of the NEXT round (or a stale show,
 * >60s) fast-forwards everything — overlays dismissed, transient meshes
 * disposed, teardown transforms restored, ring choreography settled. No
 * `round_end` message (old server) means the feature is completely inert.
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

import { buildBoundaryContours, buildWallGeometry, contourNormals, resolveEarcut, WALL_HEIGHT } from './map-walls.js?v=20260718g';
import { PILLAR_HEIGHT, composeObstacleLayout, appendObstacleBoxes, appendRoofDetail } from './obstacles.js?v=20260718h';
import { buildClusterTrimGeometry } from './obstacle-clusters.js?v=20260718g';
import { parseColor } from './utils.js';
import { isEnabled, onSettingsChange } from '../settings.js';

/* ------------------------------------------------------------------------ */
/* Pure timeline logic                                                       */
/* ------------------------------------------------------------------------ */

export const WINNER_PHASE_SECS = 2.5;
/** Teardown runs from t=0 — it overlaps the winner celebration (issue #192). */
export const TEARDOWN_PHASE_SECS = 2.0;
/** Nominal full-assembly time once the countdown opens the gate. */
export const CONSTRUCTION_SECS = 10;
/** Compression floor when the countdown is nearly over. */
export const MIN_CONSTRUCTION_SECS = 0.75;
/** Assembly aims to finish this far before the projected round start. */
export const CONSTRUCTION_LEAD_SECS = 0.5;
/** Fallback: with no countdown info this long past the announced
 *  intermission end, assembly opens anyway at nominal pacing. */
export const COUNTDOWN_GRACE_SECS = 4;
export const TITLE_CARD_SECS = 2.0;
/** A show with no new-round state after this long is stale — snap it away. */
export const STALE_SHOW_SECS = 60;
/** Per-unit transient clone budget (each unit costs a body + a trim draw);
 *  above it the stagger runs merged groups. */
export const MAX_OBSTACLE_CLONES = 60;
export const OBSTACLE_CLONE_GROUPS = 10;
/** Max transient trim-reveal meshes for the boundary contour. */
export const MAX_TRIM_STRIPS = 24;

/** Winner-despawn choreography beats inside the winner window (seconds from show start). */
export const DESPAWN_OTHERS_AT = 0.35;
export const WINNER_ASCEND_START = 1.0;
export const WINNER_ASCEND_SECS = 1.4;

/**
 * Normalize/validate a raw round_end message. Returns null when the message
 * is not a usable round_end envelope (feature stays inert). Winner is null
 * on a draw; nextMap is null when the server did not attach a usable map;
 * nextMap.safeZone is null on pre-#192 servers that don't announce the next
 * round's zone placement.
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
  let nextMap = null;
  if (nm && typeof nm === 'object' &&
      Array.isArray(nm.obstacles) &&
      Array.isArray(nm.arena_size) && nm.arena_size.length === 2) {
    const sz = nm.safe_zone;
    const safeZone = sz && typeof sz === 'object' &&
      Array.isArray(sz.center) && sz.center.length === 2 &&
      Array.isArray(sz.target_center) && sz.target_center.length === 2
      ? {
          center: [Number(sz.center[0]) || 0, Number(sz.center[1]) || 0],
          radius: Number(sz.radius) || 0,
          targetCenter: [Number(sz.target_center[0]) || 0, Number(sz.target_center[1]) || 0],
          targetRadius: Number(sz.target_radius) || 0,
        }
      : null;
    nextMap = {
      shape: typeof nm.shape === 'string' && nm.shape ? nm.shape : 'square',
      arenaSize: [Number(nm.arena_size[0]) || 0, Number(nm.arena_size[1]) || 0],
      obstacles: nm.obstacles,
      maskRects: Array.isArray(nm.mask_rects) ? nm.mask_rects : [],
      safeZone,
    };
  }
  return {
    roundNumber,
    intermissionSecs: Number.isFinite(secs) && secs > 0 ? secs : 10,
    winner,
    nextMap,
  };
}

/**
 * The pure show clock (issue #192 re-anchor): the winner beat and the
 * teardown both start at t=0 and overlap; the assembly window (construction
 * + ring glide) opens only once the teardown has completed, any external
 * block (mid-show stage resize) has cleared, AND a lobby countdown has been
 * observed — with a grace fallback when countdown info never arrives.
 * Construction progress is monotonic and its deadline re-paces against each
 * countdown observation. Owns no Babylon/DOM state — the director maps its
 * outputs onto the scene.
 */
export class IntermissionTimeline {
  constructor(intermissionSecs, flags = {}) {
    this.flags = { ...flags };
    const secs = Number.isFinite(intermissionSecs) && intermissionSecs > 0 ? intermissionSecs : 10;
    this.intermissionSecs = secs;
    this.elapsed = 0;
    this.winnerEnd = (flags.winnerBanner || flags.botDespawn) ? WINNER_PHASE_SECS : 0;
    this.teardownEnd = flags.mapTeardown ? TEARDOWN_PHASE_SECS : 0;
    /** Whether an assembly window exists at all (construction visuals and/or
     *  the gold-ring glide ride the same gate). */
    this.wantsAssembly = !!(flags.mapConstruction || flags.ringChoreography);
    this.blocked = false;
    this.countdownSeen = false;
    this.roundStartAt = null;
    this.constructionStart = null;
    this.constructionEnd = null;
    this.constructionP = 0;
    this.finished = false;
  }

  /** Advance the clock. Construction progress is monotonic: a re-paced
   *  (extended) deadline pauses it rather than running it backwards. */
  advance(dt) {
    this.elapsed += Math.max(0, Number(dt) || 0);
    this._maybeOpenAssembly();
    if (this.constructionStart !== null && !this.finished) {
      const span = Math.max(0.001, this.constructionEnd - this.constructionStart);
      const p = (this.elapsed - this.constructionStart) / span;
      this.constructionP = Math.min(1, Math.max(this.constructionP, p));
    }
  }

  /** @private Strict gate: teardown complete, not blocked, countdown seen
   *  (or the missing-countdown fallback due). */
  _maybeOpenAssembly() {
    if (this.constructionStart !== null || this.finished || !this.wantsAssembly) return;
    if (this.blocked || !this.teardownComplete()) return;
    const fallbackDue = this.elapsed >= this.intermissionSecs + COUNTDOWN_GRACE_SECS;
    if (!this.countdownSeen && !fallbackDue) return;
    this.constructionStart = this.elapsed;
    this.constructionEnd = this._paceEnd();
  }

  /** @private 10s nominal assembly, compressed only when the projected
   *  round start is sooner (finish CONSTRUCTION_LEAD_SECS early). */
  _paceEnd() {
    const nominal = this.constructionStart + CONSTRUCTION_SECS;
    if (this.roundStartAt === null) return nominal;
    return Math.min(nominal,
      Math.max(this.constructionStart + MIN_CONSTRUCTION_SECS, this.roundStartAt - CONSTRUCTION_LEAD_SECS));
  }

  /** Lobby countdown observed: the round starts in `secs`. Opens the
   *  assembly gate and (re)paces a live construction toward the projection. */
  noteRoundCountdown(secs) {
    if (!Number.isFinite(secs) || secs <= 0 || this.finished) return;
    this.countdownSeen = true;
    this.roundStartAt = this.elapsed + secs;
    if (this.constructionStart !== null && this.constructionP < 1) {
      this.constructionEnd = this._paceEnd();
    }
    this._maybeOpenAssembly();
  }

  /** External assembly gate — held closed while a mid-show stage resize is
   *  in flight so construction can never race the scene rebuild. */
  setAssemblyBlocked(blocked) {
    this.blocked = !!blocked;
    if (!this.blocked) this._maybeOpenAssembly();
  }

  /** Snap everything complete (new round arrived / show dismissed). */
  fastForward() {
    this.finished = true;
    if (this.wantsAssembly) this.constructionP = 1;
  }

  get stale() { return this.elapsed > STALE_SHOW_SECS; }

  teardownComplete() { return this.elapsed >= this.teardownEnd; }

  phase() {
    if (this.finished) return 'done';
    if (this.elapsed < this.teardownEnd) return 'teardown';
    if (this.elapsed < this.winnerEnd) return 'winner';
    if (this.constructionStart !== null && this.constructionP < 1) return 'construction';
    return 'waiting';
  }

  /** Teardown progress 0..1 — teardown starts at t=0 (issue #192). */
  teardownT() {
    if (!this.teardownEnd) return 1;
    return clamp01(this.elapsed / this.teardownEnd);
  }

  /** Whether the "ROUND N — SHAPE" title card should be on screen. It stays
   *  up through any post-assembly hold until the new round fast-forwards
   *  the show (issue #192: hold on the finished map + title card). */
  titleCardVisible() {
    if (!this.flags.mapConstruction || this.finished || this.constructionStart === null) return false;
    return this.elapsed >= this.constructionEnd - TITLE_CARD_SECS;
  }
}

/**
 * Stagger schedule for the rise: units sorted by distance from the arena
 * centre, each rising over a normalized-progress window, consecutive
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
    this._riseBodyMat = null;
    this._riseTrimMat = null;
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
      ringChoreography: isEnabled('intermissionShow', 'ringChoreography'),
      smoothWalls: isEnabled('arenaAmbience', 'smoothMapWalls'),
      // Cached so the construction pre-build (render-loop-driven) composes
      // the same layout the real event-driven rebuild will (issue #192).
      obstacleDetailing: isEnabled('arenaAmbience', 'obstacleDetailing'),
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
    if (!flags.winnerBanner && !flags.botDespawn && !flags.mapTeardown &&
        !flags.mapConstruction && !flags.ringChoreography) {
      return; // every phase toggled off — show fully inert
    }
    const env = this.engine.envRenderer;
    // Dynamic arena sizing: when the next round changes dimensions the stage
    // is rebuilt at the new size DURING the show (after teardown) instead of
    // skipping the construction phase (issue #192 — the pre-#192 skip was
    // the "map just pops in" report). The timeline holds the assembly gate
    // closed until the rebuild settles.
    const needsResize = !!(flags.mapConstruction && summary.nextMap &&
      (summary.nextMap.arenaSize[0] !== this.engine.arenaWidth ||
       summary.nextMap.arenaSize[1] !== this.engine.arenaHeight));
    const timeline = new IntermissionTimeline(summary.intermissionSecs, flags);
    if (needsResize) timeline.setAssemblyBlocked(true);
    this._show = {
      summary,
      flags,
      timeline,
      bannerShown: false,
      bannerDismissed: false,
      othersDespawned: false,
      heroSpawned: false,
      ascend: null,
      teardownBegun: false,
      teardown: null,
      needsResize,
      resizeStarted: false,
      assemblyBegun: false,
      construction: null,
      // Snapshot before any resize: the glide starts from the old round's
      // ring spot even on a freshly rebuilt scene.
      prevZoneTarget: env && env.getZoneTargetRingState ? env.getZoneTargetRingState() : null,
      titleShown: false,
      cameraPushed: false,
      fx: [],
    };
    // Blue safe-zone ring: play the round's shrink in reverse across the
    // winner/teardown window, then hide until the new round's state.
    if (flags.ringChoreography && env && env.beginZoneRingRewind) {
      env.beginZoneRingRewind(TEARDOWN_PHASE_SECS);
    }
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

  /** Lobby countdown observations open and pace the assembly window. */
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
    // Teardown and the winner celebration both fire at round_end (issue
    // #192) — they overlap by design.
    if (!show.teardownBegun && show.flags.mapTeardown) {
      show.teardownBegun = true;
      this._beginTeardown(show);
    }
    if (show.flags.winnerBanner && !show.bannerShown) {
      show.bannerShown = true;
      this._showBanner(show.summary.winner);
    }
    if (show.bannerShown && !show.bannerDismissed && tl.elapsed >= tl.winnerEnd) {
      show.bannerDismissed = true;
      this._dismissBanner();
    }
    this._updateWinnerChoreo(show);
    this._updateTeardown(show);
    this._maybeResizeStage(show);
    this._maybeBeginAssembly(show);
    this._updateConstruction(show);
    this._updateTitleCard(show);
    this._updateTransientFx(show);
  }

  /**
   * Snap the show complete: dismiss overlays, dispose transient geometry,
   * restore teardown transforms, release pooled effects, settle the ring
   * choreography, restore the camera. Called on the new round's first
   * arena_state, staleness, disposal, or a round_end over a live show.
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
    const env = this.engine.envRenderer;
    if (show.flags.ringChoreography && env && env.settleZoneChoreography) {
      env.settleZoneChoreography();
    }
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
    if (this._riseBodyMat) {
      this._riseBodyMat.dispose();
      this._riseBodyMat = null;
    }
    if (this._riseTrimMat) {
      this._riseTrimMat.dispose();
      this._riseTrimMat = null;
    }
  }

  /* ------------------------- winner celebration -------------------------- */

  _updateWinnerChoreo(show) {
    const tl = show.timeline;
    const bots = this.engine.botRenderer;
    if (!show.flags.botDespawn || !bots) return;
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

  /* ------------------------------ teardown ------------------------------- */

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
      if (t >= 1 && mesh.setEnabled) mesh.setEnabled(false);
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
      if (mesh.setEnabled) mesh.setEnabled(true);
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

  /* ------------------------- mid-show stage resize ----------------------- */

  /**
   * Dynamic arena sizing (issue #192): once the teardown has fully sunk the
   * old map, rebuild the scene at the next round's dimensions INSIDE the
   * show. The engine detaches this director before disposing the scene so
   * the show survives; every scene-bound artifact is dropped first. The
   * timeline's assembly gate stays blocked until the rebuild settles.
   */
  _maybeResizeStage(show) {
    if (!show.needsResize || show.resizeStarted) return;
    if (!show.timeline.teardownComplete()) return;
    show.resizeStarted = true;
    const nm = show.summary.nextMap;
    this._prepareForStageResize(show);
    const unblock = () => {
      if (this._show === show) show.timeline.setAssemblyBlocked(false);
    };
    let result = null;
    try {
      result = this.engine.resizeStageForShow
        ? this.engine.resizeStageForShow(nm.arenaSize[0], nm.arenaSize[1])
        : null;
    } catch {
      result = null;
    }
    if (result && typeof result.then === 'function') result.then(unblock, unblock);
    else unblock();
  }

  /** @private The stage rebuild disposes the whole scene, and every
   *  scene-bound show artifact dies with it: drop the references without
   *  releasing into (about-to-die) pools, and let the materials be re-cloned
   *  from the fresh renderers. DOM overlays survive — the canvas does. The
   *  teardown mesh list is kept: the restore/animate loops skip disposed
   *  meshes, and if the resize fails the still-live meshes can then be
   *  restored normally on fast-forward. */
  _prepareForStageResize(show) {
    show.fx.length = 0;
    show.ascend = null;
    show.construction = null;
    this._riseBodyMat = null;
    this._riseTrimMat = null;
  }

  /* ----------------------------- construction ---------------------------- */

  /**
   * The assembly window opened (countdown observed, teardown complete, any
   * stage resize settled): flip the stage identity to the next map, start
   * the gold-ring glide toward the pre-announced zone target, and pre-build
   * the color-true construction geometry.
   */
  _maybeBeginAssembly(show) {
    const tl = show.timeline;
    if (tl.constructionStart === null || show.assemblyBegun) return;
    show.assemblyBegun = true;
    const nm = show.summary.nextMap;
    const env = this.engine.envRenderer;
    const dur = Math.max(0.5, tl.constructionEnd - tl.elapsed);
    // Gold target ring: glide from wherever the old round left it to the
    // next round's announced placement, landing with the assembly. Skipped
    // gracefully on pre-#192 servers (no safe_zone in next_map).
    if (show.flags.ringChoreography && nm && nm.safeZone && env && env.glideZoneTargetRing) {
      const z = nm.safeZone;
      env.glideZoneTargetRing(
        { cx: z.targetCenter[0], cy: z.targetCenter[1], r: z.targetRadius },
        dur,
        show.prevZoneTarget,
      );
    }
    if (show.flags.mapConstruction) {
      // Stage identity flips as the build starts: palette, floor bake, and
      // wall trim re-theme now, so the construction rises in the next map's
      // final colors instead of recoloring at round start (issue #192).
      if (nm && env && env.setMapShape) env.setMapShape(nm.shape);
      // A null build (failed resize / unusable map data) degrades the phase
      // to the title card alone; the empty artifact lists keep the
      // dispose/fast-forward paths uniform.
      show.construction = this._beginConstruction(show) ||
        { ok: false, handoffDone: true, wallMesh: null, strips: [], clones: [] };
    }
  }

  /**
   * Pre-build the next map's transient rise geometry, color-true to the
   * real build (issue #192): the exact layout composition the round rebuild
   * will produce — isolated boxes with their trims and rooftop detailing
   * plus cluster union prisms with their unified trims (issue #190) — all
   * tinted with the NEXT map's palette. Returns null (phase degrades to the
   * title card alone) only when the scene/materials are unusable or a
   * mid-show stage resize failed to land the new dimensions.
   */
  _beginConstruction(show) {
    const nm = show.summary.nextMap;
    const engine = this.engine;
    const B = typeof window !== 'undefined' ? window.BABYLON : null;
    if (!nm || !B || !engine.scene || !engine.obstacleRenderer) return null;
    if (nm.arenaSize[0] !== engine.arenaWidth || nm.arenaSize[1] !== engine.arenaHeight) return null;
    const sourceMats = engine.obstacleRenderer.getConstructionMaterials
      ? engine.obstacleRenderer.getConstructionMaterials()
      : null;
    if (!sourceMats || !sourceMats.body || !sourceMats.trim) return null;
    const palette = engine.envRenderer && engine.envRenderer.getPaletteForShape
      ? engine.envRenderer.getPaletteForShape(nm.shape)
      : null;
    const mats = this._syncRiseMaterials(sourceMats, palette);

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
        const body = this._meshFromGeometry(B, 'intermissionWallRise', wall);
        body.material = mats.body;
        body.isPickable = false;
        body.scaling.y = 0.02;
        // Same glow hygiene as MapWallsRenderer: the wall BODY never glows.
        if (engine.glowLayer) engine.glowLayer.addExcludedMesh(body);
        c.wallMesh = body;

        for (const strip of splitContoursIntoStrips(groups, MAX_TRIM_STRIPS)) {
          const geo = buildTrimStripGeometry(strip.points, strip.normals, { height: WALL_HEIGHT });
          const mesh = this._meshFromGeometry(B, 'intermissionTrimStrip', geo);
          mesh.material = mats.trim;
          mesh.isPickable = false;
          mesh.setEnabled(false);
          c.strips.push(mesh);
        }
      }
    }

    // Rise units: EXACTLY the layout the real build renders (boundary rects
    // excluded — they're the wall). composeObstacleLayout + the shared
    // append helpers keep clustering, ordering, and detail hashes identical.
    const maskKeys = new Set(nm.maskRects.map((o) => `${o.x},${o.y},${o.width},${o.height}`));
    const boxes = nm.obstacles.filter((o) => !maskKeys.has(`${o.x},${o.y},${o.width},${o.height}`));
    const earcutFn = resolveEarcut();
    const { isolated, unions } = composeObstacleLayout(boxes, earcutFn);
    const detailing = show.flags.obstacleDetailing;
    const scene = engine.scene;

    const units = [];
    isolated.forEach((obs, i) => units.push({
      rect: obs,
      build: (bodyParts, trimParts) => {
        appendObstacleBoxes(scene, obs, i, bodyParts, trimParts);
        if (detailing) appendRoofDetail(scene, obs, i, bodyParts, trimParts);
      },
    }));
    unions.forEach((u, ci) => {
      let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
      for (const r of u.rects) {
        minX = Math.min(minX, r.x);
        minY = Math.min(minY, r.y);
        maxX = Math.max(maxX, r.x + r.width);
        maxY = Math.max(maxY, r.y + r.height);
      }
      units.push({
        rect: { x: minX, y: minY, width: maxX - minX, height: maxY - minY },
        build: (bodyParts, trimParts) => {
          bodyParts.push(this._meshFromGeometry(B, `intermissionRiseCluster-${ci}`,
            buildWallGeometry(u.groups, { height: PILLAR_HEIGHT, capWidth: 8, earcutFn })));
          trimParts.push(this._meshFromGeometry(B, `intermissionRiseClusterTrim-${ci}`,
            buildClusterTrimGeometry(u.groups, { height: PILLAR_HEIGHT, earcutFn })));
          if (detailing) {
            const seat = u.rects.reduce((best, r) =>
              r.width * r.height > best.width * best.height ? r : best);
            appendRoofDetail(scene, seat, isolated.length + ci, bodyParts, trimParts);
          }
        },
      });
    });

    const schedule = obstacleRiseSchedule(units.map((u) => u.rect), nm.arenaSize[0], nm.arenaSize[1]);
    c.dustEvery = Math.max(1, Math.ceil(schedule.length / 14));
    // Merging (even single parts) bakes world-space vertices, so a bare
    // scaling.y rises every clone from the floor uniformly.
    const mergeParts = (parts, material) => {
      if (!parts.length) return null;
      const merged = B.Mesh.MergeMeshes(parts, true, true);
      if (!merged) return null;
      merged.material = material;
      merged.isPickable = false;
      merged.scaling.y = 0.02;
      return merged;
    };
    if (schedule.length > MAX_OBSTACLE_CLONES) {
      // Bound the transient draw calls: batch the stagger order into merged
      // groups animated as units.
      const per = Math.ceil(schedule.length / OBSTACLE_CLONE_GROUPS);
      for (let g = 0; g < schedule.length; g += per) {
        const slice = schedule.slice(g, g + per);
        const bodyParts = [];
        const trimParts = [];
        for (const s of slice) units[s.index].build(bodyParts, trimParts);
        const meshes = [mergeParts(bodyParts, mats.body), mergeParts(trimParts, mats.trim)].filter(Boolean);
        if (!meshes.length) continue;
        const start = slice[0].start;
        const end = slice[slice.length - 1].start + slice[slice.length - 1].dur;
        c.clones.push({ meshes, start, dur: Math.max(0.05, end - start), landed: true, cx: 0, cz: 0, size: 0 });
      }
    } else {
      for (const s of schedule) {
        const unit = units[s.index];
        const bodyParts = [];
        const trimParts = [];
        unit.build(bodyParts, trimParts);
        const meshes = [mergeParts(bodyParts, mats.body), mergeParts(trimParts, mats.trim)].filter(Boolean);
        if (!meshes.length) continue;
        c.clones.push({
          meshes,
          start: s.start,
          dur: s.dur,
          landed: false,
          cx: unit.rect.x + unit.rect.width / 2,
          cz: unit.rect.y + unit.rect.height / 2,
          size: Math.max(unit.rect.width, unit.rect.height),
        });
      }
    }
    return c;
  }

  /** @private Custom mesh from raw geometry arrays. */
  _meshFromGeometry(B, name, geo) {
    const mesh = new B.Mesh(name, this.engine.scene);
    const data = new B.VertexData();
    data.positions = geo.positions;
    data.normals = geo.normals;
    data.indices = geo.indices;
    data.applyToMesh(mesh);
    return mesh;
  }

  /**
   * @private Dedicated construction materials: clones of the shared
   * obstacle body/trim, retinted with the NEXT map's palette so the
   * transient build already wears the exact colors the real palette-tinted
   * rebuild applies at the handoff — while the old round's meshes
   * (mid-teardown) keep the shared materials untouched. The trim clone
   * disables culling: strip bands and cluster trims are open geometry.
   */
  _syncRiseMaterials(source, palette) {
    if (!this._riseBodyMat) {
      this._riseBodyMat = source.body.clone('intermissionRiseBodyMat');
    }
    this._riseBodyMat.unfreeze();
    if (palette) {
      this._riseBodyMat.diffuseColor.set(...palette.obstacleBody.diffuse);
      this._riseBodyMat.emissiveColor.set(...palette.obstacleBody.emissive);
    } else {
      this._riseBodyMat.diffuseColor.copyFrom(source.body.diffuseColor);
      this._riseBodyMat.emissiveColor.copyFrom(source.body.emissiveColor);
    }
    this._riseBodyMat.freeze();
    if (!this._riseTrimMat) {
      // The clone inherits the source's frozen state — unfreeze before
      // flipping culling so the change isn't swallowed by the freeze cache.
      this._riseTrimMat = source.trim.clone('intermissionRiseTrimMat');
      this._riseTrimMat.unfreeze();
      this._riseTrimMat.backFaceCulling = false;
    } else {
      this._riseTrimMat.unfreeze();
    }
    if (palette) this._riseTrimMat.emissiveColor.set(...palette.obstacleTrim);
    else this._riseTrimMat.emissiveColor.copyFrom(source.trim.emissiveColor);
    this._riseTrimMat.freeze();
    return { body: this._riseBodyMat, trim: this._riseTrimMat };
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
      for (const mesh of clone.meshes) {
        if (mesh.isDisposed && mesh.isDisposed()) continue;
        mesh.scaling.y = s;
      }
      if (local >= 1 && !clone.landed) {
        clone.landed = true;
        if (c.clones.indexOf(clone) % c.dustEvery === 0) {
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
   * identical rects mean an identical layout fingerprint, and the stage
   * identity already flipped at assembly start, so the palette-tinted
   * rebuild lands in exactly the colors the clones wore and the keyframe
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
      if (this.engine.envRenderer && this.engine.envRenderer.setMapShape) {
        this.engine.envRenderer.setMapShape(nm.shape);
      }
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
      for (const mesh of clone.meshes) {
        if (!(mesh.isDisposed && mesh.isDisposed())) mesh.dispose();
      }
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
