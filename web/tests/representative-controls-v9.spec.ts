import { expect, test, type Page } from "@playwright/test";

type Permissions = {
 view_clients:boolean;create_clients:boolean;renew_clients:boolean;edit_clients:boolean;
 manage_clients:boolean;manage_groups:boolean;view_finance:boolean;
};
const initial:Permissions={
 view_clients:true,create_clients:true,renew_clients:true,edit_clients:true,
 manage_clients:true,manage_groups:true,view_finance:true,
};
const user={id:"owner",username:"proowner",role:"owner",representative_id:"owner"};
const csrf="c".repeat(64);
type Mutation={path:string;csrf:string;body:Record<string,unknown>};
async function fixture(page:Page){
 const writes:Mutation[]=[];
 let title="QA Representative";
 let suspended=false;
 let rights:Permissions={...initial};
 let revision=0;
 let sessions=2;
 const audit:Array<Record<string,unknown>>=[];
 await page.route("**/api/v1/session/status",r=>r.fulfill({json:{
  authenticated:true,user,csrf_token:csrf,expires_unix:1900000000,
 }}));
 await page.route("**/api/v1/fleet",r=>r.fulfill({json:{
  as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,
  nodes:[],metric_scope:"default_route_interface_not_billable",
  physical_rates_complete:false,interface_rx_bits_per_second:0,interface_tx_bits_per_second:0,
 }}));
 await page.route("**/api/v1/workspace/**",async route=>{
  const req=route.request(),url=new URL(req.url()),path=url.pathname;
  if(req.method()==="POST"){
   const body=req.postDataJSON() as Record<string,unknown>;
   writes.push({path,csrf:req.headers()["x-csrf-token"]??"",body});
   if(req.headers()["x-csrf-token"]!==csrf)
    return route.fulfill({status:403,json:{error:"invalid_csrf"}});
   if(path==="/api/v1/workspace/representatives/rep-a/control"){
    if(body.expected_revision!==revision)
     return route.fulfill({status:409,json:{error:"business_event_or_identity_conflict"}});
    audit.unshift({
     event_id:body.event_id,representative_id:"rep-a",
     previous_name:title,new_name:body.display_name,
     previous_login_suspended:suspended,new_login_suspended:body.login_suspended,
     reason:body.reason,created_unix:Math.floor(Date.now()/1000),
    });
    title=String(body.display_name);
    suspended=Boolean(body.login_suspended);
    rights=body.permissions as Permissions;
    revision++;
    sessions=0;
    return route.fulfill({json:{control:control(),
     replayed:false,sessions_revoked:true,
     balance_changed:false,clients_deleted:false,customer_traffic_changed:false}});
   }
   return route.fulfill({status:403,json:{error:"unexpected_mutation"}});
  }
  function control(){
   return {representative:{id:"rep-a",display_name:title,kind:"representative",enabled:true,
     volume_credit_bytes:10737418240,unlimited_credit_units:100},
    login_suspended:suspended,permissions:rights,revision,active_sessions:sessions};
  }
  if(path==="/api/v1/workspace/me"){
   return route.fulfill({json:{user,data_plane:"not_activated",balance:{
    id:"owner",display_name:"Primary owner",kind:"owner",enabled:true,
    volume_credit_bytes:0,unlimited_credit_units:0}}});
  }
  if(path==="/api/v1/workspace/representatives"){
   const q=url.searchParams,limit=Number(q.get("limit")??50),offset=Number(q.get("offset")??0);
   const find=q.get("q")??"",state=q.get("state")??"",sort=q.get("sort")??"";
   const all=[
    {id:"owner",display_name:"Primary owner",kind:"owner",enabled:true,
     volume_credit_bytes:0,unlimited_credit_units:0,client_count:0,archived_count:0,created_unix:0},
    {id:"rep-a",display_name:title,kind:"representative",enabled:true,
     volume_credit_bytes:10737418240,unlimited_credit_units:100,client_count:7,archived_count:1,created_unix:10},
   ].filter(r=>!find||r.id.includes(find)||r.display_name.toLowerCase().includes(find.toLowerCase()));
   const selected=all.slice(offset,offset+limit);
   return route.fulfill({json:{representatives:selected,total:all.length,limit,offset,
    q:find,state,sort,has_more:offset+selected.length<all.length}});
  }
  if(path==="/api/v1/workspace/representatives/rep-a/control")return route.fulfill({json:control()});
  if(path==="/api/v1/workspace/representatives/rep-a/activity")return route.fulfill({json:{
    representative_id:"rep-a",saved_clients:7,archived_clients:1,
    recorded_client_creations:9,recorded_client_renewals:4,
    recorded_profile_edits:2,recorded_credit_adjustments:3,
    available_volume_bytes:"10737418240",available_unlimited_units:"100",
    metric_scope:"saved_records_and_immutable_events_not_live_xray_traffic",
  }});
  if(path==="/api/v1/workspace/representatives/rep-a/control/events"){
   return route.fulfill({json:{events:audit,limit:25,offset:0,audit:"append_only"}});
  }
  if(path==="/api/v1/workspace/events")return route.fulfill({json:{events:[]}});
  return route.fulfill({status:404,json:{error:"not_found"}});
 });
 return writes;
}

