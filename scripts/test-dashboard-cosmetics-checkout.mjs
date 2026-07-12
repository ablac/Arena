import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const html = readFileSync(new URL('../frontend/dashboard/index.html', import.meta.url), 'utf8');
const accountCosmetics = readFileSync(new URL('../frontend/dashboard/account-cosmetics.js', import.meta.url), 'utf8');
const start = html.indexOf('async function startCosmeticCheckout');
const end = html.indexOf('async function assignAccountLicense', start);
assert.ok(start >= 0 && end > start, 'dashboard must expose a checkout action before assignment helpers');
const source = html.slice(start, end);

const calls = [];
const redirects = [];
let renders = 0;
let checkoutResponse = {checkout_url:'https://checkout.example/session/abc'};
const sandbox = {
  accountCatalog: {checkout_enabled:true,packs:[{id:'set-003-pack',is_purchasable:true}]},
  accountCheckoutState: {status:'idle',packID:'',message:''},
  accountRequest: async (path, options) => {
    calls.push({path, options});
    return checkoutResponse;
  },
  renderAccountCosmetics: () => { renders += 1; },
  URL,
  window: {
    ArenaAccountCosmetics: {
      checkoutIntent: () => ({ok:true,path:'/account/cosmetics/checkout',body:{pack_id:'set-003-pack',quantity:1}}),
    },
    location: {
      href:'https://arena.example/dashboard/?tab=cosmetics',
      assign:url => redirects.push(url),
    },
  },
};
vm.runInNewContext(source, sandbox);
await sandbox.startCosmeticCheckout('set-003-pack');

assert.equal(calls.length, 1, 'checkout should make exactly one request');
assert.equal(calls[0].path, '/account/cosmetics/checkout');
assert.equal(calls[0].options.method, 'POST');
assert.deepEqual(JSON.parse(calls[0].options.body), {pack_id:'set-003-pack',quantity:1});
assert.deepEqual(redirects, ['https://checkout.example/session/abc']);
assert.ok(renders >= 1, 'pending checkout should update the visible state immediately');

calls.length = 0;
redirects.length = 0;
sandbox.window.ArenaAccountCosmetics.checkoutIntent = () => ({ok:false,reason:'checkout-disabled'});
await sandbox.startCosmeticCheckout('set-003-pack');
assert.equal(calls.length, 0, 'disabled checkout must not send a request');
assert.equal(sandbox.accountCheckoutState.status, 'disabled');

sandbox.window.ArenaAccountCosmetics.checkoutIntent = () => ({ok:true,path:'/account/cosmetics/checkout',body:{pack_id:'set-003-pack',quantity:1}});
sandbox.accountCheckoutState = {status:'idle',packID:'',message:''};
checkoutResponse = {};
await sandbox.startCosmeticCheckout('set-003-pack');
assert.equal(redirects.length, 0, 'a missing checkout URL must not redirect');
assert.equal(sandbox.accountCheckoutState.status, 'error');

sandbox.accountCheckoutState = {status:'idle',packID:'',message:''};
checkoutResponse = {checkout_url:'javascript:alert(1)'};
await sandbox.startCosmeticCheckout('set-003-pack');
assert.equal(redirects.length, 0, 'a non-HTTP checkout URL must not redirect');
assert.equal(sandbox.accountCheckoutState.status, 'error');

assert.ok(!accountCosmetics.includes('Purchase complete.'), 'a client-controlled success query must not claim payment completion');
assert.match(accountCosmetics, /Checkout returned[\s\S]*still processing[\s\S]*signed payment event/, 'success return copy must defer to verified webhook fulfillment');

console.log('dashboard checkout is CSRF-ready through accountRequest, gated, and redirects only to HTTP(S) checkout URLs');
