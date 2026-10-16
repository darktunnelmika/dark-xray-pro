import { expect, test, type Page } from "@playwright/test";

const user={id:"rep-a",username:"rep-a",role:"representative",representative_id:"rep-a"};
const client={id:"usage-client",representative_id:"rep-a",name:"Metered test",plan_kind:"volume",ip_count:0,volume_bytes:1000,duration_days:30,archived:false,deployment_state:"pending_node_apply",expires_unix:1900000000};
const known={client_id:client.id,representative_id:"rep-a",uplink_bytes:"600",downlink_bytes:"700",total_bytes:"1300",limit_bytes:"1000",plan_kind:"volume",known:true,reconciliation_holds:0,exhausted:true,eligible:false};
const envelope=(usage:Record<string,unknown>)=>({usage,scope:"committed_user_counters",live_collection_enabled:false,distributed_hard_limit_verified:false});
async function fixture(page:Page) {
 await page.route("**/api/v1/fleet",r=>r.fulfill({json:{nodes:[],total_nodes:0,online_nodes:0,offline_nodes:0,as_of:new Date().toISOString(),interface_rx_bits_per_second:0,interface_tx_bits_per_second:0,physical_rates_complete:false,metric_scope:"default_route_interface_not_billable"}}));
 await page.route("**/api/v1/session/status",r=>r.fulfill({json:{authenticated:true,user,csrf_token:"test-csrf",expires_unix:1900000000}}));
 await page.route("**/api/v1/workspace/**",r=>{
  const path=new URL(r.request().url()).pathname;
  if(path.endsWith("/me"))return r.fulfill({json:{user,balance:{id:"rep-a",volume_credit_bytes:1000,unlimited_credit_units:10},data_plane:"not_activated"}});
  if(path.endsWith("/clients"))return r.fulfill({json:{clients:[client]}});
  if(path.endsWith("/client-operations"))return r.fulfill({json:{operations:[]}});
  if(path.endsWith("/events"))return r.fulfill({json:{events:[]}});
  if(path.endsWith("/groups"))return r.fulfill({json:{groups:[]}});
  if(path.endsWith("/usage"))return r.fulfill({json:envelope(known)});
  return r.fulfill({status:404,json:{}});
 });
}
async function openUsage(page:Page,mobile=false) {
 await page.goto("/");if(mobile)await page.getByRole("button",{name:"Open menu"}).click();
 await page.getByRole("button",{name:"Business",exact:true}).click();
 await page.getByRole("button",{name:"Manage",exact:true}).click();
 return page.getByRole("region",{name:"Recorded client usage"});
}
test("recorded usage shows exact bytes, quota exhaustion and no live guarantee",async({page})=>{
 await fixture(page);const calls:string[]=[];page.on("request",r=>{if(r.url().endsWith("/usage"))calls.push(r.method());});
 const card=await openUsage(page);
 await expect(card.getByRole("status")).toHaveText("Volume exhausted");
 await expect(card.locator("dl")).toContainText("1,300 bytes");
 await expect(card.locator("dl > div").filter({hasText:"Remaining"})).toContainText("0 bytes");
 await expect(card.getByText("Automatic Agent admission",{exact:false})).toBeVisible();
 // React development effect probes may repeat an idempotent GET; the actual
 // invariant is that displaying recorded usage never performs a mutation.
 expect(calls.length).toBeGreaterThan(0);
 expect([...new Set(calls)]).toEqual(["GET"]);
});
test("unknown historical baseline is never displayed as verified available quota",async({page})=>{
 await fixture(page);await page.route("**/api/v1/workspace/clients/usage-client/usage",r=>r.fulfill({json:envelope({...known,known:false,reconciliation_holds:1,total_bytes:"0",uplink_bytes:"0",downlink_bytes:"0",exhausted:false})}));
 const card=await openUsage(page);
 await expect(card.getByRole("status")).toContainText("reconciliation");
 await expect(card.locator("dl > div").filter({hasText:"Remaining"}).locator("dd")).toHaveText("—");
 await expect(card.getByText("Eligible in the quota ledger only",{exact:true})).toHaveCount(0);
});
test("usage outage removes stale eligibility and never resets the balance to zero",async({page})=>{
 await fixture(page);const card=await openUsage(page);await expect(card.getByText("Volume exhausted",{exact:true})).toBeVisible();
 await page.route("**/api/v1/workspace/clients/usage-client/usage",r=>r.fulfill({status:503,json:{}}));
 await card.getByRole("button",{name:"Refresh usage"}).click();
 await expect(card.getByRole("status")).toContainText("could not be verified");await expect(card.locator("dl")).toHaveCount(0);
});
test("string-encoded counters above 2^53 remain exact and fit 320px Persian",async({page})=>{
 await fixture(page);await page.setViewportSize({width:320,height:780});
 await page.route("**/api/v1/workspace/clients/usage-client/usage",r=>r.fulfill({json:envelope({...known,uplink_bytes:"9007199254740993",downlink_bytes:"1",total_bytes:"9007199254740994",limit_bytes:"10000000000000000",exhausted:false,eligible:true})}));
 const card=await openUsage(page,true);await expect(card.locator("dl")).toContainText("9,007,199,254,740,993 bytes");
 await page.getByRole("button",{name:"Switch language"}).click();
 await expect(card.getByRole("heading",{name:"مصرف ثبت‌شدهٔ کلاینت"})).toBeVisible();
 await expect(card.getByRole("status")).toHaveText("مجاز فقط در دفتر سهمیه");
 expect(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2)).toBe(false);
});
test("usage response cannot substitute another customer's data or malformed integer totals",async({page})=>{
 await fixture(page);await page.route("**/api/v1/workspace/clients/usage-client/usage",r=>r.fulfill({json:envelope({...known,client_id:"other-customer"})}));
 const card=await openUsage(page);await expect(card.getByRole("status")).toContainText("could not be verified");await expect(card.locator("dl")).toHaveCount(0);
 await page.route("**/api/v1/workspace/clients/usage-client/usage",r=>r.fulfill({json:envelope({...known,total_bytes:"1.3e3"})}));
 await card.getByRole("button",{name:"Refresh usage"}).click();await expect(card.getByRole("status")).toContainText("could not be verified");
});
