import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const html = readFileSync(new URL('../frontend/index.html', import.meta.url), 'utf8');
const css = readFileSync(new URL('../frontend/css/site-shell.css', import.meta.url), 'utf8');
const js = readFileSync(new URL('../frontend/js/site-shell.js', import.meta.url), 'utf8');
const app = readFileSync(new URL('../frontend/js/app.js', import.meta.url), 'utf8');

const matches = (source, pattern) => Array.from(source.matchAll(pattern));
const ids = matches(html, /\sid="([^"]+)"/g).map((match) => match[1]);
const idSet = new Set(ids);

assert.equal(ids.length, idSet.size, 'site shell must not contain duplicate IDs');
for (const id of [
  'arena',
  'arena-shell',
  'arena-canvas',
  'hud-round',
  'hud-killfeed',
  'hud-players',
  'hud-lobby',
  'ws-status',
  'zoom-slider',
  'zoom-value',
  'follow-bot',
  'auto-pan',
  'fullscreen-btn',
  'arena-tabs',
]) {
  assert(idSet.has(id), `existing renderer/app contract #${id} must be preserved`);
}
assert.equal(matches(html, /id="arena-canvas"/g).length, 1, 'the shell must own exactly one persistent arena canvas');

assert.match(html, /css\/site-shell\.css\?v=/, 'responsive shell stylesheet must be loaded');
assert.match(html, /js\/service-status\.js/, 'public service-status client must be loaded');
assert.match(html, /js\/site-shell\.js\?v=/, 'responsive shell controller must be loaded');
assert.match(html, /name="viewport"\s+content="width=device-width, initial-scale=1\.0, viewport-fit=cover"/, 'layout must be viewport and safe-area driven');
assert.doesNotMatch(html, /mobile-suggest|href="m\/"/, 'the main site must not hand phones to a separate page');
assert.match(html, /id="fullscreen-btn"[^>]*aria-pressed="false">Cinema Mode<\/button>/, 'shell action must be presented as Cinema Mode before JS boots');
assert.doesNotMatch(app, /pointer:\s*coarse|location\.href\s*=\s*appPath\('\/m\/'\)/, 'legacy coarse-pointer mobile redirect must be removed');

assert.match(css, /height:\s*100dvh/, 'shell must use the dynamic mobile viewport');
assert.match(css, /safe-area-inset-top/, 'shell must respect top safe area');
assert.match(css, /safe-area-inset-bottom/, 'shell must respect bottom safe area');
assert.match(css, /@media\s*\(max-width:\s*768px\),\s*\(max-height:\s*620px\)/, 'shell must use the mobile layout for narrow phones and short Android landscape screens');
assert.doesNotMatch(css, /max-height:\s*620px\)\s*and\s*\(min-width:\s*769px/, 'short screens must not be forced back into the desktop shell');
assert.doesNotMatch(css, /pointer:\s*(coarse|fine)/, 'mobile layout must not depend on pointer/UA heuristics');
assert.match(css, /\.arena-section[\s\S]*height:\s*100dvh/, 'the live arena must fill the first viewport');
assert.match(css, /\.site-command-dock\.is-open[\s\S]*translateY\(0\)/, 'mobile navigation must open as a bottom sheet');
assert.match(css, /\.arena-shell\.telemetry-open\s+\.arena-sidebar/, 'mobile match telemetry must be secondary and dismissible');
assert.match(css, /body\.site-cinema-mode/, 'cinema mode must hide shell chrome');
assert.match(css, /overflow:\s*hidden/, 'root shell must prevent accidental page overflow');

assert.match(js, /stopImmediatePropagation\(\)/, 'cinema control must supersede the legacy mobile redirect');
assert.match(js, /site-cinema-mode/, 'cinema control must toggle shell chrome only');
assert.match(js, /requestArenaResize/, 'layout transitions must resize the existing renderer');
assert.doesNotMatch(js, /new\s+ArenaEngine|replaceChild|removeChild|location\.(?:href|assign)|\/m\//, 'shell controller must never recreate or redirect the arena');
assert.match(js, /event\.key\s*!==\s*'Escape'/, 'sheets must support Escape');
assert.match(js, /event\.key\s*!==\s*'Tab'/, 'modal drawers must trap keyboard focus');
assert.match(js, /aria-expanded/, 'interactive sheets must publish expanded state');
assert.match(js, /initServiceStatus\(\)/, 'public service status must initialize in the shell');

assert.match(html, /id="siteBroadcast"[\s\S]*data-service-status-root[\s\S]*data-service-status[\s\S]*data-service-status-dismiss/, 'public broadcast host contract must be present');
assert.match(html, /id="siteBroadcast"[\s\S]*role="status"[\s\S]*aria-live="polite"/, 'public notices must be announced accessibly');

const keith = html.indexOf('>Keith S<');
const andrew = html.indexOf('>Andrew<');
const will = html.indexOf('>Will<');
assert(keith >= 0 && keith < andrew && andrew < will, 'builder credits must list Keith S, Andrew, then Will');
assert.match(html, /href="https:\/\/www\.linkedin\.com\/in\/keith-swoger"/, 'Keith S LinkedIn must be linked');
assert.match(html, /href="https:\/\/www\.linkedin\.com\/in\/andrew-demczuk-4038a217\/"/, 'Andrew LinkedIn must remain linked');
assert.match(html, /<strong>Will<\/strong><small>Details coming soon<\/small>/, 'Will must have a future-details placeholder');
assert.match(html, /href="https:\/\/angel-serv\.com\/"/, 'Contact must link to Angel-Serv.com');
assert.match(html, /href="mailto:Hello@angel-serv\.com"/, 'Contact must link to Hello@angel-serv.com');

for (const match of matches(html, /aria-labelledby="([^"]+)"/g)) {
  for (const labelledBy of match[1].split(/\s+/)) {
    assert(idSet.has(labelledBy), `aria-labelledby must resolve #${labelledBy}`);
  }
}
for (const match of matches(html, /data-overlay-open="([^"]+)"/g)) {
  assert(idSet.has(match[1]), `overlay trigger must resolve #${match[1]}`);
}
for (const match of matches(html, /data-close-overlay="([^"]+)"/g)) {
  assert(idSet.has(match[1]), `overlay close control must resolve #${match[1]}`);
}

const overlays = matches(html, /<div class="[^"]*onboarding-overlay[^"]*" id="([^"]+)"/g).map((match) => match[1]);
for (const overlay of overlays) {
  assert.match(html, new RegExp(`class="drawer-close" data-close-overlay="${overlay}"`), `${overlay} must have a visible close button`);
}

console.log('responsive arena-first site shell checks passed');
