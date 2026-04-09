import { expect, test, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;

test.describe.configure({ timeout: 120_000 });

async function settle(page: Page) {
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(1500);
  await expect(page.getByText(APP_ERROR_PATTERN)).toHaveCount(0);
}

async function authHeaders(page: Page): Promise<Record<string, string>> {
  const configResponse = await page.request.get('/__agency/config');
  const config = configResponse.ok() ? await configResponse.json() : {};
  const token = (config as { token?: string })?.token ?? '';
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const allowDanger = process.env.AGENCY_E2E_ALLOW_DANGER === '1';
const dangerConfirm = process.env.AGENCY_E2E_DANGER_CONFIRM === 'destroy-all';

test.skip(!allowDanger || !dangerConfirm, 'requires explicit live-danger opt-in');

test('live danger suite supports destroy all with explicit confirmation', async ({ page }) => {
  await page.goto('/admin?tab=danger');
  await settle(page);

  await expect(page.getByText('Danger Zone')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Destroy All' })).toBeVisible();

  await page.getByRole('button', { name: 'Destroy All' }).click();
  const dialog = page.getByRole('alertdialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(/cannot be undone/i)).toBeVisible();

  const destroyResponsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' &&
    response.url().includes('/api/v1/admin/destroy'),
  );
  await dialog.getByRole('button', { name: 'Destroy Everything' }).click();
  const destroyResponse = await destroyResponsePromise;
  expect(destroyResponse.ok()).toBeTruthy();

  await expect.poll(async () => {
    const response = await page.request.get('/api/v1/health', { headers: await authHeaders(page) });
    if (!response.ok()) {
      return 'down';
    }
    const body = await response.json() as { status?: string };
    return body.status ?? 'unknown';
  }, {
    timeout: 60_000,
    intervals: [1000, 2000, 5000],
  }).toBe('ok');

  await page.goto('/');
  await settle(page);
  await expect(page.getByRole('heading', { name: SETUP_HEADING_PATTERN })).toBeVisible();
});
