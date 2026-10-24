import { expect, test, type Locator, type Page } from "@playwright/test";
import type { CreationPayload, CreationPlan, CreationReceipt } from "../src/InboundCreationModel";
import type { CreationEndpointPreview } from "../src/inboundEndpointPreview";
import { certificateMetadata, firstTLS, renewedTLS } from "./tls-fixtures";

// Synthetic, disposable owner-session fixtures. These tests never contact a
// running control plane or operate on real Nodes, customers or subscriptions.
const csrf = "a".repeat(64);
const api = "/api/v1/workspace/native-inbound-plans";
const nodes = [
  { node_id: "de", installation_id: "a".repeat(32), name: "Germany" },
  { node_id: "fr", installation_id: "b".repeat(32), name: "France" },
  { node_id: "nl", installation_id: "c".repeat(32), name: "Netherlands" },
];
type Call = { path: string; method: string; body: CreationPayload; raw: string; csrf: string };
type FixtureOptions = { failSaveOnce?: boolean; tamperPreview?: (plan: CreationPlan) => void; normalizeMappedIPs?: boolean;
  previewError?: { status: number; issue: { node_id?: string; destination_node_id?: string; path?: string; port?: number; field: string; code: string } }; savedPlan?: CreationPlan;
  language?: "en" | "fa"; theme?: "light" | "dark"; profileOptions?: boolean; profileSniffing?: boolean; profileFallbacks?: boolean; trafficScope?: boolean;
  profileXHTTP?: boolean; profileClient?: boolean; endpointOptions?: boolean; networkScope?: boolean; tamperEndpoints?: (rows: CreationEndpointPreview[]) => void;
  tls?: boolean; tamperSave?: (plan: CreationPlan) => void; tamperRead?: (plan: CreationPlan) => void; };

async function fixture(page: Page, options: FixtureOptions = {}) {
  const calls: Call[] = [];
  const receipts = new Map<string, { raw: string; receipt: CreationReceipt }>();
  let saved = options.savedPlan ? structuredClone(options.savedPlan) : undefined;
  let failSave = options.failSaveOnce === true;
  // The real API omits zero fields and empty sections, and returns enabled:false
  // for disabled-but-configured Sniffing. Model that wire contract independently.
  function persistedOptions(input: CreationPayload["profile"]["options"]) {
    if (!input) return undefined;
    const result = structuredClone(input);
    if (result.sniffing) {
      const entries = Object.entries(result.sniffing).filter(([, value]) => Array.isArray(value) ? value.length > 0 : value === true);
      if (entries.length) result.sniffing = { enabled: result.sniffing.enabled === true, ...Object.fromEntries(entries) };
      else delete result.sniffing;
    }
    if (result.fallbacks?.length === 0) delete result.fallbacks;
    for (const section of ["xhttp", "client", "websocket", "http_upgrade"] as const) {
      const value = result[section];
      if (value && !Object.values(value).some(item => item !== "")) delete result[section];
    }
    return Object.keys(result).length ? result : undefined;
  }
  const profileInput = (profile: CreationPayload["profile"]) => JSON.stringify({ id: profile.id, name: profile.name,
    security: profile.security || "reality", transport: profile.transport, grpc_service: profile.grpc_service, reality_target: profile.reality_target || "",
    server_names: profile.server_names, short_ids: profile.short_ids || [], options: persistedOptions(profile.options) });
  function planFor(body: CreationPayload, saving = false): CreationPlan {
    const revision = body.expected_inbound_revision === 0 ? 1 : body.expected_inbound_revision +
      (!body.tls_credentials && saved && profileInput(saved.profile) === profileInput(body.profile) ? 0 : 1);
    const waiting = body.nodes.some(row => row.direct_port > 0 && !row.direct_hosts.length || row.tunnel_port > 0 && !row.tunnel_hosts.length || row.swap_enabled && !row.swap_entry_address);
    const rows = structuredClone(body.nodes).sort((a, b) => a.node_id.localeCompare(b.node_id));
    for (const row of rows) {
      if (!row.direct_endpoints?.length) delete row.direct_endpoints;
      if (!row.tunnel_endpoints?.length) delete row.tunnel_endpoints;
    }
    if (options.normalizeMappedIPs) for (const row of rows) {
      // net.ParseIP(...).String() represents mapped IPv6 as dotted IPv4.
      row.listen_ip = row.listen_ip.replace(/^::ffff:(\d+\.\d+\.\d+\.\d+)$/i, "$1");
      row.swap_source_listen_ip = row.swap_source_listen_ip.replace(/^::ffff:(\d+\.\d+\.\d+\.\d+)$/i, "$1");
    }
    const tls = body.profile.security === "tls";
    return { profile: { ...structuredClone(body.profile), security: tls ? "tls" : undefined, reality_target: body.profile.reality_target || "", short_ids: body.profile.short_ids || [], name: body.profile.name.trim(), enabled: false, archived: false,
      bound_clients: 0, revision, options: persistedOptions(body.profile.options), public_key: tls ? "" : saved?.profile.public_key ?? (saving ? "SYNTHETIC_PUBLIC_KEY" : ""),
      ...(tls ? { tls: body.tls_credentials ? certificateMetadata(body.tls_credentials.certificate_pem) : saved?.profile.tls } : {}) },
      nodes: rows,
      plan_revision: body.expected_plan_revision + 1, inbound_revision: revision,
      status: waiting ? "WAITING_HOST" : "SAVED_DISABLED", last_event_id: body.event_id, reason: body.reason,
      created_unix: 1800000000, updated_unix: 1800000001, runtime_apply: false, editable: true };
  }
  function endpointPreviews(plan: CreationPlan): CreationEndpointPreview[] {
    const result: CreationEndpointPreview[] = [];
    for (const row of plan.nodes) {
      const paths = (["direct", "tunnel"] as const).flatMap(path => row[`${path}_port`] ? (row[`${path}_endpoints`]?.length ? row[`${path}_endpoints`]! : row[`${path}_hosts`].map(address => ({ address, port: 0, enabled: true }))).map(endpoint => ({ path, endpoint, listener: row[`${path}_port`] })) : []);
      const entries: Array<{ path: "direct" | "tunnel" | "swap"; endpoint: NonNullable<typeof row.direct_endpoints>[number]; listener: number }> = paths;
      if (row.swap_enabled && row.swap_entry_address) entries.push({ path: "swap", listener: row.swap_destination_port, endpoint: { address: row.swap_entry_address, port: row.swap_entry_port, enabled: true } });
      for (const { path, endpoint, listener } of entries) result.push({ node_id: row.node_id, path, address: endpoint.address, port: endpoint.port || listener,
        listen_port: listener, label: endpoint.label || plan.profile.name + " · " + ({ direct: "Direct", tunnel: "Tunnel", swap: "Swap" })[path], enabled: endpoint.enabled,
        server_name: endpoint.sni || plan.profile.server_names[0], transport: plan.profile.transport,
        ...(plan.profile.security === "tls" ? { security: "tls", tls_sha256: plan.profile.tls!.sha256, tls_public_key_sha256: plan.profile.tls!.public_key_sha256 } : {}),
        fingerprint: endpoint.fingerprint || plan.profile.options?.client?.fingerprint || "chrome", spider_x: plan.profile.security === "tls" ? "" : endpoint.spider_x || plan.profile.options?.client?.spider_x || "/",
        ...(plan.profile.transport === "grpc" ? { grpc_service: plan.profile.grpc_service, grpc_authority: plan.profile.options?.grpc_authority, grpc_multi_mode: plan.profile.options?.grpc_multi_mode } : {}),
        ...(plan.profile.options?.xhttp ? { xhttp: plan.profile.options.xhttp } : {}),
        ...(plan.profile.options?.websocket ? { websocket: plan.profile.options.websocket } : {}),
        ...(plan.profile.options?.http_upgrade ? { http_upgrade: plan.profile.options.http_upgrade } : {}) });
    }
    return result;
  }
  await page.addInitScript(({ language, theme }) => {
    localStorage.setItem("dark-pro-lang", language); localStorage.setItem("dark-pro-theme", theme);
  }, { language: options.language ?? "en", theme: options.theme ?? "dark" });
  await page.route("**/api/v1/session/status", route => route.fulfill({ json: { authenticated: true,
    user: { id: "owner", role: "owner", username: "test-owner", representative_id: "owner" }, csrf_token: csrf, expires_unix: 1900000000 } }));
  await page.route("**/api/v1/fleet", route => route.fulfill({ json: { nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0,
    as_of: new Date().toISOString(), interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0,
    physical_rates_complete: false, metric_scope: "default_route_interface_not_billable" } }));
  await page.route("**/api/v1/workspace/**", async route => {
    const request = route.request(), path = new URL(request.url()).pathname;
    if (request.method() === "POST" || request.method() === "PUT") {
      const body = request.postDataJSON() as CreationPayload, raw = request.postData() ?? "";
      calls.push({ path, method: request.method(), body, raw, csrf: request.headers()["x-csrf-token"] ?? "" });
      if (path === api + "/preview") {
        if (options.previewError) return route.fulfill({ status: options.previewError.status,
          json: { error: "business_event_or_identity_conflict", issue: options.previewError.issue } });
        const plan = planFor(body); options.tamperPreview?.(plan);
        const endpoints = endpointPreviews(plan); options.tamperEndpoints?.(endpoints);
        return route.fulfill({ json: { valid: true, plan, runtime_changed: false, topology_validated: true,
          topology_validation_only: true, topology_key_scope: body.expected_plan_revision ? "saved_profile" : "preview_only", topology: { nodes: [], sha256: "d".repeat(64) }, endpoints } });
      }
      if (path === api || path === api + "/" + body.profile?.id) {
        const previous = receipts.get(body.event_id);
        if (previous) {
          if (previous.raw !== raw) return route.fulfill({ status: 409, json: { error: "business_event_or_identity_conflict" } });
          return route.fulfill({ json: { ...previous.receipt, replayed: true } });
        }
        saved = planFor(body, true);
        options.tamperSave?.(saved);
        const receipt: CreationReceipt = { plan: saved, event_id: body.event_id, replayed: false, runtime_changed: false,
          clients_deleted: false, credit_changed: false, usage_reset: false, subscription_published: false };
        receipts.set(body.event_id, { raw, receipt });
        // Simulate commit followed by a lost/unusable response, then exact replay.
        if (failSave) { failSave = false; return route.fulfill({ status: 500, json: { error: "response_lost" } }); }
        return route.fulfill({ status: request.method() === "POST" ? 201 : 200, json: receipt });
      }
      return route.fulfill({ status: 405, json: { error: "unexpected_mutation" } });
    }
    if (path === api + "/capabilities") return route.fulfill({ json: { available: true, version: options.tls ? 3 : 2, max_nodes: 64,
      max_hosts_per_node: 64, profile_options: options.profileOptions !== false, protocols: ["vless"], securities: options.tls ? ["reality", "tls"] : ["reality"], transports: options.tls ? ["tcp", "grpc", "xhttp", "ws", "httpupgrade"] : ["tcp", "grpc", "xhttp"], runtime_apply: false,
      profile_tls: options.tls === true, profile_tls_scope: options.tls ? "creation_plan" : undefined, profile_websocket: options.tls === true, profile_http_upgrade: options.tls === true,
      profile_sniffing: options.profileSniffing !== false, profile_fallbacks: options.profileFallbacks !== false, profile_traffic_options_scope: options.trafficScope === false ? undefined : "creation_plan",
      profile_xhttp: options.profileXHTTP !== false, profile_client_options: options.profileClient !== false, profile_network_options_scope: options.networkScope === false ? undefined : "creation_plan",
      endpoint_options: options.endpointOptions !== false, max_endpoints_per_path: 64, listener_port_min: 1024, listener_port_max: 65535, reserved_listener_ports: [2087, 8443, 24443],
      plans: saved ? [{ inbound_id: saved.profile.id, plan_revision: saved.plan_revision, inbound_revision: saved.inbound_revision, status: saved.status,
        node_ids: [...new Set(saved.nodes.flatMap(node => [node.node_id, ...(node.swap_enabled ? [node.swap_source_node_id] : [])]))].sort() }] : [] } });
    if (saved && path === api + "/" + saved.profile.id) { const plan = structuredClone(saved); options.tamperRead?.(plan); return route.fulfill({ json: { plan, references_current: true } }); }
    if (path.endsWith("/native-inbounds-state")) return route.fulfill({ json: {
      preflight: { scope: "new_pro_lab_only", paired_nodes: 3, online_nodes: 0, fresh_nodes: 0, configured_inbounds: 0,
        configured_targets: 0, client_bindings: 0, usage_ledger_enabled: true, remote_metered_admission: "trusted_remote_epoch_registration_pending",
        customer_data_plane_verified: false, subscription_published: false, deployment_ready: false, blocking_reason: "remote_metered_admission_not_wired" },
      inbounds: saved ? [saved.profile] : [], nodes, targets: [], runtime: [],
    } });
    if (path.endsWith("/operations")) return route.fulfill({ json: { operations: [] } });
    if (path.endsWith("/clients")) return route.fulfill({ json: { clients: [], client_ids: [] } });
    return route.fulfill({ status: 404, json: {} });
  });
  return { calls, getSaved: () => saved, makeReadOnly: () => { if (saved) { saved.editable = false; saved.edit_blocker = "profile_changed_or_materialized"; } } };
}

async function openCreation(page: Page, language: "en" | "fa" = "en") {
  await page.goto("/inbounds");
  const add = page.getByRole("button", { name: language === "fa" ? "افزودن اینباند" : "Add Inbound", exact: true });
  await expect(add).toBeEnabled(); await add.click();
  const dialog = page.getByRole("dialog", { name: language === "fa" ? "ساخت اینباند" : "Create Inbound", exact: true });
  await expect(dialog).toBeVisible(); return dialog;
}
async function profile(dialog: Locator, name = "Complete DARK setup") {
  await dialog.getByLabel("Inbound name", { exact: true }).fill(name);
  await dialog.getByLabel("REALITY target (host:port)", { exact: true }).fill("example.org:443");
  await dialog.getByLabel("Server names (comma-separated)", { exact: true }).fill("example.org");
  await dialog.getByLabel("Short IDs", { exact: true }).fill("aabb, ccdd");
}
async function destination(dialog: Locator, id: "de" | "fr" | "nl", ip: string, port: string, hosts: string) {
  await dialog.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  await dialog.getByRole("checkbox", { name: "Select destination: " + nodes.find(node => node.node_id === id)!.name, exact: true }).check();
  await dialog.getByLabel("Destination listening IP: " + id, { exact: true }).fill(ip);
  await dialog.getByLabel("Direct port: " + id, { exact: true }).fill(port);
  await dialog.getByLabel("Direct Hosts: " + id, { exact: true }).fill(hosts);
}
async function swap(dialog: Locator, id = "de", source = "nl", sourceIP = "203.0.113.30", entry = "entry.example.org") {
  await dialog.getByRole("checkbox", { name: "Dark Swap: " + id, exact: true }).check();
  await dialog.getByLabel("Source Node: " + id, { exact: true }).selectOption(source);
  await dialog.getByLabel("Source relay listening IP: " + id, { exact: true }).fill(sourceIP);
  await dialog.getByLabel("Source relay port: " + id, { exact: true }).fill("16801");
  await dialog.getByLabel("External entry address: " + id, { exact: true }).fill(entry);
  await dialog.getByLabel("External entry port: " + id, { exact: true }).fill("443");
  await dialog.getByLabel("Destination exit port: " + id, { exact: true }).fill("18443");
}
async function review(dialog: Locator, note = "Complete profile and paths") {
  await dialog.getByRole("tab", { name: "Review", exact: true }).click();
  await dialog.getByLabel("Change note", { exact: true }).fill(note);
}
async function check(dialog: Locator) {
  await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Save paused setup", exact: true })).toBeEnabled();
}
async function save(dialog: Locator) {
  await dialog.getByRole("button", { name: "Save paused setup", exact: true }).click();
  await expect(dialog).toBeHidden();
}

