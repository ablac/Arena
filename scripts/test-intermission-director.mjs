import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// ---------------------------------------------------------------------------
// Module identity / wiring pins
// ---------------------------------------------------------------------------

const engineSource = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
const settingsSource = readFileSync(new URL('../frontend/js/settings.js', import.meta.url), 'utf8');
const sectionsCss = readFileSync(new URL('../frontend/css/sections.css', import.meta.url), 'utf8');
const mobileCss = readFileSync(new URL('../frontend/m/mobile.css', import.meta.url), 'utf8');
const ciYml = readFileSync(new URL('../.github/workflows/ci.yml', import.meta.url), 'utf8');

assert.match(engineSource, /intermission-director\.js\?v=20260718h/,
  'engine.js must import the stamped intermission director');
assert.match(engineSource, /intermissionDirector\.update\(dt\)/,
  'the render loop must tick the director per frame');
assert.match(engineSource, /'round_end'/,
  'engine.setState must route the typed round_end broadcast');
assert.match(engineSource, /handleLobbyState/,
  'engine.setState must feed lobby countdowns to the director');
assert.match(engineSource, /resizeStageForShow/,
  'engine must expose the mid-show stage resize (issue #192 no-skip)');
assert.match(engineSource, /this\.intermissionDirector \|\| new IntermissionDirector\(this\)/,
  'init() must reuse a detached director so a live show survives the stage resize');

const schemaSlice = settingsSource.slice(
  settingsSource.indexOf('intermissionShow'),
  settingsSource.indexOf('siteMotion'),
);
assert.ok(schemaSlice.length > 0, 'settings schema must contain an intermissionShow section');
for (const key of ['winnerBanner', 'botDespawn', 'mapTeardown', 'mapConstruction', 'ringChoreography']) {
  assert.ok(schemaSlice.includes(key), `intermissionShow must expose the ${key} toggle`);
}
assert.ok(!schemaSlice.includes('defaultOff'),
  'the intermission show ships default ON — no defaultOff in its section (user-requested)');

assert.match(sectionsCss, /\.intermission-banner/, 'desktop shell must style the banner overlay');
assert.match(mobileCss, /\.intermission-banner/, 'mobile shell must style the banner overlay');
assert.match(ciYml, /test-intermission-director\.mjs/, 'this suite must be wired into CI');

// ---------------------------------------------------------------------------
// Pure timeline logic
// ---------------------------------------------------------------------------

const {
  IntermissionTimeline,
  summarizeRoundEnd,
  obstacleRiseSchedule,
  splitContoursIntoStrips,
  buildTrimStripGeometry,
  IntermissionDirector,
  WINNER_PHASE_SECS,
  TEARDOWN_PHASE_SECS,
  CONSTRUCTION_SECS,
  MIN_CONSTRUCTION_SECS,
  CONSTRUCTION_LEAD_SECS,
  COUNTDOWN_GRACE_SECS,
  STALE_SHOW_SECS,
  MAX_TRIM_STRIPS,
} = await import('../frontend/js/renderer/intermission-director.js');
const { buildBoundaryContours } = await import('../frontend/js/renderer/map-walls.js');
const { setEffect } = await import('../frontend/js/settings.js');

function assertClose(actual, expected, msg) {
  assert.ok(Math.abs(actual - expected) < 1e-6, `${msg} (got ${actual}, want ~${expected})`);
}

const allOn = {
  winnerBanner: true, botDespawn: true, mapTeardown: true,
  mapConstruction: true, ringChoreography: true,
};

// --- issue #192 anchor spec: 10s assembly, teardown from t=0, 2.5s winner beat
assert.equal(CONSTRUCTION_SECS, 10, 'the user-specified assembly time is 10 seconds');
assert.equal(TEARDOWN_PHASE_SECS, 2.0);
assert.equal(WINNER_PHASE_SECS, 2.5);

// --- strict ordering: teardown overlaps the winner beat and completes first;
//     construction NEVER starts before a countdown is observed
{
  const tl = new IntermissionTimeline(10, allOn);
  tl.advance(0.5);
  assert.equal(tl.phase(), 'teardown', 'teardown fires immediately at round_end');
  assert.ok(tl.teardownT() > 0, 'teardown progresses during the winner celebration');
  tl.advance(1.6); // 2.1 — teardown done, winner beat still on
  assert.ok(tl.teardownComplete());
  assert.equal(tl.phase(), 'winner');
  tl.advance(4.0); // 6.1 — no countdown yet: the show holds, nothing builds
  assert.equal(tl.constructionStart, null,
    'construction must be gated on the countdown starting');
  assert.equal(tl.phase(), 'waiting');
  // Countdown starts, longer than the assembly: full 10s build, then hold.
  tl.noteRoundCountdown(15);
  assert.notEqual(tl.constructionStart, null, 'the countdown opens the assembly gate');
  assertClose(tl.constructionEnd - tl.constructionStart, CONSTRUCTION_SECS,
    'a countdown longer than 10s never stretches the assembly — it holds instead');
  tl.advance(5.0);
  assert.equal(tl.phase(), 'construction');
  const p1 = tl.constructionP;
  assert.ok(p1 > 0 && p1 < 1);
  tl.advance(0.5);
  assert.ok(tl.constructionP >= p1, 'construction progress must be monotonic');
  tl.advance(4.6); // past constructionEnd
  assert.equal(tl.constructionP, 1);
  assert.ok(tl.titleCardVisible(), 'the finished map holds with the title card up');
  tl.advance(3.0);
  assert.ok(tl.titleCardVisible(), 'title card stays up through the hold until round start');
  tl.fastForward();
  assert.ok(!tl.titleCardVisible());
}

