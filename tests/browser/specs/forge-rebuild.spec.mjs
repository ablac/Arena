import {expect,test} from '@playwright/test';
import {readFile} from 'node:fs/promises';

test('rebuilt roster renders and all cosmetic sets survive repeated replacement', {tag:'@desktop-only'}, async ({page},testInfo) => {
  test.setTimeout(180_000);
  const errors=[];
  page.on('pageerror',error=>errors.push(String(error)));
  page.on('console',message=>{if(message.type()==='error') errors.push(message.text());});
  await page.route('https://cdn.jsdelivr.net/npm/babylonjs@9.14.0/babylon.min.js',async route=>{
    await route.fulfill({body:await readFile(new URL('../node_modules/babylonjs/babylon.js',import.meta.url)),contentType:'text/javascript'});
  });
  await page.goto('/character-lab.html');
  await expect.poll(()=>page.evaluate(()=>window._lab?.entries.length||0)).toBe(25);
  await page.evaluate(()=>window._lab.scene.whenReadyAsync());
  const budgets=await page.evaluate(()=>window._lab.entries.slice(0,7).map(entry=>({
    weapon:entry.profile.weapon,count:entry._visibleMeshCount,budget:entry.profile.meshBudget,
  })));
  for(const row of budgets) expect(row.count,`${row.weapon} mesh budget`).toBeLessThanOrEqual(row.budget);
  await page.locator('#row-toggle').click();
  await page.screenshot({path:testInfo.outputPath('rebuilt-seven-chassis.png')});
  for(let row=1;row<=3;row++) {
    await page.locator('#row-toggle').click();
    await page.screenshot({path:testInfo.outputPath(`rebuilt-body-forms-${row}.png`)});
  }
  await page.locator('#row-toggle').click();
  await page.locator('#row-toggle').click();
  for(const mode of ['walk','attack','hit','dodge','death','idle']) {
    await page.locator(`[data-mode="${mode}"]`).click();
    await page.evaluate(()=>new Promise(resolve=>{
      let frames=0;
      const scene=window._lab.scene;
      const observer=scene.onAfterRenderObservable.add(()=>{
        if(++frames===12) {scene.onAfterRenderObservable.remove(observer);resolve();}
      });
    }));
    const invalid=await page.evaluate(()=>window._lab.entries.flatMap(entry=>
      [entry.root,...entry.root.getDescendants()].filter(node=>
        [...node.computeWorldMatrix(true).asArray()].some(value=>!Number.isFinite(value))).map(node=>node.name)));
    expect(invalid,`${mode} must retain finite transforms`).toEqual([]);
  }
  const resources=await page.evaluate(async()=>{
    const {applyBotCosmetics}=await import('/js/renderer/cosmetics.js?v=20260907b');
    const {scene,entries}=window._lab;
    const entry=entries[0];
    const baseline={meshes:scene.meshes.length,materials:scene.materials.length,textures:scene.textures.length};
    const cycle=()=>{
      for(let n=1;n<=100;n++) {
        const set=`arena_set_${String(n).padStart(3,'0')}_lab`;
        const bot={...entry.botData,cosmetics:{bot_skin:set,weapon_skin:set,attachment:set,trail:'standard'}};
        applyBotCosmetics(entry,bot,scene,{forceEnabled:true});
      }
      applyBotCosmetics(entry,entry.botData,scene,{forceEnabled:true});
      return {meshes:scene.meshes.length,materials:scene.materials.length,textures:scene.textures.length};
    };
    const first=cycle(),second=cycle();
    return {baseline,first,second};
  });
  expect(resources.second,'replacing all sets twice must not retain old resources').toEqual(resources.first);
  expect(errors).toEqual([]);
});
