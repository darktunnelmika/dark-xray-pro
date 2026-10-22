import { test, expect, type Page } from "@playwright/test";

const pairCode = "c".repeat(64);
const owner = { id: "owner", username: "new-pro-owner", role: "owner", representative_id: "owner" };
const representative = { id: "rep-01", username: "rep-01", role: "representative", representative_id: "rep-01" };

const dependencyKeys = "inbound_targets active_inbound_targets assigned_clients route_drafts draft_events swap_upstream_references config_revisions runtime_deliveries runtime_reports route_channels route_commands pending_route_commands unknown_route_commands metered_channels metered_commands pending_metered_commands unknown_metered_commands metered_births foreign_installation_rows usage_epochs open_usage_epochs uncertain_usage_epochs sealed_usage_epochs usage_holds usage_receipts usage_deltas foreign_usage_epochs replacement_operations noncancelled_replacements historical_stage_references historical_upstream_command_references unmapped_historical_commands saved_config_stages pending_config_stages foreign_config_stages saved_lab_versions selected_lab_versions previous_lab_versions lab_selection_events foreign_lab_versions".split(" ");
async function setupDependencies(page:Page, mode="normal") {
  await setupNames(page);
  let reads=0;
  await page.route("**/api/v1/**",async route=>{
    if(!new URL(route.request().url()).pathname.endsWith("/lab-name/dependencies"))return route.fallback();
    expect(route.request().method()).toBe("GET");reads++;
    if(mode==="normal"&&reads>1)return route.fulfill({status:503,json:{error:"node_dependencies_unavailable"}});
    const counts:Record<string,number|null>=Object.fromEntries(dependencyKeys.map(k=>[k,0]));
    counts.inbound_targets=2;counts.swap_upstream_references=1;counts.usage_epochs=null;counts.saved_config_stages=3;counts.unmapped_historical_commands=2;counts.historical_upstream_command_references=null;
    const body={node_id:"lab-name",installation_id:"a".repeat(32),observed_at:"2026-10-20T12:00:00Z",coverage:"native_sqlite_and_retained_journals",atomic_across_stores:false,can_retire:false,can_replace:false,lifecycle_blocker:"atomic_node_lifecycle_not_implemented",counts,unknown_reasons:mode==="missing-reason"?{}:{usage_epochs:"usage_ledger_disabled",historical_upstream_command_references:"historical_node_map_missing"}};
    if(mode==="wrong-node")body.node_id="other-node";
    if(mode==="unsafe")body.can_retire=true;
    return route.fulfill({json:body});
  });
  return {reads:()=>reads};
}

test("dependency inspection is on demand and failed refresh removes old counts",async({page})=>{
  const probe=await setupDependencies(page);await page.goto("/nodes");await login(page);
  await expect(page.getByRole("button",{name:"Inspect node dependencies"})).toBeVisible();expect(probe.reads()).toBe(0);
  await page.getByRole("button",{name:"Inspect node dependencies"}).click();
  await expect(page.locator('[data-dependency="inbound_targets"] dd')).toHaveText("2");
  await expect(page.locator('[data-dependency="usage_epochs"] dd')).toContainText("Unknown");
  await expect(page.getByText(/Empty counts do NOT authorize deletion/)).toBeVisible();
  await page.getByRole("button",{name:"Refresh dependencies"}).click();
  await expect(page.getByText(/Counts are unavailable, not zero/)).toBeVisible();
  await expect(page.locator("[data-dependency]")).toHaveCount(0);expect(probe.reads()).toBe(2);
});

test("dependency snapshot fits 320px Persian with explicit unknown accounting",async({page})=>{
  await setupDependencies(page);await page.setViewportSize({width:320,height:760});await page.goto("/nodes");await login(page);
  await page.getByRole("button",{name:"Switch language"}).click();
  await page.getByRole("button",{name:"بررسی وابستگی‌های نود"}).click();
  await expect(page.locator('[data-dependency="usage_epochs"] dd')).toContainText("نامعلوم");
  await expect(page.locator('[data-dependency="swap_upstream_references"] dd')).toHaveText("۱");
  await expect(page.locator('[data-dependency="historical_upstream_command_references"] dd')).toContainText("فرمان‌های قدیمی نقشهٔ نودهای وابسته ندارند");
  await expect(page.locator('[data-dependency="saved_config_stages"] dd')).toHaveText("۳");
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
});

for(const mode of ["wrong-node","unsafe","missing-reason"]){
  test("dependency inspector rejects "+mode+" evidence",async({page})=>{
    await setupDependencies(page,mode);await page.goto("/nodes");await login(page);
    await page.getByRole("button",{name:"Inspect node dependencies"}).click();
    await expect(page.getByText(/Counts are unavailable, not zero/)).toBeVisible();
    await expect(page.locator("[data-dependency]")).toHaveCount(0);
  });
}

