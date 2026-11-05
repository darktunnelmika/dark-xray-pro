import {test,expect,type Page} from "@playwright/test";
const csrf="a".repeat(64);
async function fixture(page:Page,rep=false,lost=false,blocked=false,stopAfterApply=false) {
 const user={id:rep?"rep-a":"owner",username:"reviewer",role:rep?"representative":"owner",representative_id:rep?"rep-a":"owner"};
 const creates:Record<string,unknown>[]=[],commands:Record<string,unknown>[]=[];
 let client="",dropped=false,active=false,stopped=false;
 const state=()=>({version:1,enabled:true,inbound_id:501,revision:active?2:1,plan_revision:1,inbound_revision:1,desired_revision:1,active_revision:1,
  desired_state:stopped||(active&&stopAfterApply)?"stopped":"active",observed_state:stopped?"stopped":"active",accounting_mode:"per_client",client_ids:active?["existing",client]:["existing"],
  effective_client_ids:active?["existing",client]:["existing"],targets:[],allowed_actions:stopped?["activate"]:["apply"],blockers:[],pending_changes:blocked,owner_resume_required:false});
 await page.route("**/api/v1/**",async r=>{
  const path=new URL(r.request().url()).pathname,method=r.request().method();
  if(path==="/api/v1/session/status")return r.fulfill({json:{authenticated:true,user,csrf_token:csrf,expires_unix:1900000000}});
  if(path==="/api/v1/fleet")return r.fulfill({json:{as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,nodes:[],metric_scope:"default_route_interface_not_billable",physical_rates_complete:false,interface_rx_bits_per_second:0,interface_tx_bits_per_second:0}});
  if(path==="/api/v1/workspace/me")return r.fulfill({json:{user,data_plane:"not_activated",balance:{id:user.representative_id,kind:rep?"representative":"owner",enabled:true,volume_credit_bytes:100000000000,unlimited_credit_units:1000}}});
  if(path==="/api/v1/workspace/native-inbounds-state")return r.fulfill({json:{inbounds:[{id:501,name:"Germany Reality",spec:{protocol:"vless",network:"grpc",security:"reality"}}]}});
  if(path==="/api/v1/workspace/native-inbounds/501/execution")return r.fulfill({json:state()});
  if(method==="POST"){
   expect(r.request().headers()["x-csrf-token"]).toBe(csrf);
   const body=r.request().postDataJSON() as Record<string,unknown>;
   if(path==="/api/v1/workspace/clients"){
    creates.push(body);client=String(body.client_id);
    if(lost&&!dropped){dropped=true;return r.abort("failed");}
    return r.fulfill({status:201,json:{id:client}});
   }
   if(path.endsWith("/preview"))return r.fulfill({json:{valid:true,action:body.action,review_sha:"b".repeat(64),state:{...state(),client_ids:body.client_ids},targets:[],blockers:[]}});
   if(path.endsWith("/commands")){
    commands.push(body);active=true;
    return r.fulfill({status:202,json:{id:body.event_id,event_id:body.event_id,inbound_id:501,action:body.action,revision:2,state:"active",created_unix_nano:1,updated_unix_nano:1,targets:[],blockers:[],replayed:false}});
   }
  }
  if(path.endsWith("/inbound-subscription"))return r.fulfill({json:{enabled:false,client_id:client}});
  if(path==="/api/v1/workspace/clients")return r.fulfill({json:{clients:[],total:0,has_more:false,limit:50,offset:0}});
  if(path==="/api/v1/workspace/representatives")return r.fulfill({json:{representatives:[]}});
  if((path.endsWith("/operations")||path.endsWith("/client-operations")))return r.fulfill({json:{operations:[]}});
  if(path.endsWith("/groups"))return r.fulfill({json:{groups:[]}});
  if(path.endsWith("/events"))return r.fulfill({json:{events:[]}});
  return r.fulfill({status:404,json:{error:"not_found"}});
 });
 await page.goto("/clients");
 await page.getByRole("button",{name:"Create client",exact:true}).click();
 await expect(page.getByRole("heading",{name:"Create client",exact:true})).toBeVisible();
 return {creates,commands,setStopped:(value:boolean)=>{stopped=value;}};
}
test("owner creates manually and adds to selected inbound without replacing existing clients",async({page})=>{
 const f=await fixture(page);
 await page.getByLabel("Client name",{exact:true}).fill("Manual Alice");
 await page.getByText("Advanced identity",{exact:true}).click();
 await page.getByLabel("UUID (optional)",{exact:true}).fill("12345678-1234-4234-8234-123456789abc");
 await page.getByLabel(/Germany Reality/).check();
 await page.getByRole("button",{name:"Create and activate",exact:true}).click();
 await expect(page.getByText("Client applied to all selected inbounds.",{exact:false})).toBeVisible();
 expect(f.creates).toHaveLength(1);expect(f.commands).toHaveLength(1);
 expect(f.commands[0].client_ids).toEqual(["existing",f.creates[0].client_id].sort());
 expect(await page.evaluate(()=>JSON.stringify(sessionStorage))).not.toContain("12345678-1234-4234-8234-123456789abc");
});
test("lost creation reply survives refresh and replays the same financial request",async({page})=>{
 const f=await fixture(page,false,true);
 await page.getByLabel("Client name",{exact:true}).fill("Lost reply");
 await page.getByLabel(/Germany Reality/).check();
 await page.getByRole("button",{name:"Create and activate",exact:true}).click();
 await expect(page.getByRole("button",{name:"Resume same request"})).toBeEnabled();
 await page.reload();
 await page.getByRole("button",{name:"Resume same request"}).click();
 await expect(page.getByText("Client applied to all selected inbounds.",{exact:false})).toBeVisible();
 expect(f.creates).toHaveLength(2);expect(f.creates[1]).toEqual(f.creates[0]);expect(f.commands).toHaveLength(1);
});
test("pending inbound edits block creation before credit is charged",async({page})=>{
 const f=await fixture(page,false,false,true);
 await page.getByLabel("Client name",{exact:true}).fill("Blocked");
 await page.getByLabel(/Germany Reality/).check();
 await page.getByRole("button",{name:"Create and activate",exact:true}).click();
 await expect(page.getByRole("button",{name:"Resume same request"})).toBeEnabled();
 expect(f.creates).toHaveLength(0);expect(f.commands).toHaveLength(0);
});
test("representative creation keeps tenant scope and never calls owner activation",async({page})=>{
 const f=await fixture(page,true);
 await page.setViewportSize({width:320,height:800});
 await page.getByLabel("Client name",{exact:true}).fill("Rep manual");
 await page.getByRole("button",{name:"Create client",exact:true}).click();
 await expect(page.getByText("Account saved; awaiting owner activation.")).toBeVisible();
 expect(f.creates[0].representative_id).toBe("rep-a");expect(f.commands).toHaveLength(0);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});