test("one complete plan saves multiple destinations and independent Direct, Tunnel and Swap paths with a relay-only source", async ({ page }) => {
  const f = await fixture(page), dialog = await openCreation(page);
  await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "shared.example.org\nde.example.org");
  await dialog.getByRole("checkbox", { name: "Tunnel: de", exact: true }).check();
  await dialog.getByLabel("Tunnel port: de", { exact: true }).fill("1185");
  await dialog.getByLabel("Tunnel Hosts: de", { exact: true }).fill("shared.example.org\ntunnel.example.org");
  await swap(dialog);
  await destination(dialog, "fr", "203.0.113.20", "11443", "fr.example.org");
  await review(dialog);
  await expect(dialog.getByRole("button", { name: "Save paused setup", exact: true })).toBeDisabled();
  expect(f.calls).toHaveLength(0); await check(dialog); await save(dialog);
  expect(f.calls.map(call => call.path)).toEqual([api + "/preview", api]);
  expect(f.calls[0].raw).toBe(f.calls[1].raw);
  expect(f.calls.every(call => call.csrf === csrf)).toBe(true);
  const body = f.calls[1].body;
  expect(body).toMatchObject({ expected_plan_revision: 0, expected_inbound_revision: 0, profile: { name: "Complete DARK setup", transport: "tcp", short_ids: ["aabb", "ccdd"] } });
  expect(body.nodes.map(row => row.node_id)).toEqual(["de", "fr"]);
  expect(body.nodes[0]).toMatchObject({ direct_port: 10443, tunnel_port: 1185, swap_destination_port: 18443,
    direct_hosts: ["shared.example.org", "de.example.org"], tunnel_hosts: ["shared.example.org", "tunnel.example.org"],
    swap_source_node_id: "nl", swap_source_installation_id: nodes[2].installation_id, swap_source_listen_ip: "203.0.113.30",
    swap_source_port: 16801, swap_entry_address: "entry.example.org", swap_entry_port: 443 });
  expect(body.profile).not.toHaveProperty("private_key"); expect(body.profile).not.toHaveProperty("clients");
  expect(f.getSaved()?.profile.enabled).toBe(false); expect(f.getSaved()?.runtime_apply).toBe(false);
  await expect(page.getByRole("button", { name: "Open saved setup", exact: true })).toBeVisible();
  await expect(page.getByText("The complete Inbound setup was saved paused. No listener or subscription was activated.", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Add Node target", exact: true })).toHaveCount(0);
});

test("a saved paused setup can be archived without exposing partial activation or profile edits", async ({ page }) => {
  const f = await fixture(page), dialog = await openCreation(page);
  await profile(dialog, "Cancel paused setup");
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await review(dialog); await check(dialog); await save(dialog);
  const saved = f.getSaved()!;
  const lifecycle: Array<Record<string, unknown>> = [];
  await page.route("**/api/v1/workspace/native-inbounds/" + saved.profile.id + "/lifecycle", async route => {
    expect(route.request().headers()["x-csrf-token"]).toBe(csrf);
    const body = route.request().postDataJSON(); lifecycle.push(body);
    saved.profile = { ...saved.profile, enabled: false, archived: true, revision: saved.profile.revision + 1 };
    saved.editable = false; saved.edit_blocker = "profile_changed_or_materialized";
    await route.fulfill({ json: { inbound: saved.profile, clients_deleted: false, ledger_modified: false } });
  });
  await page.getByRole("tab", { name: "Overview", exact: true }).click();
  await page.getByText("More actions", { exact: true }).click();
  await expect(page.getByRole("button", { name: "Enable Inbound", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Edit profile", exact: true })).toHaveCount(0);
  await page.getByLabel("Reason for change", { exact: true }).fill("Cancel unused saved configuration");
  page.once("dialog", prompt => prompt.accept());
  await page.getByRole("button", { name: "Archive saved setup", exact: true }).click();
  await expect.poll(() => lifecycle.length).toBe(1);
  expect(lifecycle[0]).toMatchObject({ action: "archive", expected_revision: 1, reason: "Cancel unused saved configuration" });
  expect(lifecycle[0]).not.toHaveProperty("profile");
  await expect(page.getByRole("button", { name: "Archive saved setup", exact: true })).toHaveCount(0);
});

test("destination Tunnel reuse preserves the source Node ordinary Tunnel and independent relay port", async ({ page }) => {
  const f = await fixture(page), dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "nl", "203.0.113.30", "15443", "nl.example.org");
  await dialog.getByRole("checkbox", { name: "Tunnel: nl", exact: true }).check();
  await dialog.getByLabel("Tunnel port: nl", { exact: true }).fill("15185");
  await dialog.getByLabel("Tunnel Hosts: nl", { exact: true }).fill("nl-tunnel.example.org");
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await dialog.getByRole("checkbox", { name: "Tunnel: de", exact: true }).check();
  await dialog.getByLabel("Tunnel port: de", { exact: true }).fill("1185");
  await dialog.getByLabel("Tunnel Hosts: de", { exact: true }).fill("de-tunnel.example.org");
  await swap(dialog);
  await dialog.getByRole("button", { name: "Use this Node’s Tunnel port", exact: true }).click();
  await expect(dialog.getByLabel("Destination exit port: de", { exact: true })).toHaveValue("1185");
  await expect(dialog.getByLabel("Source relay port: de", { exact: true })).toHaveValue("16801");
  await review(dialog); await check(dialog); await save(dialog);
  const rows = f.calls[1].body.nodes;
  expect(rows.find(row => row.node_id === "nl")).toMatchObject({ direct_port: 15443, tunnel_port: 15185, swap_enabled: false });
  expect(rows.find(row => row.node_id === "de")).toMatchObject({ direct_port: 10443, tunnel_port: 1185, swap_destination_port: 1185, swap_source_port: 16801 });
});

test("equivalent mapped IPv6 Node addresses accept the backend canonical IPv4 receipt", async ({ page }) => {
  const f = await fixture(page, { normalizeMappedIPs: true }), dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "nl", "::ffff:203.0.113.30", "15443", "nl.example.org");
  await destination(dialog, "de", "::ffff:203.0.113.10", "10443", "de.example.org");
  await swap(dialog, "de", "nl", "203.0.113.30");
  await review(dialog); await check(dialog); await save(dialog);
  expect(f.getSaved()?.nodes.find(row => row.node_id === "de")).toMatchObject({ listen_ip: "203.0.113.10", swap_source_listen_ip: "203.0.113.30" });
  expect(f.getSaved()?.nodes.find(row => row.node_id === "nl")?.listen_ip).toBe("203.0.113.30");
  expect(f.calls.map(call => call.path)).toEqual([api + "/preview", api]);
});

test("invalid hidden fields reveal their tab and no partial creation request is sent", async ({ page }) => {
  const f = await fixture(page), dialog = await openCreation(page); await profile(dialog);
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("tab", { name: "Dark Swap", exact: true })).toHaveAttribute("aria-selected", "true");
  const firstDestination = dialog.getByRole("checkbox", { name: "Select destination: Germany", exact: true });
  await expect(firstDestination).toBeVisible(); await expect(firstDestination).toBeFocused(); expect(f.calls).toHaveLength(0);
  await destination(dialog, "de", "203.0.113.10", "2087", "duplicate.example.org\nduplicate.example.org");
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("tab", { name: "Dark Swap", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(dialog.getByLabel("Direct Hosts: de", { exact: true })).toBeFocused();
  await expect(dialog.getByRole("alert")).toContainText("Remove duplicate addresses");
  expect(f.calls).toHaveLength(0);
  await dialog.getByLabel("Direct Hosts: de", { exact: true }).fill("de.example.org");
  await dialog.getByLabel("Direct port: de", { exact: true }).fill("10443");
  await dialog.getByRole("tab", { name: "Connection", exact: true }).click();
  await dialog.getByRole("radio", { name: "gRPC / REALITY", exact: true }).check();
  await dialog.locator('[data-connection-group="network"] > summary').click();
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("tab", { name: "Connection", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(dialog.locator('[data-connection-group="network"]')).toHaveAttribute("open", "");
  await expect(dialog.getByLabel("gRPC service", { exact: true })).toBeFocused(); expect(f.calls).toHaveLength(0);
  await dialog.getByLabel("gRPC service", { exact: true }).fill("dark-grpc");
  await dialog.getByLabel("Inbound name", { exact: true }).fill("");
  await dialog.locator('[data-connection-group="general"] > summary').click();
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByLabel("Inbound name", { exact: true })).toBeFocused();
  await expect(dialog.locator('[data-connection-group="general"]')).toHaveAttribute("open", "");
  expect(f.calls).toHaveLength(0);
  await dialog.getByLabel("Inbound name", { exact: true }).fill("Complete DARK setup");
  await review(dialog); await check(dialog);
  expect(f.calls).toHaveLength(1); expect(f.calls[0].body.profile).toMatchObject({ transport: "grpc", grpc_service: "dark-grpc" });
});

for (const conflict of [
  { name: "destination port", node: "de", field: "direct_port", label: "Direct port: de" },
  { name: "unselected relay port", node: "nl", field: "swap_source_port", label: "Source relay port: de" },
  { name: "unselected relay IP", node: "nl", field: "swap_source_listen_ip", label: "Source relay listening IP: de" },
]) test(`a server-side ${conflict.name} conflict opens its destination row and exact input`, async ({ page }) => {
  const f = await fixture(page, { previewError: { status: 409, issue: { node_id: conflict.node, field: conflict.field, code: "existing_listener_port_collision" } } });
  const dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  if (conflict.node === "nl") await swap(dialog);
  // Leave a different Node expanded so matching the first visible input cannot pass.
  await destination(dialog, "fr", "203.0.113.20", "11443", "fr.example.org");
  await review(dialog);
  await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("tab", { name: "Dark Swap", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(dialog.getByLabel(conflict.label, { exact: true })).toBeFocused();
  await expect(dialog.getByRole("checkbox", { name: "Select destination: Netherlands", exact: true })).not.toBeChecked();
  await expect(dialog.getByRole("button", { name: "Configure: Germany", exact: true })).toHaveAttribute("aria-expanded", "true");
  await expect(dialog.getByRole("button", { name: "Reload saved setup", exact: true })).toHaveCount(0);
  await dialog.getByRole("tab", { name: "Review", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Save paused setup", exact: true })).toBeDisabled();
  expect(f.calls.map(call => call.path)).toEqual([api + "/preview"]); expect(f.getSaved()).toBeUndefined();
});

test("an uncertain committed save retains the full setup and retries the identical event only on request", async ({ page }) => {
  const f = await fixture(page, { failSaveOnce: true }), dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org"); await swap(dialog);
  await review(dialog); await check(dialog);
  await dialog.getByRole("button", { name: "Save paused setup", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Retry the same save", exact: true })).toBeEnabled();
  await expect(dialog.getByRole("alert")).toContainText("complete setup is retained");
  expect(f.calls).toHaveLength(2);
  await dialog.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  await expect(dialog.getByLabel("Source relay port: de", { exact: true })).toHaveValue("16801");
  await expect(dialog.getByLabel("Source relay port: de", { exact: true })).toBeDisabled();
  await expect(dialog.getByLabel("External entry address: de", { exact: true })).toHaveValue("entry.example.org");
  await dialog.getByRole("tab", { name: "Review", exact: true }).click();
  await dialog.getByRole("button", { name: "Retry the same save", exact: true }).click();
  await expect(dialog).toBeHidden(); expect(f.calls).toHaveLength(3);
  expect(f.calls[2].raw).toBe(f.calls[1].raw); expect(f.calls[2].body.event_id).toBe(f.calls[0].body.event_id);
  expect(f.getSaved()?.plan_revision).toBe(1);
});

test("a preview with altered route mappings is rejected even when identity and counts match", async ({ page }) => {
  const f = await fixture(page, { tamperPreview: plan => { plan.nodes[0].swap_source_port = 16802; } });
  const dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org"); await swap(dialog); await review(dialog);
  await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("could not validate this setup");
  await expect(dialog.getByRole("button", { name: "Save paused setup", exact: true })).toBeDisabled();
  expect(f.calls.map(call => call.path)).toEqual([api + "/preview"]); expect(f.getSaved()).toBeUndefined();
});

test("missing ordinary Hosts and external entry address save as WAITING_HOST without inferred public addresses", async ({ page }) => {
  const f = await fixture(page), dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", ""); await swap(dialog, "de", "nl", "203.0.113.30", "");
  await expect(dialog.getByLabel("External entry address: de", { exact: true })).toHaveValue("");
  await review(dialog); await check(dialog); await save(dialog);
  expect(f.getSaved()).toMatchObject({ status: "WAITING_HOST", runtime_apply: false, profile: { enabled: false } });
  expect(f.calls[1].body.nodes[0]).toMatchObject({ direct_hosts: [], swap_entry_address: "", swap_source_listen_ip: "203.0.113.30", swap_entry_port: 443 });
  await expect(page.getByText("Some paths are waiting for Hosts.", { exact: true })).toBeVisible();
});

test("Dark Swap owns independent Direct and Tunnel drafts, zero-port semantics and saved setup reopening while Swap is off", async ({ page }, testInfo) => {
  const f = await fixture(page), dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await dialog.getByRole("checkbox", { name: "Tunnel: de", exact: true }).check();
  await dialog.getByLabel("Tunnel port: de", { exact: true }).fill("1185");
  await dialog.getByLabel("Tunnel Hosts: de", { exact: true }).fill("tunnel.example.org\nbackup.example.org");
  await expect(dialog.getByRole("checkbox", { name: "Dark Swap: de", exact: true })).not.toBeChecked();
  await expect(dialog.getByLabel("Source Node: de", { exact: true })).toHaveCount(0);
  await dialog.getByRole("checkbox", { name: "Direct: de", exact: true }).uncheck();
  await expect(dialog.getByLabel("Direct port: de", { exact: true })).toHaveCount(0);
  await expect(dialog.getByLabel("Tunnel port: de", { exact: true })).toHaveValue("1185");
  await review(dialog); await check(dialog);
  expect(f.calls[0].body.nodes[0]).toMatchObject({ direct_port: 0, direct_hosts: [], tunnel_port: 1185,
    tunnel_hosts: ["tunnel.example.org", "backup.example.org"], swap_enabled: false, swap_source_port: 0, swap_destination_port: 0 });
  const retained = dialog.locator('[data-review-node="de"] [data-review-path="direct"]');
  await expect(retained).toHaveAttribute("data-path-enabled", "false");
  await expect(retained).toHaveAttribute("data-path-retained", "true");
  await expect(retained).toContainText("Disabled · draft values are not saved");
  await retained.getByText("Retained draft", { exact: true }).click();
  await expect(retained).toContainText("10443"); await expect(retained).toContainText("de.example.org");
  await dialog.getByRole("button", { name: "Edit Dark Swap", exact: true }).click();
  await dialog.getByRole("checkbox", { name: "Direct: de", exact: true }).check();
  await expect(dialog.getByLabel("Direct port: de", { exact: true })).toHaveValue("10443");
  await expect(dialog.getByLabel("Direct Hosts: de", { exact: true })).toHaveValue("de.example.org");
  await dialog.getByRole("checkbox", { name: "Tunnel: de", exact: true }).uncheck();
  await review(dialog); await check(dialog);
  expect(f.calls[1].body.nodes[0]).toMatchObject({ direct_port: 10443, direct_hosts: ["de.example.org"], tunnel_port: 0, tunnel_hosts: [], swap_enabled: false });
  await dialog.getByRole("tab", { name: "Connection", exact: true }).click();
  await dialog.getByRole("tab", { name: "Advanced", exact: true }).click();
  await dialog.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  await dialog.getByRole("checkbox", { name: "Tunnel: de", exact: true }).check();
  await expect(dialog.getByLabel("Tunnel port: de", { exact: true })).toHaveValue("1185");
  await expect(dialog.getByLabel("Tunnel Hosts: de", { exact: true })).toHaveValue("tunnel.example.org\nbackup.example.org");
  await expect(dialog.getByLabel("Direct port: de", { exact: true })).toHaveValue("10443");
  // Deselecting and reselecting the same installation also preserves its draft.
  const selected = dialog.getByRole("checkbox", { name: "Select destination: Germany", exact: true });
  page.once("dialog", prompt => prompt.accept());
  await selected.uncheck(); await expect(selected).not.toBeChecked(); await selected.check();
  await expect(dialog.getByLabel("Direct port: de", { exact: true })).toHaveValue("10443");
  await expect(dialog.getByLabel("Tunnel port: de", { exact: true })).toHaveValue("1185");
  await page.screenshot({ path: testInfo.outputPath("dark-swap-en-desktop.png"), fullPage: true });
  await review(dialog); await check(dialog); await save(dialog);
  expect(f.getSaved()?.nodes[0]).toMatchObject({ direct_port: 10443, direct_hosts: ["de.example.org"], tunnel_port: 1185,
    tunnel_hosts: ["tunnel.example.org", "backup.example.org"], swap_enabled: false });
  const originalID = f.getSaved()!.profile.id, originalKey = f.getSaved()!.profile.public_key;
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "Edit Inbound setup", exact: true }); await expect(edit).toBeVisible();
  await expect(edit.getByLabel("Short IDs", { exact: true })).toHaveValue("aabb, ccdd");
  await edit.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  await expect(edit.getByRole("checkbox", { name: "Dark Swap: de", exact: true })).not.toBeChecked();
  await expect(edit.getByLabel("Direct port: de", { exact: true })).toHaveValue("10443");
  await expect(edit.getByLabel("Tunnel port: de", { exact: true })).toHaveValue("1185");
  await expect(edit.getByLabel("Direct Hosts: de", { exact: true })).toHaveValue("de.example.org");
  await expect(edit.getByLabel("Tunnel Hosts: de", { exact: true })).toHaveValue("tunnel.example.org\nbackup.example.org");
  await edit.getByLabel("Direct port: de", { exact: true }).fill("10444");
  await review(edit, "Change destination port only"); await check(edit); await save(edit);
  expect(f.calls[f.calls.length - 1]).toMatchObject({ path: api + "/" + originalID, method: "PUT", body: { expected_plan_revision: 1, expected_inbound_revision: 1, profile: { id: originalID } } });
  expect(f.getSaved()).toMatchObject({ plan_revision: 2, inbound_revision: 1, profile: { id: originalID, public_key: originalKey, revision: 1 } });
  expect(f.getSaved()?.nodes[0]).toMatchObject({ direct_port: 10444, tunnel_port: 1185, tunnel_hosts: ["tunnel.example.org", "backup.example.org"], swap_enabled: false });
  f.makeReadOnly(); await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  await expect(edit.getByLabel("Inbound name", { exact: true })).toBeDisabled();
  await expect(edit.getByText(/This saved setup is read-only/)).toBeVisible();
  await edit.getByRole("tab", { name: "Review", exact: true }).click();
  await expect(edit.getByRole("button", { name: "Check complete setup", exact: true })).toBeDisabled();
});

test("Persian 320px editor keeps RTL tab navigation, visible footer and contained horizontal layout", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 760 });
  const f = await fixture(page, { language: "fa", theme: "light" }), dialog = await openCreation(page, "fa");
  await expect(dialog.getByLabel("نام اینباند", { exact: true })).toBeFocused();
  await dialog.getByLabel("نام اینباند", { exact: true }).fill("پروفایل یکپارچهٔ دارک");
  const profileTab = dialog.getByRole("tab", { name: "اتصال", exact: true }); await profileTab.focus(); await profileTab.press("ArrowLeft");
  await expect(dialog.getByRole("tab", { name: "دارک‌سواپ", exact: true })).toBeFocused();
  await dialog.getByRole("tab", { name: "دارک‌سواپ", exact: true }).click();
  await dialog.getByRole("checkbox", { name: "انتخاب مقصد: Germany", exact: true }).check();
  await dialog.getByRole("checkbox", { name: "تونل: de", exact: true }).check();
  await dialog.getByRole("checkbox", { name: "دارک‌سواپ: de", exact: true }).check();
  await dialog.getByLabel("نود مبدأ: de", { exact: true }).selectOption("nl");
  await dialog.getByLabel("پورت خروجی مقصد: de", { exact: true }).fill("18443");
  await expect(dialog.getByLabel("پورت خروجی مقصد: de", { exact: true })).toHaveAttribute("dir", "ltr");
  const footer = dialog.locator(".creation-footer"); await expect(footer).toBeInViewport();
  const bounds = await footer.boundingBox(); expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(761);
  const widths = await dialog.evaluate(element => ({ client: element.clientWidth, scroll: element.scrollWidth,
    body: document.documentElement.clientWidth, bodyScroll: document.documentElement.scrollWidth }));
  expect(widths.scroll).toBeLessThanOrEqual(widths.client + 1); expect(widths.bodyScroll).toBeLessThanOrEqual(widths.body + 1);
  await page.screenshot({ path: testInfo.outputPath("creation-fa-light-320.png"), fullPage: true });
  await dialog.getByRole("tab", { name: "دارک‌سواپ", exact: true }).focus();
  await page.keyboard.press("End"); await expect(dialog.getByRole("tab", { name: "بازبینی", exact: true })).toBeFocused();
  const dismiss = page.waitForEvent("dialog").then(prompt => prompt.dismiss()); await page.keyboard.press("Escape"); await dismiss;
  await expect(dialog).toBeVisible(); expect(f.calls).toHaveLength(0);
});

test("delayed capabilities keep direct-detail mutations blocked until the saved plan index is known", async ({ page }) => {
  const savedPlan: CreationPlan = { profile: { id: 701, name: "Previously saved setup", transport: "tcp", grpc_service: "",
    reality_target: "example.org:443", server_names: ["example.org"], short_ids: ["aabb"], public_key: "SYNTHETIC_PUBLIC_KEY",
    revision: 1, enabled: false, archived: false, bound_clients: 0 },
    nodes: [{ node_id: "de", installation_id: nodes[0].installation_id, listen_ip: "203.0.113.10", direct_port: 10443,
      direct_hosts: ["de.example.org"], tunnel_port: 0, tunnel_hosts: [], swap_enabled: false, swap_source_node_id: "",
      swap_source_installation_id: "", swap_source_listen_ip: "", swap_source_port: 0, swap_entry_address: "", swap_entry_port: 0, swap_destination_port: 0 }],
    plan_revision: 1, inbound_revision: 1, status: "SAVED_DISABLED", last_event_id: "fixture-saved-plan-event-701", reason: "Saved setup",
    created_unix: 1800000000, updated_unix: 1800000001, runtime_apply: false, editable: true };
  const f = await fixture(page, { savedPlan });
  let releaseCapabilities!: () => void, capabilityRequests = 0;
  const capabilityGate = new Promise<void>(resolve => { releaseCapabilities = resolve; });
  await page.route("**/native-inbound-plans/capabilities", async route => {
    capabilityRequests++; await capabilityGate; await route.fallback();
  });
  await page.route("**/api/v1/workspace/clients?*", route => route.fulfill({ json: { clients: [
    { id: "fixture-client", name: "Fixture Client", representative_id: "owner", archived: false, deployment_state: "pending_node_apply" },
  ] } }));
  try {
    await page.goto("/inbounds?inbound=701&tab=overview");
    await expect(page.getByRole("heading", { name: "Previously saved setup", exact: true })).toBeVisible();
    await expect.poll(() => capabilityRequests).toBeGreaterThan(0);
    await expect(page.getByRole("button", { name: "Copy settings", exact: true })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Edit profile", exact: true })).toBeDisabled();
    await page.locator(".inbound-more-actions summary").click();
    await page.getByLabel("Reason for change", { exact: true }).fill("Cannot mutate until ownership is known");
    await expect(page.getByRole("button", { name: "Enable Inbound", exact: true })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Delete safely (archive)", exact: true })).toBeDisabled();
    await page.getByRole("tab", { name: "Routes", exact: true }).click();
    await expect(page.getByRole("button", { name: "Add Node target", exact: true })).toBeDisabled();
    await page.getByRole("tab", { name: "Clients", exact: true }).click();
    await page.locator(".native-client-row").filter({ hasText: "Fixture Client" }).getByRole("checkbox").check();
    await expect(page.getByRole("button", { name: "Attach selected", exact: true })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Detach selected", exact: true })).toBeDisabled();
    expect(f.calls).toHaveLength(0);
    await page.getByRole("tab", { name: "Routes", exact: true }).click();
    releaseCapabilities();
    await expect(page.getByRole("button", { name: "Open saved setup", exact: true })).toBeEnabled();
    await expect(page.getByRole("button", { name: "Add Node target", exact: true })).toHaveCount(0);
    await page.getByRole("tab", { name: "Overview", exact: true }).click();
    await expect(page.getByRole("button", { name: "Edit profile", exact: true })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Enable Inbound", exact: true })).toHaveCount(0);
    await page.getByRole("tab", { name: "Clients", exact: true }).click();
    await expect(page.getByRole("button", { name: "Attach selected", exact: true })).toHaveCount(0);
    await expect(page.getByText("This setup is paused. Client delivery follows separate activation.", { exact: true })).toBeVisible();
    expect(f.calls).toHaveLength(0);
  } finally { releaseCapabilities(); }
});

test("four editor sections have one editable location per setting and save one atomic plan", async ({ page }, testInfo) => {
  const f = await fixture(page), dialog = await openCreation(page);
  await expect(dialog.getByRole("tab")).toHaveText(["Connection", "Dark Swap", "Advanced", "Review"]);
  for (const name of ["Profile", "Network", "Security", "Nodes & routes", "Hosts"]) {
    await expect(dialog.getByRole("tab", { name, exact: true, includeHidden: true })).toHaveCount(0);
  }
  await profile(dialog, "One editor, every supported setting");
  await dialog.getByRole("radio", { name: "gRPC / REALITY", exact: true }).check();
  await dialog.getByLabel("gRPC service", { exact: true }).fill("completepro");
  await dialog.getByLabel("REALITY target (host:port)", { exact: true }).fill("example.net:443");
  await dialog.getByLabel("Short IDs", { exact: true }).fill("aabbccdd");
  const connection = dialog.locator("#creation-panel-connection");
  for (const label of [/^Inbound name/, /^gRPC service/, /^REALITY target/, /^Server names/, /^Short IDs/]) {
    // getByLabel counts DOM controls in hidden panels too: the previous
    // duplicated editor must fail even when only one copy is visible.
    await expect(dialog.getByLabel(label)).toHaveCount(1);
    await expect(connection.getByLabel(label)).toHaveCount(1);
  }
  await expect(dialog.getByRole("radio", { name: /^TCP \/ REALITY/, includeHidden: true })).toHaveCount(1);
  await expect(dialog.getByRole("radio", { name: /^gRPC \/ REALITY/, includeHidden: true })).toHaveCount(1);
  await expect(connection.locator("[data-connection-group]")).toHaveCount(3);
  await page.screenshot({ path: testInfo.outputPath("connection-en-desktop.png"), fullPage: true });
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await swap(dialog);
  const routes = dialog.locator("#creation-panel-swap");
  for (const label of ["Destination listening IP: de", "Direct port: de", "Direct Hosts: de", "Source Node: de", "Source relay port: de", "External entry address: de", "External entry port: de", "Destination exit port: de"]) {
    await expect(dialog.getByLabel(label, { exact: true })).toHaveCount(1);
    await expect(routes.getByLabel(label, { exact: true })).toHaveCount(1);
  }
  await dialog.getByRole("tab", { name: "Advanced", exact: true }).click();
  await expect(dialog.getByRole("heading", { name: "TCP fallbacks 0", exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Add fallback", exact: true })).toBeDisabled();
  await review(dialog, "Editor stages retain every route");
  const summary = dialog.locator('[data-review-section="connection"]');
  for (const [field, value] of [["name", "One editor, every supported setting"], ["transport", "gRPC"], ["grpcService", "completepro"], ["realityTarget", "example.net:443"], ["serverNames", "example.org"], ["shortIDs", "aabbccdd"]]) {
    await expect(summary.locator(`[data-review-field="${field}"]`)).toContainText(field === "transport" ? /grpc/i : value);
  }
  await expect(dialog.locator("[data-review-section] input, [data-review-section] select, [data-review-section] textarea")).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("review-en-desktop.png"), fullPage: true });
  await dialog.getByRole("button", { name: "Edit connection", exact: true }).click();
  await expect(dialog.getByRole("tab", { name: "Connection", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(dialog.getByLabel("gRPC service", { exact: true })).toHaveValue("completepro");
  await review(dialog, "Editor stages retain every route");
  await check(dialog);
  const payload = f.calls.find(call => call.path === api + "/preview")?.body;
  expect(payload?.profile).toMatchObject({ transport: "grpc", grpc_service: "completepro", reality_target: "example.net:443", short_ids: ["aabbccdd"] });
  expect(payload?.nodes[0]).toMatchObject({ direct_hosts: ["de.example.org"], swap_enabled: true });
  await save(dialog);
  expect(f.getSaved()?.status).toBe("SAVED_DISABLED");
});


async function advanced(dialog: Locator, section: "network" | "security") {
  await dialog.getByRole("tab", { name: "Connection", exact: true }).click();
  const group = dialog.locator(`[data-connection-group="${section}"]`);
  if (await group.getAttribute("open") === null) await group.locator(":scope > summary").click();
  const disclosure = group.locator(".inbound-profile-advanced").filter({ hasText: section === "network" ? "Advanced gRPC settings" : "Advanced REALITY settings" });
  if (await disclosure.getAttribute("open") === null) await disclosure.locator("summary").click();
  return disclosure;
}

test("advanced gRPC and REALITY options survive exact retry, reopen and profile edits without rotating identity", async ({ page }, testInfo) => {
  const f = await fixture(page, { failSaveOnce: true }), dialog = await openCreation(page);
  await profile(dialog, "Advanced connection");
  await dialog.getByRole("tab", { name: "Connection", exact: true }).click();
  await dialog.getByRole("radio", { name: "gRPC / REALITY", exact: true }).check();
  await dialog.getByLabel("gRPC service", { exact: true }).fill("advanced-grpc");
  await advanced(dialog, "network");
  await dialog.getByLabel("gRPC Authority", { exact: true }).fill("edge.example.org:8443");
  await dialog.getByRole("checkbox", { name: "gRPC MultiMode", exact: true }).check();
  await dialog.getByRole("radio", { name: "TCP / REALITY", exact: true }).check();
  await dialog.getByRole("radio", { name: "gRPC / REALITY", exact: true }).check();
  await advanced(dialog, "network");
  await expect(dialog.getByLabel("gRPC Authority", { exact: true })).toHaveValue("edge.example.org:8443");
  await expect(dialog.getByRole("checkbox", { name: "gRPC MultiMode", exact: true })).toBeChecked();
  await advanced(dialog, "security");
  await dialog.getByLabel("REALITY Xver", { exact: true }).selectOption("2");
  await dialog.getByLabel("Maximum time difference (ms)", { exact: true }).fill("60000");
  await dialog.getByLabel("Minimum client version", { exact: true }).fill("1.8.0");
  await dialog.getByLabel("Maximum client version", { exact: true }).fill("25.9.11");
  await dialog.getByRole("checkbox", { name: "REALITY Show", exact: true }).check();
  await page.screenshot({ path: testInfo.outputPath("advanced-reality-en-desktop.png"), fullPage: true });
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await review(dialog);
  for (const [field, value] of [["grpcAuthority", "edge.example.org:8443"], ["realityXver", "2"], ["realityMaxTimeDiff", "60000"], ["realityMinClientVer", "1.8.0"], ["realityMaxClientVer", "25.9.11"]]) {
    await expect(dialog.locator(`[data-review-option="${field}"]`)).toContainText(value);
  }
  await expect(dialog.locator('[data-review-option="grpcMultiMode"]')).toBeVisible();
  await expect(dialog.locator('[data-review-option="realityShow"]')).toBeVisible();
  await check(dialog);
  const options = { grpc_authority: "edge.example.org:8443", grpc_multi_mode: true, reality_xver: 2,
    reality_max_time_diff: 60000, reality_min_client_ver: "1.8.0", reality_max_client_ver: "25.9.11", reality_show: true };
  expect(f.calls[0].body.profile.options).toEqual(options);
  await dialog.getByRole("button", { name: "Save paused setup", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Retry the same save", exact: true })).toBeEnabled();
  await dialog.getByRole("button", { name: "Retry the same save", exact: true }).click();
  await expect(dialog).toBeHidden();
  expect(f.calls[2].raw).toBe(f.calls[1].raw);
  expect(f.getSaved()?.profile.options).toEqual(options);
  const identity = f.getSaved()!.profile.id, publicKey = f.getSaved()!.profile.public_key;
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "Edit Inbound setup", exact: true });
  await advanced(edit, "security");
  await expect(edit.getByLabel("Maximum time difference (ms)", { exact: true })).toHaveValue("60000");
  await expect(edit.getByLabel("Minimum client version", { exact: true })).toHaveValue("1.8.0");
  await expect(edit.getByRole("checkbox", { name: "REALITY Show", exact: true })).toBeChecked();
  await advanced(edit, "network");
  await expect(edit.getByLabel("gRPC Authority", { exact: true })).toHaveValue("edge.example.org:8443");
  await edit.getByRole("radio", { name: "TCP / REALITY", exact: true }).check();
  await review(edit, "Use TCP and retain REALITY settings"); await check(edit); await save(edit);
  expect(f.calls[3].body.profile.options).toEqual({ reality_xver: 2, reality_max_time_diff: 60000,
    reality_min_client_ver: "1.8.0", reality_max_client_ver: "25.9.11", reality_show: true });
  expect(f.calls[3].body.profile.grpc_service).toBe("");
  expect(f.getSaved()).toMatchObject({ plan_revision: 2, inbound_revision: 2, profile: { id: identity, public_key: publicKey, revision: 2 } });
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  await advanced(edit, "security");
  await edit.getByLabel("REALITY Xver", { exact: true }).selectOption("0");
  await edit.getByLabel("Maximum time difference (ms)", { exact: true }).fill("0");
  await edit.getByLabel("Minimum client version", { exact: true }).fill("");
  await edit.getByLabel("Maximum client version", { exact: true }).fill("");
  await edit.getByRole("checkbox", { name: "REALITY Show", exact: true }).uncheck();
  await review(edit, "Clear advanced values explicitly"); await check(edit); await save(edit);
  expect(f.calls[5].body.profile.options).toEqual({});
  expect(f.calls[6].body.profile.options).toEqual({});
  expect(f.getSaved()?.profile.options).toBeUndefined();
  expect(f.getSaved()?.inbound_revision).toBe(3);
});

for (const change of ["missing", "altered"] as const) test(`a preview with ${change} advanced options cannot authorize saving`, async ({ page }) => {
  const f = await fixture(page, { tamperPreview: plan => {
    if (change === "missing") delete plan.profile.options;
    else plan.profile.options!.reality_max_time_diff = 1;
  } });
  const dialog = await openCreation(page); await profile(dialog);
  await advanced(dialog, "security");
  await dialog.getByLabel("Maximum time difference (ms)", { exact: true }).fill("1000");
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org"); await review(dialog);
  await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("could not validate this setup");
  await expect(dialog.getByRole("button", { name: "Save paused setup", exact: true })).toBeDisabled();
  expect(f.calls).toHaveLength(1); expect(f.getSaved()).toBeUndefined();
});

test("advanced controls require the server capability while default requests remain compatible", async ({ page }) => {
  const f = await fixture(page, { profileOptions: false }), dialog = await openCreation(page); await profile(dialog);
  await advanced(dialog, "security");
  await expect(dialog.getByLabel("Maximum time difference (ms)", { exact: true })).toBeDisabled();
  await expect(dialog.getByRole("status").filter({ hasText: "require an updated server" })).toBeVisible();
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org"); await review(dialog); await check(dialog); await save(dialog);
  expect(f.calls[0].body.profile).not.toHaveProperty("options");
  expect(f.getSaved()?.profile.enabled).toBe(false);
});

test("invalid advanced versions reveal their hidden security field without sending a request", async ({ page }) => {
  const f = await fixture(page), dialog = await openCreation(page); await profile(dialog);
  await advanced(dialog, "security");
  await dialog.getByLabel("Minimum client version", { exact: true }).fill("25.9.11");
  await dialog.getByLabel("Maximum client version", { exact: true }).fill("1.8.0");
  await dialog.locator(".inbound-profile-advanced").filter({ hasText: "Advanced REALITY settings" }).locator("summary").click();
  await dialog.locator('[data-connection-group="security"] > summary').click();
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org"); await review(dialog);
  await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("tab", { name: "Connection", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(dialog.locator('[data-connection-group="security"]')).toHaveAttribute("open", "");
  await expect(dialog.getByLabel("Maximum client version", { exact: true })).toBeFocused();
  await expect(dialog.getByRole("alert")).toContainText("at least the minimum");
  expect(f.calls).toHaveLength(0);
});

async function clientDefaults(dialog: Locator) {
  await dialog.getByRole("tab", { name: "Connection", exact: true }).click();
  const disclosure = dialog.locator("[data-client-options]");
  if (await disclosure.getAttribute("open") === null) await disclosure.locator(":scope > summary").click();
  return disclosure;
}
async function endpointSettings(dialog: Locator, index = 0) {
  const row = dialog.locator(`[data-node-id="de"] [data-endpoint-index="${index}"]`);
  const details = row.locator(".creation-endpoint-details");
  if (await details.getAttribute("open") === null) await details.locator(":scope > summary").click();
  return row;
}

test("XHTTP, client defaults and rich Hosts survive exact retry, reopen, copy, independent public ports and explicit clear", async ({ page }, testInfo) => {
  const f = await fixture(page, { failSaveOnce: true }), dialog = await openCreation(page);
  await profile(dialog, "XHTTP endpoint setup");
  await dialog.getByLabel("Server names (comma-separated)", { exact: true }).fill("example.org, Alternate.example.org");
  await dialog.getByRole("radio", { name: "XHTTP / REALITY", exact: true }).check();
  await dialog.locator('[data-creation-field="xhttpPath"]').fill("/connect%20path");
  await dialog.locator('[data-creation-field="xhttpHost"]').fill("cdn.example.org");
  await dialog.locator('[data-creation-field="xhttpMode"]').selectOption("stream-one");
  await dialog.locator('[data-creation-field="xhttpPadding"]').fill("128-256");
  await dialog.getByRole("radio", { name: "TCP / REALITY", exact: true }).check();
  await dialog.getByRole("radio", { name: "XHTTP / REALITY", exact: true }).check();
  await expect(dialog.locator('[data-creation-field="xhttpPath"]')).toHaveValue("/connect%20path");
  await clientDefaults(dialog);
  await dialog.locator('[data-creation-field="clientFingerprint"]').selectOption("firefox");
  await dialog.locator('[data-creation-field="clientSpiderX"]').fill("/assets?theme=dark");
  await destination(dialog, "de", "203.0.113.10", "10443", "edge.example.org");
  await dialog.getByRole("button", { name: "Endpoint settings: Direct: de", exact: true }).click();
  await expect(dialog.getByLabel("Direct Hosts: de", { exact: true })).toHaveCount(0);
  await dialog.locator('[data-creation-field="directEndpoints.0.address"]').fill("2001:0db8:0000:0000:0000:0000:0000:0001");
  await dialog.locator('[data-creation-field="directEndpoints.0.port"]').fill("443");
  await endpointSettings(dialog);
  await dialog.locator('[data-creation-field="directEndpoints.0.label"]').fill("لبهٔ عمومی");
  await dialog.locator('[data-creation-field="directEndpoints.0.sni"]').selectOption("Alternate.example.org");
  await dialog.locator('[data-creation-field="directEndpoints.0.fingerprint"]').selectOption("edge");
  await dialog.locator('[data-creation-field="directEndpoints.0.spiderX"]').fill("/mobile?theme=light");
  await dialog.getByRole("button", { name: "Add Host: Direct: de", exact: true }).click();
  await dialog.locator('[data-creation-field="directEndpoints.1.address"]').fill("standby.example.org");
  await dialog.locator('[data-creation-field="directEndpoints.1.enabled"]').uncheck();
  await dialog.getByRole("checkbox", { name: "Tunnel: de", exact: true }).check();
  await dialog.getByLabel("Tunnel port: de", { exact: true }).fill("1185");
  await dialog.getByLabel("Tunnel Hosts: de", { exact: true }).fill("tunnel.example.org");
  await expect(dialog.getByRole("checkbox", { name: "Dark Swap: de", exact: true })).not.toBeChecked();
  await page.screenshot({ path: testInfo.outputPath("rich-endpoints-en-desktop.png"), fullPage: true });
  await review(dialog); await check(dialog);
  const expectedOptions = { xhttp: { path: "/connect%20path", host: "cdn.example.org", mode: "stream-one", x_padding_bytes: "128-256" }, client: { fingerprint: "firefox", spider_x: "/assets?theme=dark" } };
  const expectedEndpoints = [
    { address: "2001:db8::1", port: 443, enabled: true, label: "لبهٔ عمومی", sni: "Alternate.example.org", fingerprint: "edge", spider_x: "/mobile?theme=light" },
    { address: "standby.example.org", port: 0, enabled: false },
  ];
  expect(f.calls[0].body.profile).toMatchObject({ transport: "xhttp", grpc_service: "", options: expectedOptions });
  expect(f.calls[0].body.nodes[0]).toMatchObject({ direct_port: 10443, direct_hosts: ["2001:db8::1"], direct_endpoints: expectedEndpoints, tunnel_port: 1185, tunnel_hosts: ["tunnel.example.org"], swap_enabled: false });
  expect(f.calls[0].body.nodes[0]).not.toHaveProperty("tunnel_endpoints");
  const reviewPath = dialog.locator('[data-review-node="de"] [data-review-path="direct"]');
  await expect(reviewPath.locator('[data-review-endpoint="1"]')).toContainText("Disabled · settings saved");
  await dialog.locator("[data-checked-endpoints] > summary").click();
  await expect(dialog.locator('[data-checked-endpoint="0"]')).toContainText("2001:db8::1");
  await expect(dialog.locator('[data-checked-endpoint="0"]')).toContainText("Alternate.example.org");
  await expect(dialog.locator('[data-checked-endpoint="1"]')).toContainText("firefox");
  await dialog.getByRole("button", { name: "Save paused setup", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Retry the same save", exact: true })).toBeEnabled();
  await dialog.getByRole("button", { name: "Retry the same save", exact: true }).click();
  await expect(dialog).toBeHidden();
  expect(f.calls[1].raw).toBe(f.calls[0].raw); expect(f.calls[2].raw).toBe(f.calls[1].raw);
  const identity = f.getSaved()!.profile.id, publicKey = f.getSaved()!.profile.public_key;
  expect(f.getSaved()!.profile.options).toEqual(expectedOptions);
  expect(f.getSaved()!.nodes[0].direct_endpoints).toEqual(expectedEndpoints);
  await page.getByRole("button", { name: "Copy settings", exact: true }).click();
  const copy = page.getByRole("dialog", { name: "Create Inbound", exact: true });
  await expect(copy.locator('[data-creation-field="xhttpPath"]')).toHaveValue("/connect%20path");
  await clientDefaults(copy); await expect(copy.locator('[data-creation-field="clientFingerprint"]')).toHaveValue("firefox");
  await copy.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  await expect(copy.locator('[data-creation-field="node"]:checked')).toHaveCount(0);
  await copy.getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "Edit Inbound setup", exact: true });
  await expect(edit.locator('[data-creation-field="xhttpMode"]')).toHaveValue("stream-one");
  await review(edit); await expect(edit.locator('[data-review-field="publicKey"]')).toContainText(publicKey);
  await edit.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  await expect(edit.locator('[data-creation-field="directEndpoints.1.enabled"]')).not.toBeChecked();
  await edit.locator('[data-creation-field="directEndpoints.0.port"]').fill("8443");
  await review(edit, "Change public port only"); await check(edit); await save(edit);
  expect(f.getSaved()).toMatchObject({ plan_revision: 2, inbound_revision: 1, profile: { id: identity, public_key: publicKey, enabled: false } });
  expect(f.getSaved()!.nodes[0].direct_endpoints?.[0].port).toBe(8443);
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  await edit.locator('.inbound-network-options button').click();
  await clientDefaults(edit); await edit.locator('[data-client-options] button').click();
  await edit.getByRole("radio", { name: "TCP / REALITY", exact: true }).check();
  await edit.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  page.once("dialog", confirmation => confirmation.accept());
  await edit.getByRole("button", { name: "Use simple Hosts", exact: true }).click();
  await expect(edit.getByLabel("Direct Hosts: de", { exact: true })).toHaveValue("2001:db8::1");
  await review(edit, "Explicitly clear extended settings"); await check(edit);
  expect(f.calls[5].body.profile.options).toEqual({ xhttp: {}, client: {} });
  expect(f.calls[5].body.nodes[0].direct_endpoints).toEqual([]);
  await save(edit);
  expect(f.getSaved()!.profile.options).toBeUndefined(); expect(f.getSaved()!.nodes[0].direct_endpoints).toBeUndefined();
  expect(f.getSaved()!.profile.public_key).toBe(publicKey);
});

for (const change of ["missing rows", "changed port", "unknown field"] as const) test(`rich endpoint ${change} cannot authorize a save`, async ({ page }) => {
  const f = await fixture(page, { tamperPreview: plan => {
    if (change === "missing rows") delete plan.nodes[0].direct_endpoints;
    else if (change === "changed port") plan.nodes[0].direct_endpoints![0].port = 443;
    else Object.assign(plan.nodes[0].direct_endpoints![0], { extra: "not-supported" });
  } });
  const dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await dialog.getByRole("button", { name: "Endpoint settings: Direct: de", exact: true }).click();
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("could not validate");
  await expect(dialog.getByRole("button", { name: "Save paused setup", exact: true })).toBeDisabled();
  expect(f.calls).toHaveLength(1); expect(f.getSaved()).toBeUndefined();
});

for (const change of ["changed port", "missing row", "unexpected credential"] as const) test(`server-derived endpoint preview with ${change} is rejected`, async ({ page }) => {
  const f = await fixture(page, { tamperEndpoints: rows => {
    if (change === "changed port") rows[0].port = 443;
    else if (change === "missing row") rows.pop();
    else Object.assign(rows[0], { uuid: "unexpected" });
  } });
  const dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org"); await review(dialog);
  await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("could not validate");
  await expect(dialog.locator("[data-checked-endpoints]")).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "Save paused setup", exact: true })).toBeDisabled();
  expect(f.calls).toHaveLength(1);
});

test("network and endpoint validation reveals the owning group and exact nested field without a request", async ({ page }) => {
  const f = await fixture(page), dialog = await openCreation(page); await profile(dialog);
  await dialog.getByRole("radio", { name: "XHTTP / REALITY", exact: true }).check();
  await dialog.locator('[data-creation-field="xhttpHost"]').fill("cdn.example.org:443");
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org"); await review(dialog);
  await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.locator('[data-creation-field="xhttpHost"]')).toBeFocused();
  await dialog.locator('[data-creation-field="xhttpHost"]').fill("cdn.example.org");
  await dialog.locator('[data-creation-field="xhttpPath"]').fill("/bad\u0081path");
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.locator('[data-creation-field="xhttpPath"]')).toBeFocused();
  await dialog.locator('[data-creation-field="xhttpPath"]').fill("/valid%20path");
  await clientDefaults(dialog); await dialog.locator('[data-creation-field="clientSpiderX"]').fill("/crawl?%70=1");
  await dialog.locator("[data-client-options] > summary").click();
  await dialog.locator('[data-connection-group="security"] > summary').click();
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.locator('[data-creation-field="clientSpiderX"]')).toBeFocused();
  await dialog.locator('[data-creation-field="clientSpiderX"]').fill("/crawl?theme=dark");
  await dialog.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  await dialog.getByRole("button", { name: "Endpoint settings: Direct: de", exact: true }).click();
  await dialog.locator('[data-creation-field="directEndpoints.0.port"]').fill("65536");
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.locator('[data-creation-field="directEndpoints.0.port"]')).toBeFocused();
  await dialog.locator('[data-creation-field="directEndpoints.0.port"]').fill("443");
  await dialog.getByRole("button", { name: "Add Host: Direct: de", exact: true }).click();
  await dialog.locator('[data-creation-field="directEndpoints.1.address"]').fill("de.example.org");
  await dialog.locator('[data-creation-field="directEndpoints.1.port"]').fill("443");
  await dialog.locator('[data-creation-field="directEndpoints.1.enabled"]').uncheck();
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.locator('[data-creation-field="directEndpoints.1.address"]')).toBeFocused();
  await expect(dialog.getByRole("alert")).toContainText("duplicate another Host");
  expect(f.calls).toHaveLength(0);
});

for (const missing of ["capabilities", "plan scope"] as const) test(`extended network controls require ${missing} while simple Hosts stay compatible`, async ({ page }) => {
  const f = await fixture(page, missing === "capabilities" ? { profileXHTTP: false, profileClient: false, endpointOptions: false } : { networkScope: false, endpointOptions: false });
  const dialog = await openCreation(page); await profile(dialog);
  await expect(dialog.getByRole("radio", { name: "XHTTP / REALITY", exact: true })).toHaveCount(0);
  await expect(dialog.locator("[data-client-options]")).toHaveCount(0);
  await destination(dialog, "de", "203.0.113.10", "10443", "2001:0db8::1\nde.example.org");
  await expect(dialog.getByRole("button", { name: "Endpoint settings: Direct: de", exact: true })).toHaveCount(0);
  await review(dialog); await check(dialog); await save(dialog);
  expect(f.calls[0].body.profile).not.toHaveProperty("options");
  expect(f.calls[0].body.nodes[0].direct_hosts).toEqual(["2001:db8::1", "de.example.org"]);
  expect(f.calls[0].body.nodes[0]).not.toHaveProperty("direct_endpoints");
});

for (const language of ["en", "fa"] as const) test(`XHTTP and rich Hosts stay contained and accessible at 320px in ${language}`, async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 780 });
  await fixture(page, { language }); const dialog = await openCreation(page, language);
  await dialog.locator('[data-creation-field="name"]').fill("Mobile network setup");
  await dialog.locator('[data-creation-field="realityTarget"]').fill("example.org:443");
  await dialog.locator('[data-creation-field="serverNames"]').fill("example.org");
  await dialog.getByRole("radio", { name: "XHTTP / REALITY", exact: true }).check();
  await dialog.locator('[data-creation-field="xhttpPath"]').fill("/mobile");
  await dialog.locator('[data-creation-field="xhttpMode"]').selectOption("packet-up");
  await dialog.locator("[data-client-options] > summary").click();
  await dialog.locator('[data-creation-field="clientFingerprint"]').selectOption("firefox");
  await page.screenshot({ path: testInfo.outputPath(`xhttp-${language}-320.png`) });
  await dialog.getByRole("tab", { name: language === "fa" ? "دارک‌سواپ" : "Dark Swap", exact: true }).click();
  await dialog.locator('[data-node-id="de"] [data-creation-field="node"]').check();
  await dialog.locator('[data-creation-field="listenIP"]').fill("203.0.113.10");
  await dialog.locator('[data-creation-field="directPort"]').fill("10443");
  await dialog.locator('[data-creation-field="directHosts"]').fill("de.example.org");
  await dialog.locator('[data-creation-field="directEndpoints"]').click();
  await dialog.locator('[data-creation-field="directEndpoints.0.port"]').fill("443");
  await dialog.locator(".creation-endpoint-details > summary").click();
  await dialog.locator('[data-creation-field="directEndpoints.0.label"]').fill("نام نقطهٔ اتصال موبایل");
  await dialog.locator('[data-creation-field="directEndpoints.0.fingerprint"]').selectOption("edge");
  await expect(dialog.locator('[data-creation-field="swapEnabled"]')).not.toBeChecked();
  await expect.poll(() => dialog.locator(".creation-scroll").evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  await expect(dialog.locator(".creation-tabs [role=tab]")).toHaveCount(4);
  for (const tab of await dialog.locator(".creation-tabs [role=tab]").all()) {
    const bounds = await tab.boundingBox(); expect(bounds && bounds.x >= 0 && bounds.x + bounds.width <= 321).toBe(true);
  }
  await expect(dialog.locator('[data-creation-field="directEndpoints.0.address"]')).toHaveCount(1);
  await page.screenshot({ path: testInfo.outputPath(`rich-hosts-${language}-320.png`) });
  await dialog.getByRole("tab", { name: language === "fa" ? "بازبینی" : "Review", exact: true }).click();
  await dialog.locator('[data-creation-field="reason"]').fill("Mobile endpoint review");
  await dialog.getByRole("button", { name: language === "fa" ? "بررسی کامل تنظیمات" : "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("button", { name: language === "fa" ? "ذخیرهٔ تنظیمات غیرفعال" : "Save paused setup", exact: true })).toBeEnabled();
});

for (const language of ["en", "fa"] as const) test(`320px ${language} footer navigation keeps the active stage in its scroll viewport`, async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 760 });
  await fixture(page, { language }); const dialog = await openCreation(page, language);
  const next = language === "fa" ? "ادامه" : "Continue", back = language === "fa" ? "قبلی" : "Back";
  const tabVisible = async () => {
    await expect.poll(() => dialog.locator('.creation-tabs [aria-selected="true"]').evaluate(element => {
      const selected = element.getBoundingClientRect(), viewport = element.parentElement!.getBoundingClientRect();
      return selected.left >= viewport.left - 1 && selected.right <= viewport.right + 1;
    })).toBe(true);
  };
  await expect(dialog.getByRole("tab")).toHaveText(language === "fa" ? ["اتصال", "دارک‌سواپ", "پیشرفته", "بازبینی"] : ["Connection", "Dark Swap", "Advanced", "Review"]);
  for (let stage = 0; stage < 3; stage++) { await tabVisible(); await dialog.getByRole("button", { name: next, exact: true }).click(); }
  await tabVisible();
  for (let stage = 3; stage > 0; stage--) { await dialog.getByRole("button", { name: back, exact: true }).click(); await tabVisible(); }
  await dialog.getByRole("tab", { name: language === "fa" ? "دارک‌سواپ" : "Dark Swap", exact: true }).click();
  const selectDestination = dialog.getByRole("checkbox", { name: language === "fa" ? "انتخاب مقصد: Germany" : "Select destination: Germany", exact: true });
  await expect(selectDestination).toBeEnabled();
  await expect(dialog.getByRole("tab", { name: language === "fa" ? "دارک‌سواپ" : "Dark Swap", exact: true })).toHaveAttribute("aria-selected", "true");
  await selectDestination.check();
  await dialog.getByLabel(language === "fa" ? "IP شنود نود مقصد: de" : "Destination listening IP: de", { exact: true }).fill("203.0.113.10");
  const directPort = dialog.getByLabel(language === "fa" ? "پورت مستقیم: de" : "Direct port: de", { exact: true });
  await directPort.fill("10443");
  await dialog.getByLabel(language === "fa" ? "هاست‌های مستقیم: de" : "Direct Hosts: de", { exact: true }).fill("de.example.org");
  await dialog.getByRole("checkbox", { name: language === "fa" ? "تونل: de" : "Tunnel: de", exact: true }).check();
  const tunnelPort = dialog.getByLabel(language === "fa" ? "پورت تونل: de" : "Tunnel port: de", { exact: true });
  await tunnelPort.fill("1185");
  await dialog.getByLabel(language === "fa" ? "هاست‌های تونل: de" : "Tunnel Hosts: de", { exact: true }).fill("tunnel.example.org");
  await expect(directPort).toHaveValue("10443"); await expect(tunnelPort).toHaveValue("1185");
  await expect(dialog.getByRole("checkbox", { name: language === "fa" ? "دارک‌سواپ: de" : "Dark Swap: de", exact: true })).not.toBeChecked();
  await directPort.scrollIntoViewIfNeeded();
  await page.screenshot({ path: testInfo.outputPath(`dark-swap-${language}-320-direct.png`), fullPage: true });
  await tunnelPort.scrollIntoViewIfNeeded();
  await page.screenshot({ path: testInfo.outputPath(`dark-swap-${language}-320-tunnel.png`), fullPage: true });
  await tabVisible();
  await dialog.getByRole("tab", { name: language === "fa" ? "اتصال" : "Connection", exact: true }).click();
  await dialog.locator(".inbound-profile-advanced").filter({ hasText: language === "fa" ? "تنظیمات پیشرفتهٔ REALITY" : "Advanced REALITY settings" }).locator("summary").click();
  const maxTime = dialog.getByLabel(language === "fa" ? "حداکثر اختلاف زمان (میلی‌ثانیه)" : "Maximum time difference (ms)", { exact: true });
  await maxTime.fill("60000");
  await expect(maxTime).toBeInViewport();
  const widths = await dialog.evaluate(element => ({ client: element.clientWidth, scroll: element.scrollWidth }));
  expect(widths.scroll).toBeLessThanOrEqual(widths.client + 1);
  await page.screenshot({ path: testInfo.outputPath(`advanced-reality-${language}-320.png`), fullPage: true });
});

async function traffic(dialog: Locator) {
  await dialog.getByRole("tab", { name: "Advanced", exact: true }).click();
  return dialog.getByRole("region", { name: "Sniffing", exact: true });
}
async function sniffingDetails(dialog: Locator) {
  const card = await traffic(dialog), details = card.locator("details").first();
  if (await details.getAttribute("open") === null) await details.locator(":scope > summary").click();
  return card;
}
async function exclusions(dialog: Locator) {
  const card = await sniffingDetails(dialog), details = card.locator(".inbound-traffic-exclusions");
  if (await details.getAttribute("open") === null) await details.locator("summary").click();
}
async function fallbackRow(dialog: Locator, index: number) {
  await traffic(dialog); const row = dialog.locator(".inbound-fallback-row").nth(index);
  if (await row.getAttribute("open") === null) await row.locator("summary").click();
  return row;
}

test("Sniffing and TCP fallbacks survive exact retry, reopen, disabled edits, copy and explicit reset", async ({ page }, testInfo) => {
  test.setTimeout(30000);
  const f = await fixture(page, { failSaveOnce: true }), dialog = await openCreation(page);
  await profile(dialog, "Traffic authoring"); await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await traffic(dialog); await dialog.getByRole("checkbox", { name: "Enable Sniffing", exact: true }).check();
  await dialog.getByRole("checkbox", { name: "QUIC", exact: true }).check();
  await dialog.getByRole("checkbox", { name: "FakeDNS", exact: true }).check();
  await dialog.getByRole("checkbox", { name: "Read metadata only", exact: true }).check();
  await exclusions(dialog);
  await dialog.getByLabel("Excluded domains", { exact: true }).fill("domain:Example.NET\nregexp:(?i)^internal\\.\nkeyword:keep this");
  await dialog.getByLabel("Excluded IPs or CIDRs", { exact: true }).fill("192.0.2.129/24\n2001:0db8:0000::1");
  await dialog.getByRole("button", { name: "Add fallback", exact: true }).click();
  await expect(dialog.getByLabel("Fallback 1 Destination", { exact: true })).toBeFocused();
  await dialog.getByLabel("Fallback 1 Destination", { exact: true }).fill("127.0.0.1:8080");
  await dialog.getByLabel("Fallback 1 Path", { exact: true }).fill("/health");
  await expect(dialog.getByLabel("Fallback 1 ALPN", { exact: true })).toHaveCount(0);
  await dialog.getByLabel("Fallback 1 PROXY protocol", { exact: true }).selectOption("2");
  await page.screenshot({ path: testInfo.outputPath("traffic-options-en-desktop.png"), fullPage: true });
  await review(dialog); await check(dialog);
  const sniffing = { enabled: true, metadata_only: true, route_only: true, dest_override: ["http", "tls", "quic", "fakedns"],
    domains_excluded: ["domain:Example.NET", "regexp:(?i)^internal\\.", "keyword:keep this"], ips_excluded: ["192.0.2.129/24", "2001:0db8:0000::1"] };
  const fallbacks = [{ dest: "127.0.0.1:8080", path: "/health", xver: 2 }];
  expect(f.calls[0].body.profile.options).toEqual({ sniffing, fallbacks });
  await dialog.getByRole("button", { name: "Save paused setup", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Retry the same save", exact: true })).toBeEnabled();
  await dialog.getByRole("button", { name: "Retry the same save", exact: true }).click(); await expect(dialog).toBeHidden();
  expect(f.calls[2].raw).toBe(f.calls[1].raw);
  const identity = f.getSaved()!.profile.id, publicKey = f.getSaved()!.profile.public_key;
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "Edit Inbound setup", exact: true });
  await exclusions(edit);
  await expect(edit.getByLabel("Excluded domains", { exact: true })).toHaveValue(sniffing.domains_excluded.join("\n"));
  await expect(edit.getByLabel("Excluded IPs or CIDRs", { exact: true })).toHaveValue(sniffing.ips_excluded.join("\n"));
  await edit.getByRole("checkbox", { name: "Enable Sniffing", exact: true }).uncheck();
  await expect(edit.getByText("Sniffing is off. Its configured settings are retained.", { exact: true })).toBeVisible();
  await fallbackRow(edit, 0); await expect(edit.getByLabel("Fallback 1 Path", { exact: true })).toHaveValue("/health");
  await review(edit, "Retain settings while Sniffing is disabled");
  const retainedSniffing = edit.locator('[data-review-option="sniffing"]');
  await expect(retainedSniffing).toHaveAttribute("data-sniffing-enabled", "false");
  await expect(retainedSniffing).toHaveAttribute("data-sniffing-retained", "true");
  await expect(retainedSniffing).toContainText("Disabled · settings retained");
  await expect(retainedSniffing).toContainText("domain:Example.NET");
  await expect(edit.locator('[data-review-fallback="0"]')).toContainText("127.0.0.1:8080");
  await expect(edit.locator('[data-review-fallback="0"]')).toContainText("/health");
  await edit.getByRole("button", { name: "Edit advanced settings", exact: true }).click();
  await expect(edit.getByRole("tab", { name: "Advanced", exact: true })).toHaveAttribute("aria-selected", "true");
  await review(edit, "Retain settings while Sniffing is disabled"); await check(edit); await save(edit);
  expect(f.getSaved()?.profile.options).toEqual({ sniffing: { ...sniffing, enabled: false }, fallbacks });
  expect(f.getSaved()).toMatchObject({ plan_revision: 2, inbound_revision: 2, profile: { id: identity, public_key: publicKey, enabled: false } });

  await page.getByRole("button", { name: "Copy settings", exact: true }).click();
  const copy = page.getByRole("dialog", { name: "Create Inbound", exact: true });
  await exclusions(copy); await expect(copy.getByRole("checkbox", { name: "Enable Sniffing", exact: true })).not.toBeChecked();
  await expect(copy.getByRole("checkbox", { name: "QUIC", exact: true })).toBeChecked();
  await expect(copy.getByLabel("Excluded domains", { exact: true })).toHaveValue(sniffing.domains_excluded.join("\n"));
  await fallbackRow(copy, 0); await expect(copy.getByLabel("Fallback 1 Destination", { exact: true })).toHaveValue("127.0.0.1:8080");
  await copy.getByRole("button", { name: "Cancel", exact: true }).click();

  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  await sniffingDetails(edit); await edit.getByRole("button", { name: "Reset Sniffing", exact: true }).click();
  await fallbackRow(edit, 0); await edit.getByRole("button", { name: "Remove fallback 1", exact: true }).click();
  await expect(edit.getByRole("button", { name: "Add fallback", exact: true })).toBeFocused();
  await review(edit, "Clear traffic settings explicitly"); await check(edit); await save(edit);
  expect(f.calls[5].body.profile.options).toEqual({ sniffing: {}, fallbacks: [] });
  expect(f.calls[6].body.profile.options).toEqual({ sniffing: {}, fallbacks: [] });
  expect(f.getSaved()?.profile.options).toBeUndefined();
  expect(f.getSaved()).toMatchObject({ plan_revision: 3, inbound_revision: 3, runtime_apply: false });
  expect(f.calls.every(call => call.path === api || call.path === api + "/preview" || call.path === api + "/" + identity)).toBe(true);
});

test("switching to gRPC retains fallback rows, blocks preview and allows explicit removal", async ({ page }) => {
  const f = await fixture(page), dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await traffic(dialog); await dialog.getByRole("button", { name: "Add fallback", exact: true }).click();
  await dialog.getByLabel("Fallback 1 Destination", { exact: true }).fill("[2001:db8::1]:8443");
  await dialog.getByRole("tab", { name: "Connection", exact: true }).click();
  await dialog.getByRole("radio", { name: "gRPC / REALITY", exact: true }).check();
  await dialog.getByLabel("gRPC service", { exact: true }).fill("grpc-service");
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("tab", { name: "Advanced", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(dialog.getByLabel("Fallback 1 Destination", { exact: true })).toBeFocused();
  await expect(dialog.getByLabel("Fallback 1 Destination", { exact: true })).toHaveValue("[2001:db8::1]:8443");
  expect(f.calls).toHaveLength(0);
  await expect(dialog.getByRole("button", { name: "Add fallback", exact: true })).toBeDisabled();
  await dialog.getByRole("button", { name: "Remove fallback 1", exact: true }).click();
  await review(dialog); await check(dialog); await save(dialog);
  expect(f.calls[0].body.profile.options).toEqual({ fallbacks: [] });
  expect(f.getSaved()?.profile.options).toBeUndefined();
});

test("traffic validation reveals nested exclusions and rejects oversized or duplicate fallback paths without a request", async ({ page }) => {
  const f = await fixture(page), dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await exclusions(dialog); await dialog.getByLabel("Excluded IPs or CIDRs", { exact: true }).fill("geoip:private");
  await dialog.locator(".inbound-traffic-exclusions > summary").click();
  await dialog.getByText("Sniffing settings", { exact: true }).click();
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByLabel("Excluded IPs or CIDRs", { exact: true })).toBeFocused();
  await expect(dialog.getByLabel("Excluded IPs or CIDRs", { exact: true })).toBeVisible();
  expect(f.calls).toHaveLength(0);
  await dialog.getByLabel("Excluded IPs or CIDRs", { exact: true }).fill("::ffff:192.0.2.0/120");
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByLabel("Excluded IPs or CIDRs", { exact: true })).toBeFocused();
  expect(f.calls).toHaveLength(0);
  await dialog.getByLabel("Excluded IPs or CIDRs", { exact: true }).fill("192.0.2.1/24");
  await dialog.getByRole("button", { name: "Add fallback", exact: true }).click();
  await dialog.getByLabel("Fallback 1 Destination", { exact: true }).fill("8080");
  await dialog.getByLabel("Fallback 1 Path", { exact: true }).fill("/" + "é".repeat(28));
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByLabel("Fallback 1 Path", { exact: true })).toBeFocused();
  await expect(dialog.getByRole("alert")).toContainText("at most 55 bytes");
  expect(f.calls).toHaveLength(0);
  await dialog.getByLabel("Fallback 1 Path", { exact: true }).fill("/same");
  await dialog.getByRole("button", { name: "Add fallback", exact: true }).click();
  await dialog.getByLabel("Fallback 2 Destination", { exact: true }).fill("8081");
  await dialog.getByLabel("Fallback 2 Path", { exact: true }).fill("/same");
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByLabel("Fallback 2 Path", { exact: true })).toBeFocused();
  await expect(dialog.getByRole("alert")).toContainText("unique Path");
  expect(f.calls).toHaveLength(0);
});

for (const change of ["missing sniffing", "changed fallback", "unknown nested field", "invalid nested type"] as const) test(`a preview with ${change} cannot authorize a traffic options save`, async ({ page }) => {
  const f = await fixture(page, { tamperPreview: plan => {
    if (change === "missing sniffing") delete plan.profile.options!.sniffing;
    else if (change === "changed fallback") plan.profile.options!.fallbacks![0].dest = "9090";
    else if (change === "unknown nested field") Object.assign(plan.profile.options!.sniffing!, { extra: true });
    else Object.assign(plan.profile.options!.sniffing!, { dest_override: "http" });
  } });
  const dialog = await openCreation(page); await profile(dialog); await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await traffic(dialog); await dialog.getByRole("checkbox", { name: "Enable Sniffing", exact: true }).check();
  await dialog.getByRole("button", { name: "Add fallback", exact: true }).click();
  await dialog.getByLabel("Fallback 1 Destination", { exact: true }).fill("8080");
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("could not validate this setup");
  await expect(dialog.getByRole("button", { name: "Save paused setup", exact: true })).toBeDisabled();
  expect(f.calls).toHaveLength(1); expect(f.getSaved()).toBeUndefined();
});

for (const missing of ["capabilities", "plan scope"] as const) test(`traffic options require ${missing} while untouched defaults remain compatible`, async ({ page }) => {
  const f = await fixture(page, missing === "capabilities" ? { profileSniffing: false, profileFallbacks: false } : { trafficScope: false });
  const dialog = await openCreation(page); await profile(dialog); await traffic(dialog);
  await expect(dialog.getByRole("checkbox", { name: "Enable Sniffing", exact: true })).toBeDisabled();
  await expect(dialog.getByRole("button", { name: "Add fallback", exact: true })).toBeDisabled();
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org"); await review(dialog); await check(dialog); await save(dialog);
  expect(f.calls[0].body.profile).not.toHaveProperty("options");
});

test("a server RE2 validation error reveals Sniffing exclusions in Advanced", async ({ page }) => {
  const f = await fixture(page, { previewError: { status: 400, issue: { field: "profile.options.sniffing", code: "sniffing_invalid" } } });
  const dialog = await openCreation(page); await profile(dialog); await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await exclusions(dialog); await dialog.getByLabel("Excluded domains", { exact: true }).fill("regexp:(?=unsupported)");
  await dialog.locator(".inbound-traffic-exclusions > summary").click(); await dialog.getByText("Sniffing settings", { exact: true }).click();
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("tab", { name: "Advanced", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(dialog.getByLabel("Excluded domains", { exact: true })).toBeFocused();
  await expect(dialog.getByLabel("Excluded domains", { exact: true })).toHaveValue("regexp:(?=unsupported)");
  expect(f.calls).toHaveLength(1); expect(f.getSaved()).toBeUndefined();
});

test("an unexpected saved fallback ALPN is retained and requires explicit clearing", async ({ page }) => {
  const f = await fixture(page), dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org"); await traffic(dialog);
  await dialog.getByRole("button", { name: "Add fallback", exact: true }).click();
  await dialog.getByLabel("Fallback 1 Destination", { exact: true }).fill("8080");
  await review(dialog); await check(dialog); await save(dialog);
  // Simulate a response from an incompatible server; never silently drop its value.
  f.getSaved()!.profile.options!.fallbacks![0].alpn = "h2";
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "Edit Inbound setup", exact: true });
  await review(edit); await edit.getByRole("button", { name: "Check complete setup", exact: true }).click();
  const clear = edit.getByRole("button", { name: "Clear unsupported ALPN", exact: true });
  await expect(clear).toBeFocused();
  await expect(edit.getByRole("alert")).toContainText("Clear the unsupported ALPN explicitly");
  expect(f.calls).toHaveLength(2);
  await clear.click(); await expect(edit.getByLabel("Fallback 1 Destination", { exact: true })).toBeFocused();
  await review(edit); await check(edit); await save(edit);
  expect(f.calls[2].body.profile.options).toEqual({ fallbacks: [{ dest: "8080" }] });
});

for (const language of ["en", "fa"] as const) test(`traffic controls and fallback rows fit 320px ${language} with visible focused controls`, async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 760 }); await fixture(page, { language });
  const dialog = await openCreation(page, language), fa = language === "fa";
  await dialog.getByRole("tab", { name: fa ? "پیشرفته" : "Advanced", exact: true }).click();
  await dialog.getByRole("checkbox", { name: fa ? "فعال‌کردن Sniffing" : "Enable Sniffing", exact: true }).check();
  const exclusionsToggle = dialog.locator(".inbound-traffic-exclusions > summary"); await exclusionsToggle.click();
  await dialog.getByLabel(fa ? "دامنه‌های مستثنا" : "Excluded domains", { exact: true }).fill("domain:example.org");
  await dialog.getByRole("button", { name: fa ? "افزودن Fallback" : "Add fallback", exact: true }).click();
  const dest = dialog.getByLabel(fa ? "Fallback 1 مقصد" : "Fallback 1 Destination", { exact: true });
  await expect(dest).toBeFocused(); await expect(dest).toBeInViewport({ ratio: 1 }); await dest.fill("127.0.0.1:8080");
  const widths = await dialog.evaluate(element => ({ width: element.clientWidth, scroll: element.scrollWidth }));
  expect(widths.scroll).toBeLessThanOrEqual(widths.width + 1);
  await expect.poll(() => dialog.locator('.creation-tabs [aria-selected="true"]').evaluate(element => {
    const tab = element.getBoundingClientRect(), viewport = element.parentElement!.getBoundingClientRect(); return tab.left >= viewport.left - 1 && tab.right <= viewport.right + 1;
  })).toBe(true);
  await dest.evaluate(element => element.scrollIntoView({ block: "center" }));
  await expect(dest).toBeInViewport({ ratio: 1 });
  await page.screenshot({ path: testInfo.outputPath(`traffic-options-${language}-320.png`) });
  await dialog.getByRole("button", { name: fa ? "حذف Fallback 1" : "Remove fallback 1", exact: true }).click();
  await expect(dialog.getByRole("button", { name: fa ? "افزودن Fallback" : "Add fallback", exact: true })).toBeFocused();
});

