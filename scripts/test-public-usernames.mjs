import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const context = vm.createContext({});
vm.runInContext(fs.readFileSync('frontend/dashboard/account-cosmetics.js', 'utf8'), context);
vm.runInContext(fs.readFileSync('frontend/dashboard/account-profile.js', 'utf8'), context);
const c=context.ArenaAccountCosmetics, p=context.ArenaAccountProfile;
assert.equal(c.accountLabel({public_username:'reef_pilot',display_name:'Private Real Name',email:'private@example.test'}),'reef_pilot');
assert.equal(c.accountLabel({display_name:'Private Real Name',name:'Other private',email:'private@example.test'}),'Username unavailable');
const profile=p.normalizeProfile({public_username:'reef_pilot',display_name:'Private Real Name',chat_handle:'Private#1234',bots:[]});
const panel=p.renderPanel(profile);
assert.ok(panel.includes('reef_pilot'));
assert.ok(!panel.includes('Private'));
assert.ok(!panel.includes('name="display_name"'));
assert.ok(panel.includes('https://accounts.angel-serv.com/portal/account/details'));
assert.ok(panel.includes('Refresh username'));
assert.ok(p.renderPanel(p.normalizeProfile({display_name:'Private'})).includes('Username unavailable'));
console.log('public usernames: central alias only, explicit missing state, setup and refresh controls pass');

let current = {authenticated:true,account:{id:'stable-id',public_username:'first_pilot'}};
let check;
const changes=[];
const sessionContext=vm.createContext({
 fetch:async()=>({ok:true,json:async()=>current}),
 window:{addEventListener(){},removeEventListener(){}},
 document:{addEventListener(){},removeEventListener(){},visibilityState:'visible'},
 setInterval:fn=>{check=fn;return 1;},clearInterval(){},
});
const sessionSource=fs.readFileSync('frontend/js/account-session.js','utf8').replace(/^import .*;$/mg,"const apiPath = path => path;").replaceAll('export ','');
vm.runInContext(sessionSource,sessionContext);
const stop=sessionContext.startSessionSync(session=>changes.push(session.account.public_username));
await new Promise(resolve=>setImmediate(resolve));
await check();
assert.deepEqual(changes,['first_pilot']);
current={authenticated:true,account:{id:'stable-id',public_username:'renamed_pilot'}};
await check();
current={authenticated:true,account:{id:'stable-id',public_username:null}};
await check();
assert.deepEqual(changes,['first_pilot','renamed_pilot',null], 'same-account rename and removal must notify all consumers');
stop();
console.log('session sync: unchanged identity deduplicates; rename and removal both notify');
