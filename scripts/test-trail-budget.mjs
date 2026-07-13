import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const source = readFileSync(new URL('../frontend/js/renderer/trails.js', import.meta.url), 'utf8');

assert.match(source, /const SAMPLE_INTERVAL = 0\.1(?:0+)?;/,
  'movement trails should sample at the 10 Hz authoritative state cadence');
assert.match(source, /const MAX_RENDERED_TRAILS = 48;/,
  'large rosters need a hard streaming-geometry budget');
assert.match(source, /renderedTrails\s*>=\s*MAX_RENDERED_TRAILS/,
  'the render pass must enforce the budget before allocating a new trail');
assert.doesNotMatch(source, /ribbon\.alwaysSelectAsActiveMesh\s*=\s*true/,
  'dynamic ribbons should retain normal frustum culling');
assert.match(source,
  /history\.length < 2[\s\S]{0,120}mesh\.setEnabled\(false\)[\s\S]{0,100}continue;[\s\S]{0,320}mesh\.setEnabled\(true\)/,
  'a resume reset must keep stale ribbon geometry hidden until a fresh second sample exists');

console.log('movement trails use server-cadence sampling, culling, and a bounded large-roster budget');
