import {expect,test} from '@playwright/test';

// Exercise the actual chat module and DOM without launching a game renderer.
for (const prefix of ['', '/arena']) {
 test(`open chat relabels history and blocks missing usernames at ${prefix || '/'}`,async({page})=>{
  await page.route('**/username-chat-fixture',route=>route.fulfill({contentType:'text/html',body:`<!doctype html><html><body>
   <div id="chat-overlay"><div id="chat-status-line"></div><div id="chat-messages"></div>
   <form id="chat-form"><input id="chat-input"><button id="chat-send">Send</button></form></div>
   <script type="module" src="${prefix}/js/chat-panel.js?v=20260905u"></script></body></html>`}));
  await page.route('**/api/v1/**',route=>route.fulfill({json:route.request().url().includes('/chat/config') ?
   {enabled:true,history_size:50,max_body_len:280} : {authenticated:true,oidc_login_enabled:true,account:{id:'account-one',public_username:'first_pilot'}}}));
  await page.addInitScript(()=>{
   window.__chatSockets=[];
   window.WebSocket=class {
    static OPEN=1;
    constructor(){this.readyState=1;window.__chatSockets.push(this);queueMicrotask(()=>this.onopen?.());}
    send(){}
    close(){this.readyState=3;}
   };
   window.chatMessage=msg=>window.__chatSockets.at(-1).onmessage({data:JSON.stringify(msg)});
  });
  await page.goto(`${prefix}/username-chat-fixture`,{waitUntil:'domcontentloaded'});
  await expect(page.locator('.chat-watermark')).toHaveCount(1);
  await page.evaluate(()=>document.getElementById('chat-overlay').classList.add('open'));
  await expect.poll(()=>page.evaluate(()=>window.__chatSockets.length)).toBeGreaterThan(0);
  await page.evaluate(()=>{
   window.chatMessage({type:'chat_status',can_post:true,handle:'first_pilot'});
   window.chatMessage({type:'chat_history',messages:[{id:1,account_id:'account-one',handle:'first_pilot',body:'original body',ts:1000}]});
  });
  await expect(page.locator('.chat-handle')).toHaveText('first_pilot');
  await page.evaluate(()=>window.chatMessage({type:'chat_identity',account_id:'account-one',public_username:'renamed_pilot'}));
  await expect(page.locator('.chat-handle')).toHaveText('renamed_pilot');
  await expect(page.locator('.chat-body')).toHaveText('original body');
  // A reused label cannot group a different account's message under the first author.
  await page.evaluate(()=>window.chatMessage({type:'chat_message',message:{id:2,account_id:'account-two',handle:'renamed_pilot',body:'another owner',ts:1100}}));
  await expect(page.locator('.chat-group')).toHaveCount(2);
  await page.evaluate(()=>{
   window.chatMessage({type:'chat_identity',account_id:'account-one',public_username:null});
   window.chatMessage({type:'chat_status',can_post:false,reason:'username_required'});
  });
  await expect(page.locator('.chat-handle').first()).toHaveText('Username unavailable');
  await expect(page.locator('.chat-handle').nth(1)).toHaveText('renamed_pilot');
  await expect(page.locator('#chat-input')).toBeDisabled();
  await expect(page.getByRole('link',{name:'Choose a public username in Angel Accounts'})).toHaveAttribute('href','https://accounts.angel-serv.com/portal/account/details');
  await expect(page.getByRole('button',{name:'Refresh username'})).toBeVisible();
  await page.evaluate(()=>{
   window.chatMessage({type:'chat_settings',enabled:false});
   window.chatMessage({type:'chat_status',enabled:false,can_post:true,handle:'renamed_pilot'});
  });
  await expect(page.locator('#chat-input')).toBeDisabled();
  await expect(page.locator('#chat-status-line')).toHaveText('Chat disabled by an admin');
 });
}
