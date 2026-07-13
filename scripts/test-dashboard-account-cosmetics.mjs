import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('../frontend/dashboard/account-cosmetics.js', import.meta.url), 'utf8');
const sandbox = {};
vm.runInNewContext(source, sandbox, {filename: 'account-cosmetics.js'});
const cosmetics = sandbox.ArenaAccountCosmetics;
const recentPurchaseFailures = [];

function recentPurchaseCheck(name, check) {
  try {
    check();
  } catch (error) {
    recentPurchaseFailures.push(`${name}: ${error.message}`);
  }
}

assert.ok(cosmetics, 'dashboard cosmetics helpers should attach to globalThis');
assert.equal(cosmetics.normalizeSession({authenticated:false, account:{id:'acct-1',email:'owner@example.com',email_verified:true}}).authenticated, false, 'an explicit logged-out response must never be inferred as authenticated');
const verifiedSession = cosmetics.normalizeSession({authenticated:true, login_enabled:true, login_url:'/api/v1/dashboard/login', account:{id:'acct-1',email:'owner@example.com',email_verified_at:'2026-07-10T12:00:00Z'}});
assert.equal(verifiedSession.account.email_verified, true, 'a server verification timestamp should authorize the account UX');
assert.equal(verifiedSession.login_enabled, true);
assert.equal(verifiedSession.login_url, '/api/v1/dashboard/login');
const emailSession = cosmetics.normalizeSession({authenticated:false,login_enabled:true,email_login_enabled:true,oidc_login_enabled:false,email_start_url:'/api/v1/account/email/start',email_verify_url:'/api/v1/account/email/verify'});
assert.equal(emailSession.email_login_enabled, true);
assert.equal(emailSession.oidc_login_enabled, false);
assert.equal(emailSession.email_start_url, '/api/v1/account/email/start');
assert.equal(emailSession.email_verify_url, '/api/v1/account/email/verify');
assert.equal(cosmetics.accountRoute('session'), '/account/session');
assert.equal(cosmetics.accountRoute('checkout'), '/account/cosmetics/checkout');
assert.equal(cosmetics.accountRoute('subscriptionCheckout'), '/account/cosmetics/subscription/checkout');
assert.equal(cosmetics.accountRoute('subscriptionPortal'), '/account/cosmetics/subscription/portal');
assert.equal(cosmetics.accountRoute('keys'), '/account/keys');
assert.equal(cosmetics.accountRoute('key', 'key/a'), '/account/keys/key%2Fa');
recentPurchaseCheck('orders route', () => {
  assert.equal(cosmetics.accountRoute('orders'), '/account/cosmetics/orders');
});
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
assert.equal(cosmetics.slotLabel('trail'), 'Trails');

const subscriptionOffer = cosmetics.normalizeSubscriptionOffer({
  enabled:true,
  price_cents:1999,
  currency:'USD',
  interval:'month',
  includes_future_sets:true,
  max_api_keys:5,
});
assert.deepEqual(JSON.parse(JSON.stringify(subscriptionOffer)), {
  enabled:true,
  price_cents:1999,
  currency:'USD',
  interval:'month',
  includes_future_sets:true,
  max_api_keys:5,
});
const managedSubscription = cosmetics.normalizeSubscription({
  id:'sub-1',
  status:'active',
  has_access:false,
  cancel_at_period_end:true,
  current_period_end:'2026-08-12T12:00:00Z',
  can_manage:true,
  price_cents:1999,
  currency:'USD',
  interval:'month',
  includes_future_sets:true,
  max_api_keys:5,
});
assert.equal(managedSubscription.has_access, false, 'UI must use the server-authoritative has_access flag instead of inferring access from status');
assert.equal(managedSubscription.cancel_at_period_end, true);

