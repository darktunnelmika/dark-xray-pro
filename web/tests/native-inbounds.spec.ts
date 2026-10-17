import { expect, test, type Page } from "@playwright/test";

// Disposable browser fixtures only: no real server, credential or customer.
const csrf = "a".repeat(64);
const installation = "a".repeat(32);
const digest = "d".repeat(64);
async function fixture(page: Page, role = "owner") {
 const calls: Array<{ path: string; body: Record<string, unknown>; csrf: string }> = [];
 const base = { enabled: true, archived: false, revision: 1, grpc_service: "", reality_target: "example.org:443", server_names: ["example.org"], short_ids: ["aa"], public_key: "PUBLIC_ONLY_TEST_KEY" };
 const inbounds = [{ ...base, id: 301, name: "Native Test", transport: "tcp", bound_clients: 0 }];
 const operations: Array<Record<string,unknown>> = [];
 const bindings: string[] = [];
 let applied = false;
 await page.route("**/api/v1/fleet", r => r.fulfill({ json: { nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0, as_of: new Date().toISOString(), interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false, metric_scope: "default_route_interface_not_billable" } }));
 await page.route("**/api/v1/session/status", r => r.fulfill({ json: { authenticated: true, user: { id: role, role, username: "test-owner", representative_id: role === "owner" ? "owner" : "rep-a" }, csrf_token: csrf, expires_unix: 1900000000 } }));
 await page.route("**/api/v1/workspace/**", async route => {
  const req = route.request();const path = new URL(req.url()).pathname;
  if (req.method() === "POST") {
   const body = req.postDataJSON() as Record<string, unknown>;
   calls.push({ path, body, csrf: req.headers()["x-csrf-token"] ?? "" });
   if (path.endsWith("native-inbounds")) inbounds.push({ ...base, id: Number(body.id), name: String(body.name), transport: String(body.transport), bound_clients: 0 });
   if (path.endsWith("/clients")) {
    for (const id of body.client_ids as string[]) { const i = bindings.indexOf(id);if (body.attach && i < 0) bindings.push(id);else if (!body.attach && i >= 0) bindings.splice(i, 1); }
    inbounds[0].bound_clients = bindings.length;
   }
   applied = false;
   if(path.endsWith("/lifecycle")) {
    const id=Number(path.split("/").at(-2));const inb=inbounds.find(i=>i.id===id);
    if(!inb||inb.revision!==body.expected_revision||inb.archived)return route.fulfill({status:409,json:{error:"conflict"}});
    const from=inb.revision;
    if(body.action==="edit")Object.assign(inb,body.profile);
    if(body.action==="disable"||body.action==="archive")inb.enabled=false;
    if(body.action==="enable")inb.enabled=true;
    if(body.action==="archive")inb.archived=true;
    inb.revision++;
    operations.push({event_id:body.event_id,action:body.action,reason:body.reason,from_revision:from,to_revision:inb.revision,created_unix:Math.floor(Date.now()/1000)});
    return route.fulfill({json:{inbound:inb,clients_deleted:false,ledger_modified:false,awaiting_agent_report:true}});
   }
   return route.fulfill({ json: { saved: true } });
  }
  if (path.endsWith("native-inbounds-state")) return route.fulfill({ json: {
   preflight: {
    scope: "new_pro_lab_only", paired_nodes: 1, online_nodes: 1, fresh_nodes: 1,
    configured_inbounds: inbounds.filter(i => i.enabled && !i.archived).length,
    configured_targets: 0, client_bindings: bindings.length,
    usage_ledger_enabled: true,
    remote_metered_admission: "trusted_remote_epoch_registration_pending",
    customer_data_plane_verified: false, subscription_published: false,
    deployment_ready: false, blocking_reason: "remote_metered_admission_not_wired",
   },
   inbounds, nodes: [{ node_id: "new-node", installation_id: installation, name: "New PRO Node" }], targets: [], runtime: [{
    node_id: "new-node", installation_id: installation, desired_sequence: 1, desired_sha: digest,
    reported_sequence: applied ? 1 : 0, reported_sha: applied ? digest : "", state: applied ? "applied" : "awaiting_report",
    phase: applied ? "ready" : "", process_ready: applied, reported_at: applied ? new Date().toISOString() : "",
    lease_expires_at: applied ? new Date(Date.now() + 30000).toISOString() : "",
   }],
  } });
  if (path === "/api/v1/workspace/clients") return route.fulfill({ json: { clients: [{ id: "client-one", name: "Client One", representative_id: "owner", archived: false, deployment_state: "pending_node_apply" }] } });
  if (path.endsWith("/clients")) return route.fulfill({ json: { client_ids: bindings } });
  if(path.endsWith("/operations"))return route.fulfill({json:{operations}});
  return route.fulfill({ status: 404, json: {} });
 });
 return { calls, reportReady: () => { applied = true; }, externalEdit: () => { inbounds[0].revision++; } };
}
async function openPage(page: Page, mobile = false) {
 await page.goto("/");if (mobile) await page.getByRole("button", { name: "Open menu" }).click();
 await page.getByRole("button", { name: "Inbounds", exact: true }).click();
}