// --- countdown observed EARLY (during teardown) still waits for teardown
{
  const tl = new IntermissionTimeline(10, allOn);
  tl.advance(1.0);
  tl.noteRoundCountdown(12);
  assert.equal(tl.constructionStart, null,
    'teardown must be fully complete before construction can begin');
  tl.advance(1.0); // 2.0 = teardownEnd
  assert.notEqual(tl.constructionStart, null, 'gate opens the moment teardown completes');
  assertClose(tl.constructionStart, 2.0, 'assembly opens exactly at teardown completion');
}

// --- short countdown compresses the assembly (never below the floor)
{
  const tl = new IntermissionTimeline(10, allOn);
  tl.advance(2.5);
  tl.noteRoundCountdown(6);
  assertClose(tl.constructionEnd - tl.constructionStart, 6 - CONSTRUCTION_LEAD_SECS,
    'a countdown shorter than 10s compresses the assembly to finish before round start');
  const tiny = new IntermissionTimeline(10, allOn);
  tiny.advance(2.5);
  tiny.noteRoundCountdown(0.6);
  assertClose(tiny.constructionEnd - tiny.constructionStart, MIN_CONSTRUCTION_SECS,
    'compression floors at MIN_CONSTRUCTION_SECS');
}

// --- external block (mid-show stage resize) holds the gate closed
{
  const tl = new IntermissionTimeline(10, allOn);
  tl.setAssemblyBlocked(true);
  tl.advance(3.0);
  tl.noteRoundCountdown(10);
  assert.equal(tl.constructionStart, null, 'a blocked timeline must not open assembly');
  tl.setAssemblyBlocked(false);
  assert.notEqual(tl.constructionStart, null, 'unblocking opens the gate immediately');
}

// --- fallback pacing when countdown info never arrives
{
  const tl = new IntermissionTimeline(10, allOn);
  tl.advance(10 + COUNTDOWN_GRACE_SECS - 0.1);
  assert.equal(tl.constructionStart, null, 'fallback must not preempt the countdown wait');
  tl.advance(0.2);
  assert.notEqual(tl.constructionStart, null,
    'missing/late countdown info falls back to internal pacing');
  assertClose(tl.constructionEnd - tl.constructionStart, CONSTRUCTION_SECS,
    'fallback assembly runs the nominal 10s');
}

// --- re-pacing against later countdown observations stays monotonic
{
  const tl = new IntermissionTimeline(10, allOn);
  tl.advance(2.5);
  tl.noteRoundCountdown(4);
  tl.advance(1.0);
  const p1 = tl.constructionP;
  tl.noteRoundCountdown(8); // round pushed out — deadline extends
  tl.advance(0.3);
  assert.ok(tl.constructionP >= p1, 'progress never runs backwards across re-pacing');
  assert.ok(tl.constructionEnd > tl.constructionStart + 3.5);
}

// --- settings gating: disabled phases collapse; ring choreography alone
//     still wants an assembly window (for the gold-ring glide)
{
  const tl = new IntermissionTimeline(10, {});
  tl.advance(0.1);
  assert.equal(tl.phase(), 'waiting', 'a fully-disabled show idles until fast-forward');
  assert.ok(!tl.wantsAssembly);
  const rings = new IntermissionTimeline(10, { ringChoreography: true });
  assert.ok(rings.wantsAssembly);
  assert.equal(rings.teardownEnd, 0);
  rings.noteRoundCountdown(10);
  assert.notEqual(rings.constructionStart, null);
  assert.ok(!rings.titleCardVisible(), 'no construction flag, no title card');
}

// --- fast-forward + staleness
{
  const tl = new IntermissionTimeline(10, allOn);
  tl.advance(1);
  tl.fastForward();
  assert.equal(tl.phase(), 'done');
  assert.equal(tl.constructionP, 1, 'fast-forward snaps construction complete');
  const stale = new IntermissionTimeline(10, allOn);
  stale.advance(STALE_SHOW_SECS + 1);
  assert.ok(stale.stale, 'a show without a new round must go stale');
}

