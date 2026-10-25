import { expect, test, type Page } from "@playwright/test";
import type { CreationPlan } from "../src/InboundCreationModel";
import { readExecutionState, targetEvidence, type ExecutionCommand, type ExecutionOperation, type ExecutionPreview, type ExecutionReconciliation, type ExecutionState } from "../src/inboundExecution";

// Synthetic browser contract coverage. Actual listeners, traffic and usage are
// covered by the managed Go/Agent acceptance harness, not these route fixtures.
const id = 8197, csrf = "a".repeat(64), installation = "1".repeat(32), digest = "b".repeat(64);
const base = `/api/v1/workspace/native-inbounds/${id}/execution`, plans = "/api/v1/workspace/native-inbound-plans";
const nodes = [{ node_id: "hub-local", installation_id: installation, name: "HUB — this server", kind: "hub" }, { node_id: "remote", installation_id: "2".repeat(32), name: "Remote Node", kind: "node" }];
function savedPlan(): CreationPlan {
  return { profile: { id, name: "Managed inbound", revision: 1, enabled: false, archived: false, bound_clients: 0, transport: "tcp", grpc_service: "", reality_target: "example.org:443", server_names: ["example.org"], short_ids: ["aabb"], public_key: "U".repeat(43),
    spec: { version: 1, protocol: "vless", network: "tcp", security: "reality", settings: {}, stream: {}, tls: {}, reality: { target: "example.org:443", serverNames: ["example.org"], shortIds: ["aabb"] }, client: { publicKey: "U".repeat(43) } }, spec_secret_paths: ["settings.decryption", "reality.privateKey"] },
    nodes: [{ node_id: "hub-local", installation_id: installation, listen_ip: "127.0.0.1", direct_port: 19667, direct_hosts: ["127.0.0.1"], tunnel_port: 0, tunnel_hosts: [], swap_enabled: false, swap_source_node_id: "", swap_source_installation_id: "", swap_source_listen_ip: "", swap_source_port: 0, swap_entry_address: "", swap_entry_port: 0, swap_destination_port: 0 }],
    plan_revision: 1, inbound_revision: 1, status: "SAVED_DISABLED", runtime_apply: false, editable: true, created_unix: 1800000000, updated_unix: 1800000000, last_event_id: "saved-managed-fixture-0001", reason: "Saved setup" };
}
function freshState(): ExecutionState {
  return { version: 1, enabled: true, inbound_id: id, revision: 1, plan_revision: 1, inbound_revision: 1, desired_revision: 0, active_revision: 0, desired_state: "stopped", observed_state: "saved", accounting_mode: "per_client", allowed_accounting_modes: ["per_client"], client_ids: ["client-a"], effective_client_ids: [], allowed_actions: ["activate", "archive"], blockers: [], server_time_unix_milli: Date.now(), publication_state: "withheld",
    targets: [{ node_id: "hub-local", installation_id: installation, desired_generation: 0, observed_generation: 0, state: "saved", listeners: [{ inbound_id: id, tag: "managed-test", engine: "xray", protocol: "vless", transport: "tcp", security: "reality", kind: "direct", listen: "127.0.0.1", port: 19667, network: "tcp", resource_kind: "socket", accounting_mode: "per_client" }], blockers: [], process_ready: false, health_state: "unknown", accounting_state: "pending", reported_at_unix_nano: 0, lease_expires_unix_nano: 0 }] };
}
function uncertainRuntime(): ExecutionReconciliation {
  return { epoch_id: "e".repeat(32), node_id: "hub-local", installation_id: installation, generation: 3, revision: 7, state: "uncertain", config_sha: digest, manifest_sha: "c".repeat(64), process_identity_sha: "d".repeat(64), unknown_usage_policy: "unmeasured", allowed_actions: ["accept_owner_loss"], blockers: [], has_measured_usage: true, affected_client_ids: ["client-a"], affected_inbound_ids: [id, id + 1], known_usage: [
    { principal_id: "customer-a", inbound_id: id, client_id: "client-a", role: "customer", upload: "9007199254740993", download: "24", held: true },
    { principal_id: "owner-b", inbound_id: id + 1, client_id: "", role: "owner_aggregate", upload: "12", download: "15", held: false },
    { principal_id: "transit-c", inbound_id: id + 1, client_id: "", role: "transit", upload: "90", download: "90", held: false },
  ] };
}
async function fixture(page: Page, options: { language?: "en" | "fa"; theme?: "light" | "dark"; tamper?: boolean; batch?: boolean; reconciliation?: boolean; executionEnabled?: boolean } = {}) {
  const plan = savedPlan(), state = freshState();
  const mode = { failure: "" as "" | "before_commit" | "after_commit", freshReport: true, lookup: true, reconciliationFailure: "", reconciliationLookup: true };
  const reconciliationReceipts = new Map<string, any>();
  if (options.reconciliation) { state.reconciliations = [uncertainRuntime()]; state.observed_state = "failed"; state.allowed_actions = []; }
  const writes: Array<{ path: string; raw: string; body: any; csrf: string }> = [], reads: string[] = [];
  const clients = [{ id: "client-a", name: "Client A", archived: false, deployment_state: "pending_node_apply", expires_unix: 1900000000 }, { id: "client-b", name: "Client B", archived: false, deployment_state: "pending_node_apply", expires_unix: 1900000000 }, { id: "client-disabled", name: "Disabled client", archived: false, deployment_state: "disabled", expires_unix: 1900000000 }];
  const copy = <T,>(value: T): T => structuredClone(value);
  function makeActive() {
    state.desired_state = "active"; state.observed_state = "active"; state.desired_revision ||= 1; state.active_revision = state.desired_revision; state.effective_client_ids = [...state.client_ids]; state.allowed_actions = ["apply", "stop", "restart", "archive"];
    Object.assign(state.targets[0], { state: "active", desired_generation: 1, observed_generation: 1, process_ready: true, health_state: "verified", accounting_state: "admitted", reported_at_unix_nano: Date.now() * 1e6, lease_expires_unix_nano: (Date.now() + 30000) * 1e6 });
  }
  await page.addInitScript(({ language, theme }) => { localStorage.setItem("dark-pro-lang", language); localStorage.setItem("dark-pro-theme", theme); }, { language: options.language ?? "en", theme: options.theme ?? "dark" });
  await page.route("**/api/v1/session/status", route => route.fulfill({ json: { authenticated: true, user: { id: "owner", role: "owner", username: "synthetic-owner", representative_id: "owner" }, csrf_token: csrf, expires_unix: 1900000000 } }));
  await page.route("**/api/v1/fleet", route => route.fulfill({ json: { nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0, as_of: new Date().toISOString(), interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false, metric_scope: "default_route_interface_not_billable" } }));
  await page.route("**/api/v1/workspace/**", async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    if (request.method() === "GET") {
      reads.push(path);
      if (path === "/api/v1/workspace/native-inbounds-state") return route.fulfill({ json: { inbounds: [plan.profile], nodes, targets: [], runtime: [], ...(options.executionEnabled === undefined ? {} : { execution_enabled: options.executionEnabled }), ...(options.batch ? { executions: [state] } : {}) } });
      if (path === plans + "/capabilities") return route.fulfill({ json: { available: true, version: 4, authoring_spec: true, protocols: ["vless"], transports: ["tcp"], securities: ["reality"], max_nodes: 64, max_hosts_per_node: 64, runtime_apply: false, plans: [{ inbound_id: id, plan_revision: plan.plan_revision, inbound_revision: plan.inbound_revision, status: plan.status, node_ids: plan.nodes.map(node => node.node_id) }] } });
      if (path === plans + "/" + id) return route.fulfill({ json: { plan, references_current: true } });
      if (path === base) {
        state.server_time_unix_milli = Date.now();
        if (state.targets[0].process_ready && mode.freshReport) { state.targets[0].reported_at_unix_nano = Date.now() * 1e6; state.targets[0].lease_expires_unix_nano = (Date.now() + 30000) * 1e6; }
        return route.fulfill({ json: options.tamper ? { ...state, private_key: "must-not-render" } : state });
      }
      if (path.startsWith(base + "/operations/")) return state.latest_operation && mode.lookup ? route.fulfill({ json: state.latest_operation }) : route.fulfill({ status: 404, json: { error: "not_found" } });
      if (path.startsWith(base + "/reconciliations/")) {
        const receipt = reconciliationReceipts.get(path.split("/").at(-1)!);
        return receipt && mode.reconciliationLookup ? route.fulfill({ json: receipt }) : route.fulfill({ status: 404, json: { error: "not_found" } });
      }
      if (path === "/api/v1/workspace/clients") return route.fulfill({ json: { clients, limit: 25, offset: Number(url.searchParams.get("offset")), has_more: false } });
      if (path.endsWith("/operations")) return route.fulfill({ json: { operations: [] } });
      return route.fulfill({ status: 404, json: { error: "not_found" } });
    }
    const body = request.postDataJSON(); writes.push({ path, raw: request.postData()!, body, csrf: request.headers()["x-csrf-token"] ?? "" });
    if (path === base + "/reconciliations/preview") {
      const entry = state.reconciliations?.find(entry => entry.epoch_id === body.epoch_id);
      return entry && entry.revision === body.expected_revision ? route.fulfill({ json: { valid: true, review_sha: digest, state: copy(entry), blockers: [] } }) : route.fulfill({ status: 409, json: { error: "conflict" } });
    }
    if (path === base + "/reconciliations") {
      if (mode.reconciliationFailure === "rejected") return route.fulfill({ status: 409, json: { error: "conflict" } });
      const prior = reconciliationReceipts.get(body.event_id);
      if (prior) return route.fulfill({ json: { ...prior, replayed: true } });
      const entry = state.reconciliations?.find(entry => entry.epoch_id === body.epoch_id);
      if (!entry || entry.revision !== body.expected_revision || body.review_sha !== digest) return route.fulfill({ status: 409, json: { error: "conflict" } });
      const receipt = { event_id: body.event_id, epoch_id: entry.epoch_id, inbound_id: id, node_id: entry.node_id, installation_id: entry.installation_id, generation: entry.generation, actor_id: "owner", reason: body.reason, revision: entry.revision,
        state: "accepted_owner_loss", unknown_usage_policy: "owner_loss", known_usage: copy(entry.known_usage), affected_client_ids: copy(entry.affected_client_ids), affected_inbound_ids: copy(entry.affected_inbound_ids), has_measured_usage: entry.has_measured_usage, created_unix_nano: Date.now() * 1e6, replayed: false };
      reconciliationReceipts.set(body.event_id, receipt); state.reconciliations = []; state.allowed_actions = ["activate"];
      return mode.reconciliationFailure === "after_commit" ? route.fulfill({ status: 500, json: { error: "response_lost" } }) : route.fulfill({ json: receipt });
    }
    if (path === base + "/preview") {
      const preview: ExecutionPreview = { valid: true, review_sha: digest, action: body.action, state: copy(state), targets: copy(state.targets), blockers: [] };
      return route.fulfill({ json: preview });
    }
    if (path === base + "/clients") {
      if (body.expected_revision !== state.revision) return route.fulfill({ status: 409, json: { error: "revision_conflict" } });
      state.revision++; state.client_ids = [...body.client_ids]; state.accounting_mode = body.accounting_mode;
      return route.fulfill({ json: state });
    }
    if (path === base + "/commands") {
      if (mode.failure === "before_commit") return route.fulfill({ status: 503, json: { error: "response_lost" } });
      if (!state.latest_operation || state.latest_operation.event_id !== body.event_id) {
        state.revision = body.expected_revision + 1; state.desired_revision = state.revision; state.desired_state = body.action === "archive" ? "archived" : body.action === "stop" ? "stopped" : "active"; state.observed_state = "queued";
        state.targets[0].state = "queued"; state.targets[0].desired_generation++; state.allowed_actions = ["stop"];
        state.latest_operation = { id: body.event_id, event_id: body.event_id, inbound_id: id, action: body.action, revision: state.revision, state: "queued", created_unix_nano: Date.now() * 1e6, updated_unix_nano: Date.now() * 1e6, targets: copy(state.targets), blockers: [], replayed: false };
      }
      return mode.failure === "after_commit" ? route.fulfill({ status: 500, json: { error: "response_lost" } }) : route.fulfill({ status: 202, json: state.latest_operation });
    }
    return route.fulfill({ status: 405, json: { error: "unexpected_mutation" } });
  });
  return { state, plan, mode, writes, reads, makeActive };
}
const region = (page: Page) => page.getByRole("region", { name: "Inbound execution", exact: true });
async function open(page: Page) { await page.goto(`/inbounds?inbound=${id}&tab=overview`); await expect(region(page)).toBeVisible(); }
async function review(page: Page, action = "Activate") {
  await region(page).getByRole("button", { name: action, exact: true }).click();
  const dialog = page.getByRole("dialog", { name: `Review execution · ${action}`, exact: true });
  await expect(dialog.getByRole("button", { name: "Confirm and continue", exact: true })).toBeEnabled(); return dialog;
}

