import { expect, test, type Page } from "@playwright/test";
import { parseRestoreReview, createReviewMapping } from "../src/business/darkrestoreReview";

const sha="a".repeat(64);
const client=(id:string,group:string,status:"requires_manual_review"|"archive_history_only"|"skip_already_promoted",bytes:string)=>({
 source_id:id,source_group_id:group,label:"Legacy "+id,
 quota_kind:"volume",quota_bytes:"100",historical_bytes:bytes,expiry_unix:1893456000,
 status,review_reasons:["uuid_not_verified"], });
const plan={
 version:1,mode:"offline_operator_review_only",source_sha256:sha,source_size_bytes:9000,
 as_of_unix:1780000000,status:"ready_for_review",import_enabled:false,
 summary:{status:"ready_for_manual_review",subscriptions:3,groups:1,total_consumed_bytes:"120",findings:[]},
 groups:[{source_group_id:"group-a",name:"Old customers",count:3,archived:0,promoted:1,historical_bytes:"120"}],
 clients:[client("legacy-a","group-a","requires_manual_review","30"),
  client("legacy-b","group-a","requires_manual_review","50"),
  client("legacy-c","group-a","skip_already_promoted","40")],
};
async function setup(page:Page,role:"owner"|"representative"="owner"){
 const calls:string[]=[];const user={id:role==="owner"?"owner":"rep-one",username:"review-test",role,representative_id:role==="owner"?"owner":"rep-one"};
 const csrf="c".repeat(64);
 await page.route("**/api/v1/**",route=>{
  const req=route.request(),path=new URL(req.url()).pathname,q=new URL(req.url()).searchParams;
  calls.push(req.method()+" "+path);
  if(path==="/api/v1/session/status")return route.fulfill({json:{authenticated:true,user,csrf_token:csrf,expires_unix:1900000000}});
  if(path==="/api/v1/workspace/me")return route.fulfill({json:{user,balance:{id:user.representative_id,display_name:"Owner",kind:role,enabled:true,volume_credit_bytes:1000,unlimited_credit_units:100},data_plane:"not_activated"}});
  if(path==="/api/v1/workspace/representatives")return route.fulfill({json:{representatives:[{id:"rep-target",display_name:"Target representative",kind:"representative",enabled:true}],limit:50,offset:Number(q.get("offset")??0),q:q.get("q")??"",state:q.get("state")??"",sort:q.get("sort")??"",has_more:false}});
  if(path==="/api/v1/fleet")return route.fulfill({json:{as_of:new Date().toISOString(),total_nodes:0,online_nodes:0,offline_nodes:0,nodes:[]}});
  if(path==="/api/v1/workspace/clients")return route.fulfill({json:{clients:[],limit:50,offset:0,total:0,has_more:false,q:"",state:"all",plan:"all",sort:"newest",representative_id:""}});
  if(path==="/api/v1/workspace/client-operations")return route.fulfill({json:{operations:[]}});
  return route.fulfill({json:{[path.split("/").at(-1)!]:[]}});
 });
 return calls;
}

test("private review validates exact historical group totals and never enables import",()=>{
 const p=parseRestoreReview(plan);
 expect(p.clients).toHaveLength(3);
 const map=createReviewMapping(p,{"group-a":"rep-target"},["rep-target"]);
 expect(map).toMatchObject({import_enabled:false,source_sha256:sha,groups:[{source_group_id:"group-a",target_representative_id:"rep-target"}]});
 for(const changed of [
  {...plan,import_enabled:true}, {...plan,groups:[{...plan.groups[0],historical_bytes:"119"}]},
  {...plan,clients:[{...plan.clients[0],historical_bytes:"9007199254740993"},...plan.clients.slice(1)]},
  {...plan,clients:[{...plan.clients[0],status:"already_imported"},...plan.clients.slice(1)]},
  {...plan,summary:{...plan.summary,total_consumed_bytes:"121"}},
 ]){
  expect(()=>parseRestoreReview(changed)).toThrow();
 }
 expect(()=>createReviewMapping(p,{},["rep-target"])).toThrow();
});

test("legacy /dark-restore link opens Clients review; file stays browser-local and no import POST is possible",async({page})=>{
 const calls=await setup(page);
 await page.goto("/dark-restore");
 await expect(page).toHaveURL(url=>url.pathname==="/clients"&&url.searchParams.get("view")==="restore");
 const panel=page.getByRole("region",{name:"Dark Restore — import review"});
 await expect(panel.getByText("Review an independently verified offline legacy snapshot here", {exact:false})).toBeVisible();
 expect(await panel.locator("button.darkrestore-review-head").getAttribute("aria-expanded")).toBe("true");
 await expect(page.getByRole("navigation").getByRole("button",{name:"Dark Restore",includeHidden:true})).toHaveCount(0);
 await panel.locator('input[type="file"]').setInputFiles({name:"review.json",mimeType:"application/json",buffer:Buffer.from(JSON.stringify(plan))});
 await expect(panel.getByText("Review loaded — import remains disabled")).toBeVisible();
 await expect(panel.getByText("120 B",{exact:false})).toHaveCount(2);
 await expect(panel.locator(".darkrestore-rows article")).toHaveCount(3);
 await panel.getByRole("button",{name:/Target representative/}).click();
 await panel.getByRole("button",{name:"Map all groups to this account"}).click();
 await expect(panel.getByText("1 / 1")).toBeVisible();
 expect(calls.filter(v=>v.startsWith("POST "))).toEqual([]);
 expect(await page.evaluate(()=>JSON.stringify({...localStorage,...sessionStorage}))).not.toContain("legacy-a");
});

test("invalid plan is rejected without any mutation",async({page})=>{
 const calls=await setup(page);
 await page.goto("/clients");
 const panel=page.getByRole("region",{name:"Dark Restore — import review"});
 await panel.locator("button.darkrestore-review-head").click();
 await panel.locator('input[type="file"]').setInputFiles({name:"review.json",mimeType:"application/json",buffer:Buffer.from(JSON.stringify({...plan,summary:{...plan.summary,total_consumed_bytes:"0"}}))});
 await expect(panel.getByRole("alert")).toContainText("invalid");
 expect(calls.filter(v=>v.startsWith("POST "))).toEqual([]);
});

test("representative never receives owner-only private review control",async({page})=>{
 await setup(page,"representative");await page.goto("/clients");
 await expect(page.getByRole("region",{name:"Dark Restore — import review"})).toHaveCount(0);
});

for(const theme of ["light","dark"] as const){
 test(`320px Persian offline-review layout remains usable (${theme})`,async({page})=>{
  await page.setViewportSize({width:320,height:780});
  await page.addInitScript(mode=>{
   localStorage.setItem("dark-pro-lang","fa");
   localStorage.setItem("dark-pro-theme",mode);
  },theme);
  const calls=await setup(page);
  await page.goto("/dark-restore");
  const panel=page.getByRole("region",{name:"دارک ریستور — بررسی واردسازی"});
  await expect(panel).toBeVisible();
  await panel.locator('input[type="file"]').setInputFiles({name:"private-review.json",mimeType:"application/json",buffer:Buffer.from(JSON.stringify(plan))});
  await expect(panel.getByText("پیش‌نمایش دریافت شد؛ واردسازی همچنان غیرفعال است")).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+2)).toBe(true);
  expect(calls.some(x=>x.startsWith("POST "))).toBe(false);
 });
}
