// Real compiled UI -> cookie/CSRF API -> disposable SQLite acceptance.
import { chromium, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';

const base = new URL(process.env.DARK_PRO_QA_UI_URL ?? '');
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || !base.port) throw new Error('An isolated loopback test origin is required');
const control = JSON.parse(await readFile(process.env.DARK_PRO_QA_CONTROL, 'utf8'));
for (const field of ['username', 'password', 'representative_username', 'representative_password']) {
  if (typeof control[field] !== 'string' || !control[field]) throw new Error('Invalid private test identity');
}
const browser = await chromium.launch({ headless: true, args: ['--no-zygote'] });
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
  await rep.getByLabel('Quota unlimited units').fill('10');
  await rep.getByLabel('Quota transfer reason').fill('Allocate real SQLite reserve');
  await rep.getByRole('button', { name: 'Record transfer', exact: true }).click();
  await expect(rep.getByRole('region', { name: 'Seller details' }).locator('.seller-balances').getByText('10,485,760', { exact: true })).toBeVisible();

  stage = 'catalog order commit with lost response';
  const orders = [];
  let responseLost = false;
  await rep.route('**/api/v1/workspace/representatives/browser-seller-rep/sellers/browser-seller/catalog/browser-catalog-product/clients', async route => {
    orders.push(route.request().postDataJSON());
    if (!responseLost) {
      // Forward to the real handler and await the durable response, then
      // discard only that response. Never fabricate an order or receipt.
      const response = await route.fetch();
      if (response.status() !== 201) throw new Error('Real catalog order was rejected: ' + response.status());
      await response.body();
      responseLost = true;
      await route.abort('failed');
    } else await route.continue();
  });
  await rep.getByLabel('Catalog order product').selectOption('browser-catalog-product');
  await expect(rep.getByLabel('Catalog order terms')).toContainText('250,000');
  await rep.getByLabel('Catalog client ID').fill('browser-catalog-client');
  await rep.getByLabel('Catalog client name').fill('Real catalog customer');
  await rep.getByLabel('Catalog order reason').fill('Real browser catalog accounting acceptance');
  await rep.getByRole('button', { name: 'Record catalog order', exact: true }).click();
  await expect(rep.getByRole('button', { name: 'Retry same request', exact: true })).toBeVisible();
  await rep.reload();
  await rep.getByRole('button', { name: 'Retry same request', exact: true }).click();
  await expect(rep.getByText('Recovered existing receipt', { exact: true })).toBeVisible();
  await expect(rep.getByLabel('Catalog receipt terms')).toContainText('2 × 3 = 6');
  await expect(rep.getByLabel('Catalog receipt terms')).toContainText('no payment recorded');
  expect(orders).toHaveLength(2);
  expect(orders[0]).toEqual(orders[1]);
  expect(orders[0]).toMatchObject({ product_id: 'browser-catalog-product', expected_product_revision: 2,
    expected_price_toman: 250000, expected_duration_days: 60, expected_user_count: 3 });

  stage = 'disable preserves reserve';
  await rep.getByLabel('Seller state reason').fill('Pause synthetic seller');
  await rep.getByRole('button', { name: 'Disable seller', exact: true }).click();
  await expect(rep.getByRole('button', { name: 'Enable seller', exact: true })).toBeVisible();
  await expect(rep.getByRole('region', { name: 'Seller details' }).locator('.seller-balances').getByText('10,485,760', { exact: true })).toBeVisible();

  stage = 'reclaim unused credit';
  await rep.getByLabel('Quota volume unit').selectOption('MiB');
  await rep.getByLabel('Quota action').selectOption('reclaim');
  await rep.getByLabel('Quota volume', { exact: true }).fill('1');
  await rep.getByLabel('Quota unlimited units').fill('1');
  await rep.getByLabel('Quota transfer reason').fill('Reclaim only unused synthetic reserve');
  await rep.getByRole('button', { name: 'Record transfer', exact: true }).click();
  await expect(rep.getByRole('region', { name: 'Seller details' }).locator('.seller-balances').getByText('9,437,184', { exact: true })).toBeVisible();

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
