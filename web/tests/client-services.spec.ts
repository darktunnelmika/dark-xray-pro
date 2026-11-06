import {test,expect,type Page} from '@playwright/test';
async function setup(page:Page,opt:{assigned?:boolean;lost?:boolean;stopped?:boolean;rep?:boolean;pending?:boolean;conflict?:boolean}={}){
 const user={id:opt.rep?'rep-a':'owner',username:'reviewer',role:opt.rep?'representative':'owner',representative_id:opt.rep?'rep-a':'owner'};
 const commands:any[]=[],previews:any[]=[];let ids=opt.assigned?['customer','existing']:['existing'],revision=1,dropped=false;
 const state=()=>({version:1,enabled:true,inbound_id:501,revision,plan_revision:1,inbound_revision:1,desired_revision:revision,active_revision:revision,desired_state:opt.stopped?'stopped':'active',observed_state:opt.pending&&revision>1?'queued':opt.stopped?'stopped':'active',accounting_mode:'per_client',client_ids:ids,effective_client_ids:opt.pending?['existing']:ids,targets:[],allowed_actions:['apply'],blockers:[],pending_changes:!!opt.pending&&revision>1,owner_resume_required:false});
 const receipt=(body:any)=>({id:body.event_id,event_id:body.event_id,inbound_id:501,action:body.action,revision:2,state:'queued',created_unix_nano:1,updated_unix_nano:1,targets:[],blockers:[],replayed:commands.length>1});
 const usage={client_id:'customer',representative_id:user.representative_id,uplink_bytes:'1073741824',downlink_bytes:'1073741824',total_bytes:'2147483648',limit_bytes:'10737418240',reserved_historical_bytes:'1073741824',remaining_native_budget_bytes:'7516192768',known:true,reconciliation_holds:0,plan_kind:'volume'};
 const customer={id:'customer',representative_id:user.representative_id,name:'Customer',plan_kind:'volume',ip_count:0,volume_bytes:10737418240,duration_days:30,deployment_state:'pending_node_apply',expires_unix:1900000000,archived:false,directory:{scope:'saved_membership_and_committed_usage',inbounds:[{id:501,name:'Germany Reality'}],usage}};
 await page.route('**/api/v1/**',async r=>{
  const path=new URL(r.request().url()).pathname,method=r.request().method();
  if(path==='/api/v1/session/status')return r.fulfill({json:{authenticated:true,user,csrf_token:'a'.repeat(64),expires_unix:1900000000}});
  if(path==='/api/v1/fleet')return r.fulfill({json:{as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,nodes:[],metric_scope:'default_route_interface_not_billable',physical_rates_complete:false,interface_rx_bits_per_second:0,interface_tx_bits_per_second:0}});
  if(path==='/api/v1/workspace/me')return r.fulfill({json:{user,data_plane:'not_activated',balance:{id:user.representative_id,kind:user.role,enabled:true,volume_credit_bytes:100000000000,unlimited_credit_units:1000}}});
  if(path==='/api/v1/workspace/clients')return r.fulfill({json:{clients:[customer],total:1,has_more:false,limit:50,offset:0}});
  if(path==='/api/v1/workspace/native-inbounds-state')return r.fulfill({json:{inbounds:[{id:501,name:'Germany Reality'}]}});
  if(path.endsWith('/execution'))return r.fulfill({json:state()});
  if(path.includes('/execution/operations/'))return r.fulfill({status:404,json:{error:'not_found'}});
  if(path.endsWith('/preview')){const b=r.request().postDataJSON();previews.push(b);return r.fulfill({json:{valid:true,action:b.action,review_sha:'b'.repeat(64),state:{...state(),client_ids:b.client_ids},targets:[],blockers:[]}});}
  if(path.endsWith('/commands')){expect(r.request().headers()['x-csrf-token']).toBe('a'.repeat(64));const b=r.request().postDataJSON();commands.push(b);
   if(opt.conflict)return r.fulfill({status:409,json:{error:'revision_conflict'}});
   ids=b.client_ids;revision=2;if(opt.lost&&!dropped){dropped=true;return r.abort('failed');}return r.fulfill({status:202,json:receipt(b)});
  }
  if(path==='/api/v1/workspace/representatives')return r.fulfill({json:{representatives:[]}});
  if(path.endsWith('/client-operations')||path.endsWith('/operations'))return r.fulfill({json:{operations:[]}});
  if(path.endsWith('/groups'))return r.fulfill({json:{groups:[]}});
  if(path.endsWith('/events'))return r.fulfill({json:{events:[]}});
  return r.fulfill({status:404,json:{error:'not_found'}});
 });
 await page.goto('/clients');await expect(page.getByText('Customer',{exact:true})).toBeVisible();
 const open=async()=>{await page.getByRole('button',{name:'Manage',exact:true}).click();await page.getByRole('button',{name:'Services',exact:true}).click();await expect(page.getByLabel('Service inbound')).toHaveValue('501');};
 return {commands,previews,open,usage};
}
async function review(page:Page,remove=false){await page.getByLabel('Change reason').fill('Requested service membership change');await page.getByRole('button',{name:remove?'Review removal':'Review addition',exact:true}).click();await expect(page.getByRole('button',{name:'Confirm service change'})).toBeEnabled();}
test('add preserves existing clients and only confirms after runtime evidence',async({page})=>{
 const f=await setup(page);await f.open();await review(page);await page.getByRole('button',{name:'Confirm service change'}).click();await expect(page.getByText('Service change confirmed on the runtime.')).toBeVisible();expect(f.commands).toHaveLength(1);expect(f.commands[0].client_ids).toEqual(['customer','existing']);expect(f.commands[0].action).toBe('apply');expect(f.commands[0].expected_revision).toBe(1);
});
test('removal preserves every other member',async({page})=>{
 const f=await setup(page,{assigned:true});await f.open();await review(page,true);await page.getByRole('button',{name:'Confirm service change'}).click();await expect(page.getByText('Service change confirmed on the runtime.')).toBeVisible();expect(f.commands[0].client_ids).toEqual(['existing']);
});
test('lost command reply replays exact request after reload',async({page})=>{
 const f=await setup(page,{lost:true});await f.open();await review(page);await page.getByRole('button',{name:'Confirm service change'}).click();await expect(page.getByText('Result unknown. Check or retry the same request.')).toBeVisible();await page.reload();await f.open();await page.getByRole('button',{name:'Check / resume same request'}).click();await expect(page.getByText('Service change confirmed on the runtime.')).toBeVisible();expect(f.commands).toHaveLength(2);expect(f.commands[1]).toEqual(f.commands[0]);
});
test('stopped inbound and representative cannot issue service changes',async({page})=>{
 const f=await setup(page,{stopped:true});await f.open();await page.getByLabel('Change reason').fill('Test');await expect(page.getByRole('button',{name:'Review addition'})).toBeDisabled();expect(f.previews).toHaveLength(0);expect(f.commands).toHaveLength(0);
});
test('representative has no owner service editor',async({page})=>{
 const f=await setup(page,{rep:true});await page.getByRole('button',{name:'Manage',exact:true}).click();await page.getByRole('button',{name:'Services',exact:true}).click();await expect(page.getByLabel('Service inbound')).toHaveCount(0);await expect(page.getByRole('region',{name:'Representative client services'})).toBeVisible();expect(f.commands).toHaveLength(0);
});
test('accepted command is not presented as applied',async({page})=>{
 const f=await setup(page,{pending:true});await f.open();await review(page);await page.getByRole('button',{name:'Confirm service change'}).click();await expect(page.getByText('Request recorded; awaiting runtime confirmation…')).toBeVisible();await expect(page.getByText('Service change confirmed on the runtime.')).toHaveCount(0);await expect(page.getByText('Runtime application is not yet confirmed. Check the same request again.')).toBeVisible({timeout:15000});expect(f.commands).toHaveLength(1);
});
test('concurrent revision rejection requires a new review',async({page})=>{
 const f=await setup(page,{conflict:true});await f.open();await review(page);await page.getByRole('button',{name:'Confirm service change'}).click();await expect(page.getByText('Server rejected the request. Refresh and review again.')).toBeVisible();await expect(page.getByRole('button',{name:'Confirm service change'})).toHaveCount(0);expect(f.commands).toHaveLength(1);
});
test('directory uses reserved-history-aware remaining bytes and stays compact',async({page})=>{
 const f=await setup(page);await page.setViewportSize({width:360,height:800});await expect(page.getByText('Remaining: 7.00 GiB')).toBeVisible();await expect(page.getByText('Recorded here: 2.00 GiB')).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 f.usage.known=false;await page.reload();await expect(page.getByText('Remaining: unknown')).toBeVisible();await expect(page.getByText('Remaining: 7.00 GiB')).toHaveCount(0);
});

