import { chromium, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';

const base = new URL(process.env.DARK_PRO_QA_UI_URL ?? '');
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || !base.port) throw new Error('Isolated loopback origin required');
const control = JSON.parse(await readFile(process.env.DARK_PRO_QA_CONTROL, 'utf8'));
const browser = await chromium.launch({ headless: true });
const errors = [];
try {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  await context.route('**/*', route => new URL(route.request().url()).origin === base.origin ? route.continue() : route.abort());
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  page.on('pageerror', error => errors.push(error.message));
  page.on('dialog', dialog => dialog.accept());
  await page.goto(new URL('/clients', base).href);
  await page.getByLabel('Username', { exact: true }).fill(control.username);
  await page.getByLabel('Password', { exact: true }).fill(control.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const get = path => page.evaluate(async path => {
    const response = await fetch(path, { credentials: 'same-origin', cache: 'no-store' });
    if (!response.ok) throw new Error('Synthetic browser read failed: ' + response.status);
    return response.json();
  }, path);

  async function archive(id) {
    const directory = page.getByRole('region', { name: 'Client directory' });
    await directory.locator('.clients-explorer-row').filter({ hasText: id }).getByRole('button', { name: 'Manage', exact: true }).click();
    const form = page.locator('form').filter({ has: page.getByRole('heading', { name: 'Client action', exact: true }) });
    await form.getByRole('combobox').selectOption('archive');
    await form.getByLabel('Operation reason', { exact: true }).fill('Synthetic representative manual deletion');
    await form.getByRole('button', { name: 'Record state change', exact: true }).click();
    await expect.poll(async () => (await get('/api/v1/workspace/clients/' + id)).archived).toBe(true);
  }
  await archive('refund-volume');
  await expect.poll(async () => {
    return (await get('/api/v1/workspace/clients/refund-volume/volume-refund')).state;
  }, { timeout: 15000 }).toBe('already_refunded');
  await archive('refund-unlimited');
  const unlimited = await get('/api/v1/workspace/clients/refund-unlimited/volume-refund');
  expect(unlimited.state).toBe('not_volume');
  await page.goto(new URL('/finance', base).href);
  const history = page.getByRole('region', { name: 'Volume return history' });
  await expect(history.getByText('refund-volume', { exact: true })).toBeVisible();
  await expect(history.getByText('100', { exact: true })).toBeVisible();
  await expect(history.getByText('refund-unlimited', { exact: true })).toHaveCount(0);
  const balance = (await get('/api/v1/workspace/me')).balance;
  expect(balance.volume_credit_bytes).toBe(1000);
  expect(balance.unlimited_credit_units).toBe(8);
  if (errors.length) throw new Error(errors.join('; '));
  console.log('REAL_VOLUME_REFUND_BROWSER_PASS');
} finally {
  await browser.close();
}
