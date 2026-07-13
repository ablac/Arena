import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {runInNewContext} from 'node:vm';

const html = readFileSync(new URL('../frontend/admin/index.html', import.meta.url), 'utf8');

assert.match(html, /data\.login_enabled\s*!==\s*false/, 'disabled admin OIDC must stay hidden without a 404-driven console error');
assert.match(html, /data-tab="cosmetics"/, 'cosmetics needs a dedicated admin navigation destination');
assert.match(html, /id="panel-cosmetics"/, 'cosmetics needs its own admin panel');
const cosmeticsPanel = html.slice(html.indexOf('id="panel-cosmetics"'), html.indexOf('id="panel-controls"'));

const cosmeticWorkspaceNames = ['catalog', 'access', 'orders', 'activity'];
for (const workspace of cosmeticWorkspaceNames) {
  assert.match(cosmeticsPanel, new RegExp(`data-cosmetics-workspace-tab="${workspace}"`),
    `cosmetics needs a ${workspace} workspace tab`);
  assert.match(cosmeticsPanel, new RegExp(`data-cosmetics-workspace-panel="${workspace}"`),
    `cosmetics needs a ${workspace} workspace panel`);
}
const cosmeticWorkspaceTabTag = workspace =>
  cosmeticsPanel.match(new RegExp(`<[^>]+data-cosmetics-workspace-tab="${workspace}"[^>]*>`))?.[0] || '';
const cosmeticWorkspacePanelTag = workspace =>
  cosmeticsPanel.match(new RegExp(`<[^>]+data-cosmetics-workspace-panel="${workspace}"[^>]*>`))?.[0] || '';
assert.match(cosmeticWorkspaceTabTag('catalog'), /role="tab"/, 'workspace destinations need tab semantics');
assert.match(cosmeticWorkspaceTabTag('catalog'), /aria-selected="true"/, 'Catalog must be the default selected workspace');
assert.doesNotMatch(cosmeticWorkspacePanelTag('catalog'), /\shidden(?:\s|=|>)/, 'the default Catalog workspace must be visible');
for (const workspace of ['access', 'orders', 'activity']) {
  assert.match(cosmeticWorkspaceTabTag(workspace), /role="tab"/, `${workspace} destination needs tab semantics`);
  assert.match(cosmeticWorkspaceTabTag(workspace), /aria-selected="false"/, `${workspace} must not be selected by default`);
  assert.match(cosmeticWorkspacePanelTag(workspace), /\shidden(?:\s|=|>)/, `${workspace} workspace must be hidden by default`);
}
const cosmeticWorkspaceSwitch = html.slice(
  html.indexOf('function switchCosmeticsWorkspace'),
  html.indexOf('async function loadCosmeticsAdmin'),
);
assert.match(cosmeticWorkspaceSwitch, /dataset\.cosmeticsWorkspaceTab/, 'workspace switching must select the requested tab');
assert.match(cosmeticWorkspaceSwitch, /setAttribute\('aria-selected'/, 'workspace switching must announce the selected tab');
assert.match(cosmeticWorkspaceSwitch, /dataset\.cosmeticsWorkspacePanel/, 'workspace switching must select the requested panel');
assert.match(cosmeticWorkspaceSwitch, /\.hidden\s*=\s*!/, 'workspace switching must show only the selected panel');
assert.match(cosmeticWorkspaceSwitch, /workspace\s*===\s*'orders'[\s\S]*loadCosmeticOrders\(\)/,
  'orders must load only when their workspace is opened');
assert.match(cosmeticWorkspaceSwitch, /workspace\s*===\s*'activity'[\s\S]*loadCosmeticCatalogAudit\(\)/,
  'activity must load only when its workspace is opened');

assert.match(cosmeticsPanel, /id="cosmeticCategoryList"/, 'dedicated panel must expose category administration');
assert.match(cosmeticsPanel, /id="cosmeticPackList"/, 'dedicated panel must expose pack and price administration');
assert.match(cosmeticsPanel, /id="cosmeticItemList"/, 'dedicated panel must expose individual catalog items');
assert.match(cosmeticsPanel, /id="cosmeticCatalogAudit"/, 'catalog changes need an operator-visible audit trail');
assert.match(cosmeticsPanel, /Cosmetic Products \/ Packs/, 'the admin should group coordinated sets and one-item trail products without changing pack APIs');
assert.match(cosmeticsPanel, /id="cosmeticCatalogStatus"[^>]*role="status"[^>]*aria-live="polite"/, 'catalog refreshes need one concise live status');
for (const listID of ['cosmeticCategoryList', 'cosmeticPackList', 'cosmeticItemList']) {
  const listTag = cosmeticsPanel.match(new RegExp(`<[^>]+id="${listID}"[^>]*>`))?.[0] || '';
  assert.doesNotMatch(listTag, /aria-live=/, `${listID} must not announce every rendered row`);
}
for (const filterID of ['cosmeticItemSearch', 'cosmeticItemCategoryFilter', 'cosmeticItemSlotFilter',
  'cosmeticItemRarityFilter', 'cosmeticItemLifecycleFilter', 'cosmeticItemFilterCount']) {
  assert.match(cosmeticsPanel, new RegExp(`id="${filterID}"`), `300-item catalog needs ${filterID}`);
}
for (const pickerID of ['cosmeticPackItemSearch', 'cosmeticPackItemCategoryFilter',
  'cosmeticPackItemSlotFilter', 'cosmeticPackSelectionCount']) {
  assert.match(cosmeticsPanel, new RegExp(`id="${pickerID}"`), `set builder needs ${pickerID}`);
}
assert.match(cosmeticsPanel, /id="cosmeticCatalogMaster"/, 'catalog editing needs one master list surface');
assert.match(cosmeticsPanel, /id="cosmeticCatalogDetail"/, 'catalog editing needs one selected-record detail surface');
assert.match(cosmeticsPanel, /Built-in cosmetics are code-seeded[\s\S]*deactivate/i,
  'built-in catalog entries need deactivate-versus-delete guidance');
assert.match(cosmeticsPanel, /id="cosmeticPackPrice"[^>]*value="199"[^>]*readonly/, 'sale-ready product price must remain read-only');
assert.match(cosmeticsPanel, /id="cosmeticItemSlot"[\s\S]*?<option value="trail">Trail<\/option>/,
  'catalog item editing must expose the trail slot');
assert.match(cosmeticsPanel, /id="cosmeticPackItemSlotFilter"[\s\S]*?<option value="trail">Trails<\/option>/,
  'product contents must be filterable to trail items');
assert.match(cosmeticsPanel, /id="cosmeticItemPrice"[^>]*min="0"[^>]*max="1000000"/, 'item price must match the backend upper bound');
assert.doesNotMatch(cosmeticsPanel, /max="100000000"/, 'catalog forms must not accept prices the API always rejects');
const savePack = html.slice(html.indexOf('async function saveCosmeticPack'), html.indexOf('async function deleteCosmeticPack'));
assert.match(savePack, /price_cents:\s*isFree\s*\?\s*0\s*:\s*cosmeticPackFixedPrice\(categoryID\)/,
  'Admin must derive the fixed $1.99 set or $0.99 trail price from the server category contract');
assert.match(html, /function cosmeticPackFixedPrice[\s\S]*categoryID === 'trails' \? 99 : 199/,
  'trail products must display and submit their fixed $0.99 price');
assert.match(cosmeticsPanel, /cosmeticPackCategory" onchange="syncCosmeticPackCategoryRules\(\)"/,
  'changing product category must immediately apply its item-count and slot rules');
assert.match(cosmeticsPanel, /id="cosmeticPackRuleHint"[^>]*role="status"/,
  'the product editor must explain the one-trail rule before save');
assert.match(cosmeticsPanel, /cosmeticItemCategory" onchange="syncCosmeticItemCategoryAndSlot\('category'\)"/,
  'item category changes must keep the trail category and slot paired');
assert.match(cosmeticsPanel, /cosmeticItemSlot" onchange="syncCosmeticItemCategoryAndSlot\('slot'\)"/,
  'item slot changes must keep the trail category and slot paired');
assert.match(cosmeticsPanel, /id="cosmeticItemFree"[^>]*onchange="syncCosmeticItemPrice\(\)"/,
  'changing Free must immediately resync fixed trail price presentation');
assert.match(html, /function syncCosmeticItemPrice[\s\S]*const free = [^;]+cosmeticItemFree[^;]+;[\s\S]*price\.readOnly = trail;[\s\S]*price\.value = free \? 0 : 99/,
  'trail item reference metadata must be fixed at $0.99 in the editor');
assert.doesNotMatch(cosmeticsPanel, /<main class="cosmetics-admin-workbench"/, 'the cosmetics workbench must not nest a second main landmark');
for (const orderID of ['cosmeticOrderSearch', 'cosmeticOrderStatusFilter', 'cosmeticOrderRefresh',
  'cosmeticOrderStatus', 'cosmeticOrderList']) {
  assert.match(cosmeticsPanel, new RegExp(`id="${orderID}"`), `commerce support needs ${orderID}`);
}
assert.match(cosmeticsPanel, /id="cosmeticOrderStatus"[^>]*role="status"[^>]*aria-live="polite"/,
  'commerce order loading and failure state needs one concise announcement');
const orderListTag = cosmeticsPanel.match(/<[^>]+id="cosmeticOrderList"[^>]*>/)?.[0] || '';
assert.match(orderListTag, /aria-busy="false"/, 'orders need a programmatic loading state');
assert.doesNotMatch(orderListTag, /aria-live=/, 'rendering up to 50 orders must not announce every row');
const ordersPanel = cosmeticsPanel.slice(
  cosmeticsPanel.indexOf('data-cosmetics-workspace-panel="orders"'),
  cosmeticsPanel.indexOf('data-cosmetics-workspace-panel="activity"'),
);
assert.doesNotMatch(ordersPanel, /<button[^>]*(refund|revoke)/i,
  'commerce order support is read-only; refunds and revocations stay outside this panel');
assert.match(html, /\.cosmetics-order-list\{[^}]*max-block-size:[^;}]+;[^}]*overflow:auto/,
  'the 50-order support list must be bounded');
