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

assert.match(engineSource, /intermission-director\.js\?v=20260718f/,
  'engine.js must import the stamped intermission director');
assert.match(engineSource, /intermissionDirector\.update\(dt\)/,
  'the render loop must tick the director per frame');
assert.match(engineSource, /'round_end'/,
  'engine.setState must route the typed round_end broadcast');
assert.match(engineSource, /handleLobbyState/,
  'engine.setState must feed lobby countdowns to the director');

const schemaSlice = settingsSource.slice(
  settingsSource.indexOf('intermissionShow'),
  settingsSource.indexOf('siteMotion'),
);
assert.ok(schemaSlice.length > 0, 'settings schema must contain an intermissionShow section');
for (const key of ['winnerBanner', 'botDespawn', 'mapTeardown', 'mapConstruction']) {
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
  planPhases,
  IntermissionTimeline,
  summarizeRoundEnd,
  obstacleRiseSchedule,
  splitContoursIntoStrips,
  buildTrimStripGeometry,
  IntermissionDirector,
  WINNER_PHASE_SECS,
  TEARDOWN_PHASE_SECS,
  MIN_CONSTRUCTION_SECS,
  STALE_SHOW_SECS,
  MAX_TRIM_STRIPS,
} = await import('../frontend/js/renderer/intermission-director.js');
const { buildBoundaryContours } = await import('../frontend/js/renderer/map-walls.js');
const { setEffect } = await import('../frontend/js/settings.js');

// --- phase scheduling from intermission_secs
const allOn = { winnerBanner: true, botDespawn: true, mapTeardown: true, mapConstruction: true };
{
  const plan = planPhases(10, allOn);
  assert.equal(plan.winner, WINNER_PHASE_SECS);
  assert.equal(plan.teardown, TEARDOWN_PHASE_SECS);
  assert.equal(plan.constructionStart, WINNER_PHASE_SECS + TEARDOWN_PHASE_SECS);
  assert.equal(plan.constructionEnd, 9.5, 'construction paces to finish just before the intermission ends');
}
{
  // Short intermissions still get a readable construction (floor applies).
  const plan = planPhases(3, allOn);
  assert.equal(plan.constructionEnd - plan.constructionStart, MIN_CONSTRUCTION_SECS);
}
{
  // Bad input falls back to the 10s default.
  const plan = planPhases(NaN, allOn);
  assert.equal(plan.constructionEnd, 9.5);
}

// --- settings gating: disabled phases collapse to zero
{
  const plan = planPhases(10, { mapConstruction: true });
  assert.equal(plan.winner, 0);
  assert.equal(plan.teardown, 0);
  assert.equal(plan.constructionStart, 0);
  assert.equal(plan.constructionEnd, 9.5);
}
{
  const plan = planPhases(10, {});
  assert.equal(plan.winner + plan.teardown + plan.constructionEnd, 0,
    'all phases off must schedule nothing');
  const tl = new IntermissionTimeline(10, {});
  tl.advance(0.1);
  assert.equal(tl.phase(), 'waiting', 'a fully-disabled show idles until fast-forward');
}

