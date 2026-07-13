import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const html = readFileSync(new URL('../frontend/shop/index.html', import.meta.url), 'utf8');
const preview = readFileSync(new URL('../frontend/js/shop-preview.js', import.meta.url), 'utf8');
const shop = readFileSync(new URL('../frontend/js/cosmetics-shop.js', import.meta.url), 'utf8');
const css = readFileSync(new URL('../frontend/css/shop.css', import.meta.url), 'utf8');

const weapons = ['sword', 'bow', 'spear', 'daggers', 'staff', 'shield', 'grapple'];
assert.match(html, /<fieldset[^>]*data-shop-chassis-picker/,
  'the showroom needs one keyboard-native chassis choice group');
for (const weapon of weapons) {
  assert.match(html, new RegExp(`type="radio"[^>]+value="${weapon}"|value="${weapon}"[^>]+type="radio"`),
    `the showroom is missing the ${weapon} chassis`);
}
assert.match(preview, /setCharacter\s*\(/,
  'the isolated preview must support swapping its active chassis');
assert.match(preview, /const SHOP_FRAME_INTERVAL_MS = 1000 \/ 30;/,
  'the continuously rotating showroom must be capped at 30 FPS');
assert.match(preview, /const onDemand = this\._reducedMotion \|\| !this\.autoRotate;[\s\S]{0,100}onDemand && !this\._renderRequested/,
  'reduced-motion previews should render only after an explicit interaction or selection');
assert.match(preview, /disposeBotCosmetics\(this\.entry\)[\s\S]{0,220}disposeBotEntry\(this\.entry\)/,
  'chassis swaps must dispose cosmetic clones before the old model');
assert.match(shop, /data-shop-chassis-picker/);
assert.match(shop, /\.setCharacter\(/,
  'the showroom controller must send the selected chassis to the preview');
assert.match(css, /\.shop-chassis-picker/,
  'the seven-way picker needs a responsive, visible layout');
assert.match(css, /\.shop-chassis-option[^}]*:focus-within|\.shop-chassis-option:has\([^)]*:focus-visible/,
  'keyboard focus must remain visible around each chassis choice');

console.log('the Shop exposes all seven accessible Forge chassis and swaps one disposable preview model');