const controlsPanel = html.slice(html.indexOf('id="panel-controls"'));
assert.doesNotMatch(controlsPanel, /id="cosmeticGrantEmail"/, 'cosmetic fulfillment must not remain inside crowded Game Config');
assert.match(html, /id="cosmeticGrantEmail"[^>]*type="email"/, 'admin fulfillment must target the purchaser email');
assert.doesNotMatch(html, /id="cosmeticGrantBot"/, 'admin fulfillment must not make a bot the durable owner');
assert.match(html, /id="cosmeticGrantReference"[^>]*required/, 'manual grants need an idempotency reference');
const accessWorkspace = cosmeticsPanel.slice(
  cosmeticsPanel.indexOf('data-cosmetics-workspace-panel="access"'),
  cosmeticsPanel.indexOf('data-cosmetics-workspace-panel="orders"'),
);
assert.match(accessWorkspace, /id="cosmeticAccessLookupForm"/, 'Customer Access needs one focused lookup form');
assert.match(accessWorkspace, /id="cosmeticAccessEmail"[^>]*type="email"[^>]*required/, 'customer access lookup must start with a required email');
assert.match(accessWorkspace, /id="cosmeticAccessLookupStatus"[^>]*role="status"[^>]*aria-live="polite"/,
  'customer lookup needs a concise live status');
assert.match(accessWorkspace, /Cosmetics-only access/i, 'manual membership must be described as cosmetics-only access');
assert.match(accessWorkspace, /future purchasable sets and trails/i,
  'manual membership must explicitly include future purchasable sets and trails');
assert.doesNotMatch(accessWorkspace, /(?:API[- ]?key|key)\s+(?:cap|limit)|\b5\s+API[- ]?keys?/i,
  'manual cosmetics membership must not claim an API-key limit');
assert.match(accessWorkspace, /id="cosmeticMembershipDurationDays"[^>]*type="number"[^>]*min="1"/,
  'membership grants need a positive duration in days');
assert.match(accessWorkspace, /id="cosmeticMembershipExpiresAt"[^>]*type="datetime-local"/,
  'membership grants need an explicit expiry option');
