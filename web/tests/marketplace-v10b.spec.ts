import { expect, test, type Page } from "@playwright/test";

const csrf="c".repeat(64);
type Role="owner"|"representative";
type Policy={representative_id:string;manage_products:boolean;can_publish:boolean;
 allow_volume:boolean;allow_unlimited:boolean;min_price_toman:number;max_price_toman:number;
 max_duration_days:number;max_ip_count:number;max_volume_bytes:number;revision:number;updated_unix:number};
type Write={path:string;body:Record<string,unknown>};
function defaultPolicy():Policy{return {representative_id:"rep-a",manage_products:false,can_publish:false,
 allow_volume:true,allow_unlimited:false,min_price_toman:1,max_price_toman:1e12,
 max_duration_days:3650,max_ip_count:1000,max_volume_bytes:9e15,revision:0,updated_unix:0};}
async function fixture(page:Page,role:Role,startingPolicy:Policy){
 const writes:Write[]=[];
 const user={id:role==="owner"?"owner":"rep-a",username:role==="owner"?"proowner":"rep-a",
  role,representative_id:role==="owner"?"owner":"rep-a",permissions:{view_clients:true}};
 let policy={...startingPolicy};
 const now=Math.floor(Date.now()/1000);
 const products=[{
  id:"rep-draft",representative_id:"rep-a",name:"My unpublished draft",plan_kind:"volume",
  ip_count:0,duration_days:30,volume_bytes:10*1024**3,price_toman:45000,
  status:"draft",revision:1,created_unix:now,updated_unix:now,
 },{
  id:"rep-published",representative_id:"rep-a",name:"Published offer",plan_kind:"volume",
  ip_count:0,duration_days:30,volume_bytes:5*1024**3,price_toman:60000,
  status:"published",revision:1,created_unix:now-2,updated_unix:now,
 },{
  id:"other-rep",representative_id:"rep-b",name:"Foreign offer",plan_kind:"volume",
  ip_count:0,duration_days:30,volume_bytes:1*1024**3,price_toman:75000,
  status:"published",revision:1,created_unix:now-3,updated_unix:now,
 }];
 await page.route("**/api/v1/session/status",r=>r.fulfill({json:{
  authenticated:true,user,csrf_token:csrf,expires_unix:now+36000,
 }}));
 await page.route("**/api/v1/fleet",r=>r.fulfill({json:{
  as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,nodes:[],
  metric_scope:"default_route_interface_not_billable",physical_rates_complete:false,
  interface_rx_bits_per_second:0,interface_tx_bits_per_second:0,
 }}));
 await page.route("**/api/v1/workspace/**",async route=>{
  const req=route.request(),uri=new URL(req.url()),path=uri.pathname;
  if(req.method()==="POST"){
   const body=req.postDataJSON() as Record<string,unknown>;
   writes.push({path,body});
   if(req.headers()["x-csrf-token"]!==csrf)return route.fulfill({status:403,json:{error:"invalid_csrf"}});
   if(path==="/api/v1/workspace/representatives/rep-a/catalog-policy"){
    if(role!=="owner")return route.fulfill({status:403,json:{error:"owner_only"}});
    if(body.expected_revision!==policy.revision)return route.fulfill({status:409,json:{error:"stale_version"}});
    policy={...policy,...body,revision:policy.revision+1,updated_unix:now} as Policy;
    return route.fulfill({json:{policy,sessions_revoked:true,credit_changed:false,clients_changed:false,
     applied_to_xray:false,replayed:false}});
   }
   if(path==="/api/v1/workspace/products"){
    if(role==="representative"&&!policy.manage_products)return route.fulfill({status:403,json:{error:"not_allowed"}});
    const product={...body,revision:1,status:"draft",created_unix:now,updated_unix:now};
    products.unshift(product as typeof products[number]);
    return route.fulfill({status:201,json:{product,checkout_enabled:false,applied_to_xray:false,billing_changed:false}});
   }
   if(path.endsWith("/update")){
    const id=path.split("/")[5];
    const item=products.find(p=>p.id===id);
    if(!item||item.revision!==body.expected_revision)return route.fulfill({status:409,json:{error:"conflict"}});
    Object.assign(item,body,{revision:item.revision+1,updated_unix:now});
    return route.fulfill({json:{product:item,checkout_enabled:false,applied_to_xray:false,billing_changed:false}});
   }
   return route.fulfill({status:403,json:{error:"unexpected_write"}});
  }
  if(path==="/api/v1/workspace/me")return route.fulfill({json:{user,data_plane:"not_activated",
   balance:{id:user.representative_id,display_name:"QA",kind:role,enabled:true,
    volume_credit_bytes:0,unlimited_credit_units:0}}});
  if(path==="/api/v1/workspace/representatives")return route.fulfill({json:{
   representatives:[{id:"rep-a",display_name:"Rep A",kind:"representative",enabled:true,
    volume_credit_bytes:0,unlimited_credit_units:0}],
  }});
  if(path==="/api/v1/workspace/representatives/rep-a/catalog-policy")return route.fulfill({json:policy});
  if(path==="/api/v1/workspace/representatives/rep-a/catalog-policy/events")return route.fulfill({
   json:{events:[],limit:20,offset:0,audit:"append_only"}});
  if(path==="/api/v1/workspace/products/policy")return route.fulfill({json:policy});
  if(path==="/api/v1/workspace/products/manage"&&(!policy.manage_products||role==="owner")){
   return route.fulfill({status:409,json:{error:"catalog_denied"}});
  }
  if(path==="/api/v1/workspace/products"||path==="/api/v1/workspace/products/manage"){
   const q=uri.searchParams,limit=Number(q.get("limit")??50),offset=Number(q.get("offset")??0);
   const term=q.get("q")??"",rep=q.get("representative_id")??"",state=q.get("state")??"all";
   const effectiveRep=role==="owner"?rep:"rep-a";
   const managed=path.endsWith("/manage");
   const effectiveState=role==="owner"||managed?state:"published";
   const rows=products.filter(p=>(!effectiveRep||p.representative_id===effectiveRep)&&
    (!term||p.id.includes(term)||p.name.toLowerCase().includes(term.toLowerCase()))&&
    (effectiveState==="all"||effectiveState===""||p.status===effectiveState));
   const selected=rows.slice(offset,offset+limit);
   return route.fulfill({json:{products:selected,total:rows.length,
    limit,offset,q:term,state:effectiveState,representative_id:effectiveRep,
    has_more:offset+selected.length<rows.length}});
  }
  return route.fulfill({status:404,json:{error:"not_found"}});
 });
 return {writes,products,getPolicy:()=>policy};
}