test('representative service editor uses narrow commands and recovers after reload',async({page})=>{
 await setup(page,{rep:true});const sent:any[]=[];let assigned=false;
 await page.route('**/api/v1/workspace/clients/customer/services',r=>r.fulfill({json:{client_id:'customer',services:[{inbound_id:501,name:'Allowed service',grant_revision:1,execution_revision:2,plan_revision:1,inbound_revision:1,assigned,applied:false,ready:!assigned}]}}));
 await page.route('**/api/v1/workspace/clients/customer/services/501/*',async r=>{
  const b=r.request().postDataJSON();expect(b.client_ids).toBeUndefined();expect(b.action).toBeUndefined();
  if(r.request().url().endsWith('/preview'))return r.fulfill({json:{client_id:'customer',inbound_id:501,valid:true,review_sha:'b'.repeat(64)}});
  sent.push(b);expect(r.request().headers()['x-csrf-token']).toBe('a'.repeat(64));assigned=true;
  if(sent.length===1)return r.abort('failed');
  return r.fulfill({json:{client_id:'customer',inbound_id:501,valid:true,operation_id:b.event_id,state:'queued'}});
 });
 const open=async()=>{await page.getByRole('button',{name:'Manage',exact:true}).click();await page.getByRole('button',{name:'Services',exact:true}).click()};
 await open();await page.getByLabel('Allowed service').selectOption('501');await page.getByLabel('Service change reason').fill('Customer request');
 await page.getByRole('button',{name:'Review change',exact:true}).click();expect(sent).toHaveLength(0);
 await page.getByRole('button',{name:'Confirm change',exact:true}).click();await expect(page.getByRole('button',{name:'Retry same request'})).toBeVisible();
 await page.reload();await open();await page.getByRole('button',{name:'Retry same request'}).click();
 await expect(page.getByText('Command recorded. Refresh to check runtime application.')).toBeVisible();expect(sent).toHaveLength(2);expect(sent[1]).toEqual(sent[0]);
 await expect(page.getByText('Applied to service',{exact:true})).toHaveCount(0);
});
test('representative sees loading then an actionable empty state without a change form',async({page})=>{
 const f=await setup(page,{rep:true});let release!:()=>void;const gate=new Promise<void>(resolve=>{release=resolve});
 await page.route('**/api/v1/workspace/clients/customer/services',async r=>{await gate;await r.fulfill({json:{client_id:'customer',services:[]}})});
 await page.getByRole('button',{name:'Manage',exact:true}).click();await page.getByRole('button',{name:'Services',exact:true}).click();
 await expect(page.getByText('Loading allowed services…')).toBeVisible();await expect(page.getByLabel('Allowed service')).toBeDisabled();await expect(page.getByLabel('Service change reason')).toHaveCount(0);
 release();await expect(page.getByText('No services have been granted to you. Contact the owner to request access.')).toBeVisible();await expect(page.getByLabel('Service change reason')).toHaveCount(0);expect(f.commands).toHaveLength(0);
});
test('unready service explains why editing is unavailable and refresh can recover',async({page})=>{
 await setup(page,{rep:true});let ready=false;
 await page.route('**/api/v1/workspace/clients/customer/services',r=>r.fulfill({json:{client_id:'customer',services:[{inbound_id:501,name:'Allowed service',grant_revision:1,execution_revision:2,plan_revision:1,inbound_revision:1,assigned:false,applied:false,ready}]}}));
 await page.getByRole('button',{name:'Manage',exact:true}).click();await page.getByRole('button',{name:'Services',exact:true}).click();
 await expect(page.getByText("Choose a service to manage this client's membership.")).toBeVisible();await page.getByLabel('Allowed service').selectOption('501');
 await expect(page.getByText('This service is not ready for membership changes. Refresh its status or contact the owner.')).toBeVisible();await expect(page.getByLabel('Service change reason')).toHaveCount(0);
 ready=true;await page.getByRole('button',{name:'Refresh service status'}).click();await expect(page.getByLabel('Service change reason')).toBeVisible();
});
test('failed service refresh removes stale edit choices without claiming no grants',async({page})=>{
 const f=await setup(page,{rep:true});let fail=false;
 await page.route('**/api/v1/workspace/clients/customer/services',r=>fail?r.fulfill({status:503,json:{error:'unavailable'}}):r.fulfill({json:{client_id:'customer',services:[{inbound_id:501,name:'Allowed service',grant_revision:1,execution_revision:2,plan_revision:1,inbound_revision:1,assigned:false,applied:false,ready:true}]}}));
 await page.getByRole('button',{name:'Manage',exact:true}).click();await page.getByRole('button',{name:'Services',exact:true}).click();await page.getByLabel('Allowed service').selectOption('501');await expect(page.getByLabel('Service change reason')).toBeVisible();
 fail=true;await page.getByRole('button',{name:'Refresh service status'}).click();await expect(page.getByText('Result not verified. Check service state and permission.')).toBeVisible();await expect(page.getByLabel('Service change reason')).toHaveCount(0);await expect(page.getByText('No services have been granted to you. Contact the owner to request access.')).toHaveCount(0);expect(f.previews).toHaveLength(0);expect(f.commands).toHaveLength(0);
 fail=false;await page.getByRole('button',{name:'Refresh service status'}).click();await expect(page.getByLabel('Service change reason')).toBeVisible();
});
