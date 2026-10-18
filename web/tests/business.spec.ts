import { expect, test, type Page } from "@playwright/test";

const rep = { id: "rep-a", username: "rep-a", role: "representative", representative_id: "rep-a" };
const owner = { id: "owner", username: "proowner", role: "owner", representative_id: "owner" };
const noFleet = {
  as_of: new Date("2026-10-15T09:00:00Z").toISOString(),
  total_nodes: 0, online_nodes: 0, offline_nodes: 0,
  metric_scope: "default_route_interface_not_billable",
  interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false,
  nodes: [],
};
async function mockBase(page: Page) {
  await page.route("**/api/v1/fleet", (route) => route.fulfill({
    status: 200, contentType: "application/json", body: JSON.stringify(noFleet),
  }));
}
function successfulLogin(user: typeof rep) {
  return { authenticated: true, user, csrf_token: "a".repeat(64), expires_unix: 1800000000 };
}
async function mockBusiness(page: Page, user: typeof rep, seedClientCount=0) {
  let signedIn = false;
  const requests: { path: string; method: string; csrf: string; body: unknown }[] = [];
  const runningClients: unknown[] = Array.from({length:seedClientCount},(_,i)=>({
    id:"paged-client-"+String(i).padStart(3,"0"),
    name:"Member "+String(i).padStart(3,"0"),
    representative_id:user.representative_id,archived:false,
    deployment_state:"pending_node_apply",plan_kind:"volume",duration_days:30,
    volume_bytes:1024,ip_count:0,expires_unix:1800000000,
  }));
  const operations: unknown[] = [];
  const runningGroups: Array<{ id: string; representative_id: string; name: string; color: string; member_count: number; created_unix: number; updated_unix: number }> = [];
  const membership = new Map<string,string>();
  const updateCounts = () => {
    for (const group of runningGroups) {
      group.member_count = [...membership.values()].filter(id => id === group.id).length;
    }
  };
  await page.route("**/api/v1/session/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith("/status")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify(signedIn ? successfulLogin(user) : { enabled: true, authenticated: false }) });
    } else if (path.endsWith("/login")) {
      signedIn = true;
      const body = request.postDataJSON() as Record<string, unknown>;
      expect(body.password).toBe("test-private-password-for-lab");
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(successfulLogin(user)) });
    } else {
      await route.fulfill({ status: 404, body: "{}" });
    }
  });
  await page.route("**/api/v1/workspace/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (request.method() !== "GET") {
      const body: unknown = request.postDataJSON();
      requests.push({ path, method: request.method(), csrf: request.headers()["x-csrf-token"] ?? "", body });
      if (path.includes("/clients/") && path.endsWith("/extend")) {
        const n = body as { event_id: string; client_id: string; additional_days: number; additional_volume_bytes: number; reason: string };
        const c = runningClients.find((item) => (item as { id: string }).id === n.client_id) as Record<string, unknown> | undefined;
        if (c) {
          c.duration_days = Number(c.duration_days) + n.additional_days;
          c.expires_unix = Number(c.expires_unix) + n.additional_days * 86400;
          c.volume_bytes = Number(c.volume_bytes) + n.additional_volume_bytes;
        }
        operations.push({ event_id: n.event_id, client_id: n.client_id, representative_id: user.representative_id,
          kind: "extend", reason: n.reason, added_days: n.additional_days,
          added_volume_bytes: n.additional_volume_bytes, volume_credit_debit: n.additional_volume_bytes,
          unlimited_credit_debit: 5, created_unix: 1800000000 });
      } else if (path.includes("/clients/") && path.endsWith("/state")) {
        const n = body as { event_id: string; client_id: string; action: string; reason: string };
        const c = runningClients.find((item) => (item as { id: string }).id === n.client_id) as Record<string, unknown> | undefined;
        if (c) { c.archived = n.action === "archive"; c.deployment_state = n.action === "enable" ? "pending_node_apply" : "disabled"; }
        operations.push({ event_id: n.event_id, client_id: n.client_id, representative_id: user.representative_id,
          kind: n.action, reason: n.reason, added_days: 0,
          added_volume_bytes: 0, volume_credit_debit: 0, unlimited_credit_debit: 0, created_unix: 1800000000 });
      }
      if (path === "/api/v1/workspace/groups" && request.method() === "POST") {
        const n = body as { id: string; representative_id: string; name: string; color: string };
        runningGroups.push({ ...n, member_count: 0, created_unix: 0, updated_unix: 0 });
      } else if (path.endsWith("/members") && request.method() === "POST") {
        const groupId = path.split("/").at(-2)!;
        for (const id of (body as { client_ids: string[] }).client_ids) membership.set(id, groupId);
        updateCounts();
      } else if (path.includes("/members/") && request.method() === "DELETE") {
        const groupId = path.split("/").at(-3)!;
        const id = path.split("/").at(-1)!;
        if (membership.get(id) === groupId) membership.delete(id);
        updateCounts();
      } else if (path.includes("/groups/") && request.method() === "DELETE") {
        const groupId = path.split("/").at(-1);
        const i = runningGroups.findIndex((row) => row.id === groupId);
        if (i >= 0) runningGroups.splice(i, 1);
        for (const [client,owner] of membership) if (owner === groupId) membership.delete(client);
      } else if (path.includes("/groups/") && request.method() === "PATCH") {
        const groupId = path.split("/").at(-1);
        const g = runningGroups.find((row) => row.id === groupId);
        if (g) Object.assign(g, body);
      }
      if (path.endsWith("/clients")) {
        const n = body as Record<string, unknown>;
        runningClients.push({
          id: n.client_id, name: n.name, representative_id: n.representative_id,
          plan_kind: n.unlimited ? "unlimited" : "volume", ip_count: n.ip_count,
          volume_bytes: n.volume_bytes, duration_days: n.duration_days,
          charged_unlimited_units: 10, deployment_state: "pending_node_apply", archived: false,
          expires_unix: 1800000000,
        });
      }
      await route.fulfill({ status: request.method() === "POST" ? 201 : 200,
        contentType: "application/json", body: JSON.stringify(body) });
      return;
    }
    if (path.endsWith("/client-operations")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ operations }) });
    } else if (path.endsWith("/me")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ user, balance: {
          id: user.representative_id, display_name: "Visible Account", kind: user.role,
          enabled: true, volume_credit_bytes: 10737418240, unlimited_credit_units: 90,
        }, data_plane: "not_activated" }),
      });
    } else if (path.endsWith("/clients")) {
      const requested = new URL(request.url()).searchParams.get("representative_id");
      if (user.role !== "owner" && requested && requested !== user.representative_id) {
        return route.fulfill({status:403,json:{error:"representative_scope_denied"}});
      }
      const repID = user.role === "representative" ? user.representative_id : requested;
      const page = new URL(request.url()).searchParams;
      const limit = Number(page.get("limit") ?? 50),offset = Number(page.get("offset") ?? 0);
      const q=page.get("q")??"";
      const state=page.get("state")??"",plan=page.get("plan")??"",sort=page.get("sort")??"";
      const now=Date.now()/1000;
      const scoped = runningClients.filter(raw=>{
        const c=raw as {id:string;name:string;representative_id:string;plan_kind:string;
          archived:boolean;deployment_state:string;expires_unix:number};
        if(repID && c.representative_id!==repID)return false;
        if(q && !c.id.toLowerCase().includes(q.toLowerCase())&&!c.name.toLowerCase().includes(q.toLowerCase()))return false;
        if(plan && plan!=="all" && c.plan_kind!==plan)return false;
        if(state && state!=="all"){
          const current=c.archived?"archived":c.deployment_state==="disabled"?"disabled":
            c.expires_unix<=now?"expired":c.deployment_state==="pending_node_apply"?"pending":"other";
          if(current!==state)return false;
        }
        return true;
      }).slice().sort((a,b)=>{
        const x=a as {id:string;name:string;expires_unix:number};
        const y=b as {id:string;name:string;expires_unix:number};
        if(sort==="name")return x.name.localeCompare(y.name)||x.id.localeCompare(y.id);
        if(sort==="expiry_soon")return x.expires_unix-y.expires_unix||x.id.localeCompare(y.id);
        if(sort==="expiry_late")return y.expires_unix-x.expires_unix||x.id.localeCompare(y.id);
        if(sort==="oldest")return x.id.localeCompare(y.id);
        return x.id.localeCompare(y.id);
      });
      const selected=scoped.slice(offset,offset+limit);
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ clients:selected,total:scoped.length,
          limit,offset,q,state,plan,sort,representative_id:repID??"",
          has_more:offset+selected.length<scoped.length }) });
    } else if (/^\/api\/v1\/workspace\/groups\/[^/]+\/members$/.test(path)) {
      const groupId=path.split("/").at(-2)!;
      const group=runningGroups.find(g=>g.id===groupId);
      if (!group || user.role!=="owner" && group.representative_id!==user.representative_id)
        return route.fulfill({status:403,json:{error:"representative_scope_denied"}});
      const q=new URL(request.url()).searchParams;
      const limit=Number(q.get("limit")??50),offset=Number(q.get("offset")??0);
      const ids=[...membership.entries()].filter(([_,g])=>g===groupId).map(([id])=>id).sort();
      return route.fulfill({status:200,json:{client_ids:ids.slice(offset,offset+limit),limit,offset}});
    } else if (path.endsWith("/groups")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ groups: runningGroups }) });
    } else if (path.endsWith("/events")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ events: [{ event_id: "credit-test-123456789", kind: "credit_adjustment",
          representative_id: user.representative_id, object_id: "", reason: "Private grant",
          volume_delta_bytes: 10737418240, unlimited_delta_units: 100 }] }) });
    } else if (path.endsWith("/representatives")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ representatives: [
          { id: "owner", display_name: "Owner", kind: "owner", enabled: true, volume_credit_bytes: 0, unlimited_credit_units: 0 },
          { id: "rep-a", display_name: "Rep A", kind: "representative", enabled: true, volume_credit_bytes: 0, unlimited_credit_units: 90 },
        ] }) });
    } else {
      await route.fulfill({ status: 404, body: "{}" });
    }
  });
  return requests;
}
test("Business page never invents owner, representative or credit when backend is disabled", async ({ page }) => {
  await mockBase(page);
  await page.route("**/api/v1/session/status", (route) => route.fulfill({ status: 404, body: "{}" }));
  await page.goto("/clients");
  await expect(page.getByText("Sign-in service unavailable. No account or permissions are assumed.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Create representative" })).toHaveCount(0);
  await expect(page.getByText("90", { exact: true })).toHaveCount(0);
});

