import { expect, test, type Page } from "@playwright/test";

// Disposable browser fixtures only: no real server, credential or customer.
const csrf = "a".repeat(64);
const installation = "a".repeat(32);
const digest = "d".repeat(64);
async function fixture(page: Page, role = "owner", includeUSA = false, duplicateNodeNames = false) {
 const calls: Array<{ path: string; body: Record<string, unknown>; csrf: string }> = [];
 const base = { enabled: true, archived: false, revision: 1, grpc_service: "", reality_target: "example.org:443", server_names: ["example.org"], short_ids: ["aa"], public_key: "PUBLIC_ONLY_TEST_KEY" };
 const inbounds = [{ ...base, id: 301, name: "Native Test", transport: "tcp", bound_clients: 0 }];
 const operations: Array<Record<string,unknown>> = [];
 const bindings: string[] = [];
 const targets: Array<Record<string,unknown>> = [];
 let canaryStaged = false;
 let applied = false;
 await page.route("**/api/v1/fleet", r => r.fulfill({ json: { nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0, as_of: new Date().toISOString(), interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false, metric_scope: "default_route_interface_not_billable" } }));
 await page.route("**/api/v1/session/status", r => r.fulfill({ json: { authenticated: true, user: { id: role, role, username: "test-owner", representative_id: role === "owner" ? "owner" : "rep-a" }, csrf_token: csrf, expires_unix: 1900000000 } }));
 await page.route("**/api/v1/workspace/**", async route => {
  const req = route.request();const path = new URL(req.url()).pathname;
  if (req.method() === "POST") {
   const body = req.postDataJSON() as Record<string, unknown>;
   calls.push({ path, body, csrf: req.headers()["x-csrf-token"] ?? "" });
   if (path.endsWith("/native-canary/stage")) {
    if (!includeUSA || body.confirmation !== "USA_PRO_LOOPBACK_QA_ONLY") return route.fulfill({status:422,json:{error:"confirmation_required"}});
    if (!canaryStaged) {
     inbounds.push({ ...base, id:901617, name:"USA PRO QA", transport:"tcp", bound_clients:1 });
     targets.push({ inbound_id:901617,node_id:"usa-pro-01",installation_id:installation,listen_ip:"127.0.0.1",direct_port:19666,swap_port:0 });
    }
    canaryStaged = true;
    return route.fulfill({status:201,json:{stage:"staged_only",customer_listener_started:false,subscription_published:false}});
   }
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
   inbounds, nodes: [
    { node_id: "new-node", installation_id: installation, name: "New PRO Node" },
    ...(includeUSA ? [{node_id:"usa-pro-01",installation_id:installation,name:duplicateNodeNames ? "New PRO Node" : "USA PRO"}] : []),
   ], targets, runtime: [{
    node_id: "new-node", installation_id: installation, desired_sequence: 1, desired_sha: digest,
    reported_sequence: applied ? 1 : 0, reported_sha: applied ? digest : "", state: applied ? "applied" : "awaiting_report",
    phase: applied ? "ready" : "", process_ready: applied, reported_at: applied ? new Date().toISOString() : "",
    lease_expires_at: applied ? new Date(Date.now() + 30000).toISOString() : "",
   }],
  } });
  if (path === "/api/v1/workspace/clients") return route.fulfill({ json: { clients: [
   { id: "client-one", name: "Client One", representative_id: "owner", archived: false, deployment_state: "pending_node_apply" },
   ...(canaryStaged ? [{ id: "pro-usa-qa01",name:"USA QA LOOPBACK ONLY",representative_id:"owner",archived:false,deployment_state:"pending_node_apply" }] : []),
  ] } });
  if (path.endsWith("/clients")) return route.fulfill({ json: { client_ids: bindings } });
  if(path.endsWith("/operations"))return route.fulfill({json:{operations}});
  return route.fulfill({ status: 404, json: {} });
 });
 return { calls, inbounds, targets, reportReady: () => { applied = true; }, externalEdit: () => { inbounds[0].revision++; } };
}
async function openPage(page: Page, mobile = false) {
 await page.goto("/");if (mobile) await page.getByRole("button", { name: "Open menu" }).click();
 await page.getByRole("button", { name: "Inbounds", exact: true }).click();
}
async function openInbound(page: Page, name = "Native Test") {
 await page.locator(".inbound-name").filter({ hasText: name }).click();
}
async function openDiagnostics(page: Page, persian = false) {
 const toggle = page.getByRole("button", { name: persian ? "عیب‌یابی" : "Diagnostics", exact: true });
 await expect(toggle).toBeVisible();
 if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
}
async function openTarget(page: Page, persian = false) {
 await page.getByRole("tab", { name: persian ? "مسیرها" : "Routes", exact: true }).click();
 await page.getByRole("button", { name: persian ? "افزودن نود مقصد" : "Add Node target", exact: true }).click();
 return page.getByRole("dialog", { name: persian ? "تنظیم مقصد نود" : "Node deployment", exact: true });
}

test("native owner form saves an Inbound without claiming completed deployment", async ({ page }) => {
 const f = await fixture(page);await openPage(page);
 await page.getByRole("button", { name: "Add Inbound", exact: true }).click();
 const dialog = page.getByRole("dialog", { name: "Create REALITY Inbound", exact: true });
 await dialog.getByLabel("Inbound name", { exact: true }).fill("New actual inbound");
 await dialog.getByRole("tab", { name: "Security", exact: true }).click();
 await dialog.getByLabel("REALITY target (host:port)").fill("example.net:443");
 await dialog.getByLabel("Server names (comma-separated)").fill("example.net");
 await dialog.getByRole("button", { name: "Create and set up routes", exact: true }).click();
 await expect(dialog).toBeHidden();
 await expect(page.getByRole("heading", { name: "New actual inbound", exact: true })).toBeVisible();
 await expect(page).toHaveURL(url => url.pathname === "/inbounds" && url.searchParams.get("inbound") === String(f.calls[0].body.id));
 await expect(page.getByRole("tab", { name: "Routes", exact: true })).toHaveAttribute("aria-selected", "true");
 await expect(page.getByRole("button", { name: "Add Node target", exact: true })).toBeVisible();
 expect(f.calls).toHaveLength(1);expect(f.calls[0].csrf).toBe(csrf);expect(f.calls[0].body.transport).toBe("tcp");
 await expect(page.getByText("Matching process reported", { exact: true })).toHaveCount(0);
});

