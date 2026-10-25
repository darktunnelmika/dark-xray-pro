import { expect, test, type Locator, type Page } from "@playwright/test";

// Entirely synthetic owner/API fixtures. No control plane, nodes or public
// targets are contacted by these browser tests.
const api = "/api/v1/workspace/native-inbound-plans", securityAPI = "/api/v1/workspace/inbound-security";
const protocols = ["vmess", "vless", "trojan", "shadowsocks", "wireguard", "hysteria", "http", "mixed", "tunnel", "tun", "mtproto", "amneziawg", "tuic", "masque"];
const networks = ["tcp", "kcp", "ws", "grpc", "httpupgrade", "xhttp", "xdrive"];
const csrf = "a".repeat(64);
const nodes = [{ node_id: "hub-local", installation_id: "1".repeat(32), name: "HUB — this server", kind: "hub" }, { node_id: "de", installation_id: "2".repeat(32), name: "Germany", kind: "node" }];
type Body = Record<string, any>;
async function fixture(page: Page, options: { language?: "en" | "fa"; theme?: "dark" | "light" } = {}) {
  const calls: Array<{ path: string; body: Body; csrf: string }> = [];
  let saved: Body | undefined;
  const material = new Map<string, unknown>();
  const publicSpec = (input: Body) => {
    const paths: string[] = [];
    function clean(value: unknown, path = ""): unknown {
      if (Array.isArray(value)) return value.map((item, index) => clean(item, `${path}.${index}`));
      if (!value || typeof value !== "object") return value;
      const result: Body = {};
      for (const [key, item] of Object.entries(value)) {
        const next = path ? path + "." + key : key;
        if (["password", "privateKey", "secretKey", "decryption", "certificate", "key", "certificateFile", "keyFile", "headers"].includes(key)) { paths.push(next); material.set(next, item); }
        else result[key] = clean(item, next);
      }
      return result;
    }
    const result = clean(input) as Body;
    for (const path of material.keys()) if (!paths.includes(path) && saved?.profile.spec.protocol === input.protocol) paths.push(path);
    return { spec: result, paths };
  };
  function makePlan(body: Body, saving: boolean) {
    const { spec, paths } = publicSpec(body.profile.spec);
    spec.stream ??= {};
    if (networks.includes(spec.network)) spec.stream[`${spec.network}Settings`] ??= {};
    if (spec.security === "reality") { spec.client ??= {}; spec.client.publicKey ??= (saving ? "S" : "P").repeat(43); }
    const revision = body.expected_inbound_revision === 0 ? 1 : body.expected_inbound_revision + (saved?.profile.name === body.profile.name ? 0 : 1);
    const waiting = body.nodes.some((node: Body) => node.direct_port && !node.direct_hosts.length || node.tunnel_port && !node.tunnel_hosts.length);
    return { profile: { ...body.profile, spec, spec_secret_paths: paths, revision, enabled: false, archived: false, bound_clients: 0, public_key: "", reality_target: body.profile.reality_target ?? "", short_ids: body.profile.short_ids ?? [] }, nodes: body.nodes,
      plan_revision: body.expected_plan_revision + 1, inbound_revision: revision, status: waiting ? "WAITING_HOST" : "SAVED_DISABLED", created_unix: 1800000000, updated_unix: 1800000001, reason: body.reason, runtime_apply: false, editable: true, last_event_id: body.event_id };
  }
  await page.addInitScript(({ language, theme }) => { localStorage.setItem("dark-pro-lang", language); localStorage.setItem("dark-pro-theme", theme); }, { language: options.language ?? "en", theme: options.theme ?? "dark" });
  await page.route("**/api/v1/session/status", route => route.fulfill({ json: { authenticated: true, user: { id: "owner", role: "owner", username: "synthetic-owner", representative_id: "owner" }, csrf_token: csrf, expires_unix: 1900000000 } }));
  await page.route("**/api/v1/fleet", route => route.fulfill({ json: { nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0, as_of: new Date().toISOString(), interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false, metric_scope: "default_route_interface_not_billable" } }));
  await page.route("**/api/v1/workspace/**", async route => {
    const req = route.request(), path = new URL(req.url()).pathname;
    if (["POST", "PUT"].includes(req.method())) {
      const body = req.postDataJSON() as Body; calls.push({ path, body, csrf: req.headers()["x-csrf-token"] ?? "" });
      if (path === securityAPI + "/keys/x25519") return route.fulfill({ json: { kind: "x25519", private_key: "K".repeat(43), public_key: "U".repeat(43) } });
      if (path === securityAPI + "/reality/scan") return route.fulfill({ json: { ...body, host: "target.example", ip: "203.0.113.5", port: 443, server_name: "target.example", server_name_source: "request", ok: true, suitable: true, error: "", reason: "", tls13: true, tls_version: "TLS 1.3", h2: true, alpn: "h2", x25519: true, hybrid_key_exchange: "not_measured", latency_ms: 24, cert_chain_bytes: 2000 } });
      if (path === api + "/preview") return route.fulfill({ json: { valid: true, plan: makePlan(body, false), runtime_changed: false, topology_validated: true, topology_validation_only: true, topology_key_scope: body.expected_plan_revision ? "saved_profile" : "preview_only", endpoints: [] } });
      if (path === api || path === api + "/" + body.profile?.id) { saved = makePlan(body, true); return route.fulfill({ status: req.method() === "POST" ? 201 : 200, json: { plan: saved, event_id: body.event_id, replayed: false, runtime_changed: false, clients_deleted: false, credit_changed: false, usage_reset: false, subscription_published: false } }); }
      return route.fulfill({ status: 405, json: { error: "unexpected_fixture_mutation" } });
    }
    if (path === api + "/capabilities") return route.fulfill({ json: { available: true, version: 4, authoring_spec: true, max_nodes: 64, max_hosts_per_node: 64, protocols, transports: networks, securities: ["none", "tls", "reality"], runtime_apply: false, endpoint_options: true, plans: saved ? [{ inbound_id: saved.profile.id, plan_revision: saved.plan_revision, inbound_revision: saved.inbound_revision, status: saved.status, node_ids: saved.nodes.map((node: Body) => node.node_id) }] : [] } });
    if (path === securityAPI + "/capabilities") return route.fulfill({ json: { enabled: true, keys: { x25519: true, mldsa65: false, ech: false }, certificates: { inspect: true, generate: true, remote: true }, reality: { scan: true, find: true, max_targets: 32, max_cidr_addresses: 256 }, node_probe_available: true, default_targets: [{ target: "example.org:443", server_name: "example.org" }], unavailable_reasons: { mldsa65: "core_not_available", ech: "core_not_available" } } });
    if (saved && path === api + "/" + saved.profile.id) return route.fulfill({ json: { plan: saved, references_current: true } });
    if (path.endsWith("/native-inbounds-state")) return route.fulfill({ json: { inbounds: saved ? [saved.profile] : [], nodes, targets: [], runtime: [] } });
    if (path.endsWith("/operations")) return route.fulfill({ json: { operations: [] } });
    if (path.endsWith("/clients")) return route.fulfill({ json: { clients: [], client_ids: [] } });
    return route.fulfill({ status: 404, json: {} });
  });
  return { calls, saved: () => saved };
}
async function open(page: Page, language: "en" | "fa" = "en") {
  await page.goto("/inbounds");
  await page.getByRole("button", { name: language === "fa" ? "افزودن اینباند" : "Add Inbound", exact: true }).click();
  const dialog = page.getByRole("dialog"); await expect(dialog).toBeVisible(); return dialog;
}
async function hub(dialog: Locator) {
  await dialog.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  await dialog.getByRole("checkbox", { name: "Select destination: HUB — this server", exact: true }).check();
  await dialog.getByLabel("Destination listening IP: hub-local", { exact: true }).fill("127.0.0.1");
  await dialog.getByLabel("Direct port: hub-local", { exact: true }).fill("10443");
  await dialog.getByLabel("Direct Hosts: hub-local", { exact: true }).fill("node.example.org");
}
async function review(dialog: Locator) {
  await dialog.getByRole("tab", { name: "Review", exact: true }).click();
  await dialog.getByLabel("Change note", { exact: true }).fill("Synthetic specification review");
  await dialog.getByRole("button", { name: "Check complete setup", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Save paused setup", exact: true })).toBeEnabled();
}

test("v4 exposes fourteen protocols and seven transports, with explicit unselected HUB", async ({ page }) => {
  const fx = await fixture(page), dialog = await open(page);
  expect(await dialog.getByLabel("Protocol", { exact: true }).locator("option").evaluateAll(options => options.map(option => (option as HTMLOptionElement).value))).toEqual(protocols);
  expect(await dialog.getByLabel("Transport", { exact: true }).locator("option").evaluateAll(options => options.map(option => (option as HTMLOptionElement).value))).toEqual(networks);
  for (const protocol of protocols) { await dialog.getByLabel("Protocol", { exact: true }).selectOption(protocol); await expect(dialog.getByLabel("Protocol", { exact: true })).toHaveValue(protocol); if (protocol !== "vless") await expect(dialog.locator(".creation-protocol")).not.toHaveText("VLESS / REALITY"); }
  await dialog.getByLabel("Protocol", { exact: true }).selectOption("wireguard");
  await expect(dialog.getByLabel("Transport", { exact: true })).toHaveCount(0);
  await expect(dialog.locator('[data-creation-field="spec.settings.secretKey"]')).toBeVisible();
  await dialog.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  await expect(dialog.getByRole("checkbox", { name: "Select destination: HUB — this server", exact: true })).not.toBeChecked();
  await expect(dialog.getByRole("checkbox", { name: "Select destination: Germany", exact: true })).not.toBeChecked();
  expect(fx.calls).toHaveLength(0);
});

test("non-VLESS preview/save and redacted edit preserve private material without legacy requirements", async ({ page }) => {
  const fx = await fixture(page), dialog = await open(page);
  await dialog.getByLabel("Inbound name", { exact: true }).fill("Synthetic Shadowsocks");
  await dialog.getByLabel("Protocol", { exact: true }).selectOption("shadowsocks");
  await dialog.getByLabel("Server password", { exact: true }).fill("SYNTHETIC_WRITE_ONLY_PASSWORD");
  await hub(dialog); await review(dialog);
  await expect(dialog.locator(".spec-public-json")).not.toContainText("SYNTHETIC_WRITE_ONLY_PASSWORD");
  await dialog.getByRole("button", { name: "Save paused setup", exact: true }).click(); await expect(dialog).toBeHidden();
  const first = fx.calls.find(call => call.path === api)!;
  expect(first.body.profile.spec.protocol).toBe("shadowsocks"); expect(first.body.profile.options).toBeUndefined(); expect(first.body.tls_credentials).toBeUndefined();
  expect(first.body.nodes.map((node: Body) => node.node_id)).toEqual(["hub-local"]);
  expect(fx.saved()?.profile.spec.settings.password).toBeUndefined(); expect(fx.saved()?.profile.spec_secret_paths).toContain("settings.password");
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  const edit = page.getByRole("dialog"); await expect(edit.getByLabel("Server password", { exact: true })).toHaveValue("");
  // Re-entering the identical write-only secret is a server-side no-op: the
  // plan advances, while the private inbound revision remains unchanged.
  await edit.getByLabel("Server password", { exact: true }).fill("SYNTHETIC_WRITE_ONLY_PASSWORD");
  await review(edit);
  await edit.getByRole("button", { name: "Save paused setup", exact: true }).click(); await expect(edit).toBeHidden();
  expect(fx.saved()?.inbound_revision).toBe(1); expect(fx.saved()?.plan_revision).toBe(2);
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  await expect(edit.getByLabel("Server password", { exact: true })).toHaveValue("");
  await edit.getByLabel("Inbound name", { exact: true }).fill("Synthetic Shadowsocks renamed"); await review(edit);
  const last = fx.calls.filter(call => call.path === api + "/preview").at(-1)!;
  expect(last.body.profile.spec.settings.password).toBeUndefined(); expect(last.body.profile.spec_secret_paths).toBeUndefined();
  expect(last.csrf).toBe(csrf);
});

test("security tools only run after an explicit action and preserve the selected probe source", async ({ page }) => {
  const fx = await fixture(page), dialog = await open(page);
  await dialog.locator(".spec-tools-disclosure > summary").click();
  await expect(dialog.getByRole("button", { name: "X25519", exact: true })).toBeEnabled(); expect(fx.calls).toHaveLength(0);
  await dialog.getByRole("button", { name: "X25519", exact: true }).click();
  await expect(dialog.locator('[data-creation-field="spec.reality.privateKey"]')).toHaveValue("K".repeat(43));
  await expect(dialog.getByRole("button", { name: "ML-DSA-65", exact: true })).toBeDisabled();
  await dialog.getByText("Scan and find targets", { exact: true }).click();
  await dialog.getByLabel("Probe from", { exact: true }).selectOption("de");
  await dialog.getByLabel("Scan target", { exact: true }).fill("target.example:443");
  await dialog.getByLabel("Scan SNI", { exact: true }).fill("target.example");
  await dialog.getByRole("button", { name: "Check targets", exact: true }).click();
  await dialog.getByRole("button", { name: "Use target and SNI", exact: true }).click();
  await expect(dialog.locator('[data-creation-field="spec.reality.target"]')).toHaveValue("target.example:443");
  const scan = fx.calls.find(call => call.path === securityAPI + "/reality/scan")!;
  expect(scan.body).toEqual({ scope: "node", node_id: "de", target: "target.example:443", server_name: "target.example" }); expect(scan.csrf).toBe(csrf);
  await dialog.getByRole("tab", { name: "Dark Swap", exact: true }).click();
  await expect(dialog.getByRole("checkbox", { name: "Select destination: HUB — this server", exact: true })).not.toBeChecked();
});

for (const theme of ["light", "dark"] as const) test(`Persian mobile ${theme}: transport select and security fit without horizontal overflow`, async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 }); await fixture(page, { language: "fa", theme }); const dialog = await open(page, "fa");
  await expect(dialog.getByLabel("انتقال", { exact: true })).toBeVisible();
  await dialog.screenshot({ path: testInfo.outputPath(`fa-${theme}-connection.png`) });
  await dialog.getByLabel("انتقال", { exact: true }).selectOption("xdrive");
  await expect(dialog.getByLabel("Storage service", { exact: true })).toBeVisible();
  await dialog.getByLabel("Storage service", { exact: true }).selectOption("local");
  await dialog.getByLabel("Remote folder", { exact: true }).fill("/var/lib/synthetic-xdrive");
  expect(await dialog.evaluate(node => node.scrollWidth <= node.clientWidth + 1)).toBe(true);
  await dialog.locator('[data-authoring-section="transport"]').scrollIntoViewIfNeeded();
  await dialog.screenshot({ path: testInfo.outputPath(`fa-${theme}-transport.png`) });
  await dialog.getByLabel("انتقال", { exact: true }).selectOption("tcp");
  await dialog.getByLabel("نوع امنیت", { exact: true }).selectOption("tls");
  await dialog.locator('[data-creation-field="spec.tls.serverName"]').fill("example.org");
  await dialog.locator('[data-connection-group="security"] > summary').scrollIntoViewIfNeeded();
  await dialog.screenshot({ path: testInfo.outputPath(`fa-${theme}-security.png`) });
  await dialog.getByRole("tab", { name: "پیشرفته", exact: true }).click();
  await dialog.getByText("FinalMask", { exact: true }).click();
  expect(await dialog.evaluate(node => node.scrollWidth <= node.clientWidth + 1)).toBe(true);
});