test("representative only sees own scoped business tools and submits CSRF-bound pending client", async ({ page }) => {
  await mockBase(page);
  const requests = await mockBusiness(page, rep);
  await page.setViewportSize({ width: 320, height: 750 });
  await page.goto("/clients");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await page.getByLabel("Username").fill("rep-a");
  await page.getByLabel("Password").fill("test-private-password-for-lab");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByText("Visible Account")).toBeHidden(); // not rendered from balance display name
  await expect(page.getByText("Scoped to your representative account.")).toBeVisible();
  await expect(page.getByText("90", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create representative" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Record credit transaction" })).toHaveCount(0);
  await page.locator(".business-client-form").getByLabel("Client ID").fill("sample-pro-rep");
  await page.getByLabel("Client name").fill("Private Test User");
  await page.getByLabel("Plan type").selectOption("unlimited");
  await page.getByLabel("Duration (days)").fill("60");
  await page.getByLabel("IP/User slots (0 = unlimited for volume)").fill("5");
  await page.getByRole("button", { name: "Create pending client" }).click();
  await expect(page.locator(".clients-explorer-row").filter({ hasText: "sample-pro-rep" }).getByText("Private Test User")).toBeVisible();
  expect(requests).toHaveLength(1);
  expect(requests[0].path).toBe("/api/v1/workspace/clients");
  expect(requests[0].csrf).toBe("a".repeat(64));
  const body = requests[0].body as Record<string, unknown>;
  expect(body.representative_id).toBe("rep-a");
  expect(body.duration_days).toBe(60);
  expect(body.ip_count).toBe(5);
  expect(body.unlimited).toBe(true);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2);
  expect(overflow).toBe(false);
});