// --- round_end summary (winner / no-winner / safe_zone / invalid)
{
  const summary = summarizeRoundEnd({
    type: 'round_end',
    round_number: 7,
    intermission_secs: 12,
    winner: { id: 'b1', name: 'Alpha', color: '#ffce54' },
    next_map: {
      shape: 'circle', arena_size: [400, 400], obstacles: [], mask_rects: [],
      safe_zone: { center: [200, 200], radius: 282.8, target_center: [120, 90], target_radius: 20 },
    },
  });
  assert.equal(summary.roundNumber, 7);
  assert.equal(summary.intermissionSecs, 12);
  assert.deepEqual(summary.winner, { id: 'b1', name: 'Alpha', color: '#ffce54' });
  assert.equal(summary.nextMap.shape, 'circle');
  assert.deepEqual(summary.nextMap.safeZone, {
    center: [200, 200], radius: 282.8, targetCenter: [120, 90], targetRadius: 20,
  }, 'the next round zone preview must parse for the ring glide');

  const old = summarizeRoundEnd({
    type: 'round_end', round_number: 4, intermission_secs: 10,
    next_map: { shape: 'square', arena_size: [400, 400], obstacles: [] },
  });
  assert.equal(old.nextMap.safeZone, null, 'pre-#192 servers degrade gracefully');

  const draw = summarizeRoundEnd({ type: 'round_end', round_number: 3, intermission_secs: 10 });
  assert.equal(draw.winner, null, 'a draw carries no winner');
  assert.equal(draw.nextMap, null, 'a missing next_map degrades gracefully');

  assert.equal(summarizeRoundEnd({ type: 'arena_state' }), null);
  assert.equal(summarizeRoundEnd(null), null);
  assert.equal(summarizeRoundEnd({ type: 'round_end' }), null, 'round_number is required');
}

// --- obstacle stagger: center-out order, ~60% overlap, ends by 0.9
{
  const boxes = [
    { x: 90, y: 90, width: 20, height: 20 },   // center of a 200x200 arena
    { x: 0, y: 0, width: 20, height: 20 },     // far corner
    { x: 120, y: 100, width: 20, height: 20 }, // near center
    { x: 20, y: 150, width: 20, height: 20 },
    { x: 160, y: 30, width: 20, height: 20 },
  ];
  const sched = obstacleRiseSchedule(boxes, 200, 200);
  assert.equal(sched.length, boxes.length);
  assert.equal(sched[0].index, 0, 'the centre-most box rises first');
  for (let i = 1; i < sched.length; i++) {
    assert.ok(sched[i].start >= sched[i - 1].start, 'starts must be ordered by distance');
    const overlap = (sched[i - 1].start + sched[i - 1].dur - sched[i].start) / sched[i].dur;
    assert.ok(overlap > 0.55 && overlap < 0.65, `consecutive windows overlap ~60% (got ${overlap})`);
  }
  const last = sched[sched.length - 1];
  assert.ok(last.start + last.dur <= 0.9 + 1e-9, 'all rises finish by p=0.9');
  assert.deepEqual(obstacleRiseSchedule([], 200, 200), []);
}

// --- trim strips: bounded count, shared endpoints, sane geometry
{
  const ringRects = [
    { x: 0, y: 0, width: 200, height: 20 },
    { x: 0, y: 180, width: 200, height: 20 },
    { x: 0, y: 20, width: 20, height: 160 },
    { x: 180, y: 20, width: 20, height: 160 },
  ];
  const { groups } = buildBoundaryContours(ringRects);
  assert.ok(groups.length >= 1 && groups[0].holes.length >= 1,
    'border ring must produce an outer contour with the playfield hole');
  const strips = splitContoursIntoStrips(groups, MAX_TRIM_STRIPS);
  const contourCount = groups.reduce((n, g) => n + 1 + g.holes.length, 0);
  assert.ok(strips.length >= 2, 'the reveal needs multiple strips');
  assert.ok(strips.length <= MAX_TRIM_STRIPS + contourCount,
    `strip count must stay bounded (got ${strips.length})`);
  for (const strip of strips) {
    assert.ok(strip.points.length >= 2);
    assert.equal(strip.points.length, strip.normals.length);
  }
  const totalPts = groups.reduce((n, g) => n + [g.outer, ...g.holes].reduce((m, c) => m + c.length, 0), 0);
  const covered = strips.reduce((n, s) => n + (s.points.length - 1), 0);
  assert.equal(covered, totalPts, 'strips must cover every contour segment exactly once');

  const geo = buildTrimStripGeometry(strips[0].points, strips[0].normals);
  const n = strips[0].points.length;
  assert.equal(geo.positions.length, 12 * n, 'band + skirt = 4 vertices per point');
  assert.equal(geo.indices.length, 12 * (n - 1), 'open strip: no wrap-around quads');
  assert.equal(geo.normals.length, geo.positions.length);
}

// ---------------------------------------------------------------------------
// Director integration with stubbed BABYLON/DOM
// ---------------------------------------------------------------------------

function vec3() {
  return { x: 0, y: 0, z: 0, set(x, y, z) { this.x = x; this.y = y; this.z = z; } };
}
function color4() {
  return { set(r, g, b, a) { Object.assign(this, { r, g, b, a }); } };
}
function fakeMesh(name = '') {
  return {
    name,
    position: vec3(),
    rotation: vec3(),
    scaling: {
      x: 1, y: 1, z: 1,
      set(x, y, z) { this.x = x; this.y = y; this.z = z; },
      setAll(v) { this.x = this.y = this.z = v; },
    },
    material: null,
    isPickable: true,
    _enabled: true,
    _disposed: false,
    _frozen: true,
    setEnabled(v) { this._enabled = !!v; },
    isEnabled() { return this._enabled; },
    dispose() { this._disposed = true; },
    isDisposed() { return this._disposed; },
    freezeWorldMatrix() { this._frozen = true; },
    unfreezeWorldMatrix() { this._frozen = false; },
  };
}