assert.doesNotMatch(accessWorkspace, /<input[^>]*id="cosmeticRevokeLicense"/,
  'license revocation must not require pasting a raw UUID');
assert.doesNotMatch(accessWorkspace, /<input[^>]*id="cosmeticMembershipRevoke[^>]*>/,
  'membership revocation must not require pasting a raw UUID');

const accessLookup = html.slice(
  html.indexOf('async function lookupCosmeticCustomerAccess'),
  html.indexOf('async function grantCosmeticLicense'),
);
assert.match(accessLookup, /new URLSearchParams\(\)/, 'customer lookup must encode the email as a query parameter');
assert.match(accessLookup, /params\.set\('email',\s*email\)/, 'customer lookup must query by normalized email');
assert.match(accessLookup, /api\('\/cosmetics\/[^']+\?'\s*\+\s*params\.toString\(\)\)/,
  'customer lookup must use a protected cosmetics access endpoint');

const grant = html.slice(html.indexOf('async function grantCosmeticLicense'), html.indexOf('async function revokeCosmeticLicense'));
assert.match(grant, /if \(button\.disabled\) return;/, 'double clicks must not submit duplicate grants');
assert.match(grant, /!payload\.external_reference/, 'blank idempotency references must be rejected');
assert.match(grant, /finally\s*\{[\s\S]*button\.disabled = false;/, 'grant button must recover after success or failure');
assert.doesNotMatch(grant, /\bsource\s*:/, 'the server, not the browser, must assign the grant source');
assert.doesNotMatch(accessWorkspace, /id="cosmeticGrantSource"/, 'operators must not control the grant source');
assert.match(accessWorkspace, /id="cosmeticGrantSearch"[^>]*type="search"/, 'large grant catalogs need a searchable item picker');
assert.match(accessWorkspace, /id="cosmeticGrantItem"[^>]*disabled/, 'grant picker must stay bounded until the operator searches');
assert.match(html, /function renderCosmeticGrantPicker[\s\S]*\.slice\(0,60\)/,
  'grant search must render a bounded result set instead of 300 native options');

const revoke = html.slice(html.indexOf('async function revokeCosmeticLicense'), html.indexOf('async function grantCosmeticMembership'));
assert.match(revoke, /async function revokeCosmeticLicense\(licenseID\)/,
  'a rendered customer license must pass its ID directly to one-click revoke');
assert.match(revoke, /license_id:licenseID/, 'revocation body must carry the exact license ID');
assert.match(revoke, /confirm\('Revoke cosmetic license ' \+ licenseID/, 'exact-copy revocation needs an explicit confirmation');
assert.match(revoke, /if \(button\.disabled\) return;/, 'double clicks must not submit duplicate revocations');
assert.match(accessLookup + grant + revoke, /data-license-id="\$\{escAttr\(license\.id\)\}"/,
  'each loaded license row needs its own safe one-click revoke target');
assert.match(accessLookup + grant + revoke, /revokeCosmeticLicense\(this\.dataset\.licenseId\)/,
  'license revoke buttons must use the row ID instead of a raw UUID field');

const membershipGrant = html.slice(
  html.indexOf('async function grantCosmeticMembership'),
  html.indexOf('async function revokeCosmeticMembership'),
);
assert.match(membershipGrant, /duration_days\s*:\s*durationDays/, 'membership submission must carry the chosen duration');
assert.match(membershipGrant, /expires_at\s*:\s*expiresAt/, 'membership submission must carry the explicit expiry when supplied');
assert.match(membershipGrant, /!durationDays\s*&&\s*!expiresAt/,
  'membership submission must require a duration or explicit expiry');
assert.match(membershipGrant, /api\('\/cosmetics\/memberships',\s*\{method:'POST'/,
  'membership grants must use the protected admin membership endpoint');
assert.doesNotMatch(membershipGrant, /api[_-]?key|stripe|billing/i,
  'manual cosmetics membership must not send billing or API-key entitlement controls');

const membershipRevoke = html.slice(
  html.indexOf('async function revokeCosmeticMembership'),
  html.indexOf('async function cleanupStale'),
);
assert.match(membershipRevoke, /async function revokeCosmeticMembership\(membershipID\)/,
  'a rendered membership must pass its ID directly to one-click revoke');
assert.match(membershipRevoke, /membership_id\s*:\s*membershipID/,
  'membership revoke must target the exact loaded membership');
assert.match(membershipRevoke, /api\('\/cosmetics\/memberships',\s*\{method:'DELETE'/,
  'membership revoke must use the protected admin membership endpoint');
assert.match(accessLookup + membershipGrant + membershipRevoke, /data-membership-id="\$\{escAttr\(membership\.id\)\}"/,
  'each loaded membership needs its own safe one-click revoke target');
assert.match(accessLookup + membershipGrant + membershipRevoke, /revokeCosmeticMembership\(this\.dataset\.membershipId\)/,
  'membership revoke buttons must use the row ID instead of a raw UUID field');

const switchTab = html.slice(html.indexOf('function switchTab'), html.indexOf('// Direct tab clicks'));
assert.match(switchTab, /currentTab === 'cosmetics'[\s\S]*loadCosmeticsAdmin\(\)/, 'opening the cosmetics tab must load its catalog state');
assert.match(switchTab, /setAttribute\('aria-current', 'page'\)/, 'the active admin destination must be announced to assistive technology');

const tabSetupSource = html.slice(html.indexOf('function setupAdminTab'), html.indexOf('// Direct tab clicks'));
assert.match(tabSetupSource, /setAttribute\('role', 'button'\)/, 'admin destinations need interactive semantics');
assert.match(tabSetupSource, /setAttribute\('tabindex', '0'\)/, 'admin destinations need keyboard focus');
assert.match(tabSetupSource, /event\.key !== 'Enter' && event\.key !== ' '/, 'admin destinations need Enter and Space activation');
assert.match(html, /\.tab-direct:focus-visible,\.tab-item:focus-visible/, 'keyboard focus needs a visible nav treatment');

const tabAttributes = new Map();
const tabListeners = new Map();
const tabActivations = [];
const fakeTab = {
  dataset:{tab:'cosmetics'},
  setAttribute(name, value) { tabAttributes.set(name, value); },
  addEventListener(name, listener) { tabListeners.set(name, listener); },
};
const tabContext = {switchTab:(name, item) => tabActivations.push({name, item})};
runInNewContext(tabSetupSource, tabContext);
tabContext.setupAdminTab(fakeTab, true);
assert.equal(tabAttributes.get('role'), 'button');
assert.equal(tabAttributes.get('tabindex'), '0');
assert.equal(tabAttributes.get('aria-controls'), 'panel-cosmetics');
let prevented = 0;
let stopped = 0;
tabListeners.get('keydown')({key:'Escape', preventDefault:() => { prevented += 1; }, stopPropagation:() => { stopped += 1; }});
assert.equal(tabActivations.length, 0, 'unrelated keys must not activate a destination');
for (const key of ['Enter', ' ']) {
  tabListeners.get('keydown')({key, preventDefault:() => { prevented += 1; }, stopPropagation:() => { stopped += 1; }});
}
assert.deepEqual(tabActivations.map(entry => entry.name), ['cosmetics', 'cosmetics']);
assert.equal(prevented, 2, 'Enter and Space should prevent their default behavior');
assert.equal(stopped, 2, 'dropdown keyboard activation should not bubble into group toggles');

const catalogAdmin = html.slice(html.indexOf('async function loadCosmeticsAdmin'), html.indexOf('async function loadCosmeticCatalog()'));
assert.match(catalogAdmin, /api\('\/cosmetics\/catalog'\)/, 'admin catalog must include inactive records from its protected route');
assert.match(catalogAdmin, /\/cosmetics\/categories\//, 'category editor must use the protected category route');
assert.match(catalogAdmin, /\/cosmetics\/packs\//, 'pack editor must use the protected pack route');
assert.match(catalogAdmin, /\/cosmetics\/items\//, 'item editor must use the protected item route');
assert.doesNotMatch(catalogAdmin, /\/cosmetics\/audit\?limit=50/,
  'catalog refresh must not eagerly load Activity data');
assert.doesNotMatch(catalogAdmin, /api\(['"][^'"]*(checkout|payment|webhook)/i, 'admin catalog controls must not invent an unsigned payment path');
assert.match(catalogAdmin, /preservedPackItemIDs[\s\S]*renderCosmeticPackItemPicker\(preservedPackItemIDs\)/, 'catalog refresh must preserve an open pack editor\'s selected contents');
assert.match(catalogAdmin, /preservedPackCategory[\s\S]*packCategorySelect\.value = preservedPackCategory/, 'catalog refresh must preserve an open pack editor\'s category');
assert.match(catalogAdmin, /preservedItemCategory[\s\S]*itemCategorySelect\.value = preservedItemCategory/, 'catalog refresh must preserve an open item editor\'s category');
assert.match(catalogAdmin, /Promise\.allSettled\([\s\S]*fetchPublicCosmeticCatalog\(\)/,
  'admin load must fetch the canonical public projection without making it a hard dependency');
assert.match(catalogAdmin, /public readiness unavailable/i,
  'public projection failure must produce an explicit unknown-readiness status');
assert.match(catalogAdmin, /function cosmeticVisibilityState[\s\S]*Readiness unknown/,
  'network failure must never fall through to a Live label');
assert.match(catalogAdmin, /Blocked by pack member/,
  'sets omitted because of inactive members need a clear readiness label');
assert.match(catalogAdmin, /function filteredCosmeticAdminItems/,
  'item filtering should be a reusable render seam');
assert.match(catalogAdmin, /function resetCosmeticItemFilters/,
  'no-results filtering needs one-step recovery');
assert.match(catalogAdmin, /function toggleCosmeticPackItemSelection/,
  'pack membership must survive filters independently of rendered checkboxes');
assert.doesNotMatch(catalogAdmin, /loadCosmeticOrders\(\)/,
  'catalog refresh must not eagerly load Orders data');

const orderAdmin = html.slice(html.indexOf('function cosmeticOrderMoney'), html.indexOf('function cosmeticPriceLabel'));
assert.match(orderAdmin, /new URLSearchParams\(\)/, 'order filters must be encoded as query parameters');
assert.match(orderAdmin, /params\.set\('limit','50'\)/, 'orders must request at most 50 rows');
assert.match(orderAdmin, /\.slice\(0,50\)/, 'the client must remain bounded even if the server over-returns');
assert.match(orderAdmin, /esc\(order\.account_email[\s\S]*esc\(order\.pack_name/,
  'network-provided order identity fields must be escaped before rendering');
assert.doesNotMatch(orderAdmin, /innerHTML[^;]+query|innerHTML[^;]+statusFilter/,
  'client filter values must never be copied into rendered HTML');
assert.match(orderAdmin, /requestGeneration\s*=\s*\+\+cosmeticOrderRequestGeneration/,
  'orders need a request generation so stale responses cannot overwrite newer filters');
assert.match(orderAdmin, /requestGeneration\s*!==\s*cosmeticOrderRequestGeneration/,
  'orders must ignore stale request completions');
const auditAdmin = html.slice(html.indexOf('async function loadCosmeticCatalogAudit'), html.indexOf('async function lookupCosmeticCustomerAccess'));
assert.doesNotMatch(auditAdmin, /requestGeneration/, 'Activity loading must not depend on the Orders request generation');
assert.match(auditAdmin, /finally\s*\{\s*root\.setAttribute\('aria-busy',\s*'false'\)/,
  'Activity must always clear its own loading state');

const itemEdit = html.slice(html.indexOf('function openCosmeticItemEditor'), html.indexOf('async function saveCosmeticItem'));
assert.match(itemEdit, /scrollIntoView/,
  'opening an item editor from a long list must bring the editor into view');
assert.match(itemEdit, /openCosmeticItemEditor\('cosmeticItemName'\)/,
  'editing must focus the first editable field rather than the immutable ID');

const packEdit = html.slice(html.indexOf('function editCosmeticPack'), html.indexOf('async function saveCosmeticPack'));
assert.match(packEdit, /const selectedIDs = [^;]+;/,
  'opening a product editor must retain its selected cosmetic IDs');
assert.match(packEdit, /syncCosmeticPackCategoryRules\(selectedIDs\)/,
  'opening a product editor must synchronize trail/set picker filters before rendering retained contents');

assert.match(html, /\.cosmetics-item-list\{[^}]*max-block-size:[^;}]+;[^}]*overflow:auto/,
  'the 300-item list must have a bounded scroll region');
assert.match(html, /\.cosmetics-item-picker\{[^}]*max-block-size:[^;}]+;[^}]*overflow:auto/,
  'the 300-item set picker must have a bounded scroll region');
assert.doesNotMatch(html.slice(html.indexOf('async function loadOpsConsole'), html.indexOf('async function loadBroadcasts')),
  /loadCosmeticCatalog\(\)/, 'Game Config must not load the hidden cosmetics catalog');

const deleteEntity = html.slice(html.indexOf('async function deleteCosmeticEntity'), html.indexOf('function deleteCosmeticCategory'));
assert.match(deleteEntity, /const data = await api\(/, 'delete must inspect the server response');
assert.match(deleteEntity, /data\?\.deleted !== true/, 'deleted:false must not be reported as success');
assert.match(deleteEntity, /was unchanged[\s\S]*return;[\s\S]*logAudit\(/, 'unchanged deletes must return before success audit logging');

assert.match(html, /@media\(max-width:980px\)\{[\s\S]*#panel-cosmetics button,[\s\S]*min-height:44px[\s\S]*font-size:16px/, 'phone and tablet cosmetics controls need 44px targets and 16px fields');
assert.match(html, /@media\(pointer:coarse\)\{[\s\S]*#panel-cosmetics button,[\s\S]*min-height:44px[\s\S]*font-size:16px/, 'coarse pointers need touch-safe cosmetics controls regardless of viewport width');
assert.match(html, /@media\(max-width:980px\)\{[\s\S]*\.cosmetics-filter-toolbar :is\(input,select\),[\s\S]*min-height:44px;font-size:16px/,
  'item and set filters need touch-safe native controls on tablet and phone widths');

// Exercise the two stateful failure paths with a tiny DOM seam. This catches
// regressions that source-only assertions cannot, without booting Arena.
const elements = new Map();
function element(id, initial = {}) {
  const value = {style:{}, textContent:'', value:'', open:false, _innerHTML:'', ...initial};
  Object.defineProperty(value, 'innerHTML', {
    get() { return this._innerHTML; },
    set(markup) {
      this._innerHTML = String(markup);
      if (this.resetSelectionOnRender) this.value = '';
    },
  });
  elements.set(id, value);
  return value;
}
for (const id of ['cosmeticCategoryCount', 'cosmeticPackCount', 'cosmeticItemCount',
  'cosmeticCheckoutState', 'cosmeticCatalogStatus', 'cosmeticCategoryList', 'cosmeticPackList',
  'cosmeticItemList', 'cosmeticItemFilterCount', 'cosmeticPackItems', 'cosmeticPackSelectionCount',
  'cosmeticPackRuleHint']) element(id);
element('cosmeticPackEditor', {open:true});
element('cosmeticItemEditor', {open:true});
const packCategory = element('cosmeticPackCategory', {value:'category-4', resetSelectionOnRender:true});
const itemCategory = element('cosmeticItemCategory', {value:'category-1', resetSelectionOnRender:true});
element('cosmeticItemSearch', {value:''});
element('cosmeticItemCategoryFilter', {value:'all', resetSelectionOnRender:true});
element('cosmeticItemSlotFilter', {value:'all'});
element('cosmeticItemRarityFilter', {value:'all', resetSelectionOnRender:true});
element('cosmeticItemLifecycleFilter', {value:'all'});
element('cosmeticPackItemSearch', {value:''});
element('cosmeticPackItemCategoryFilter', {value:'all', resetSelectionOnRender:true});
element('cosmeticPackItemSlotFilter', {value:'all'});
const largeCategories = Array.from({length:10}, (_, index) => ({
  id:`category-${index}`,
  name:`Category ${index}`,
  is_active:index !== 9,
  sort_order:index * 10,
  is_builtin:index < 3,
}));
const slots = ['bot_skin', 'weapon_skin', 'attachment'];
const rarities = ['common', 'rare', 'epic'];
const largeItems = Array.from({length:300}, (_, index) => ({
  id:`item-${String(index).padStart(3, '0')}`,
  name:`Cosmetic ${String(index).padStart(3, '0')}`,
  description:`Catalog item ${index}`,
  category_id:`category-${index % 10}`,
  slot:slots[index % slots.length],
  asset_key:`asset_${index}`,
  rarity:rarities[index % rarities.length],
  is_active:index % 11 !== 0,
  is_free:index % 20 === 0,
  is_purchasable:index % 20 !== 0,
  price_cents:99,
  sort_order:index,
  is_builtin:index < 9,
}));
const largePacks = Array.from({length:100}, (_, index) => ({
  id:`pack-${String(index).padStart(3, '0')}`,
  name:`Set ${String(index).padStart(3, '0')}`,
  category_id:`category-${index % 10}`,
  item_ids:[largeItems[index * 3].id, largeItems[index * 3 + 1].id, largeItems[index * 3 + 2].id],
  is_active:true,
  is_purchasable:true,
  price_cents:199,
  currency:'USD',
  sort_order:index,
}));
const publicItems = largeItems.filter(item => item.is_active && item.category_id !== 'category-9');
const publicPacks = largePacks.filter(pack => pack.category_id !== 'category-9' &&
  pack.item_ids.every(id => publicItems.some(item => item.id === id)));
const renderContext = {
  cosmeticAdminCatalog: {
    checkout_enabled:false,
    categories:largeCategories,
    packs:largePacks,
    items:largeItems,
  },
  cosmeticPublicProjection: {
    available:true,
    categories:new Set(largeCategories.filter(category => category.is_active).map(category => category.id)),
    packs:new Set(publicPacks.map(pack => pack.id)),
    items:new Set(publicItems.map(item => item.id)),
  },
  cosmeticPackSelection:new Set(['item-001', 'item-250']),
  document: {
    getElementById(id) { return elements.get(id); },
  },
  esc:value => String(value),
  escAttr:value => String(value),
  prettyLabel:value => String(value).replaceAll('_', ' '),
  cosmeticPriceLabel:() => '$0.99',
  cosmeticCatalogState:() => 'preview',
};
const renderSource = html.slice(html.indexOf('function cosmeticVisibilityState'), html.indexOf('function setCosmeticFormBusy'));
runInNewContext(renderSource, renderContext);
renderContext.renderCosmeticAdminCatalog();
assert.equal(packCategory.value, 'category-4', 'refresh must retain an open pack editor category');
assert.equal(itemCategory.value, 'category-1', 'refresh must retain an open item editor category');
assert.match(elements.get('cosmeticItemFilterCount').textContent, /Showing 300 of 300 items/,
  'the unfiltered 300-item fixture needs a concise visible count');
assert.match(elements.get('cosmeticItemList').innerHTML, /Built-in/, 'upcoming is_builtin entries should be visibly labelled');
assert.match(elements.get('cosmeticItemList').innerHTML, /Live/, 'canonical public IDs should be labelled Live');
assert.match(elements.get('cosmeticItemList').innerHTML, /Inactive/, 'inactive entries should be labelled clearly');
assert.match(elements.get('cosmeticItemList').innerHTML, /Hidden by category/, 'active items under inactive categories need the correct reason');
assert.match(elements.get('cosmeticPackList').innerHTML, /Blocked by pack member/, 'sets with omitted members need the correct reason');
assert.match(elements.get('cosmeticPackItems').innerHTML, /value="item-001" checked/, 'refresh must retain the first checked pack member');
assert.match(elements.get('cosmeticPackItems').innerHTML, /value="item-250" checked/, 'refresh must retain checked members outside the first catalog page');
assert.match(elements.get('cosmeticPackSelectionCount').textContent, /2 selected/, 'set picker must report all selections');
assert.ok(elements.get('cosmeticPackItems').innerHTML.indexOf('item-001') < elements.get('cosmeticPackItems').innerHTML.indexOf('item-000'),
  'selected set members should render before unselected items');
const pickerMarkupBeforeToggle = elements.get('cosmeticPackItems').innerHTML;
renderContext.toggleCosmeticPackItemSelection({value:'item-002', checked:true});
assert.equal(renderContext.cosmeticPackSelection.has('item-002'), true);
assert.equal(elements.get('cosmeticPackItems').innerHTML, pickerMarkupBeforeToggle,
  'checking a set member must preserve keyboard focus by avoiding a full picker rerender');
assert.match(elements.get('cosmeticPackSelectionCount').textContent, /3 selected/);

elements.get('cosmeticItemSearch').value = 'item-299';
renderContext.renderCosmeticAdminCatalog();
assert.match(elements.get('cosmeticItemFilterCount').textContent, /Showing 1 of 300 items/);
assert.match(elements.get('cosmeticItemList').innerHTML, /item-299/);
assert.doesNotMatch(elements.get('cosmeticItemList').innerHTML, /item-001/);
elements.get('cosmeticItemSearch').value = 'not-in-the-catalog';
renderContext.renderCosmeticAdminCatalog();
assert.match(elements.get('cosmeticItemList').innerHTML, /No items match/i, 'zero results need a recoverable empty state');
renderContext.resetCosmeticItemFilters();
assert.equal(elements.get('cosmeticItemSearch').value, '');
assert.match(elements.get('cosmeticItemFilterCount').textContent, /Showing 300 of 300 items/,
  'clearing filters should recover the full catalog');

elements.get('cosmeticItemCategoryFilter').value = 'category-3';
renderContext.renderCosmeticAdminCatalog();
assert.match(elements.get('cosmeticItemFilterCount').textContent, /Showing 30 of 300 items/,
  'category filter should bound the working set');
renderContext.resetCosmeticItemFilters();
elements.get('cosmeticItemSlotFilter').value = 'weapon_skin';
renderContext.renderCosmeticAdminCatalog();
assert.match(elements.get('cosmeticItemFilterCount').textContent, /Showing 100 of 300 items/,
  'slot filter should bound the working set');
renderContext.resetCosmeticItemFilters();
elements.get('cosmeticItemRarityFilter').value = 'epic';
renderContext.renderCosmeticAdminCatalog();
assert.match(elements.get('cosmeticItemFilterCount').textContent, /Showing 100 of 300 items/,
  'rarity filter should bound the working set');
renderContext.resetCosmeticItemFilters();
elements.get('cosmeticItemLifecycleFilter').value = 'hidden-category';
renderContext.renderCosmeticAdminCatalog();
const hiddenCategoryItems = largeItems.filter(item => item.category_id === 'category-9' && item.is_active).length;
assert.match(elements.get('cosmeticItemFilterCount').textContent, new RegExp(`Showing ${hiddenCategoryItems} of 300 items`),
  'lifecycle filter should use canonical readiness reasons');
renderContext.resetCosmeticItemFilters();

elements.get('cosmeticPackItemSearch').value = 'item-250';
renderContext.renderCosmeticPackItemPicker();
assert.match(elements.get('cosmeticPackItems').innerHTML, /item-250/);
assert.doesNotMatch(elements.get('cosmeticPackItems').innerHTML, /item-001/);
assert.match(elements.get('cosmeticPackSelectionCount').textContent, /3 selected/, 'hidden selections must survive picker filtering');
elements.get('cosmeticPackItemSearch').value = '';
elements.get('cosmeticPackItemCategoryFilter').value = 'category-1';
renderContext.renderCosmeticPackItemPicker();
assert.match(elements.get('cosmeticPackSelectionCount').textContent, /30 shown of 300 items/,
  'set picker category filtering should scale independently of selection state');
elements.get('cosmeticPackItemCategoryFilter').value = 'all';
elements.get('cosmeticPackItemSlotFilter').value = 'attachment';
renderContext.renderCosmeticPackItemPicker();
assert.match(elements.get('cosmeticPackSelectionCount').textContent, /100 shown of 300 items/,
  'set picker slot filtering should scale independently of selection state');
renderContext.resetCosmeticPackItemFilters();
assert.match(elements.get('cosmeticPackSelectionCount').textContent, /300 shown of 300 items/);

renderContext.cosmeticPublicProjection = {available:false, categories:new Set(), packs:new Set(), items:new Set()};
elements.get('cosmeticItemSearch').value = '';
renderContext.renderCosmeticAdminCatalog();
assert.match(elements.get('cosmeticItemList').innerHTML, /Readiness unknown/);
assert.doesNotMatch(elements.get('cosmeticItemList').innerHTML, />Live</,
  'failed public projection must not falsely claim any item is Live');

renderContext.cosmeticAdminCatalog.items.push(
  {id:'trail-one',name:'Trail One',category_id:'trails',slot:'trail',asset_key:'trail_one'},
  {id:'trail-two',name:'Trail Two',category_id:'trails',slot:'trail',asset_key:'trail_two'},
);
packCategory.value = 'trails';
elements.get('cosmeticPackItemSlotFilter').value = 'all';
renderContext.renderCosmeticPackItemPicker(['item-001', 'trail-one']);
assert.deepEqual(Array.from(renderContext.cosmeticPackSelection), ['trail-one'],
  'switching a product to Trails must discard incompatible set members');
assert.match(elements.get('cosmeticPackItems').innerHTML, /trail-one/);
assert.doesNotMatch(elements.get('cosmeticPackItems').innerHTML, /item-001/);
assert.match(elements.get('cosmeticPackRuleHint').textContent, /exactly one trail item/i);
renderContext.toggleCosmeticPackItemSelection({value:'trail-two',checked:true});
assert.deepEqual(Array.from(renderContext.cosmeticPackSelection), ['trail-two'],
  'selecting another trail must replace the prior one-item product selection');

const itemSyncElements = new Map([
  ['cosmeticItemCategory', {value:'trails',dataset:{lockedCategory:''}}],
  ['cosmeticItemSlot', {value:'bot_skin',disabled:false}],
  ['cosmeticItemPrice', {value:0,readOnly:false}],
]);
const itemSyncContext = {document:{getElementById:id => itemSyncElements.get(id)}};
const itemSyncSource = html.slice(
  html.indexOf('function syncCosmeticItemCategoryAndSlot'),
  html.indexOf('function cosmeticVisibilityState'),
);
runInNewContext(itemSyncSource, itemSyncContext);
itemSyncContext.syncCosmeticItemCategoryAndSlot('category');
assert.equal(itemSyncElements.get('cosmeticItemSlot').value, 'trail');
assert.equal(itemSyncElements.get('cosmeticItemPrice').value, 99);
assert.equal(itemSyncElements.get('cosmeticItemPrice').readOnly, true);
itemSyncElements.get('cosmeticItemSlot').value = 'attachment';
itemSyncContext.syncCosmeticItemCategoryAndSlot('slot');
assert.equal(itemSyncElements.get('cosmeticItemCategory').value, 'attachments');
assert.equal(itemSyncElements.get('cosmeticItemPrice').readOnly, false);
itemSyncElements.get('cosmeticItemCategory').value = 'trails';
itemSyncContext.syncCosmeticItemCategoryAndSlot('category');
assert.equal(itemSyncElements.get('cosmeticItemSlot').value, 'trail');
itemSyncElements.get('cosmeticItemSlot').disabled = true;
itemSyncElements.get('cosmeticItemSlot').value = 'attachment';
itemSyncElements.get('cosmeticItemCategory').dataset.lockedCategory = 'attachments';
itemSyncElements.get('cosmeticItemCategory').value = 'trails';
itemSyncContext.syncCosmeticItemCategoryAndSlot('category');
assert.equal(itemSyncElements.get('cosmeticItemCategory').value, 'attachments',
  'an existing item cannot cross the immutable trail-slot boundary by editing its category');
assert.equal(itemSyncElements.get('cosmeticItemSlot').value, 'attachment');

const packSyncElements = new Map([
  ['cosmeticPackItemSlotFilter', {value:'all'}],
  ['cosmeticPackItemCategoryFilter', {value:'all'}],
]);
let packSyncTrail = true;
const packSyncContext = {
  cosmeticFilterValue:() => packSyncTrail ? 'trails' : 'starter-packs',
  document:{getElementById:id => packSyncElements.get(id)},
  updateCosmeticPackFixedPrice() {},
  renderCosmeticPackItemPicker() {},
};
const packSyncSource = html.slice(
  html.indexOf('function syncCosmeticPackCategoryRules'),
  html.indexOf('function resetCosmeticPackItemFilters'),
);
runInNewContext(packSyncSource, packSyncContext);
packSyncContext.syncCosmeticPackCategoryRules();
assert.equal(packSyncElements.get('cosmeticPackItemSlotFilter').value, 'trail');
assert.equal(packSyncElements.get('cosmeticPackItemCategoryFilter').value, 'trails');
packSyncTrail = false;
packSyncContext.syncCosmeticPackCategoryRules();
assert.equal(packSyncElements.get('cosmeticPackItemSlotFilter').value, 'all');
assert.equal(packSyncElements.get('cosmeticPackItemCategoryFilter').value, 'all');

const editor = element('focusEditor', {open:false, scrollIntoView(){ this.scrolled = true; }});
const editable = element('focusName', {focus(){ this.focused = true; }});
const focusContext = {
  document:{getElementById(id) { return id === 'focusEditor' ? editor : editable; }},
  requestAnimationFrame:callback => callback(),
};
const openEditorSource = html.slice(html.indexOf('function openCosmeticItemEditor'), html.indexOf('function resetCosmeticItemForm'));
runInNewContext(openEditorSource, focusContext);
focusContext.openCosmeticItemEditor('focusName', 'focusEditor');
assert.equal(editor.open, true);
assert.equal(editor.scrolled, true, 'editor should scroll into view after a row edit');
assert.equal(editable.focused, true, 'editor should focus its first editable field');

const deleteResult = {style:{}, textContent:''};
const localAudit = [];
let reloads = 0;
let deleteResponse = {deleted:false};
const deleteContext = {
  confirm:() => true,
  document:{getElementById:() => deleteResult},
  api:async () => deleteResponse,
  prettyLabel:value => value,
  toast:() => {},
  logAudit:(action, id) => localAudit.push({action, id}),
  loadCosmeticsAdmin:async () => { reloads += 1; },
  encodeURIComponent,
};
runInNewContext(deleteEntity, deleteContext);
await deleteContext.deleteCosmeticEntity('pack', 'starter-pack', 'cosmeticPackResult');
assert.match(deleteResult.textContent, /was not deleted/, 'deleted:false must show an unchanged result');
assert.equal(localAudit.length, 0, 'deleted:false must not create a success audit event');
assert.equal(reloads, 1, 'unchanged delete should refresh authoritative state');
deleteResponse = {deleted:true};
await deleteContext.deleteCosmeticEntity('pack', 'starter-pack', 'cosmeticPackResult');
assert.match(deleteResult.textContent, /deleted:/, 'deleted:true should show success');
assert.equal(localAudit.length, 1, 'only a confirmed deletion should create a success audit event');

const orderElements = new Map([
  ['cosmeticOrderSearch', {value:'<img src=x onerror=alert(1)>'}],
  ['cosmeticOrderStatusFilter', {value:'paid'}],
  ['cosmeticOrderStatus', {textContent:'', style:{}}],
  ['cosmeticOrderList', {
    textContent:'', innerHTML:'', attributes:new Map(),
    setAttribute(name, value) { this.attributes.set(name, String(value)); },
  }],
]);
const escaped = value => String(value ?? '')
  .replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
  .replaceAll('"', '&quot;').replaceAll("'", '&#39;');
const orderFixture = Array.from({length:55}, (_, index) => ({
  id:`order-${index}<img src=x onerror=alert(1)>`,
  account_email:index === 0 ? 'buyer<img src=x onerror=alert(1)>@example.com' : `buyer${index}@example.com`,
  pack_name:index === 0 ? 'Launch <script>alert(1)</script> Set' : `Launch Set ${index}`,
  quantity:2,
  currency:'usd',
  expected_subtotal_cents:1000,
  amount_received_cents:1000,
  amount_refunded_cents:index === 1 ? 250 : 0,
  status:'paid',
  checkout_session_id:`cs_test_${String(index).padStart(4, '0')}_full_identifier`,
  payment_intent_id:`pi_test_${String(index).padStart(4, '0')}_full_identifier`,
  fulfilled_license_count:2,
  created_at:'2026-07-11T12:00:00Z',
  updated_at:'2026-07-11T12:05:00Z',
}));
let orderAPIResult = {orders:orderFixture};
let orderPath = '';
const orderContext = {
  cosmeticOrderRequestGeneration:0,
  document:{getElementById:id => orderElements.get(id)},
  api:async path => {
    orderPath = path;
    if (orderAPIResult instanceof Error) throw orderAPIResult;
    return orderAPIResult;
  },
  URLSearchParams,
  Intl,
  esc:escaped,
  escAttr:escaped,
  prettyLabel:value => String(value).replaceAll('_', ' '),
  fmtTime:value => String(value),
};
runInNewContext(orderAdmin, orderContext);
await orderContext.loadCosmeticOrders({preventDefault(){}});
const orderParams = new URLSearchParams(orderPath.slice(orderPath.indexOf('?') + 1));
assert.equal(orderPath.startsWith('/cosmetics/orders?'), true);
assert.equal(orderParams.get('query'), '<img src=x onerror=alert(1)>', 'search text must round-trip only through URL encoding');
assert.equal(orderParams.get('status'), 'paid', 'status filter must be sent independently');
assert.equal(orderParams.get('limit'), '50');
assert.equal((orderElements.get('cosmeticOrderList').innerHTML.match(/<article/g) || []).length, 50,
  'an over-returning endpoint must still render at most 50 compact rows');
assert.doesNotMatch(orderElements.get('cosmeticOrderList').innerHTML, /<(?:img|script)\b/i,
  'untrusted order data must not render executable HTML');
assert.match(orderElements.get('cosmeticOrderList').innerHTML, /&lt;script&gt;/,
  'untrusted order data should remain visible as escaped support text');
assert.match(orderElements.get('cosmeticOrderList').innerHTML, /10\.00/,
  'minor-unit amounts need operator-readable currency formatting');
assert.match(orderElements.get('cosmeticOrderList').innerHTML, /title="cs_test_0000_full_identifier"/,
  'terse provider IDs need the full copy-safe value in the title');
assert.match(orderElements.get('cosmeticOrderStatus').textContent, /Showing 50 of 55 orders/);
assert.equal(orderElements.get('cosmeticOrderList').attributes.get('aria-busy'), 'false');

orderAPIResult = {orders:[]};
await orderContext.loadCosmeticOrders();
assert.match(orderElements.get('cosmeticOrderList').textContent, /No commerce orders/i,
  'empty checkout history is a normal, explicit state');
orderAPIResult = new Error('checkout disabled');
await orderContext.loadCosmeticOrders();
assert.match(orderElements.get('cosmeticOrderList').textContent, /Orders unavailable/i,
  'orders failure needs a local error state instead of rejecting catalog load');
assert.match(orderElements.get('cosmeticOrderStatus').textContent, /Catalog editing is unaffected/i);
assert.equal(orderElements.get('cosmeticOrderList').attributes.get('aria-busy'), 'false');

const pendingOrders = [];
orderContext.api = path => new Promise(resolve => pendingOrders.push({path, resolve}));
orderElements.get('cosmeticOrderSearch').value = '';
const staleOrderLoad = orderContext.loadCosmeticOrders();
orderElements.get('cosmeticOrderSearch').value = 'newest filter';
const newestOrderLoad = orderContext.loadCosmeticOrders();
pendingOrders[0].resolve({orders:orderFixture});
await staleOrderLoad;
assert.equal(orderElements.get('cosmeticOrderList').attributes.get('aria-busy'), 'true',
  'a stale request must not clear the newest request loading state');
pendingOrders[1].resolve({orders:[]});
await newestOrderLoad;
assert.match(orderElements.get('cosmeticOrderList').textContent, /No commerce orders/i,
  'a stale unfiltered order response must not overwrite the newest filter state');
assert.equal(orderElements.get('cosmeticOrderList').attributes.get('aria-busy'), 'false');

console.log('admin cosmetic fulfillment is email-owned, idempotent, and exact-copy revocable');
