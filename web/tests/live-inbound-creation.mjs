// Opt-in Go acceptance harness: real compiled UI, real HTTP handlers and SQLite.
// There are no route mocks, response substitutions or direct API mutations.
import { chromium, expect } from '@playwright/test';
import { readFile, writeFile } from 'node:fs/promises';
import { createServer, request as httpRequest } from 'node:http';
import { isAbsolute } from 'node:path';

const base = new URL(process.env.DARK_PRO_QA_UI_URL || '');
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || !base.port || base.username || base.password) throw new Error('QA loopback only');
const control = JSON.parse(await readFile(process.env.DARK_PRO_QA_CONTROL, 'utf8'));
if (typeof control.username !== 'string' || typeof control.password !== 'string' || !control.username || !control.password ||
  typeof control.result_path !== 'string' || !isAbsolute(control.result_path)) throw new Error('Invalid QA control file');
const api = '/api/v1/workspace/native-inbound-plans';
const diagnostics = [], mutations = [], requests = [];
let stage = 'launch', inboundID = 0;

// A transport-only proxy permits exactly the disposable Go harness origin.
// It forwards the original method/body and actual handler response unchanged;
// it never fabricates an application response or permits external connections.
const denyProxy = createServer((request, response) => {
  let target;
  try { target = new URL(request.url); } catch { response.writeHead(403).end(); return; }
  if (target.origin !== base.origin || target.username || target.password) { response.writeHead(403).end(); return; }
  const headers = { ...request.headers, host: base.host };
  delete headers['proxy-connection']; delete headers['proxy-authorization'];
  const upstream = httpRequest({ hostname: '127.0.0.1', port: base.port, method: request.method,
    path: target.pathname + target.search, headers }, incoming => {
    response.writeHead(incoming.statusCode, incoming.headers); incoming.pipe(response);
  });
  upstream.on('error', () => { diagnostics.push('Harness transport connection failed'); response.writeHead(502).end(); });
  request.on('aborted', () => upstream.destroy());
  request.pipe(upstream);
});
denyProxy.on('connect', (_request, socket) => { socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n'); });
await new Promise((resolve, reject) => { denyProxy.once('error', reject); denyProxy.listen(0, '127.0.0.1', resolve); });
let browser, page;

function publicOnly(value, path = 'response') {
  if (!value || typeof value !== 'object') return;
  for (const [key, nested] of Object.entries(value)) {
    const normalized = key.replace(/[_-]/g, '').toLowerCase();
    if (['privatekey', 'realityprivatekey', 'config'].includes(normalized)) throw new Error('Private configuration field exposed at ' + path + '.' + key);
    publicOnly(nested, path + '.' + key);
  }
}
function receiptSafe(receipt, planRevision, inboundRevision) {
  publicOnly(receipt);
  expect(receipt).toMatchObject({ runtime_changed: false, clients_deleted: false, credit_changed: false,
    usage_reset: false, subscription_published: false, replayed: false });
  expect(receipt.plan).toMatchObject({ runtime_apply: false, editable: true, status: 'SAVED_DISABLED',
    plan_revision: planRevision, inbound_revision: inboundRevision, profile: { enabled: false, archived: false, bound_clients: 0, revision: inboundRevision } });
  expect(receipt.event_id).toMatch(/^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$/);
  expect(receipt.plan.last_event_id).toBe(receipt.event_id);
}
const expectedOptions = edited => ({
  ...(edited ? { grpc_authority: 'edge.example.org:8443', grpc_multi_mode: true } : {}),
  reality_xver: 2, reality_max_time_diff: edited ? 120000 : 60000,
  reality_min_client_ver: '1.8.0', reality_max_client_ver: '25.9.11',
  sniffing: { enabled: !edited, dest_override: ['http', 'tls', 'quic'], route_only: true,
    domains_excluded: edited ? ['domain:example.net', 'full:keep.example.org'] : ['domain:example.net'], ips_excluded: ['192.0.2.0/24'] },
  ...(edited ? {} : { fallbacks: [{ dest: '127.0.0.1:8080', path: '/health', xver: 2 }] }),
});
const expectedNodes = edited => [
  { node_id: 'de', installation_id: 'a'.repeat(32), listen_ip: '198.51.100.10', direct_port: 10443,
    direct_hosts: ['de.example.org', 'shared.example.org'], tunnel_port: 1185, tunnel_hosts: ['tunnel.example.org'],
    swap_enabled: edited, swap_source_node_id: edited ? 'nl' : '', swap_source_installation_id: edited ? 'c'.repeat(32) : '', swap_source_listen_ip: edited ? '198.51.100.30' : '',
    swap_source_port: edited ? 16801 : 0, swap_entry_address: edited ? 'entry.example.org' : '', swap_entry_port: edited ? 444 : 0, swap_destination_port: edited ? 18443 : 0 },
  { node_id: 'fr', installation_id: 'b'.repeat(32), listen_ip: '198.51.100.20', direct_port: 11443,
    direct_hosts: edited ? ['fr.example.org', 'fr-edge.example.org'] : ['fr.example.org'], tunnel_port: 1285, tunnel_hosts: ['fr-tunnel.example.org'],
    swap_enabled: false, swap_source_node_id: '', swap_source_installation_id: '', swap_source_listen_ip: '',
    swap_source_port: 0, swap_entry_address: '', swap_entry_port: 0, swap_destination_port: 0 },
];

try {
  browser = await chromium.launch({ headless: true, proxy: { server: 'http://127.0.0.1:' + denyProxy.address().port, bypass: '127.0.0.1' } });
  page = await browser.newPage({ viewport: { width: 1280, height: 900 }, locale: 'en-US' });
  await page.addInitScript(() => { localStorage.setItem('dark-pro-lang', 'en'); });
  page.setDefaultTimeout(15000);
  page.on('pageerror', error => diagnostics.push('Browser page error: ' + error.message));
  page.on('requestfailed', request => requests.push({ path: new URL(request.url()).pathname, failed: request.failure()?.errorText }));
  page.on('request', request => {
    const url = new URL(request.url());
    if (url.origin !== base.origin) diagnostics.push('Unexpected request outside the harness origin');
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) {
      mutations.push({ method: request.method(), path: url.pathname });
      const allowed = request.method() === 'POST' && ['/api/v1/session/login', api + '/preview', api].includes(url.pathname) ||
        request.method() === 'PUT' && inboundID > 0 && url.pathname === api + '/' + inboundID;
      if (!allowed) diagnostics.push('Unexpected mutation: ' + request.method() + ' ' + url.pathname);
    }
  });
  page.on('response', response => {
    const url = new URL(response.url());
    requests.push({ path: url.pathname, status: response.status() });
  });
  const observe = (method, path) => page.waitForResponse(response => new URL(response.url()).origin === base.origin &&
    new URL(response.url()).pathname === path && response.request().method() === method);
  async function expand(dialog, name) {
    const button = dialog.getByRole('button', { name: 'Configure: ' + name, exact: true });
    if (await button.getAttribute('aria-expanded') !== 'true') await button.click();
  }
  async function fillDestination(dialog, id, name, ip, directPort, directHosts, tunnelPort, tunnelHosts) {
    await dialog.getByRole('checkbox', { name: 'Select destination: ' + name, exact: true }).check();
    await dialog.getByLabel('Destination listening IP: ' + id, { exact: true }).fill(ip);
    await dialog.getByLabel('Direct port: ' + id, { exact: true }).fill(directPort);
    await dialog.getByLabel('Direct Hosts: ' + id, { exact: true }).fill(directHosts);
    await dialog.getByRole('checkbox', { name: 'Tunnel: ' + id, exact: true }).check();
    await dialog.getByLabel('Tunnel port: ' + id, { exact: true }).fill(tunnelPort);
    await dialog.getByLabel('Tunnel Hosts: ' + id, { exact: true }).fill(tunnelHosts);
  }
  async function checkSetup(dialog, edited) {
    const connection = dialog.locator('[data-review-section="connection"]');
    await expect(connection.locator('[data-review-field="realityTarget"]')).toContainText('example.org:443');
    await expect(connection.locator('[data-review-field="shortIDs"]')).toContainText(edited ? 'eeff' : 'ccdd');
    await expect(dialog.locator('[data-review-option="realityMaxTimeDiff"]')).toContainText(edited ? '120000' : '60000');
    await expect(dialog.locator('[data-review-option="sniffing"]')).toHaveAttribute('data-sniffing-enabled', String(!edited));
    await expect(dialog.locator('[data-review-option="sniffing"]')).toHaveAttribute('data-sniffing-retained', String(edited));
    for (const row of expectedNodes(edited)) {
      const summary = dialog.locator(`[data-review-node="${row.node_id}"]`);
      for (const [path, port] of [['direct', row.direct_port], ['tunnel', row.tunnel_port]]) {
        await expect(summary.locator(`[data-review-path="${path}"]`)).toHaveAttribute('data-path-enabled', 'true');
        await expect(summary.locator(`[data-review-path="${path}"]`)).toContainText(String(port));
      }
      await expect(summary.locator('[data-review-path="swap"]')).toHaveAttribute('data-path-enabled', String(row.swap_enabled));
    }
    const pending = observe('POST', api + '/preview');
    await dialog.getByRole('button', { name: 'Check complete setup', exact: true }).click();
    const response = await pending; expect(response.status()).toBe(200);
    const preview = await response.json(); publicOnly(preview);
    expect(preview).toMatchObject({ valid: true, runtime_changed: false, topology_validated: true,
      topology_validation_only: true, topology_key_scope: edited ? 'saved_profile' : 'preview_only' });
    expect(preview.plan.nodes).toEqual(expectedNodes(edited));
    expect(preview.plan.profile.options).toEqual(expectedOptions(edited));
    expect(preview.plan.profile.enabled).toBe(false); expect(preview.plan.runtime_apply).toBe(false);
    expect(preview.topology.nodes.map(node => node.node_id).sort()).toEqual(edited ? ['de', 'fr', 'nl'] : ['de', 'fr']);
    for (const node of preview.topology.nodes) expect(node.candidate).toMatchObject({ can_activate: false,
      requires_xray_test: true, requires_data_plane_proof: true, requires_subscription_commit: true });
    if (edited) {
      const relay = preview.topology.nodes.find(node => node.node_id === 'nl');
      expect(relay.candidate.listeners).toHaveLength(1);
      expect(relay.candidate.listeners[0]).toMatchObject({ kind: 'swap_relay', node_port: 16801, attribution: 'transit_not_billable' });
    } else {
      expect(preview.topology.nodes.flatMap(node => node.candidate.listeners).some(listener => listener.kind === 'swap_relay')).toBe(false);
    }
    await expect(dialog.getByRole('button', { name: 'Save paused setup', exact: true })).toBeEnabled();
    return response.request().postDataJSON();
  }
  async function persist(dialog, edited) {
    const pending = observe(edited ? 'PUT' : 'POST', api + (edited ? '/' + inboundID : ''));
    await dialog.getByRole('button', { name: 'Save paused setup', exact: true }).click();
    const response = await pending; expect(response.status()).toBe(edited ? 200 : 201);
    const receipt = await response.json(); receiptSafe(receipt, edited ? 2 : 1, edited ? 2 : 1);
    expect(receipt.plan.profile.options).toEqual(expectedOptions(edited));
    await expect(dialog).toBeHidden();
    return { receipt, body: response.request().postDataJSON() };
  }
  async function reopen() {
    await page.reload({ waitUntil: 'domcontentloaded' });
    const open = page.getByRole('button', { name: 'Open saved setup', exact: true }); await expect(open).toBeEnabled();
    const pending = observe('GET', api + '/' + inboundID); await open.click();
    const response = await pending; expect(response.status()).toBe(200);
    const saved = await response.json(); publicOnly(saved);
    expect(saved.references_current).toBe(true); expect(saved.plan.editable).toBe(true); expect(saved.plan.runtime_apply).toBe(false);
    const dialog = page.getByRole('dialog', { name: 'Edit Inbound setup', exact: true }); await expect(dialog).toBeVisible();
    return { dialog, plan: saved.plan };
  }
  async function advanced(dialog, section) {
    await dialog.getByRole('tab', { name: 'Connection', exact: true }).click();
    const group = dialog.locator(`[data-connection-group="${section}"]`);
    if (await group.getAttribute('open') === null) await group.locator(':scope > summary').click();
    const details = group.locator('.inbound-profile-advanced').filter({ hasText: section === 'network' ? 'Advanced gRPC settings' : 'Advanced REALITY settings' });
    if (await details.getAttribute('open') === null) await details.locator('summary').click();
  }
  async function traffic(dialog) {
    await dialog.getByRole('tab', { name: 'Advanced', exact: true }).click();
    const settings = dialog.getByRole('region', { name: 'Sniffing', exact: true }).locator('details').first();
    if (await settings.getAttribute('open') === null) await settings.locator(':scope > summary').click();
    const exclusions = dialog.locator('.inbound-traffic-exclusions');
    if (await exclusions.getAttribute('open') === null) await exclusions.locator('summary').click();
  }
  async function verifyProfile(dialog, edited) {
    await expect(dialog.getByRole('tab')).toHaveText(['Connection', 'Dark Swap', 'Advanced', 'Review']);
    for (const label of [/^Inbound name/, /^REALITY target/, /^Server names/, /^Short IDs/]) await expect(dialog.getByLabel(label)).toHaveCount(1);
    await expect(dialog.getByLabel('Inbound name', { exact: true })).toHaveValue(edited ? 'Real browser creation edited' : 'Real browser creation');
    await expect(dialog.getByRole('radio', { name: edited ? 'gRPC / REALITY' : 'TCP / REALITY', exact: true })).toBeChecked();
    if (edited) await expect(dialog.getByLabel('gRPC service', { exact: true })).toHaveValue('dark-grpc');
    await expect(dialog.getByLabel('REALITY target (host:port)', { exact: true })).toHaveValue('example.org:443');
    await expect(dialog.getByLabel('Server names (comma-separated)', { exact: true })).toHaveValue('example.org');
    await expect(dialog.getByLabel('Short IDs', { exact: true })).toHaveValue(edited ? 'aabb, ccdd, eeff' : 'aabb, ccdd');
    await advanced(dialog, 'security');
    await expect(dialog.getByLabel('REALITY Xver', { exact: true })).toHaveValue('2');
    await expect(dialog.getByLabel('Maximum time difference (ms)', { exact: true })).toHaveValue(edited ? '120000' : '60000');
    await expect(dialog.getByLabel('Minimum client version', { exact: true })).toHaveValue('1.8.0');
    await expect(dialog.getByLabel('Maximum client version', { exact: true })).toHaveValue('25.9.11');
    if (edited) {
      await advanced(dialog, 'network');
      await expect(dialog.getByLabel('gRPC Authority', { exact: true })).toHaveValue('edge.example.org:8443');
      await expect(dialog.getByRole('checkbox', { name: 'gRPC MultiMode', exact: true })).toBeChecked();
    }
    await traffic(dialog);
    await expect(dialog.getByRole('checkbox', { name: 'Enable Sniffing', exact: true })).toBeChecked({ checked: !edited });
    for (const protocol of ['HTTP', 'TLS', 'QUIC']) await expect(dialog.getByRole('checkbox', { name: protocol, exact: true })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Use the detected domain for routing only', exact: true })).toBeChecked();
    await expect(dialog.getByLabel('Excluded domains', { exact: true })).toHaveValue(expectedOptions(edited).sniffing.domains_excluded.join('\n'));
    await expect(dialog.getByLabel('Excluded IPs or CIDRs', { exact: true })).toHaveValue('192.0.2.0/24');
    await expect(dialog.locator('.inbound-fallback-row')).toHaveCount(edited ? 0 : 1);
    if (!edited) {
      const row = dialog.locator('.inbound-fallback-row'); await row.locator('summary').click();
      await expect(dialog.getByLabel('Fallback 1 Destination', { exact: true })).toHaveValue('127.0.0.1:8080');
      await expect(dialog.getByLabel('Fallback 1 Path', { exact: true })).toHaveValue('/health');
      await expect(dialog.getByLabel('Fallback 1 ALPN', { exact: true })).toHaveCount(0);
      await expect(dialog.getByLabel('Fallback 1 PROXY protocol', { exact: true })).toHaveValue('2');
    }
  }
  async function verifyNodes(dialog, edited) {
    await dialog.getByRole('tab', { name: 'Dark Swap', exact: true }).click();
    await expect(dialog.getByRole('checkbox', { name: 'Select destination: Germany', exact: true })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Select destination: France', exact: true })).toBeChecked();
    await expect(dialog.getByRole('checkbox', { name: 'Select destination: Netherlands', exact: true })).not.toBeChecked();
    for (const row of expectedNodes(edited)) {
      const id = row.node_id; await expand(dialog, id === 'de' ? 'Germany' : 'France');
      await expect(dialog.getByLabel('Destination listening IP: ' + id, { exact: true })).toHaveValue(row.listen_ip);
      await expect(dialog.getByRole('checkbox', { name: 'Direct: ' + id, exact: true })).toBeChecked();
      await expect(dialog.getByRole('checkbox', { name: 'Tunnel: ' + id, exact: true })).toBeChecked();
      await expect(dialog.getByLabel('Direct port: ' + id, { exact: true })).toHaveValue(String(row.direct_port));
      await expect(dialog.getByLabel('Direct Hosts: ' + id, { exact: true })).toHaveValue(row.direct_hosts.join('\n'));
      await expect(dialog.getByLabel('Tunnel port: ' + id, { exact: true })).toHaveValue(String(row.tunnel_port));
      await expect(dialog.getByLabel('Tunnel Hosts: ' + id, { exact: true })).toHaveValue(row.tunnel_hosts.join('\n'));
      await expect(dialog.getByRole('checkbox', { name: 'Dark Swap: ' + id, exact: true })).toBeChecked({ checked: row.swap_enabled });
      if (row.swap_enabled) {
        await expect(dialog.getByLabel('Source Node: de', { exact: true })).toHaveValue('nl');
        await expect(dialog.getByLabel('Source relay listening IP: de', { exact: true })).toHaveValue('198.51.100.30');
        await expect(dialog.getByLabel('Source relay port: de', { exact: true })).toHaveValue('16801');
        await expect(dialog.getByLabel('External entry address: de', { exact: true })).toHaveValue('entry.example.org');
        await expect(dialog.getByLabel('External entry port: de', { exact: true })).toHaveValue(edited ? '444' : '443');
        await expect(dialog.getByLabel('Destination exit port: de', { exact: true })).toHaveValue('18443');
      }
    }
  }

  stage = 'real owner sign-in';
  await page.goto(new URL('/inbounds', base).href);
  await page.getByLabel('Username', { exact: true }).fill(control.username);
  await page.getByLabel('Password', { exact: true }).fill(control.password);
  const capabilitiesResponse = observe('GET', api + '/capabilities');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const capabilityHTTP = await capabilitiesResponse; expect(capabilityHTTP.status()).toBe(200);
  const capabilities = await capabilityHTTP.json(); publicOnly(capabilities);
  expect(capabilities).toMatchObject({ available: true, runtime_apply: false, profile_options: true, profile_sniffing: true, profile_fallbacks: true,
    profile_traffic_options_scope: 'creation_plan', protocols: ['vless'], securities: ['reality'], transports: ['tcp', 'grpc'] });
  const add = page.getByRole('button', { name: 'Add Inbound', exact: true }); await expect(add).toBeEnabled(); await add.click();
  const dialog = page.getByRole('dialog', { name: 'Create Inbound', exact: true }); await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('tab')).toHaveText(['Connection', 'Dark Swap', 'Advanced', 'Review']);
  for (const label of [/^Inbound name/, /^REALITY target/, /^Server names/, /^Short IDs/]) await expect(dialog.getByLabel(label)).toHaveCount(1);
  stage = 'complete creation form';
  await dialog.getByLabel('Inbound name', { exact: true }).fill('Real browser creation');
  await dialog.getByLabel('REALITY target (host:port)', { exact: true }).fill('example.org:443');
  await dialog.getByLabel('Server names (comma-separated)', { exact: true }).fill('example.org');
  await dialog.getByLabel('Short IDs', { exact: true }).fill('aabb, ccdd');
  await advanced(dialog, 'security');
  await dialog.getByLabel('REALITY Xver', { exact: true }).selectOption('2');
  await dialog.getByLabel('Maximum time difference (ms)', { exact: true }).fill('60000');
  await dialog.getByLabel('Minimum client version', { exact: true }).fill('1.8.0');
  await dialog.getByLabel('Maximum client version', { exact: true }).fill('25.9.11');
  await traffic(dialog);
  await dialog.getByRole('checkbox', { name: 'Enable Sniffing', exact: true }).check();
  await dialog.getByRole('checkbox', { name: 'QUIC', exact: true }).check();
  await dialog.getByLabel('Excluded domains', { exact: true }).fill('domain:example.net');
  await dialog.getByLabel('Excluded IPs or CIDRs', { exact: true }).fill('192.0.2.0/24');
  await dialog.getByRole('button', { name: 'Add fallback', exact: true }).click();
  await dialog.getByLabel('Fallback 1 Destination', { exact: true }).fill('127.0.0.1:8080');
  await dialog.getByLabel('Fallback 1 Path', { exact: true }).fill('/health');
  await dialog.getByLabel('Fallback 1 PROXY protocol', { exact: true }).selectOption('2');
  await dialog.getByRole('tab', { name: 'Dark Swap', exact: true }).click();
  await fillDestination(dialog, 'de', 'Germany', '198.51.100.10', '10443', 'de.example.org\nshared.example.org', '1185', 'tunnel.example.org');
  await expect(dialog.getByRole('checkbox', { name: 'Dark Swap: de', exact: true })).not.toBeChecked();
  await dialog.getByRole('checkbox', { name: 'Direct: de', exact: true }).uncheck();
  await expect(dialog.getByLabel('Tunnel port: de', { exact: true })).toHaveValue('1185');
  await dialog.getByRole('tab', { name: 'Connection', exact: true }).click();
  await dialog.getByRole('tab', { name: 'Dark Swap', exact: true }).click();
  await dialog.getByRole('checkbox', { name: 'Direct: de', exact: true }).check();
  await expect(dialog.getByLabel('Direct port: de', { exact: true })).toHaveValue('10443');
  await expect(dialog.getByLabel('Direct Hosts: de', { exact: true })).toHaveValue('de.example.org\nshared.example.org');
  await dialog.getByRole('checkbox', { name: 'Tunnel: de', exact: true }).uncheck();
  await expect(dialog.getByLabel('Direct port: de', { exact: true })).toHaveValue('10443');
  await dialog.getByRole('tab', { name: 'Advanced', exact: true }).click();
  await dialog.getByRole('tab', { name: 'Dark Swap', exact: true }).click();
  await dialog.getByRole('checkbox', { name: 'Tunnel: de', exact: true }).check();
  await expect(dialog.getByLabel('Tunnel port: de', { exact: true })).toHaveValue('1185');
  await expect(dialog.getByLabel('Tunnel Hosts: de', { exact: true })).toHaveValue('tunnel.example.org');
  await fillDestination(dialog, 'fr', 'France', '198.51.100.20', '11443', 'fr.example.org', '1285', 'fr-tunnel.example.org');
  await dialog.getByRole('tab', { name: 'Review', exact: true }).click();
  await dialog.getByLabel('Change note', { exact: true }).fill('Real browser complete setup');
  stage = 'real creation preview and transaction';
  const creationPreviewBody = await checkSetup(dialog, false);
  const created = await persist(dialog, false); inboundID = created.receipt.plan.profile.id;
  expect(Number.isSafeInteger(inboundID) && inboundID > 0).toBe(true);
  expect(created.body).toEqual(creationPreviewBody);
  expect(created.body).toMatchObject({ expected_plan_revision: 0, expected_inbound_revision: 0, reason: 'Real browser complete setup' });
  expect(created.receipt.plan.nodes).toEqual(expectedNodes(false));
  const createdPublicKey = created.receipt.plan.profile.public_key;
  expect(createdPublicKey).toMatch(/^[A-Za-z0-9_-]{43}$/);

  stage = 'reloaded creation persistence';
  const reopened = await reopen();
  expect(reopened.plan.profile.public_key).toBe(createdPublicKey); expect(reopened.plan.nodes).toEqual(expectedNodes(false));
  await verifyProfile(reopened.dialog, false); await verifyNodes(reopened.dialog, false);
  stage = 'complete persisted setup edit';
  await traffic(reopened.dialog);
  await reopened.dialog.getByRole('checkbox', { name: 'Enable Sniffing', exact: true }).uncheck();
  await reopened.dialog.getByLabel('Excluded domains', { exact: true }).fill('domain:example.net\nfull:keep.example.org');
  await reopened.dialog.getByRole('button', { name: 'Remove fallback 1', exact: true }).click();
  await expect(reopened.dialog.locator('.inbound-fallback-row')).toHaveCount(0);
  await reopened.dialog.getByRole('tab', { name: 'Connection', exact: true }).click();
  await reopened.dialog.getByLabel('Inbound name', { exact: true }).fill('Real browser creation edited');
  await reopened.dialog.getByRole('radio', { name: 'gRPC / REALITY', exact: true }).check();
  await reopened.dialog.getByLabel('gRPC service', { exact: true }).fill('dark-grpc');
  await reopened.dialog.getByLabel('Short IDs', { exact: true }).fill('aabb, ccdd, eeff');
  await advanced(reopened.dialog, 'security');
  await reopened.dialog.getByLabel('Maximum time difference (ms)', { exact: true }).fill('120000');
  await advanced(reopened.dialog, 'network');
  await reopened.dialog.getByLabel('gRPC Authority', { exact: true }).fill('edge.example.org:8443');
  await reopened.dialog.getByRole('checkbox', { name: 'gRPC MultiMode', exact: true }).check();
  await reopened.dialog.getByRole('tab', { name: 'Dark Swap', exact: true }).click();
  await expand(reopened.dialog, 'Germany');
  await reopened.dialog.getByRole('checkbox', { name: 'Dark Swap: de', exact: true }).check();
  await reopened.dialog.getByLabel('Source Node: de', { exact: true }).selectOption('nl');
  await reopened.dialog.getByLabel('Source relay listening IP: de', { exact: true }).fill('198.51.100.30');
  await reopened.dialog.getByLabel('Source relay port: de', { exact: true }).fill('16801');
  await reopened.dialog.getByLabel('External entry address: de', { exact: true }).fill('entry.example.org');
  await reopened.dialog.getByLabel('External entry port: de', { exact: true }).fill('444');
  await reopened.dialog.getByLabel('Destination exit port: de', { exact: true }).fill('18443');
  await expand(reopened.dialog, 'France');
  await reopened.dialog.getByLabel('Direct Hosts: fr', { exact: true }).fill('fr.example.org\nfr-edge.example.org');
  await reopened.dialog.getByRole('tab', { name: 'Review', exact: true }).click();
  await reopened.dialog.getByLabel('Change note', { exact: true }).fill('Real browser edited setup');
  stage = 'real update preview and transaction';
  const updatePreviewBody = await checkSetup(reopened.dialog, true), updated = await persist(reopened.dialog, true);
  expect(updated.body).toEqual(updatePreviewBody);
  expect(updated.body.profile.options.fallbacks).toEqual([]);
  expect(updated.body).toMatchObject({ expected_plan_revision: 1, expected_inbound_revision: 1, reason: 'Real browser edited setup' });
  expect(updated.receipt.plan.profile).toMatchObject({ id: inboundID, name: 'Real browser creation edited', transport: 'grpc', grpc_service: 'dark-grpc',
    short_ids: ['aabb', 'ccdd', 'eeff'], public_key: createdPublicKey });
  expect(updated.receipt.plan.nodes).toEqual(expectedNodes(true));
  expect(updated.receipt.event_id).not.toBe(created.receipt.event_id);

  stage = 'reloaded update persistence';
  const final = await reopen(); expect(final.plan).toEqual(updated.receipt.plan);
  await verifyProfile(final.dialog, true); await verifyNodes(final.dialog, true);
  expect(mutations).toEqual([{ method: 'POST', path: '/api/v1/session/login' }, { method: 'POST', path: api + '/preview' },
    { method: 'POST', path: api }, { method: 'POST', path: api + '/preview' }, { method: 'PUT', path: api + '/' + inboundID }]);
  if (diagnostics.length) throw new Error(diagnostics.join('; '));
  await writeFile(control.result_path, JSON.stringify({ inbound_id: inboundID, created_public_key: createdPublicKey,
    created_event_id: created.receipt.event_id, updated_event_id: updated.receipt.event_id }) + '\n', { mode: 0o600, flag: 'wx' });
  process.stdout.write('NATIVE_UI_CREATION_PLAN_VERIFIED\n');
} catch (error) {
  const message = String(error?.message ?? error).split(control.password).join('[REDACTED]');
  const rendered = page ? await page.locator('body').innerText({ timeout: 1000 }).catch(() => '[rendered body unavailable]') : '[page unavailable]';
  const context = JSON.stringify({ diagnostics, requests: requests.slice(-40), rendered: rendered.slice(0, 3000) }).split(control.password).join('[REDACTED]');
  throw new Error('Live creation acceptance failed during ' + stage + ': ' + message + '\nSafe browser diagnostics: ' + context);
} finally {
  await browser?.close();
  await new Promise(resolve => denyProxy.close(resolve));
}