class FakeBabylonMesh {
  constructor(name) { Object.assign(this, fakeMesh(name)); }
  static MergeMeshes(meshes) { meshes.forEach((m) => m.dispose()); return fakeMesh('merged'); }
}

globalThis.window = {
  BABYLON: {
    Color3: class { constructor(r, g, b) { this.r = r; this.g = g; this.b = b; } },
    ParticleSystem: { BLENDMODE_ADD: 1, BLENDMODE_STANDARD: 0 },
    MeshBuilder: {
      CreateBox: (name) => fakeMesh(name),
      CreateCylinder: (name) => fakeMesh(name),
    },
    Mesh: FakeBabylonMesh,
    VertexData: class { applyToMesh() {} },
  },
};

function makeElement(tag) {
  const el = {
    tagName: tag,
    className: '',
    textContent: '',
    style: {},
    children: [],
    parentNode: null,
    attrs: {},
    classes: new Set(),
    setAttribute(k, v) { el.attrs[k] = v; },
    appendChild(child) { child.parentNode = el; el.children.push(child); return child; },
    remove() {
      if (!el.parentNode) return;
      const i = el.parentNode.children.indexOf(el);
      if (i >= 0) el.parentNode.children.splice(i, 1);
      el.parentNode = null;
    },
  };
  el.classList = {
    add: (c) => el.classes.add(c),
    remove: (c) => el.classes.delete(c),
    contains: (c) => el.classes.has(c),
  };
  return el;
}
globalThis.document = { createElement: (tag) => makeElement(tag) };

function makeFxStub() {
  return {
    scene: {},
    teleports: [],
    launched: [],
    acquiredKeys: [],
    releasedKeys: [],
    releasedDiscs: 0,
    spawnTeleportBurst(...args) { this.teleports.push(args); },
    _acquirePS() {
      return {
        emitter: vec3(), direction1: vec3(), direction2: vec3(), gravity: vec3(),
        color1: color4(), color2: color4(), colorDead: color4(),
      };
    },
    _launch(ps) { this.launched.push(ps); },
    _acquireFxMesh(key, build) {
      this.acquiredKeys.push(key);
      return { key, mesh: build(), mat: { alpha: 1, emissiveColor: { set() {} } } };
    },
    _releaseFxMesh(entry) { this.releasedKeys.push(entry.key); },
    _acquireDisc() { return { mesh: fakeMesh('disc'), mat: { alpha: 1, emissiveColor: { set() {} } } }; },
    _releaseDisc() { this.releasedDiscs++; },
  };
}

function colorStub() {
  return {
    r: 0, g: 0, b: 0,
    set(r, g, b) { this.r = r; this.g = g; this.b = b; },
    copyFrom(c) { this.r = c.r; this.g = c.g; this.b = c.b; },
  };
}

function materialStub(name) {
  return {
    name,
    backFaceCulling: true,
    diffuseColor: colorStub(),
    emissiveColor: colorStub(),
    frozen: true,
    disposed: false,
    unfreeze() { this.frozen = false; },
    freeze() { this.frozen = true; },
    dispose() { this.disposed = true; },
    clone(cloneName) { return materialStub(cloneName); },
  };
}

// The fixture palette the env stub hands the director for the NEXT shape —
// the construction clones must wear exactly these values (issue #192 color
// parity: same source ObstacleRenderer._applyPalette reads at the handoff).
const NEXT_PALETTE = {
  obstacleBody: { diffuse: [0.09, 0.08, 0.095], emissive: [0.036, 0.024, 0.03] },
  obstacleTrim: [0.55, 0.26, 0.08],
};

function makeEnvStub() {
  return {
    shapes: [],
    rewinds: [],
    glides: [],
    settles: 0,
    paletteShapes: [],
    targetRingState: { cx: 30, cy: 40, r: 22 },
    setMapShape(shape) { this.shapes.push(shape); },
    getPaletteForShape(shape) { this.paletteShapes.push(shape); return NEXT_PALETTE; },
    beginZoneRingRewind(dur) { this.rewinds.push(dur); return true; },
    getZoneTargetRingState() { return this.targetRingState; },
    glideZoneTargetRing(to, dur, from) { this.glides.push({ to, dur, from }); return true; },
    settleZoneChoreography() { this.settles++; },
  };
}