test("admission stays queued until exact target evidence is observed; HUB is explicit and legacy controls stay hidden", async ({ page }) => {
  const f = await fixture(page); await open(page);
  await expect(region(page).locator('[data-execution-target="hub-local"]')).toHaveCount(1);
  await expect(region(page).locator('[data-execution-target="remote"]')).toHaveCount(0);
  await expect(page.getByRole("region", { name: "Inbound LAB preparation" })).toHaveCount(0);
  const dialog = await review(page); await dialog.getByRole("button", { name: "Confirm and continue" }).click();
  await expect(region(page).locator('[data-managed-state="queued"]')).toBeVisible();
  expect(f.writes.map(item => item.path)).toEqual([base + "/preview", base + "/commands"]);
  expect(f.writes[1].body).toEqual({ ...f.writes[0].body, review_sha: digest });
  expect(f.writes.every(item => item.csrf === csrf)).toBe(true);
  f.makeActive(); f.state.targets[0].health_state = "unknown";
  await region(page).getByRole("button", { name: "Refresh state" }).click();
  await expect(region(page).locator('[data-managed-state="verifying"]')).toBeVisible();
  f.state.targets[0].health_state = "engine_ready"; f.state.publication_state = "available_unverified_path"; await region(page).getByRole("button", { name: "Refresh state" }).click();
  await expect(region(page).locator('[data-managed-state="running"]')).toBeVisible();
  await expect(region(page).getByText("Engine ready", { exact: true })).toBeVisible();
  await expect(region(page).getByText("Available; connection not yet tested", { exact: true })).toBeVisible();
  f.state.targets[0].health_state = "verified"; await region(page).getByRole("button", { name: "Refresh state" }).click();
  await expect(region(page).locator('[data-managed-state="active"]')).toBeVisible();
  expect(f.reads.some(path => path.includes("/promotion") || path.includes("metered-route-transactions"))).toBe(false);
});