// These shapes are emitted by real SQLite reservation failures, covered by
// TestCreationPlanExistingCollisionFeedbackNamesActualListener in business.
for (const conflict of [
  { path: "tunnel", field: "tunnel_port", port: 1185, label: "Tunnel port: de" },
  { path: "swap_destination", field: "swap_destination_port", port: 18443, label: "Destination exit port: de" },
]) test(`actual ${conflict.path} reservation feedback focuses its own listener instead of Direct`, async ({ page }) => {
  const f = await fixture(page, { previewError: { status: 409, issue: { node_id: "de", destination_node_id: "de",
    path: conflict.path, port: conflict.port, field: conflict.field, code: "existing_listener_port_collision" } } });
  const dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await dialog.getByRole("checkbox", { name: "Tunnel: de", exact: true }).check();
  await dialog.getByLabel("Tunnel port: de", { exact: true }).fill("1185");
  await dialog.getByLabel("Tunnel Hosts: de", { exact: true }).fill("tunnel.example.org");
  await swap(dialog);
  await destination(dialog, "fr", "203.0.113.20", "11443", "fr.example.org");
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByLabel(conflict.label, { exact: true })).toBeFocused();
  await expect(dialog.getByLabel("Direct port: de", { exact: true })).not.toBeFocused();
  expect(f.calls.map(call => call.path)).toEqual([api + "/preview"]);
  expect(f.getSaved()).toBeUndefined();
});