function makeEngineStub() {
  const host = makeElement('div');
  const teardownMeshes = [fakeMesh('obstacleBodies'), fakeMesh('mapWallBody')];
  const winnerRoot = fakeMesh('winner-root');
  winnerRoot.position.set(100, 0, 100);
  const loserRoot = fakeMesh('loser-root');
  loserRoot.position.set(40, 0, 60);
  const entries = new Map([
    ['b-win', { root: winnerRoot, nameLabel: { isVisible: true }, hpContainer: { isVisible: true }, botData: { avatar_color: '#ffce54' } }],
    ['b-lose', { root: loserRoot, botData: { avatar_color: '#47d7ff' } }],
  ]);
  return {
    host,
    teardownMeshes,
    canvas: { parentElement: host },
    arenaWidth: 200,
    arenaHeight: 200,
    scene: {},
    glowLayer: null,
    resizes: [],
    shouldSpawnEffects: () => true,
    resizeStageForShow(w, h) {
      this.resizes.push([w, h]);
      this.arenaWidth = w;
      this.arenaHeight = h;
      return true;
    },
    camera: {
      autoPan: true,
      followed: [],
      autoPanSet: [],
      followBot(id) { this.followed.push(id); },
      setAutoPan(v) { this.autoPanSet.push(v); this.autoPan = v; },
    },
    botRenderer: {
      entries,
      despawned: [],
      despawnEntry(id) { this.despawned.push(id); this.entries.delete(id); },
    },
    envRenderer: makeEnvStub(),
    obstacleRenderer: {
      updates: [],
      collectTeardownMeshes: () => teardownMeshes,
      getConstructionMaterials: () => ({ body: materialStub('obsMat'), trim: materialStub('obsEdgeMat') }),
      getLastLayout: () => ({ obstacles: [{ x: 40, y: 40, width: 40, height: 20 }], maskRects: [] }),
      update(obstacles, maskRects) { this.updates.push({ obstacles, maskRects }); },
    },
    effectRenderer: makeFxStub(),
  };
}

const MASK_RECTS = [
  { x: 0, y: 0, width: 200, height: 20 },
  { x: 0, y: 180, width: 200, height: 20 },
  { x: 0, y: 20, width: 20, height: 160 },
  { x: 180, y: 20, width: 20, height: 160 },
];
const NEXT_OBSTACLES = [
  { x: 40, y: 40, width: 40, height: 20 },
  { x: 120, y: 60, width: 20, height: 40 },
  { x: 80, y: 120, width: 40, height: 40 },
  ...MASK_RECTS, // keyframe parity: mask rects ride the obstacles list too
];
const SAFE_ZONE = { center: [100, 100], radius: 141.4, target_center: [60, 80], target_radius: 20 };
const ROUND_END_MSG = {
  type: 'round_end',
  round_number: 5,
  intermission_secs: 10,
  winner: { id: 'b-win', name: 'Alpha', color: '#ffce54' },
  next_map: {
    shape: 'cross', arena_size: [200, 200],
    obstacles: NEXT_OBSTACLES, mask_rects: MASK_RECTS, safe_zone: SAFE_ZONE,
  },
};

