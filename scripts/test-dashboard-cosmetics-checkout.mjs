import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const html = readFileSync(new URL('../frontend/dashboard/index.html', import.meta.url), 'utf8');
const accountCosmetics = readFileSync(new URL('../frontend/dashboard/account-cosmetics.js', import.meta.url), 'utf8');
const start = html.indexOf('let accountCheckoutOperationGeneration');
const end = html.indexOf('async function assignAccountLicense', start);
assert.ok(start >= 0 && end > start, 'dashboard must expose a checkout action before assignment helpers');
const source = html.slice(start, end);

const calls = [];
const sequence = [];
const redirects = [];
let renders = 0;
let refreshNotice = '';
let checkoutResponse = {
  presentation:'embedded',
  session_id:'cs_embedded_dashboard',
  client_secret:'cs_embedded_dashboard_secret_browser',
};
let prepareError = null;
const sandbox = {
	AbortController,
	document: {visibilityState:'visible'},
  accountCatalog: {checkout_enabled:true,packs:[{id:'set-003-pack',is_purchasable:true}]},
	accountSubscriptionState: {status:'idle',message:''},
  accountCheckoutState: {status:'idle',packID:'',message:''},
  accountOrders: [],
  accountBusyOrderID: '',
  accountRequest: async (path, options) => {
    sequence.push('request');
    calls.push({path, options});
    return checkoutResponse;
  },
  renderAccountCosmetics: () => { renders += 1; },
  refreshAccountCosmetics: async notice => { refreshNotice = notice; },
  navigateAccount: url => redirects.push(url),
  URL,
  window: {
    ArenaAccountCosmetics: {
      checkoutIntent: () => ({ok:true,path:'/account/cosmetics/checkout',body:{pack_id:'set-003-pack',quantity:1}}),
      accountRoute: (name, id) => name === 'orderCheckout' ? `/account/cosmetics/orders/${encodeURIComponent(id)}/checkout` : '',
    },
    ArenaEmbeddedCheckout: {
      prepare: async () => {
        sequence.push('prepare');
        if (prepareError) throw prepareError;
      },
      mount: async payload => {
        sequence.push('mount');
        assert.deepEqual(JSON.parse(JSON.stringify(payload)), checkoutResponse);
      },
      abort: () => sequence.push('abort'),
    },
    confirm: () => false,
    location: {
      href:'https://arena.example/dashboard/?tab=cosmetics',
      assign:url => redirects.push(url),
    },
  },
};
vm.runInNewContext(source, sandbox);
await sandbox.startCosmeticCheckout('set-003-pack');

assert.deepEqual(sequence, ['prepare','request','mount'], 'Stripe host must be ready before a Checkout Session is created');
assert.equal(calls.length, 1, 'checkout should make exactly one request');
assert.equal(calls[0].path, '/account/cosmetics/checkout');
assert.equal(calls[0].options.method, 'POST');
assert.deepEqual(JSON.parse(calls[0].options.body), {pack_id:'set-003-pack',quantity:1,presentation:'embedded'});
assert.deepEqual(redirects, [], 'embedded Checkout must not navigate away from Arena');
assert.ok(renders >= 1, 'pending checkout should update the visible state immediately');

calls.length = 0;
sequence.length = 0;
redirects.length = 0;
sandbox.accountCheckoutState = {status:'idle',packID:'',message:''};
checkoutResponse = {checkout_url:'https://checkout.example/session/persisted-hosted',presentation:'hosted',session_id:'cs_hosted_persisted'};
await sandbox.startCosmeticCheckout('set-003-pack');
assert.deepEqual(sequence, ['prepare','request','abort'], 'a persisted hosted retry must dismiss the unused embedded preflight before redirecting');
assert.deepEqual(redirects, ['https://checkout.example/session/persisted-hosted'], 'the stored server presentation must override the browser preflight choice');

calls.length = 0;
sequence.length = 0;
sandbox.window.ArenaAccountCosmetics.checkoutIntent = () => ({ok:false,reason:'checkout-disabled'});
await sandbox.startCosmeticCheckout('set-003-pack');
assert.equal(calls.length, 0, 'disabled checkout must not send a request');
assert.deepEqual(sequence, [], 'disabled checkout must not load Stripe');
assert.equal(sandbox.accountCheckoutState.status, 'disabled');