for (const selectedSource of [false, true]) test(`actual shared relay feedback targets the second destination with source selected=${selectedSource}`, async ({ page }) => {
  const f = await fixture(page, { previewError: { status: 409, issue: { node_id: "nl", destination_node_id: "fr",
    path: "swap_source", port: 16802, field: "swap_source_port", code: "existing_listener_port_collision" } } });
  const dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org"); await swap(dialog);
  await destination(dialog, "fr", "203.0.113.20", "11443", "fr.example.org"); await swap(dialog, "fr", "nl", "203.0.113.30", "fr-entry.example.org");
  await dialog.getByLabel("Source relay port: fr", { exact: true }).fill("16802");
  if (selectedSource) await destination(dialog, "nl", "203.0.113.30", "15443", "nl.example.org");
  else await dialog.getByRole("button", { name: "Configure: Germany", exact: true }).click();
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Configure: France", exact: true })).toHaveAttribute("aria-expanded", "true");
  await expect(dialog.getByLabel("Source relay port: fr", { exact: true })).toBeFocused();
  expect(f.calls.map(call => call.path)).toEqual([api + "/preview"]);
  expect(f.getSaved()).toBeUndefined();
});

for (const field of [
  { wire: "profile.server_names", code: "server_names_invalid", label: "Server names (comma-separated)" },
  { wire: "profile.reality_target", code: "target_invalid", label: "REALITY target (host:port)" },
]) test(`authoritative ${field.wire} feedback reveals Security instead of Name`, async ({ page }) => {
  const f = await fixture(page, { previewError: { status: 422, issue: { field: field.wire, code: field.code } } });
  const dialog = await openCreation(page); await profile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "de.example.org");
  await dialog.getByRole("tab", { name: "Connection", exact: true }).click();
  await dialog.locator('[data-connection-group="security"] > summary').click();
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.locator('[data-connection-group="security"]')).toHaveAttribute("open", "");
  await expect(dialog.getByLabel(field.label, { exact: true })).toBeFocused();
  await expect(dialog.getByLabel("Inbound name", { exact: true })).not.toBeFocused();
  expect(f.calls.map(call => call.path)).toEqual([api + "/preview"]);
});