async function setupNames(page: Page, mode: "lost" | "conflict" | "read-error" = "lost") {
  await setup(page);
  let name = "Original";
  let revision = 1;
  let reads = 0;
  const requests: Record<string, unknown>[] = [];
  let receipt: Record<string, unknown> | null = null;
  await page.route("**/api/v1/**", async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const node = { node_id: "lab-name", name, installation_id: "a".repeat(32), paired_at: "2026-10-20T12:00:00Z", status: "offline", metrics_current: false, last_seen: null };
    if (path === "/api/v1/workspace/nodes") return route.fulfill({json:{mode:"new_pro_lab_only",nodes:[node]}});
    if (path.endsWith("/lab-name/name")) {
      reads++;
      if (mode === "read-error") return route.fulfill({status:503,json:{error:"unavailable"}});
      return route.fulfill({json:{node,revision,audit_count:receipt ? 1 : 0,recent:receipt ? [receipt] : []}});
    }
    if (path.endsWith("/lab-name/rename")) {
      expect(req.headers()["x-csrf-token"]).toBe("a".repeat(64));
      const input = req.postDataJSON(); requests.push(input);
      if (mode === "conflict") { name="Other owner";revision=2;return route.fulfill({status:409,json:{error:"node_name_conflict"}}); }
      if (!receipt) {
        expect(input.expected_revision).toBe(1);
        receipt={...input,previous_name:name,revision:2,actor_id:"owner",at:"2026-10-20T12:01:00Z"};
        name=input.name;revision=2;
        return route.fulfill({status:503,json:{error:"node_name_outcome_unknown"}});
      }
      expect(input).toEqual(requests[0]);
      return route.fulfill({json:{receipt}});
    }
    return route.fallback();
  });
  return {requests, reads:()=>reads};
}

test("name editor retries a committed lost reply with the identical event and reads live state", async ({page}) => {
  const probe=await setupNames(page);
  await page.goto("/nodes");await login(page);
  await expect(page.getByRole("button",{name:"Manage display name"})).toBeVisible();
  expect(probe.reads()).toBe(0);
  await page.getByRole("button",{name:"Manage display name"}).click();
  await page.getByLabel("New display name").fill("Renamed");
  await page.getByRole("button",{name:"Save name",exact:true}).click();
  await expect(page.getByText(/Outcome is unknown/)).toBeVisible();
  await expect(page.getByLabel("New display name")).toBeDisabled();
  await page.getByRole("button",{name:"Retry same request"}).click();
  await expect(page.getByText("Receipt confirmed; current state refreshed.")).toBeVisible();
  await expect(page.getByLabel("New display name")).toHaveValue("Renamed");
  expect(probe.requests).toHaveLength(2);
  expect(probe.requests[0]).toEqual(probe.requests[1]);
  await expect(page.locator(".pro-node-name-editor li")).toHaveCount(1);
});

test("name revision conflict locks editing until explicit refresh", async ({page}) => {
  await setupNames(page,"conflict");await page.goto("/nodes");await login(page);
  await page.getByRole("button",{name:"Manage display name"}).click();
  await page.getByLabel("New display name").fill("Stale change");
  await page.getByRole("button",{name:"Save name",exact:true}).click();
  await expect(page.getByText(/Revision or event conflict/)).toBeVisible();
  await expect(page.getByRole("button",{name:"Save name",exact:true})).toBeDisabled();
  await page.getByRole("button",{name:"Read current name"}).click();
  await expect(page.getByLabel("New display name")).toHaveValue("Other owner");
  await expect(page.getByText("Revision: 2",{exact:true})).toBeVisible();
});

test("Persian name editor remains locked on unreadable state at 320px", async ({page}) => {
  const probe=await setupNames(page,"read-error");await page.setViewportSize({width:320,height:760});
  await page.goto("/nodes");await login(page);
  await page.getByRole("button",{name:"Switch language"}).click();
  await page.getByRole("button",{name:"مدیریت نام نمایشی"}).click();
  await expect(page.getByText("نام فعلی قابل دریافت نیست؛ ویرایش قفل است.")).toBeVisible();
  await expect(page.getByRole("button",{name:"ذخیرهٔ نام"})).toBeDisabled();
  expect(probe.requests).toHaveLength(0);
  const overflow = await page.evaluate(() => [...document.querySelectorAll("main *")].map(el => ({ tag: el.tagName, cls: el.className, width: el.getBoundingClientRect().width, left: el.getBoundingClientRect().left, right: el.getBoundingClientRect().right })).filter(el => el.left < -2 || el.right > innerWidth + 2));
  expect(overflow).toEqual([]);
});

