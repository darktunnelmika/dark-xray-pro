import { expect, test, type Page } from "@playwright/test";

const csrf="a".repeat(64);
const user={id:"rep-a",username:"rep-a",role:"representative",representative_id:"rep-a"};
type Call={path:string;body:Record<string,unknown>;csrf:string};
async function fixture(page:Page){
 const calls:Call[]=[];
 const clients=Array.from({length:3},(_,i)=>({
  id:"v7-client-"+i,name:"Test Client "+i,representative_id:"rep-a",
  archived:false,deployment_state:"pending_node_apply",plan_kind:"volume",
  ip_count:0,duration_days:30,volume_bytes:1073741824,expires_unix:1890000000,
 }));
 await page.route("**/api/v1/session/status",r=>r.fulfill({json:{
  authenticated:true,user,csrf_token:csrf,expires_unix:1900000000,
 }}));
 await page.route("**/api/v1/fleet",r=>r.fulfill({json:{
  total_nodes:0,online_nodes:0,offline_nodes:0,nodes:[],
  as_of:new Date().toISOString(),metric_scope:"default_route_interface_not_billable",
  physical_rates_complete:false,interface_rx_bits_per_second:0,interface_tx_bits_per_second:0,
 }}));
 await page.route("**/api/v1/workspace/**",async r=>{
  const req=r.request(),path=new URL(req.url()).pathname;
  if(req.method()==="POST"){
   const body=req.postDataJSON() as Record<string,unknown>;
   calls.push({path,body,csrf:req.headers()["x-csrf-token"]??""});
   if(req.headers()["x-csrf-token"]!==csrf){
    return r.fulfill({status:403,json:{error:"invalid_csrf"}});
   }
   if(path.endsWith("/bulk/extend/quote")){
    const ids=body.client_ids as string[];
    const days=body.additional_days as number;
    const bytes=body.additional_volume_bytes as number;
    const selected=ids.map(id=>clients.find(c=>c.id===id));
    if(selected.some(c=>!c))return r.fulfill({status:403,json:{error:"representative_scope_denied"}});
    return r.fulfill({json:{
     quote_hash:"a".repeat(64),quote_unix:Math.floor(Date.now()/1000),
     count:selected.length,sufficient:true,applied_to_xray:false,
     representatives:[{representative_id:"rep-a",owner_exempt:false,
      available_volume_bytes:"10737418240",available_unlimited_units:"100",
      volume_debit_bytes:String(bytes*ids.length),unlimited_debit_units:"0",sufficient:true}],
     clients:selected.map(c=>({client_id:c!.id,representative_id:"rep-a",plan_kind:"volume",
      expires_unix:c!.expires_unix,estimated_expires_unix:c!.expires_unix+days*86400,
      volume_cost_bytes:String(bytes),unlimited_cost_units:"0"})),
    }});
   }
   if(path.endsWith("/bulk/extend/commit")){
    const ids=body.client_ids as string[];
    for(const id of ids){
     const c=clients.find(c=>c.id===id);
     if(c){c.duration_days+=Number(body.additional_days);c.expires_unix+=Number(body.additional_days)*86400;}
    }
    return r.fulfill({json:{count:ids.length,replayed:false,
     applied_to_xray:false,customer_traffic_changed:false,client_or_usage_history_deleted:false}});
   }
   return r.fulfill({status:403,json:{error:"unexpected_write"}});
  }
  if(path.endsWith("/me"))return r.fulfill({json:{user,data_plane:"not_activated",
   balance:{id:"rep-a",display_name:"Test Rep",kind:"representative",enabled:true,
    volume_credit_bytes:10737418240,unlimited_credit_units:100}}});
  if(path.endsWith("/clients")){
   const q=new URL(req.url()).searchParams;
   const limit=Number(q.get("limit")??50),offset=Number(q.get("offset")??0);
   const search=q.get("q")??"",state=q.get("state")??"all",plan=q.get("plan")??"all",sort=q.get("sort")??"newest";
   const filtered=clients.filter(c=>!search||c.id.includes(search)||c.name.includes(search));
   const selected=filtered.slice(offset,offset+limit);
   return r.fulfill({json:{clients:selected,total:filtered.length,has_more:offset+selected.length<filtered.length,
    limit,offset,q:search,state,plan,sort,representative_id:"rep-a"}});
  }
  if(path.endsWith("/client-operations"))return r.fulfill({json:{operations:[]}});
  if(path.endsWith("/events"))return r.fulfill({json:{events:[]}});
  if(path.endsWith("/groups"))return r.fulfill({json:{groups:[]}});
  return r.fulfill({status:404,json:{error:"not_found"}});
 });
 return calls;
}