// --- phase sequencing + monotonic construction progress
{
  const tl = new IntermissionTimeline(10, allOn);
  tl.advance(1.0);
  assert.equal(tl.phase(), 'winner');
  tl.advance(2.0); // 3.0
  assert.equal(tl.phase(), 'teardown');
  assert.ok(tl.teardownT() > 0 && tl.teardownT() < 1);
  tl.advance(2.0); // 5.0
  assert.equal(tl.phase(), 'construction');
  const p1 = tl.constructionP;
  assert.ok(p1 > 0 && p1 < 1);
  // Lobby countdown observed: round starts later than planned — progress
  // pauses (never runs backwards) and the deadline extends.
  tl.noteRoundCountdown(8);
  tl.advance(0.5);
  assert.ok(tl.constructionP >= p1, 'construction progress must be monotonic across re-pacing');
  assert.ok(tl.constructionEnd > 9.5, 're-pacing must extend the deadline toward the observed countdown');
  assert.ok(!tl.titleCardVisible());
  // Run past the re-paced deadline (5.0 + max(0.75, 8-0.5) = 12.5s): completes.
  tl.advance(7.5);
  assert.equal(tl.constructionP, 1);
  tl.advance(20);
  assert.equal(tl.phase(), 'waiting');
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

// --- round_end summary (winner / no-winner / invalid)
{
  const summary = summarizeRoundEnd({
    type: 'round_end',
    round_number: 7,
    intermission_secs: 12,
    winner: { id: 'b1', name: 'Alpha', color: '#ffce54' },
    next_map: { shape: 'circle', arena_size: [400, 400], obstacles: [], mask_rects: [] },
  });
  assert.equal(summary.roundNumber, 7);
  assert.equal(summary.intermissionSecs, 12);
  assert.deepEqual(summary.winner, { id: 'b1', name: 'Alpha', color: '#ffce54' });
  assert.equal(summary.nextMap.shape, 'circle');

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

function makeTrimMatStub() {
  return {
    emissiveColor: {},
    clone(name) {
      return {
        name,
        backFaceCulling: true,
        emissiveColor: { copyFrom() {} },
        unfreeze() {},
        freeze() {},
        dispose() {},
      };
    },
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
    shouldSpawnEffects: () => true,
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
    obstacleRenderer: {
      updates: [],
      collectTeardownMeshes: () => teardownMeshes,
      getConstructionMaterials: () => ({ body: { name: 'obsMat' }, trim: makeTrimMatStub() }),
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
const ROUND_END_MSG = {
  type: 'round_end',
  round_number: 5,
  intermission_secs: 10,
  winner: { id: 'b-win', name: 'Alpha', color: '#ffce54' },
  next_map: { shape: 'circle', arena_size: [200, 200], obstacles: NEXT_OBSTACLES, mask_rects: MASK_RECTS },
};

// --- full winner show: banner, despawn, teardown, construction, handoff
{
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd(ROUND_END_MSG);
  assert.ok(director.active);
  assert.ok(director.holdsBots(), 'despawn phase must hold bot snapshot updates');
  assert.ok(director.holdsWorld(), 'teardown/construction must hold obstacle keyframes');
  assert.deepEqual(engine.camera.followed, ['b-win'],
    'auto-pan camera gets the gentle push toward the winner');

  director.update(0.05);
  const overlay = engine.host.children[0];
  assert.equal(overlay.className, 'intermission-overlay');
  const banner = overlay.children[0];
  assert.equal(banner.className, 'intermission-banner');
  assert.equal(banner.children[0].textContent, 'WINNER');
  assert.equal(banner.children[1].textContent, 'Alpha');
  assert.equal(banner.children[1].style.color, '#ffce54');

  // Beat 1 (0.35s): everyone but the winner teleports out; hero pillar rises.
  director.update(0.4);
  assert.deepEqual(engine.botRenderer.despawned, ['b-lose']);
  assert.equal(engine.effectRenderer.teleports.length, 1);
  assert.ok(engine.effectRenderer.acquiredKeys.includes('intermission-pillar'));

  // Beat 2 (1.0s+): winner ascends...
  director.update(0.8); // t=1.25
  const winnerRoot = engine.botRenderer.entries.get('b-win').root;
  assert.ok(winnerRoot.position.y > 0, 'winner must lift off during the ascend');
  // ...and despawns by the end of phase A.
  director.update(1.3); // t=2.55
  assert.ok(engine.botRenderer.despawned.includes('b-win'));
  assert.ok(!banner.classes.has('visible'), 'banner auto-dismisses when phase A ends');

  // Phase B: teardown meshes unfreeze and sink.
  director.update(1.0); // t=3.55
  for (const mesh of engine.teardownMeshes) {
    assert.equal(mesh._frozen, false, 'teardown must unfreeze the merged meshes');
    assert.ok(mesh.scaling.y < 1, 'teardown must sink the merged meshes');
  }

  // Phase C: construction progresses toward the 9.5s deadline.
  director.update(2.0); // t=5.55
  const construction = director._show.construction;
  assert.ok(construction && construction.ok, 'construction must pre-build from next_map');
  assert.ok(construction.wallMesh, 'smooth boundary wall must pre-build');
  assert.ok(construction.strips.length >= 2, 'trim reveal needs strips');
  assert.equal(construction.clones.length, 3, 'mask rects must not become obstacle clones');
  director.update(2.5); // t=8.05 — title card window opens at 7.5
  const title = overlay.children.find((c) => c.className === 'intermission-title');
  assert.ok(title, 'ROUND N title card must appear near the end');
  assert.equal(title.children[0].textContent, 'ROUND 6');
  assert.equal(title.children[1].textContent, 'CIRCLE');

  // Completion: transient geometry disposed, real builders get keyframe data.
  director.update(1.6); // t=9.65 >= constructionEnd
  assert.equal(engine.obstacleRenderer.updates.length, 1, 'handoff must feed the real renderers once');
  assert.equal(engine.obstacleRenderer.updates[0].obstacles, NEXT_OBSTACLES);
  assert.equal(engine.obstacleRenderer.updates[0].maskRects, MASK_RECTS);
  assert.ok(construction.wallMesh === null || construction.wallMesh.isDisposed());
  for (const mesh of engine.teardownMeshes) {
    assert.equal(mesh.scaling.y, 1, 'teardown meshes restore before the real rebuild');
    assert.equal(mesh._frozen, true, 'restored meshes re-freeze');
  }

  // New round's first arena_state: fast-forward returns control.
  director.handleArenaState({ type: 'arena_state', round_number: 6 });
  assert.ok(!director.active);
  assert.ok(!director.holdsBots() && !director.holdsWorld());
  assert.deepEqual(engine.camera.autoPanSet, [true], 'camera push restores auto-pan');
  director.dispose();
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
  director.update(3.0); // into teardown
  assert.ok(engine.teardownMeshes[0].scaling.y < 1);
  director.handleArenaState({ type: 'arena_state', round_number: 6 });
  assert.ok(!director.active);
  assert.equal(engine.teardownMeshes[0].scaling.y, 1, 'fast-forward restores sunken meshes');
  assert.equal(engine.teardownMeshes[0]._frozen, true);
  assert.ok(engine.botRenderer.despawned.includes('b-win'),
    'a mid-ascend winner is dropped through the disposal path');
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

// --- arena resize next round: construction degrades to the title card
{
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  const resized = {
    ...ROUND_END_MSG,
    next_map: { ...ROUND_END_MSG.next_map, arena_size: [400, 400] },
  };
  director.handleRoundEnd(resized);
  director.update(5.0); // straight into the construction window
  const c = director._show.construction;
  assert.ok(c && !c.ok, 'a resizing next round must skip the 3D pre-build');
  assert.equal(c.wallMesh, null);
  assert.equal(c.clones.length, 0);
  director.update(4.7); // past the deadline
  assert.equal(engine.obstacleRenderer.updates.length, 0,
    'no handoff with mismatched arena dimensions — the keyframe rebuild owns the swap');
  director.dispose();
}

// --- live settings gating: with every phase off the show is inert
{
  for (const key of ['winnerBanner', 'botDespawn', 'mapTeardown', 'mapConstruction']) {
    setEffect('intermissionShow', key, false);
  }
  const engine = makeEngineStub();
  const director = new IntermissionDirector(engine);
  director.handleRoundEnd(ROUND_END_MSG);
  assert.ok(!director.active, 'all phases disabled leaves the round_end inert');
  assert.equal(engine.host.children.length, 0, 'no overlay is mounted');
  assert.deepEqual(engine.botRenderer.despawned, []);
  director.dispose();
  for (const key of ['winnerBanner', 'botDespawn', 'mapTeardown', 'mapConstruction']) {
    setEffect('intermissionShow', key, true);
  }
}

// --- partial gating: banner-only shows never touch bots or the world
{
  setEffect('intermissionShow', 'botDespawn', false);
  setEffect('intermissionShow', 'mapTeardown', false);
  setEffect('intermissionShow', 'mapConstruction', false);
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
}

console.log('intermission director: timeline, gating, choreography, and fast-forward behave');
