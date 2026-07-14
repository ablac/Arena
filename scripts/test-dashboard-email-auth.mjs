import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const dashboard = readFileSync(new URL('../frontend/dashboard/index.html', import.meta.url), 'utf8');
const cosmeticsSource = readFileSync(new URL('../frontend/dashboard/account-cosmetics.js', import.meta.url), 'utf8');
const sandbox = {};
vm.runInNewContext(cosmeticsSource, sandbox, {filename:'account-cosmetics.js'});

const session = sandbox.ArenaAccountCosmetics.normalizeSession({
  authenticated:false,
  login_enabled:true,
  email_login_enabled:true,
  oidc_login_enabled:false,
  email_start_url:'/api/v1/account/email/start',
  email_verify_url:'/api/v1/account/email/verify',
});
assert.equal(session.email_login_enabled, true);
assert.equal(session.oidc_login_enabled, false);
assert.equal(session.email_start_url, '/api/v1/account/email/start');
assert.equal(session.email_verify_url, '/api/v1/account/email/verify');

assert.match(dashboard, /id="accountEmailForm"/);
assert.match(dashboard, /id="accountEmailInput"[^>]*type="email"[^>]*autocomplete="email"/);
assert.match(dashboard, /id="accountDisplayNameInput"[^>]*autocomplete="name"/);
assert.match(dashboard, /id="accountEmailSubmit"[^>]*type="submit"/);
assert.match(dashboard, /id="accountEmailConfirm"[^>]*hidden/);
assert.match(dashboard, /id="accountEmailConfirmButton"[^>]*type="button"/);
assert.match(dashboard, /Email me a sign-in link/);
assert.doesNotMatch(dashboard.slice(dashboard.indexOf('id="accountEmailForm"'), dashboard.indexOf('</form>', dashboard.indexOf('id="accountEmailForm"'))), /type="password"/, 'native registration must remain passwordless');

assert.match(dashboard, /function takeCustomerEmailTokenFromHash/);
assert.match(dashboard, /params\.get\('email_token'\)/);
assert.match(dashboard, /history\.replaceState\(null, '', cleanURL\)/, 'the bearer token must leave browser history before verification');
assert.match(dashboard, /function emailAuthRequest/);
assert.match(dashboard, /function confirmCustomerEmailToken/);
assert.match(dashboard, /accountEmailConfirmButton'\)\.addEventListener\('click', confirmCustomerEmailToken\)/, 'one-time tokens require a deliberate customer click');
assert.match(dashboard, /credentials:'same-origin'/);
assert.match(dashboard, /account\/email\/start/);
assert.match(dashboard, /account\/email\/verify/);
assert.match(dashboard, /If that address can receive Arena mail/);

const emailRequestStart = dashboard.indexOf('async function emailAuthRequest');
const emailRequestEnd = dashboard.indexOf('\n}', emailRequestStart) + 2;
const emailRequestSource = dashboard.slice(emailRequestStart, emailRequestEnd);
assert.doesNotMatch(emailRequestSource, /X-CSRF-Token/, 'pre-auth link requests use strict same-origin enforcement, not an unavailable session CSRF token');

function extractFunction(source, name) {
  const start = source.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `missing ${name}`);
  const open = source.indexOf('{', start);
  let depth = 0;
  for (let index = open; index < source.length; index += 1) {
    if (source[index] === '{') depth += 1;
    if (source[index] === '}') {
      depth -= 1;
      if (depth === 0) return source.slice(start, index + 1);
    }
  }
  throw new Error(`unterminated ${name}`);
}

function extractAsyncFunction(source, name) {
  return 'async ' + extractFunction(source, name).replace(/^function /, 'function ');
}

const elements = {
  accountEmailConfirmButton:{disabled:false, textContent:''},
  accountEmailConfirmStatus:{className:'', textContent:''},
  accountEmailConfirm:{hidden:false},
};
const navigations = [];
const location = {
  origin:'https://arena.example',
  pathname:'/dashboard/',
  search:'',
  assign(value) { navigations.push(value); },
};
const windowObject = {location};
windowObject.top = windowObject;
windowObject.self = windowObject;
const behaviorSandbox = {
  URL,
  location,
  window:windowObject,
  document:{getElementById(id) { return elements[id]; }},
};
vm.createContext(behaviorSandbox);
vm.runInContext(`
  let pendingCustomerEmailToken = 'one-time-token';
  const accountSession = {email_login_enabled:true, email_verify_url:'/api/v1/account/email/verify'};
  let initializeCalls = 0;
  let verifyRequest = null;
  async function emailAuthRequest(path, body) {
    verifyRequest = {path, body};
    return {redirect_to:'/dashboard/?pack=cosmetic-set-050'};
  }
  async function initializeAccountMode() { initializeCalls += 1; }
  function notifyOtherTabsSignedIn() {}
  ${extractFunction(dashboard, 'accountReturnPath')}
  ${extractFunction(dashboard, 'safeCustomerEmailRedirectPath')}
  ${extractFunction(dashboard, 'navigateAccount')}
  ${extractAsyncFunction(dashboard, 'confirmCustomerEmailToken')}
  globalThis.runConfirmation = confirmCustomerEmailToken;
  globalThis.behaviorState = () => ({pendingCustomerEmailToken, initializeCalls, verifyRequest});
`, behaviorSandbox);
await behaviorSandbox.runConfirmation();
assert.deepEqual(
  JSON.parse(JSON.stringify(behaviorSandbox.behaviorState().verifyRequest)),
  {path:'/api/v1/account/email/verify', body:{token:'one-time-token'}},
);
assert.equal(behaviorSandbox.behaviorState().pendingCustomerEmailToken, '');
assert.equal(behaviorSandbox.behaviorState().initializeCalls, 0, 'a restored purchase path should navigate instead of rendering stale state');
assert.deepEqual(navigations, ['/dashboard/?pack=cosmetic-set-050']);
assert.equal(elements.accountEmailConfirm.hidden, true);

const safeRedirect = vm.runInContext('safeCustomerEmailRedirectPath', behaviorSandbox);
assert.equal(safeRedirect('/arena/dashboard/?pack=cosmetic-set-100'), '/arena/dashboard/?pack=cosmetic-set-100');
assert.equal(safeRedirect('https://attacker.example/dashboard/'), '/dashboard/');
assert.equal(safeRedirect('//attacker.example/dashboard/'), '/dashboard/');
assert.equal(safeRedirect('/admin/'), '/dashboard/');

assert.match(dashboard, /account-cosmetics\.js\?v=20260714h/);
console.log('dashboard preserves the validated cosmetic purchase path after verified-email registration');