// --- full show: banner+teardown overlap, countdown-gated color-true build,
//     ring choreography, handoff, hold, fast-forward
{
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd(ROUND_END_MSG);
  assert.ok(director.active);
  assert.ok(director.holdsBots(), 'despawn phase must hold bot snapshot updates');
  assert.ok(director.holdsWorld(), 'teardown/construction must hold obstacle keyframes');
  assert.deepEqual(engine.camera.followed, ['b-win'],
    'auto-pan camera gets the gentle push toward the winner');
  assert.deepEqual(engine.envRenderer.rewinds, [TEARDOWN_PHASE_SECS],
    'the blue ring rewind starts immediately at round_end');

  director.update(0.05);
  const overlay = engine.host.children[0];
  assert.equal(overlay.className, 'intermission-overlay');
  const banner = overlay.children[0];
  assert.equal(banner.className, 'intermission-banner');
  assert.equal(banner.children[0].textContent, 'WINNER');
  assert.equal(banner.children[1].textContent, 'Alpha');
  assert.equal(banner.children[1].style.color, '#ffce54');
  // Teardown overlaps the winner beat: the meshes are already unfrozen and
  // sinking on the very first frame (issue #192 re-anchor).
  for (const mesh of engine.teardownMeshes) {
    assert.equal(mesh._frozen, false, 'teardown must start at round_end, not after the banner');
    assert.ok(mesh.scaling.y < 1, 'teardown must sink the merged meshes from t=0');
  }

  // Beat 1 (0.35s): everyone but the winner teleports out; hero pillar rises.
  director.update(0.4);
  assert.deepEqual(engine.botRenderer.despawned, ['b-lose']);
  assert.equal(engine.effectRenderer.teleports.length, 1);
  assert.ok(engine.effectRenderer.acquiredKeys.includes('intermission-pillar'));

  // Beat 2 (1.0s+): winner ascends...
  director.update(0.8); // t=1.25
  const winnerRoot = engine.botRenderer.entries.get('b-win').root;
  assert.ok(winnerRoot.position.y > 0, 'winner must lift off during the ascend');
  // ...and despawns by the end of the winner beat; the banner dismisses.
  director.update(1.3); // t=2.55
  assert.ok(engine.botRenderer.despawned.includes('b-win'));
  assert.ok(!banner.classes.has('visible'), 'banner auto-dismisses when the winner beat ends');
  // Teardown is complete — old map fully gone — but with no countdown yet
  // there must be NO construction (strict gating).
  for (const mesh of engine.teardownMeshes) {
    assertClose(mesh.scaling.y, 0.02, 'old map fully sunk before any construction');
    assert.equal(mesh.isEnabled(), false, 'old map must be hidden when the 2s teardown completes');
  }
  assert.equal(director._show.construction, null,
    'construction must not start before the lobby countdown');
  director.update(3.0); // t=5.55 — still waiting
  assert.equal(director._show.timeline.constructionStart, null);

  // Countdown starts (12s > 10s assembly): construction opens at full 10s.
  director.handleLobbyState({ countdown: 12 });
  director.update(0.05); // t=5.6 — assembly begins this frame
  const tl = director._show.timeline;
  assertClose(tl.constructionEnd - tl.constructionStart, CONSTRUCTION_SECS,
    'a 12s countdown gives the full 10s assembly');
  const construction = director._show.construction;
  assert.ok(construction && construction.ok, 'construction must pre-build from next_map');
  assert.ok(construction.wallMesh, 'smooth boundary wall must pre-build');
  assert.ok(construction.strips.length >= 2, 'trim reveal needs strips');
  assert.equal(construction.clones.length, 3, 'mask rects must not become obstacle clones');
  for (const clone of construction.clones) {
    assert.equal(clone.meshes.length, 2,
      'each rise unit carries a body AND a trim mesh (final appearance, issue #192)');
  }

  // Color parity: the stage flips to the next map identity as the build
  // starts, and every clone wears the NEXT palette exactly.
  assert.deepEqual(engine.envRenderer.shapes, ['cross'],
    'stage identity (palette/floor/wall trim) flips at assembly start');
  assert.deepEqual(engine.envRenderer.paletteShapes, ['cross']);
  const bodyMat = construction.clones[0].meshes[0].material;
  const trimMat = construction.clones[0].meshes[1].material;
  assert.deepEqual(
    [bodyMat.diffuseColor.r, bodyMat.diffuseColor.g, bodyMat.diffuseColor.b],
    NEXT_PALETTE.obstacleBody.diffuse, 'clone bodies wear the next palette diffuse');
  assert.deepEqual(
    [bodyMat.emissiveColor.r, bodyMat.emissiveColor.g, bodyMat.emissiveColor.b],
    NEXT_PALETTE.obstacleBody.emissive, 'clone bodies wear the next palette emissive');
  assert.deepEqual(
    [trimMat.emissiveColor.r, trimMat.emissiveColor.g, trimMat.emissiveColor.b],
    NEXT_PALETTE.obstacleTrim, 'clone trims wear the next palette trim emissive');
  assert.equal(trimMat.backFaceCulling, false, 'trim clone must render open geometry');
  assert.equal(construction.wallMesh.material, bodyMat, 'wall rise shares the tinted body mat');
  for (const strip of construction.strips) {
    assert.equal(strip.material, trimMat, 'trim strips share the tinted trim mat');
  }

  // Gold-ring glide: old spot -> announced next-round target, ends with the
  // assembly.
  assert.equal(engine.envRenderer.glides.length, 1);
  const glide = engine.envRenderer.glides[0];
  assert.deepEqual(glide.to, { cx: 60, cy: 80, r: 20 }, 'glide lands on next_map.safe_zone target');
  assert.deepEqual(glide.from, engine.envRenderer.targetRingState);
  assert.ok(Math.abs(glide.dur - (tl.constructionEnd - 5.6)) < 0.01,
    'glide is timed to land with the assembly');

  // Progress toward the 10s deadline; title card near the end.
  director.update(7.0); // t=12.6
  assert.ok(!director._show.titleShown);
  director.update(1.5); // t=14.1 >= constructionEnd(15.55) - 2
  const title = overlay.children.find((c) => c.className === 'intermission-title');
  assert.ok(title, 'ROUND N title card must appear near the end');
  assert.equal(title.children[0].textContent, 'ROUND 6');
  assert.equal(title.children[1].textContent, 'CROSS');

  // Completion: transient geometry disposed, real builders get keyframe data.
  director.update(1.6); // t=15.7 >= constructionEnd
  assert.equal(engine.obstacleRenderer.updates.length, 1, 'handoff must feed the real renderers once');
  assert.equal(engine.obstacleRenderer.updates[0].obstacles, NEXT_OBSTACLES);
  assert.equal(engine.obstacleRenderer.updates[0].maskRects, MASK_RECTS);
  assert.ok(construction.wallMesh === null || construction.wallMesh.isDisposed());
  for (const mesh of engine.teardownMeshes) {
    assert.equal(mesh.scaling.y, 1, 'teardown meshes restore before the real rebuild');
    assert.equal(mesh.isEnabled(), true, 'handoff restores teardown meshes before replacing them');
    assert.equal(mesh._frozen, true, 'restored meshes re-freeze');
  }
  // The hold: title card stays up until the round actually starts.
  director.update(2.0);
  assert.ok(director._show.timeline.titleCardVisible(), 'finished map + title card hold');

  // New round's first arena_state: fast-forward returns control.
  director.handleArenaState({ type: 'arena_state', round_number: 6 });
  assert.ok(!director.active);
  assert.ok(!director.holdsBots() && !director.holdsWorld());
  assert.deepEqual(engine.camera.autoPanSet, [true], 'camera push restores auto-pan');
  assert.equal(engine.envRenderer.settles, 1, 'ring choreography settles on fast-forward');
  director.dispose();
}