for (const language of ["en", "fa"] as const) test(`planned Nodes remain discoverable without live assignments in ${language}`, async ({ page }) => {
  const fa = language === "fa";
  if (fa) await page.setViewportSize({ width: 320, height: 760 });
  const planned: CreationPlan = { profile: { id: 799, name: "Discoverable paused setup", transport: "xhttp", grpc_service: "",
    reality_target: "example.org:443", server_names: ["example.org"], short_ids: ["aabb"], public_key: "SYNTHETIC_PUBLIC_KEY",
    enabled: false, archived: false, bound_clients: 0, revision: 1 },
    nodes: [{ node_id: "de", installation_id: nodes[0].installation_id, listen_ip: "203.0.113.10", direct_port: 10443,
      direct_hosts: [], tunnel_port: 0, tunnel_hosts: [], swap_enabled: true, swap_source_node_id: "nl",
      swap_source_installation_id: nodes[2].installation_id, swap_source_listen_ip: "203.0.113.30", swap_source_port: 16801,
      swap_entry_address: "entry.example.org", swap_entry_port: 443, swap_destination_port: 18443 },
    { node_id: "fr", installation_id: nodes[1].installation_id, listen_ip: "203.0.113.20", direct_port: 11443,
      direct_hosts: ["fr.example.org"], tunnel_port: 0, tunnel_hosts: [], swap_enabled: false, swap_source_node_id: "",
      swap_source_installation_id: "", swap_source_listen_ip: "", swap_source_port: 0, swap_entry_address: "", swap_entry_port: 0, swap_destination_port: 0 }],
    plan_revision: 1, inbound_revision: 1, status: "WAITING_HOST", last_event_id: "directory-feedback-fixture", reason: "Discover planned Nodes",
    created_unix: 1800000000, updated_unix: 1800000001, runtime_apply: false, editable: true };
  const f = await fixture(page, { savedPlan: planned, language });
  await page.goto("/inbounds");
  await expect(page.getByRole("button", { name: fa ? "افزودن اینباند" : "Add Inbound", exact: true })).toBeEnabled();
  const row = page.getByRole("row").filter({ has: page.getByRole("button", { name: planned.profile.name, exact: true }) });
  await expect(row.locator("[data-planned-nodes]")).toContainText(fa ? "نودهای طرح" : "Planned Nodes");
  for (const name of ["Germany", "France", "Netherlands"]) await expect(row.locator("[data-planned-nodes]")).toContainText(name);
  await expect(row.locator("[data-assigned-nodes]")).toHaveCount(0);
  await expect(row).toContainText("XHTTP / REALITY");
  await expect(row.locator(".inbound-state")).toHaveText(fa ? "در انتظار هاست" : "Waiting for Host");
  if (fa) await page.getByRole("button", { name: "فیلترها", exact: true }).click();
  await page.getByRole("combobox", { name: fa ? "نود" : "Node", exact: true }).selectOption("nl");
  await page.getByRole("combobox", { name: fa ? "وضعیت" : "Status", exact: true }).selectOption("planned");
  await expect(row).toBeVisible();
  await page.getByRole("combobox", { name: fa ? "ترنسپورت" : "Transport", exact: true }).selectOption("xhttp");
  await expect(row).toBeVisible();
  await page.getByRole("combobox", { name: fa ? "وضعیت" : "Status", exact: true }).selectOption("enabled");
  await expect(row).toHaveCount(0);
  await page.getByRole("combobox", { name: fa ? "وضعیت" : "Status", exact: true }).selectOption("waiting_host");
  await row.getByRole("button", { name: planned.profile.name, exact: true }).click();
  const count = page.locator(".inbound-summary [data-node-summary]");
  await expect(count.locator("strong")).toHaveText("3");
  await expect(count.locator("small")).toHaveText(fa ? "نودهای اختصاص‌یافته: 0" : "Assigned Nodes: 0");
  await expect(page.getByRole("button", { name: fa ? "فعال‌کردن اینباند" : "Enable Inbound", exact: true })).toHaveCount(0);
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
  expect(f.calls).toHaveLength(0);
});

