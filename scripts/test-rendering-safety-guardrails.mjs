import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const engine = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
const settings = readFileSync(new URL('../frontend/js/settings.js', import.meta.url), 'utf8');

assert.match(
  settings,
  /glowLayer:\s*\{[^}]*defaultOff:\s*true/,
  'the extra neon GlowLayer must stay opt-in until it has been visually signed off',
);

const ratio = Number(engine.match(/mainTextureRatio:\s*([0-9.]+)/)?.[1]);
const intensity = Number(engine.match(/glow\.intensity\s*=\s*([0-9.]+)/)?.[1]);
assert.ok(Number.isFinite(ratio) && ratio <= 0.5,
  `the opt-in GlowLayer must remain half-resolution or lower (found ${ratio})`);
assert.ok(Number.isFinite(intensity) && intensity <= 0.75,
  `the opt-in GlowLayer intensity must remain bounded (found ${intensity})`);

console.log('rendering safety guardrails keep neon glow opt-in, bounded, and half-resolution');
