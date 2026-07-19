import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

function dataModule(source) {
  return `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
}

const bodyFormRosterURL = new URL(
  '../frontend/js/renderer/body-form-roster.js?cosmetics-shop-preview-test',
  import.meta.url,
).href;

const botBodySource = readFileSync(new URL('../frontend/js/renderer/bot-body.js', import.meta.url), 'utf8');
const cosmeticsSource = readFileSync(new URL('../frontend/js/renderer/cosmetics.js', import.meta.url), 'utf8');
const previewSource = readFileSync(new URL('../frontend/js/shop-preview.js', import.meta.url), 'utf8');

assert.match(botBodySource, /createBotEntry\(bot, scene, options = \{\}\)/,
  'bot entry builder must accept backwards-compatible presentation options');
assert.match(botBodySource, /createForgeCharacter\(bot, scene/,
  'presentation options must reach the production Forge builder');
assert.equal((botBodySource.match(/\.getScene\(\) !== scene/g) || []).length >= 1, true,
  'the shared shadow template must never cross Babylon scenes');
assert.match(cosmeticsSource, /forceEnabled/,
  'cosmetic application must expose an explicit shop-preview override');
assert.doesNotMatch(botBodySource, /swordsman-body\.js|weapons\.js|animations\.js/,
  'the Shop entry path must not load retired character systems');
assert.match(previewSource, /bot-body\.js\?v=20260718o/);
assert.match(previewSource, /cosmetics\.js\?v=20260714e/);
assert.doesNotMatch(previewSource, /swordsman-anims\.js|updateSwordsmanAnim|isSwordsman/,
  'preview must execute only the Forge presentation path');
assert.match(previewSource, /character-anims\.js\?v=20260714e/,
  'preview must use the allocation-stable Forge animation module');
assert.match(previewSource, /trails\.js\?v=20260714e/,
  'preview must use the same bounded cosmetic trail renderer as the live arena');
assert.match(previewSource, /const DEFAULT_RADIUS = 46;/,
  'the shared Shop/Dashboard camera must leave headroom for the tallest body forms');
assert.match(previewSource, /const DEFAULT_TARGET_Y = 11;/,
  'the shared preview target must keep complete body forms vertically centered');
assert.doesNotMatch(previewSource, /ArenaEngine|BotRenderer/,
  'shop preview must not construct the live spectator renderer');

// Prove the override changes real cosmetic behavior while the persisted viewer
// setting is off. This follows the same fake-Babylon seam as the renderer test.
class FakeColor3 {
  constructor(r, g, b) { this.r = r; this.g = g; this.b = b; }
  clone() { return new FakeColor3(this.r, this.g, this.b); }
  scale(value) { return new FakeColor3(this.r * value, this.g * value, this.b * value); }
  static White() { return new FakeColor3(1, 1, 1); }
}
class FakeMaterial {
  constructor(name) {
    this.name = name;
    this.diffuseColor = new FakeColor3(0.2, 0.2, 0.2);
    this.emissiveColor = new FakeColor3(0, 0, 0);
    this.specularColor = new FakeColor3(0, 0, 0);
    this.disposed = false;
  }
  clone(name) { return Object.assign(new FakeMaterial(name), {
    diffuseColor: this.diffuseColor.clone(),
    emissiveColor: this.emissiveColor.clone(),
    specularColor: this.specularColor.clone(),
  }); }
  freeze() {}
  unfreeze() {}
  dispose() { this.disposed = true; }
}
class FakeVector {
  constructor() { this.x = 0; this.y = 0; this.z = 0; }
  set(x, y, z) { this.x = x; this.y = y; this.z = z; }
}
class FakeNode {
  constructor(name) {
    this.name = name;
    this.position = new FakeVector();
    this.rotation = new FakeVector();
    this.scaling = new FakeVector();
    this.material = null;
    this.disposed = false;
  }
  isDisposed() { return this.disposed; }
  dispose() { this.disposed = true; }
}
const fakeMeshBuilder = new Proxy({}, {
  get: (_, name) => name.startsWith('Create') ? (meshName => new FakeNode(meshName)) : undefined,
});

globalThis.window = {
  BABYLON: {Color3: FakeColor3, TransformNode: FakeNode, MeshBuilder: fakeMeshBuilder},
  FakeMaterial,
};
const themeSource = readFileSync(new URL('../frontend/js/cosmetic-themes.js', import.meta.url), 'utf8');
vm.runInThisContext(themeSource, {filename: 'cosmetic-themes.js'});
window.ArenaCosmeticThemes = globalThis.ArenaCosmeticThemes;

let isolatedCosmeticsSource = cosmeticsSource
  .replace(/import \{ isEnabled \} from '[^']+';\r?\n/, 'const isEnabled = () => false;\n')
  .replace(/import \{ makeMat, parseColor \} from '[^']+';\r?\n/, `
    const parseColor = value => {
      const hex = String(value || '#000000').replace('#', '');
      return new window.BABYLON.Color3(parseInt(hex.slice(0,2),16)/255, parseInt(hex.slice(2,4),16)/255, parseInt(hex.slice(4,6),16)/255);
    };
    const makeMat = name => new window.FakeMaterial(name);
  `)
  .replace(
    /from '\.\/body-form-roster\.js[^']*';/,
    `from '${bodyFormRosterURL}';`,
  );
const cosmetics = await import(dataModule(isolatedCosmeticsSource));
const originalWeaponMaterial = new FakeMaterial('weapon-original');
const weaponMesh = new FakeNode('blade');
weaponMesh.material = originalWeaponMaterial;
const cosmeticEntry = {
  root: new FakeNode('bot-root'),
  weapon: {getChildMeshes: () => [weaponMesh]},
};
const cosmeticBot = {
  bot_id: 'preview-bot',
  avatar_color: '#22ccff',
  cosmetics: {
    bot_skin: 'arena_set_003_ember_signal',
    weapon_skin: 'arena_set_003_ember_signal',
    attachment: 'arena_set_003_ember_signal',
  },
};
cosmetics.applyBotCosmetics(cosmeticEntry, cosmeticBot, {});
assert.equal(cosmeticEntry._cosmeticState.groups.length, 0,
  'persisted disabled settings should still suppress spectator cosmetics');
cosmetics.applyBotCosmetics(cosmeticEntry, cosmeticBot, {}, {forceEnabled: true});
assert.ok(cosmeticEntry._cosmeticState.groups.length >= 2,
  'shop override must render the skin and attachment despite disabled spectator settings');
assert.notEqual(weaponMesh.material, originalWeaponMaterial,
  'shop override must render the weapon finish despite disabled spectator settings');
cosmetics.disposeBotCosmetics(cosmeticEntry);

// Exercise the preview lifecycle with its heavy model dependencies replaced by
// tiny fakes. The production class itself, including observers and teardown, is
// executed unchanged.
const events = [];
const applyCalls = [];
let createCount = 0;
const previewDependencies = `
  const createBotEntry = (bot, scene, options) => {
    globalThis.__previewCreateCount += 1;
    globalThis.__previewEvents.push('create-entry');
    globalThis.__previewCreateOptions = options;
    if (globalThis.__previewCreateError) throw new Error('model build failed');
    return {
      root: new window.BABYLON.TransformNode('preview-entry', scene),
      weapon: {getChildMeshes: () => []},
      isForgeCharacter: true,
    };
  };
  const disposeBotEntry = () => globalThis.__previewEvents.push('dispose-entry');
  const applyBotCosmetics = (entry, bot, scene, options) => {
    globalThis.__previewApplyCalls.push({entry, bot: JSON.parse(JSON.stringify(bot)), scene, options});
  };
  const disposeBotCosmetics = () => globalThis.__previewEvents.push('dispose-cosmetics');
`;
globalThis.__previewEvents = events;
globalThis.__previewApplyCalls = applyCalls;
globalThis.__previewCreateCount = createCount;
globalThis.__previewCreateOptions = null;
globalThis.__previewAnimationCount = 0;
globalThis.__previewTrailRenderCount = 0;
globalThis.__previewTrailOptions = null;
globalThis.__previewCreateError = false;

let isolatedPreviewSource = previewSource
  .replace(/import \{ createBotEntry, disposeBotEntry \} from '[^']+';\r?\n/, '')
  .replace(/import \{ applyBotCosmetics, disposeBotCosmetics \} from '[^']+';\r?\n/, previewDependencies)
  .replace(/import \{ updateForgeCharacter \} from '[^']+';\r?\n/,
    'const updateForgeCharacter = () => { globalThis.__previewAnimationCount += 1; };\n')
  .replace(/import \{ TrailRenderer \} from '[^']+';\r?\n/, `
    class TrailRenderer {
      constructor(scene, options) { globalThis.__previewTrailOptions = options; }
      render(entries) {
        globalThis.__previewTrailRenderCount += 1;
        globalThis.__previewTrailEntries = entries;
      }
      dispose() { globalThis.__previewEvents.push('dispose-trails'); }
    }
  `)
  .replace(
    /from '\.\/renderer\/body-form-roster\.js[^']*';/,
    `from '${bodyFormRosterURL}';`,
  );

class PreviewVector {
  constructor(x = 0, y = 0, z = 0) { this.x = x; this.y = y; this.z = z; }
  set(x, y, z) { this.x = x; this.y = y; this.z = z; return this; }
}
class PreviewTransformNode {
  constructor(name, scene) {
    this.name = name;
    this.scene = scene;
    this.position = new PreviewVector();
    this.rotation = new PreviewVector();
    this.scaling = new PreviewVector(1, 1, 1);
    this.parent = null;
  }
}
class PreviewEngine {
  constructor(canvas, antialias, options) {
    Object.assign(this, {canvas, antialias, options, resizeCount: 0, stopped: false, disposed: false});
    globalThis.__previewEvents.push('create-engine');
  }
  setHardwareScalingLevel(value) { this.hardwareScalingLevel = value; }
  runRenderLoop(callback) { this.renderCallback = callback; }
  stopRenderLoop(callback) {
    assert.equal(callback, this.renderCallback, 'preview must stop its exact render callback');
    this.stopped = true;
    globalThis.__previewEvents.push('stop-loop');
  }
  resize() { this.resizeCount += 1; }
  dispose() { this.disposed = true; globalThis.__previewEvents.push('dispose-engine'); }
}
class PreviewScene {
  constructor(engine) {
    this.engine = engine;
    this.renderCount = 0;
    this.disposed = false;
    globalThis.__previewEvents.push('create-scene');
  }
  render() { this.renderCount += 1; }
  dispose() { this.disposed = true; globalThis.__previewEvents.push('dispose-scene'); }
}
class PreviewCamera {
  constructor(name, alpha, beta, radius, target, scene) {
    Object.assign(this, {name, alpha, beta, radius, target, scene});
  }
  attachControl(canvas) { this.attachedCanvas = canvas; }
  detachControl(canvas) {
    assert.equal(canvas, this.attachedCanvas);
    globalThis.__previewEvents.push('detach-camera');
  }
}
class PreviewLight {
  constructor(name, direction, scene) { Object.assign(this, {name, direction, scene}); }
}
class PreviewColor3 extends PreviewVector {}
class PreviewColor4 {
  constructor(r, g, b, a) { Object.assign(this, {r, g, b, a}); }
}

const resizeObservers = [];
class FakeResizeObserver {
  constructor(callback) { this.callback = callback; resizeObservers.push(this); }
  observe(target) { this.target = target; }
  disconnect() { this.disconnected = true; globalThis.__previewEvents.push('disconnect-resize'); }
}
const intersectionObservers = [];
class FakeIntersectionObserver {
  constructor(callback) { this.callback = callback; intersectionObservers.push(this); }
  observe(target) { this.target = target; }
  disconnect() { this.disconnected = true; globalThis.__previewEvents.push('disconnect-visibility'); }
}
const mediaQuery = {
  matches: false,
  addEventListener(name, callback) { this.listener = callback; },
  removeEventListener(name, callback) {
    assert.equal(callback, this.listener);
    globalThis.__previewEvents.push('remove-motion-listener');
  },
};
const windowListeners = new Map();
globalThis.window = {
  BABYLON: {
    Engine: PreviewEngine,
    Scene: PreviewScene,
    ArcRotateCamera: PreviewCamera,
    TransformNode: PreviewTransformNode,
    HemisphericLight: PreviewLight,
    DirectionalLight: PreviewLight,
    Vector3: PreviewVector,
    Color3: PreviewColor3,
    Color4: PreviewColor4,
  },
  devicePixelRatio: 2,
  matchMedia: () => mediaQuery,
  addEventListener(name, callback) { windowListeners.set(name, callback); },
  removeEventListener(name, callback) {
    assert.equal(callback, windowListeners.get(name));
    windowListeners.delete(name);
    globalThis.__previewEvents.push(`remove-${name}`);
  },
};
globalThis.document = {hidden: false};
globalThis.ResizeObserver = FakeResizeObserver;
globalThis.IntersectionObserver = FakeIntersectionObserver;

const {CosmeticShopPreview} = await import(dataModule(isolatedPreviewSource));
const canvas = {nodeName: 'CANVAS'};
const preview = new CosmeticShopPreview(canvas, {autoRotate: true, rotationSpeed: 1});
assert.equal(preview.init(), preview, 'init should be chainable');
assert.equal(preview.init(), preview, 'init should be idempotent');
assert.equal(globalThis.__previewCreateCount, 1, 'one reusable bot model must be created');
assert.equal(globalThis.__previewAnimationCount, 1, 'initialization must establish one stable Forge pose');
assert.deepEqual(globalThis.__previewCreateOptions, {presentationOnly: true});
assert.equal(applyCalls.length, 1);
assert.deepEqual(applyCalls[0].options, {forceEnabled: true});
assert.equal(canvas.tabIndex, -1,
  'Babylon must not place the pointer-driven preview canvas before the Shop skip link in keyboard order');
assert.equal(preview.engine.hardwareScalingLevel, 1, 'preview must cap HiDPI rendering at 1x');
assert.equal(resizeObservers[0].target, canvas);
assert.equal(intersectionObservers[0].target, canvas);
assert.deepEqual(globalThis.__previewTrailOptions, {
  forceEnabled: true,
  showStandard: false,
  previewPath: true,
}, 'the running preview needs an immediate bounded ribbon path without forcing static particles');

preview.setLoadout({bot_skin: 'skin-a', weapon_skin: 'weapon-a', attachment: 'attachment-a', trail: 'ember_sparks'});
preview.setLoadout({bot_skin: 'skin-b'});
assert.equal(globalThis.__previewCreateCount, 1, 'loadout changes must reuse the model and scene');
assert.equal(applyCalls.length, 3);
assert.deepEqual(applyCalls.at(-1).bot.cosmetics, {
  bot_skin: 'skin-b',
  weapon_skin: 'standard',
  attachment: 'none',
  trail: 'standard',
});

const initialRotation = preview.turntable.rotation.y;
const initialRunPosition = {...preview.entry.root.position};
preview._lastFrame -= 100;
preview.engine.renderCallback();
assert.equal(preview.scene.renderCount, 1);
assert.equal(globalThis.__previewAnimationCount, 2, 'visible preview must apply the production idle pose');
assert.equal(globalThis.__previewTrailRenderCount, 1,
  'visible preview must render the selected trail through the production trail path');
assert.ok(preview.turntable.rotation.y >= initialRotation, 'visible preview may auto-rotate');
assert.notDeepEqual(
  {x: preview.entry.root.position.x, z: preview.entry.root.position.z},
  {x: initialRunPosition.x, z: initialRunPosition.z},
  'the showroom bot must run a real loop so a selected trail can be evaluated in motion',
);
intersectionObservers[0].callback([{isIntersecting: false}]);
preview.engine.renderCallback();
assert.equal(preview.scene.renderCount, 1, 'offscreen preview must skip GPU rendering');
assert.equal(globalThis.__previewAnimationCount, 2, 'offscreen preview must skip idle animation work');
assert.equal(globalThis.__previewTrailRenderCount, 1, 'offscreen preview must skip trail work');
intersectionObservers[0].callback([{isIntersecting: true}]);
document.hidden = true;
preview.engine.renderCallback();
assert.equal(preview.scene.renderCount, 1, 'hidden document must skip GPU rendering');
assert.equal(globalThis.__previewAnimationCount, 2, 'hidden document must skip idle animation work');
assert.equal(globalThis.__previewTrailRenderCount, 1, 'hidden document must skip trail work');
document.hidden = false;

mediaQuery.matches = true;
mediaQuery.listener({matches: true});
const reducedMotionRotation = preview.turntable.rotation.y;
const reducedMotionPosition = {...preview.entry.root.position};
preview._lastFrame -= 100;
preview.engine.renderCallback();
assert.equal(preview.scene.renderCount, 2);
assert.equal(globalThis.__previewAnimationCount, 3,
  'reduced motion must still apply the production sampler in its restrained mode');
assert.equal(preview.turntable.rotation.y, reducedMotionRotation,
  'reduced motion must suppress automatic rotation and idle pose updates');
assert.deepEqual(
  {x: preview.entry.root.position.x, z: preview.entry.root.position.z},
  {x: reducedMotionPosition.x, z: reducedMotionPosition.z},
  'reduced motion must freeze the running loop while retaining the last static trail shape',
);
preview.rotateBy(Math.PI / 4);
assert.equal(preview.turntable.rotation.y, reducedMotionRotation + Math.PI / 4);
preview.resetRotation();
assert.equal(preview.turntable.rotation.y, 0);

resizeObservers[0].callback();
assert.ok(preview.engine.resizeCount >= 2, 'initialization and ResizeObserver must resize the engine');

const rendersBeforePageCache = preview.scene.renderCount;
windowListeners.get('pagehide')({persisted: true});
assert.ok(preview.engine, 'BFCache pagehide must suspend rather than destroy the preview');
preview.engine.renderCallback();
assert.equal(preview.scene.renderCount, rendersBeforePageCache,
  'BFCache-suspended preview must not render');
windowListeners.get('pageshow')({persisted: true});
preview.engine.renderCallback();
assert.equal(preview.scene.renderCount, rendersBeforePageCache + 1,
  'BFCache restore must resume the existing preview');

preview.dispose();
preview.dispose();
const disposalOrder = events.filter(event => [
  'dispose-cosmetics', 'dispose-entry', 'detach-camera', 'stop-loop', 'dispose-scene', 'dispose-engine',
].includes(event));
assert.deepEqual(disposalOrder, [
  'dispose-cosmetics', 'dispose-entry', 'detach-camera', 'stop-loop', 'dispose-scene', 'dispose-engine',
]);
assert.equal(resizeObservers[0].disconnected, true);
assert.equal(intersectionObservers[0].disconnected, true);
assert.ok(events.includes('remove-motion-listener'));
assert.ok(events.includes('remove-pagehide'));
assert.ok(events.includes('remove-pageshow'));

globalThis.__previewCreateError = true;
const failedPreview = new CosmeticShopPreview({nodeName: 'CANVAS'});
assert.throws(() => failedPreview.init(), /model build failed/);
assert.equal(failedPreview.scene, null, 'failed initialization must dispose its partial scene');
assert.equal(failedPreview.engine, null, 'failed initialization must dispose its partial engine');

console.log('cosmetics shop preview is isolated, reusable, settings-independent, visibility-aware, and strictly disposable');
