import {test,expect,type Page} from '@playwright/test';
async function fixture(page:Page,lost=false,inbound=false,echo=true){
 const user={id:'owner',username:'reviewer',role:'owner',representative_id:'owner'},csrf='a'.repeat(64);
 const creates:any[]=[],updates:any[]=[],queries:string[]=[],inboundQueries:string[]=[];let optionReads=0;
 let row:any={id:'contact-fixture',name:'Alice',representative_id:'owner',plan_kind:'volume',ip_count:0,volume_bytes:10737418240,duration_days:30,expires_unix:1900000000,created_unix:1800000000,deployment_state:'pending_node_apply',archived:false,directory:{scope:'saved_membership_and_committed_usage',inbounds:[],representative:{id:'owner',display_name:'Example Owner'}},contact:{telegram_username:'alice_test',telegram_id:'123456789',note:'test note',revision:1}},dropped=false;
 await page.route('**/api/v1/**',async r=>{
  const u=new URL(r.request().url()),p=u.pathname,method=r.request().method();
  if(p==='/api/v1/session/status')return r.fulfill({json:{authenticated:true,user,csrf_token:csrf,expires_unix:1900000000}});
  if(p==='/api/v1/fleet')return r.fulfill({json:{as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,nodes:[],metric_scope:'default_route_interface_not_billable',physical_rates_complete:false,interface_rx_bits_per_second:0,interface_tx_bits_per_second:0}});
  if(p==='/api/v1/workspace/me')return r.fulfill({json:{user,data_plane:'not_activated',balance:{id:'owner',kind:'owner',enabled:true,volume_credit_bytes:0,unlimited_credit_units:0}}});
  if(p==='/api/v1/workspace/client-inbound-options'&&inbound){optionReads++;return r.fulfill({json:{scope:'saved_client_assignments',representative_id:'',inbounds:[{id:501,name:'Saved service',archived:false,clients:1}]}});}
  if(p==='/api/v1/workspace/native-inbounds-state')return r.fulfill({json:{inbounds:[]}});
  if(p==='/api/v1/workspace/clients'&&method==='POST'){
   const b=r.request().postDataJSON();expect(r.request().headers()['x-csrf-token']).toBe(csrf);creates.push(b);row={...row,id:b.client_id,name:b.name,contact:b.contact?{...b.contact,revision:1}:undefined};return r.fulfill({status:201,json:row});
  }
  if(p.endsWith('/contact')&&method==='POST'){
   const b=r.request().postDataJSON();updates.push(b);expect(r.request().headers()['x-csrf-token']).toBe(csrf);
   row.contact={telegram_username:b.telegram_username,telegram_id:b.telegram_id,note:b.note,revision:b.expected_revision+1};
   if(lost&&!dropped){dropped=true;return r.abort('failed');}return r.fulfill({json:{contact:row.contact,replayed:updates.length>1}});
  }
  if(p==='/api/v1/workspace/clients'){
   inboundQueries.push(u.searchParams.get('inbound_id')??'');queries.push(u.searchParams.get('q')??'');return r.fulfill({json:{clients:[row],total:1,has_more:false,limit:50,offset:0,...(inbound&&echo?{inbound_id:Number(u.searchParams.get('inbound_id')||0)}:{})}});
  }
  if(p==='/api/v1/workspace/clients/'+row.id)return r.fulfill({json:row});
  if(p.endsWith('/edits'))return r.fulfill({json:{edits:[],limit:25,offset:0}});
  if(p.endsWith('/inbound-subscription'))return r.fulfill({json:{enabled:false,client_id:row.id}});
  if(p.endsWith('/representatives'))return r.fulfill({json:{representatives:[]}});
  if(p.endsWith('/groups'))return r.fulfill({json:{groups:[]}});
  if(p.endsWith('/events'))return r.fulfill({json:{events:[]}});
  if(p.endsWith('/client-operations'))return r.fulfill({json:{operations:[]}});
  return r.fulfill({status:404,json:{error:'not_found'}});
 });
 await page.goto('/clients');return {creates,updates,queries,inboundQueries,optionReads:()=>optionReads};
}
test('manual creation sends Telegram metadata and shows it in directory',async({page})=>{
 const f=await fixture(page);await page.getByRole('button',{name:'Create client',exact:true}).click();const form=page.getByRole('region',{name:'Create client',exact:true});
 await form.getByLabel('Client name',{exact:true}).fill('Telegram customer');
 await form.getByText('Telegram and note (optional)',{exact:true}).click();
 await form.getByLabel('Telegram username',{exact:true}).fill('@NEW_CLIENT');
 await form.getByLabel('Telegram numeric ID',{exact:true}).fill('987654321');
 await form.getByLabel('Internal note',{exact:true}).fill('Customer support note');
 await form.getByLabel("Creation mode",{exact:true}).selectOption("account");
 await form.getByRole('button',{name:'Create client',exact:true}).click();
 await expect(form.getByText('Account saved. Choose an allowed service to activate this client.')).toBeVisible();
 await expect(form.getByRole("button",{name:"Choose service",exact:true})).toBeVisible();
 expect(f.creates[0].contact).toEqual({telegram_username:'new_client',telegram_id:'987654321',note:'Customer support note'});
 await expect(page.locator('.clients-explorer-row').getByText('@new_client',{exact:true})).toBeVisible();
});
test('Telegram directory search is sent to server and not a current-page-only filter',async({page})=>{
 const f=await fixture(page);await page.getByLabel('Search by name, ID, Telegram or note',{exact:true}).fill('@ALICE_TEST');
 await expect.poll(()=>f.queries.includes('@ALICE_TEST')).toBe(true);
 await expect(page.locator('.clients-explorer-row').getByText('@alice_test',{exact:true})).toBeVisible();
});
test('contact edit preserves exact request after lost response and refreshes directory',async({page})=>{
 const f=await fixture(page,true);await page.locator('.clients-explorer-row').getByRole('button',{name:'Manage',exact:true}).click();
 await page.getByRole('button',{name:'Details & Telegram',exact:true}).click();const editor=page.getByRole('region',{name:'Client contact',exact:true});
 await expect(editor.getByLabel('Telegram username',{exact:true})).toHaveValue('alice_test');
 await editor.getByLabel('Telegram username',{exact:true}).fill('@UPDATED_TEST');
 await editor.getByRole('button',{name:'Save contact',exact:true}).click();
 await expect(editor.getByRole('button',{name:'Retry same contact update',exact:true})).toBeEnabled();
 await expect(editor.getByLabel('Telegram username',{exact:true})).toBeDisabled();
 await editor.getByRole('button',{name:'Retry same contact update',exact:true}).click();
 await expect(editor.getByText('Contact saved',{exact:true})).toBeVisible();
 expect(f.updates).toHaveLength(2);expect(f.updates[1]).toEqual(f.updates[0]);
 await page.getByRole('button',{name:'Close details',exact:true}).click();
 await expect(page.locator('.clients-explorer-row').getByText('@updated_test',{exact:true})).toBeVisible();
 await page.setViewportSize({width:320,height:800});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
});
test('invalid Telegram ID is rejected before a creation request is locked',async({page})=>{
 const f=await fixture(page);await page.getByRole('button',{name:'Create client',exact:true}).click();const form=page.getByRole('region',{name:'Create client',exact:true});
 await form.getByLabel('Client name',{exact:true}).fill('Bad contact');await form.getByText('Telegram and note (optional)',{exact:true}).click();
 await form.getByLabel('Telegram numeric ID',{exact:true}).fill('-123');await form.getByLabel("Creation mode",{exact:true}).selectOption("account");
 await form.getByRole('button',{name:'Create client',exact:true}).click();await expect(form.getByText('Invalid Telegram details or note.')).toBeVisible();
 expect(f.creates).toHaveLength(0);await expect(form.getByLabel('Telegram numeric ID',{exact:true})).toBeEnabled();
});

 test('client directory starts compact and retains draft when creation is closed',async({page})=>{
 await fixture(page);
 await page.setViewportSize({width:320,height:750});
 const form=page.getByRole('region',{name:'Create client',exact:true});
 await expect(form).toBeHidden();
 await expect(page.getByLabel('Additional days')).toHaveCount(0);
 await page.getByRole('button',{name:'Create client',exact:true}).click();
 await form.getByLabel('Client name',{exact:true}).fill('Retained draft');
 await page.getByRole('button',{name:'Back to client list',exact:true}).click();
 await expect(form).toBeHidden();
 await page.getByRole('button',{name:'Create client',exact:true}).click();
 await expect(form.getByLabel('Client name',{exact:true})).toHaveValue('Retained draft');
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBeTruthy();
 });

