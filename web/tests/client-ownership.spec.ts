import { expect, test, type Page } from "@playwright/test";
import { readOwnershipQuote, readOwnershipReceipt, ownershipStorageKey,
  type OwnershipIntent, type OwnershipItem, type OwnershipRequest, type OwnershipReceipt, type PendingOwnership } from "../src/business/clientOwnership";

const base="/api/v1/workspace/clients/ownership",csrf="a".repeat(64);
const now=()=>Math.floor(Date.now()/1000);
type Call={path:string;method:string;body:Record<string,unknown>};
const consent="I reviewed the exact clients and destination. Move their management access.";
const intent:OwnershipIntent={client_ids:["customer-a"],source_group_id:"",representative_id:"rep-target",group_id:"",reason:"Owner reviewed transfer"};
function item(id="customer-a",sourceGroup=""):OwnershipItem{return {
 client_id:id,name:"Customer "+id,previous_representative_id:"rep-source",representative_id:"rep-target",previous_group_id:sourceGroup,group_id:"",revision:1,
 usage_present:true,usage_complete:true,uplink_bytes:"9007199254740993",downlink_bytes:"2",reconciliation_holds:0,
};}
async function fixture(page:Page,options:{role?:"owner"|"representative";enabled?:boolean;lose?:boolean;hide?:boolean;reject?:boolean;badReceipt?:boolean;manyReps?:boolean}={}){
 const role=options.role??"owner",user={id:role==="owner"?"owner":"rep-source",username:"ownership-test",role,representative_id:role==="owner"?"owner":"rep-source"};
 const calls:Call[]=[],reads:string[]=[],receipts=new Map<string,OwnershipReceipt>();
 let lose=!!options.lose,hide=!!options.hide,reject=!!options.reject;
 const clients=["a","b","c"].map((v,i)=>({id:"customer-"+v,name:"Customer "+v,uuid:`00000000-0000-4000-8000-00000000000${i}`,representative_id:"rep-source",archived:i===2,
   deployment_state:"pending_node_apply",plan_kind:"volume",duration_days:30,volume_bytes:100,charged_volume_bytes:100,charged_unlimited_units:0,ip_count:2,expires_unix:1900000000,created_unix:1790000000+i}));
 const groups=[{id:"source-group",name:"All old clients",representative_id:"rep-source",member_count:3,color:"",created_unix:1,updated_unix:1},{id:"target-group",name:"Destination group",representative_id:"rep-target",member_count:0,color:"",created_unix:1,updated_unix:1}];
 const reps=[{id:"owner",display_name:"Owner",kind:"owner",enabled:true},{id:"rep-source",display_name:"Original representative",kind:"representative",enabled:true},
 ...Array.from({length:options.manyReps?60:0},(_,i)=>({id:"extra-"+i,display_name:"Middle representative "+String(i).padStart(2,"0"),kind:"representative",enabled:true})),
 {id:"rep-target",display_name:"Target representative",kind:"representative",enabled:true}].map(r=>({...r,volume_credit_bytes:1000,unlimited_credit_units:100,client_count:0,archived_count:0,created_unix:1}));
 await page.route("**/api/v1/**",async route=>{
  const req=route.request(),url=new URL(req.url()),path=url.pathname,q=url.searchParams;
  if(path==="/api/v1/session/status")return route.fulfill({json:{authenticated:true,user,csrf_token:csrf,expires_unix:1900000000}});
  if(path==="/api/v1/fleet")return route.fulfill({json:{as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,nodes:[],metric_scope:"default_route_interface_not_billable",physical_rates_complete:false,interface_rx_bits_per_second:0,interface_tx_bits_per_second:0}});
  if(path===base+"/quote"||path===base+"/commit"){
   const body=req.postDataJSON() as Record<string,unknown>;calls.push({path,method:req.method(),body});
   expect(req.headers()["x-csrf-token"]).toBe(csrf);expect(body).not.toHaveProperty("actor_user_id");expect(body).not.toHaveProperty("actor_id");
   if(role!=="owner")return route.fulfill({status:403,json:{error:"forbidden"}});
   const selection=body.source_group_id?clients:clients.filter(c=>(body.client_ids as string[]).includes(c.id));
   const members=selection.map(c=>({...item(c.id,String(body.source_group_id)),name:c.name,previous_representative_id:c.representative_id,representative_id:String(body.representative_id),group_id:String(body.group_id)}));
   if(path.endsWith("/quote"))return route.fulfill({json:{intent:body,quote_hash:"b".repeat(64),quote_unix:now(),count:members.length,clients:members,
     refund_policy:"original_funder_only_mixed_funding_requires_review",balance_changed:false,usage_reset:false}});
   if(reject){reject=false;return route.fulfill({status:409,json:{error:"operation_conflict"}});}
   let result=receipts.get(String(body.event_id));const replayed=!!result;
   if(!result){result={event_id:String(body.event_id),actor_user_id:user.id,representative_id:String(body.representative_id),group_id:String(body.group_id),count:members.length,clients:members,created_unix:now(),replayed:false,balance_changed:false,usage_reset:false,credentials_changed:false};receipts.set(result.event_id,result);for(const c of selection)c.representative_id=result.representative_id;}
   if(lose){lose=false;return route.abort("failed");}
   if(options.badReceipt)return route.fulfill({json:{...result,balance_changed:true}});
   hide=false;return route.fulfill({json:{...result,replayed}});
  }
  if(path.startsWith(base+"/receipts/")){
   reads.push(path);const result=receipts.get(decodeURIComponent(path.split("/").at(-1)!));
   return hide||!result?route.fulfill({status:404,json:{error:"not_found"}}):route.fulfill({json:options.badReceipt?{...result,balance_changed:true}:result});
  }
  if(path==="/api/v1/workspace/me")return route.fulfill({json:{user,balance:{id:user.representative_id,display_name:"Private account",kind:role,enabled:true,volume_credit_bytes:1000,unlimited_credit_units:100},data_plane:"not_activated",capabilities:{client_ownership_v1:options.enabled!==false&&role==="owner"}}});
  if(path==="/api/v1/workspace/clients"){
   const limit=Number(q.get("limit")??25),offset=Number(q.get("offset")??0),search=q.get("q")??"",state=q.get("state")??"all",plan=q.get("plan")??"all",sort=q.get("sort")??"newest",rep=q.get("representative_id")??(role==="owner"?"":"rep-source");
   const rows=clients.filter(c=>(!rep||c.representative_id===rep)&&(!search||c.name.includes(search)||c.id.includes(search))&&(state!=="archived"||c.archived));
   return route.fulfill({json:{clients:rows.slice(offset,offset+limit),total:rows.length,limit,offset,q:search,state,plan,sort,representative_id:rep,has_more:offset+limit<rows.length}});
  }
  if(path==="/api/v1/workspace/representatives"){
   const limit=Number(q.get("limit")??50),offset=Number(q.get("offset")??0),search=q.get("q")??"",state=q.get("state")??"all",sort=q.get("sort")??"name";
   const rows=reps.filter(r=>!search||(r.display_name+" "+r.id).toLowerCase().includes(search.toLowerCase())).sort((a,b)=>a.display_name.localeCompare(b.display_name));
   return route.fulfill({json:{representatives:rows.slice(offset,offset+limit),total:rows.length,limit,offset,q:search,state,sort,has_more:offset+limit<rows.length}});
  }
  if(path==="/api/v1/workspace/groups"){
   const limit=Number(q.get("limit")??25),offset=Number(q.get("offset")??0),rep=q.get("representative_id");
   const rows=groups.filter(g=>!rep||g.representative_id===rep);return route.fulfill({json:{groups:rows.slice(offset,offset+limit),limit,offset}});
  }
  if(path==="/api/v1/workspace/events")return route.fulfill({json:{events:[]}});
  if(path==="/api/v1/workspace/client-operations")return route.fulfill({json:{operations:[]}});
  return route.fulfill({status:404,json:{error:"not_found"}});
 });
 return {calls,reads,receipts,clients,showReceipts:()=>{hide=false;}};
}
async function openTransfer(page:Page,fa=false){
 await page.goto("/clients");
 const bulk=page.locator(".clients-bulk");
 await bulk.getByRole("combobox",{name:fa?"عملیات":"Action",exact:true}).selectOption("transfer");
 const panel=page.getByRole("region",{name:fa?"واگذاری کاربران به نماینده":"Assign clients to a representative",exact:true});
 await expect(panel).toBeVisible();
 return {bulk,panel};
}
async function prepare(page:Page,fa=false){
 const {bulk,panel}=await openTransfer(page,fa);
 await page.getByRole("checkbox",{name:(fa?"انتخاب کلاینت ":"Select client ")+"customer-a",exact:true}).check();
 await bulk.getByLabel(fa?"دلیل ضروری برای حسابرسی":"Required audit reason",{exact:true}).fill("Owner reviewed transfer");
 await panel.getByRole("button",{name:/Target representative/}).click();
 await panel.getByRole("button",{name:fa?"پیش‌نمایش واگذاری":"Review assignment",exact:true}).click();
 await expect(panel.getByRole("button",{name:fa?"تأیید واگذاری":"Confirm assignment",exact:true})).toBeDisabled();
 return {bulk,panel};
}
const commits=(f:{calls:Call[]})=>f.calls.filter(c=>c.path.endsWith("/commit"));