// --- cluster parity: touching rects rise as ONE union unit (issue #190)
{
  globalThis.earcut = (flat) => (flat.length >= 6 ? [0, 1, 2] : []);
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  const clusterMsg = {
    ...ROUND_END_MSG,
    next_map: {
      ...ROUND_END_MSG.next_map,
      obstacles: [
        { x: 40, y: 40, width: 20, height: 20 },
        { x: 60, y: 40, width: 20, height: 20 }, // touches the first — one union
        { x: 120, y: 120, width: 20, height: 20 },
        ...MASK_RECTS,
      ],
    },
  };
  director.handleRoundEnd(clusterMsg);
  director.update(2.1);
  director.handleLobbyState({ countdown: 10 });
  director.update(0.05);
  const c = director._show.construction;
  assert.ok(c && c.ok);
  assert.equal(c.clones.length, 2,
    'touching rects must rise as one cluster unit + one isolated unit');
  for (const clone of c.clones) assert.equal(clone.meshes.length, 2);
  director.dispose();
  delete globalThis.earcut;
}

// --- no-winner branch: ROUND OVER banner, no hero, no camera push
{
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd({ ...ROUND_END_MSG, winner: undefined });
  assert.deepEqual(engine.camera.followed, [], 'no winner, no camera push');
  director.update(0.05);
  const banner = engine.host.children[0].children[0];
  assert.equal(banner.children[0].textContent, 'ROUND OVER');
  assert.equal(banner.children.length, 1, 'draw banner has no name line');
  director.update(0.4);
  assert.deepEqual(engine.botRenderer.despawned.sort(), ['b-lose', 'b-win'],
    'a draw teleports every lingering bot out');
  assert.ok(!engine.effectRenderer.acquiredKeys.includes('intermission-pillar'),
    'no hero pillar without a winner');
  director.dispose();
}

// --- mid-show fast-forward (early new round) restores everything
{
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd(ROUND_END_MSG);
  director.update(1.5); // teardown in flight
  assert.ok(engine.teardownMeshes[0].scaling.y < 1);
  director.handleArenaState({ type: 'arena_state', round_number: 6 });
  assert.ok(!director.active);
  assert.equal(engine.teardownMeshes[0].scaling.y, 1, 'fast-forward restores sunken meshes');
  assert.equal(engine.teardownMeshes[0]._frozen, true);
  assert.ok(engine.botRenderer.despawned.includes('b-win'),
    'a mid-ascend winner is dropped through the disposal path');
  assert.equal(engine.envRenderer.settles, 1);
  director.dispose();
}

// --- stale show snaps itself away
{
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd(ROUND_END_MSG);
  director.update(STALE_SHOW_SECS + 1);
  assert.ok(!director.active, 'a stale show must fast-forward on its own');
  director.dispose();
}

// --- arena resize next round: NO skip — the stage rebuilds inside the show
//     (issue #192; this was the "map just pops in" report on Cross)
{
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  const resized = {
    ...ROUND_END_MSG,
    next_map: { ...ROUND_END_MSG.next_map, arena_size: [400, 400] },
  };
  director.handleRoundEnd(resized);
  assert.ok(director._show.needsResize);
  assert.ok(director._show.timeline.blocked, 'assembly gate held closed for the resize');
  director.handleLobbyState({ countdown: 15 }); // countdown early — still strict order
  director.update(1.0);
  assert.deepEqual(engine.resizes, [], 'resize must wait for the teardown to complete');
  assert.equal(director._show.timeline.constructionStart, null);
  director.update(1.1); // t=2.1 — teardown done: resize fires, gate opens
  assert.deepEqual(engine.resizes, [[400, 400]], 'stage rebuilds at the new size in-show');
  assert.equal(engine.arenaWidth, 400);
  director.update(0.05);
  const c = director._show.construction;
  assert.ok(c && c.ok,
    'a resizing next round must still run the full construction (no more skip)');
  assert.ok(c.clones.length > 0);
  director.update(15); // past the deadline
  assert.equal(engine.obstacleRenderer.updates.length, 1,
    'handoff still feeds the real renderers after an in-show resize');
  director.dispose();
}

// --- async resize path: assembly stays blocked until the rebuild settles
{
  const engine = makeEngineStub();
  engine.resizeStageForShow = function (w, h) {
    this.resizes.push([w, h]);
    this.arenaWidth = w;
    this.arenaHeight = h;
    return Promise.resolve(true);
  };
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd({
    ...ROUND_END_MSG,
    next_map: { ...ROUND_END_MSG.next_map, arena_size: [400, 400] },
  });
  director.handleLobbyState({ countdown: 15 });
  director.update(2.1); // resize kicked off, promise pending
  assert.deepEqual(engine.resizes, [[400, 400]]);
  director.update(0.05);
  assert.equal(director._show.construction, null,
    'construction must not race an in-flight stage rebuild');
  await Promise.resolve(); // settle the rebuild
  director.update(0.05);
  assert.ok(director._show.construction && director._show.construction.ok,
    'assembly opens once the rebuild settles');
  director.dispose();
}