test("owner gets real scoped representative forms in Persian without sending owner token to browser", async ({ page }) => {
  await mockBase(page);
  const requests = await mockBusiness(page, owner);
  await page.goto("/representatives");
  await page.getByLabel("Username").fill("proowner");
  await page.getByLabel("Password").fill("test-private-password-for-lab");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Create representative" })).toBeVisible();
  await page.getByRole("button", { name: "Switch language" }).click();
  await expect(page.getByRole("heading", { name: "ساخت نماینده" })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  const form = page.getByRole("heading", { name: "ساخت نماینده" }).locator("..");
  await form.getByLabel("شناسه نماینده").fill("rep-new");
  await form.getByLabel("نام نمایشی").fill("نماینده جدید");
  await form.getByRole("button", { name: "ساخت نماینده" }).click();
  expect(requests).toHaveLength(1);
  expect(requests[0].csrf).toBe("a".repeat(64));
  const body = requests[0].body as Record<string, unknown>;
  expect(body.id).toBe("rep-new");
  expect(body.name).toBe("نماینده جدید");
  const leaked = await page.evaluate(() => JSON.stringify(localStorage));
  expect(leaked).not.toContain("csrf_token");
  expect(leaked).not.toContain("test-private-password-for-lab");
});


test("owner can create and bulk-edit a real group in Persian then delete group without deleting clients", async ({ page }) => {
  await mockBase(page);
  const requests = await mockBusiness(page, owner);
  await page.setViewportSize({ width: 320, height: 750 });
  await page.goto("/groups");
  await page.getByLabel("Username").fill("proowner");
  await page.getByLabel("Password").fill("test-private-password-for-lab");
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.getByRole("button", { name: "Switch language" }).click();
  await expect(page.getByRole("heading", { name: "گروه‌های کلاینت‌ها" })).toBeVisible();
  const create = page.getByRole("heading", { name: "ساخت گروه" }).locator("..");
  await create.getByLabel("شناسه گروه").fill("group-new");
  await create.getByLabel("نام گروه").fill("وی آی پی");
  await create.getByLabel("رنگ (اختیاری #RRGGBB)").fill("#FF00AA");
  await create.getByRole("button", { name: "ساخت گروه" }).click();
  await expect(page.getByText("وی آی پی (group-new)")).toBeVisible();
  const assign = page.getByRole("heading", { name: "انتقال گروهی کلاینت‌ها" }).locator("..");
  await assign.getByLabel("شناسه گروه").fill("group-new");
  await assign.getByLabel("شناسه کلاینت‌ها (با کاما، حداکثر ۵۰۰)").fill("client-one,client-two");
  await assign.getByRole("button", { name: "انتقال گروهی کلاینت‌ها" }).click();
  await expect(page.getByRole("heading", { name: "گروه‌های کلاینت‌ها" }).locator("..").getByText("owner · 2 عضو")).toBeVisible();
  const groupList = page.getByRole("heading", { name: "گروه‌های کلاینت‌ها" }).locator("..");
  page.once("dialog", (dialog) => dialog.accept());
  await groupList.getByRole("button", { name: "حذف گروه" }).click();
  await expect(page.getByText("وی آی پی (group-new)")).toHaveCount(0);
  expect(requests.map((r) => r.path)).toEqual([
    "/api/v1/workspace/groups",
    "/api/v1/workspace/groups/group-new/members",
    "/api/v1/workspace/groups/group-new",
  ]);
  for (const request of requests) expect(request.csrf).toBe("a".repeat(64));
  const groupCreate = requests[0].body as Record<string, unknown>;
  expect(groupCreate.name).toBe("وی آی پی");
  const assignBody = requests[1].body as { client_ids: string[] };
  expect(assignBody.client_ids).toEqual(["client-one", "client-two"]);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2);
  expect(overflow).toBe(false);
});


