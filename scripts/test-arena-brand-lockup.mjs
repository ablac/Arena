import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const root = new URL('../', import.meta.url);
const stylesheet = readFileSync(new URL('frontend/css/brand-lockup.css', root), 'utf8');
const surfaces = [
  {name: 'public Arena', path: 'frontend/index.html', cssHref: 'css/brand-lockup.css'},
  {name: 'Cosmetic Shop', path: 'frontend/shop/index.html', cssHref: '../css/brand-lockup.css'},
  {name: 'Dashboard', path: 'frontend/dashboard/index.html', cssHref: '../css/brand-lockup.css'},
  {name: 'mobile spectator', path: 'frontend/m/index.html', cssHref: '../css/brand-lockup.css'},
];

for (const surface of surfaces) {
  const html = readFileSync(new URL(surface.path, root), 'utf8');
  const brandAnchor = html.match(/<a\b[^>]*class="[^"]*\barena-brand\b[^"]*"[^>]*>[\s\S]*?<\/a>/)?.[0] || '';

  assert.ok(brandAnchor, `${surface.name} should expose the shared Arena brand lockup`);
  assert.match(brandAnchor, /href="https:\/\/angel-serv\.com\/"/,
    `${surface.name} brand should link to Angel Software Solutions`);
  assert.match(brandAnchor, /target="_blank"/,
    `${surface.name} external brand link should not replace the Arena session`);
  assert.match(brandAnchor, /rel="noopener"/,
    `${surface.name} external brand link should isolate the opener`);
  assert.match(brandAnchor, /aria-label="Angel Software Solutions home"/,
    `${surface.name} brand should have a stable accessible name`);
  assert.match(brandAnchor, /<span class="arena-brand-name">Angel Software Solutions<\/span>/);
  assert.match(brandAnchor, /<span class="arena-brand-product">THE ARENA<\/span>/);
  assert.doesNotMatch(brandAnchor, /site-brand-mark|brand-mark|AI Battle Arena/,
    `${surface.name} lockup should not retain the old dot or visible product name`);

  const brandCSSIndex = html.lastIndexOf(`href="${surface.cssHref}`);
  const lastStylesheetIndex = html.lastIndexOf('rel="stylesheet"');
  assert.ok(brandCSSIndex >= 0, `${surface.name} should load the shared brand stylesheet`);
  assert.ok(brandCSSIndex > lastStylesheetIndex,
    `${surface.name} should load brand-lockup.css after its existing stylesheets`);
}

const publicHTML = readFileSync(new URL('frontend/index.html', root), 'utf8');
assert.match(publicHTML, /aria-label="Live AI Battle Arena"/,
  'descriptive Arena copy should remain after replacing the top-left brand');
assert.match(publicHTML, /id="about-title">AI Battle Arena<\/h3>/,
  'the product description may still use AI Battle Arena outside the brand lockup');

assert.match(stylesheet, /\.arena-brand\s*\{/);
assert.match(stylesheet, /\.arena-brand-name\s*\{/);
assert.match(stylesheet, /\.arena-brand-product\s*\{/);
assert.match(stylesheet, /\.arena-brand:focus-visible\s*\{/,
  'the linked brand needs a visible keyboard focus treatment');
assert.match(stylesheet, /\.dashboard-embedded\s+\.dashboard-arena-brand\s*\{/,
  'embedded Dashboard views should not duplicate the parent Arena brand');
assert.match(stylesheet, /\.dashboard-arena-brand\s*\{[^}]*position:\s*absolute/s,
  'the Dashboard brand must scroll with the document instead of covering outfitter content');
assert.doesNotMatch(stylesheet, /\.dashboard-arena-brand\s*\{[^}]*position:\s*fixed/s,
  'a fixed Dashboard brand overlaps auto-scrolled inventory and preview controls');
assert.match(stylesheet, /body:not\(\.dashboard-embedded\)\s+#app\s*\{[^}]*padding-top:/s,
  'standalone Dashboard content must reserve the brand lockup height');
assert.match(stylesheet, /#tb-brand\.arena-brand\s*\{[^}]*display:\s*inline-grid/s,
  'the mobile ID selector must preserve the two-line grid over its legacy dot layout');
assert.match(stylesheet, /@media\s*\(max-width:\s*420px\)/,
  'the two-line lockup needs an explicit narrow-screen treatment');

console.log('Arena brand lockup is consistent across public, Shop, Dashboard, and mobile surfaces');
