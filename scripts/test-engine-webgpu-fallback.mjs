import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
const isolatedSource = source.replace(/import[\s\S]*?from '[^']+';\r?\n/g, '');
const moduleURL = `data:text/javascript;base64,${Buffer.from(isolatedSource).toString('base64')}`;
const { webGPUAvailableWithin } = await import(moduleURL);

assert.match(
  source,
  /new B\.Engine\(this\.canvas, false, \{[\s\S]{0,420}stencil:\s*true/,
  'the WebGL fallback must enable stencil for the pickup HighlightLayer',
);

assert.equal(typeof webGPUAvailableWithin, 'function',
  'the renderer must expose one bounded WebGPU capability probe');

const neverSettles = new Promise(() => {});
const startedAt = performance.now();
const timedOut = await webGPUAvailableWithin({
  WebGPUEngine: { IsSupportedAsync: neverSettles },
}, 10);
const elapsed = performance.now() - startedAt;

assert.equal(timedOut, false, 'an unresolved WebGPU probe must fall back to WebGL');
assert.ok(elapsed < 250, `WebGPU fallback exceeded its bounded test window (${elapsed.toFixed(1)}ms)`);
assert.equal(await webGPUAvailableWithin({
  WebGPUEngine: { IsSupportedAsync: Promise.resolve(true) },
}, 100), true);

console.log('WebGPU capability probing is bounded and falls back to WebGL when the browser stalls');