test("native owner form saves an Inbound without claiming completed deployment", async ({ page }) => {
 const f = await fixture(page);await openPage(page);
 await page.getByLabel("Inbound name", { exact: true }).fill("New actual inbound");
 await page.getByLabel("REALITY target (host:port)").fill("example.net:443");
 await page.getByLabel("Server names (comma-separated)").fill("example.net");
 await page.getByRole("button", { name: "Create Inbound", exact: true }).click();
 await expect(page.getByText("New actual inbound", { exact: true })).toBeVisible();
 expect(f.calls).toHaveLength(1);expect(f.calls[0].csrf).toBe(csrf);expect(f.calls[0].body.transport).toBe("tcp");
 await expect(page.getByText("Matching process reported", { exact: true })).toHaveCount(0);
});

test("native workflow binds selected clients and reports only acknowledged Node revision", async ({ page }) => {
 const f = await fixture(page);await openPage(page);
 await page.getByRole("button", { name: "Native Test", exact: false }).click();
 await page.locator(".native-client-row").filter({ hasText: "Client One" }).getByRole("checkbox").check();
 await page.getByRole("button", { name: "Attach selected", exact: true }).click();
 await expect(page.getByLabel("Bound", { exact: true })).toBeVisible();
 await page.getByLabel("New paired Node").selectOption("new-node");
 await page.getByLabel("Node's assigned IP").fill("203.0.113.7");
 await page.getByLabel("Direct port (0 = disabled)").fill("19600");
 await page.getByLabel("Dark Swap destination port (0 = disabled)").fill("19601");
 page.once("dialog", d => d.accept());await page.getByRole("button", { name: "Save Node target" }).click();
 await expect(page.getByText("Awaiting Agent report", { exact: true })).toBeVisible();
 expect(f.calls).toHaveLength(2);expect(f.calls[0].body).toEqual({ client_ids: ["client-one"], attach: true });
 expect(f.calls[1].body.installation_id).toBe(installation);expect(f.calls[1].body.direct_port).toBe(19600);
 for (const call of f.calls) expect(call.csrf).toBe(csrf);
 f.reportReady();await page.getByRole("button", { name: "Refresh", exact: true }).click();
 await expect(page.getByText("Matching process reported", { exact: true })).toBeVisible();
});

test("representative view does not display owner deployment controls", async ({ page }) => {
 await fixture(page, "representative");let reads = 0;page.on("request", r => { if (r.url().includes("/workspace/native-")) reads++; });
 await page.goto("/inbounds");await expect(page).toHaveURL(/\/clients$/);
 await expect(page.getByRole("button", { name: "Inbounds", exact: true })).toHaveCount(0);
 await expect(page.getByRole("button", { name: "Create Inbound", exact: true })).toHaveCount(0);expect(reads).toBe(0);
});

test("native Persian mobile view retains selection on refresh without overflow", async ({ page }) => {
 await fixture(page);await page.setViewportSize({ width: 320, height: 780 });await openPage(page, true);
 await page.getByRole("button", { name: "Native Test", exact: false }).click();
 await page.getByRole("button", { name: "Switch language" }).click();
 await expect(page.getByRole("heading", { name: "اینباندهای واقعی PRO" })).toBeVisible();
 await page.getByRole("button", { name: "به‌روزرسانی", exact: true }).click();
 await expect(page.getByRole("button", { name: "Native Test", exact: false })).toHaveAttribute("aria-pressed", "true");
 expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2)).toBe(false);
});

test("failed polling demotes a previous ready report and disables mutation buttons", async ({ page }) => {
 const f = await fixture(page);f.reportReady();await openPage(page);
 await expect(page.getByText("Matching process reported", { exact: true })).toBeVisible();
 await page.route("**/api/v1/workspace/native-inbounds-state", r => r.fulfill({ status: 503, json: {} }));
 await page.getByRole("button", { name: "Refresh", exact: true }).click();
 await expect(page.getByRole("alert")).toBeVisible();await expect(page.getByText("Report stale / lease expired", { exact: true })).toBeVisible();
 await expect(page.getByRole("button", { name: "Create Inbound", exact: true })).toBeDisabled();
});