test("owner reviews exact clients and preserves int64 counters before explicit transfer",async({page})=>{
 const f=await fixture(page);const {panel}=await prepare(page);
 await expect(panel.getByText("9,007,199,254,740,995",{exact:true})).toBeVisible();
 expect(commits(f)).toHaveLength(0);
 await panel.getByRole("checkbox",{name:consent}).check();await panel.getByRole("button",{name:"Confirm assignment",exact:true}).click();
 await expect(panel.getByText("Management assignment recorded",{exact:true})).toBeVisible();
 expect(commits(f)).toHaveLength(1);expect(commits(f)[0].body).toMatchObject({...intent,quote_hash:"b".repeat(64)});
 expect(f.clients[0].representative_id).toBe("rep-target");
});
test("entire group is resolved by the server including archived clients",async({page})=>{
 const f=await fixture(page);const {bulk,panel}=await openTransfer(page);
 await bulk.getByLabel("Required audit reason",{exact:true}).fill("Entire source group");
 await panel.getByLabel("Selection",{exact:true}).selectOption("group");
 await panel.getByLabel("Source group",{exact:true}).selectOption("source-group");
 await panel.getByRole("button",{name:/Target representative/}).click();
 await panel.getByLabel("Destination group",{exact:true}).selectOption("target-group");
 await panel.getByRole("button",{name:"Review assignment",exact:true}).click();
 await expect(panel.locator(".ownership-members article")).toHaveCount(3);
 expect(f.calls[0].body).toMatchObject({client_ids:[],source_group_id:"source-group",representative_id:"rep-target",group_id:"target-group"});
 await panel.getByRole("checkbox",{name:consent}).check();await panel.getByRole("button",{name:"Confirm assignment",exact:true}).click();
 await expect(panel.getByText("Management assignment recorded",{exact:true})).toBeVisible();
 expect(f.clients.every(c=>c.representative_id==="rep-target")).toBe(true);
});
test("closing a preview sends no ownership mutation",async({page})=>{
 const f=await fixture(page);const {panel}=await prepare(page);
 await panel.getByRole("button",{name:"Close preview",exact:true}).click();
 await expect(panel.getByRole("button",{name:"Confirm assignment",exact:true})).toHaveCount(0);expect(commits(f)).toHaveLength(0);
});
test("lost response survives reload and is reconciled without a second commit",async({page})=>{
 const f=await fixture(page,{lose:true,hide:true});const {panel}=await prepare(page);
 await panel.getByRole("checkbox",{name:consent}).check();await panel.getByRole("button",{name:"Confirm assignment",exact:true}).click();
 await expect(panel.getByRole("region",{name:"Saved transfer request"})).toBeVisible();expect(commits(f)).toHaveLength(1);
 f.showReceipts();await page.reload();
 await expect(page.getByText("Management assignment recorded",{exact:true})).toBeVisible();expect(commits(f)).toHaveLength(1);
});
test("unconfirmed outcome only retries an identical persisted body explicitly",async({page})=>{
 const f=await fixture(page,{lose:true,hide:true});const {panel}=await prepare(page);
 await panel.getByRole("checkbox",{name:consent}).check();await panel.getByRole("button",{name:"Confirm assignment",exact:true}).click();
 await expect(panel.getByRole("button",{name:"Retry the identical request"})).toBeEnabled();const original=structuredClone(commits(f)[0].body);
 await page.reload();await page.getByRole("button",{name:"Retry the identical request"}).click();
 await expect(page.getByText("Management assignment recorded",{exact:true})).toBeVisible();expect(commits(f)).toHaveLength(2);expect(commits(f)[1].body).toEqual(original);
});
test("stale quote is never silently committed again with a new request",async({page})=>{
 const f=await fixture(page,{reject:true});const {panel}=await prepare(page);
 await panel.getByRole("checkbox",{name:consent}).check();await panel.getByRole("button",{name:"Confirm assignment",exact:true}).click();
 await expect(panel.getByRole("button",{name:"Discard refused request and review again"})).toBeVisible();expect(commits(f)).toHaveLength(1);
 await panel.getByRole("button",{name:"Discard refused request and review again"}).click();
 await expect(panel.getByRole("button",{name:"Review assignment",exact:true})).toBeEnabled();expect(commits(f)).toHaveLength(1);
});
test("invalid success flags cannot claim a completed financial-safe transfer",async({page})=>{
 const f=await fixture(page,{badReceipt:true});const {panel}=await prepare(page);
 await panel.getByRole("checkbox",{name:consent}).check();await panel.getByRole("button",{name:"Confirm assignment",exact:true}).click();
 await expect(panel.getByRole("region",{name:"Saved transfer request"})).toBeVisible();await expect(panel.getByText("Management assignment recorded",{exact:true})).toHaveCount(0);expect(commits(f)).toHaveLength(1);
});
test("request persistence failure prevents any transfer",async({page})=>{
 await page.addInitScript(()=>{const original=Storage.prototype.setItem;Storage.prototype.setItem=function(key:string,value:string){if(key.startsWith("dark-pro-client-ownership-v1:"))throw new Error("synthetic storage failure");return original.call(this,key,value);};});
 const f=await fixture(page);const {panel}=await prepare(page);
 await panel.getByRole("checkbox",{name:consent}).check();await panel.getByRole("button",{name:"Confirm assignment",exact:true}).click();
 await expect(panel.getByText("The browser could not save the request. No transfer was sent.",{exact:true})).toBeVisible();expect(commits(f)).toHaveLength(0);
});
test("representative and disabled owner capability cannot open transfer controls",async({page})=>{
 const f=await fixture(page,{role:"representative"});await page.goto("/clients");
 await expect(page.getByRole("option",{name:"Assign to representative",exact:true})).toHaveCount(0);
 await expect(page.getByRole("region",{name:"Assign clients to a representative",exact:true})).toHaveCount(0);expect(f.calls).toHaveLength(0);expect(f.reads).toHaveLength(0);
});
test("disabled deployment does not expose an executable assignment",async({page})=>{
 const f=await fixture(page,{enabled:false});await page.goto("/clients");
 await expect(page.getByRole("option",{name:"Assign to representative",exact:true})).toHaveCount(0);expect(f.calls).toHaveLength(0);
});
test("representative search reaches targets beyond the first page",async({page})=>{
 const f=await fixture(page,{manyReps:true});const {bulk,panel}=await openTransfer(page);
 await page.getByRole("checkbox",{name:"Select client customer-a",exact:true}).check();await bulk.getByLabel("Required audit reason",{exact:true}).fill("Search every representative");
 await expect(panel.getByRole("button",{name:/Target representative/})).toHaveCount(0);
 await panel.getByLabel("Search all representatives",{exact:true}).fill("Target");
 await panel.getByRole("button",{name:/Target representative/}).click();await panel.getByRole("button",{name:"Review assignment",exact:true}).click();
 expect(f.calls[0].body.representative_id).toBe("rep-target");expect(commits(f)).toHaveLength(0);
});
test("selection or reason changes invalidate previous consent and preview",async({page})=>{
 const f=await fixture(page);const {bulk,panel}=await prepare(page);
 await panel.getByRole("checkbox",{name:consent}).check();await bulk.getByLabel("Required audit reason",{exact:true}).fill("Different transfer reason");
 await expect(panel.getByRole("button",{name:"Confirm assignment",exact:true})).toHaveCount(0);expect(commits(f)).toHaveLength(0);
});
for(const theme of ["light","dark"])test(`Persian 320px ownership preview fits in ${theme}`,async({page},testInfo)=>{
 await page.setViewportSize({width:320,height:780});await page.addInitScript(theme=>{localStorage.setItem("dark-pro-lang","fa");localStorage.setItem("dark-pro-theme",theme);},theme);
 const f=await fixture(page);const {panel}=await prepare(page,true);
 await expect(panel.getByText("۹٬۰۰۷٬۱۹۹٬۲۵۴٬۷۴۰٬۹۹۵",{exact:true})).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 await panel.screenshot({path:testInfo.outputPath("ownership-320-"+theme+".png")});expect(commits(f)).toHaveLength(0);
});
test("wire validators bind actor exact client set and decimal counters",()=>{
 const quote={intent,quote_hash:"b".repeat(64),quote_unix:now(),count:1,clients:[item()],refund_policy:"original_funder_only_mixed_funding_requires_review",balance_changed:false,usage_reset:false};
 expect(readOwnershipQuote(quote,intent)).toBe(quote);
 expect(()=>readOwnershipQuote({...quote,clients:[{...item(),uplink_bytes:9007199254740993}]},intent)).toThrow();
 const request:OwnershipRequest={...intent,event_id:"ownership-validator-0001",quote_hash:quote.quote_hash,quote_unix:quote.quote_unix};
 const pending:PendingOwnership={version:1,actor_id:"owner",request,client_ids:["customer-a"]};
 const result={event_id:request.event_id,actor_user_id:"owner",representative_id:"rep-target",group_id:"",count:1,clients:[item()],created_unix:now(),replayed:false,balance_changed:false,usage_reset:false,credentials_changed:false};
 expect(readOwnershipReceipt(result,pending)).toBe(result);
 for(const changed of [{actor_user_id:"other-owner"},{count:2},{usage_reset:true},{credentials_changed:true},{clients:[item("foreign-client")]}])expect(()=>readOwnershipReceipt({...result,...changed},pending)).toThrow();
 expect(ownershipStorageKey("owner")).toBe("dark-pro-client-ownership-v1:owner");
});
