import assert from 'node:assert/strict';
import {existsSync, readFileSync} from 'node:fs';

const mainHTML = readFileSync(new URL('../frontend/index.html', import.meta.url), 'utf8');
const appSource = readFileSync(new URL('../frontend/js/app.js', import.meta.url), 'utf8');
const dashboardHTML = readFileSync(new URL('../frontend/dashboard/index.html', import.meta.url), 'utf8');
const accountSource = readFileSync(new URL('../frontend/dashboard/account-cosmetics.js', import.meta.url), 'utf8');
const debugHTML = readFileSync(new URL('../frontend/debug2.html', import.meta.url), 'utf8');
const publicDocs = [
  ['README.md', readFileSync(new URL('../README.md', import.meta.url), 'utf8')],
  ['sdk/README.md', readFileSync(new URL('../sdk/README.md', import.meta.url), 'utf8')],
  ['bots/anismin_bot/README.md', readFileSync(new URL('../bots/anismin_bot/README.md', import.meta.url), 'utf8')],
  ['frontend/llms.txt', readFileSync(new URL('../frontend/llms.txt', import.meta.url), 'utf8')],
];

assert.doesNotMatch(mainHTML, /\/api\/v1\/keys\/generate|No signup required/i,
  'public Arena UI must not advertise anonymous API-key generation');
assert.match(mainHTML, /Create API key in Dashboard/i);
assert.match(mainHTML, /href="dashboard\/\?tab=cosmetics"/,
  'Get Started must send key creation through the authenticated Dashboard');
assert.doesNotMatch(appSource, /key-generator|initKeyGenerator|keygen-card/,
  'the public spectator app must not ship the anonymous key generator');
assert.doesNotMatch(debugHTML, /key-generator|initKeyGenerator/,
  'public debug pages must not import the retired anonymous key generator');
assert.equal(existsSync(new URL('../frontend/js/key-generator.js', import.meta.url)), false,
  'the obsolete anonymous key generator module should be removed');
assert.equal(existsSync(new URL('../frontend/js/credential-events.js', import.meta.url)), false,
  'the credential event helper should be removed with its only caller');

for (const [name, content] of publicDocs) {
  assert.doesNotMatch(content, /\/api\/v1\/keys\/generate/i,
    `${name} must not direct users to the retired anonymous key route`);
  assert.match(content, /Dashboard/i, `${name} should direct users to authenticated Dashboard key creation`);
}

assert.match(accountSource, /keys:\s*'\/account\/keys'/);
assert.match(accountSource, /key:\s*`\/account\/keys\/\$\{encoded\}`/);
assert.match(dashboardHTML, /async function createAccountKey/);
assert.match(dashboardHTML, /accountRoute\('keys'\)[\s\S]*method:'POST'/,
  'Dashboard key creation must use the authenticated account route');
assert.match(dashboardHTML, /async function revokeAccountKey/);
assert.match(dashboardHTML, /accountRoute\('key', keyID\)[\s\S]*method:'DELETE'/,
  'Dashboard key revocation must target one account-owned key');
assert.match(dashboardHTML, /input\.value = '';/,
  'one-time API-key values must be zeroed before removal from the Dashboard');

console.log('API-key creation is account-owned and available only in the authenticated Dashboard');