test("copy settings prefills the public profile and creates a fresh identity without copying keys or assignments", async ({ page }) => {
 const f = await fixture(page);
 Object.assign(f.inbounds[0], { transport: "grpc", grpc_service: "source-grpc", reality_target: "source.example.org:443",
  server_names: ["source.example.org", "alternate.example.org"], bound_clients: 7 });
 const source = f.inbounds[0];
 f.targets.push({ inbound_id: source.id, node_id: "new-node", installation_id: installation,
  listen_ip: "203.0.113.7", direct_port: 19600, swap_port: 19601 });
 await openPage(page);
 await openInbound(page);
 await page.getByRole("button", { name: "Copy settings", exact: true }).click();
 const dialog = page.getByRole("dialog", { name: "Create REALITY Inbound", exact: true });
 const copyName = source.name + " — copy";
 await expect(dialog.getByLabel("Inbound name", { exact: true })).toHaveValue(copyName);
 await dialog.getByRole("tab", { name: "Network", exact: true }).click();
 await expect(dialog.getByLabel("Transport", { exact: true })).toHaveValue(source.transport);
 await expect(dialog.getByLabel("gRPC service", { exact: true })).toHaveValue(source.grpc_service);
 await dialog.getByRole("tab", { name: "Security", exact: true }).click();
 await expect(dialog.getByLabel("REALITY target (host:port)", { exact: true })).toHaveValue(source.reality_target);
 await expect(dialog.getByLabel("Server names (comma-separated)", { exact: true })).toHaveValue(source.server_names.join(", "));
 expect(f.calls).toHaveLength(0);
 await dialog.getByRole("button", { name: "Create and set up routes", exact: true }).click();
 await expect(dialog).toBeHidden();
 await expect(page.getByRole("heading", { name: copyName, exact: true })).toBeVisible();
 expect(f.calls).toHaveLength(1);
 const call = f.calls[0];
 expect(call.path).toBe("/api/v1/workspace/native-inbounds");
 expect(call.csrf).toBe(csrf);
 expect(Object.keys(call.body).sort()).toEqual(["id", "name", "transport", "grpc_service", "reality_target", "server_names", "short_ids"].sort());
 expect(call.body).toMatchObject({ name: copyName, transport: source.transport, grpc_service: source.grpc_service,
  reality_target: source.reality_target, server_names: source.server_names });
 expect(Number.isSafeInteger(call.body.id)).toBe(true);
 expect(Number(call.body.id)).toBeGreaterThan(0);
 expect(call.body.id).not.toBe(source.id);
 expect(call.body.short_ids).toEqual([expect.stringMatching(/^[a-f0-9]{8}$/)]);
 expect(call.body.short_ids).not.toEqual(source.short_ids);
 for (const field of ["public_key", "private_key", "clients", "client_ids", "targets", "node_id", "installation_id"]) expect(call.body).not.toHaveProperty(field);
 await expect(page).toHaveURL(url => url.pathname === "/inbounds" && url.searchParams.get("inbound") === String(call.body.id));
 await expect(page.getByRole("tab", { name: "Routes", exact: true })).toHaveAttribute("aria-selected", "true");
 expect(f.targets).toHaveLength(1);
 expect(f.inbounds.find(inbound => inbound.id === call.body.id)?.bound_clients).toBe(0);
});

test("native workflow binds selected clients and reports only acknowledged Node revision", async ({ page }) => {
 const f = await fixture(page, "owner", true, true);await openPage(page);
 await openInbound(page);
 await page.getByRole("tab", { name: "Clients", exact: true }).click();
 await page.locator(".native-client-row").filter({ hasText: "Client One" }).getByRole("checkbox").check();
 await page.getByRole("button", { name: "Attach selected", exact: true }).click();
 await expect(page.getByLabel("Bound", { exact: true })).toBeVisible();
 const target = await openTarget(page);
 await expect(target.getByRole("option", { name: "New PRO Node (new-node)", exact: true })).toHaveCount(1);
 await expect(target.getByRole("option", { name: "New PRO Node (usa-pro-01)", exact: true })).toHaveCount(1);
 await target.getByLabel("New paired Node").selectOption("new-node");
 await target.getByLabel("Node's assigned IP").fill("203.0.113.7");
 await target.getByLabel("Direct port (0 = disabled)").fill("2087");
 await expect(target.getByRole("button", { name: "Save Node target" })).toBeDisabled();
 await expect(target.getByRole("alert")).toContainText("reserved");
 await target.getByLabel("Direct port (0 = disabled)").fill("19600");
 await target.getByRole("checkbox", { name: /Dark Swap destination listener/ }).check();
 await target.getByLabel("Dark Swap destination port (0 = disabled)").fill("19601");
 await target.getByRole("checkbox", { name: /Dark Swap destination listener/ }).uncheck();
 await expect(target.getByLabel("Dark Swap destination port (0 = disabled)")).toHaveCount(0);
 await target.getByRole("checkbox", { name: /Dark Swap destination listener/ }).check();
 await expect(target.getByLabel("Dark Swap destination port (0 = disabled)")).toHaveValue("19601");
 page.once("dialog", d => d.accept());await target.getByRole("button", { name: "Save Node target" }).click();
 await expect(target).toBeHidden();
 await openDiagnostics(page);
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
 await openInbound(page);
 await page.getByRole("button", { name: "Switch language" }).click();
 await expect(page.getByRole("heading", { name: "Native Test", exact: true })).toBeVisible();
 await page.getByRole("button", { name: "به‌روزرسانی", exact: true }).click();
 await expect(page.getByRole("tab", { name: "خلاصه", exact: true })).toHaveAttribute("aria-selected", "true");
 await expect(page).toHaveURL(url => url.pathname === "/inbounds" && url.searchParams.get("inbound") === "301");
 expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2)).toBe(false);
});

