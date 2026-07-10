import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('../frontend/dashboard/account-cosmetics.js', import.meta.url), 'utf8');
const sandbox = {};
vm.runInNewContext(source, sandbox, {filename: 'account-cosmetics.js'});
const cosmetics = sandbox.ArenaAccountCosmetics;

assert.ok(cosmetics, 'dashboard cosmetics helpers should attach to globalThis');
assert.equal(cosmetics.normalizeSession({authenticated:false, account:{id:'acct-1',email:'owner@example.com',email_verified:true}}).authenticated, false, 'an explicit logged-out response must never be inferred as authenticated');
const verifiedSession = cosmetics.normalizeSession({authenticated:true, login_enabled:true, login_url:'/api/v1/dashboard/login', account:{id:'acct-1',email:'owner@example.com',email_verified_at:'2026-07-10T12:00:00Z'}});
assert.equal(verifiedSession.account.email_verified, true, 'a server verification timestamp should authorize the account UX');
assert.equal(verifiedSession.login_enabled, true);
assert.equal(verifiedSession.login_url, '/api/v1/dashboard/login');
assert.equal(cosmetics.accountRoute('session'), '/account/session');
assert.equal(cosmetics.accountRoute('assignment', 'license/a'), '/account/cosmetic-licenses/license%2Fa/assignment');
assert.equal(cosmetics.accountRoute('equip', 'bot/a'), '/account/bots/bot%2Fa/cosmetics');
assert.deepEqual(
  JSON.parse(JSON.stringify(cosmetics.requestHeaders('PUT', 'csrf-value', true))),
  {Accept:'application/json','Content-Type':'application/json','X-CSRF-Token':'csrf-value'},
);
assert.equal(cosmetics.requestHeaders('GET', 'csrf-value', false)['X-CSRF-Token'], undefined, 'CSRF token should only be sent on mutations');

const snapshot = cosmetics.normalizeSnapshot({
  account: {id: 'acct-1', email: 'Owner@Example.COM ', email_verified: true},
  bots: [
    {id: 'bot-1', name: 'Alpha', key_prefix: 'arena_alpha'},
    {id: 'bot-2', name: 'Beta', key_prefix: 'arena_beta'},
  ],
  licenses: [
    {
      id: 'license-1',
      cosmetic_id: 'skin-neon-grid',
      item: {id: 'skin-neon-grid', name: 'Neon <Grid>', slot: 'bot_skin', rarity: 'rare'},
      assigned_bot_id: 'bot-1',
      equipped: true,
    },
    {
      id: 'license-2',
      cosmetic_id: 'skin-neon-grid',
      item: {id: 'skin-neon-grid', name: 'Neon <Grid>', slot: 'bot_skin', rarity: 'rare'},
      assigned_bot_id: 'bot-2',
    },
  ],
});

assert.equal(snapshot.account.email, 'owner@example.com');
assert.equal(snapshot.licenses.length, 2, 'multiple purchased copies must remain separate licenses');

assert.deepEqual(
  JSON.parse(JSON.stringify(cosmetics.assignmentIntent(snapshot, 'license-1', 'bot-2'))),
  {ok: true, kind: 'move', license_id: 'license-1', bot_id: 'bot-2', previous_bot_id: 'bot-1'},
);
assert.equal(cosmetics.assignmentIntent(snapshot, 'license-1', 'bot-1').reason, 'already-assigned');
assert.equal(cosmetics.assignmentIntent(snapshot, 'license-2', 'bot-missing').reason, 'bot-not-linked');

