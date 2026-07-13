import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const trailURL = new URL('../frontend/js/renderer/trails.js', import.meta.url);
let source = readFileSync(trailURL, 'utf8');
const goCatalogSource = readFileSync(new URL('../go-arena/internal/db/cosmetics.go', import.meta.url), 'utf8');
const themeSource = readFileSync(new URL('../frontend/js/cosmetic-themes.js', import.meta.url), 'utf8');

assert.match(source, /const MAX_RENDERED_TRAILS = 48;/,
  'live cosmetic trails must retain the existing large-roster ribbon budget');
assert.match(source, /const MAX_PARTICLE_SYSTEMS = 24;/,
  'particle-emitting trail styles need an independent bounded system budget');
assert.match(source, /const MAX_PARTICLES_PER_TRAIL = 28;/,
  'each particle system needs a fixed per-bot particle capacity');
assert.equal((source.match(/new B\.StandardMaterial\(/g) || []).length, 1,
  'all 48 ribbons must share one renderer-owned Babylon material');
assert.match(source, /this\.sharedRibbonMaterial/,
  'ribbon cleanup must retain one shared material instead of allocating per bot');
assert.match(source, /material\.freeze\(\)/,
  'the static renderer-owned ribbon material should remain frozen');
assert.match(source, /this\.particleSystemCount\s*>=\s*MAX_PARTICLE_SYSTEMS/,
  'particle creation must enforce the independent system cap before allocating');
assert.match(source, /new B\.ParticleSystem\([\s\S]*?MAX_PARTICLES_PER_TRAIL/,
  'particle systems must use the fixed per-trail capacity');
assert.match(source, /document\.hidden/,
  'hidden tabs must stop trail particle emission before scene rendering resumes');
assert.match(source, /dispose\(false\)/,
  'per-bot particle systems must not dispose the shared procedural texture');
assert.match(source, /sharedParticleTexture/,
  'particle trail styles must reuse one renderer-owned procedural texture');
assert.match(source, /prefers-reduced-motion:\s*reduce/,
  'reduced motion must keep a static ribbon fallback without particle emission');

source = source.replace(/import \{ isEnabled \} from '[^']+';\r?\n/, 'const isEnabled = () => true;\n');
const trails = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);

const paidAssets = [
  'ember_sparks', 'frost_shards', 'ion_stream', 'plasma_ribbon',
  'void_motes', 'solar_wake', 'lunar_dust', 'comet_tail',
  'nebula_pulse', 'storm_arcs', 'static_glitch', 'pixel_scatter',
  'data_stream', 'holo_prism', 'toxic_spores', 'verdant_leaves',
  'sand_wake', 'magma_cinders', 'ocean_spray', 'gilded_dust',
  'rune_sparks', 'phantom_smoke', 'gear_sparks', 'bounty_flare',
];
const goTrailSeedBlock = goCatalogSource.slice(
  goCatalogSource.indexOf('var trailCosmeticSeeds'),
  goCatalogSource.indexOf('var generatedCosmeticAssetPattern'),
);
const goTrailAssets = Array.from(goTrailSeedBlock.matchAll(/Slug:\s*"([a-z0-9_]+)"/g), match => match[1]);
assert.deepEqual(goTrailAssets, paidAssets,
  'Go catalog trail seeds and the renderer allowlist must remain in the same explicit order');
for (const asset of paidAssets) {
  assert.match(themeSource, new RegExp(`\\b${asset}:`),
    `Go/renderer trail ${asset} must also have a local storefront swatch`);
}

const resolved = paidAssets.map(asset => trails.resolveTrailStyle(asset));
assert.equal(resolved.length, 24);
assert.ok(resolved.every(style => style && style.key !== 'standard'),
  'every paid catalog key must resolve to a non-default local trail style');
assert.ok(resolved.every(style => style.width >= 1.2 && style.alpha >= 0.58),
  'every paid trail must remain clearly wider and more opaque than the free standard wake');
assert.ok(resolved.every(style => style.particles?.emitRate >= 24),
  'paid particle signatures need enough emission to remain legible at spectator zoom');
assert.ok(resolved.every(style => /^#[0-9a-f]{6}$/i.test(style.primary)
  && /^#[0-9a-f]{6}$/i.test(style.secondary)),
  'every trail style needs two bounded local colors');
assert.ok(resolved.some(style => style.particles?.emitRate > 0),
  'the trail collection must include particle-emitting styles');
assert.ok(resolved.some(style => style.particles?.gravityY < 0)
  && resolved.some(style => style.particles?.gravityY > 0),
  'the collection should cover falling and rising particle motion');
assert.equal(trails.resolveTrailStyle('unknown_trail').key, 'standard',
  'untrusted or retired trail keys must fail closed to the free standard wake');

class FakeColor3 {
  static parseCount = 0;
  constructor(r, g, b) { Object.assign(this, {r, g, b}); }
  static FromHexString(value) {
    FakeColor3.parseCount += 1;
    const hex = String(value).replace('#', '');
    return new FakeColor3(
      Number.parseInt(hex.slice(0, 2), 16) / 255,
      Number.parseInt(hex.slice(2, 4), 16) / 255,
      Number.parseInt(hex.slice(4, 6), 16) / 255,
    );
  }
}
class FakeColor4 {
  constructor(r, g, b, a) { Object.assign(this, {r, g, b, a}); }
}
class FakeVector3 {
  constructor(x = 0, y = 0, z = 0) { Object.assign(this, {x, y, z}); }
  set(x, y, z) { Object.assign(this, {x, y, z}); return this; }
  copyFrom(other) { return this.set(other.x, other.y, other.z); }
}
const materials = [];
class FakeMaterial {
  constructor(name) { this.name = name; this.disposed = false; this.frozen = false; materials.push(this); }
  freeze() { this.frozen = true; }
  dispose() { this.disposed = true; }
}
const textures = [];
class FakeDynamicTexture {
  constructor(name) { this.name = name; this.disposed = false; textures.push(this); }
  getContext() {
    return {
      clearRect() {}, fillRect() {},
      createRadialGradient() { return {addColorStop() {}}; },
      set fillStyle(_) {},
    };
  }
  update() {}
  dispose() { this.disposed = true; }
}
const particleSystems = [];
class FakeParticleSystem {
  static BLENDMODE_ADD = 1;
  constructor(name, capacity) {
    Object.assign(this, {name, capacity, emitRate: 0, disposed: false, disposeTexture: null});
    particleSystems.push(this);
  }
  start() { this.started = true; }
  stop() { this.stopped = true; }
  dispose(disposeTexture) { this.disposed = true; this.disposeTexture = disposeTexture; }
}
function fakeRibbon(name, options) {
  if (options.instance) return options.instance;
  return {
    name, enabled: true, disposed: false,
    getTotalVertices: () => 24,
    setVerticesData() {},
    isEnabled() { return this.enabled; },
    setEnabled(value) { this.enabled = value; },
    dispose() { this.disposed = true; },
  };
}
globalThis.document = {hidden: false};
globalThis.window = {
  matchMedia: () => ({matches: false}),
  BABYLON: {
    Color3: FakeColor3, Color4: FakeColor4, Vector3: FakeVector3,
    StandardMaterial: FakeMaterial, DynamicTexture: FakeDynamicTexture,
    ParticleSystem: FakeParticleSystem,
    MeshBuilder: {CreateRibbon: fakeRibbon},
    Mesh: {DOUBLESIDE: 2}, VertexBuffer: {ColorKind: 'color'},
  },
};
const entryFor = (id, asset) => ({
  isAlive: true,
  _interpReady: true,
  root: {position: new FakeVector3()},
  bodyMat: {diffuseColor: new FakeColor3(0.4, 0.6, 0.8)},
  botData: {cosmetics: {trail: asset}},
});

const renderer = new trails.TrailRenderer({}, {forceEnabled: true, staticPreview: true});
const entries = new Map(Array.from({length: 30}, (_, index) => [
  `bot-${index}`,
  entryFor(`bot-${index}`, paidAssets[index % paidAssets.length]),
]));
renderer.render(entries, 0.1);
assert.equal(materials.length, 1, '48 possible ribbons must still allocate one material');
assert.equal(materials[0].frozen, true);
assert.equal(textures.length, 1, 'all particle styles must share one procedural texture');
assert.equal(renderer.particleSystemCount, 24, '30 visible cosmetic trails must stop at 24 particle systems');
assert.equal(particleSystems.filter(system => !system.disposed).length, 24);
assert.ok(particleSystems.every(system => system.capacity === 28));
assert.ok(particleSystems.every(system => system.particleTexture === textures[0]));
assert.equal(FakeColor3.parseCount, paidAssets.length * 2,
  'fixed trail colors should be parsed once per style and reused by particles and ribbons');

entries.get('bot-0').botData.cosmetics.trail = 'frost_shards';
renderer.render(entries, 0.1);
assert.equal(renderer.particleSystemCount, 24, 'style replacement must release its old system before replacing it');
assert.equal(particleSystems[0].disposed, true);
assert.equal(particleSystems[0].disposeTexture, false,
  'style replacement must retain the renderer-owned shared texture');
assert.equal(textures[0].disposed, false);

document.hidden = true;
renderer.render(entries, 0.1);
assert.ok(Array.from(renderer.trails.values()).every(trail => !trail.mesh || !trail.mesh.isEnabled()),
  'a hidden document must disable already-built ribbon meshes');
assert.ok(particleSystems.filter(system => !system.disposed).every(system => system.emitRate === 0),
  'a hidden document must zero emission on every live particle system');
document.hidden = false;

renderer.render(new Map(), 0.1);
assert.equal(renderer.particleSystemCount, 0, 'disconnect cleanup must release every per-bot particle system');
assert.ok(particleSystems.every(system => system.disposed),
  'disconnect cleanup must dispose every created per-bot particle system');
assert.ok(particleSystems.every(system => system.disposeTexture === false),
  'all per-bot disposal paths must preserve the shared texture');
assert.equal(textures[0].disposed, false, 'disconnect cleanup must not dispose the shared texture');
renderer.dispose();
assert.equal(textures[0].disposed, true, 'renderer teardown owns final shared-texture disposal');
assert.equal(materials[0].disposed, true, 'renderer teardown owns final shared-material disposal');

const reducedRenderer = new trails.TrailRenderer({}, {
  forceEnabled: true,
  staticPreview: true,
  reducedMotion: true,
});
reducedRenderer.render(new Map([['reduced-bot', entryFor('reduced-bot', 'ember_sparks')]]), 0.1);
assert.equal(reducedRenderer.particleSystemCount, 0,
  'reduced motion must not allocate a particle system for a paid trail');
assert.equal(reducedRenderer.trails.get('reduced-bot').mesh.isEnabled(), true,
  'reduced motion must retain the static ribbon preview');
reducedRenderer.dispose();

const previewPathRenderer = new trails.TrailRenderer({}, {
  forceEnabled: true,
  previewPath: true,
  reducedMotion: true,
});
previewPathRenderer.render(new Map([['preview-bot', entryFor('preview-bot', 'ember_sparks')]]), 0.016);
assert.equal(previewPathRenderer.particleSystemCount, 0,
  'the static reduced-motion preview path must not allocate particles');
assert.equal(previewPathRenderer.trails.get('preview-bot').mesh.isEnabled(), true,
  'the preview must retain a visible paid ribbon before motion begins');
previewPathRenderer.dispose();

const motionRenderer = new trails.TrailRenderer({}, {forceEnabled: true});
const motionEntry = entryFor('moving-bot', 'ember_sparks');
const motionEntries = new Map([['moving-bot', motionEntry]]);
motionRenderer.render(motionEntries, 0.1);
motionEntry.root.position.x = 1;
motionRenderer.render(motionEntries, 0.1);
const movingTrail = motionRenderer.trails.get('moving-bot');
assert.ok(movingTrail.particles.emitRate > 0,
  'a sampled position change must start particle emission');
motionRenderer.render(motionEntries, 0.016);
assert.ok(movingTrail.particles.emitRate > 0,
  'particle emission must persist between 10 Hz movement samples');
motionRenderer.render(motionEntries, 0.1);
assert.equal(movingTrail.particles.emitRate, 0,
  'the next stationary movement sample must stop particle emission');
motionRenderer.dispose();

const priorityRenderer = new trails.TrailRenderer({}, {forceEnabled: true, staticPreview: true});
const priorityEntries = new Map(Array.from({length: 48}, (_, index) => [
  `standard-${index}`,
  entryFor(`standard-${index}`, 'standard'),
]));
priorityEntries.set('paid-last', entryFor('paid-last', 'bounty_flare'));
priorityRenderer.render(priorityEntries, 0.1);
assert.equal(priorityRenderer.trails.size, 48, 'the live ribbon budget must remain capped');
assert.ok(priorityRenderer.trails.has('paid-last'),
  'a paid trail must not be crowded out by earlier free standard wakes');
priorityRenderer.dispose();

const paidPriorityScene = {activeCamera: {target: new FakeVector3(500, 0, 500)}};
const paidPriorityRenderer = new trails.TrailRenderer(paidPriorityScene, {forceEnabled: true, staticPreview: true});
const paidPriorityEntries = new Map(Array.from({length: 49}, (_, index) => {
  const entry = entryFor(`paid-${index}`, paidAssets[index % paidAssets.length]);
  entry.root.position.set(-1000 - index, 0, -1000 - index);
  return [`paid-${index}`, entry];
}));
paidPriorityEntries.get('paid-48').root.position.set(500, 0, 500);
paidPriorityRenderer.render(paidPriorityEntries, 0.1);
assert.equal(paidPriorityRenderer.trails.size, 48, '49 paid trails must retain the same hard ribbon cap');
assert.ok(paidPriorityRenderer.trails.has('paid-48'),
  'a nearby paid trail must not be permanently hidden by paid-bot insertion order');
paidPriorityRenderer.dispose();

console.log('24 cosmetic trails resolve locally with bounded ribbon and particle lifecycle contracts');