test("failed polling demotes a previous ready report and disables mutation buttons", async ({ page }) => {
 const f = await fixture(page);f.reportReady();await openPage(page);
 await openDiagnostics(page);
 await expect(page.getByText("Matching process reported", { exact: true })).toBeVisible();
 await page.route("**/api/v1/workspace/native-inbounds-state", r => r.fulfill({ status: 503, json: {} }));
 await page.getByRole("button", { name: "Refresh", exact: true }).click();
 await expect(page.getByRole("alert")).toBeVisible();await expect(page.getByText("Report stale / lease expired", { exact: true })).toBeVisible();
 await expect(page.getByRole("button", { name: "Add Inbound", exact: true })).toBeDisabled();
});

test("native profile edit preserves key fields and disable/archive uses revisioned owner operations", async ({ page }) => {
 const f=await fixture(page);await page.setViewportSize({width:320,height:780});await openPage(page,true);
 await openInbound(page);
 const control=page.getByRole("region",{name:"Inbound lifecycle"});
 await control.getByRole("button",{name:"Edit profile",exact:true}).click();
 await control.getByLabel("Reason for change",{exact:true}).fill("Edit without key rotation");
 const editor=page.getByRole("form",{name:"Edit Inbound profile"});
 await editor.getByLabel("Name",{exact:true}).fill("Edited Native");
 await editor.getByRole("tab",{name:"Security",exact:true}).click();
 await editor.getByLabel("Server names",{exact:true}).fill("updated.example.org");
 page.once("dialog",d=>d.accept());await editor.getByRole("button",{name:"Save profile changes",exact:true}).click();
 await expect(page.getByRole("heading",{name:"Edited Native",exact:true})).toBeVisible();
 await expect(control.getByText("Change recorded; a fresh Agent report is still required.")).toBeVisible();
 expect(f.calls).toHaveLength(1);expect(f.calls[0].body.expected_revision).toBe(1);
 const p=f.calls[0].body.profile as Record<string,unknown>;
 expect(p.short_ids).toEqual(["aa"]);expect(p.id).toBe(301);expect(p).not.toHaveProperty("private_key");expect(p).not.toHaveProperty("public_key");
 await control.locator("summary").filter({hasText:"More actions"}).click();
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
 await openInbound(page);
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
 await openInbound(page);
 const target = await openTarget(page);
 await target.getByLabel("New paired Node").selectOption("new-node");
 await target.getByLabel("Node's assigned IP").fill("203.0.113.7");
 await target.getByLabel("Direct port (0 = disabled)").fill("19600");
 await target.getByRole("checkbox", { name: /Dark Swap destination listener/ }).check();
 await target.getByLabel("Dark Swap destination port (0 = disabled)").fill("19600");
 await expect(target.getByRole("alert").filter({hasText:"Direct and Dark Swap cannot use the same Node port"})).toBeVisible();
 await expect(target.getByRole("button",{name:"Save Node target"})).toBeDisabled();
 expect(f.calls.filter(c=>c.path.endsWith("/target"))).toHaveLength(0);
 await target.getByLabel("Dark Swap destination port (0 = disabled)").fill("19601");
 await expect(target.getByRole("button",{name:"Save Node target"})).toBeEnabled();
 expect(f.calls).toHaveLength(0);
});

test("320px Persian shared-port warning remains readable and no save is possible",async({page})=>{
 const f=await fixture(page);
 await page.setViewportSize({width:320,height:780});
 await openPage(page,true);
 await openInbound(page);
 await page.getByRole("button",{name:"Switch language"}).click();
 const target = await openTarget(page, true);
 await target.getByLabel("نود جدید متصل").selectOption("new-node");
 await target.getByLabel("IP اختصاص‌یافته به نود").fill("203.0.113.7");
 await target.getByLabel("پورت مستقیم (صفر = غیرفعال)").fill("19600");
 await target.getByRole("checkbox", { name: /دریافت دارک‌سواپ/ }).check();
 await target.getByLabel("پورت مقصد Dark Swap (صفر = غیرفعال)").fill("19600");
 await expect(target.getByRole("alert").filter({hasText:"پورت Direct و Dark Swap نباید یکسان باشد"})).toBeVisible();
 await expect(target.getByRole("button",{name:"ذخیره مقصد نود"})).toBeDisabled();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(f.calls.filter(c=>c.path.endsWith("/target"))).toHaveLength(0);
});


test("owner sees true native deployment prerequisites and an explicit durable accounting blocker",async({page})=>{
 const f=await fixture(page);
 await openPage(page);
 await openDiagnostics(page);
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
 await openDiagnostics(page);
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
 await openDiagnostics(page, true);
 const readiness=page.getByRole("region",{name:"Native deployment readiness"});
 await expect(readiness.getByRole("heading",{name:"آمادگی استقرار Native Xray"})).toBeVisible();
 await expect(readiness.getByText("فعال‌سازی مشتری مسدود است").first()).toBeVisible();
 await expect(readiness.getByRole("heading",{name:"سد تأیید اولین مصرف"})).toBeVisible();
 await expect(readiness.getByText(/HUB عمداً درخواست اجرای Xray/)).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(f.calls).toHaveLength(0);
});