test("representative can extend then archive a client without publishing Xray, altering groups or exposing other accounts", async ({ page }) => {
  await mockBase(page);
  const requests = await mockBusiness(page, rep);
  await page.goto("/clients");
  await page.getByLabel("Username").fill("rep-a");
  await page.getByLabel("Password").fill("test-private-password-for-lab");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByText("Scoped to your representative account.")).toBeVisible();
  await page.getByLabel("Client ID", { exact: false }).first().fill("test-lifecycle-user");
  await page.getByLabel("Client name").fill("Lifecycle test");
  await page.getByLabel("Plan type").selectOption("unlimited");
  await page.getByLabel("Duration (days)").fill("30");
  await page.getByLabel("IP/User slots (0 = unlimited for volume)").fill("5");
  await page.getByRole("button", { name: "Create pending client" }).click();
  await expect(page.locator(".clients-explorer-row").filter({ hasText: "test-lifecycle-user" }).getByText("Lifecycle test")).toBeVisible();
  await page.getByRole("button", { name: "Manage", exact: true }).click();
  await page.getByLabel("Additional days").fill("30");
  await page.getByLabel("Additional volume (GB)").fill("0");
  const renewForm = page.getByRole("heading", { name: "Extend / add volume" }).locator("..");
  await renewForm.getByLabel("Operation reason").fill("Renewing for 30 days");
  await renewForm.getByRole("button", { name: "Extend / add volume" }).click();
  await expect(page.getByText("Renewing for 30 days", { exact: false })).toBeVisible();
  const actionForm = page.getByRole("heading", { name: "Client action" }).locator("..");
  await actionForm.getByLabel("Client action").selectOption("archive");
  await actionForm.getByLabel("Operation reason").fill("Archived after expiry");
  page.once("dialog", (dialog) => dialog.accept());
  await actionForm.getByRole("button", { name: "Record state change" }).click();
  await expect(page.getByText("Archived after expiry", { exact: false })).toBeVisible();
  expect(requests.map((v) => v.path)).toEqual([
    "/api/v1/workspace/clients",
    "/api/v1/workspace/clients/test-lifecycle-user/extend",
    "/api/v1/workspace/clients/test-lifecycle-user/state",
  ]);
  for (const item of requests) expect(item.csrf).toBe("a".repeat(64));
  const renew = requests[1].body as Record<string, unknown>;
  expect(renew.additional_days).toBe(30);
  expect(renew.additional_volume_bytes).toBe(0);
  const archived = requests[2].body as Record<string, unknown>;
  expect(archived.action).toBe("archive");
  expect(archived.client_id).toBe("test-lifecycle-user");
  const xrayCalls = requests.filter((v) => v.path.includes("xray") || v.path.includes("telegram"));
  expect(xrayCalls).toHaveLength(0);
});