async function setup(page: Page, role: "owner" | "representative" = "owner") {
  let signed = false;
  let invites = 0;
  let cancellations = 0;
  const paths: string[] = [];
  const user = role === "owner" ? owner : representative;
  const session = { authenticated: true, user, csrf_token: "a".repeat(64), expires_unix: 1900000000 };
  await page.route("**/api/v1/**", async route => {
    const req = route.request();const path = new URL(req.url()).pathname;
    paths.push(req.method() + " " + path);
    if (path.endsWith("/session/status")) return route.fulfill({ json: signed ? session : { enabled: true, authenticated: false } });
    if (path.endsWith("/session/login")) { signed = true; return route.fulfill({ json: session }); }
    if (!signed) return route.fulfill({ status: 401, json: { error: "session_required" } });
    if (path.endsWith("/fleet")) return route.fulfill({ json: {
      as_of: new Date().toISOString(), nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0,
      interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false,
      metric_scope: "default_route_interface_not_billable",
    } });
    if (req.method() === "GET" && path === "/api/v1/workspace/nodes") return route.fulfill({
      json: { mode: "new_pro_lab_only", nodes: [],
        installer_available: true, agent_transport_scope: "loopback_only" },
    });
    if (req.method() === "GET" && path === "/api/v1/workspace/nodes/installer-bundle") {
      return route.fulfill({ status: 200, contentType: "application/zip",
        headers: { "Content-Disposition": "attachment; filename=\"dark-pro-new-node-installer.zip\"" },
        body: "PK-test-only-isolated-bundle" });
    }
    if (req.method() === "POST" && path === "/api/v1/workspace/nodes/invites") {
      expect(req.headers()["x-csrf-token"]).toBe(session.csrf_token);
      expect(req.headers().origin).toBe("http://127.0.0.1:" + new URL(req.url()).port);
      const data = req.postDataJSON() as { node_id: string; name: string };
      expect(data.node_id).toBe("qa-new-node");
      invites += 1;
      return route.fulfill({ status: 201, json: {
        node_id: data.node_id, name: data.name, pair_code: pairCode,
        expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
      } });
    }
    if (req.method() === "POST" && path === "/api/v1/workspace/nodes/invites/qa-new-node/cancel") {
      expect(req.headers()["x-csrf-token"]).toBe(session.csrf_token);
      cancellations += 1;
      return route.fulfill({ json: { cancelled: true } });
    }
    if (path.startsWith("/api/v1/workspace/")) return route.fulfill({ json: {
      clients: [], groups: [], events: [], operations: [], balance: {},
    } });
    return route.fulfill({ status: 403, json: { error: "not_allowed" } });
  });
  return { paths, invites: () => invites, cancellations: () => cancellations };
}
async function login(page: Page) {
  await page.getByLabel("Username", { exact: true }).fill("new-pro-owner");
  await page.getByLabel("Password", { exact: true }).fill("test-secret-not-persisted");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
}

test("owner creates and revokes a one-use pairing ticket without storing it in browser", async ({ page }) => {
  const probe = await setup(page);
  await page.goto("/nodes");
  await login(page);
  await expect(page.getByRole("heading", { name: "Add a new PRO Node" })).toBeVisible();
  await page.getByLabel("New Node ID").fill("qa-new-node");
  await page.getByLabel("Display name").fill("QA node");
  await page.getByRole("button", { name: "Generate one-time Pair Code" }).click();
  await expect(page.locator(".pro-node-ticket-code")).toHaveText(pairCode);
  expect(probe.invites()).toBe(1);
  const storage = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }));
  expect(storage).not.toContain(pairCode);
  expect(storage).not.toContain("test-secret-not-persisted");
  await page.getByRole("button", { name: "Cancel unused code" }).click();
  await expect(page.getByText("Pending Pair Code revoked. No Node or client was changed.", { exact: true })).toBeVisible();
  await expect(page.locator(".pro-node-ticket-code")).toHaveCount(0);
  expect(probe.cancellations()).toBe(1);
  await page.getByRole("button", { name: "Generate one-time Pair Code" }).click();
  await expect(page.locator(".pro-node-ticket-code")).toHaveText(pairCode);
  await page.reload();
  await expect(page.getByRole("heading", { name: "Add a new PRO Node" })).toBeVisible();
  await expect(page.locator(".pro-node-ticket-code")).toHaveCount(0);
  await expect(page.getByText(/Pending Pair Code revoked/)).toHaveCount(0);
});