test("V7 quote does not charge, cancel does not charge, explicit commit does",async({page})=>{
 const calls=await fixture(page);
 await page.goto("/clients");
 const directory=page.getByRole("region",{name:"Client directory"});
 await directory.getByLabel("Select client v7-client-0").check();
 await directory.getByLabel("Select client v7-client-1").check();
 await directory.locator(".clients-bulk-fields select").selectOption("extend");
 const renew=page.getByRole("region",{name:"Bulk client renewal"});
 await renew.getByLabel("Add days").fill("60");
 await renew.getByLabel("Add volume (GB)").fill("0.25");
 await directory.getByLabel("Required audit reason").fill("Paid renewal approved");
 await expect(renew.getByRole("button",{name:"Confirm and renew all"})).toHaveCount(0);
 await renew.getByRole("button",{name:"Review exact charges"}).click();
 await expect(renew.getByRole("region",{name:"Verified bulk renewal quote"})).toBeVisible();
 expect(calls.filter(c=>c.path.endsWith("/extend/commit"))).toHaveLength(0);
 page.once("dialog",d=>d.dismiss());
 await renew.getByRole("button",{name:"Confirm and renew all"}).click();
 expect(calls.filter(c=>c.path.endsWith("/extend/commit"))).toHaveLength(0);
 page.once("dialog",d=>d.accept());
 await renew.getByRole("button",{name:"Confirm and renew all"}).click();
 await expect(page.getByRole("status").filter({hasText:"The entire renewal batch was saved once."})).toBeVisible();
 const posts=calls.filter(c=>c.path.endsWith("/extend/commit"));
 expect(posts).toHaveLength(1);
 expect(posts[0].csrf).toBe(csrf);
 expect(posts[0].body).toMatchObject({client_ids:["v7-client-0","v7-client-1"],
  additional_days:60,additional_volume_bytes:268435456,reason:"Paid renewal approved",
  quote_hash:"a".repeat(64)});
});

test("V7 changing selected clients, terms or audit reason invalidates quote",async({page})=>{
 const calls=await fixture(page);
 await page.goto("/clients");
 const directory=page.getByRole("region",{name:"Client directory"});
 await directory.getByLabel("Select client v7-client-0").check();
 await directory.locator(".clients-bulk-fields select").selectOption("extend");
 const renew=page.getByRole("region",{name:"Bulk client renewal"});
 await directory.getByLabel("Required audit reason").fill("First reviewed payment");
 await renew.getByRole("button",{name:"Review exact charges"}).click();
 await expect(renew.getByRole("button",{name:"Confirm and renew all"})).toBeEnabled();
 await renew.getByLabel("Add days").fill("90");
 await expect(renew.getByRole("button",{name:"Confirm and renew all"})).toHaveCount(0);
 await renew.getByRole("button",{name:"Review exact charges"}).click();
 await expect(renew.getByRole("button",{name:"Confirm and renew all"})).toBeEnabled();
 await directory.getByLabel("Select client v7-client-1").check();
 await expect(renew.getByRole("button",{name:"Confirm and renew all"})).toHaveCount(0);
 await directory.getByLabel("Required audit reason").fill("Different approval");
 await expect(renew.getByRole("button",{name:"Confirm and renew all"})).toHaveCount(0);
 expect(calls.filter(c=>c.path.endsWith("/extend/commit"))).toHaveLength(0);
});

test("V7 Persian 320px quote is responsive without an implicit debit",async({page})=>{
 await page.setViewportSize({width:320,height:750});
 const calls=await fixture(page);
 await page.goto("/clients");
 await page.getByRole("button",{name:"Switch language"}).click();
 const directory=page.getByRole("region",{name:"Client directory"});
 await directory.getByLabel("انتخاب کلاینت v7-client-0").check();
 await directory.getByLabel("عملیات").selectOption("extend");
 const renew=page.getByRole("region",{name:"Bulk client renewal"});
 await directory.getByLabel("دلیل ضروری برای حسابرسی").fill("تمدید با تایید");
 await renew.getByRole("button",{name:"بررسی هزینهٔ دقیق"}).click();
 await expect(renew.getByRole("region",{name:"Verified bulk renewal quote"})).toBeVisible();
 await expect(renew.getByText(/نماینده: rep-a/)).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(calls.filter(c=>c.path.endsWith("/extend/commit"))).toHaveLength(0);
});