test("client creation never starts a stopped inbound or charges before readiness",async({page})=>{
 const f=await fixture(page);f.setStopped(true);
 const form=page.getByRole('region',{name:'Create client',exact:true});
 await form.getByLabel('Client name',{exact:true}).fill('Blocked inactive service');
 await form.getByLabel(/Germany Reality/).check();
 await form.getByRole('button',{name:'Create and activate',exact:true}).click();
 await expect(form.getByText('Selected inbound is not ready.',{exact:false})).toBeVisible();
 expect(f.creates).toHaveLength(0);expect(f.commands).toHaveLength(0);
 await expect(form.getByRole('button',{name:'Edit unsent request',exact:true})).toBeVisible();
 await expect(page.getByRole('region',{name:'Native Inbounds',exact:true})).toHaveCount(0);
});
test("lost client receipt replays its financial request even if the inbound has since stopped",async({page})=>{
 const f=await fixture(page,false,true);
 await page.getByLabel('Client name',{exact:true}).fill('Recover paid account');
 await page.getByLabel(/Germany Reality/).check();
 await page.getByRole('button',{name:'Create and activate',exact:true}).click();
 await expect(page.getByRole('button',{name:'Resume same request',exact:true})).toBeEnabled();
 f.setStopped(true);
 await page.getByRole('button',{name:'Resume same request',exact:true}).click();
 await expect(page.getByRole('button',{name:'View saved client',exact:true})).toBeVisible();
 expect(f.creates).toHaveLength(2);expect(f.creates[1]).toEqual(f.creates[0]);expect(f.commands).toHaveLength(0);
});
test("client form separates identity service and plan and opens delivery outside creation",async({page})=>{
 const f=await fixture(page);
 const form=page.getByRole('region',{name:'Create client',exact:true});
 await expect(form.locator('.client-form-section')).toHaveCount(3);
 await expect(form.getByRole('region',{name:'Client service',exact:true}).getByLabel(/Germany Reality/)).toBeVisible();
 await expect(form.getByRole('region',{name:'Client service',exact:true}).getByLabel('Volume (GiB)',{exact:true})).toHaveCount(0);
 await form.getByLabel('Client name',{exact:true}).fill('Clear customer form');
 await form.getByLabel(/Germany Reality/).check();
 await form.getByRole('button',{name:'Create and activate',exact:true}).click();
 await expect(form.getByText('Client applied to all selected inbounds.',{exact:false})).toBeVisible();
 await expect(form.locator('.lab-subscription')).toHaveCount(0);
 expect(f.commands[0].action).toBe('apply');
 await form.getByRole('button',{name:'View saved client',exact:true}).click();
 await expect(page.getByRole('button',{name:'Configuration & QR',exact:true})).toHaveAttribute('aria-pressed','true');
 await expect(form).toBeHidden();
});

test("a stop requested during membership apply is never presented as ready delivery",async({page})=>{
 test.setTimeout(30000);
 const f=await fixture(page,false,false,false,true);
 await page.getByLabel('Client name',{exact:true}).fill('Stop racing delivery');
 await page.getByLabel(/Germany Reality/).check();
 await page.getByRole('button',{name:'Create and activate',exact:true}).click();
 await expect(page.getByText('Account saved; activation is not yet confirmed.',{exact:false})).toBeVisible({timeout:20000});
 await expect(page.getByText('Client applied to all selected inbounds.',{exact:false})).toHaveCount(0);
 expect(f.creates).toHaveLength(1);expect(f.commands).toHaveLength(1);expect(f.commands[0].action).toBe('apply');
});
