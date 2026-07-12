import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const desktopHTML = readFileSync(new URL('../frontend/index.html', import.meta.url), 'utf8');
const mobileHTML = readFileSync(new URL('../frontend/m/index.html', import.meta.url), 'utf8');
const appSource = readFileSync(new URL('../frontend/js/app.js', import.meta.url), 'utf8');
const mobileSource = readFileSync(new URL('../frontend/m/mobile.js', import.meta.url), 'utf8');
const engineSource = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
const botsSource = readFileSync(new URL('../frontend/js/renderer/bots.js', import.meta.url), 'utf8');
const botBodySource = readFileSync(new URL('../frontend/js/renderer/bot-body.js', import.meta.url), 'utf8');
const swordsmanBodySource = readFileSync(new URL('../frontend/js/renderer/swordsman-body.js', import.meta.url), 'utf8');
const swordsmanAnimsSource = readFileSync(new URL('../frontend/js/renderer/swordsman-anims.js', import.meta.url), 'utf8');

assert.ok(desktopHTML.indexOf('js/cosmetic-themes.js') < desktopHTML.indexOf('js/app.js'),
  'desktop must load procedural themes before the live renderer module chain');
assert.ok(mobileHTML.indexOf('../js/cosmetic-themes.js') < mobileHTML.indexOf('mobile.js'),
  'mobile must load procedural themes before its shared renderer');
assert.match(mobileHTML, /data-mobile-cosmetic-shop[^>]+href="\.\.\/shop\/"/,
  'mobile spectator must open the dedicated Shop');
assert.doesNotMatch(appSource, /initCosmeticsPanel|cosmetics-panel\.js/,
  'the live Arena must not retain the replaced embedded catalog');

assert.match(appSource, /renderer\/engine\.js\?v=20260712a/);
assert.match(mobileSource, /renderer\/engine\.js\?v=20260712a/);
assert.match(engineSource, /bots\.js\?v=20260712a/);
assert.match(botsSource, /bot-body\.js\?v=20260712a/);
assert.match(botsSource, /swordsman-anims\.js\?v=20260712a/);
assert.match(botsSource, /cosmetics\.js\?v=20260712a/);
assert.match(botBodySource, /swordsman-body\.js\?v=20260712a/);
assert.match(swordsmanBodySource, /bot-body\.js\?v=20260712a/);
assert.match(swordsmanBodySource, /swordsman-anims\.js\?v=20260712a/);
assert.match(swordsmanAnimsSource, /swordsman-body\.js\?v=20260712a/);

console.log('live renderers and dedicated Shop share one fresh cosmetic module identity');
