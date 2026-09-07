import { expect, test } from '@playwright/test';
import { arenaState, roundEnd, lobbyState } from '../fixtures/round-cycle.mjs';

const realismEffects = ['sculptedLighting', 'surfaceDetail', 'characterMotion'];

// This is a functional rendering/lifecycle check, not a GPU benchmark.
// Keep the desktop CSS viewport and every quality effect, but rasterize a
// quarter as many pixels so single-core SwiftShader can finish each frame.
test.use({ deviceScaleFactor: 0.5 });

async function installFixture(page, errors) {
  let socket;
  await page.route('https://fonts.googleapis.com/**', route => route.fulfill({ body: '', contentType: 'text/css' }));
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname;
    const json = path.endsWith('/content') ? { blocks: {} }
      : path.endsWith('/service-status') ? { type: 'service_status', revision: 1, broadcast: null, maintenance: null }
        : path.endsWith('/chat/config') ? { enabled: false }
          : path.endsWith('/account/session') ? { authenticated: false }
            : path.endsWith('/leaderboard') || path.endsWith('/bounties') ? { entries: [] }
              : path.endsWith('/weapon-stats') ? { weapons: [] }
                : path.endsWith('/version') ? { commit: 'realism-fixture', build_time: 'fixture' } : {};
    return route.fulfill({ json });
  });
  await page.routeWebSocket('**/ws/spectator', route => {
    socket = route;
    route.onMessage(() => {});
  });
  await page.routeWebSocket('**/ws/chat', route => route.onMessage(() => {}));
  await page.addInitScript(() => {
    try {
      Object.defineProperty(Navigator.prototype, 'gpu', { configurable: true, get: () => undefined });
    } catch { /* Already using WebGL. */ }
  });
  page.on('pageerror', error => errors.push(String(error)));
  page.on('console', message => {
    if (message.type() === 'error' || /webgl warning|content security policy/i.test(message.text())) {
      errors.push(message.text());
    }
  });
  page.on('requestfailed', request => errors.push(`${request.url()}: ${request.failure()?.errorText}`));
  page.on('response', response => {
    if (response.status() >= 400) errors.push(`${response.status()}: ${response.url()}`);
  });
  return {
    ready: () => expect.poll(() => !!socket).toBe(true),
    send: state => socket.send(JSON.stringify(state)),
  };
}

async function nextFrame(page) {
  const frame = await page.evaluate(() => window.BABYLON.EngineStore.LastCreatedScene.getEngine().frameId);
  await expect.poll(() => page.evaluate(() => window.BABYLON.EngineStore.LastCreatedScene.getEngine().frameId), {
    timeout: 30_000,
  }).toBeGreaterThan(frame);
}

async function capture(page, testInfo, name) {
  const pixels = await page.evaluate(() => new Promise(resolve => {
    const scene = window.BABYLON.EngineStore.LastCreatedScene;
    const timeout = setTimeout(() => resolve({ litFraction: -1 }), 30_000);
    scene.onAfterRenderObservable.addOnce(() => {
      const gl = scene.getEngine()._gl;
      const width = gl.drawingBufferWidth;
      const height = gl.drawingBufferHeight;
      const data = new Uint8Array(width * height * 4);
      gl.readPixels(0, 0, width, height, gl.RGBA, gl.UNSIGNED_BYTE, data);
      let lit = 0;
      for (let i = 0; i < data.length; i += 4) {
        if (data[i] + data[i + 1] + data[i + 2] > 30) lit++;
      }
      clearTimeout(timeout);
      resolve({ litFraction: lit / (width * height), width, height });
    });
  }));
  expect(pixels.litFraction, `${name}: ${JSON.stringify(pixels)}`).toBeGreaterThan(0.05);
  await page.screenshot({ path: testInfo.outputPath(`${name}.png`) });
}

async function setQuality(page, enabled) {
  return page.evaluate(async ({ effects, enabled }) => {
    const settings = await import('/js/settings.js');
    for (const effect of effects) settings.setEffect('rendering', effect, enabled);
    return effects.map(effect => settings.isEnabled('rendering', effect));
  }, { effects: realismEffects, enabled });
}

async function sceneHealth(page) {
  return page.evaluate(() => {
    const scene = window.BABYLON.EngineStore.LastCreatedScene;
    const invalid = [];
    for (const node of [...scene.meshes, ...scene.transformNodes]) {
      const values = [node.position.x, node.position.y, node.position.z,
        node.rotation.x, node.rotation.y, node.rotation.z,
        node.scaling.x, node.scaling.y, node.scaling.z,
        ...node.computeWorldMatrix(true).asArray()];
      if (values.some(value => !Number.isFinite(value))) invalid.push(node.name);
    }
    return { invalid, textures: scene.textures.length, materials: scene.materials.length };
  });
}