async function tlsProfile(dialog: Locator, transport: "tcp" | "grpc" | "xhttp" | "ws" | "httpupgrade" = "ws", credentials = firstTLS) {
  await dialog.getByLabel("Inbound name", { exact: true }).fill("TLS endpoint setup");
  await dialog.getByRole("radio", { name: "TLS", exact: true }).check();
  await dialog.getByLabel("Server names (comma-separated)", { exact: true }).fill("edge.example.org, cdn.example.org");
  await dialog.getByLabel("TLS certificate (PEM)", { exact: true }).fill(credentials.certificate_pem);
  await dialog.getByLabel("TLS private key (PEM)", { exact: true }).fill(credentials.private_key_pem);
  await dialog.getByRole("radio", { name: { tcp: "TCP", grpc: "gRPC", xhttp: "XHTTP", ws: "WebSocket", httpupgrade: "HTTPUpgrade" }[transport] + " / TLS", exact: true }).check();
  if (transport === "grpc") await dialog.getByLabel("gRPC service", { exact: true }).fill("tls_grpc");
  if (transport === "ws" || transport === "httpupgrade") {
    await dialog.locator(`[data-creation-field="${transport === "ws" ? "websocket" : "httpUpgrade"}Path"]`).fill("/connect");
    await dialog.locator(`[data-creation-field="${transport === "ws" ? "websocket" : "httpUpgrade"}Host"]`).fill("edge.example.org");
  }
}
function savedTLSPlan(): CreationPlan {
  return { profile: { id: 881, name: "Saved TLS setup", security: "tls", transport: "ws", grpc_service: "", reality_target: "", server_names: ["edge.example.org", "cdn.example.org"], short_ids: [], public_key: "", enabled: false, archived: false, bound_clients: 0, revision: 1,
    tls: certificateMetadata(firstTLS.certificate_pem), options: { websocket: { path: "/connect", host: "edge.example.org" }, client: { fingerprint: "firefox" } } },
    nodes: [{ node_id: "de", installation_id: nodes[0].installation_id, listen_ip: "203.0.113.10", direct_port: 10443, direct_hosts: ["edge.example.org"], tunnel_port: 0, tunnel_hosts: [], swap_enabled: false, swap_source_node_id: "", swap_source_installation_id: "", swap_source_listen_ip: "", swap_source_port: 0, swap_entry_address: "", swap_entry_port: 0, swap_destination_port: 0 }],
    plan_revision: 1, inbound_revision: 1, status: "SAVED_DISABLED", last_event_id: "tls-fixture-event-881", reason: "Saved TLS setup", created_unix: 1800000000, updated_unix: 1800000001, runtime_apply: false, editable: true };
}
async function openTLSPlan(page: Page) {
  await page.goto("/inbounds?inbound=881&tab=overview");
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Edit Inbound setup", exact: true });
  await expect(dialog).toBeVisible(); return dialog;
}

