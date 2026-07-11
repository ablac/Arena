import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {runInNewContext} from 'node:vm';

const html = readFileSync(new URL('../frontend/admin/index.html', import.meta.url), 'utf8');
assert.match(html, /data-tab="cosmetics"/, 'cosmetics needs a dedicated admin navigation destination');
assert.match(html, /id="panel-cosmetics"/, 'cosmetics needs its own admin panel');
const cosmeticsPanel = html.slice(html.indexOf('id="panel-cosmetics"'), html.indexOf('id="panel-controls"'));
assert.match(cosmeticsPanel, /id="cosmeticCategoryList"/, 'dedicated panel must expose category administration');
assert.match(cosmeticsPanel, /id="cosmeticPackList"/, 'dedicated panel must expose pack and price administration');
assert.match(cosmeticsPanel, /id="cosmeticItemList"/, 'dedicated panel must expose individual catalog items');
assert.match(cosmeticsPanel, /id="cosmeticCatalogAudit"/, 'catalog changes need an operator-visible audit trail');
assert.match(cosmeticsPanel, /id="cosmeticPackPrice"[^>]*min="0"[^>]*max="1000000"/, 'pack price must match the backend upper bound');
assert.match(cosmeticsPanel, /id="cosmeticItemPrice"[^>]*min="0"[^>]*max="1000000"/, 'item price must match the backend upper bound');
assert.doesNotMatch(cosmeticsPanel, /max="100000000"/, 'catalog forms must not accept prices the API always rejects');
assert.doesNotMatch(cosmeticsPanel, /<main class="cosmetics-admin-workbench"/, 'the cosmetics workbench must not nest a second main landmark');
const controlsPanel = html.slice(html.indexOf('id="panel-controls"'));
assert.doesNotMatch(controlsPanel, /id="cosmeticGrantEmail"/, 'cosmetic fulfillment must not remain inside crowded Game Config');
assert.match(html, /id="cosmeticGrantEmail"[^>]*type="email"/, 'admin fulfillment must target the purchaser email');
assert.doesNotMatch(html, /id="cosmeticGrantBot"/, 'admin fulfillment must not make a bot the durable owner');
assert.match(html, /id="cosmeticGrantReference"[^>]*required/, 'manual grants need an idempotency reference');
assert.match(html, /id="cosmeticRevokeLicense"/, 'revocation must target one exact purchased copy');