test("owner stages a single isolated USA canary only after explicit checkbox consent, never activates Xray",async({page})=>{
 const f=await fixture(page,"owner",true);
 await openPage(page);
 await openDiagnostics(page);
 const canary=page.getByRole("region",{name:"USA PRO loopback canary"});
 await expect(canary.getByRole("heading",{name:"USA loopback canary"})).toBeVisible();
 await expect(canary.getByText(/127.0.0.1:19666/)).toBeVisible();
 await expect(canary.getByText(/1 MiB limit/)).toBeVisible();
 const stage=canary.getByRole("button",{name:"Stage private test"});
 await expect(stage).toBeDisabled();
 const checkbox=canary.getByRole("checkbox");
 await checkbox.check();
 await expect(stage).toBeEnabled();
 await stage.click();
 await expect(canary.getByRole("button",{name:/Canary staged/})).toBeDisabled();
 expect(f.calls).toHaveLength(1);
 expect(f.calls[0].path).toBe("/api/v1/workspace/native-canary/stage");
 expect(f.calls[0].csrf).toBe(csrf);
 expect(f.calls[0].body).toEqual({confirmation:"USA_PRO_LOOPBACK_QA_ONLY"});
 await expect(page.getByText("Matching process reported",{exact:true})).toHaveCount(0);
});

test("a staged canary stays staged when client search and later pages exclude its QA client", async ({ page }) => {
 const f = await fixture(page, "owner", true);
 f.inbounds.push({ ...f.inbounds[0], id: 901617, name: "USA PRO QA", bound_clients: 1 });
 f.targets.push({ inbound_id: 901617, node_id: "usa-pro-01", installation_id: installation,
  listen_ip: "127.0.0.1", direct_port: 19666, swap_port: 0 });
 const qa = { id: "pro-usa-qa01", name: "USA QA LOOPBACK ONLY", representative_id: "owner", archived: false, deployment_state: "pending_node_apply" };
 const clients = [qa, ...Array.from({ length: 101 }, (_, index) => ({ ...qa,
  id: "regular-client-" + index, name: "Regular client " + String(index).padStart(3, "0") }))];
 const queries: Array<{ q: string; limit: number; offset: number }> = [];
 await page.route("**/api/v1/workspace/clients?*", route => {
  const params = new URL(route.request().url()).searchParams;
  const q = params.get("q") ?? "", limit = Number(params.get("limit") ?? 100), offset = Number(params.get("offset") ?? 0);
  queries.push({ q, limit, offset });
  const matches = clients.filter(client => (client.id + " " + client.name).toLowerCase().includes(q.toLowerCase()));
  return route.fulfill({ json: { clients: matches.slice(offset, offset + limit) } });
 });
 await openPage(page);
 await openInbound(page);
 await openDiagnostics(page);
 const canary = page.getByRole("region", { name: "USA PRO loopback canary", exact: true });
 const staged = canary.getByRole("button", { name: /Canary staged/ });
 await expect(staged).toBeDisabled();
 expect(queries).toContainEqual({ q: "pro-usa-qa01", limit: 25, offset: 0 });
 await page.getByRole("tab", { name: "Clients", exact: true }).click();
 const clientPanel = page.getByRole("tabpanel", { name: "Clients", exact: true });
 const rows = clientPanel.locator(".native-client-row");
 const search = clientPanel.getByLabel("Search all clients", { exact: true });
 await expect(rows).toHaveCount(100);
 await search.fill("Regular client 100");
 await expect(rows).toHaveCount(1);
 await expect(rows.filter({ hasText: qa.id })).toHaveCount(0);
 await expect(staged).toBeDisabled();
 await search.clear();
 await expect(rows).toHaveCount(100);
 await clientPanel.getByRole("button", { name: "Next", exact: true }).click();
 await expect(rows).toHaveCount(2);
 await expect(rows.filter({ hasText: qa.id })).toHaveCount(0);
 await expect(staged).toBeDisabled();
 expect(queries).toContainEqual({ q: "", limit: 100, offset: 100 });
 expect(f.calls).toHaveLength(0);
});

test("unpaired USA and delayed API never allow a canary command",async({page})=>{
 const f=await fixture(page);
 await openPage(page);
 await openDiagnostics(page);
 const canary=page.getByRole("region",{name:"USA PRO loopback canary"});
 await expect(canary.getByRole("button",{name:"Stage private test"})).toBeDisabled();
 expect(f.calls).toHaveLength(0);
 await expect(canary.getByText(/Requires a fresh authenticated USA PRO Node/)).toBeVisible();
});

test("320px Persian owner canary requires consent and does not overflow",async({page})=>{
 const f=await fixture(page,"owner",true);
 await page.setViewportSize({width:320,height:760});
 await openPage(page,true);
 await page.getByRole("button",{name:"Switch language"}).click();
 await openDiagnostics(page, true);
 const card=page.getByRole("region",{name:"USA PRO loopback canary"});
 await expect(card.getByRole("heading",{name:"تست کنترل‌شدهٔ آمریکا"})).toBeVisible();
 await expect(card.getByText(/127.0.0.1:19666/)).toBeVisible();
 const button=card.getByRole("button",{name:"آماده‌سازی تست خصوصی"});
 await expect(button).toBeDisabled();
 await card.getByRole("checkbox").check();
 await expect(button).toBeEnabled();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(f.calls).toHaveLength(0);
});