test('compact list hides selection controls and modal supports keyboard and mobile',async({page})=>{
 await fixture(page);
 await page.setViewportSize({width:1280,height:850});
 await expect(page.locator('.clients-explorer-row')).toHaveCount(1);
 await expect(page.locator('.clients-bulk-checkbox')).toHaveCount(0);
 await expect(page.locator('.clients-bulk')).toBeHidden();
 await page.screenshot({path:'/var/tmp/dark-pro-clients-redesign-desktop.png',fullPage:true});
 await page.getByRole('button',{name:'Select multiple clients',exact:true}).click();
 await expect(page.locator('.clients-bulk-checkbox input')).toHaveCount(1);
 await page.locator('.clients-bulk-checkbox input').check();
 await page.getByRole('button',{name:/Close selection/}).click();
 await expect(page.locator('.clients-bulk-checkbox')).toHaveCount(0);
 await page.getByRole('button',{name:/Select multiple clients/}).click();
 await expect(page.locator('.clients-bulk-checkbox input')).toBeChecked();
 await page.getByRole('button',{name:/Close selection/}).click();
 await page.getByRole('button',{name:'Create client',exact:true}).click();
 await expect(page.getByRole('dialog',{name:'New client',exact:true})).toBeVisible();
 await page.screenshot({path:'/var/tmp/dark-pro-clients-redesign-create.png'});
 await page.keyboard.press('Escape');
 await expect(page.getByRole('dialog',{name:'New client',exact:true})).toBeHidden();
 await page.setViewportSize({width:320,height:750});
 await page.getByRole('button',{name:'Switch language',exact:true}).click();
 await expect(page.getByRole('heading',{name:'فهرست کلاینت‌ها',exact:true})).toBeVisible();
 await page.screenshot({path:'/var/tmp/dark-pro-clients-redesign-mobile.png',fullPage:true});
 await page.getByRole('button',{name:'ایجاد کلاینت',exact:true}).click();
 const dialog=page.getByRole('dialog',{name:'ایجاد کلاینت',exact:true});
 await expect(dialog).toBeVisible();
 expect(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBeTruthy();
 await page.screenshot({path:'/var/tmp/dark-pro-clients-redesign-mobile-create.png'});
});


test('saved inbound filter loads lazily and sends server filter without writes',async({page})=>{
 const f=await fixture(page,false,true);
 await expect(page.locator('.clients-explorer-row')).toHaveCount(1);
 expect(f.optionReads()).toBe(0);
 await page.getByText('More filters',{exact:false}).click();
 const filter=page.getByLabel('Client inbound',{exact:true});
 await expect(filter).toBeEnabled();await filter.selectOption('501');
 await expect.poll(()=>f.inboundQueries.includes('501')).toBe(true);
 await expect(page.locator('.clients-explorer-row')).toHaveCount(1);
 await page.getByRole('button',{name:'Clear inbound filter',exact:true}).click();
 await expect(filter).toHaveValue('');
 expect(f.creates).toHaveLength(0);expect(f.updates).toHaveLength(0);
});
test('inbound filter rejects an older backend that ignores the selected inbound',async({page})=>{
 await fixture(page,false,true,false);
 await expect(page.locator('.clients-explorer-row')).toHaveCount(1);
 await page.getByText('More filters',{exact:false}).click();
 const filter=page.getByLabel('Client inbound',{exact:true});
 await expect(filter).toBeEnabled();await filter.selectOption('501');
 await expect(page.locator('.clients-explorer-row')).toHaveCount(0);
 await expect(filter).toHaveValue('501');
});

test('row QR opens delivery directly and preserves the directory search',async({page})=>{
 const f=await fixture(page);
 const search=page.getByLabel('Search by name, ID, Telegram or note',{exact:true});
 await search.fill('Alice');await expect.poll(()=>f.queries.includes('Alice')).toBe(true);
 await page.getByRole('button',{name:'Configuration & QR Alice',exact:true}).click();
 const dialog=page.getByRole('dialog');
 await expect(dialog.getByRole('button',{name:'Configuration & QR',exact:true})).toHaveAttribute('aria-pressed','true');
 await dialog.getByRole('button',{name:'Close details',exact:true}).click();
 await expect(search).toHaveValue('Alice');
 await expect(page.locator('.clients-explorer-row')).toHaveCount(1);
});

test('client tabs preserve unsaved contact edits without eager delivery reads',async({page})=>{
 const delivery:string[]=[];page.on('request',r=>{if(r.url().includes('/inbound-subscription'))delivery.push(r.url());});
 await fixture(page);await page.getByRole('button',{name:'Manage',exact:true}).click();
 await page.getByRole('button',{name:'Details & Telegram',exact:true}).click();
 const input=page.getByRole('region',{name:'Client contact',exact:true}).getByLabel('Telegram username',{exact:true});
 await expect(input).toHaveValue('alice_test');await input.fill('unsaved_name');
 expect(delivery).toHaveLength(0);
 await page.getByRole('button',{name:'Overview',exact:true}).click();
 await page.getByRole('button',{name:'Details & Telegram',exact:true}).click();
 await expect(input).toHaveValue('unsaved_name');
});
test('directory actions fit desktop and mobile and search can be cleared',async({page})=>{
 const f=await fixture(page);
 for(const width of [1440,1024,390,320]){
  await page.setViewportSize({width,height:850});
  await expect(page.getByRole('button',{name:'Configuration & QR Alice',exact:true})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 }
 const search=page.getByLabel('Search by name, ID, Telegram or note',{exact:true});
 await search.fill('Alice');await expect.poll(()=>f.queries.at(-1)).toBe('Alice');
 await page.getByRole('button',{name:'Clear search',exact:true}).click();
 await expect(search).toHaveValue('');await expect.poll(()=>f.queries.at(-1)).toBe('');
});

test('active filters can be reset and renewal opens from the row',async({page})=>{
 const urls:string[]=[];page.on('request',r=>{if(new URL(r.url()).pathname==='/api/v1/workspace/clients'&&r.method()==='GET')urls.push(r.url());});
 await fixture(page);
 await page.getByLabel('Status',{exact:true}).selectOption('expired');
 await page.getByLabel('Search by name, ID, Telegram or note',{exact:true}).fill('Alice');
 await expect(page.getByText('Active search and filters: 2',{exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Reset all filters',exact:true}).click();
 await expect(page.getByLabel('Status',{exact:true})).toHaveValue('all');
 await expect(page.getByLabel('Search by name, ID, Telegram or note',{exact:true})).toHaveValue('');
 await expect.poll(()=>{const u=new URL(urls.at(-1)!);return !u.searchParams.get('q')&&(!u.searchParams.get('state')||u.searchParams.get('state')==='all');}).toBe(true);
 await page.getByRole('button',{name:'Renew Alice',exact:true}).click();
 await expect(page.getByRole('dialog').getByLabel('Additional days',{exact:true})).toBeVisible();
});

test('relative expiry keeps exact date and direct edit opens the contact tab',async({page})=>{
 await page.clock.setFixedTime(new Date(1900000000*1000-2*86400000));
 await fixture(page);
 await expect(page.getByText('About 2 days left',{exact:true})).toBeVisible();
 await expect(page.locator('.clients-expiry')).toContainText('Expires:');
 await page.getByRole('button',{name:'Edit Alice',exact:true}).click();
 await expect(page.getByRole('dialog').getByRole('button',{name:'Details & Telegram',exact:true})).toHaveAttribute('aria-pressed','true');
 await expect(page.getByRole('region',{name:'Client contact',exact:true}).getByLabel('Telegram username',{exact:true})).toHaveValue('alice_test');
});

test('directory shows the matching representative name',async({page})=>{
 await fixture(page);await expect(page.locator('.clients-explorer-row').getByText('Example Owner',{exact:true})).toBeVisible();
});
