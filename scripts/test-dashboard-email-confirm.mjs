import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const root = new URL('../', import.meta.url);
const dashboard = readFileSync(new URL('frontend/dashboard/index.html', root), 'utf8');
const confirmPage = readFileSync(new URL('frontend/dashboard/confirm.html', root), 'utf8');

// A magic-link email always opens a fresh top-level tab -- the browser's
// call, not something the site can prevent. The standalone Dashboard must
// hand that tab off to the minimal confirm page instead of loading the full
// Dashboard (or the live Arena) just to show a "click to continue" button.
const redirectStart = dashboard.indexOf("if (hash.indexOf('email_token=')");
const redirectEnd = dashboard.indexOf('}', redirectStart);
const redirectSource = dashboard.slice(redirectStart, redirectEnd);
assert.match(redirectSource, /\/dashboard\/confirm\.html/, 'the standalone Dashboard must redirect the fresh tab to the minimal confirm page');
assert.match(redirectSource, /\/arena\/dashboard\/confirm\.html/, 'the redirect must respect the /arena mount prefix');
assert.match(redirectSource, /\+ hash\)/, 'the email token must still travel as a hash fragment, never a query param');
assert.doesNotMatch(redirectSource, /window\.location\.replace\(\(mounted \? '\/arena\/' : '\/'\) \+ hash\)/,
  'the old handoff to the full Arena root page must be gone -- confirm.html now owns this');

assert.match(confirmPage, /<meta name="robots" content="noindex, nofollow">/, 'a one-time confirm link has no reason to be indexed');
assert.doesNotMatch(confirmPage, /fonts\.googleapis\.com|cdn\.babylonjs\.com/,
  'this page must stay minimal and fast -- no external font/CDN dependency for a one-click confirmation');

assert.match(confirmPage, /id="confirmState"/);
assert.match(confirmPage, /id="doneState"[^>]*hidden/);
assert.match(confirmPage, /id="missingState"[^>]*hidden/);
assert.match(confirmPage, /id="confirmButton"/);
assert.match(confirmPage, /Continue signing in/);

// The token must be stripped from the visible URL before any network
// request, matching takeCustomerEmailTokenFromHash's pattern elsewhere in
// the dashboard (see dashboard/index.html) -- a token left in the URL bar
// or browser history is a token an attacker with local access could reuse.
assert.match(confirmPage, /history\.replaceState\(null, '', cleanURL\)/);
assert.match(confirmPage, /params\.get\('email_token'\)/);

// No token in the hash must show a dead-end state, not an infinite loading
// spinner or a broken form.
assert.match(confirmPage, /if \(!token\) \{/);
assert.match(confirmPage, /missingStateEl\.hidden = false/);

assert.match(confirmPage, /apiPath\('\/account\/email\/verify'\)/, 'must hit the real customer email verification endpoint');
assert.match(confirmPage, /method: 'POST'/);
assert.match(confirmPage, /credentials: 'same-origin'/);
assert.doesNotMatch(confirmPage, /X-CSRF-Token/,
  'pre-auth verification uses same-origin enforcement server-side, not an unavailable session CSRF token');

// This is the other half of the actual fix: once verified, any already-open
// Arena tab must be told to pick up the new session itself, since this tab
// is just going to be closed.
assert.match(confirmPage, /notifySessionChanged/);
assert.match(confirmPage, /account-session\.js/);

// Closing is opportunistic (browsers only allow it for a script-openable
// window, which a fresh email-link tab often but not always is) -- the
// fallback message must always be correct even when the close is a no-op.
assert.match(confirmPage, /window\.close\(\)/);
assert.match(confirmPage, /close this window/i);

console.log('the standalone Dashboard hands a fresh email-link tab off to a minimal confirm-and-close page');