test("native profile edit preserves key fields and disable/archive uses revisioned owner operations", async ({ page }) => {
 const f=await fixture(page);await page.setViewportSize({width:320,height:780});await openPage(page,true);
 await page.getByRole("button",{name:"Native Test",exact:false}).click();
 const control=page.getByRole("region",{name:"Inbound lifecycle"});
 await control.getByRole("button",{name:"Edit profile",exact:true}).click();
 await control.getByLabel("Reason for change",{exact:true}).fill("Edit without key rotation");
 const editor=page.getByRole("form",{name:"Edit Inbound profile"});
 await editor.getByLabel("Name",{exact:true}).fill("Edited Native");
 await editor.getByLabel("Server names",{exact:true}).fill("updated.example.org");
 page.once("dialog",d=>d.accept());await editor.getByRole("button",{name:"Save profile changes",exact:true}).click();
 await expect(page.getByRole("button",{name:"Edited Native",exact:false})).toBeVisible();
 await expect(control.getByText("Change recorded; a fresh Agent report is still required.")).toBeVisible();
 expect(f.calls).toHaveLength(1);expect(f.calls[0].body.expected_revision).toBe(1);
 const p=f.calls[0].body.profile as Record<string,unknown>;
 expect(p.short_ids).toEqual(["aa"]);expect(p.id).toBe(301);expect(p).not.toHaveProperty("private_key");expect(p).not.toHaveProperty("public_key");
 await control.getByLabel("Reason for change",{exact:true}).fill("Pause temporarily");
 page.once("dialog",d=>d.accept());await control.getByRole("button",{name:"Disable Inbound",exact:true}).click();
 await expect(control.getByRole("button",{name:"Enable Inbound",exact:true})).toBeVisible();
 expect(f.calls[1].body.action).toBe("disable");expect(f.calls[1].body.expected_revision).toBe(2);
 await control.getByLabel("Reason for change",{exact:true}).fill("Safe archive");
 page.once("dialog",d=>d.accept());await control.getByRole("button",{name:"Delete safely (archive)",exact:true}).click();
 await expect(control.getByRole("button",{name:"Enable Inbound",exact:true})).toHaveCount(0);
 await expect(control.getByText("Archived",{exact:true})).toBeVisible();
 expect(f.calls).toHaveLength(3);expect(f.calls[2].body.expected_revision).toBe(3);
 for(const c of f.calls){expect(c.csrf).toBe(csrf);expect(c.path).toBe("/api/v1/workspace/native-inbounds/301/lifecycle");expect(String(c.body.event_id).length).toBeGreaterThan(30);}
 await page.getByRole("button",{name:"Switch language"}).click();
 await expect(control.getByText("آرشیوشده",{exact:true})).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2)).toBe(false);
});

test("an external profile revision blocks stale editor without silently overwriting fields", async ({ page }) => {
 const f=await fixture(page);await openPage(page);
 await page.getByRole("button",{name:"Native Test",exact:false}).click();
 const control=page.getByRole("region",{name:"Inbound lifecycle"});
 await control.getByRole("button",{name:"Edit profile",exact:true}).click();
 const editor=page.getByRole("form",{name:"Edit Inbound profile"});
 await editor.getByLabel("Name",{exact:true}).fill("Uncommitted local draft");
 await control.getByLabel("Reason for change",{exact:true}).fill("Stale editor check");
 f.externalEdit();await page.getByRole("button",{name:"Refresh",exact:true}).click();
 await expect(editor.getByRole("alert")).toContainText("profile changed");
 await expect(editor.getByRole("button",{name:"Save profile changes",exact:true})).toBeDisabled();
 await expect(editor.getByLabel("Name",{exact:true})).toHaveValue("Uncommitted local draft");
 expect(f.calls).toHaveLength(0);
});

test("shared port is blocked at owner form and cannot reach live Node mutation endpoint",async({page})=>{
 const f=await fixture(page);
 await openPage(page);
 await page.getByRole("button",{name:"Native Test",exact:false}).click();
 await page.getByLabel("New paired Node").selectOption("new-node");
 await page.getByLabel("Node's assigned IP").fill("203.0.113.7");
 await page.getByLabel("Direct port (0 = disabled)").fill("19600");
 await page.getByLabel("Dark Swap destination port (0 = disabled)").fill("19600");
 await expect(page.getByRole("alert").filter({hasText:"Direct and Dark Swap cannot use the same Node port"})).toBeVisible();
 await expect(page.getByRole("button",{name:"Save Node target"})).toBeDisabled();
 expect(f.calls.filter(c=>c.path.endsWith("/target"))).toHaveLength(0);
 await page.getByLabel("Dark Swap destination port (0 = disabled)").fill("19601");
 await expect(page.getByRole("button",{name:"Save Node target"})).toBeEnabled();
 expect(f.calls).toHaveLength(0);
});