// This scoped picker is a user-driven convenience around EXISTING tenant
// guarded endpoints. It must neither touch credit nor assume a Node Apply.
test("owner moves only same-representative clients through safe picker then removes a member without deleting account",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,owner);
 await page.goto("/clients");
 await page.getByLabel("Username").fill("proowner");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();

 const create=page.getByRole("heading",{name:"Create client record"}).locator("..");
 await create.getByLabel("Client ID").fill("owner-isolated");
 await create.getByLabel("Client name").fill("Owner only");
 await create.getByRole("button",{name:"Create pending client"}).click();
 await expect(page.locator(".clients-explorer-row").filter({hasText:"owner-isolated"}).getByText("Owner only")).toBeVisible();

 await create.getByLabel("Client ID").fill("rep-isolated");
 await create.getByLabel("Client name").fill("Rep selected");
 await create.getByLabel("Representative ID").selectOption("rep-a");
 await create.getByRole("button",{name:"Create pending client"}).click();
 await expect(page.locator(".clients-explorer-row").filter({hasText:"rep-isolated"}).getByText("Rep selected")).toBeVisible();
 await page.goto("/groups");
 const groupForm=page.getByRole("heading",{name:"Create group"}).locator("..");
 await groupForm.getByLabel("Group ID").fill("rep-group-v2");
 await groupForm.getByLabel("Group name").fill("Rep group");
 await groupForm.getByLabel("Representative ID").selectOption("rep-a");
 await groupForm.getByRole("button",{name:"Create group"}).click();
 await expect(page.getByText("Rep group (rep-group-v2)")).toBeVisible();
 await page.getByRole("button",{name:"Manage members"}).click();
 const m=page.getByRole("region",{name:"Group membership manager"});
 await expect(m.getByRole("heading",{name:"Group members: Rep group"})).toBeVisible();
 await expect(m.getByText("Rep selected")).toBeVisible();
 await expect(m.getByText("Owner only")).toHaveCount(0);
 await expect(m.getByText(/Group ownership is enforced by the HUB/)).toBeVisible();

 await m.locator(".group-members-choice input").check();
 await expect(m.getByRole("button",{name:"Move selected clients to this group"})).toBeEnabled();
 page.once("dialog",dialog=>dialog.accept());
 await m.getByRole("button",{name:"Move selected clients to this group"}).click();
 await expect(m.locator(".group-members-existing-row").getByText("rep-isolated")).toBeVisible();
 await expect(m.getByText("1 members")).toBeVisible();
 page.once("dialog",dialog=>dialog.accept());
 await m.locator(".group-members-existing-row").getByRole("button",{name:"Remove membership"}).click();
 await expect(m.locator(".group-members-existing-row")).toHaveCount(0);
 await expect(m.getByText("0 members")).toBeVisible();
 const paths=requests.map(r=>r.path);
 expect(paths).toEqual([
  "/api/v1/workspace/clients",
  "/api/v1/workspace/clients",
  "/api/v1/workspace/groups",
  "/api/v1/workspace/groups/rep-group-v2/members",
  "/api/v1/workspace/groups/rep-group-v2/members/rep-isolated",
 ]);
 for(const r of requests)expect(r.csrf).toBe("a".repeat(64));
 expect(requests[3].body).toEqual({client_ids:["rep-isolated"]});
 expect(paths.filter(x=>/usage|xray|telegram|credits/.test(x))).toHaveLength(0);
});

