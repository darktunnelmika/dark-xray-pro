import { expect, test, type Page } from "@playwright/test";

const csrf = "c".repeat(64);
const user={id:"owner",role:"owner",username:"proowner",representative_id:"owner"};
type Mutation = {path:string;method:string;csrf:string;body:Record<string,unknown>};
async function fixture(page:Page){
 const mutations:Mutation[]=[];
 const fingerprints=new Map<string,string>();
 const attempts=new Map<string,number>();
 await page.route("**/api/v1/session/status",r=>r.fulfill({
  json:{enabled:true,authenticated:true,user,csrf_token:csrf,expires_unix:1900000000},
 }));
 await page.route("**/api/v1/fleet",r=>r.fulfill({json:{
  as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,
  nodes:[],metric_scope:"default_route_interface_not_billable",
  interface_rx_bits_per_second:0,interface_tx_bits_per_second:0,physical_rates_complete:false,
 }}));
 await page.route("**/api/v1/workspace/**",async r=>{
  const req=r.request(),path=new URL(req.url()).pathname;
  if(req.method()!=="GET"){
   const body=req.postDataJSON() as Record<string,unknown>;
   mutations.push({path,method:req.method(),csrf:req.headers()["x-csrf-token"]??"",body});
   if(!["/api/v1/workspace/clients","/api/v1/workspace/representatives/rep-a/credits"].includes(path))
    return r.fulfill({status:403,json:{error:"unauthorized_action_in_test"}});
   const {event_id,...canonical}=body;
   if(typeof event_id!=="string"||!event_id.startsWith("pro-"))
    return r.fulfill({status:422,json:{error:"invalid_event_id"}});
   const fingerprint=JSON.stringify(canonical);
   const previous=fingerprints.get(event_id);
   if(previous!==undefined&&previous!==fingerprint)
    return r.fulfill({status:409,json:{error:"business_event_or_identity_conflict"}});
   fingerprints.set(event_id,fingerprint);
   const count=(attempts.get(path)??0)+1;
   attempts.set(path,count);
   // An ambiguous HTTP outage is modeled AFTER the server has recorded the
   // event. Exact retries must reuse the same ID to avoid a second debit.
   return r.fulfill({status:count<=2?503:path.endsWith("/clients")?201:200,
    json:count<=2?{error:"business_store_unavailable"}:{saved:true}});
  }
  if(path==="/api/v1/workspace/me")return r.fulfill({json:{
   user,balance:{id:"owner",display_name:"Owner",kind:"owner",enabled:true,
    volume_credit_bytes:0,unlimited_credit_units:0},data_plane:"not_activated",
  }});
  if(path==="/api/v1/workspace/representatives"){
   const search=new URL(req.url()).searchParams;
   const limit=Number(search.get("limit")??50),offset=Number(search.get("offset")??0);
   const q=search.get("q")??"",state=search.get("state")??"",sort=search.get("sort")??"";
   const records=[
    {id:"owner",display_name:"Owner",kind:"owner",enabled:true,
     volume_credit_bytes:0,unlimited_credit_units:0,client_count:0,archived_count:0},
    {id:"rep-a",display_name:"QA Rep A",kind:"representative",enabled:true,
     volume_credit_bytes:0,unlimited_credit_units:0,client_count:0,archived_count:0},
   ].filter(row=>!q||row.id.toLowerCase().includes(q.toLowerCase())||row.display_name.toLowerCase().includes(q.toLowerCase()));
   const current=records.slice(offset,offset+limit);
   return r.fulfill({json:{representatives:current,total:records.length,limit,offset,
    q,state,sort,has_more:offset+current.length<records.length}});
  }
  if(path==="/api/v1/workspace/clients")return r.fulfill({json:{clients:[]}});
  if(path==="/api/v1/workspace/client-operations")return r.fulfill({json:{operations:[]}});
  if(path==="/api/v1/workspace/events")return r.fulfill({json:{events:[]}});
  if(path==="/api/v1/workspace/groups")return r.fulfill({json:{groups:[]}});
  return r.fulfill({status:404,json:{error:"not_found"}});
 });
 return mutations;
}
test("ambiguous credit retry uses SAME event; editing amount creates NEW event without auto-debit",async({page})=>{
 const m=await fixture(page);
 await page.goto("/representatives");
 const credit=page.getByRole("heading",{name:"Adjust representative credit"}).locator("..");
 await page.getByRole("button",{name:"Select for credit"}).click();
 await expect(credit.getByLabel("Representative ID")).toHaveValue("rep-a");
 await credit.getByLabel("Volume delta (GB, +/-)").fill("1.0");
 await credit.getByLabel("Unlimited-unit delta (+/-)").fill("0");
 await credit.getByLabel("Reason").fill("Independent QA grant");
 const button=credit.getByRole("button",{name:"Record credit transaction"});
 await button.click();
 await expect(page.locator(".business-notice")).toContainText("Request failed");
 await credit.getByLabel("Volume delta (GB, +/-)").fill("1.00");
 await button.click();
 await expect(page.locator(".business-notice")).toContainText("Request failed");
 expect(m).toHaveLength(2);
 expect(m[0].body.event_id).toBe(m[1].body.event_id);
 expect(m[0].body.volume_delta_bytes).toBe(1073741824);
 expect(m[1].body.volume_delta_bytes).toBe(1073741824);
 await credit.getByLabel("Volume delta (GB, +/-)").fill("2");
 await button.click();
 await expect(page.locator(".business-notice")).toContainText("Saved in private PRO database");
 expect(m).toHaveLength(3);
 expect(m[2].body.event_id).not.toBe(m[0].body.event_id);
 expect(m[2].body.volume_delta_bytes).toBe(2147483648);
 await credit.getByLabel("Volume delta (GB, +/-)").fill("3");
 await credit.getByLabel("Reason").fill("Separate reviewed grant");
 await button.click();
 await expect(page.locator(".business-notice")).toContainText("Saved in private PRO database");
 expect(m).toHaveLength(4);
 expect(m[3].body.event_id).not.toBe(m[2].body.event_id);
 for(const req of m)expect(req.csrf).toBe(csrf);
 expect(m.every(req=>req.path==="/api/v1/workspace/representatives/rep-a/credits")).toBe(true);
});
test("ambiguous client creation keeps identical event but changes ID for edited name/plan",async({page})=>{
 const m=await fixture(page);
 await page.goto("/clients");
 const form=page.getByRole("heading",{name:"Create client record"}).locator("..");
 await form.getByLabel("Client ID").fill("qa-safe-client");
 await form.getByLabel("Client name").fill("Initial customer");
 await form.getByLabel("Plan type").selectOption("volume");
 await form.getByLabel("IP/User slots (0 = unlimited for volume)").fill("0");
 const button=form.getByRole("button",{name:"Create pending client"});
 await button.click();
 await expect(page.locator(".business-notice")).toContainText("Request failed");
 await button.click();
 await expect(page.locator(".business-notice")).toContainText("Request failed");
 expect(m).toHaveLength(2);
 expect(m[0].body.event_id).toBe(m[1].body.event_id);
 await form.getByLabel("Client name").fill("Reviewed customer");
 await button.click();
 await expect(page.locator(".business-notice")).toContainText("Saved in private PRO database");
 expect(m).toHaveLength(3);
 expect(m[2].body.event_id).not.toBe(m[0].body.event_id);
 expect(m[2].body.client_id).toBe("qa-safe-client");
 expect(m[2].body.name).toBe("Reviewed customer");
 await form.getByLabel("Client ID").fill("another-qa-client");
 await form.getByLabel("Client name").fill("Second reviewed");
 await button.click();
 await expect(page.locator(".business-notice")).toContainText("Saved in private PRO database");
 expect(m).toHaveLength(4);
 expect(m[3].body.event_id).not.toBe(m[2].body.event_id);
 for(const req of m)expect(req.csrf).toBe(csrf);
 expect(m.every(req=>req.path==="/api/v1/workspace/clients")).toBe(true);
});
