import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const bots = readFileSync(new URL('../frontend/js/renderer/bots.js', import.meta.url), 'utf8');
const body = readFileSync(new URL('../frontend/js/renderer/bot-body.js', import.meta.url), 'utf8');
const rig = readFileSync(new URL('../frontend/js/renderer/character-rig.js', import.meta.url), 'utf8');
const cosmetics = readFileSync(new URL('../frontend/js/renderer/cosmetics.js', import.meta.url), 'utf8');
const preview = readFileSync(new URL('../frontend/js/shop-preview.js', import.meta.url), 'utf8');

assert.match(body, /createForgeCharacter/,
  'all default combat characters must be built by the shared Forge rig');
assert.match(body, /disposeForgeCharacter/,
  'Forge character resources need their own disposal path');
assert.doesNotMatch(body, /legacyBody|swordsman-body\.js|weapons\.js|BotAnimState|createWeaponMesh/,
  'the production bot builder must not parse unreachable legacy chassis builders');
assert.doesNotMatch(bots, /swordsman-anims\.js|updateSwordsmanAnim|updateBotAnim|triggerSwordsman|triggerAttack\(/,
  'the live renderer must not retain unreachable generic or swordsman animation branches');
assert.doesNotMatch(preview, /swordsman-anims\.js|updateSwordsmanAnim|isSwordsman/,
  'the Shop preview must load only the Forge presentation path');
assert.match(bots, /character-anims\.js/,
  'live interpolation must execute the allocation-stable Forge motion system');
assert.match(bots, /entry\?\.profile\?\.weapon\s*!==\s*weaponType/,
  'a server-side weapon change must rebuild the bot into the matching chassis');
assert.match(bots, /triggerForgeAttack/);
assert.match(bots, /triggerForgeDodge/);
assert.match(bots, /triggerForgeShove/);
assert.match(bots, /updateForgeCharacter/);
assert.match(bots, /_disposeTaunt\(entry\)[\s\S]{0,180}disposeBotCosmetics\(entry\)[\s\S]{0,100}disposeBotEntry\(entry\)/,
  'weapon-change rebuilds must release GUI taunt controls before disposing their linked mesh');
assert.match(cosmetics, /entry\.mounts\?\.cosmeticRoot|entry\.mounts && entry\.mounts\.cosmeticRoot/,
  'cosmetics must follow the animated semantic mount instead of floating at the world root');
for (const mount of ['chest', 'head', 'back', 'shoulderL', 'shoulderR']) {
  assert.match(cosmetics, new RegExp(`entry\\.mounts\\?\\.${mount}|entry\\.mounts\\.${mount}`),
    `Forge cosmetics must place geometry through the ${mount} semantic mount`);
}
assert.match(cosmetics, /entry\.weapon\._forgeMeshes[\s\S]{0,140}getChildMeshes/,
  'weapon finishes must include detached left/right dagger meshes before falling back to descendants');
assert.match(rig, /cosmeticRoot\.parent\s*=\s*bodyJoint;[\s\S]{0,120}cosmeticRoot\.position\.y\s*=\s*-bodyY;/,
  'the compatibility cosmetic coordinate root must inherit body animation without changing legacy offsets');
assert.doesNotMatch(cosmetics, /function finishMesh[\s\S]{0,220}alwaysSelectAsActiveMesh\s*=\s*true/,
  'static cosmetic meshes must retain normal frustum culling');

class FakeVector3 {
  constructor(x = 0, y = 0, z = 0) { Object.assign(this, {x, y, z}); }
  set(x, y, z) { Object.assign(this, {x, y, z}); return this; }
  copyFrom(value) { return this.set(value.x, value.y, value.z); }
  clone() { return new FakeVector3(this.x, this.y, this.z); }
}
class FakeNode {
  constructor(name, scene) {
    this.name = name;
    this._scene = scene;
    this.position = new FakeVector3();
    this.rotation = new FakeVector3();
    this.scaling = new FakeVector3(1, 1, 1);
    this.parent = null;
    this.disposed = false;
  }
  getScene() { return this._scene; }
  isDisposed() { return this.disposed; }
  dispose() { this.disposed = true; }
}
class FakeMaterial {
  constructor(name, scene) { Object.assign(this, {name, scene}); }
  freeze() {}
}
class FakeColor3 extends FakeVector3 {}
const meshBuilder = new Proxy({}, {
  get: (_, key) => key.startsWith('Create')
    ? (name, _options, scene) => new FakeNode(name, scene)
    : undefined,
});
globalThis.window = {
  BABYLON: {
    Color3: FakeColor3,
    StandardMaterial: FakeMaterial,
    TransformNode: FakeNode,
    MeshBuilder: meshBuilder,
    Mesh: {CAP_ALL: 3},
    Vector3: FakeVector3,
  },
};

const {getCharacterProfile} = await import(
  new URL('../frontend/js/renderer/character-roster.js?dagger-mount-test', import.meta.url)
);
const {createForgeWeapon, disposeForgeWeapon} = await import(
  new URL('../frontend/js/renderer/forge-weapons.js?dagger-mount-test', import.meta.url)
);
const {
  BODY_FORM_NEAR_CHARACTER_LIMIT,
  bodyFormRenderState,
  cooldownActionStarted,
  selectNearBodyFormIDs,
} = await import(
  new URL('../frontend/js/renderer/bots.js?grapple-edge-test', import.meta.url)
);
const scene = {};
const mounts = {
  handL: new FakeNode('left-hand', scene),
  handR: new FakeNode('right-hand', scene),
  chest: new FakeNode('chest', scene),
};
const daggerWeapon = createForgeWeapon(
  getCharacterProfile('daggers'),
  'dagger-test',
  scene,
  mounts,
  new FakeMaterial('accent', scene),
  {handSpan: 3},
);
assert.equal(daggerWeapon._forgePoseNodes.length, 2,
  'dual daggers need one independently animated pose root per hand');
assert.equal(daggerWeapon._forgePoseNodes[0].parent, mounts.handL,
  'the left dagger must inherit the articulated left hand');
assert.equal(daggerWeapon._forgePoseNodes[1].parent, mounts.handR,
  'the right dagger must inherit the articulated right hand');
assert.ok(daggerWeapon._forgeMeshes.every(mesh => daggerWeapon._forgePoseNodes.includes(mesh.parent)),
  'every dagger mesh must live below one of the two hand roots');
disposeForgeWeapon(daggerWeapon);
assert.ok(daggerWeapon._forgePoseNodes.every(node => node.disposed),
  'disposing the logical weapon must dispose both independently parented dagger roots');

assert.equal(cooldownActionStarted('grapple', '', 4, 0, 'grapple'), true,
  'a newly accepted grapple must create one animation/effect edge');
assert.equal(cooldownActionStarted('grapple', 'grapple', 3.9, 4, 'grapple'), false,
  'repeated snapshots of the same grapple must not replay its pose or effect');
assert.equal(cooldownActionStarted('grapple', 'grapple', 4, 0, 'grapple'), true,
  'a later grapple must be detected by its cooldown rising again');

const configuredBodyFormBot = {
  bot_id: 'body-form-setting',
  cosmetics: {bot_skin: 'body_giant_chicken', trail: 'standard'},
};
assert.equal(bodyFormRenderState(configuredBodyFormBot, true).renderBot, configuredBodyFormBot,
  'enabled body forms must preserve the exact live bot snapshot');
const disabledBodyForm = bodyFormRenderState(configuredBodyFormBot, false);
assert.equal(disabledBodyForm.bodyFormKey, 'standard',
  'disabling chassis skins must suppress construction-time body geometry');
assert.equal(disabledBodyForm.renderBot.cosmetics.bot_skin, 'standard');
assert.equal(configuredBodyFormBot.cosmetics.bot_skin, 'body_giant_chicken',
  'the settings projection must not mutate authoritative server state');

assert.equal(BODY_FORM_NEAR_CHARACTER_LIMIT, 64,
  'crowd-scale body forms need an explicit bounded near-detail budget');
const bodyFormEntries = new Map();
for (let index = 0; index < 6; index += 1) {
  bodyFormEntries.set(`form-${index}`, {
    bodyFormKey: 'giant_chicken',
    root: {position: new FakeVector3(index * 10, 0, 0)},
  });
}
bodyFormEntries.set('standard', {bodyFormKey: 'standard', root: {position: new FakeVector3()}});
bodyFormEntries.set('preview', {
  bodyFormKey: 'giant_chicken', presentationOnly: true, root: {position: new FakeVector3()},
});
const nearBodyForms = selectNearBodyFormIDs(
  bodyFormEntries,
  {target: new FakeVector3()},
  3,
  'form-5',
);
assert.deepEqual([...nearBodyForms], ['form-5', 'form-0', 'form-1'],
  'the selected body form plus the closest candidates must win the bounded near-detail budget');
assert.equal(nearBodyForms.has('standard'), false,
  'standard instanced chassis must not consume the body-form budget');
assert.equal(nearBodyForms.has('preview'), false,
  'Shop and Dashboard presentation rigs must stay outside the live-crowd cap');

const corpseCompetition = new Map([
  ['hidden-corpse', {
    bodyFormKey: 'giant_chicken',
    isAlive: false,
    root: {position: new FakeVector3(0, 0, 0), isEnabled: () => false},
  }],
  ['visible-death-animation', {
    bodyFormKey: 'giant_chicken',
    isAlive: false,
    root: {position: new FakeVector3(1, 0, 0), isEnabled: () => true},
  }],
  ['living-form', {
    bodyFormKey: 'giant_chicken',
    isAlive: true,
    root: {position: new FakeVector3(20, 0, 0), isEnabled: () => true},
  }],
]);
assert.deepEqual(
  [...selectNearBodyFormIDs(corpseCompetition, {target: new FakeVector3()}, 1)],
  ['living-form'],
  'a hidden corpse or nearby death animation must not displace a living body form from the near-detail budget',
);
assert.match(bots,
  /grappleJustStarted[\s\S]{0,350}cooldownActionStarted\([\s\S]{0,220}'grapple'[\s\S]{0,700}target_position[\s\S]{0,700}triggerForgeAttack[\s\S]{0,700}onGrapple/,
  'the grapple edge must animate once and support both bot and anchor-position targets');

console.log('Forge characters are wired into live rendering, weapon changes, semantic cosmetics, and bounded culling');