test("V10b owner grants bounded products in a separate audited account policy",async({page})=>{
 const {writes}=await fixture(page,"owner",defaultPolicy());
 await page.goto("/marketplace");
 const policy=page.getByRole("region",{name:"Owner catalog policy"});
 await policy.getByLabel("Policy representative").selectOption("rep-a");
 await expect(policy.getByLabel("Policy manage_products")).not.toBeChecked();
 await policy.getByLabel("Policy manage_products").check();
 await policy.getByLabel("Policy can_publish").check();
 await policy.getByLabel("Policy allow_unlimited").check();
 await policy.getByLabel("Policy min_price_toman").fill("50000");
 await policy.getByLabel("Policy max_duration_days").fill("60");
 await policy.getByLabel("Policy audit reason").fill("Owner limits offers and price floor");
 expect(writes).toHaveLength(0);
 page.once("dialog",d=>d.dismiss());
 await policy.getByRole("button",{name:"Save permissions and limits"}).click();
 expect(writes).toHaveLength(0);
 page.once("dialog",d=>d.accept());
 await policy.getByRole("button",{name:"Save permissions and limits"}).click();
 await expect(policy.getByLabel("Policy manage_products")).toBeChecked();
 expect(writes).toHaveLength(1);
 expect(writes[0].path).toContain("catalog-policy");
 expect(writes[0].body).toMatchObject({
  expected_revision:0,representative_id:"rep-a",manage_products:true,can_publish:true,
  min_price_toman:50000,max_duration_days:60,
 });
 expect(writes.some(w=>/credits|clients/.test(w.path))).toBe(false);
});

test("V10b seller with explicit policy can manage own draft but cannot undercut floor or publish",async({page})=>{
 const bounded:Policy={...defaultPolicy(),manage_products:true,can_publish:false,
  min_price_toman:20000,max_price_toman:90000,max_duration_days:60,max_ip_count:5,
  max_volume_bytes:20*1024**3,revision:1,updated_unix:Math.floor(Date.now()/1000)};
 const {writes}=await fixture(page,"representative",bounded);
 await page.goto("/marketplace");
 const editor=page.getByRole("region",{name:"Marketplace product editor"});
 const directory=page.getByRole("region",{name:"Marketplace directory"});
 await expect(editor.getByLabel("Catalog representative")).toHaveValue("rep-a");
 await expect(directory.getByText("My unpublished draft")).toBeVisible();
 await expect(directory.getByText("Foreign offer")).toHaveCount(0);
 await editor.getByLabel("Catalog ID").fill("rep-new-offer");
 await editor.getByLabel("Catalog name").fill("Custom representative offer");
 await editor.getByLabel("Catalog GB").fill("10");
 await editor.getByLabel("Catalog price").fill("10000");
 await editor.getByLabel("Catalog reason").fill("Trying under owner price floor");
 await expect(editor.getByRole("button",{name:"Create draft"})).toBeDisabled();
 await editor.getByLabel("Catalog price").fill("30000");
 await expect(editor.getByRole("button",{name:"Create draft"})).toBeEnabled();
 page.once("dialog",d=>d.accept());
 await editor.getByRole("button",{name:"Create draft"}).click();
 await expect(directory.getByText("Custom representative offer")).toBeVisible();
 expect(writes).toHaveLength(1);
 expect(writes[0].body).toMatchObject({
  id:"rep-new-offer",representative_id:"rep-a",price_toman:30000,
 });
 const item=directory.locator(".catalog-product").filter({hasText:"rep-new-offer"});
 await item.getByRole("button",{name:"Manage"}).click();
 await editor.getByLabel("Catalog status").selectOption("published");
 await editor.getByLabel("Catalog reason").fill("Unauthorized self-publish");
 await expect(editor.getByRole("button",{name:"Save product"})).toBeDisabled();
 expect(writes.filter(w=>w.path.endsWith("/update"))).toHaveLength(0);
});

test("V10b default disabled rep and Persian 320px never expose unauthorized management",async({page})=>{
 await page.setViewportSize({width:320,height:740});
 const {writes}=await fixture(page,"representative",defaultPolicy());
 await page.goto("/marketplace");
 await page.getByRole("button",{name:"Switch language"}).click();
 await expect(page.getByRole("region",{name:"Marketplace product editor"})).toHaveCount(0);
 const directory=page.getByRole("region",{name:"Marketplace directory"});
 await expect(directory.getByText("Published offer")).toBeVisible();
 await expect(directory.getByText("My unpublished draft")).toHaveCount(0);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(writes).toHaveLength(0);
});