test("mobile Persian group picker restores selected group and candidate page after browser refresh",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,owner,55);
 await page.setViewportSize({width:320,height:750});
 await page.goto("/groups");
 await page.getByLabel("Username").fill("proowner");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();
 await page.getByRole("button",{name:"Switch language"}).click();
 const form=page.getByRole("heading",{name:"ساخت گروه"}).locator("..");
 await form.getByLabel("شناسه گروه").fill("owners-paged-v2");
 await form.getByLabel("نام گروه").fill("گروه تست");
 await form.getByRole("button",{name:"ساخت گروه"}).click();
 await page.getByRole("button",{name:"مدیریت اعضا"}).click();
 const panel=page.getByRole("region",{name:"Group membership manager"});
 await expect(panel.getByRole("heading",{name:"مدیریت اعضای گروه: گروه تست"})).toBeVisible();
 await expect(panel.getByText("Member 000")).toBeVisible();
 await panel.locator(".group-members-column").first().getByRole("button",{name:"بعدی"}).click();
 await expect(panel.getByText("Member 054")).toBeVisible();
 await expect(panel.getByText("Member 000")).toHaveCount(0);
 await page.reload();
 await expect(panel.getByRole("heading",{name:"مدیریت اعضای گروه: گروه تست"})).toBeVisible();
 await expect(panel.getByText("Member 054")).toBeVisible();
 await expect(panel.getByText("Member 000")).toHaveCount(0);
 await expect(panel.getByRole("checkbox")).toHaveCount(5);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(requests).toHaveLength(1); // Only group create; selection & navigation are read-only
});

test("group picker fails closed when membership lookup fails and sends no mutation",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,rep,1);
 await page.goto("/groups");
 await page.getByLabel("Username").fill("rep-a");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();
 const form=page.getByRole("heading",{name:"Create group"}).locator("..");
 await form.getByLabel("Group ID").fill("rep-offline-v2");
 await form.getByLabel("Group name").fill("Offline safe");
 await form.getByRole("button",{name:"Create group"}).click();
 await page.route("**/api/v1/workspace/groups/rep-offline-v2/members?*",r=>r.fulfill({status:503,json:{error:"unavailable"}}));
 await page.getByRole("button",{name:"Manage members"}).click();
 const panel=page.getByRole("region",{name:"Group membership manager"});
 await expect(panel.getByRole("alert")).toContainText("Membership data unavailable");
 await expect(panel.getByRole("button",{name:"Move selected clients to this group"})).toBeDisabled();
 await expect(panel.locator(".group-members-existing-row")).toHaveCount(0);
 expect(requests).toHaveLength(1);
});

// Selecting a representative is a local form action only; it must not
// grant quota, create a session or leak a password until owner submits.
test("owner representative cards prefill credit and login targets without creating credit transactions",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,owner);
 await page.goto("/representatives");
 await page.getByLabel("Username").fill("proowner");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();
 const credits=page.getByRole("heading",{name:"Adjust representative credit"}).locator("..");
 const login=page.getByRole("heading",{name:"Set representative login"}).locator("..");
 await page.getByRole("button",{name:"Select for credit"}).click();
 await expect(credits.getByLabel("Representative ID")).toHaveValue("rep-a");
 await page.getByRole("button",{name:"Select for login"}).click();
 await expect(login.getByLabel("Representative ID")).toHaveValue("rep-a");
 expect(requests).toHaveLength(0);
 await page.getByRole("button",{name:"Switch language"}).click();
 await page.getByRole("button",{name:"انتخاب برای اعتبار"}).click();
 await expect(page.getByRole("heading",{name:"تغییر اعتبار نماینده"}).locator("..")
  .getByLabel("شناسه نماینده")).toHaveValue("rep-a");
 expect(requests).toHaveLength(0);
});

test("client plan quick presets match HUB debit and never submit until representative confirms",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,rep);
 await page.goto("/clients");
 await page.getByLabel("Username").fill("rep-a");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();
 const form=page.getByRole("heading",{name:"Create client record"}).locator("..");
 const builder=page.getByRole("region",{name:"Plan builder and credit preview"});
 await expect(builder.getByText("10 GB volume credit",{exact:true})).toBeVisible();
 await builder.getByRole("button",{name:"Unlimited IPs"}).click();
 await expect(form.getByLabel("IP/User slots (0 = unlimited for volume)")).toHaveValue("0");
 await form.getByLabel("Plan type").selectOption("unlimited");
 await expect(builder.getByRole("button",{name:"Unlimited IPs"})).toHaveCount(0);
 await expect(builder.getByText("Enter valid duration, user limit and volume to calculate an estimate.")).toBeVisible();
 await builder.getByRole("button",{name:"60 days"}).click();
 await builder.getByRole("button",{name:"5",exact:true}).click();
 await expect(builder.getByText("10 unlimited units",{exact:true})).toBeVisible();
 await expect(builder.getByText(/Last loaded balance: 90 unlimited units/)).toBeVisible();
 await builder.getByRole("button",{name:"30 days"}).click();
 await builder.getByRole("button",{name:"2",exact:true}).click();
 await expect(builder.getByText("2 unlimited units",{exact:true})).toBeVisible();
 await form.getByLabel("Duration (days)").fill("45");
 await builder.getByRole("button",{name:"3",exact:true}).click();
 await expect(builder.getByText("5 unlimited units",{exact:true})).toBeVisible();
 expect(requests).toHaveLength(0);
 await builder.getByRole("button",{name:"60 days"}).click();
 await builder.getByRole("button",{name:"5",exact:true}).click();
 await form.getByLabel("Client ID").fill("rep-test-plan-v3");
 await form.getByLabel("Client name").fill("Custom name");
 await form.getByRole("button",{name:"Create pending client"}).click();
 await expect(page.getByRole("status").filter({hasText:"Saved in private PRO database"})).toBeVisible();
 expect(requests).toHaveLength(1);
 const payload=requests[0].body as Record<string,unknown>;
 expect(payload).toMatchObject({client_id:"rep-test-plan-v3",name:"Custom name",
  representative_id:"rep-a",duration_days:60,ip_count:5,volume_bytes:0,unlimited:true});
 expect(requests[0].csrf).toBe("a".repeat(64));
 expect(requests[0].path).toBe("/api/v1/workspace/clients");
});

