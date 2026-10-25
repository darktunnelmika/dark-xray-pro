// Shared selectors and public-response assertions for the opt-in, real-API
// authoring harnesses. Every edit still goes through the rendered UI.
import { expect } from '@playwright/test';

export const protocols = ['vmess', 'vless', 'trojan', 'shadowsocks', 'wireguard', 'hysteria', 'http', 'mixed', 'tunnel', 'tun', 'mtproto', 'amneziawg', 'tuic', 'masque'];
export const transports = ['tcp', 'kcp', 'ws', 'grpc', 'httpupgrade', 'xhttp', 'xdrive'];

export function assertCapabilities(value) {
  expect(value).toMatchObject({ version: 4, available: true, authoring_spec: true, explicit_hub_destination: true,
    endpoint_options: true, profile_tls: true, profile_websocket: true, profile_http_upgrade: true, runtime_apply: false });
  expect([...value.protocols].sort()).toEqual([...protocols].sort());
  expect(value.transports).toEqual(transports);
  expect([...value.securities].sort()).toEqual(['none', 'reality', 'tls']);
  expect(value.protocol_catalog.map(entry => entry.protocol).sort()).toEqual([...protocols].sort());
}

export function field(dialog, path) { return dialog.locator(`[data-creation-field="spec.${path}"]`); }

export async function reveal(locator) {
  // Open outer disclosures first, using the same summary clicks as a user.
  for (let i = 0; i < 8; i++) {
    const closed = locator.locator('xpath=ancestor::details[not(@open)]');
    if (!await closed.count()) break;
    await closed.first().locator(':scope > summary').click();
  }
  await expect(locator).toBeVisible();
  return locator;
}

export async function section(dialog, name) {
  const tab = dialog.getByRole('tab', { name, exact: true });
  if (await tab.getAttribute('aria-selected') !== 'true') await tab.click();
}

export async function input(dialog, path, tab = 'Connection') {
  await section(dialog, tab);
  return reveal(field(dialog, path));
}

export async function assertNewSelectors(dialog) {
  expect(await field(dialog, 'protocol').locator('option').evaluateAll(options => options.map(option => option.value))).toEqual(protocols);
  expect(await field(dialog, 'network').locator('option').evaluateAll(options => options.map(option => option.value))).toEqual(transports);
  await expect(field(dialog, 'protocol')).toHaveValue('vless');
  await section(dialog, 'Dark Swap');
  await expect(dialog.getByRole('checkbox', { name: 'Select destination: HUB — this server', exact: true })).not.toBeChecked();
  await expect(dialog.locator('[data-creation-field="node"]:checked')).toHaveCount(0);
  await section(dialog, 'Connection');
}

export function publicOnly(value, path = 'response') {
  if (typeof value === 'string') {
    if (value.includes('-----BEGIN')) throw new Error('PEM material exposed at ' + path);
    return;
  }
  if (!value || typeof value !== 'object') return;
  for (const [key, nested] of Object.entries(value)) {
    const normalized = key.replace(/[_-]/g, '').toLowerCase();
    if (['privatekey', 'realityprivatekey', 'secretkey', 'password', 'decryption', 'certificate', 'key', 'certificatefile', 'keyfile', 'headers', 'tlscredentials', 'certificatepem', 'privatekeypem', 'config'].includes(normalized)) {
      throw new Error('Private field exposed at ' + path + '.' + key);
    }
    publicOnly(nested, path + '.' + key);
  }
}

export function assertSpecOnly(profile) {
  publicOnly(profile);
  expect(profile.spec).toMatchObject({ version: 1, protocol: 'vless' });
  expect(profile.options).toBeUndefined();
  expect(profile.tls).toBeUndefined();
  expect(profile.spec_secret_paths).toEqual(expect.arrayContaining(['settings.decryption']));
}