// --- ring choreography gating
{
  setEffect('intermissionShow', 'ringChoreography', false);
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd(ROUND_END_MSG);
  assert.deepEqual(engine.envRenderer.rewinds, [], 'toggle off: no blue-ring rewind');
  director.update(2.1);
  director.handleLobbyState({ countdown: 10 });
  director.update(0.05);
  assert.deepEqual(engine.envRenderer.glides, [], 'toggle off: no gold-ring glide');
  director.handleArenaState({ type: 'arena_state', round_number: 6 });
  assert.equal(engine.envRenderer.settles, 0, 'toggle off: nothing to settle');
  director.dispose();
  setEffect('intermissionShow', 'ringChoreography', true);
}

// --- ring choreography ALONE still runs (and holds neither bots nor world)
{
  for (const key of ['winnerBanner', 'botDespawn', 'mapTeardown', 'mapConstruction']) {
    setEffect('intermissionShow', key, false);
  }
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd(ROUND_END_MSG);
  assert.ok(director.active, 'ring choreography alone keeps the show live');
  assert.ok(!director.holdsBots() && !director.holdsWorld());
  assert.equal(engine.envRenderer.rewinds.length, 1);
  director.handleLobbyState({ countdown: 10 });
  director.update(0.05);
  assert.equal(engine.envRenderer.glides.length, 1);
  assert.equal(director._show.construction, null, 'no construction visuals when toggled off');
  director.dispose();
  for (const key of ['winnerBanner', 'botDespawn', 'mapTeardown', 'mapConstruction']) {
    setEffect('intermissionShow', key, true);
  }
}

// --- live settings gating: with every phase off the show is inert
{
  const keys = ['winnerBanner', 'botDespawn', 'mapTeardown', 'mapConstruction', 'ringChoreography'];
  for (const key of keys) setEffect('intermissionShow', key, false);
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd(ROUND_END_MSG);
  assert.ok(!director.active, 'all phases disabled leaves the round_end inert');
  assert.equal(engine.host.children.length, 0, 'no overlay is mounted');
  assert.deepEqual(engine.botRenderer.despawned, []);
  director.dispose();
  for (const key of keys) setEffect('intermissionShow', key, true);
}

// --- partial gating: banner-only shows never touch bots or the world
{
  setEffect('intermissionShow', 'botDespawn', false);
  setEffect('intermissionShow', 'mapTeardown', false);
  setEffect('intermissionShow', 'mapConstruction', false);
  setEffect('intermissionShow', 'ringChoreography', false);
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd(ROUND_END_MSG);
  assert.ok(director.active);
  assert.ok(!director.holdsBots(), 'banner-only show must not hold bot updates');
  assert.ok(!director.holdsWorld(), 'banner-only show must not hold obstacle keyframes');
  director.update(0.5);
  assert.deepEqual(engine.botRenderer.despawned, [], 'botDespawn off keeps every entry');
  assert.equal(engine.teardownMeshes[0]._frozen, true, 'mapTeardown off never touches the meshes');
  director.dispose();
  setEffect('intermissionShow', 'botDespawn', true);
  setEffect('intermissionShow', 'mapTeardown', true);
  setEffect('intermissionShow', 'mapConstruction', true);
  setEffect('intermissionShow', 'ringChoreography', true);
}

// ---------------------------------------------------------------------------
// Color-parity source pin: the director's rise tint and the real build's
// _applyPalette must read the SAME palette fields, and the real build's
// handoff path must re-theme through setMapShape before updating obstacles.
// ---------------------------------------------------------------------------
{
  const obstaclesSource = readFileSync(new URL('../frontend/js/renderer/obstacles.js', import.meta.url), 'utf8');
  const directorSource = readFileSync(new URL('../frontend/js/renderer/intermission-director.js', import.meta.url), 'utf8');
  for (const field of ['palette.obstacleBody.diffuse', 'palette.obstacleBody.emissive', 'palette.obstacleTrim']) {
    assert.ok(obstaclesSource.includes(field), `real build must tint from ${field}`);
    assert.ok(directorSource.includes(field), `construction clones must tint from ${field}`);
  }
  assert.match(directorSource, /setMapShape\(nm\.shape\)/,
    'the director must flip the stage identity so the handoff palette matches');
  const updateStart = directorSource.indexOf('  update(dt) {');
  const updateBody = directorSource.slice(
    updateStart,
    directorSource.indexOf('  fastForward() {', updateStart),
  );
  assert.ok(updateBody.length > 0, 'director.update body must remain discoverable');
  assert.ok(!/isEnabled\(/.test(updateBody),
    'director.update must stay free of settings reads (issue #180 discipline)');
}

console.log('intermission director: re-anchored timeline, gating, resize, rings, color parity, and fast-forward behave');
