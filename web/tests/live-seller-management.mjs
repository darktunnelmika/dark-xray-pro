// Real compiled UI -> cookie/CSRF API -> disposable SQLite acceptance.
import { chromium, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';

const base = new URL(process.env.DARK_PRO_QA_UI_URL ?? '');
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || !base.port) throw new Error('An isolated loopback test origin is required');
const control = JSON.parse(await readFile(process.env.DARK_PRO_QA_CONTROL, 'utf8'));
for (const field of ['username', 'password', 'representative_username', 'representative_password']) {
  if (typeof control[field] !== 'string' || !control[field]) throw new Error('Invalid private test identity');
}
const browser = await chromium.launch({ headless: true });
const errors = [];
let stage = 'launch';
async function signedIn(username, password, path) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  // This is an egress guard only: all same-origin HTTP responses come from the
  // actual Go server, without response mocking or injected authentication.
  await context.route('**/*', route => new URL(route.request().url()).origin === base.origin ? route.continue() : route.abort());
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(new URL(path, base).href);
  await page.getByLabel('Username', { exact: true }).fill(username);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  return page;
}
try {
  stage = 'owner grant';
  const owner = await signedIn(control.username, control.password, '/representatives');
  await owner.getByRole('button', { name: 'Manage account', exact: true }).click();
  await owner.getByRole('button', { name: 'Subordinate sellers', exact: true }).click();
  await expect(owner.getByLabel('Allow seller management')).not.toBeChecked();
  await owner.getByLabel('Allow seller management').check();
  await owner.getByLabel('Seller permission reason').fill('Enable isolated representative seller workflow');
  await owner.getByRole('button', { name: 'Save seller permission', exact: true }).click();
  await expect(owner.getByText('Change recorded.', { exact: true })).toBeVisible();

  stage = 'representative creates seller';
  const rep = await signedIn(control.representative_username, control.representative_password, '/account');
  await expect(rep.getByLabel('Allow seller management')).toHaveCount(0);
  await rep.getByRole('button', { name: 'Add seller', exact: true }).click();
  await rep.getByLabel('Seller ID', { exact: true }).fill('browser-seller');
  await rep.getByLabel('Seller name', { exact: true }).fill('Real browser seller');
  await rep.getByLabel('Telegram user ID', { exact: true }).fill('123456789');
  await rep.getByRole('button', { name: 'Create seller', exact: true }).click();
  await rep.getByRole('button', { name: /Real browser seller/ }).click();

  stage = 'representative allocation';
  await rep.getByLabel('Quota volume unit').selectOption('MiB');
  await rep.getByLabel('Quota volume', { exact: true }).fill('10');
  await rep.getByLabel('Quota unlimited units').fill('2');
  await rep.getByLabel('Quota transfer reason').fill('Allocate real SQLite reserve');
  await rep.getByRole('button', { name: 'Record transfer', exact: true }).click();
  await expect(rep.getByRole('region', { name: 'Seller details' }).getByText('10,485,760', { exact: true })).toBeVisible();

  stage = 'disable preserves reserve';
  await rep.getByLabel('Seller state reason').fill('Pause synthetic seller');
  await rep.getByRole('button', { name: 'Disable seller', exact: true }).click();
  await expect(rep.getByRole('button', { name: 'Enable seller', exact: true })).toBeVisible();
  await expect(rep.getByRole('region', { name: 'Seller details' }).getByText('10,485,760', { exact: true })).toBeVisible();

  stage = 'reclaim unused credit';
  await rep.getByLabel('Quota action').selectOption('reclaim');
  await rep.getByLabel('Quota volume', { exact: true }).fill('1');
  await rep.getByLabel('Quota unlimited units').fill('1');
  await rep.getByLabel('Quota transfer reason').fill('Reclaim only unused synthetic reserve');
  await rep.getByRole('button', { name: 'Record transfer', exact: true }).click();
  await expect(rep.getByRole('region', { name: 'Seller details' }).getByText('9,437,184', { exact: true })).toBeVisible();

  stage = 'owner revokes and existing representative session expires';
  const ownerSellers = owner.getByRole('region', { name: 'Seller management' });
  await ownerSellers.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(ownerSellers.getByRole('button', { name: /Real browser seller/ })).toBeVisible();
  await owner.getByLabel('Allow seller management').uncheck();
  await owner.getByLabel('Seller permission reason').fill('Revoke synthetic representative seller access');
  await owner.getByRole('button', { name: 'Save seller permission', exact: true }).click();
  await expect(owner.getByText('Change recorded.', { exact: true })).toBeVisible();
  await rep.getByRole('region', { name: 'Seller management' }).getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(rep.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  if (errors.length) throw new Error('Browser page errors: ' + errors.join('; '));
  console.log('REAL_SELLER_MANAGEMENT_BROWSER_PASS');
} catch (error) {
  console.error('Seller acceptance stage: ' + stage);
  throw error;
} finally {
  await browser.close();
}