const grant = html.slice(html.indexOf('async function grantCosmeticLicense'), html.indexOf('async function revokeCosmeticLicense'));
assert.match(grant, /if \(button\.disabled\) return;/, 'double clicks must not submit duplicate grants');
assert.match(grant, /!payload\.external_reference/, 'blank idempotency references must be rejected');
assert.match(grant, /finally\s*\{[\s\S]*button\.disabled = false;/, 'grant button must recover after success or failure');

const revoke = html.slice(html.indexOf('async function revokeCosmeticLicense'), html.indexOf('async function cleanupStale'));
assert.match(revoke, /license_id:licenseID/, 'revocation body must carry the exact license ID');
assert.match(revoke, /confirm\('Revoke cosmetic license ' \+ licenseID/, 'exact-copy revocation needs an explicit confirmation');
assert.match(revoke, /if \(button\.disabled\) return;/, 'double clicks must not submit duplicate revocations');

const switchTab = html.slice(html.indexOf('function switchTab'), html.indexOf('// Direct tab clicks'));
assert.match(switchTab, /currentTab === 'cosmetics'[\s\S]*loadCosmeticsAdmin\(\)/, 'opening the cosmetics tab must load its catalog state');

const catalogAdmin = html.slice(html.indexOf('async function loadCosmeticsAdmin'), html.indexOf('async function loadCosmeticCatalog()'));
assert.match(catalogAdmin, /api\('\/cosmetics\/catalog'\)/, 'admin catalog must include inactive records from its protected route');
assert.match(catalogAdmin, /\/cosmetics\/categories\//, 'category editor must use the protected category route');
assert.match(catalogAdmin, /\/cosmetics\/packs\//, 'pack editor must use the protected pack route');
assert.match(catalogAdmin, /\/cosmetics\/items\//, 'item editor must use the protected item route');
assert.match(catalogAdmin, /\/cosmetics\/audit\?limit=50/, 'catalog audit must be loaded from the protected audit route');
assert.doesNotMatch(catalogAdmin, /api\(['"][^'"]*(checkout|payment|webhook)/i, 'admin catalog controls must not invent an unsigned payment path');
assert.match(catalogAdmin, /preservedPackItemIDs[\s\S]*renderCosmeticPackItemPicker\(preservedPackItemIDs\)/, 'catalog refresh must preserve an open pack editor\'s selected contents');
assert.match(catalogAdmin, /preservedPackCategory[\s\S]*packCategorySelect\.value = preservedPackCategory/, 'catalog refresh must preserve an open pack editor\'s category');
assert.match(catalogAdmin, /preservedItemCategory[\s\S]*itemCategorySelect\.value = preservedItemCategory/, 'catalog refresh must preserve an open item editor\'s category');

const deleteEntity = html.slice(html.indexOf('async function deleteCosmeticEntity'), html.indexOf('function deleteCosmeticCategory'));
assert.match(deleteEntity, /const data = await api\(/, 'delete must inspect the server response');
assert.match(deleteEntity, /data\?\.deleted !== true/, 'deleted:false must not be reported as success');
assert.match(deleteEntity, /was unchanged[\s\S]*return;[\s\S]*logAudit\(/, 'unchanged deletes must return before success audit logging');

assert.match(html, /@media\(max-width:980px\)\{[\s\S]*#panel-cosmetics button,[\s\S]*min-height:44px[\s\S]*font-size:16px/, 'phone and tablet cosmetics controls need 44px targets and 16px fields');
assert.match(html, /@media\(pointer:coarse\)\{[\s\S]*#panel-cosmetics button,[\s\S]*min-height:44px[\s\S]*font-size:16px/, 'coarse pointers need touch-safe cosmetics controls regardless of viewport width');

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
  'cosmeticCheckoutState', 'cosmeticCategoryList', 'cosmeticPackList', 'cosmeticItemList',
  'cosmeticPackItems']) element(id);
element('cosmeticPackEditor', {open:true});
element('cosmeticItemEditor', {open:true});
const packCategory = element('cosmeticPackCategory', {value:'starter-packs', resetSelectionOnRender:true});
const itemCategory = element('cosmeticItemCategory', {value:'chassis', resetSelectionOnRender:true});
const renderContext = {
  cosmeticAdminCatalog: {
    checkout_enabled:false,
    categories:[
      {id:'chassis', name:'Chassis', is_active:true, sort_order:10},
      {id:'starter-packs', name:'Starter Packs', is_active:true, sort_order:40},
    ],
    packs:[],
    items:[{id:'skin-neon-grid', name:'Neon Grid', slot:'bot_skin', category_id:'chassis', rarity:'rare', is_active:true}],
  },
  document: {
    getElementById(id) { return elements.get(id); },
    querySelectorAll(selector) {
      assert.equal(selector, '[data-cosmetic-pack-item]:checked');
      return [{value:'skin-neon-grid'}];
    },
  },
  esc:value => String(value),
  escAttr:value => String(value),
  cosmeticPriceLabel:() => '$0.99',
  cosmeticCatalogState:() => 'preview',
};
const renderSource = html.slice(html.indexOf('function renderCosmeticAdminCatalog'), html.indexOf('function setCosmeticFormBusy'));
runInNewContext(renderSource, renderContext);
renderContext.renderCosmeticAdminCatalog();
assert.equal(packCategory.value, 'starter-packs', 'refresh must retain an open pack editor category');
assert.equal(itemCategory.value, 'chassis', 'refresh must retain an open item editor category');
assert.match(elements.get('cosmeticPackItems').innerHTML, /value="skin-neon-grid" checked/, 'refresh must retain checked pack members');

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

console.log('admin cosmetic fulfillment is email-owned, idempotent, and exact-copy revocable');