test("the compact list paginates and combines search, status, transport and Node filters without mutations", async ({ page }) => {
 const f = await fixture(page);
 const base = f.inbounds[0];
 f.inbounds.splice(0, f.inbounds.length, ...Array.from({ length: 26 }, (_, index) => ({
  ...base, id: 501 + index, name: "Fixture inbound " + String(index + 1).padStart(2, "0"),
  transport: index % 2 ? "grpc" : "tcp", grpc_service: index % 2 ? "fixture-grpc" : "",
  enabled: index < 24, archived: index === 25,
 })));
 f.targets.push({ inbound_id: 526, node_id: "new-node", installation_id: installation,
  listen_ip: "203.0.113.7", direct_port: 19600, swap_port: 0 });
 await openPage(page);
 const rows = page.locator(".inbound-name").filter({ hasText: /Fixture inbound \d\d/ });
 const pager = page.getByLabel("Inbound pagination", { exact: true });
 const search = page.getByLabel("Search Inbounds", { exact: true });
 await expect(rows).toHaveCount(20);
 await expect(page.getByRole("dialog")).toHaveCount(0);
 await expect(page.getByRole("tab")).toHaveCount(0);
 await expect(page.getByRole("button", { name: "Diagnostics", exact: true })).toHaveAttribute("aria-expanded", "false");
 for (const region of ["Native deployment readiness", "USA PRO loopback canary", "LAB route transactions", "Metered TCP LAB transactions"]) {
  await expect(page.getByRole("region", { name: region, exact: true })).toHaveCount(0);
 }
 const firstPage = await rows.allTextContents();
 await pager.getByRole("button", { name: "Next", exact: true }).click();
 await expect(rows).toHaveCount(6);
 expect((await rows.allTextContents()).some(row => firstPage.includes(row))).toBe(false);
 await expect(pager.getByRole("button", { name: "Next", exact: true })).toBeDisabled();

 await search.fill("Fixture inbound 03");
 await expect(rows).toHaveCount(1);
 await expect(rows).toContainText("Fixture inbound 03");
 await expect(pager.getByRole("button", { name: "Previous", exact: true })).toBeDisabled();
 await search.fill("526");
 await expect(rows).toHaveCount(1);
 await expect(rows).toContainText("Fixture inbound 26");
 await search.clear();
 await page.getByLabel("Status", { exact: true }).selectOption("disabled");
 await expect(rows).toHaveCount(1);
 await expect(rows).toContainText("Fixture inbound 25");
 await page.getByLabel("Status", { exact: true }).selectOption("archived");
 await expect(rows).toHaveCount(1);
 await expect(rows).toContainText("Fixture inbound 26");
 await page.getByLabel("Status", { exact: true }).selectOption({ index: 0 });
 await page.getByLabel("Transport", { exact: true }).selectOption("grpc");
 await expect(rows).toHaveCount(13);
 await page.getByLabel("Node", { exact: true }).selectOption("new-node");
 await expect(rows).toHaveCount(1);
 await expect(rows).toContainText("Fixture inbound 26");
 await search.fill("No matching inbound");
 await expect(rows).toHaveCount(0);
 await search.clear();
 await expect(rows).toHaveCount(1);
 expect(f.calls).toHaveLength(0);
});