test("client explorer searches all scoped clients from server, selects client and preserves pagination",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,rep,55);
 await page.goto("/clients");
 await page.getByLabel("Username").fill("rep-a");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();
 const list=page.getByRole("region",{name:"Client directory"});
 await expect(list.locator(".clients-explorer-row")).toHaveCount(50);
 await expect(list.getByText("Search, filter and sort across every client",{exact:false})).toBeVisible();
 await list.getByPlaceholder("Search all clients by name or ID").fill("Member 001");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(1);
 await expect(list.getByText("Member 001")).toBeVisible();
 await list.getByPlaceholder("Search all clients by name or ID").fill("");
 await list.getByRole("button",{name:"Next"}).click();
 await expect(list.locator(".clients-explorer-row")).toHaveCount(5);
 await expect(list.getByText("Member 054")).toBeVisible();
 await expect(list.getByText("Member 000")).toHaveCount(0);
 await list.locator(".clients-explorer-row").filter({hasText:"Member 054"})
  .getByRole("button",{name:"Manage"}).click();
 await expect(page.getByLabel("Choose an existing client")).toHaveValue("paged-client-054");
 await page.reload();
 await expect(list.getByText("Member 054")).toBeVisible();
 await expect(list.locator(".clients-explorer-row")).toHaveCount(5);
 expect(requests).toHaveLength(0);
});

test("representative client directory fails closed on a cross-account or malformed API page",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,rep,3);
 await page.route("**/api/v1/workspace/clients?*",route=>route.fulfill({
  json:{clients:[{
    id:"other-rep-secret",name:"DO NOT DISCLOSE",representative_id:"not-rep-a",
    archived:false,deployment_state:"pending_node_apply",ip_count:1,
    duration_days:30,plan_kind:"volume",volume_bytes:1073741824,expires_unix:1800000000,
  }]}
 }));
 await page.goto("/clients");
 await page.getByLabel("Username").fill("rep-a");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();
 const list=page.getByRole("region",{name:"Client directory"});
 await expect(list.getByText("DO NOT DISCLOSE")).toHaveCount(0);
 await expect(page.getByText("other-rep-secret")).toHaveCount(0);
 await expect(list.locator(".clients-explorer-row")).toHaveCount(0);
 await expect(page.getByRole("button",{name:"Extend / add volume"})).toBeDisabled();
 expect(requests).toHaveLength(0);
});

test("320px Persian sale presets and current-page client status filters avoid fake active traffic",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,rep,3);
 await page.setViewportSize({width:320,height:770});
 await page.goto("/clients");
 await page.getByLabel("Username").fill("rep-a");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();
 await page.getByRole("button",{name:"Switch language"}).click();
 const preview=page.getByRole("region",{name:"Plan builder and credit preview"});
 await expect(preview.getByRole("heading",{name:"ساخت سریع پلن"})).toBeVisible();
 await preview.getByRole("button",{name:"90 روز"}).click();
 const list=page.getByRole("region",{name:"Client directory"});
 await expect(list.getByRole("heading",{name:"فهرست کلاینت‌ها"})).toBeVisible();
 await list.getByLabel("وضعیت").selectOption("archived");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(0);
 await list.getByLabel("وضعیت").selectOption("pending");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(3);
 await expect(list.getByText("IP نامحدود").first()).toBeVisible();
 await expect(list.getByText("مصرف",{exact:true})).toHaveCount(0);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
 expect(requests).toHaveLength(0);
});

