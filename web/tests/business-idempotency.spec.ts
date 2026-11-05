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
   if(typeof event_id!=="string"||!/^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$/.test(event_id))
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
    json:count<=2?{error:"business_store_unavailable"}:path.endsWith("/clients")?{id:body.client_id}:{saved:true}});
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
 await page.getByRole("region",{name:"Representative directory"})
  .getByRole("button",{name:"Manage account"}).click();
 await page.getByRole("button",{name:"Credit",exact:true}).click();
 const credit=page.locator(".rep-v2-detail-form");
 await expect(credit.getByRole("heading",{name:"Credit",exact:true})).toBeVisible();
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
test("ambiguous client creation locks the payload until identical replay succeeds",async({page})=>{
 const m=await fixture(page);
 await page.goto("/clients");
 await page.getByRole("button",{name:"Create client",exact:true}).click();
 const form=page.getByRole("heading",{name:"Create client",exact:true}).locator("..");
 await form.getByLabel("Creation mode",{exact:true}).selectOption("account");
 await form.getByText("Advanced identity",{exact:true}).click();
 await form.getByLabel("Client ID (optional)",{exact:true}).fill("qa-safe-client");
 await form.getByLabel("Client name",{exact:true}).fill("Initial customer");
 await form.getByRole("button",{name:"Create client",exact:true}).click();
 await expect(form.getByRole("button",{name:"Resume same request"})).toBeEnabled();
 await form.getByRole("button",{name:"Resume same request"}).click();
 await expect(form.getByRole("button",{name:"Resume same request"})).toBeEnabled();
 expect(m).toHaveLength(2);expect(m[1].body).toEqual(m[0].body);
 await expect(form.getByLabel("Client name",{exact:true})).toHaveCount(0);
 await form.getByRole("button",{name:"Resume same request"}).click();
 await expect(form.getByText("Account saved; awaiting owner activation.")).toBeVisible();
 expect(m).toHaveLength(3);expect(m[2].body).toEqual(m[0].body);
 await form.getByRole("button",{name:"Create another client"}).click();
 await form.getByLabel("Client name",{exact:true}).fill("Second customer");
 await form.getByRole("button",{name:"Create client",exact:true}).click();
 await expect(form.getByText("Account saved; awaiting owner activation.")).toBeVisible();
 expect(m).toHaveLength(4);expect(m[3].body.event_id).not.toBe(m[2].body.event_id);
 for(const req of m)expect(req.csrf).toBe(csrf);
});