test("320px Persian shared-port warning remains readable and no save is possible",async({page})=>{
 const f=await fixture(page);
 await page.setViewportSize({width:320,height:780});
 await openPage(page,true);
 await page.getByRole("button",{name:"Native Test",exact:false}).click();
 await page.getByRole("button",{name:"Switch language"}).click();
 await page.getByLabel("نود جدید متصل").selectOption("new-node");
 await page.getByLabel("IP اختصاص‌یافته به نود").fill("203.0.113.7");
 await page.getByLabel("پورت مستقیم (صفر = غیرفعال)").fill("19600");
 await page.getByLabel("پورت مقصد Dark Swap (صفر = غیرفعال)").fill("19600");
 await expect(page.getByRole("alert").filter({hasText:"پورت Direct و Dark Swap نباید یکسان باشد"})).toBeVisible();
 await expect(page.getByRole("button",{name:"ذخیره مقصد نود"})).toBeDisabled();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(f.calls.filter(c=>c.path.endsWith("/target"))).toHaveLength(0);
});


test("owner sees true native deployment prerequisites and an explicit durable accounting blocker",async({page})=>{
 const f=await fixture(page);
 await openPage(page);
 const readiness=page.getByRole("region",{name:"Native deployment readiness"});
 await expect(readiness).toBeVisible();
 await expect(readiness.getByRole("heading",{name:"Native Xray · release gates"})).toBeVisible();
 await expect(readiness.getByText("Customer activation blocked").first()).toBeVisible();
 await expect(readiness.getByText(/trusted remote process-birth/)).toBeVisible();
 await expect(readiness.getByText(/initial usage receipt/)).toBeVisible();
 await expect(readiness.getByText("Paired Nodes")).toBeVisible();
 await expect(readiness.getByText("Online Agents")).toBeVisible();
 await expect(readiness.getByRole("heading",{name:"Accounting journal"})).toBeVisible();
 await expect(readiness.getByRole("heading",{name:"First-use receipt barrier"})).toBeVisible();
 await expect(readiness.locator(".native-preflight-step-status").last()).toHaveText("Requires implementation");
 const link=readiness.getByRole("link",{name:"Inspect live Nodes"});
 await expect(link).toHaveAttribute("href","/nodes");
 expect(f.calls).toHaveLength(0);
 await expect(readiness.getByText(/old DARK XRAY subscriptions never imported/)).toBeVisible();
});

test("readiness remains blocked with successful Agent report, and expired owner API fails closed",async({page})=>{
 const f=await fixture(page);f.reportReady();
 await openPage(page);
 const readiness=page.getByRole("region",{name:"Native deployment readiness"});
 await expect(page.getByText("Matching process reported",{exact:true})).toBeVisible();
 await expect(readiness.getByText("Customer activation blocked").first()).toBeVisible();
 await page.route("**/api/v1/workspace/native-inbounds-state",r=>r.fulfill({status:503,json:{error:"unavailable"}}));
 await page.getByRole("button",{name:"Refresh",exact:true}).click();
 await expect(page.getByRole("alert")).toBeVisible();
 await expect(readiness.locator(".native-preflight-stats strong").first()).toHaveText("—");
 await expect(readiness.getByText("Customer activation blocked").first()).toBeVisible();
 expect(f.calls).toHaveLength(0);
});

test("native readiness fits 320px Persian RTL and never claims customer traffic is verified",async({page})=>{
 const f=await fixture(page);
 await page.setViewportSize({width:320,height:760});
 await openPage(page,true);
 await page.getByRole("button",{name:"Switch language"}).click();
 const readiness=page.getByRole("region",{name:"Native deployment readiness"});
 await expect(readiness.getByRole("heading",{name:"آمادگی استقرار Native Xray"})).toBeVisible();
 await expect(readiness.getByText("فعال‌سازی مشتری مسدود است").first()).toBeVisible();
 await expect(readiness.getByRole("heading",{name:"سد تأیید اولین مصرف"})).toBeVisible();
 await expect(readiness.getByText(/HUB عمداً درخواست اجرای Xray/)).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(f.calls).toHaveLength(0);
});