sandbox.window.ArenaAccountCosmetics.checkoutIntent = () => ({ok:true,path:'/account/cosmetics/checkout',body:{pack_id:'set-003-pack',quantity:1}});
sandbox.accountCheckoutState = {status:'idle',packID:'',message:''};
redirects.length = 0;
checkoutResponse = {};
await sandbox.startCosmeticCheckout('set-003-pack');
assert.equal(redirects.length, 0, 'a missing embedded client secret must not redirect');
assert.equal(sandbox.accountCheckoutState.status, 'error');
assert.ok(sequence.includes('abort'), 'a failed session response should dismiss the prepared modal');

calls.length = 0;
sequence.length = 0;
sandbox.accountCheckoutState = {status:'idle',packID:'',message:''};
checkoutResponse = {checkout_url:'https://checkout.example/session/fallback',presentation:'hosted',session_id:'cs_hosted'};
prepareError = new Error('Stripe.js unavailable');
sandbox.window.confirm = () => true;
await sandbox.startCosmeticCheckout('set-003-pack');
assert.deepEqual(sequence, ['prepare','request'], 'hosted fallback must be selected before session creation');
assert.equal(JSON.parse(calls[0].options.body).presentation, 'hosted');
assert.deepEqual(redirects, ['https://checkout.example/session/fallback']);

calls.length = 0;
sequence.length = 0;
redirects.length = 0;
prepareError = null;
sandbox.accountOrders = [{id:'order-resume',pack_id:'set-003-pack',status:'checkout_pending',checkout_session_id:'cs_resume'}];
sandbox.accountBusyOrderID = '';
checkoutResponse = {
  resumed:true,resumable:true,checkout_status:'open',presentation:'embedded',
  session_id:'cs_resume',client_secret:'cs_resume_secret_browser',
};
await sandbox.resumeCosmeticCheckout('order-resume');
assert.deepEqual(sequence, ['request','prepare','mount'], 'resume must retrieve the attached session before preparing its authoritative UI mode');
assert.equal(calls[0].path, '/account/cosmetics/orders/order-resume/checkout');
assert.equal(calls[0].options.method, 'POST');
assert.equal(calls[0].options.body, undefined, 'resume must not choose a new presentation or create another order');
assert.deepEqual(redirects, [], 'an embedded resumed session stays on Arena');
assert.equal(sandbox.accountBusyOrderID, '', 'resume action must release its busy state after mounting');

calls.length = 0;
sequence.length = 0;
redirects.length = 0;
sandbox.accountOrders = [{
  id:'order-retry',pack_id:'set-003-pack',status:'payment_failed',checkout_session_id:'',checkout_presentation:'embedded',
}];
checkoutResponse = {
  resumed:true,resumable:true,checkout_status:'open',presentation:'embedded',
  session_id:'cs_retry',client_secret:'cs_retry_secret_browser',
};
await sandbox.resumeCosmeticCheckout('order-retry');
assert.deepEqual(sequence, ['request','prepare','mount'], 'an unattached failure must replay the same reserved checkout and then mount its stored mode');
assert.equal(calls[0].path, '/account/cosmetics/orders/order-retry/checkout');

calls.length = 0;
sequence.length = 0;
refreshNotice = '';
sandbox.accountOrders = [{id:'order-resume',pack_id:'set-003-pack',status:'checkout_pending',checkout_session_id:'cs_resume'}];
checkoutResponse = {resumed:false,resumable:false,checkout_status:'complete',order_id:'order-resume'};
await sandbox.resumeCosmeticCheckout('order-resume');
assert.deepEqual(sequence, ['request'], 'a completed session must not be mounted again');
assert.equal(sandbox.accountCheckoutState.status, 'reconciling');
assert.match(refreshNotice, /signed payment event/i);

calls.length = 0;
sequence.length = 0;
checkoutResponse = {
  resumed:true,resumable:true,checkout_status:'open',presentation:'hosted',
  session_id:'cs_resume',checkout_url:'https://checkout.stripe.com/c/pay/cs_resume',
};
await sandbox.resumeCosmeticCheckout('order-resume');
assert.deepEqual(sequence, ['request'], 'legacy hosted resume must preserve its stored UI mode without loading embedded Stripe');
assert.deepEqual(redirects, ['https://checkout.stripe.com/c/pay/cs_resume']);