test("saved client changes retain effective clients until apply; eligibility prevents adding disabled clients", async ({ page }) => {
  const f = await fixture(page); f.makeActive(); await open(page);
  await page.getByRole("tab", { name: "Clients", exact: true }).click();
  const clients = page.getByRole("region", { name: "Client access", exact: true });
  await expect(clients.getByLabel("Selected: client-disabled", { exact: true })).toBeDisabled();
  await clients.getByLabel("Selected: client-a", { exact: true }).uncheck();
  await clients.getByLabel("Selected: client-b", { exact: true }).check();
  await expect(clients.getByText("Removal awaits apply", { exact: true }).first()).toBeVisible();
  await clients.getByRole("button", { name: "Save selection", exact: true }).click();
  await expect(page.getByText("Client selection saved. Review and apply it from Overview.", { exact: true })).toBeVisible();
  expect(f.state.client_ids).toEqual(["client-b"]); expect(f.state.effective_client_ids).toEqual(["client-a"]);
  expect(f.writes.map(item => item.path)).toEqual([base + "/clients"]);
  await page.getByRole("tab", { name: "Overview", exact: true }).click();
  await expect(region(page).getByText("Unapplied changes", { exact: true })).toBeVisible();
  const dialog = await review(page, "Apply changes"); await dialog.getByRole("button", { name: "Confirm and continue" }).click();
  expect(f.writes.at(-1)?.body.client_ids).toEqual(["client-b"]);
});