for (const transport of ["tcp", "grpc", "xhttp", "ws", "httpupgrade"] as const) test(`TLS ${transport} creation submits only matching security and network fields`, async ({ page }) => {
  const f = await fixture(page, { tls: true }), dialog = await openCreation(page);
  await tlsProfile(dialog, transport);
  await expect(dialog.locator('[data-creation-field="realityTarget"]')).toHaveCount(0);
  await expect(dialog.locator('[data-creation-field="shortIDs"]')).toHaveCount(0);
  await expect(dialog.getByRole("radio", { name: / \/ TLS$/ })).toHaveCount(5);
  await clientDefaults(dialog); await expect(dialog.locator('[data-creation-field="clientSpiderX"]')).toHaveCount(0);
  await dialog.locator('[data-creation-field="clientFingerprint"]').selectOption("firefox");
  await destination(dialog, "de", "203.0.113.10", "10443", "edge.example.org");
  await dialog.getByRole("button", { name: "Endpoint settings: Direct: de", exact: true }).click();
  await dialog.locator('[data-endpoint-index="0"] summary').click();
  await expect(dialog.locator('[data-creation-field="directEndpoints.0.spiderX"]')).toHaveCount(0);
  await dialog.locator('[data-creation-field="directEndpoints.0.sni"]').selectOption("cdn.example.org");
  await dialog.getByRole("tab", { name: "Advanced", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Add fallback", exact: true })).toHaveCount(0);
  await review(dialog); await check(dialog);
  await expect(dialog.locator("[data-tls-metadata]")).toContainText(certificateMetadata(firstTLS.certificate_pem).sha256);
  await expect(dialog.locator('[data-review-field="transport"]')).toContainText("TLS");
  await expect(dialog.locator('[data-review-field="publicKey"]')).toHaveCount(0);
  await expect(dialog.locator('[data-review-field="shortIDs"]')).toHaveCount(0);
  const payload = f.calls[0].body;
  expect(payload.profile).toMatchObject({ security: "tls", transport });
  expect(payload.profile).not.toHaveProperty("reality_target"); expect(payload.profile).not.toHaveProperty("short_ids");
  expect(payload.tls_credentials).toEqual(firstTLS);
  expect(payload.nodes[0].direct_endpoints?.[0]).not.toHaveProperty("spider_x");
  await save(dialog);
  await expect(page.getByRole("tab", { name: "Overview", exact: true })).toHaveAttribute("aria-selected", "true");
  expect(f.calls[0].raw).toBe(f.calls[1].raw);
  expect(f.getSaved()!.profile.tls).toEqual(certificateMetadata(firstTLS.certificate_pem));
  expect(JSON.stringify(f.getSaved())).not.toContain("BEGIN");
  expect(await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }))).not.toContain("BEGIN");
});

