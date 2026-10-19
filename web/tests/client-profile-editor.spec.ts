import { expect, test, type Page } from "@playwright/test";

const csrf="a".repeat(64);
const user={id:"rep-a",username:"rep-a",role:"representative",representative_id:"rep-a"};
type Call={path:string;method:string;body:Record<string,unknown>};
async function fixture(page:Page){
 const calls:Call[]=[];
 const clients=[
  {id:"edit-volume",name:"Original volume",uuid:"12345678-1234-1234-1234-123456789abc",
   representative_id:"rep-a",plan_kind:"volume",ip_count:0,volume_bytes:1073741824,
   duration_days:30,charged_volume_bytes:1073741824,charged_unlimited_units:0,
   deployment_state:"pending_node_apply",archived:false,expires_unix:1900000000},
  {id:"edit-unlimited",name:"Original unlimited",uuid:"23456789-1234-1234-1234-123456789abc",
   representative_id:"rep-a",plan_kind:"unlimited",ip_count:2,volume_bytes:0,
   duration_days:30,charged_volume_bytes:0,charged_unlimited_units:2,
   deployment_state:"disabled",archived:false,expires_unix:1900000000},
 ];
 const history:Array<Record<string,unknown>>=[];
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
   calls.push({path,method:"POST",body});
   if(req.headers()["x-csrf-token"]!==csrf)return route.fulfill({status:403,json:{error:"invalid_csrf"}});
   const id=path.split("/")[5];
   const c=clients.find(row=>row.id===id);
   if(!c)return route.fulfill({status:404,json:{error:"client_not_found"}});
   if(path.endsWith("/edit/quote")){
    const days=Number(body.additional_days),volume=Number(body.additional_volume_bytes);
    const ip=Number(body.ip_count),name=String(body.name);
    const targetUnits=Math.ceil((c.duration_days+days)*ip/30);
    const units=c.plan_kind==="unlimited"?targetUnits-c.charged_unlimited_units:0;
    return route.fulfill({json:{
     quote_hash:"a".repeat(64),quote_unix:Math.floor(Date.now()/1000),
     client_id:c.id,representative_id:c.representative_id,plan_kind:c.plan_kind,
     previous_name:c.name,new_name:name,previous_ip_count:c.ip_count,new_ip_count:ip,
     previous_expires_unix:c.expires_unix,new_expires_unix:c.expires_unix+days*86400,
     added_volume_bytes:String(volume),
     volume_credit_debit_bytes:String(c.plan_kind==="volume"?volume:0),
     unlimited_credit_debit_units:String(units),
     available_volume_credit_bytes:"10737418240",available_unlimited_units:"100",
     owner_exempt:false,sufficient:true,applied_to_xray:false,credit_refunded:false,
    }});
   }
   if(path.endsWith("/edit/commit")){
    const before={...c};
    c.name=String(body.name);c.ip_count=Number(body.ip_count);
    c.duration_days+=Number(body.additional_days);
    c.expires_unix+=Number(body.additional_days)*86400;
    c.volume_bytes+=Number(body.additional_volume_bytes);
    if(c.plan_kind==="volume")c.charged_volume_bytes+=Number(body.additional_volume_bytes);
    else c.charged_unlimited_units=Math.ceil(c.duration_days*c.ip_count/30);
    history.push({event_id:body.event_id,client_id:c.id,representative_id:c.representative_id,
     previous_name:before.name,new_name:c.name,plan_kind:c.plan_kind,
     previous_ip_count:before.ip_count,new_ip_count:c.ip_count,
     added_days:body.additional_days,added_volume_bytes:String(body.additional_volume_bytes),
     volume_credit_debit_bytes:String(c.plan_kind==="volume"?body.additional_volume_bytes:0),
     unlimited_credit_debit_units:String(c.plan_kind==="unlimited"?c.charged_unlimited_units-before.charged_unlimited_units:0),
     previous_expires_unix:before.expires_unix,new_expires_unix:c.expires_unix,
     reason:body.reason,created_unix:Math.floor(Date.now()/1000)});
    return route.fulfill({json:{client:c,replayed:false,applied_to_xray:false,
     credit_refunded:false,customer_traffic_changed:false,client_or_usage_history_deleted:false}});
   }
   return route.fulfill({status:403,json:{error:"unapproved_write"}});
  }
  if(path==="/api/v1/workspace/me")return route.fulfill({json:{
   user,data_plane:"not_activated",balance:{
    id:"rep-a",display_name:"QA Rep A",kind:"representative",enabled:true,
    volume_credit_bytes:10737418240,unlimited_credit_units:100,
   },
  }});
  if(path==="/api/v1/workspace/clients"){
   const params=url.searchParams;
   const limit=Number(params.get("limit")??50),offset=Number(params.get("offset")??0);
   const q=params.get("q")??"",state=params.get("state")??"all",plan=params.get("plan")??"all",sort=params.get("sort")??"newest";
   const rows=clients.filter(c=>!q||c.name.toLowerCase().includes(q.toLowerCase())||c.id.includes(q));
   return route.fulfill({json:{
    clients:rows.slice(offset,offset+limit),total:rows.length,limit,offset,q,state,plan,sort,
    has_more:offset+Math.min(limit,rows.length-offset)<rows.length,representative_id:"rep-a",
   }});
  }
  if(path.startsWith("/api/v1/workspace/clients/")){
   const bits=path.split("/"),id=bits[5],c=clients.find(c=>c.id===id);
   if(!c)return route.fulfill({status:404,json:{error:"unknown_client"}});
   if(path.endsWith("/edits")){
    return route.fulfill({json:{edits:history.filter(h=>h.client_id===id),limit:20,offset:0,audit:"append_only"}});
   }
   if(bits.length===6)return route.fulfill({json:c});
  }
  if(path.endsWith("/client-operations"))return route.fulfill({json:{operations:[]}});
  if(path.endsWith("/groups"))return route.fulfill({json:{groups:[]}});
  if(path.endsWith("/events"))return route.fulfill({json:{events:[]}});
  return route.fulfill({status:404,json:{error:"not_found"}});
 });
 return calls;
}
test("V8 volume customer edits name/IP/GB only after quote and confirmation",async({page})=>{
 const calls=await fixture(page);
 await page.goto("/clients");
 const directory=page.getByRole("region",{name:"Client directory"});
 await directory.locator(".clients-explorer-row").filter({hasText:"edit-volume"}).getByRole("button",{name:"Manage"}).click();
 const editor=page.getByRole("region",{name:"Client profile editor"});
 await expect(editor.getByLabel("Edit client name")).toHaveValue("Original volume");
 await editor.getByLabel("Edit client name").fill("Manual editable name");
 await editor.getByLabel("Edit client IP slots").fill("5");
 await editor.getByLabel("Edit client additional GB").fill("1");
 await editor.getByLabel("Edit client audit reason").fill("Reviewed 1 GB + 5 IPs");
 await expect(editor.getByRole("button",{name:"Confirm and save edit"})).toHaveCount(0);
 await editor.getByRole("button",{name:"Review authoritative price"}).click();
 await expect(editor.getByRole("region",{name:"Verified client edit quote"})).toBeVisible();
 expect(calls.filter(x=>x.path.endsWith("/edit/commit"))).toHaveLength(0);
 page.once("dialog",d=>d.dismiss());
 await editor.getByRole("button",{name:"Confirm and save edit"}).click();
 expect(calls.filter(x=>x.path.endsWith("/edit/commit"))).toHaveLength(0);
 page.once("dialog",d=>d.accept());
 await editor.getByRole("button",{name:"Confirm and save edit"}).click();
 await expect(page.getByRole("status").filter({hasText:"Client edit saved with an immutable audit receipt."})).toBeVisible();
 const saves=calls.filter(x=>x.path.endsWith("/edit/commit"));
 expect(saves).toHaveLength(1);
 expect(saves[0].body).toMatchObject({
  client_id:"edit-volume",name:"Manual editable name",plan_kind:"volume",ip_count:5,
  additional_days:0,additional_volume_bytes:1073741824,reason:"Reviewed 1 GB + 5 IPs",
 });
 await expect(editor.getByText("Manual editable name").first()).toBeVisible();
 await expect(editor.locator(".profile-edit-history>div")).toHaveCount(1);
});
test("V8 protects unlimited paid slots and stale reviewed price",async({page})=>{
 const calls=await fixture(page);
 await page.goto("/clients");
 const directory=page.getByRole("region",{name:"Client directory"});
 await directory.locator(".clients-explorer-row").filter({hasText:"edit-unlimited"}).getByRole("button",{name:"Manage"}).click();
 const editor=page.getByRole("region",{name:"Client profile editor"});
 await expect(editor.getByLabel("Edit client IP slots")).toHaveValue("2");
 await editor.getByLabel("Edit client IP slots").fill("1");
 await editor.getByLabel("Edit client audit reason").fill("Not allowed downgrade");
 await expect(editor.getByRole("button",{name:"Review authoritative price"})).toBeDisabled();
 await editor.getByLabel("Edit client IP slots").fill("5");
 await editor.getByLabel("Edit client additional days").fill("30");
 await editor.getByLabel("Edit client audit reason").fill("Increase users and duration");
 await editor.getByRole("button",{name:"Review authoritative price"}).click();
 await expect(editor.getByRole("region",{name:"Verified client edit quote"})).toBeVisible();
 await editor.getByLabel("Edit client IP slots").fill("6");
 await expect(editor.getByRole("button",{name:"Confirm and save edit"})).toHaveCount(0);
 expect(calls.filter(x=>x.path.endsWith("/edit/commit"))).toHaveLength(0);
});
test("V8 Persian 320px edit quote remains readable without implicit credit debit",async({page})=>{
 await page.setViewportSize({width:320,height:760});
 const calls=await fixture(page);
 await page.goto("/clients");
 await page.getByRole("button",{name:"Switch language"}).click();
 const directory=page.getByRole("region",{name:"Client directory"});
 await directory.locator(".clients-explorer-row").filter({hasText:"edit-volume"}).getByRole("button",{name:"مدیریت"}).click();
 const editor=page.getByRole("region",{name:"Client profile editor"});
 await editor.getByLabel("Edit client name").fill("نام کاربر فارسی");
 await editor.getByLabel("Edit client audit reason").fill("تغییر نام با حسابرسی");
 await editor.getByRole("button",{name:"بررسی هزینه در HUB"}).click();
 await expect(editor.getByRole("region",{name:"Verified client edit quote"})).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(calls.filter(x=>x.path.endsWith("/edit/commit"))).toHaveLength(0);
});