test("unknown response survives reload and retries only the exact reviewed request", async ({ page }) => {
  const f = await fixture(page); f.mode.failure = "before_commit"; await open(page);
  const dialog = await review(page); await dialog.getByRole("button", { name: "Confirm and continue" }).click();
  await expect(page.getByRole("heading", { name: "Unconfirmed request" })).toBeVisible();
  const original = f.writes.find(item => item.path === base + "/commands")!.raw;
  await page.reload(); await expect(page.getByRole("heading", { name: "Unconfirmed request" })).toBeVisible();
  expect(f.writes.filter(item => item.path === base + "/commands")).toHaveLength(1);
  f.mode.failure = ""; await page.getByRole("button", { name: "Retry the same request", exact: true }).click();
  await expect(region(page).locator('[data-managed-state="queued"]')).toBeVisible();
  expect(f.writes.filter(item => item.path === base + "/commands").map(item => item.raw)).toEqual([original, original]);
  expect(await page.evaluate(() => Object.keys(sessionStorage).filter(key => key.startsWith("dark-pro-inbound-execution:")).length)).toBe(0);
});

test("expired evidence cannot stay ready and a stop request does not pretend settlement completed", async ({ page }) => {
  const f = await fixture(page); f.makeActive(); f.mode.freshReport = false; f.state.targets[0].lease_expires_unix_nano = (Date.now() - 1000) * 1e6;
  await open(page); await expect(region(page).locator('[data-managed-state="stale"]')).toBeVisible();
  const dialog = await review(page, "Stop"); await dialog.getByRole("button", { name: "Confirm and continue" }).click();
  await expect(region(page).locator('[data-managed-state="queued"]')).toBeVisible();
  expect(f.state.effective_client_ids).toEqual(["client-a"]);
  expect(f.writes.at(-1)?.body.action).toBe("stop");
});