const keyCollection = cosmetics.normalizeKeyCollection({
  keys:[
    {id:'key-1',key_prefix:'arena_alpha',bot_id:'bot-1',bot_name:'Alpha',created_at:'2026-07-12T10:00:00Z',is_active:true},
    {id:'key-2',key_prefix:'arena_beta',bot_id:'bot-2',bot_name:'Beta',created_at:'2026-07-12T11:00:00Z',is_active:true},
  ],
  active_count:2,
  limit:5,
});
assert.equal(keyCollection.active_count, 2);
assert.equal(keyCollection.limit, 5);
assert.equal(keyCollection.keys[0].key_prefix, 'arena_alpha');
assert.deepEqual(
  JSON.parse(JSON.stringify(cosmetics.keyCreateIntent('  New Bot  ', keyCollection))),
  {ok:true,path:'/account/keys',body:{bot_name:'New Bot'}},
);
assert.equal(cosmetics.keyCreateIntent('Sixth Bot', {...keyCollection,active_count:5}).reason, 'key-limit-reached');
assert.equal(cosmetics.keyCreateIntent('Pending Bot', null).reason, 'keys-unavailable');
assert.deepEqual(
  JSON.parse(JSON.stringify(cosmetics.keyRevokeIntent('key/1'))),
  {ok:true,path:'/account/keys/key%2F1'},
);
assert.deepEqual(
  JSON.parse(JSON.stringify(cosmetics.subscriptionIntent(subscriptionOffer, null))),
  {ok:true,kind:'checkout',path:'/account/cosmetics/subscription/checkout'},
);
assert.deepEqual(
  JSON.parse(JSON.stringify(cosmetics.subscriptionIntent(subscriptionOffer, {...managedSubscription,can_manage:true}))),
  {ok:true,kind:'portal',path:'/account/cosmetics/subscription/portal'},
);
for (const status of ['created', 'checkout_pending', 'canceled', 'expired']) {
  assert.deepEqual(
    JSON.parse(JSON.stringify(cosmetics.subscriptionIntent(subscriptionOffer, {...managedSubscription,status,can_manage:false}))),
    {ok:true,kind:'checkout',path:'/account/cosmetics/subscription/checkout'},
    `${status} subscriptions must be resumable or replaceable through checkout`,
  );
}
assert.deepEqual(
  JSON.parse(JSON.stringify(cosmetics.subscriptionIntent(subscriptionOffer, {...managedSubscription,status:'checkout_pending',can_manage:true}))),
  {ok:true,kind:'portal',path:'/account/cosmetics/subscription/portal'},
  'a completed Checkout with a known Stripe customer must expose cancellation while lifecycle webhooks catch up',
);
assert.deepEqual(
  JSON.parse(JSON.stringify(cosmetics.subscriptionIntent(subscriptionOffer, {...managedSubscription,status:'billing_mismatch',can_manage:true}))),
  {ok:true,kind:'portal',path:'/account/cosmetics/subscription/portal'},
  'billing mismatches must stay manageable while access remains revoked',
);
assert.equal(
  cosmetics.subscriptionIntent(subscriptionOffer, {...managedSubscription,status:'active',can_manage:false}).reason,
  'subscription-unmanageable',
  'an active subscription without provider management must never fall through into duplicate checkout',
);

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

const catalog = {
  checkout_enabled: true,
  subscription_offer: subscriptionOffer,
  categories: [{id:'sets',name:'Sets'}, {id:'trails',name:'Trails'}],
  packs: [...Array.from({length:100}, (_, index) => {
    const number = String(index + 1).padStart(3, '0');
    const assetKey = `arena_set_${number}_signal_${number}`;
    return {
      id:`set-${number}-pack`, name:`Signal Set ${number}`, description:`Set ${number}`,
      category_id:'sets', is_purchasable:true, price_cents:199, currency:'USD',
      items:['bot_skin','weapon_skin','attachment'].map(slot => ({id:`${slot}-${number}`,slot,asset_key:assetKey,name:`${slot} ${number}`})),
    };
  }), {
    id:'trail-ember-sparks-pack', name:'Ember Sparks', description:'Hot cinders in a fire-red wake.',
    category_id:'trails', is_purchasable:true, price_cents:99, currency:'USD',
    items:[{id:'trail-ember-sparks',slot:'trail',asset_key:'ember_sparks',name:'Ember Sparks'}],
  }],
};
const shopHTML = cosmetics.renderPanel(snapshot, {catalog});
assert.equal((shopHTML.match(/data-shop-pack=/g) || []).length, 12, 'dashboard shop should bound its initial pack render');
assert.match(shopHTML, /data-pack-checkout="set-001-pack"/, 'enabled sale-ready packs should expose checkout');
assert.match(shopHTML, /\$1\.99/, 'every one-time cosmetic set should display the $1.99 catalog price');
assert.match(shopHTML, /All Access/);
assert.match(shopHTML, /\$19\.99[\s\S]*month/);
assert.match(shopHTML, /every current and future cosmetic set and trail/i);
assert.match(shopHTML, /up to 5 active API keys/i);
assert.match(shopHTML, /subscription cosmetics are removed/i);
assert.match(shopHTML, /data-subscription-checkout/, 'accounts without a managed subscription should be able to start All Access checkout');
const searchedShopHTML = cosmetics.renderPanel(snapshot, {catalog, shopQuery:'Signal Set 099'});
assert.equal((searchedShopHTML.match(/data-shop-pack=/g) || []).length, 1, 'dashboard search should narrow the pack list');
const trailShopHTML = cosmetics.renderPanel(snapshot, {catalog, shopQuery:'Ember Sparks'});
assert.match(trailShopHTML, /Individual trail/);
assert.match(trailShopHTML, /\$0\.99/);
assert.match(trailShopHTML, /data-pack-checkout="trail-ember-sparks-pack"/);
const noResultsHTML = cosmetics.renderPanel(snapshot, {catalog, shopQuery:'missing set'});
assert.match(noResultsHTML, /No cosmetic products match/i);
const disabledShopHTML = cosmetics.renderPanel(snapshot, {catalog:{...catalog,checkout_enabled:false}});
assert.doesNotMatch(disabledShopHTML, /data-pack-checkout=/, 'checkout-disabled catalogs must not render purchase buttons');
assert.deepEqual(
  JSON.parse(JSON.stringify(cosmetics.checkoutIntent(catalog, 'set-001-pack'))),
  {ok:true,path:'/account/cosmetics/checkout',body:{pack_id:'set-001-pack',quantity:1}},
);
assert.deepEqual(
  JSON.parse(JSON.stringify(cosmetics.checkoutIntent(catalog, 'trail-ember-sparks-pack'))),
  {ok:true,path:'/account/cosmetics/checkout',body:{pack_id:'trail-ember-sparks-pack',quantity:1}},
);

