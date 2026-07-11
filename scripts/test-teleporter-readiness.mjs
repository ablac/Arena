import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const source = await readFile(new URL('../frontend/js/renderer/gameplay.js', import.meta.url), 'utf8');

assert.match(source, /const ready = pad\.is_ready !== false;/, 'renderer must use server readiness');
assert.match(source, /const blend = ready \? 1 : 0;/, 'locked pads must not brighten during cooldown');
assert.match(source, /entry\.beam\.emitRate = ready \? 16 : 0;/, 'locked pad beam must be off');
assert.match(source, /entry\.swirl\.emitRate = ready \? 12 : 0;/, 'locked pad swirl must be off');
assert.match(source, /entry\.beam\.stop\(\);[\s\S]*entry\.swirl\.stop\(\);/, 'locked transition must stop particles');
assert.doesNotMatch(source, /1\s*-\s*cooldown\s*\/\s*30/, 'cooldown must not visually re-light an unready pad');

console.log('teleporter readiness renderer checks passed');