test("active setup remains editable and route management uses Dark Swap without duplicate port forms", async ({ page }) => {
  const f = await fixture(page); f.makeActive(); await open(page);
  await region(page).getByRole("button", { name: "Edit saved setup", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Edit Inbound setup", exact: true });
  await expect(dialog.getByLabel("Inbound name", { exact: true })).toBeEnabled();
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.getByRole("tab", { name: "Routes", exact: true }).click();
  await expect(page.getByRole("button", { name: "Add Node target", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Manage paths in Dark Swap", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/dark-swap\\?inbound=${id}&tab=routes`));
  await expect(page.getByRole("button", { name: "Edit saved paths", exact: true })).toBeVisible();
  expect(f.writes).toHaveLength(0);
});

for (const theme of ["light", "dark"] as const) test(`Persian ${theme} at 320px keeps client and execution controls within the viewport`, async ({ page }) => {
  const errors: string[] = []; page.on("pageerror", error => errors.push(error.message));
  await page.setViewportSize({ width: 320, height: 820 }); await fixture(page, { language: "fa", theme });
  await page.goto(`/inbounds?inbound=${id}`);
  const panel = page.getByRole("region", { name: "اجرای اینباند", exact: true }); await expect(panel).toBeVisible();
  await panel.getByRole("button", { name: "فعال‌سازی", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "بازبینی اجرا · فعال‌سازی", exact: true });
  await expect(dialog.getByRole("button", { name: "تأیید و ادامه", exact: true })).toBeEnabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  expect(await dialog.evaluate(node => node.scrollWidth <= node.clientWidth + 1)).toBe(true);
  await dialog.getByRole("button", { name: "انصراف", exact: true }).last().click();
  await page.getByRole("tab", { name: "کلاینت‌ها", exact: true }).click();
  await expect(page.getByRole("region", { name: "دسترسی کلاینت‌ها", exact: true }).getByLabel("انتخاب‌شده: client-b")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true); expect(errors).toEqual([]);
});

test("private or mismatched execution state is rejected before action controls are enabled", async ({ page }) => {
  await fixture(page, { tamper: true }); await page.goto(`/inbounds?inbound=${id}`);
  await expect(page.getByText("Execution state could not be verified. Refresh before making a change.", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Activate", exact: true })).toHaveCount(0);
  expect(await page.locator("body").textContent()).not.toContain("must-not-render");
});

test("readiness requires coherent generation, live lease, measured connectivity and admitted usage", () => {
  const now = Date.now(), state = freshState(), target = state.targets[0];
  Object.assign(target, { state: "active", desired_generation: 2, observed_generation: 2, process_ready: true, health_state: "verified", accounting_state: "admitted", reported_at_unix_nano: now * 1e6, lease_expires_unix_nano: (now + 10000) * 1e6 });
  expect(targetEvidence(target, now)).toBe("ready");
  expect(targetEvidence({ ...target, observed_generation: 1 }, now)).toBe("pending");
  expect(targetEvidence({ ...target, health_state: "unknown" }, now)).toBe("pending");
  expect(targetEvidence({ ...target, health_state: "engine_ready" }, now)).toBe("running");
  expect(targetEvidence({ ...target, accounting_state: "pending" }, now)).toBe("pending");
  expect(targetEvidence({ ...target, accounting_state: "stale" }, now)).toBe("stale");
  expect(targetEvidence(target, now + 10001)).toBe("stale");
  expect(() => readExecutionState({ ...state, inbound_id: id + 1 }, id)).toThrow();
  expect(() => readExecutionState({ ...state, targets: [target, target] }, id)).toThrow();
});


test("directory uses batched execution evidence without per-row execution requests", async ({ page }) => {
  const f = await fixture(page, { batch: true }); f.makeActive(); f.state.targets[0].health_state = "engine_ready";
  await page.goto("/inbounds");
  await expect(page.getByRole("row").filter({ has: page.getByRole("button", { name: "Managed inbound", exact: true }) }).getByText("Running", { exact: true })).toBeVisible();
  expect(f.reads).not.toContain(base);
});

async function ownerLossReview(page: Page) {
  await page.getByRole("region", { name: "Uncertain usage", exact: true }).getByRole("button", { name: "Review recovery", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Review recovery", exact: true });
  await dialog.getByLabel("Reason for accepting owner loss", { exact: true }).fill("Worker stopped; final counter file cannot be recovered.");
  await dialog.getByRole("button", { name: "Review owner loss", exact: true }).click();
  await expect(dialog.getByRole("checkbox")).toBeEnabled();
  return dialog;
}

test("owner-loss recovery reviews precise measured bytes, every affected inbound and explicit acknowledgement", async ({ page }) => {
  const f = await fixture(page, { reconciliation: true }); await open(page);
  const recovery = page.getByRole("region", { name: "Uncertain usage", exact: true });
  await expect(recovery.getByText("Amount unknown", { exact: true })).toBeVisible();
  await recovery.getByRole("button", { name: "Review recovery", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Review recovery", exact: true });
  await expect(dialog.getByRole("button", { name: "Review owner loss", exact: true })).toBeDisabled();
  await expect(dialog.getByRole("button", { name: "Accept owner loss", exact: true })).toBeDisabled();
  await expect(dialog.getByText("9,007,199,254,740,993", { exact: true })).toBeVisible();
  await expect(dialog.getByText("Owner total", { exact: true })).toBeVisible();
  await expect(dialog.getByText("Transit (not billable)", { exact: true })).toBeVisible();
  await expect(dialog.getByText(new RegExp(`#${id} · #${id + 1}`))).toBeVisible();
  await dialog.getByLabel("Reason for accepting owner loss", { exact: true }).fill("Worker stopped; final counter file cannot be recovered.");
  await dialog.getByRole("button", { name: "Review owner loss", exact: true }).click();
  await expect(dialog.getByRole("checkbox")).toBeEnabled();
  await expect(dialog.getByRole("button", { name: "Accept owner loss", exact: true })).toBeDisabled();
  await dialog.getByRole("checkbox").check();
  await dialog.getByRole("button", { name: "Accept owner loss", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(recovery.getByText("The owner-loss decision is recorded. Measured usage is retained. Starting the runtime requires a separate reviewed action.", { exact: true })).toBeVisible();
  expect(f.writes.map(write => write.path)).toEqual([base + "/reconciliations/preview", base + "/reconciliations"]);
  expect(f.writes[1].body).toEqual({ ...f.writes[0].body, review_sha: digest });
  expect(f.writes[0].body.expected_revision).toBe(7); expect(f.writes.every(write => write.csrf === csrf)).toBe(true);
  expect(f.state.observed_state).toBe("failed");
  const persisted = await page.evaluate(() => JSON.stringify({ local: Object.entries(localStorage), session: Object.entries(sessionStorage) }));
  expect(persisted).not.toContain(csrf); expect(persisted).not.toContain("known_usage");
});

test("a newly accepted counter report invalidates the owner-loss review and acknowledgement", async ({ page }) => {
  const f = await fixture(page, { reconciliation: true }); await open(page);
  const dialog = await ownerLossReview(page); await dialog.getByRole("checkbox").check();
  f.state.reconciliations![0].revision++; f.state.reconciliations![0].known_usage[0].upload = "9007199254740994";
  await expect(dialog.getByText("The usage evidence changed. Review the current evidence again before confirming.", { exact: true })).toBeVisible({ timeout: 7000 });
  await expect(dialog.getByRole("button", { name: "Accept owner loss", exact: true })).toBeDisabled();
  await expect(dialog.getByRole("checkbox")).toHaveCount(0);
  await expect(dialog.getByText("9,007,199,254,740,994", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "Review owner loss", exact: true }).click();
  await dialog.getByRole("checkbox").check();
  await dialog.getByRole("button", { name: "Accept owner loss", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(f.writes.filter(write => write.path === base + "/reconciliations")[0].body.expected_revision).toBe(8);
});

test("an interrupted owner-loss decision retains exact review and reason across reload", async ({ page }) => {
  const f = await fixture(page, { reconciliation: true }); f.mode.reconciliationFailure = "after_commit"; f.mode.reconciliationLookup = false;
  page.on("dialog", dialog => dialog.accept()); await open(page);
  const dialog = await ownerLossReview(page); await dialog.getByRole("checkbox").check();
  await dialog.getByRole("button", { name: "Accept owner loss", exact: true }).click();
  await expect(page.getByRole("button", { name: "Retry exact decision", exact: true })).toBeEnabled();
  const first = f.writes.find(write => write.path === base + "/reconciliations")!;
  await page.reload();
  await expect(page.getByRole("button", { name: "Retry exact decision", exact: true })).toBeEnabled();
  expect(f.writes.filter(write => write.path === base + "/reconciliations")).toHaveLength(1);
  await expect(page.getByRole("button", { name: "Discard rejected decision", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Check decision receipt", exact: true }).click();
  await expect(page.getByRole("button", { name: "Retry exact decision", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Retry exact decision", exact: true }).click();
  await expect(page.getByText("The owner-loss decision is recorded. Measured usage is retained. Starting the runtime requires a separate reviewed action.", { exact: true })).toBeVisible();
  expect(f.writes.filter(write => write.path === base + "/reconciliations")[1].raw).toBe(first.raw);
  expect(f.writes.some(write => write.path === base + "/commands")).toBe(false);
});

test("a rejected owner-loss decision can be discarded and must be reviewed again", async ({ page }) => {
  const f = await fixture(page, { reconciliation: true }); f.mode.reconciliationFailure = "rejected"; await open(page);
  const dialog = await ownerLossReview(page); await dialog.getByRole("checkbox").check();
  await dialog.getByRole("button", { name: "Accept owner loss", exact: true }).click();
  await page.getByRole("button", { name: "Discard rejected decision", exact: true }).click();
  await expect(page.getByRole("button", { name: "Review recovery", exact: true })).toBeEnabled();
  expect(f.state.reconciliations).toHaveLength(1);
  f.mode.reconciliationFailure = "";
  const next = await ownerLossReview(page); await next.getByRole("checkbox").check();
  await next.getByRole("button", { name: "Accept owner loss", exact: true }).click();
  await expect(next).toHaveCount(0);
  const commands = f.writes.filter(write => write.path === base + "/reconciliations");
  expect(commands).toHaveLength(2); expect(commands[0].body.event_id).not.toBe(commands[1].body.event_id);
});

test("owner-loss evidence rejects floating point bytes and never assigns zero to an unmeasured epoch", async ({ page }) => {
  const state = freshState(); state.reconciliations = [uncertainRuntime()];
  expect(() => readExecutionState({ ...state, reconciliations: [{ ...state.reconciliations[0], known_usage: [{ ...state.reconciliations[0].known_usage[0], upload: 9007199254740993 }] }] }, id)).toThrow();
  const f = await fixture(page, { reconciliation: true });
  Object.assign(f.state.reconciliations![0], { revision: 0, has_measured_usage: false, known_usage: [] });
  await open(page); const dialog = await ownerLossReview(page);
  await expect(dialog.getByText("No measured report was accepted. The unknown amount is not zero.", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("table")).toHaveCount(0);
  await expect(dialog.getByText("Amount unknown", { exact: true })).toBeVisible();
});

test("owner-loss review fits Persian light mode at 320px without hiding the acknowledgement", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 760 }); await fixture(page, { reconciliation: true, language: "fa", theme: "light" });
  await page.goto(`/inbounds?inbound=${id}`);
  await page.getByRole("region", { name: "مصرف نامشخص", exact: true }).getByRole("button", { name: "بازبینی بازیابی", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "بازبینی بازیابی", exact: true });
  await dialog.getByLabel("دلیل پذیرش زیان مالک", { exact: true }).fill("گزارش نهایی اجرا قابل بازیابی نیست.");
  await dialog.getByRole("button", { name: "بازبینی زیان مالک", exact: true }).click();
  await expect(dialog.getByRole("checkbox")).toBeEnabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
  expect(await dialog.evaluate(node => node.scrollWidth <= node.clientWidth + 2)).toBe(true);
});

for (const executionEnabled of [true, false, undefined]) test(`diagnostics keeps readiness scoped when managed execution is ${String(executionEnabled)}`, async ({ page }) => {
  await fixture(page, { executionEnabled }); await open(page);
  await page.getByRole("button", { name: "Diagnostics", exact: true }).click();
  await expect(page.getByRole("region", { name: "Native deployment readiness", exact: true })).toHaveCount(executionEnabled === true ? 0 : 1);
});