test("V9 owner edits name, suspends login and restricts abilities only after confirmation",async({page})=>{
 const writes=await fixture(page);
 await page.goto("/representatives");
 const directory=page.getByRole("region",{name:"Representative directory"});
 await expect(directory.getByText("Results: 2")).toBeVisible();
 await directory.getByRole("button",{name:"Manage account"}).click();
 const manager=page.getByRole("region",{name:"Representative account manager"});
 await expect(manager.getByLabel("Representative display name")).toHaveValue("QA Representative");
 await expect(manager.getByText("Saved clients")).toBeVisible();
 await expect(manager.getByText("7",{exact:true})).toBeVisible();
 await expect(manager.getByText("Active login sessions: 2")).toBeVisible();
 await manager.getByLabel("Representative display name").fill("Manual representative name");
 await manager.getByLabel("Suspend representative login").check();
 await manager.getByLabel("Permission create_clients").uncheck();
 await manager.getByLabel("Permission view_finance").uncheck();
 await expect(manager.getByRole("button",{name:"Review and save account changes"})).toBeDisabled();
 await manager.getByLabel("Representative audit reason").fill("Approved representative sales limits");
 await expect(manager.getByRole("button",{name:"Review and save account changes"})).toBeEnabled();
 expect(writes).toHaveLength(0);
 page.once("dialog",dialog=>dialog.dismiss());
 await manager.getByRole("button",{name:"Review and save account changes"}).click();
 expect(writes).toHaveLength(0);
 page.once("dialog",dialog=>dialog.accept());
 await manager.getByRole("button",{name:"Review and save account changes"}).click();
 await expect(manager.getByLabel("Representative display name")).toHaveValue("Manual representative name");
 await expect(manager.getByLabel("Suspend representative login")).toBeChecked();
 await expect(manager.getByText("Active login sessions: 0")).toBeVisible();
 await expect(manager.getByText("Approved representative sales limits")).toBeVisible();
 expect(writes).toHaveLength(1);
 expect(writes[0].path).toBe("/api/v1/workspace/representatives/rep-a/control");
 expect(writes[0].csrf).toBe(csrf);
 expect(writes[0].body).toMatchObject({
  representative_id:"rep-a",expected_revision:0,display_name:"Manual representative name",
  login_suspended:true,reason:"Approved representative sales limits",
  permissions:{create_clients:false,view_finance:false,view_clients:true,renew_clients:true},
 });
 await expect(directory.getByText("Manual representative name")).toBeVisible();
});

test("V9 Persian 320px representative control is usable and cannot write until confirmed",async({page})=>{
 await page.setViewportSize({width:320,height:740});
 const writes=await fixture(page);
 await page.goto("/representatives");
 await page.getByRole("button",{name:"Switch language"}).click();
 const directory=page.getByRole("region",{name:"Representative directory"});
 await directory.getByRole("button",{name:"مدیریت حساب"}).click();
 const manager=page.getByRole("region",{name:"Representative account manager"});
 await expect(manager.getByText("آمار ثبت‌شدهٔ واقعی (نه ترافیک آنلاین)")).toBeVisible();
 await manager.getByLabel("Permission renew_clients").uncheck();
 await manager.getByLabel("Representative audit reason").fill("محدودسازی تمدید نماینده");
 await expect(manager.getByRole("button",{name:"بررسی و ذخیرهٔ تنظیمات"})).toBeEnabled();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(writes).toHaveLength(0);
});

test("V9 corrupted or unverified control records fail closed without owner mutations",async({page})=>{
 const writes=await fixture(page);
 await page.route("**/api/v1/workspace/representatives/rep-a/control",route=>route.fulfill({
  json:{representative:{id:"rep-b",display_name:"Another tenant",kind:"representative",enabled:true,
   volume_credit_bytes:0,unlimited_credit_units:0},
   login_suspended:false,permissions:initial,revision:0,active_sessions:0},
 }));
 await page.goto("/representatives");
 await page.getByRole("region",{name:"Representative directory"})
  .getByRole("button",{name:"Manage account"}).click();
 const manager=page.getByRole("region",{name:"Representative account manager"});
 await expect(manager.getByText("Could not verify account settings. Actions are blocked.")).toBeVisible();
 await expect(manager.getByRole("button",{name:"Review and save account changes"})).toHaveCount(0);
 expect(writes).toHaveLength(0);
});
