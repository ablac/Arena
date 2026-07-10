import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const html = readFileSync(new URL('../frontend/admin/index.html', import.meta.url), 'utf8');
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

console.log('admin cosmetic fulfillment is email-owned, idempotent, and exact-copy revocable');
