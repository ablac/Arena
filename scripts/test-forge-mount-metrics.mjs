import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const source = readFileSync(new URL('../frontend/js/renderer/character-rig.js', import.meta.url), 'utf8');
const required = [
  'bodyY', 'torsoWidth', 'torsoHeight', 'torsoDepth', 'shoulderX',
  'headWidth', 'headHeight', 'headDepth',
];

assert.match(source, /const mountMetrics = Object\.freeze\(\{/,
  'Forge mount geometry must expose an immutable metric contract');
for (const metric of required) {
  assert.match(source, new RegExp(`mountMetrics[\\s\\S]{0,420}\\b${metric}\\b`),
    `Forge mount metrics are missing ${metric}`);
}
assert.match(source, /mounts,[\s\S]{0,100}mountMetrics,/,
  'every Forge entry must return mount metrics beside its semantic mounts');

console.log('Forge entries expose stable chassis-local metrics for semantic cosmetics');