test("expired pairing secret is removed from the active enrollment view", async ({ page }) => {
  await page.clock.install({ time: new Date("2026-10-22T12:00:00Z") });
  const probe = await setup(page);
  await page.goto("/nodes");
  await login(page);
  await page.getByLabel("New Node ID").fill("qa-new-node");
  await page.getByLabel("Display name").fill("QA node");
  await page.getByRole("button", { name: "Generate one-time Pair Code" }).click();
  await expect(page.locator(".pro-node-ticket-code")).toHaveText(pairCode);
  await page.clock.fastForward(10 * 60_000 + 1_000);
  await expect(page.locator(".pro-node-ticket-code")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Copy Pair Code" })).toHaveCount(0);
  expect(probe.invites()).toBe(1);
});

test("representative cannot see or request Node management", async ({ page }) => {
  const probe = await setup(page, "representative");
  await page.goto("/nodes");
  await login(page);
  await expect(page).toHaveURL(/\/clients$/);
  await expect(page.locator(".pro-node-manager")).toHaveCount(0);
  expect(probe.paths.some(s => s.includes("/workspace/nodes"))).toBe(false);
});

test("Node enrollment form fits 320px Persian and does not expose fake live status", async ({ page }) => {
  const probe = await setup(page);
  await page.setViewportSize({ width: 320, height: 760 });
  await page.goto("/nodes");
  await login(page);
  await page.getByRole("button", { name: "Switch language" }).click();
  await expect(page.getByRole("heading", { name: "افزودن نود جدید PRO" })).toBeVisible();
  await expect(page.getByText("هنوز نود تازه‌ای Pair نشده است.")).toBeVisible();
  await page.getByLabel("شناسه نود جدید").fill("qa-new-node");
  await page.getByLabel("نام نمایشی").fill("آزمایشی");
  await page.getByRole("button", { name: "ساخت کد اتصال یک‌بارمصرف" }).click();
  await expect(page.locator(".pro-node-ticket-code")).toHaveText(pairCode);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
  expect(probe.invites()).toBe(1);
});


test("owner downloads verified installer before the short-lived ticket and never places Pair Code into a shell command",async({page})=>{
  const probe=await setup(page);
  await page.goto("/nodes");await login(page);
  const installer=page.getByRole("region",{name:"New Node installer · signed-in owner only"});
  await expect(installer).toBeVisible();
  await expect(installer.getByText(/loopback-only/)).toBeVisible();
  const download=page.waitForEvent("download");
  await installer.getByRole("button",{name:"Download checked Agent bundle"}).click();
  const file=await download;
  expect(file.suggestedFilename()).toBe("dark-pro-new-node-installer.zip");
  await page.getByLabel("New Node ID").fill("qa-new-node");
  await page.getByLabel("Display name").fill("Fresh QA only");
  await page.getByRole("button",{name:"Generate one-time Pair Code"}).click();
  await expect(page.locator(".pro-node-ticket-code")).toHaveText(pairCode);
  const command=await page.locator(".pro-node-command pre").innerText();
  expect(command).toContain("DARK_PRO_NODE_ID='qa-new-node'");
  expect(command).toContain("DARK_PRO_HUB_AGENT_URL='https://VERIFIED_AGENT_HOST:24443'");
  expect(command).toContain("DARK_PRO_NEW_NODE_ACK=I_CONFIRM_NEW_EMPTY_PRO_NODE");
  expect(command).not.toContain(pairCode);
  expect(command).not.toContain("agent_token");
  expect(probe.paths.some(p=>p==="GET /api/v1/workspace/nodes/installer-bundle")).toBe(true);
  await page.reload();
  await expect(page.locator(".pro-node-ticket-code")).toHaveCount(0);
  await expect(page.locator(".pro-node-command")).toHaveCount(0);
  const stored=await page.evaluate(()=>JSON.stringify({...localStorage,...sessionStorage}));
  expect(stored).not.toContain(pairCode);
});

test("fresh Node installer instructions and command do not overflow 320px Persian RTL",async({page})=>{
  await setup(page);
  await page.setViewportSize({width:320,height:760});
  await page.goto("/nodes");await login(page);
  await page.getByRole("button",{name:"Switch language"}).click();
  await expect(page.getByRole("region",{name:"نصب Agent نود جدید · فقط مالک"})).toBeVisible();
  await page.getByLabel("شناسه نود جدید").fill("qa-new-node");
  await page.getByLabel("نام نمایشی").fill("نود جدید");
  await page.getByRole("button",{name:"ساخت کد اتصال یک‌بارمصرف"}).click();
  await expect(page.getByRole("region",{name:"فقط داخل VPS جدید اجرا کن"})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
});
