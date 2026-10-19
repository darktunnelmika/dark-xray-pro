import { expect, test, type Page } from "@playwright/test";

type SessionUser={id:string;username:string;role:"owner"|"representative";representative_id:string};
const csrf="c".repeat(64);
type Spec={id:string;representative_id:string;name:string;plan_kind:string;duration_days:number;
 ip_count:number;volume_bytes:number;price_toman:number;status:string;revision:number;created_unix:number;updated_unix:number};
type Write={path:string;body:Record<string,unknown>;csrf:string};
async function setup(page:Page,user:SessionUser){
 const writes:Write[]=[];
 const now=Math.floor(Date.now()/1000);
 const products:Spec[]=[
  {id:"visible-a",representative_id:"rep-a",name:"Published own",plan_kind:"volume",
   duration_days:30,ip_count:0,volume_bytes:10*1024**3,price_toman:35000,status:"published",revision:2,
   created_unix:now-1,updated_unix:now},
  {id:"draft-a",representative_id:"rep-a",name:"Owner draft",plan_kind:"unlimited",
   duration_days:60,ip_count:3,volume_bytes:0,price_toman:80000,status:"draft",revision:1,
   created_unix:now-2,updated_unix:now},
  {id:"foreign-b",representative_id:"rep-b",name:"Foreign published",plan_kind:"volume",
   duration_days:30,ip_count:0,volume_bytes:4*1024**3,price_toman:20000,status:"published",revision:1,
   created_unix:now-3,updated_unix:now},
 ];
 await page.route("**/api/v1/session/status",r=>r.fulfill({json:{
  authenticated:true,user,csrf_token:csrf,expires_unix:now+43200,
 }}));
 await page.route("**/api/v1/fleet",r=>r.fulfill({json:{as_of:new Date().toISOString(),
  total_nodes:0,online_nodes:0,offline_nodes:0,nodes:[],
  metric_scope:"default_route_interface_not_billable",physical_rates_complete:false,
  interface_rx_bits_per_second:0,interface_tx_bits_per_second:0}}));
 await page.route("**/api/v1/workspace/**",async route=>{
  const request=route.request(),url=new URL(request.url()),path=url.pathname;
  if(request.method()==="POST"){
   const body=request.postDataJSON() as Record<string,unknown>;
   writes.push({path,body,csrf:request.headers()["x-csrf-token"]??""});
   if(request.headers()["x-csrf-token"]!==csrf)return route.fulfill({status:403,json:{error:"invalid_csrf"}});
   if(user.role!=="owner")return route.fulfill({status:403,json:{error:"owner_only"}});
   if(path==="/api/v1/workspace/products"){
    const p={...body,status:"draft",revision:1,created_unix:now,updated_unix:now} as unknown as Spec;
    products.unshift(p);
    return route.fulfill({status:201,json:{product:p,checkout_enabled:false,applied_to_xray:false,billing_changed:false}});
   }
   if(path.endsWith("/update")){
    const id=path.split("/")[5],p=products.find(x=>x.id===id);
    if(!p||p.revision!==Number(body.expected_revision))
     return route.fulfill({status:409,json:{error:"conflict"}});
    Object.assign(p,body,{revision:p.revision+1,updated_unix:now});
    return route.fulfill({json:{product:p,checkout_enabled:false,applied_to_xray:false,billing_changed:false}});
   }
   return route.fulfill({status:403,json:{error:"unexpected_write"}});
  }
  if(path==="/api/v1/workspace/me")return route.fulfill({json:{
   user,data_plane:"not_activated",
   balance:{id:user.representative_id,display_name:"QA user",kind:user.role,enabled:true,
    volume_credit_bytes:0,unlimited_credit_units:0},
  }});
  if(path==="/api/v1/workspace/representatives")return route.fulfill({json:{
   representatives:[{id:"rep-a",display_name:"Rep A",kind:"representative",enabled:true,volume_credit_bytes:0,unlimited_credit_units:0},
    {id:"rep-b",display_name:"Rep B",kind:"representative",enabled:true,volume_credit_bytes:0,unlimited_credit_units:0}],
  }});
  if(path==="/api/v1/workspace/products"){
   const q=url.searchParams,limit=Number(q.get("limit")??50),offset=Number(q.get("offset")??0);
   const term=q.get("q")??"",status=q.get("state")??"",repFilter=q.get("representative_id")??"";
   const filter=user.role==="owner"?repFilter:user.representative_id;
   const state=user.role==="owner"?status:"published";
   const rows=products.filter(x=>(!term||x.name.toLowerCase().includes(term.toLowerCase())||x.id.includes(term))&&
     (!filter||x.representative_id===filter)&&(state===""||state==="all"||x.status===state));
   const pageRows=rows.slice(offset,offset+limit);
   return route.fulfill({json:{products:pageRows,total:rows.length,limit,offset,
    q:term,state,representative_id:filter,has_more:offset+pageRows.length<rows.length}});
  }
  return route.fulfill({status:404,json:{error:"not_found"}});
 });
 return {writes,products};
}
test("V10 owner drafts and publishes an offer only after confirmation with no billing",async({page})=>{
 const {writes}=await setup(page,{id:"owner",username:"proowner",role:"owner",representative_id:"owner"});
 await page.goto("/marketplace");
 const directory=page.getByRole("region",{name:"Marketplace directory"});
 await expect(directory.getByText("Published own")).toBeVisible();
 const form=page.getByRole("region",{name:"Marketplace product editor"});
 await form.getByLabel("Catalog ID").fill("new-owner-product");
 await form.getByLabel("Catalog representative").fill("rep-a");
 await form.getByLabel("Catalog name").fill("مولتی توربو 25");
 await form.getByLabel("Catalog GB").fill("25");
 await form.getByLabel("Catalog price").fill("50000");
 await form.getByLabel("Catalog reason").fill("Owner approved draft without checkout");
 expect(writes).toHaveLength(0);
 page.once("dialog",d=>d.dismiss());
 await form.getByRole("button",{name:"Create draft"}).click();
 expect(writes).toHaveLength(0);
 page.once("dialog",d=>d.accept());
 await form.getByRole("button",{name:"Create draft"}).click();
 await expect(directory.getByText("مولتی توربو 25")).toBeVisible();
 expect(writes).toHaveLength(1);
 expect(writes[0].csrf).toBe(csrf);
 expect(writes[0].body).toMatchObject({
  id:"new-owner-product",representative_id:"rep-a",plan_kind:"volume",
  volume_bytes:25*1024**3,price_toman:50000,duration_days:30,ip_count:0,
 });
 const card=directory.locator(".catalog-product").filter({hasText:"new-owner-product"});
 await card.getByRole("button",{name:"Manage"}).click();
 await form.getByLabel("Catalog status").selectOption("published");
 await form.getByLabel("Catalog reason").fill("Owner publishes product after review");
 page.once("dialog",d=>d.accept());
 await form.getByRole("button",{name:"Save product"}).click();
 await expect(directory.locator(".catalog-product").filter({hasText:"new-owner-product"}).getByText("Published")).toBeVisible();
 expect(writes).toHaveLength(2);
 expect(writes[1].body).toMatchObject({status:"published",expected_revision:1,price_toman:50000});
 expect(writes.some(x=>/clients|credits/.test(x.path))).toBe(false);
});
test("V10 representative sees only own published catalog without write or checkout",async({page})=>{
 const {writes}=await setup(page,{id:"rep-a",username:"qa",role:"representative",representative_id:"rep-a"});
 await page.goto("/marketplace");
 const directory=page.getByRole("region",{name:"Marketplace directory"});
 await expect(directory.getByText("Published own")).toBeVisible();
 await expect(directory.getByText("Owner draft")).toHaveCount(0);
 await expect(directory.getByText("Foreign published")).toHaveCount(0);
 await expect(page.getByRole("region",{name:"Marketplace product editor"})).toHaveCount(0);
 await expect(directory.getByText(/Purchase is disabled/).first()).toBeVisible();
 expect(writes).toHaveLength(0);
});
test("V10 Persian 320px owner catalog is readable and confirmation remains required",async({page})=>{
 await page.setViewportSize({width:320,height:730});
 const {writes}=await setup(page,{id:"owner",username:"proowner",role:"owner",representative_id:"owner"});
 await page.goto("/marketplace");
 await page.getByRole("button",{name:"Switch language"}).click();
 const directory=page.getByRole("region",{name:"Marketplace directory"});
 await expect(directory.getByText("Published own")).toBeVisible();
 await expect(page.getByText(/خرید، پرداخت/).first()).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(writes).toHaveLength(0);
});