const baseOrder = {
  id:'order-001',
  pack_id:'set-003-pack',
  pack_name:'Signal Set 003',
  quantity:2,
  expected_subtotal_cents:1000,
  amount_received_cents:900,
  amount_refunded_cents:100,
  currency:'USD',
  status:'refund_review',
  fulfilled_license_count:6,
  created_at:'2026-07-11T12:00:00Z',
};

const loadingOrdersHTML = cosmetics.renderPanel(snapshot, {catalog, orders:null, ordersError:''});
recentPurchaseCheck('loading state', () => {
  assert.match(loadingOrdersHTML, /Recent purchases/);
  assert.match(loadingOrdersHTML, /Loading recent purchases\.\.\./);
});

const failedOrdersHTML = cosmetics.renderPanel(snapshot, {
  catalog,
  orders:null,
  ordersError:'Ledger <offline>',
});
recentPurchaseCheck('independent error state', () => {
  assert.match(failedOrdersHTML, /Recent purchases unavailable/);
  assert.match(failedOrdersHTML, /Ledger &lt;offline&gt;/);
  assert.match(failedOrdersHTML, /data-shop-pack="set-001-pack"/, 'order failure must leave the shop visible');
  assert.match(failedOrdersHTML, /data-license-id="license-1"/, 'order failure must leave owned inventory visible');
});

const emptyOrdersHTML = cosmetics.renderPanel(snapshot, {catalog, orders:[]});
recentPurchaseCheck('empty state', () => {
  assert.match(emptyOrdersHTML, /No purchases yet\./);
});

