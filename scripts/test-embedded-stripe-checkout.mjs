import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('../frontend/js/embedded-checkout.js', import.meta.url), 'utf8');
const listeners = new Map();
const sandbox = {
  URL,
  console,
  crypto:{randomUUID:()=> 'request-1'},
  location:{origin:'https://arena.example'},
  addEventListener:(type, handler)=>listeners.set(type, handler),
  dispatchEvent:()=>{},
};
sandbox.window = sandbox;
sandbox.self = sandbox;
sandbox.top = sandbox;
vm.runInNewContext(source, sandbox, {filename:'embedded-checkout.js'});

const controller = sandbox.ArenaEmbeddedCheckout;
assert.ok(controller, 'shared Embedded Checkout controller must be installed');
assert.equal(typeof controller.prepare, 'function');
assert.equal(typeof controller.mount, 'function');
assert.equal(typeof controller.abort, 'function');

const internals = sandbox.ArenaEmbeddedCheckoutInternals;
assert.ok(internals, 'controller must expose frozen protocol validators for deterministic tests');
assert.equal(Object.isFrozen(internals), true);
assert.deepEqual(
  JSON.parse(JSON.stringify(internals.normalizeMountPayload({
    presentation:'embedded',session_id:'cs_123',client_secret:'cs_123_secret_abc',
  }))),
  {presentation:'embedded',session_id:'cs_123',client_secret:'cs_123_secret_abc'},
);
assert.equal(internals.normalizeMountPayload({presentation:'embedded',session_id:'',client_secret:'secret'}), null);
assert.equal(internals.normalizeMountPayload({presentation:'hosted',session_id:'cs_1',checkout_url:'javascript:alert(1)'}), null);
assert.deepEqual(JSON.parse(JSON.stringify(internals.normalizeReservation({request_id:'request-123'}))), {request_id:'request-123'});
assert.equal(internals.normalizeReservation({request_id:''}), null);
assert.equal(internals.completionCloseIsSafe(4, 4, ''), true);
assert.equal(internals.completionCloseIsSafe(4, 5, ''), false, 'an older completion timer must not close a newer generation');
assert.equal(internals.completionCloseIsSafe(4, 4, 'cs_new'), false, 'an older completion timer must not close a newly mounted session');

const frameWindow = {};
const frame = {contentWindow:frameWindow};
assert.equal(internals.isTrustedDashboardMessage({origin:'https://arena.example',source:frameWindow}, frame, 'https://arena.example'), true);
assert.equal(internals.isTrustedDashboardMessage({origin:'https://evil.example',source:frameWindow}, frame, 'https://arena.example'), false);
assert.equal(internals.isTrustedDashboardMessage({origin:'https://arena.example',source:{}}, frame, 'https://arena.example'), false);
assert.equal(internals.checkoutConfigURL('/dashboard/'), '/api/v1/cosmetics/checkout/config');
assert.equal(internals.checkoutConfigURL('/arena/dashboard/'), '/arena/api/v1/cosmetics/checkout/config');

for (const token of ['sk_live_forbidden','rk_live_forbidden','whsec_forbidden']) {
  assert.equal(source.includes(token), false, `browser controller must not contain ${token.split('_')[0]} server credentials`);
}
assert.match(source, /https:\/\/js\.stripe\.com\/clover\/stripe\.js/);
assert.ok(listeners.has('message'), 'controller must listen for the same-origin dashboard bridge');

const childListeners = new Map();
const childEvents = [];
const parentWindow = {postMessage:()=>{}};
const childSandbox = {
  URL,
  console,
  crypto:{randomUUID:()=> 'request-child'},
  location:{origin:'https://arena.example'},
  parent:parentWindow,
  top:{},
  addEventListener:(type, handler)=>childListeners.set(type, handler),
  dispatchEvent:event=>childEvents.push(event),
  CustomEvent:class CustomEvent {
    constructor(type, init={}) { this.type = type; this.detail = init.detail; }
  },
};
childSandbox.window = childSandbox;
childSandbox.self = childSandbox;
vm.runInNewContext(source, childSandbox, {filename:'embedded-checkout-child.js'});
await childListeners.get('message')({
  origin:'https://arena.example',
  source:parentWindow,
  data:{
    type:'arena:stripe-checkout:abort',
    request_id:'request-child',
    session_id:'cs_pending',
    message:'Secure checkout closed.',
  },
});
assert.deepEqual(
  JSON.parse(JSON.stringify(childEvents)),
	[{type:'arena:stripe-checkout:abort',detail:{request_id:'request-child',session_id:'cs_pending',message:'Secure checkout closed.'}}],
  'closing the top-level dialog must release the framed Dashboard checkout state',
);

const raceListeners = new Map();
const racePosts = [];
const requestIDs = ['request-old','request-new'];
const raceParent = {postMessage:message=>racePosts.push(message)};
const raceSandbox = {
  URL,
  console,
  crypto:{randomUUID:()=> requestIDs.shift()},
  location:{origin:'https://arena.example'},
  parent:raceParent,
  top:{},
  addEventListener:(type, handler)=>raceListeners.set(type, handler),
  dispatchEvent:()=>{},
};
raceSandbox.window = raceSandbox;
raceSandbox.self = raceSandbox;
vm.runInNewContext(source, raceSandbox, {filename:'embedded-checkout-race-child.js'});
const oldPrepare = raceSandbox.ArenaEmbeddedCheckout.prepare();
const newPrepare = raceSandbox.ArenaEmbeddedCheckout.prepare();
assert.deepEqual(racePosts.map(message => message.request_id), ['request-old','request-new']);
await raceListeners.get('message')({
  origin:'https://arena.example',source:raceParent,
  data:{type:'arena:stripe-checkout:abort',request_id:'request-old',message:'Checkout replaced by a newer request.'},
});
await raceListeners.get('message')({
  origin:'https://arena.example',source:raceParent,
  data:{type:'arena:stripe-checkout:ready',request_id:'request-new'},
});
await assert.rejects(oldPrepare, /newer request/i);
const newReservation = await newPrepare;
assert.equal(newReservation.request_id, 'request-new');
racePosts.length = 0;
const mounted = raceSandbox.ArenaEmbeddedCheckout.mount({
  presentation:'embedded',session_id:'cs_new',client_secret:'cs_new_secret_browser',
}, newReservation);
assert.equal(racePosts[0].request_id, 'request-new', 'mount must carry the exact reservation returned by its own prepare');
await raceListeners.get('message')({
  origin:'https://arena.example',source:raceParent,
  data:{type:'arena:stripe-checkout:mounted',request_id:'request-new',session_id:'cs_new'},
});
await mounted;
racePosts.length = 0;
await raceSandbox.ArenaEmbeddedCheckout.abort('Checkout replaced by a hosted request.', newReservation);
assert.equal(racePosts[0]?.type, 'arena:stripe-checkout:abort');
assert.equal(racePosts[0]?.request_id, 'request-new', 'a mounted child reservation must remain abortable by its owning Dashboard operation');
await assert.rejects(
  raceSandbox.ArenaEmbeddedCheckout.mount({
    presentation:'embedded',session_id:'cs_old',client_secret:'cs_old_secret_browser',
  }, {request_id:'request-old'}),
  /not prepared/i,
  'a superseded reservation must never mount another operation\'s session',
);

console.log('embedded Stripe controller validates same-origin frame messages and browser-safe mount payloads');