const inactiveState = cosmetics.normalizeSnapshot({
  account: {id:'acct-1',email:'owner@example.com',email_verified:true},
  bots: [{id:'bot-inactive',name:'Dormant',key_is_active:false}],
  licenses: [{id:'license-refunded',status:'refunded',item:{id:'skin-neon-grid',name:'Neon Grid',slot:'bot_skin'}}],
});
assert.equal(cosmetics.assignmentIntent(inactiveState, 'license-refunded', 'bot-inactive').reason, 'license-inactive');
inactiveState.licenses[0].status = 'active';
assert.equal(cosmetics.assignmentIntent(inactiveState, 'license-refunded', 'bot-inactive').reason, 'bot-key-inactive');
const inactiveHTML = cosmetics.renderPanel({...inactiveState, licenses:[{...inactiveState.licenses[0],status:'refunded'}]}, {});
assert.match(inactiveHTML, /Refunded/);
assert.match(inactiveHTML, /Key inactive/);
assert.doesNotMatch(inactiveHTML, /data-license-equip="license-refunded"/);
const inactiveBotHTML = cosmetics.renderPanel({
  ...inactiveState,
  licenses:[{...inactiveState.licenses[0],status:'active',assigned_bot_id:'bot-inactive',equipped:false}],
}, {});
assert.match(inactiveBotHTML, /data-license-equip="license-refunded" disabled>Bot key inactive/);

const unverified = structuredClone(snapshot);
unverified.account.email_verified = false;
assert.equal(cosmetics.assignmentIntent(unverified, 'license-2', 'bot-2').reason, 'verified-email-required');

const html = cosmetics.renderPanel(snapshot, {});
assert.match(html, /Purchases stay with this account even if a bot API key is rotated, revoked, or lost/);
assert.match(html, /Each license can be assigned to one bot at a time/);
assert.match(html, /Equipped on/);
assert.match(html, /Assigned to <strong>Alpha \(arena_alpha\.\.\.\)<\/strong>/);
assert.match(html, /Assigned, not equipped/);
assert.match(html, /data-license-equip="license-2"/);
assert.match(html, /data-license-id="license-1"/);
assert.match(html, /data-license-id="license-2"/);
assert.match(html, /Move to bot/);
assert.match(html, /Alpha - arena_alpha\.\.\./, 'duplicate bot names must remain distinguishable by safe key prefix');
assert.match(html, /Remove from bot/);
assert.match(html, /data-bot-unlink="bot-1"/);
assert.doesNotMatch(html, /Neon <Grid>/, 'cosmetic names must be escaped');
assert.match(html, /Neon &lt;Grid&gt;/);
assert.doesNotMatch(html, /value="arena_alpha"/, 'rendered account UI must not contain raw API keys');

const dashboardHTML = readFileSync(new URL('../frontend/dashboard/index.html', import.meta.url), 'utf8');
assert.match(dashboardHTML, /dashboard\/login/, 'verified-email sign-in should use the customer dashboard login route');
assert.match(dashboardHTML, /id="accountSignInButton"[^>]*disabled/, 'email login stays disabled until session capability is known');
assert.match(dashboardHTML, /method:'POST'/, 'account sign-out should use a CSRF-protected POST');
assert.match(dashboardHTML, /data-account-retry/, 'an initial inventory failure should expose a retry action');
assert.match(dashboardHTML, /Retry email sign-in check/, 'a failed session capability request should be retryable, not mislabeled as unconfigured');
assert.match(dashboardHTML, /tabName==='cosmetics' && hasVerifiedAccount\(\)\) refreshAccountCosmetics\(\)/, 'opening Cosmetics must fetch even when API-key login won the startup race');
assert.match(dashboardHTML, /sessionStorage\.setItem\('arena_keys'/, 'legacy bot-performance keys should be tab-scoped');
assert.doesNotMatch(dashboardHTML, /localStorage\.setItem\('arena_keys'/, 'bot bearer keys must not persist across browser sessions');
assert.match(dashboardHTML, /input\.value = '';/, 'the one-time link key should be cleared as soon as it is submitted');
const linkHandler = dashboardHTML.slice(
  dashboardHTML.indexOf('async function handleAccountPanelSubmit'),
  dashboardHTML.indexOf('async function assignAccountLicense'),
);
assert.doesNotMatch(linkHandler, /saveKey|localStorage/, 'linking a bot must not persist the proof key in dashboard storage');

console.log('account-owned dashboard cosmetics rendering and assignment rules pass');