for (const language of ["en", "fa"] as const) {
 test(`create dialog contains keyboard focus and Cancel/Escape never writes (${language})`, async ({ page }) => {
  const f = await fixture(page);
  const persian = language === "fa";
  await page.addInitScript(theme => localStorage.setItem("dark-pro-theme", theme), persian ? "dark" : "light");
  if (persian) await page.setViewportSize({ width: 320, height: 780 });
  await openPage(page, persian);
  if (persian) await page.getByRole("button", { name: "Switch language" }).click();
  if (persian) {
   const filters = page.getByRole("button", { name: "فیلترها", exact: true });
   await expect(filters).toHaveAttribute("aria-expanded", "false");
   await expect(page.getByLabel("وضعیت", { exact: true })).toBeHidden();
   await filters.click();
   await page.getByLabel("وضعیت", { exact: true }).selectOption("disabled");
   await expect(page.locator(".inbound-table tbody tr")).toHaveCount(0);
   await page.getByRole("button", { name: "پاک‌کردن فیلترها", exact: true }).click();
   await expect(page.locator(".inbound-table tbody tr")).toHaveCount(1);
   await filters.click();
   await expect(filters).toHaveAttribute("aria-expanded", "false");
  }
  const opener = page.getByRole("button", { name: persian ? "افزودن اینباند" : "Add Inbound", exact: true });
  const dialog = page.getByRole("dialog", { name: persian ? "ساخت اینباند REALITY" : "Create REALITY Inbound", exact: true });
  await opener.click();
  await expect(dialog).toBeVisible();
  expect(await dialog.evaluate(element => element.matches(":modal"))).toBe(true);
  const name = dialog.getByLabel(persian ? "نام اینباند" : "Inbound name", { exact: true });
  await expect(name).toBeFocused();
  await name.fill("Unsubmitted browser fixture");
  await dialog.getByRole("button").last().focus();
  await page.keyboard.press("Tab");
  expect(await dialog.evaluate(element => element.contains(document.activeElement))).toBe(true);
  let prompts = 0;
  page.once("dialog", async confirmation => { prompts++; await confirmation.dismiss(); });
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  await expect(name).toHaveValue("Unsubmitted browser fixture");
  page.once("dialog", async confirmation => { prompts++; await confirmation.accept(); });
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(opener).toBeFocused();
  expect(prompts).toBe(2);
  expect(f.calls).toHaveLength(0);
  await opener.click();
  await dialog.getByRole("button", { name: persian ? "انصراف" : "Cancel", exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(opener).toBeFocused();
  expect(f.calls).toHaveLength(0);
  await expect(page.locator("html")).toHaveAttribute("dir", persian ? "rtl" : "ltr");
  await expect(page.locator("html")).toHaveAttribute("data-theme", persian ? "dark" : "light");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
 });
}

test("detail tabs support keyboard navigation and preserve an unsaved profile across visited panels", async ({ page }) => {
 const f = await fixture(page);
 await openPage(page);
 await openInbound(page);
 const overview = page.getByRole("tab", { name: "Overview", exact: true });
 const routes = page.getByRole("tab", { name: "Routes", exact: true });
 const activity = page.getByRole("tab", { name: "Activity", exact: true });
 await expect(overview).toHaveAttribute("aria-selected", "true");
 await expect(overview).toHaveAttribute("aria-controls", /.+/);
 await expect(page.getByRole("heading", { name: "Inbound change history", exact: true })).toHaveCount(0);
 const lifecycle = page.getByRole("region", { name: "Inbound lifecycle", exact: true });
 await lifecycle.getByRole("button", { name: "Edit profile", exact: true }).click();
 const editor = page.getByRole("form", { name: "Edit Inbound profile", exact: true });
 await editor.getByLabel("Name", { exact: true }).fill("Keep this local edit");
 await lifecycle.getByLabel("Reason for change", { exact: true }).fill("Unsaved tab navigation check");
 await overview.focus();
 await page.keyboard.press("ArrowRight");
 await expect(routes).toBeFocused();
 await expect(routes).toHaveAttribute("aria-selected", "true");
 await expect(editor).toBeHidden();
 await expect(page.getByRole("region", { name: "Protected Inbound routing draft", exact: true })).toBeVisible();
 await page.keyboard.press("End");
 await expect(activity).toBeFocused();
 await expect(activity).toHaveAttribute("aria-selected", "true");
 await expect(page.getByRole("heading", { name: "Inbound change history", exact: true })).toBeVisible();
 await expect(page.getByRole("button", { name: "Edit profile", exact: true })).toHaveCount(0);
 await page.keyboard.press("Home");
 await expect(overview).toBeFocused();
 await expect(overview).toHaveAttribute("aria-selected", "true");
 await expect(editor.getByLabel("Name", { exact: true })).toHaveValue("Keep this local edit");
 await expect(lifecycle.getByLabel("Reason for change", { exact: true })).toHaveValue("Unsaved tab navigation check");
 await page.getByRole("tab", { name: "Clients", exact: true }).click();
 await page.locator(".native-client-row").filter({ hasText: "Client One" }).getByRole("checkbox").check();
 await routes.click();
 await page.getByRole("tab", { name: "Clients", exact: true }).click();
 await expect(page.locator(".native-client-row").filter({ hasText: "Client One" }).getByRole("checkbox")).toBeChecked();
 expect(f.calls).toHaveLength(0);
});

test("sidebar navigation asks before discarding an unsaved Inbound profile", async ({ page }) => {
 const f = await fixture(page);
 await openPage(page);
 await openInbound(page);
 await page.getByRole("button", { name: "Edit profile", exact: true }).click();
 const editor = page.getByRole("form", { name: "Edit Inbound profile", exact: true });
 await editor.getByLabel("Name", { exact: true }).fill("Keep before leaving Inbounds");
 const currentURL = page.url();
 const nodes = page.getByRole("navigation", { name: "Main navigation" }).getByRole("button", { name: "Nodes", exact: true });
 let prompts = 0;
 page.once("dialog", async dialog => { prompts++; await dialog.dismiss(); });
 await nodes.click();
 await expect(page).toHaveURL(currentURL);
 await expect(editor.getByLabel("Name", { exact: true })).toHaveValue("Keep before leaving Inbounds");
 expect(prompts).toBe(1);
 expect(f.calls).toHaveLength(0);
 page.once("dialog", async dialog => { prompts++; await dialog.accept(); });
 await nodes.click();
 await expect(page).toHaveURL(/\/nodes$/);
 await expect(editor).toHaveCount(0);
 expect(prompts).toBe(2);
 expect(f.calls).toHaveLength(0);
});

for (const language of ["en", "fa"] as const) {
 test(`DARK creation sections keep fields, keyboard order and visible footer (${language})`, async ({ page }) => {
  const persian = language === "fa";
  const f = await fixture(page);
  await page.setViewportSize(persian ? { width: 320, height: 780 } : { width: 1440, height: 900 });
  await page.addInitScript(theme => localStorage.setItem("dark-pro-theme", theme), persian ? "dark" : "light");
  await openPage(page, persian);
  if (persian) await page.getByRole("button", { name: "Switch language" }).click();
  await page.getByRole("button", { name: persian ? "افزودن اینباند" : "Add Inbound", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: persian ? "ساخت اینباند REALITY" : "Create REALITY Inbound", exact: true });
  const basics = dialog.getByRole("tab", { name: persian ? "پایه" : "Basics", exact: true });
  const network = dialog.getByRole("tab", { name: persian ? "شبکه" : "Network", exact: true });
  const security = dialog.getByRole("tab", { name: persian ? "امنیت" : "Security", exact: true });
  const name = dialog.getByLabel(persian ? "نام اینباند" : "Inbound name", { exact: true });
  const transport = dialog.getByLabel(persian ? "ترنسپورت" : "Transport", { exact: true });
  const service = dialog.getByLabel(persian ? "سرویس gRPC" : "gRPC service", { exact: true });
  const target = dialog.getByLabel(persian ? "مقصد REALITY (هاست:پورت)" : "REALITY target (host:port)", { exact: true });
  const names = dialog.getByLabel(persian ? "نام‌های سرور (با کاما)" : "Server names (comma-separated)", { exact: true });
  await expect(dialog.getByRole("tab")).toHaveCount(3);
  await expect(basics).toHaveAttribute("aria-selected", "true");
  await expect(dialog.getByRole("tabpanel")).toHaveCount(1);
  await name.fill("Retained creation draft");
  await basics.focus();
  await page.keyboard.press(persian ? "ArrowLeft" : "ArrowRight");
  await expect(network).toBeFocused();
  await expect(network).toHaveAttribute("aria-selected", "true");
  await expect(name).toBeHidden();
  await expect(transport.locator("option")).toHaveText(["TCP / REALITY", "gRPC / REALITY"]);
  await transport.selectOption("grpc");
  await service.fill("retained-grpc");
  await network.focus();
  await page.keyboard.press("End");
  await expect(security).toBeFocused();
  await target.fill("retained.example.org:443");
  await names.fill("retained.example.org, alternate.example.org");
  await security.focus();
  await page.keyboard.press("Home");
  await expect(basics).toBeFocused();
  await expect(name).toHaveValue("Retained creation draft");
  await network.click();
  await expect(service).toHaveValue("retained-grpc");
  await transport.selectOption("tcp");
  await expect(service).toHaveCount(0);
  await transport.selectOption("grpc");
  await expect(service).toHaveValue("retained-grpc");
  await security.click();
  await expect(target).toHaveValue("retained.example.org:443");
  await expect(names).toHaveValue("retained.example.org, alternate.example.org");
  for (const tab of [basics, network, security]) {
   await tab.click();
   const layout = await dialog.evaluate((element, minimumHeight) => {
    const bounds = element.getBoundingClientRect();
    const footer = element.querySelector("footer")!.getBoundingClientRect();
    const controls = Array.from(element.querySelectorAll<HTMLElement>("button,input,select")).filter(control => control.getClientRects().length > 0);
    return { left: bounds.left, right: bounds.right, width: innerWidth, footerTop: footer.top, footerBottom: footer.bottom, height: innerHeight,
     overflow: element.scrollWidth > element.clientWidth + 2,
     smallControls: controls.filter(control => control.getBoundingClientRect().height < minimumHeight).map(control => control.getAttribute("aria-label") || control.textContent) };
   }, persian ? 44 : 39);
   expect(layout.left).toBeGreaterThanOrEqual(-1);
   expect(layout.right).toBeLessThanOrEqual(layout.width + 1);
   expect(layout.footerTop).toBeGreaterThanOrEqual(0);
   expect(layout.footerBottom).toBeLessThanOrEqual(layout.height + 1);
   expect(layout.overflow).toBe(false);
   expect(layout.smallControls).toEqual([]);
  }
  expect(f.calls).toHaveLength(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
 });
}

test("creation validation reveals the first invalid section without POST and accepts an IPv6 target", async ({ page }) => {
 const f = await fixture(page); await openPage(page);
 const browserErrors: string[] = [];
 page.on("pageerror", error => browserErrors.push(error.message));
 await page.getByRole("button", { name: "Add Inbound", exact: true }).click();
 const dialog = page.getByRole("dialog", { name: "Create REALITY Inbound", exact: true });
 const basics = dialog.getByRole("tab", { name: "Basics", exact: true });
 const network = dialog.getByRole("tab", { name: "Network", exact: true });
 const security = dialog.getByRole("tab", { name: "Security", exact: true });
 const name = dialog.getByLabel("Inbound name", { exact: true });
 const service = dialog.getByLabel("gRPC service", { exact: true });
 const target = dialog.getByLabel("REALITY target (host:port)", { exact: true });
 const names = dialog.getByLabel("Server names (comma-separated)", { exact: true });
 const save = dialog.getByRole("button", { name: "Create and set up routes", exact: true });
 await save.click();
 await expect(basics).toHaveAttribute("aria-selected", "true");
 await expect(name).toBeFocused();
 expect(f.calls).toHaveLength(0);
 await name.fill("Validated IPv6 fixture");
 await network.click();
 await dialog.getByLabel("Transport", { exact: true }).selectOption("grpc");
 await basics.click();
 await save.click();
 await expect(network).toHaveAttribute("aria-selected", "true");
 await expect(service).toBeFocused();
 await service.fill("not a service");
 await save.click();
 await expect(service).toHaveAttribute("aria-invalid", "true");
 expect(f.calls).toHaveLength(0);
 await service.fill("valid-grpc");
 await basics.click();
 await save.click();
 await expect(security).toHaveAttribute("aria-selected", "true");
 await expect(target).toBeFocused();
 await target.fill("example.org");
 await names.fill("example.org");
 await basics.click();
 await save.click();
 await expect(target).toBeFocused();
 expect(f.calls).toHaveLength(0);
 await target.fill("[2001:db8::10]:443");
 await names.fill(", , ");
 await basics.click();
 await save.click();
 await expect(security).toHaveAttribute("aria-selected", "true");
 await expect(names).toBeFocused();
 expect(f.calls).toHaveLength(0);
 await names.fill("first.example.org, second.example.org");
 await dialog.getByRole("button", { name: "Check form", exact: true }).click();
 await expect(dialog.getByRole("status")).toHaveText("Profile is ready to save.");
 expect(f.calls).toHaveLength(0);
 await save.click();
 await expect(dialog).toBeHidden();
 expect(f.calls).toHaveLength(1);
 expect(f.calls[0].body).toMatchObject({ name: "Validated IPv6 fixture", transport: "grpc", grpc_service: "valid-grpc",
  reality_target: "[2001:db8::10]:443", server_names: ["first.example.org", "second.example.org"] });
 await expect(page.getByRole("tab", { name: "Routes", exact: true })).toHaveAttribute("aria-selected", "true");
 expect(browserErrors).toEqual([]);
});

test("failed creation keeps the drawer and retries the exact identity and payload", async ({ page }) => {
 const f = await fixture(page);
 const attempts: Array<{ body: Record<string, unknown>; csrf: string }> = [];
 await page.route("**/api/v1/workspace/native-inbounds", async route => {
  if (route.request().method() !== "POST") return route.fallback();
  attempts.push({ body: route.request().postDataJSON() as Record<string, unknown>, csrf: route.request().headers()["x-csrf-token"] ?? "" });
  if (attempts.length === 1) return route.fulfill({ status: 503, json: { error: "synthetic_transient_failure" } });
  return route.fallback();
 });
 await openPage(page);
 await page.getByRole("button", { name: "Add Inbound", exact: true }).click();
 const dialog = page.getByRole("dialog", { name: "Create REALITY Inbound", exact: true });
 const save = dialog.getByRole("button", { name: "Create and set up routes", exact: true });
 await dialog.getByLabel("Inbound name", { exact: true }).fill("Retry creation fixture");
 await dialog.getByRole("tab", { name: "Security", exact: true }).click();
 await dialog.getByLabel("REALITY target (host:port)", { exact: true }).fill("retry.example.org:443");
 await dialog.getByLabel("Server names (comma-separated)", { exact: true }).fill("retry.example.org");
 await save.click();
 await expect(dialog.getByRole("alert")).toContainText("Request failed.");
 await expect(dialog).toBeVisible();
 await dialog.getByRole("tab", { name: "Basics", exact: true }).click();
 await expect(dialog.getByLabel("Inbound name", { exact: true })).toHaveValue("Retry creation fixture");
 await dialog.getByRole("tab", { name: "Security", exact: true }).click();
 await expect(dialog.getByLabel("REALITY target (host:port)", { exact: true })).toHaveValue("retry.example.org:443");
 // A fresh state poll clears the existing stale-data gate; it must not resubmit.
 await expect(save).toBeEnabled({ timeout: 8000 });
 expect(attempts).toHaveLength(1);
 expect(f.calls).toHaveLength(0);
 await save.click();
 await expect(dialog).toBeHidden();
 await expect(page.getByRole("heading", { name: "Retry creation fixture", exact: true })).toBeVisible();
 expect(attempts).toHaveLength(2);
 expect(attempts[1]).toEqual(attempts[0]);
 expect(attempts[0].csrf).toBe(csrf);
 expect(f.calls).toHaveLength(1);
 expect(f.calls[0].body).toEqual(attempts[0].body);
});

test("canceling browser Back preserves dirty fields and accepted Back clears the previous Inbound client selection", async ({ page }) => {
 const f = await fixture(page);
 f.inbounds.push({ ...f.inbounds[0], id: 302, name: "Second Inbound" });
 await openPage(page);
 await openInbound(page);
 const clientsTab = page.getByRole("tab", { name: "Clients", exact: true });
 const overviewTab = page.getByRole("tab", { name: "Overview", exact: true });
 const client = page.locator(".native-client-row").filter({ hasText: "Client One" }).getByRole("checkbox");
 await clientsTab.click();
 await page.getByLabel("Search all clients", { exact: true }).fill("Client");
 await client.check();
 await overviewTab.click();
 await page.getByRole("button", { name: "Edit profile", exact: true }).click();
 const editor = page.getByRole("form", { name: "Edit Inbound profile", exact: true });
 await editor.getByLabel("Name", { exact: true }).fill("Back must not lose this draft");
 const currentURL = page.url();
 let prompts = 0;
 page.once("dialog", async dialog => { prompts++; await dialog.dismiss(); });
 await page.goBack();
 await expect(page).toHaveURL(currentURL);
 await expect(editor.getByLabel("Name", { exact: true })).toHaveValue("Back must not lose this draft");
 await clientsTab.click();
 await expect(client).toBeChecked();
 await expect(page.getByLabel("Search all clients", { exact: true })).toHaveValue("Client");
 await overviewTab.click();
 expect(prompts).toBe(1);

 page.once("dialog", async dialog => { prompts++; await dialog.accept(); });
 await page.goBack();
 await expect(page).toHaveURL(url => url.pathname === "/inbounds" && !url.searchParams.has("inbound"));
 await openInbound(page, "Second Inbound");
 await clientsTab.click();
 await expect(page.getByRole("heading", { name: "Second Inbound", exact: true })).toBeVisible();
 await expect(client).not.toBeChecked();
 await expect(page.getByLabel("Search all clients", { exact: true })).toHaveValue("");
 await expect(page.getByRole("button", { name: "Attach selected", exact: true })).toBeDisabled();
 expect(prompts).toBe(2);
 expect(f.calls).toHaveLength(0);
});


test("legacy profile edit and copy retain advanced settings even when capability controls are unavailable", async ({ page }) => {
 const f = await fixture(page);
 const options = { grpc_authority: "edge.example.org:8443", grpc_multi_mode: true, reality_max_time_diff: 15000,
  reality_min_client_ver: "1.8.0", reality_max_client_ver: "25.9.11", reality_xver: 2 };
 Object.assign(f.inbounds[0], { transport: "grpc", grpc_service: "retained-grpc", options });
 await openPage(page); await openInbound(page);
 const control = page.getByRole("region", { name: "Inbound lifecycle" });
 await control.getByRole("button", { name: "Edit profile", exact: true }).click();
 const editor = page.getByRole("form", { name: "Edit Inbound profile" });
 await expect(editor.getByRole("checkbox", { name: "Enable Sniffing", exact: true })).toHaveCount(0);
 await expect(editor.getByRole("button", { name: "Add fallback", exact: true })).toHaveCount(0);
 await editor.getByLabel("Name", { exact: true }).fill("Retained advanced profile");
 await editor.getByLabel("Reason for change", { exact: true }).fill("Rename without clearing settings");
 page.once("dialog", prompt => prompt.accept()); await editor.getByRole("button", { name: "Save profile changes", exact: true }).click();
 await expect(editor).toBeHidden();
 expect(f.calls[0].body.profile).toMatchObject({ name: "Retained advanced profile", options });
 await page.getByRole("button", { name: "Copy settings", exact: true }).click();
 const dialog = page.getByRole("dialog", { name: "Create REALITY Inbound", exact: true });
 await dialog.getByRole("tab", { name: "Security", exact: true }).click();
 await dialog.locator(".inbound-profile-advanced").filter({ hasText: "Advanced REALITY settings" }).locator("summary").click();
 await expect(dialog.getByLabel("Maximum time difference (ms)", { exact: true })).toHaveValue("15000");
 await expect(dialog.getByLabel("Maximum time difference (ms)", { exact: true })).toBeDisabled();
 await dialog.getByRole("button", { name: "Create and set up routes", exact: true }).click();
 await expect(dialog).toBeHidden();
 expect(f.calls[1].body).toMatchObject({ name: "Retained advanced profile — copy", options });
 expect(f.calls[1].body.id).not.toBe(301);
 expect(f.calls[1].body).not.toHaveProperty("public_key");
});