calls.length = 0;
sequence.length = 0;
redirects.length = 0;
sandbox.accountCheckoutState = {status:'idle',packID:'',message:''};
let resolveStaleRequest;
let checkoutRequestNumber = 0;
checkoutResponse = {
  presentation:'embedded',order_id:'order-new',session_id:'cs_new',client_secret:'cs_new_secret_browser',
};
sandbox.accountRequest = async (path, options) => {
  sequence.push('request');
  calls.push({path,options});
  checkoutRequestNumber += 1;
  if (checkoutRequestNumber === 1) {
    return new Promise(resolve => { resolveStaleRequest = resolve; });
  }
  return checkoutResponse;
};
const staleCheckout = sandbox.startCosmeticCheckout('set-003-pack');
for (let step = 0; step < 4 && calls.length < 1; step += 1) await Promise.resolve();
assert.equal(calls.length, 1, 'first checkout must reach its in-flight request');
sandbox.accountCheckoutState = {status:'idle',packID:'',message:''};
const currentCheckout = sandbox.startCosmeticCheckout('set-003-pack');
await currentCheckout;
resolveStaleRequest({
  presentation:'hosted',order_id:'order-stale',session_id:'cs_stale',checkout_url:'https://checkout.example/session/stale',
});
await staleCheckout;
assert.deepEqual(sequence, ['prepare','request','prepare','request','mount'], 'only the newest checkout operation may mount after an older response arrives late');
assert.deepEqual(redirects, [], 'a stale hosted response must not navigate over the newer embedded checkout');
assert.equal(calls[0].options.signal.aborted, true, 'starting a newer checkout must abort the older HTTP request');

const guardedOperation = sandbox.beginAccountCheckoutOperation();
guardedOperation.embeddedReservation = {request_id:'request-current'};
assert.equal(
  sandbox.embeddedCheckoutAbortMatchesActiveOperation({detail:{request_id:'request-stale'}}),
  false,
  'a stale prepare abort must not invalidate the current checkout operation',
);
assert.equal(
  sandbox.embeddedCheckoutAbortMatchesActiveOperation({detail:{request_id:'request-current'}}),
  true,
  'the active reservation abort must still release its own checkout operation',
);
assert.equal(
  sandbox.embeddedCheckoutCompletionMatchesActiveOperation({detail:{request_id:'request-stale',session_id:'cs_stale'}}),
  false,
  'checkout A completing after checkout B prepares must not invalidate B or render A as the current success',
);
assert.equal(
  sandbox.embeddedCheckoutCompletionMatchesActiveOperation({detail:{request_id:'request-current'}}),
  true,
  'the active reservation completion must still reconcile its signed fulfillment',
);

let pollCount = 0;
sandbox.accountOrders = [{id:'order-poll',status:'processing',fulfilled_license_count:0}];
sandbox.refreshAccountCosmetics = async notice => {
  refreshNotice = notice;
  pollCount += 1;
  if (pollCount === 3) sandbox.accountOrders = [{id:'order-poll',status:'paid',fulfilled_license_count:3}];
};
const orderSettled = await sandbox.reconcileCompletedAccountCheckout({kind:'order',id:'order-poll',sessionID:'cs_poll'}, [0,0,0,0]);
assert.equal(orderSettled, true, 'completion polling must stop once signed fulfillment appears');
assert.equal(pollCount, 3, 'completion polling must use bounded backoff attempts instead of a single refresh');
assert.match(refreshNotice, /signed payment event/i);

pollCount = 0;
sandbox.accountOrders = [{id:'order-still-processing',status:'processing',fulfilled_license_count:0}];
const bounded = await sandbox.reconcileCompletedAccountCheckout({kind:'order',id:'order-still-processing',sessionID:'cs_wait'}, [0,0]);
assert.equal(bounded, false);
assert.equal(pollCount, 2, 'completion reconciliation must stop after its bounded attempt list');

pollCount = 0;
sandbox.document.visibilityState = 'hidden';
const hidden = await sandbox.reconcileCompletedAccountCheckout({kind:'order',id:'order-hidden',sessionID:'cs_hidden'}, [0,0]);
assert.equal(hidden, false);
assert.equal(pollCount, 0, 'completion reconciliation must stop without polling while the page is hidden');

assert.ok(!accountCosmetics.includes('Purchase complete.'), 'a client-controlled success query must not claim payment completion');
assert.match(accountCosmetics, /Checkout returned[\s\S]*still processing[\s\S]*signed payment event/, 'success return copy must defer to verified webhook fulfillment');

console.log('dashboard prepares a top-level Stripe host, mounts embedded Checkout, and chooses hosted fallback only before session creation');