test("TLS editing retains credentials, clears old transport options and replaces a complete pair with exact retry", async ({ page }) => {
  const initial = savedTLSPlan(), f = await fixture(page, { tls: true, savedPlan: initial, failSaveOnce: true });
  const edit = await openTLSPlan(page);
  await expect(edit.getByRole("radio", { name: "TLS", exact: true })).toBeDisabled();
  await expect(edit.getByRole("radio", { name: "REALITY", exact: true })).toBeDisabled();
  await expect(edit.locator("[data-security-immutable]")).toContainText("Security is fixed");
  await expect(edit.getByLabel("TLS private key (PEM)", { exact: true })).toHaveCount(0);
  await edit.getByRole("radio", { name: "HTTPUpgrade / TLS", exact: true }).check();
  await edit.getByLabel("HTTPUpgrade Path", { exact: true }).fill("/upgrade");
  await edit.getByLabel("HTTPUpgrade Host", { exact: true }).fill("edge.example.org");
  await review(edit); await check(edit);
  expect(f.calls[0].body).not.toHaveProperty("tls_credentials");
  expect(f.calls[0].body.profile.options).toMatchObject({ websocket: {}, http_upgrade: { path: "/upgrade", host: "edge.example.org" } });
  await edit.getByRole("button", { name: "Save paused setup", exact: true }).click();
  await expect(edit.getByRole("button", { name: "Retry the same save", exact: true })).toBeEnabled();
  await edit.getByRole("button", { name: "Retry the same save", exact: true }).click(); await expect(edit).toBeHidden();
  expect(f.calls[1].raw).toBe(f.calls[2].raw);
  expect(f.getSaved()).toMatchObject({ plan_revision: 2, inbound_revision: 2, profile: { id: 881, tls: initial.profile.tls } });
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  await edit.getByRole("button", { name: "Replace certificate and key", exact: true }).click();
  await expect(edit.getByLabel("TLS certificate (PEM)", { exact: true })).toHaveValue("");
  await edit.getByLabel("TLS certificate (PEM)", { exact: true }).fill(renewedTLS.certificate_pem);
  await edit.getByLabel("TLS private key (PEM)", { exact: true }).fill(renewedTLS.private_key_pem);
  await edit.getByRole("button", { name: "Keep saved certificate", exact: true }).click();
  await expect(edit.getByLabel("TLS private key (PEM)", { exact: true })).toHaveCount(0);
  await edit.getByRole("button", { name: "Replace certificate and key", exact: true }).click();
  await expect(edit.getByLabel("TLS private key (PEM)", { exact: true })).toHaveValue("");
  await edit.getByLabel("TLS certificate (PEM)", { exact: true }).fill(renewedTLS.certificate_pem);
  await edit.getByLabel("TLS private key (PEM)", { exact: true }).fill(renewedTLS.private_key_pem);
  await review(edit, "Replace TLS certificate"); await check(edit); await save(edit);
  expect(f.calls[3].body.tls_credentials).toEqual(renewedTLS); expect(f.calls[3].raw).toBe(f.calls[4].raw);
  expect(f.getSaved()).toMatchObject({ plan_revision: 3, inbound_revision: 3, profile: { id: 881, public_key: "", tls: certificateMetadata(renewedTLS.certificate_pem) } });
  expect(await page.evaluate(() => JSON.stringify({ ...sessionStorage, ...localStorage }))).not.toContain("BEGIN");
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  await expect(edit.getByLabel("TLS private key (PEM)", { exact: true })).toHaveCount(0);
  await expect(edit.locator('[data-connection-group="security"] [data-tls-metadata]')).toContainText(certificateMetadata(renewedTLS.certificate_pem).sha256);
});

test("TLS copy requires fresh credentials and read-only setup never reveals a key", async ({ page }) => {
  const f = await fixture(page, { tls: true, savedPlan: savedTLSPlan() });
  await page.goto("/inbounds?inbound=881&tab=overview");
  await page.getByRole("button", { name: "Copy settings", exact: true }).click();
  const copy = page.getByRole("dialog", { name: "Create Inbound", exact: true });
  await expect(copy.getByRole("radio", { name: "TLS", exact: true })).toBeChecked();
  await expect(copy.getByLabel("TLS certificate (PEM)", { exact: true })).toHaveValue("");
  await expect(copy.getByLabel("TLS private key (PEM)", { exact: true })).toHaveValue("");
  await expect(copy.locator("[data-tls-metadata]")).toHaveCount(0);
  await destination(copy, "de", "203.0.113.10", "10444", "edge.example.org"); await review(copy);
  await copy.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(copy.getByLabel("TLS certificate (PEM)", { exact: true })).toBeFocused(); expect(f.calls).toHaveLength(0);
  page.once("dialog", confirmation => confirmation.accept()); await copy.getByRole("button", { name: "Close", exact: true }).click();
  f.makeReadOnly(); await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  const read = page.getByRole("dialog", { name: "Edit Inbound setup", exact: true });
  await expect(read.getByRole("button", { name: "Replace certificate and key", exact: true })).toBeDisabled();
  await expect(read.getByLabel("TLS private key (PEM)", { exact: true })).toHaveCount(0);
  await read.getByRole("tab", { name: "Review", exact: true }).click(); await expect(read.getByRole("button", { name: "Check complete setup", exact: true })).toBeDisabled();
});

for (const part of ["certificate", "key"] as const) test(`TLS partial ${part} entry is blocked before preview and focuses the missing field`, async ({ page }) => {
  const f = await fixture(page, { tls: true }), dialog = await openCreation(page);
  await tlsProfile(dialog);
  await dialog.getByLabel(part === "certificate" ? "TLS private key (PEM)" : "TLS certificate (PEM)", { exact: true }).fill("");
  await destination(dialog, "de", "203.0.113.10", "10443", "edge.example.org"); await review(dialog);
  await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByLabel(part === "certificate" ? "TLS private key (PEM)" : "TLS certificate (PEM)", { exact: true })).toBeFocused();
  expect(f.calls).toHaveLength(0);
});

for (const mutation of ["private field", "PEM string", "leaf hash", "endpoint hash"] as const) test(`TLS preview rejects a mismatched or private ${mutation}`, async ({ page }) => {
  const f = await fixture(page, { tls: true,
    tamperPreview: mutation === "endpoint hash" ? undefined : plan => {
      if (mutation === "private field") (plan.profile as unknown as Record<string, unknown>).private_key_pem = firstTLS.private_key_pem;
      else if (mutation === "PEM string") plan.profile.name = firstTLS.certificate_pem;
      else plan.profile.tls!.sha256 = "f".repeat(64);
    }, tamperEndpoints: mutation === "endpoint hash" ? rows => { rows[0].tls_public_key_sha256 = "f".repeat(64); } : undefined });
  const dialog = await openCreation(page); await tlsProfile(dialog); await destination(dialog, "de", "203.0.113.10", "10443", "edge.example.org"); await review(dialog);
  await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("could not validate");
  await expect(dialog.getByRole("button", { name: "Save paused setup", exact: true })).toBeDisabled();
  expect(f.calls).toHaveLength(1);
});

test("TLS save rejects a receipt whose certificate differs from the checked metadata", async ({ page }) => {
  const f = await fixture(page, { tls: true, tamperSave: plan => { plan.profile.tls = certificateMetadata(renewedTLS.certificate_pem); } });
  const dialog = await openCreation(page); await tlsProfile(dialog); await destination(dialog, "de", "203.0.113.10", "10443", "edge.example.org"); await review(dialog); await check(dialog);
  await dialog.getByRole("button", { name: "Save paused setup", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Retry the same save", exact: true })).toBeEnabled();
  await expect(dialog).toBeVisible(); expect(f.calls).toHaveLength(2);
});

test("TLS plan reads reject injected credentials before opening the editor", async ({ page }) => {
  await fixture(page, { tls: true, savedPlan: savedTLSPlan(), tamperRead: plan => { (plan as unknown as Record<string, unknown>).tls_credentials = firstTLS; } });
  await page.goto("/inbounds?inbound=881&tab=overview"); await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  await expect(page.getByText(/Request failed\. No successful Apply/)).toBeVisible();
  await expect(page.getByRole("dialog", { name: "Edit Inbound setup", exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => document.body.textContent)).not.toContain("BEGIN");
});

test("TLS retained certificate expiry focuses the visible replacement action", async ({ page }) => {
  const f = await fixture(page, { tls: true, savedPlan: savedTLSPlan(), previewError: { status: 422, issue: { field: "tls_credentials.certificate_pem", code: "tls_certificate_expired" } } });
  const dialog = await openTLSPlan(page); await dialog.locator('[data-connection-group="security"] > summary').click();
  await review(dialog); await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Replace certificate and key", exact: true })).toBeFocused();
  await expect(dialog.locator('[data-connection-group="security"] .creation-issues')).toContainText("certificate has expired");
  expect(f.calls[0].body).not.toHaveProperty("tls_credentials");
});

test("TLS security switching keeps REALITY draft values out of payload and legacy capabilities stay compatible", async ({ page }) => {
  const f = await fixture(page, { tls: true }), dialog = await openCreation(page); await profile(dialog);
  await dialog.getByText("Advanced REALITY settings", { exact: true }).click();
  await dialog.locator('[data-creation-field="realityXver"]').selectOption("2");
  await clientDefaults(dialog); await dialog.locator('[data-creation-field="clientSpiderX"]').fill("/private-crawl");
  await dialog.getByRole("tab", { name: "Advanced", exact: true }).click();
  await dialog.getByRole("button", { name: "Add fallback", exact: true }).click();
  await dialog.locator('[data-creation-field="fallbacks.0.dest"]').fill("invalid-retained-destination");
  await dialog.getByRole("tab", { name: "Connection", exact: true }).click();
  await tlsProfile(dialog, "ws"); await destination(dialog, "de", "203.0.113.10", "10443", "edge.example.org"); await review(dialog); await check(dialog);
  expect(f.calls[0].body.profile.options).not.toHaveProperty("reality_xver");
  expect(f.calls[0].body.profile.options).not.toHaveProperty("fallbacks");
  expect(f.calls[0].body.profile.options?.client).not.toHaveProperty("spider_x");
  await dialog.getByRole("tab", { name: "Connection", exact: true }).click();
  await dialog.getByRole("radio", { name: "REALITY", exact: true }).check();
  await expect(dialog.getByRole("radio", { name: "TCP / REALITY", exact: true })).toBeChecked();
  await expect(dialog.getByRole("radio", { name: /WebSocket|HTTPUpgrade/ })).toHaveCount(0);
  await expect(dialog.locator('[data-creation-field="realityTarget"]')).toHaveValue("example.org:443");
  await expect(dialog.getByLabel("TLS private key (PEM)", { exact: true })).toHaveCount(0);
  page.once("dialog", confirmation => confirmation.accept()); await dialog.getByRole("button", { name: "Close", exact: true }).click();
  const old = await fixture(page); const oldDialog = await openCreation(page); await profile(oldDialog);
  await expect(oldDialog.getByRole("radio", { name: "TLS", exact: true })).toHaveCount(0);
  await destination(oldDialog, "de", "203.0.113.10", "10443", "edge.example.org"); await review(oldDialog); await check(oldDialog);
  expect(old.calls[0].body.profile).not.toHaveProperty("security");
});

test("TLS WebSocket and HTTPUpgrade reject unsafe path and Host spellings locally", async ({ page }) => {
  const f = await fixture(page, { tls: true }), dialog = await openCreation(page); await tlsProfile(dialog);
  await destination(dialog, "de", "203.0.113.10", "10443", "edge.example.org");
  for (const [transport, prefix] of [["WebSocket", "websocket"], ["HTTPUpgrade", "httpUpgrade"]]) {
    await dialog.getByRole("tab", { name: "Connection", exact: true }).click();
    await dialog.getByRole("radio", { name: transport + " / TLS", exact: true }).check();
    for (const path of ["//absolute", "/encoded%20path", "/query?x=1", "/fragment#x", "/back\\slash", "/space path", "/مسیر"]) {
      await dialog.locator(`[data-creation-field="${prefix}Path"]`).fill(path); await review(dialog);
      await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
      await expect(dialog.locator(`[data-creation-field="${prefix}Path"]`)).toBeFocused();
    }
    await dialog.locator(`[data-creation-field="${prefix}Path"]`).fill("/valid");
    for (const host of ["edge.example.org:443", "[2001:db8::1]", "https://edge.example.org"]) {
      await dialog.locator(`[data-creation-field="${prefix}Host"]`).fill(host); await review(dialog);
      await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
      await expect(dialog.locator(`[data-creation-field="${prefix}Host"]`)).toBeFocused();
    }
    await dialog.locator(`[data-creation-field="${prefix}Host"]`).fill("edge.example.org");
  }
  expect(f.calls).toHaveLength(0);
});

for (const language of ["en", "fa"] as const) test(`TLS certificate review and four-stage navigation fit 320px in ${language}`, async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 760 });
  await fixture(page, { tls: true, language, savedPlan: savedTLSPlan() });
  await page.goto("/inbounds?inbound=881&tab=overview");
  await page.getByRole("button", { name: language === "fa" ? "بازکردن تنظیمات ذخیره‌شده" : "Open saved setup", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("radio", { name: "WebSocket / TLS", exact: true })).toBeChecked();
  await expect(dialog.locator(".creation-tabs [role=tab]")).toHaveCount(4);
  const cert = dialog.locator('[data-connection-group="security"] [data-tls-metadata]'); await expect(cert).toContainText("edge.example.org");
  await dialog.getByRole("tab", { name: language === "fa" ? "بازبینی" : "Review", exact: true }).click();
  await expect(dialog.locator('[data-review-section="connection"] [data-tls-metadata]')).toContainText(certificateMetadata(firstTLS.certificate_pem).sha256);
  const layout = await dialog.evaluate(element => ({ width: element.clientWidth, scroll: element.scrollWidth, document: document.documentElement.scrollWidth, viewport: window.innerWidth }));
  expect(layout.scroll).toBeLessThanOrEqual(layout.width + 1); expect(layout.document).toBeLessThanOrEqual(layout.viewport + 1);
  for (const tab of await dialog.locator(".creation-tabs [role=tab]").all()) {
    const box = await tab.boundingBox(); expect(box!.x).toBeGreaterThanOrEqual(0); expect(box!.x + box!.width).toBeLessThanOrEqual(320);
  }
  await expect(dialog.locator(".creation-footer")).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath(`tls-review-${language}-320.png`) });
});
