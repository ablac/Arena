import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('../frontend/js/cosmetic-themes.js', import.meta.url), 'utf8');
const sandbox = {};
vm.runInNewContext(source, sandbox, {filename: 'cosmetic-themes.js'});

const themes = sandbox.ArenaCosmeticThemes;
assert.ok(themes, 'the global cosmetic theme helper should be installed');

const parsed = themes.parseAssetKey('arena_set_003_ember_signal');
assert.deepEqual(
  JSON.parse(JSON.stringify(parsed)),
  {key: 'arena_set_003_ember_signal', number: 3, slug: 'ember_signal'},
);

const first = themes.themeFor('arena_set_003_ember_signal');
const repeated = themes.themeFor('arena_set_003_ember_signal');
assert.deepEqual(first, repeated, 'the same key must always derive the same recipe');
assert.equal(first, repeated, 'theme recipes should be cached across renderer ticks');
assert.equal(first.key, 'arena_set_003_ember_signal');
assert.match(first.palette.primary, /^#[0-9a-f]{6}$/);
assert.match(first.palette.accent, /^#[0-9a-f]{6}$/);
assert.ok(['bands', 'plates', 'chevrons', 'core'].includes(first.skin.pattern));
assert.ok(['halo', 'antenna', 'crown', 'orbitals', 'fins', 'reactor'].includes(first.attachment.kind));

for (const malformed of [
  '',
  'arena_set_3_ember',
  'arena_set_000_ember',
  'arena_set_1000_ember',
  'arena_set_003_Ember',
  'arena_set_003_ember signal',
  'https://example.com/model.glb',
  `arena_set_003_${'x'.repeat(100)}`,
]) {
  assert.equal(themes.parseAssetKey(malformed), null, `malformed key should be rejected: ${malformed}`);
  assert.equal(themes.themeFor(malformed), null, `malformed key must not produce a recipe: ${malformed}`);
}

const swatch = themes.swatchStyle('arena_set_100_void_orbit');
assert.match(swatch, /^linear-gradient\(/);
assert.doesNotMatch(swatch, /url\(|javascript:|https?:/i, 'swatches must stay local and data-only');
for (const trail of [
  'ember_sparks', 'frost_shards', 'ion_stream', 'plasma_ribbon',
  'void_motes', 'solar_wake', 'lunar_dust', 'comet_tail',
  'nebula_pulse', 'storm_arcs', 'static_glitch', 'pixel_scatter',
  'data_stream', 'holo_prism', 'toxic_spores', 'verdant_leaves',
  'sand_wake', 'magma_cinders', 'ocean_spray', 'gilded_dust',
  'rune_sparks', 'phantom_smoke', 'gear_sparks', 'bounty_flare',
]) {
  assert.match(themes.swatchStyle(trail), /^linear-gradient\(/,
    `paid trail ${trail} needs a local Shop and Dashboard swatch`);
}
assert.equal(themes.swatchStyle('malformed'), '', 'invalid keys must not produce CSS');

console.log('procedural cosmetic themes are deterministic, bounded, and reject malformed keys');