const hostileOrdersHTML = cosmetics.renderPanel(snapshot, {
  catalog,
  orders:[
    {
      ...baseOrder,
      id:'order-<img src=x onerror=alert(1)>',
      pack_name:'Launch <script>alert(1)</script> Set',
    },
    {
      ...baseOrder,
      id:'order-hostile-status',
      status:'<svg onload=alert(1)>',
    },
  ],
});
recentPurchaseCheck('hostile data and complete order facts', () => {
  assert.doesNotMatch(hostileOrdersHTML, /<img src=x onerror=alert\(1\)>/);
  assert.doesNotMatch(hostileOrdersHTML, /<script>alert\(1\)<\/script>/);
  assert.doesNotMatch(hostileOrdersHTML, /<svg onload=alert\(1\)>/);
  assert.match(hostileOrdersHTML, /order-&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.match(hostileOrdersHTML, /Launch &lt;script&gt;alert\(1\)&lt;\/script&gt; Set/);
  assert.match(hostileOrdersHTML, /Quantity 2/);
  assert.match(hostileOrdersHTML, /Expected[\s\S]*\$10\.00/);
  assert.match(hostileOrdersHTML, /Received[\s\S]*\$9\.00/);
  assert.match(hostileOrdersHTML, /Refunded[\s\S]*\$1\.00/);
  assert.match(hostileOrdersHTML, /6 licenses fulfilled/);
  assert.match(hostileOrdersHTML, /<time[^>]*datetime="2026-07-11T12:00:00\.000Z"[^>]*>[^<]*2026[^<]*<\/time>/);
});

const statusCases = [
  ['created', 'Checkout pending'],
  ['checkout_pending', 'Checkout pending'],
  ['processing', 'Processing'],
  ['paid', 'Paid'],
  ['refund_review', 'Refund review'],
  ['refunded', 'Refunded'],
  ['disputed', 'Disputed'],
  ['expired', 'Expired'],
  ['payment_failed', 'Failed'],
  ['failed', 'Failed'],
];
const statusOrdersHTML = cosmetics.renderPanel(snapshot, {
  catalog,
  orders:statusCases.map(([status], index) => ({...baseOrder, id:`status-${index}`, status})),
});
for (const [status, label] of statusCases) {
  recentPurchaseCheck(`truthful ${status} status`, () => {
    assert.match(
      statusOrdersHTML,
      new RegExp(`data-order-status="${status}"[\\s\\S]*?<span class="cosmetic-purchase-status[^"]*">${label}<\\/span>`),
    );
  });
}

const boundedOrdersHTML = cosmetics.renderPanel(snapshot, {
  catalog,
  orders:Array.from({length:25}, (_, index) => ({...baseOrder, id:`bounded-${index}`})),
});
recentPurchaseCheck('bounded history', () => {
  assert.equal((boundedOrdersHTML.match(/data-purchase-order=/g) || []).length, 20);
});

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

const managedHTML = cosmetics.renderPanel({
  ...snapshot,
  subscription:{
    id:'sub-active',status:'active',has_access:true,cancel_at_period_end:true,
    current_period_end:'2026-08-12T12:00:00Z',can_manage:true,price_cents:1999,currency:'USD',interval:'month',
    includes_future_sets:true,max_api_keys:5,
  },
  subscription_offer:subscriptionOffer,
}, {
  catalog,
  keys:keyCollection,
  generatedKey:{api_key:'arena_one_time_secret',bot_id:'bot-new',key:{id:'key-new',key_prefix:'arena_one'}},
});
assert.match(managedHTML, /All Access/);
assert.match(managedHTML, /Access active/);
assert.match(managedHTML, /data-subscription-portal/, 'an existing provider subscription should expose customer portal management');
assert.doesNotMatch(managedHTML, /data-subscription-checkout/, 'managed subscriptions must not offer a duplicate checkout');
assert.match(managedHTML, /2 of 5 active/);
assert.match(managedHTML, /id="accountKeyForm"/);
assert.match(managedHTML, /data-account-key-revoke="key-1"/);
assert.match(managedHTML, /value="arena_one_time_secret"/, 'a newly issued key should be shown exactly once inside the authenticated Dashboard');
assert.match(managedHTML, /data-account-key-clear/);

const billingMismatchHTML = cosmetics.renderPanel({
  ...snapshot,
  subscription:{...managedSubscription,status:'billing_mismatch',has_access:false,can_manage:true},
  subscription_offer:subscriptionOffer,
}, {catalog,keys:keyCollection});
assert.match(billingMismatchHTML, /Billing needs attention/, 'a plan mismatch must clearly direct the customer to billing management');
assert.match(billingMismatchHTML, /data-subscription-portal/);

const pendingSubscriptionHTML = cosmetics.renderPanel({
  ...snapshot,
  subscription:{...managedSubscription,status:'checkout_pending',has_access:false,can_manage:false},
  subscription_offer:subscriptionOffer,
}, {catalog,keys:keyCollection});
assert.match(pendingSubscriptionHTML, /Checkout pending/, 'an resumable hosted session must not be described as ended access');
assert.match(pendingSubscriptionHTML, /data-subscription-checkout/);

const atLimitHTML = cosmetics.renderPanel(snapshot, {
  keys:{...keyCollection,active_count:5},
});
assert.match(atLimitHTML, /5 of 5 active/);
assert.match(atLimitHTML, /id="accountKeyCreate"[^>]*disabled/, 'Dashboard must disable key generation at the five-active-key limit');
const loadingKeysHTML = cosmetics.renderPanel(snapshot, {keys:null});
assert.match(loadingKeysHTML, /id="accountKeyCreate"[^>]*disabled/, 'Dashboard must not create a key before current usage is known');
const failedKeysHTML = cosmetics.renderPanel(snapshot, {keys:null,keysError:'key service offline'});
assert.match(failedKeysHTML, /id="accountKeyCreate"[^>]*disabled/, 'Dashboard must keep generation fail-closed when the key list is unavailable');

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

recentPurchaseCheck('dashboard script cache version', () => {
  assert.match(dashboardHTML, /account-cosmetics\.js\?v=20260713b/);
});
recentPurchaseCheck('long purchase data remains contained', () => {
  assert.match(dashboardHTML, /\.cosmetic-purchase-head>div\{[^}]*min-width:0/);
  assert.match(dashboardHTML, /\.cosmetic-purchase-head h3\{[^}]*overflow-wrap:anywhere/);
});

const refreshStart = dashboardHTML.indexOf('async function refreshAccountCosmetics');
const refreshEnd = dashboardHTML.indexOf('async function handleAccountPanelSubmit', refreshStart);
recentPurchaseCheck('refresh helper source', () => {
  assert.ok(refreshStart >= 0 && refreshEnd > refreshStart);
});

if (refreshStart >= 0 && refreshEnd > refreshStart) {
  const refreshCalls = [];
  const refreshRenders = [];
  let rejectOrders = null;
  const refreshSandbox = {
    accountRefreshSequence:0,
    accountSnapshot:null,
    accountCatalog:null,
    accountCatalogError:'',
    accountOrders:null,
    accountOrdersError:'',
    accountKeys:null,
    accountKeysError:'',
    accountSession:{account:{id:'acct-1',email:'owner@example.com',email_verified:true}},
    accountViewError:'',
    accountViewNotice:'',
    window:{
      ArenaAccountCosmetics:{
        accountRoute:name => ({cosmetics:'/account/cosmetics',orders:'/account/cosmetics/orders',keys:'/account/keys'})[name],
        normalizeSnapshot:value => value,
        normalizeCatalog:value => value,
        normalizeKeyCollection:value => value,
      },
    },
  };
  refreshSandbox.renderAccountCosmetics = () => {
    refreshRenders.push({
      hasInventory:Boolean(refreshSandbox.accountSnapshot),
      orders:refreshSandbox.accountOrders,
      ordersError:refreshSandbox.accountOrdersError,
    });
  };
  refreshSandbox.accountRequest = path => {
    refreshCalls.push(path);
    if (path === '/account/cosmetics/orders?limit=20') {
      return new Promise((_, reject) => {
        rejectOrders = reject;
      });
    }
    if (path === '/account/keys') return Promise.resolve(keyCollection);
    if (path === '/cosmetics/catalog') return Promise.resolve(catalog);
    return Promise.resolve({account:refreshSandbox.accountSession.account,bots:[],licenses:[]});
  };

  vm.runInNewContext(dashboardHTML.slice(refreshStart, refreshEnd), refreshSandbox, {filename:'dashboard-refresh.js'});
  const refreshPromise = refreshSandbox.refreshAccountCosmetics();
  await new Promise(resolve => setImmediate(resolve));
  recentPurchaseCheck('independent bounded fetch', () => {
    assert.ok(refreshCalls.includes('/account/cosmetics/orders?limit=20'));
    assert.ok(refreshCalls.includes('/account/keys'));
    assert.equal(refreshSandbox.accountSnapshot.account.id, 'acct-1');
    assert.equal(refreshSandbox.accountKeys.active_count, 2);
    assert.ok(refreshRenders.some(state => state.hasInventory && state.orders === null),
      'inventory must render while purchase history is still pending');
  });
  if (rejectOrders) rejectOrders(new Error('history offline'));
  await refreshPromise;
  recentPurchaseCheck('history failure leaves inventory and shop data usable', () => {
    assert.equal(refreshSandbox.accountSnapshot.account.id, 'acct-1');
    assert.equal(refreshSandbox.accountCatalog.checkout_enabled, true);
    assert.match(refreshSandbox.accountOrdersError, /history offline/);
    assert.equal(refreshSandbox.accountViewError, '');
  });
}

assert.deepEqual(recentPurchaseFailures, [], `recent purchase behavior failures:\n${recentPurchaseFailures.join('\n')}`);

console.log('account-owned dashboard cosmetics rendering and assignment rules pass');
