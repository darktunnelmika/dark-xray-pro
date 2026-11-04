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

test("owner sees staged import even when client operation history is unavailable",async({page})=>{
 await setup(page);
 await page.route("**/api/v1/workspace/me",route=>route.fulfill({json:{
   user:{id:"owner",role:"owner",representative_id:"owner",username:"review-test"},
   balance:{id:"owner",kind:"owner",display_name:"Owner",enabled:true,volume_credit_bytes:0,unlimited_credit_units:0},
   data_plane:"not_activated",capabilities:{darkrestore_import_v1:true}
 }}));
 await page.route("**/api/v1/workspace/client-operations**",route=>route.fulfill({status:503,json:{error:"unavailable"}}));
 await page.goto("/clients");
 const panel=page.getByRole("region",{name:"Dark Restore — import review"});
 await panel.locator("button.darkrestore-review-head").click();
 await expect(panel.getByRole("button",{name:"Open approved server snapshot"})).toBeVisible();
});

test("owner attaches a pinned signed handoff in Clients at 320px without a browser file",async({page})=>{
 await page.setViewportSize({width:320,height:780});
 await page.addInitScript(()=>{localStorage.setItem("dark-pro-lang","fa");localStorage.setItem("dark-pro-theme","dark");});
 const calls=await setup(page);
 const source={
  scope:"single_local_source_epoch",source_id:"legacy-a",source_sha256:sha,
  pre_epoch_bytes:"310",epoch_uplink_bytes:"1696",epoch_downlink_bytes:"1639327",
  settled_historical_bytes:"1641333",finite_quota_remaining_bytes:"132576395"
 };
 const evidence="b".repeat(64),publicKey="c".repeat(64);
 await page.route("**/api/v1/workspace/me",route=>route.fulfill({json:{
  user:{id:"owner",role:"owner",representative_id:"owner",username:"review-test"},
  balance:{id:"owner",kind:"owner",display_name:"Owner",enabled:true,volume_credit_bytes:1000,unlimited_credit_units:100},
  data_plane:"not_activated",capabilities:{darkrestore_receipts_v1:true,darkrestore_signed_handoff_v1:true}
 }}));
 await page.route("**/api/v1/workspace/dark-restore/handoff",route=>route.fulfill({json:{
  handoff_sha256:evidence,source_settlement:source
 }}));
 let attached=0;
 await page.route("**/api/v1/workspace/dark-restore/handoff/attach",route=>{
  expect(route.request().postDataJSON()).toEqual({handoff_sha256:evidence});
  expect(route.request().headers()["x-csrf-token"]).toBe("c".repeat(64));
  attached++;
  return route.fulfill({json:{
   client_id:"dr-"+sha.slice(0,20),original_import_event_id:"restore-original-0001",actor_user_id:"owner",
   source_settlement:source,handoff_sha256:evidence,handoff_public_key_sha256:publicKey,replayed:attached>1
  }});
 });
 await page.route("**/api/v1/workspace/dark-restore/handoff/readiness",route=>route.fulfill({json:{
  client_id:"dr-"+sha.slice(0,20),source_sha256:sha,source_id:"legacy-a",source_scope:"single_local_source_epoch",
  source_epoch_settled:true,global_source_custody_verified:false,native_baseline_complete:false,
  subscription_delivered:false,client_archived:false,client_expired:false,
  finite_quota_remaining_bytes:"132576395",activation_allowed:false,cutover_verified:false,
  native_apply_recorded:false,initial_apply_subscription_delivered:false,native_activation_authorized:false,
  activation_blockers:["global_source_custody_not_proven"],
  verification_blockers:["native_baseline_incomplete","subscription_not_delivered"]
 }}));
 await page.goto("/dark-restore");
 const panel=page.getByRole("region",{name:"دارک ریستور — بررسی واردسازی"});
 await panel.getByRole("button",{name:"بررسی تحویل تأییدشدهٔ سرور"}).click();
 await expect(panel.getByText("legacy-a")).toBeVisible();
 await expect(panel.getByText(evidence)).toBeVisible();
 expect(calls.some(x=>x.startsWith("POST "))).toBe(false);
 await panel.getByRole("button",{name:"ثبت یا بازیابی رسید اتصال"}).click();
 await expect(panel.getByText("اتصال ثبت شد",{exact:false})).toBeVisible();
 await panel.getByRole("button",{name:"بررسی آمادگی فعال‌سازی"}).click();
 const readiness=panel.getByLabel("آمادگی مهاجرت");
 await expect(readiness.getByText("فعال‌سازی مسدود است")).toBeVisible();
 await expect(readiness.getByText("global_source_custody_not_proven")).toBeVisible();
 await expect(readiness.getByText("native_baseline_incomplete")).toBeVisible();
 await expect(readiness.getByText("subscription_not_delivered")).toBeVisible();
 await page.route("**/api/v1/workspace/dark-restore/handoff/readiness",route=>route.fulfill({json:{
  client_id:"dr-"+sha.slice(0,20),source_sha256:sha,source_id:"legacy-a",source_scope:"single_local_source_epoch",
  source_epoch_settled:true,global_source_custody_verified:false,native_baseline_complete:true,
  subscription_delivered:true,subscription_delivery_scope:"darkrestore_prepared_not_applied_v1",client_archived:false,client_expired:false,
  finite_quota_remaining_bytes:"132576395",activation_allowed:false,cutover_verified:false,
  native_apply_recorded:false,initial_apply_subscription_delivered:false,native_activation_authorized:false,
  activation_blockers:["native_apply_not_recorded"],verification_blockers:["global_source_custody_not_proven"]
 }}));
 await panel.getByRole("button",{name:"بررسی آمادگی فعال‌سازی"}).click();
 await page.route("**/api/v1/workspace/dark-restore/handoff/prepare",route=>route.fulfill({json:{
  scope:"darkrestore_prepared_not_applied_v1",client_id:"dr-"+sha.slice(0,20),content:"synthetic-prepared-subscription",applied:false,path_verified:false
 }}));
 await panel.getByLabel("شناسه اینباند",{exact:true}).fill("802");
 await panel.getByLabel("نسخه طرح",{exact:true}).fill("1");
 await panel.getByLabel("نسخه اینباند",{exact:true}).fill("1");
 await panel.getByRole("button",{name:"دانلود اشتراک پیش از فعال‌سازی"}).click();
 const transfer=panel.getByRole("region",{name:"آماده‌سازی انتقال مشتری"});
 await expect(transfer).toHaveCount(1);
 const promotionBody={handoff_sha256:evidence,event_id:`drp-${evidence}-802-1-1`,promotion_event_id:`drn-${evidence}-802-1-1`,inbound_id:802,plan_revision:1,inbound_revision:1,reason:"Owner-reviewed prepared Dark Restore promotion"};
 let previews=0,commits=0,recoveries=0,foreign=true;
 await page.route("**/api/v1/workspace/dark-restore/handoff/promotion/preview",route=>{
  previews++;expect(route.request().postDataJSON()).toEqual(promotionBody);
  return route.fulfill({json:{scope:"darkrestore_prepared_native_promotion_v1",client_id:foreign?"foreign":"dr-"+sha.slice(0,20),inbound_id:802,authorizes_mutation:false,eligible:true,blockers:[],review_sha256:"d".repeat(64),transition_sha256:sha,pre_state_sha256:sha,post_state_sha256:sha,promotion:{inbound_id:802,plan_revision:1,inbound_revision:1,eligible:true,client_ids:["dr-"+sha.slice(0,20)],runtime_changed:false,subscription_published:false,review_sha256:"e".repeat(64),node_id:"synthetic-node"}}});
 });
 await page.route("**/api/v1/workspace/dark-restore/handoff/promotion",route=>{
  if(route.request().method()==="POST"){
   commits++;expect(route.request().postDataJSON()).toEqual({...promotionBody,confirmed:true,review_sha256:"d".repeat(64),promotion_review_sha256:"e".repeat(64)});
   return route.abort("failed");
  }
  recoveries++;return route.fulfill({json:{scope:"darkrestore_prepared_native_promotion_v1",...promotionBody,client_id:"dr-"+sha.slice(0,20),prepared_inbound_revision:1,native_promotion_recorded:true,native_apply_recorded:false,subscription_activated:false,authorizes_further_mutation:false,transition_sha256:sha,prepared_content_sha256:sha}});
 });
 await transfer.getByRole("button",{name:"پیش‌نمایش انتقال",exact:true}).click();
 await expect(transfer.getByRole("alert")).toBeVisible();
 await expect(transfer.getByRole("button",{name:"تأیید آماده‌سازی انتقال",exact:true})).toHaveCount(0);
 foreign=false;
 await transfer.getByRole("button",{name:"پیش‌نمایش انتقال",exact:true}).click();
 await expect(transfer.getByText("synthetic-node",{exact:true})).toBeVisible();
 expect(commits).toBe(0);
 page.once("dialog",d=>d.dismiss());
 await transfer.getByRole("button",{name:"تأیید آماده‌سازی انتقال",exact:true}).click();
 expect(commits).toBe(0);
 page.once("dialog",d=>d.accept());
 await transfer.getByRole("button",{name:"تأیید آماده‌سازی انتقال",exact:true}).click();
 await expect(transfer.getByRole("button",{name:"تکرار همان تأیید",exact:true})).toBeEnabled();
 await expect(transfer.getByRole("button",{name:"پیش‌نمایش انتقال",exact:true})).toBeDisabled();
 await transfer.getByRole("button",{name:"بازیابی رسید آماده‌سازی",exact:true}).click();
 await expect(transfer.getByRole("status")).toContainText("رسید آماده‌سازی ثبت شد");
 expect({previews,commits,recoveries}).toEqual({previews:2,commits:1,recoveries:1});
 // Recover after reload without re-delivering a now-promoted subscription.
 await page.reload();
 await panel.getByRole("button",{name:"بررسی تحویل تأییدشدهٔ سرور"}).click();
 await panel.getByRole("button",{name:"ثبت یا بازیابی رسید اتصال"}).click();
 await panel.getByRole("button",{name:"بررسی آمادگی فعال‌سازی"}).click();
 await panel.getByLabel("شناسه اینباند",{exact:true}).fill("802");
 await panel.getByLabel("نسخه طرح",{exact:true}).fill("1");
 await panel.getByLabel("نسخه اینباند",{exact:true}).fill("1");
 await panel.getByRole("button",{name:"بررسی انتقال این طرح",exact:true}).click();
 await expect(transfer.getByRole("button",{name:"بازیابی رسید آماده‌سازی",exact:true})).toBeVisible();
 await transfer.getByRole("button",{name:"بازیابی رسید آماده‌سازی",exact:true}).click();
 await expect(transfer.getByRole("status")).toContainText("رسید آماده‌سازی ثبت شد");
 expect(recoveries).toBe(2);
 const execution=transfer.getByRole("region",{name:"اجرای طرح انتقال‌یافته"});
 let foreignExecution=true;
 const nativeReceipt={event_id:promotionBody.promotion_event_id,inbound_id:802,plan_revision:1,source_inbound_revision:1,inbound_revision:2,node_id:"synthetic-node",installation_id:"a".repeat(32),draft_revision:1,client_ids:["dr-"+sha.slice(0,20)],review_sha256:"e".repeat(64),promoted:true,runtime_changed:false,subscription_published:false,created_unix:1,replayed:false};
 await page.route("**/api/v1/workspace/native-inbound-plans/802/promotion",route=>route.fulfill({json:{available:true,scope:"single_direct_tcp_reality_loopback",promoted:true,blockers:[],receipt:{...nativeReceipt,client_ids:foreignExecution?["foreign"]:nativeReceipt.client_ids}}}));
 await execution.getByRole("button",{name:"بررسی و بازکردن کنترل‌های اجرا",exact:true}).click();
 await expect(execution.getByRole("alert")).toBeVisible();
 await expect(execution.getByRole("region",{name:"سلامت اجرای مشتری واردشده"})).toHaveCount(0);
 foreignExecution=false;
 await page.route("**/api/v1/workspace/metered-route-transactions",route=>route.fulfill({json:{enabled:false,scope:"metered_tcp_loopback_lab"}}));
 let preparation:Record<string,unknown>|undefined,healthRound:Record<string,unknown>|undefined,applied=false,prepares=0,rounds=0;
 const binding={initial_apply_sha:"1".repeat(64),apply_command_id:"2".repeat(32),stage_id:"3".repeat(32),epoch_id:"4".repeat(32)};
 const identity={inbound_id:802,promotion_event_id:nativeReceipt.event_id,node_id:nativeReceipt.node_id,installation_id:nativeReceipt.installation_id};
 await page.route("**/api/v1/workspace/native-inbound-plans/802/initial-health",route=>route.fulfill({json:{
  enabled:true,available:true,scope:"lab_initial_apply",...identity,direct_port:21186,inbound_revision:2,draft_revision:1,prepared:!!preparation,can_prepare:!preparation,eligible:applied,blockers:[],preparation,client_ids:nativeReceipt.client_ids,server_time_unix_milli:Date.now(),...(applied?binding:{}),latest_round:healthRound
 }}));
 await page.route("**/api/v1/workspace/native-inbound-plans/802/initial-health/prepare",route=>{
  const b=route.request().postDataJSON();expect(b.confirmed).toBe(true);expect(b.promotion_event_id).toBe(nativeReceipt.event_id);
  prepares++;preparation={...identity,event_id:b.event_id,prepared:true,created_unix_nano:Date.now()*1e6,replayed:prepares>1,server_time_unix_milli:Date.now()};
  return route.abort("failed");
 });
 await page.route("**/api/v1/workspace/native-inbound-plans/802/initial-health/rounds",route=>{
  const b=route.request().postDataJSON();expect(b.initial_apply_sha).toBe(binding.initial_apply_sha);expect(b.confirmed).toBe(true);rounds++;
  const created=Date.now()*1e6;
  healthRound={...identity,...binding,id:b.id,scope:"lab_initial_apply",endpoint_sha:"5".repeat(64),manifest_sha:"6".repeat(64),created_unix_nano:created,expires_unix_nano:created+7e9,members:[{inbound_id:802,intent_id:"7".repeat(32),port:21186,state:"accepted",challenge_id:"8".repeat(32),accepted_unix_nano:created+1e6}],state:"complete",current:true,replayed:false,server_time_unix_milli:Date.now()};
  return route.fulfill({json:healthRound});
 });
 const subscriptionWrites:string[]=[];
 page.on("request",r=>{if(r.method()==="POST"&&/subscription/.test(r.url()))subscriptionWrites.push(r.url());});
 await execution.getByRole("button",{name:"بررسی و بازکردن کنترل‌های اجرا",exact:true}).click();
 const health=execution.getByRole("region",{name:"سلامت اجرای مشتری واردشده"});
 await expect(health.getByRole("button",{name:"آماده‌سازی آزمون بدون کسر حجم",exact:true})).toBeEnabled();
 await expect(health.getByRole("button",{name:"آزمون اتصال اجرای فعلی",exact:true})).toBeDisabled();
 page.once("dialog",d=>d.accept());
 await health.getByRole("button",{name:"آماده‌سازی آزمون بدون کسر حجم",exact:true}).click();
 await health.getByRole("button",{name:"به‌روزرسانی سلامت اجرا",exact:true}).click();
 await expect(health.getByRole("button",{name:"تکرار همان درخواست سلامت",exact:true})).toHaveCount(0);
 expect(prepares).toBe(1);
 applied=true;
 await health.getByRole("button",{name:"به‌روزرسانی سلامت اجرا",exact:true}).click();
 await expect(health.getByRole("button",{name:"آزمون اتصال اجرای فعلی",exact:true})).toBeEnabled();
 page.once("dialog",d=>d.accept());
 await health.getByRole("button",{name:"آزمون اتصال اجرای فعلی",exact:true}).click();
 await expect(health.getByRole("status")).toContainText("اتصال همین اجرا در مهلت اثبات تأیید شد");
 expect(rounds).toBe(1);expect(subscriptionWrites).toEqual([]);

 // An expired valid receipt is history, never current health or an auto-renewal.
 const past=Date.now()*1e6-9e9;
 healthRound={...healthRound,created_unix_nano:past,expires_unix_nano:past+7e9,current:false,state:"expired",members:[{inbound_id:802,intent_id:"7".repeat(32),port:21186,state:"accepted",challenge_id:"8".repeat(32),accepted_unix_nano:past+1e6}]};
 await health.getByRole("button",{name:"به‌روزرسانی سلامت اجرا",exact:true}).click();
 await expect(health.getByRole("status")).toHaveText("سلامت فعلی تأیید نشده است.");
 expect(rounds).toBe(1);
 // A mismatched latest proof clears the prior success, even if marked current.
 healthRound={...healthRound,client_id:"foreign"};
 await health.getByRole("button",{name:"به‌روزرسانی سلامت اجرا",exact:true}).click();
 await expect(health.getByRole("alert")).toBeVisible();
 await expect(health.getByText("اتصال همین اجرا در مهلت اثبات تأیید شد",{exact:false})).toHaveCount(0);


 let executionState="pending";
 let probe:Record<string,unknown>|undefined;
 const probeFor=(state:string)=>({
  scope:"darkrestore_system_probe_round_v1",client_id:"dr-"+sha.slice(0,20),plan_sha256:"c".repeat(64),
  challenge_id:state==="not_requested"?undefined:"d".repeat(32),state,
  serving_health_recorded:state==="recorded",authorizes_mutation:false
 });
 await page.route("**/api/v1/workspace/dark-restore/handoff/quarantine/admission/evidence",route=>{
  expect(route.request().method()).toBe("GET");
  return route.fulfill({json:{
   scope:"darkrestore_native_admission_evidence_v1",plan_sha256:"c".repeat(64),
   client_id:executionState==="foreign"?"another-client":"dr-"+sha.slice(0,20),
   initial_zero_receipt_recorded:true,listener_apply_recorded:executionState!=="pending",
   listener_apply_receipt_sha256:executionState==="pending"?undefined:"e".repeat(64),
   native_apply_recorded:false,serving_health_recorded:false,authorizes_mutation:false,system_probe:probe
  }});
 });
 await panel.getByRole("button",{name:"بررسی آمادگی فعال‌سازی"}).click();
 await panel.getByRole("button",{name:"بررسی وضعیت اجرای سرویس"}).click();
 await expect(panel.getByLabel("وضعیت اجرای سرویس").getByText("در انتظار رسید Agent",{exact:true})).toBeVisible();
 executionState="applied";
 await panel.getByRole("button",{name:"بررسی وضعیت اجرای سرویس"}).click();
 await expect(panel.getByLabel("وضعیت اجرای سرویس").getByText("رسید Agent ثبت شده",{exact:true})).toBeVisible();
 await expect(panel.getByText("فعال‌سازی مسدود است",{exact:true})).toBeVisible();
 await expect(readiness.locator("p").filter({hasText:"اشتراک پیش از فعال‌سازی"}).getByText("رسید تأیید شد",{exact:true})).toBeVisible();
 let settlementClient="dr-"+sha.slice(0,20);
 await page.route("**/api/v1/workspace/dark-restore/handoff/quarantine/settlement",route=>{
  expect(route.request().method()).toBe("GET");
  return route.fulfill({json:{scope:"darkrestore_native_closed_cycle_evidence_v1",client_id:settlementClient,
   plan_sha256:"c".repeat(64),final_report_sha256:"e".repeat(64),sealed_cycle_recorded:true,
   customer_uplink_bytes:"1859",customer_downlink_bytes:"17924",authorizes_mutation:false}});
 });
 const settled=panel.getByRole("button",{name:"بررسی تسویهٔ مصرف مشتری"});
 await settled.click();
 await expect(panel.getByText(/مصرف مشتری در دورهٔ بسته‌شده/)).toContainText(BigInt(17924).toLocaleString("fa-IR"));
 settlementClient="another-client";
 await settled.click();
 await expect(panel.getByText("رسید نهایی مصرف این مشتری قابل تأیید نیست.")).toBeVisible();
 await expect(panel.getByText(/مصرف مشتری در دورهٔ بسته‌شده/)).toHaveCount(0);
 let connectionState="not_requested", connectionPosts=0;
 await page.route("**/api/v1/workspace/dark-restore/handoff/quarantine/connection-check",route=>{
  if(route.request().method()==="POST"){
   connectionPosts++;
   expect(route.request().postDataJSON()).toEqual({event_id:"connection-"+"c".repeat(64),plan_sha256:"c".repeat(64),acknowledge_usage:true});
   connectionState="pending";
  }
  return route.fulfill({json:{enabled:true,scope:"darkrestore_customer_connection_v1",client_id:connectionState==="foreign"?"foreign-client":"dr-"+sha.slice(0,20),
   plan_sha256:connectionState==="not_requested"?undefined:"c".repeat(64),id:connectionState==="not_requested"?undefined:"d".repeat(32),
   state:connectionState,customer_connection_verified:connectionState==="verified",authorizes_mutation:false,
   receipt_sha256:connectionState==="verified"?"e".repeat(64):undefined,completed_unix_nano:connectionState==="verified"?1700000000000000000:undefined}});
 });
 const refresh=panel.getByRole("button",{name:"بررسی وضعیت اجرای سرویس"});
 for(const [state,message] of [
  ["not_requested","درخواستی ثبت نشده است."],
  ["pending","درخواست ثبت شده و در انتظار نود است."],
  ["issued","درخواست برای نود صادر شده و هنوز رسیدی ثبت نشده است."],
  ["expired","مهلت آزمون گذشته و هنوز رسیدی ثبت نشده است."],
  ["recorded","رسید آزمون خودکار این نود ثبت شد."]
 ]){
  probe=probeFor(state);
  await refresh.click();
  await expect(panel.getByText(message,{exact:true})).toBeVisible();
  await expect(panel.getByText("فعال‌سازی مسدود است",{exact:true})).toBeVisible();
 }
 for(const mismatch of [{client_id:"another-client"},{plan_sha256:"e".repeat(64)},
   {serving_health_recorded:false},{authorizes_mutation:true},{challenge_id:""}]){
  probe={...probeFor("recorded"),...mismatch};
  await refresh.click();
  await expect(panel.getByLabel("وضعیت اجرای سرویس")).toHaveCount(0);
  await expect(panel.getByText("رسید کامل و مطابق این کاربر در دسترس نیست؛ دوباره بررسی کن.")).toBeVisible();
 }
 expect(connectionPosts).toBe(0);
 probe=probeFor("recorded");
 await refresh.click();
 const connection=panel.getByLabel("آزمون اتصال کاربر",{exact:true});
 await connection.getByRole("button",{name:"بررسی نتیجهٔ آزمون اتصال"}).click();
 const run=connection.getByRole("button",{name:"اجرای یک‌بارهٔ آزمون اتصال"});
 await expect(run).toBeDisabled();
 expect(connectionPosts).toBe(0);
 await connection.getByRole("checkbox").check();
 await run.click();
 await expect(connection.getByText("در انتظار Agent؛ درخواست ثبت شده است.")).toBeVisible();
 await expect(run).toHaveCount(0);
 expect(connectionPosts).toBe(1);
 connectionState="verified";
 await connection.getByRole("button",{name:"بررسی نتیجهٔ آزمون اتصال"}).click();
 await expect(connection.getByText("رسید اتصال این کاربر تأیید و ثبت شد.")).toBeVisible();
 await expect(panel.getByText("فعال‌سازی مسدود است",{exact:true})).toBeVisible();
 connectionState="foreign";
 await connection.getByRole("button",{name:"بررسی نتیجهٔ آزمون اتصال"}).click();
 await expect(connection.getByRole("alert")).toBeVisible();
 await expect(connection.getByText("رسید اتصال این کاربر تأیید و ثبت شد.")).toHaveCount(0);
 expect(connectionPosts).toBe(1);
 executionState="foreign";
 await panel.getByRole("button",{name:"بررسی وضعیت اجرای سرویس"}).click();
 await expect(panel.getByLabel("وضعیت اجرای سرویس")).toHaveCount(0);
 await expect(panel.getByText("رسید کامل و مطابق این کاربر در دسترس نیست؛ دوباره بررسی کن.")).toBeVisible();
 await panel.getByRole("button",{name:"ثبت یا بازیابی رسید اتصال"}).click();
 await expect(panel.getByText("رسید پیشین بازیابی شد",{exact:false})).toBeVisible();
 expect(attached).toBe(3);
 const progressBase={client_id:"dr-"+sha.slice(0,20),source_sha256:sha,source_id:"legacy-a",
  source_scope:"pinned_inventory_plus_independent_coverage_and_retirement_receipts",
  source_epoch_settled:true,global_source_custody_verified:false,native_baseline_complete:true,
  subscription_delivered:true,subscription_delivery_scope:"darkrestore_prepared_not_applied_v1",
  client_archived:false,client_expired:false,finite_quota_remaining_bytes:"132576395",
  native_apply_recorded:true,initial_apply_subscription_delivered:true,native_activation_authorized:false,
  activation_allowed:false,cutover_verified:false,activation_blockers:["native_activation_not_authorized"],verification_blockers:[]};
 let progress:Record<string,unknown>={...progressBase};
 await page.route("**/api/v1/workspace/dark-restore/handoff/readiness",route=>route.fulfill({json:progress}));
 const check=panel.getByRole("button",{name:"بررسی آمادگی فعال‌سازی"});
 await check.click();
 const stages=panel.getByLabel("مراحل تحویل اشتراک");
 await expect(stages.getByText("تأیید شد",{exact:true})).toHaveCount(2);
 await expect(stages.getByText("در انتظار تأیید",{exact:true})).toHaveCount(1);
 progress={...progressBase,native_activation_authorized:true,activation_allowed:true,cutover_verified:true,activation_blockers:[]};
 await check.click();
 await expect(stages.getByText("تأیید شد",{exact:true})).toHaveCount(3);
 await expect(readiness.locator("p").filter({hasText:"مهاجرت تأیید شد"})).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+2)).toBe(true);
 for(const invalid of [
  {...progress,native_apply_recorded:false},
  {...progress,initial_apply_subscription_delivered:false},
  {...progress,native_activation_authorized:false},
  {...progress,native_apply_recorded:undefined},
  {...progress,subscription_delivery_scope:"unrecognized_delivery_scope"},
  {...progress,subscription_delivery_scope:"lab_single_handover"},
  {...progress,subscription_delivered:false},
  {...progress,source_scope:"unrecognized_source"},
  {...progress,client_id:"foreign-client"},
 ]) {
  progress=invalid;
  await check.click();
  await expect(panel.getByLabel("آمادگی مهاجرت")).toHaveCount(0);
  await expect(panel.getByRole("alert")).toBeVisible();
 }

 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+2)).toBe(true);
});