test("global directory searches clients outside loaded page and supports 25/50/100/200 server page sizes",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,rep,125);
 await page.goto("/clients");
 await page.getByLabel("Username").fill("rep-a");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();
 const list=page.getByRole("region",{name:"Client directory"});
 await expect(list.locator(".clients-explorer-row")).toHaveCount(50);
 await expect(list.getByText("Matches: 125")).toBeVisible();
 await list.getByPlaceholder("Search all clients by name or ID").fill("Member 120");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(1);
 await expect(list.getByText("Member 120")).toBeVisible();
 await expect(list.getByText("Matches: 1")).toBeVisible();
 await list.getByPlaceholder("Search all clients by name or ID").fill("");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(50);
 await list.getByLabel("Per page").selectOption("200");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(125);
 await expect(list.getByText("Member 124")).toBeVisible();
 await expect(list.getByRole("button",{name:"Next"})).toBeDisabled();
 await list.getByLabel("Per page").selectOption("25");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(25);
 await list.getByRole("button",{name:"Next"}).click();
 await expect(list.getByText("Page: 2")).toBeVisible();
 await expect(list.getByText("Member 025")).toBeVisible();
 await page.reload();
 await expect(list.getByText("Page: 2")).toBeVisible();
 await expect(list.locator(".clients-explorer-row")).toHaveCount(25);
 await expect(list.getByLabel("Per page")).toHaveValue("25");
 const storage=await page.evaluate(()=>JSON.stringify({...sessionStorage}));
 expect(storage).not.toContain("Member 120");
 expect(storage).not.toContain("a".repeat(64));
 expect(requests).toHaveLength(0);
});

test("owner can search/filter across representatives without cross-representative writes",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,owner,58);
 await page.goto("/clients");
 await page.getByLabel("Username").fill("proowner");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();
 const form=page.getByRole("heading",{name:"Create client record"}).locator("..");
 await form.getByLabel("Client ID").fill("other-qa-customer");
 await form.getByLabel("Client name").fill("Remote verified customer");
 await form.getByLabel("Representative ID").selectOption("rep-a");
 await form.getByRole("button",{name:"Create pending client"}).click();
 const list=page.getByRole("region",{name:"Client directory"});
 await expect(list.getByText("Matches: 59")).toBeVisible();
 await list.getByLabel("Representative ID (owner, optional)").fill("rep-a");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(1);
 await expect(list.getByText("Remote verified customer")).toBeVisible();
 await list.getByLabel("Sort").selectOption("name");
 await expect(list.getByText("Matches: 1")).toBeVisible();
 await list.getByLabel("Plan").selectOption("unlimited");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(0);
 await expect(list.getByText("Matches: 0")).toBeVisible();
 await list.getByLabel("Plan").selectOption("volume");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(1);
 await list.getByLabel("Status").selectOption("archived");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(0);
 await list.getByLabel("Status").selectOption("pending");
 await expect(list.locator(".clients-explorer-row")).toHaveCount(1);
 expect(requests).toHaveLength(1);
 expect(requests[0].path).toBe("/api/v1/workspace/clients");
});

test("global directory prevents stale actions when second page is malformed",async({page})=>{
 await mockBase(page);
 const requests=await mockBusiness(page,rep,60);
 await page.route("**/api/v1/workspace/clients?*",route=>{
  const params=new URL(route.request().url()).searchParams;
  if(params.get("offset")==="50"){
   return route.fulfill({json:{clients:[{
    id:"untrusted-owner-client",name:"PRIVATE OWNER",representative_id:"owner",
    archived:false,deployment_state:"pending_node_apply",plan_kind:"volume",ip_count:0,
    volume_bytes:1024,duration_days:30,expires_unix:1800000000
   }],total:60,limit:50,offset:50,q:"",state:"all",plan:"all",sort:"newest",
    representative_id:"rep-a",has_more:false}});
  }
  return route.fallback();
 });
 await page.goto("/clients");
 await page.getByLabel("Username").fill("rep-a");
 await page.getByLabel("Password").fill("test-private-password-for-lab");
 await page.getByRole("button",{name:"Sign in"}).click();
 const list=page.getByRole("region",{name:"Client directory"});
 await expect(list.locator(".clients-explorer-row")).toHaveCount(50);
 await list.getByRole("button",{name:"Next"}).click();
 await expect(list.getByText("PRIVATE OWNER")).toHaveCount(0);
 await expect(list.locator(".clients-explorer-row")).toHaveCount(0);
 await expect(page.getByRole("button",{name:"Extend / add volume"})).toBeDisabled();
 await expect(page.getByText(/Business data unavailable or unverified/)).toBeVisible();
 expect(requests).toHaveLength(0);
});
