import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const html = readFileSync(new URL('../frontend/index.html', import.meta.url), 'utf8');
const css = readFileSync(new URL('../frontend/css/site-shell.css', import.meta.url), 'utf8');
const sectionsCss = readFileSync(new URL('../frontend/css/sections.css', import.meta.url), 'utf8');
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

const mobileMediaStart = css.indexOf('@media (max-width: 768px), (max-height: 620px)');
const mobileMediaEnd = css.indexOf('@media (max-width: 420px)', mobileMediaStart);
assert(mobileMediaStart >= 0 && mobileMediaEnd > mobileMediaStart, 'mobile shell rules must have a bounded source block');
const mobileCss = css.slice(mobileMediaStart, mobileMediaEnd);
assert.match(
  mobileCss,
  /\.arena-sidebar,\s*\.arena-shell\.is-floating \.arena-sidebar\s*\{[^}]*grid-template-rows:\s*auto auto minmax\(0,\s*1fr\)/,
  'mobile telemetry must reserve stable intro, tab, and scrollable-content rows',
);
assert.match(
  mobileCss,
  /\.hud-round\.is-collapsed\s*\{[^}]*top:\s*calc\(var\(--shell-safe-top\) \+ var\(--shell-header-height\) \+ 6px\)[^}]*max-width:\s*calc\(100% - 240px\)/,
  'the compact mobile spectator HUD must align with and reserve room for the live-feed controls',
);
assert.match(
  mobileCss,
  /\.hud-round\.is-collapsed \.hud-compact-title\s*\{[^}]*text-overflow:\s*ellipsis[^}]*white-space:\s*nowrap/,
  'long compact HUD labels must truncate instead of covering live-feed controls',
);
assert.match(
  mobileCss,
  /\.arena-tab-panel\s*\{[^}]*height:\s*100%/,
  'mobile telemetry panels must fill the bounded content track',
);
assert.match(
  mobileCss,
  /\.site-command-dock\.onboarding-rail-stack,\s*\.onboarding-drawer-scroll,\s*\.onboarding-flow-shell-scroll\s*\{[^}]*-webkit-overflow-scrolling:\s*touch[^}]*touch-action:\s*pan-y/,
  'mobile menu and drawer scroll owners must support direct touch panning',
);
assert.match(
  mobileCss,
  /\.overlay-standings \.onboarding-drawer-scroll\s*\{[^}]*overflow-y:\s*auto[^}]*touch-action:\s*pan-y/,
  'mobile Standings must expose one reachable outer vertical scroller',
);
assert.match(
  mobileCss,
  /\.overlay-standings \.onboarding-flow-standings,[\s\S]*?\.overlay-standings \.leaderboard-widget-overlay\s*\{[^}]*height:\s*auto[^}]*min-height:\s*0[^}]*flex:\s*none/,
  'mobile Standings must release the desktop full-height chain',
);
assert.match(
  mobileCss,
  /\.overlay-standings \.leaderboard-scroll-inner\s*\{[^}]*height:\s*auto[^}]*overflow-y:\s*visible[^}]*overflow-x:\s*auto/,
  'mobile Standings tables must expand into the outer vertical scroller while retaining horizontal scroll',
);
assert.match(
  mobileCss,
  /\.overlay-standings \.leaderboard-scroll-inner\s*\{[^}]*overscroll-behavior-x:\s*contain[^}]*overscroll-behavior-y:\s*auto/,
  'wide Standings tables must pass vertical touch gestures to the outer drawer',
);
assert.match(
  sectionsCss,
  /\.arena-tab-content\s*\{[^}]*min-height:\s*0[^}]*overflow:\s*hidden/,
  'telemetry content must stay inside its grid track',
);
assert.match(
  sectionsCss,
  /\.arena-tab-panel\s*\{[^}]*height:\s*100%[^}]*overflow-y:\s*auto/,
  'each telemetry panel must scroll without resizing the tabs',
);

assert.match(js, /stopImmediatePropagation\(\)/, 'cinema control must supersede the legacy mobile redirect');
assert.match(js, /site-cinema-mode/, 'cinema control must toggle shell chrome only');
assert.match(js, /requestArenaResize/, 'layout transitions must resize the existing renderer');
assert.doesNotMatch(js, /new\s+ArenaEngine|replaceChild|removeChild|location\.(?:href|assign)|\/m\//, 'shell controller must never recreate or redirect the arena');
assert.match(js, /event\.key\s*!==\s*'Escape'/, 'sheets must support Escape');
assert.match(js, /event\.key\s*!==\s*'Tab'/, 'modal drawers must trap keyboard focus');
assert.match(js, /aria-expanded/, 'interactive sheets must publish expanded state');
assert.match(js, /initServiceStatus\(\)/, 'public service status must initialize in the shell');
assert.match(js, /if\s*\(open\)\s*\{[^}]*dock\.scrollTop\s*=\s*0/, 'the mobile command menu must reset to its first item whenever it opens');
assert.match(
  app,
  /const scrollRoot\s*=\s*drawer\.querySelector\('\.onboarding-flow-shell-scroll'\)\s*\|\|\s*drawer\.querySelector\('\.onboarding-drawer-scroll'\)\s*\|\|\s*drawer/,
  'overlays must reset and target their real scroll owner',
);
assert.match(app, /scrollRoot\.scrollTo\(\{ top:\s*0, behavior:\s*'smooth' \}\)/, 'opening an overlay must reset its scroll owner');

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