test('glass arena and moving combatants paint at overview and close range', { tag: '@desktop-only' }, async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  const errors = [];
  const fixture = await installFixture(page, errors);
  await page.goto('/?arena-test=1', { waitUntil: 'networkidle' });
  await fixture.ready();
  await expect.poll(() => page.evaluate(() => window.__ARENA_TEST__?.diagnostics()?.ready)).toBe(true);
  fixture.send(arenaState(7, { bountyTarget: null }));
  await expect.poll(() => page.evaluate(() => window.__ARENA_TEST__.diagnostics().roundNumber)).toBe(7);
  const defaults = await page.evaluate(async effects => {
    const settings = await import('/js/settings.js');
    return effects.map(effect => settings.isEnabled('rendering', effect));
  }, realismEffects);
  expect(defaults).toEqual([true, true, true]);

  // The wide frame exposes the floating deck edge and surrounding space.
  await page.locator('#zoom-slider').fill('0.4');
  await page.locator('#zoom-slider').dispatchEvent('input');
  await nextFrame(page);
  await capture(page, testInfo, 'realism-wide-default');

  await page.locator('#follow-bot').selectOption('winner');
  await page.locator('#zoom-slider').fill('5');
  await page.locator('#zoom-slider').dispatchEvent('input');
  // State snapshots move Aurora and trigger distinct authoritative attack
  // edges. Repeated frames of the same action must not restart the swing.
  for (let tick = 1; tick <= 8; tick++) {
    const state = arenaState(7, { tickOffset: tick, winnerPosition: [920 + tick * 6, 1040 - tick * 3], bountyTarget: null });
    Object.assign(state.bots[0], {
      last_action: tick % 4 === 0 ? 'attack' : 'move',
      last_action_tick: 700 + tick,
      cooldown_remaining: tick % 4 === 0 ? 0.8 : 0,
      target_position: state.bots[1].position,
    });
    fixture.send(state);
    await page.waitForTimeout(120);
  }
  await expect.poll(() => page.evaluate(() => {
    const snapshot = window.__ARENA_TEST__.diagnostics();
    const winner = snapshot.bots.find(bot => bot.id === 'winner');
    const B = window.BABYLON;
    const scene = B.EngineStore.LastCreatedScene;
    const mesh = scene.getMeshByName('world-hud-name-winner');
    if (!winner || winner.x < 950 || !mesh) return false;
    const engine = scene.getEngine();
    const canvas = document.getElementById('arena-canvas').getBoundingClientRect();
    const point = B.Vector3.Project(mesh.getAbsolutePosition(), B.Matrix.Identity(),
      scene.getTransformMatrix(), scene.activeCamera.viewport.toGlobal(engine.getRenderWidth(), engine.getRenderHeight()));
    const x = point.x * canvas.width / engine.getRenderWidth();
    const y = point.y * canvas.height / engine.getRenderHeight();
    const safe = snapshot.safeViewport;
    return x >= safe.left && x <= canvas.width - safe.right &&
      y >= safe.top && y <= canvas.height - safe.bottom;
  })).toBe(true);
  const contact = arenaState(7, { tickOffset: 9, winnerPosition: [968, 1016], bountyTarget: null });
  Object.assign(contact.bots[0], {
    last_action: 'attack', last_action_tick: 709, cooldown_remaining: 0.8,
    target_position: contact.bots[1].position,
  });
  fixture.send(contact);
  await nextFrame(page);
  await capture(page, testInfo, 'realism-close-combat-default');
  expect((await sceneHealth(page)).invalid).toEqual([]);

  expect(await setQuality(page, false)).toEqual([false, false, false]);
  await nextFrame(page);
  await capture(page, testInfo, 'realism-close-quality-disabled');
  expect((await sceneHealth(page)).invalid).toEqual([]);
  expect(await setQuality(page, true)).toEqual([true, true, true]);
  await nextFrame(page);
  // Warm both paths before measuring retained GPU resources. Toggling must
  // reuse detail textures rather than leave a new set behind on every pass.
  const baseline = await sceneHealth(page);
  for (let cycle = 0; cycle < 2; cycle++) {
    await setQuality(page, false);
    await nextFrame(page);
    await setQuality(page, true);
    await nextFrame(page);
  }
  const restored = await sceneHealth(page);
  expect(restored.invalid).toEqual([]);
  expect(restored.textures).toBeLessThanOrEqual(baseline.textures);
  await capture(page, testInfo, 'realism-close-quality-restored');

  // Exercise the production material-clone paths with actual GPU textures.
  // Babylon's DynamicTexture.clone creates an empty canvas: cosmetic finishes
  // and next-map construction must keep the ready shared surface instead.
  const dressed = arenaState(7, { tickOffset: 10, winnerPosition: [968, 1016], bountyTarget: null });
  dressed.bots[0].cosmetics = { weapon_skin: 'solar_flare' };
  fixture.send(dressed);
  const cloneState = prefix => page.evaluate(prefix => {
    const scene = window.BABYLON.EngineStore.LastCreatedScene;
    return scene.materials.filter(material => material.name.startsWith(prefix) && material._forgeSurfaceTexture)
      .map(material => ({
        shared: material.diffuseTexture === material._forgeSurfaceTexture,
        ready: material._forgeSurfaceTexture.isReady(),
        hidden: material.diffuseTexture === null,
        addsEmission: material.emissiveTexture !== null,
      }));
  }, prefix);
  await expect.poll(async () => (await cloneState('cosmetic-weapon-solar_flare')).length).toBeGreaterThan(0);
  for (const state of await cloneState('cosmetic-weapon-solar_flare')) {
    expect(state).toEqual({ shared: true, ready: true, hidden: false, addsEmission: false });
  }
  const ending = roundEnd(7);
  ending.intermission_secs = 12;
  ending.next_map.obstacles = [{ x: 850, y: 850, width: 90, height: 90 }];
  fixture.send(ending);
  fixture.send({ ...lobbyState(8), countdown: 12 });
  await expect.poll(async () => (await cloneState('intermissionRiseBodyMat')).length).toBe(1);
  expect(await cloneState('intermissionRiseBodyMat')).toEqual([
    { shared: true, ready: true, hidden: false, addsEmission: false },
  ]);
  await setQuality(page, false);
  expect((await cloneState('intermissionRiseBodyMat'))[0].hidden).toBe(true);
  await setQuality(page, true);
  expect((await cloneState('intermissionRiseBodyMat'))[0].shared).toBe(true);
  expect(errors).toEqual([]);
  await testInfo.attach('realism-scene-health', {
    body: Buffer.from(JSON.stringify({ baseline, restored }, null, 2)),
    contentType: 'application/json',
  });
});
