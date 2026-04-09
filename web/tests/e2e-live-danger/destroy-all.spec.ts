import { expect, test, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;

test.describe.configure({ timeout: 120_000 });

async function settle(page: Page) {
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(1500);
  await expect(page.getByText(APP_ERROR_PATTERN)).toHaveCount(0);
}

const allowDanger = process.env.AGENCY_E2E_ALLOW_DANGER === '1';
const dangerConfirm = process.env.AGENCY_E2E_DANGER_CONFIRM === 'destroy-all';

test.skip(!allowDanger || !dangerConfirm, 'requires explicit live-danger opt-in');

test('live danger suite supports destroy all with explicit confirmation', async ({ page }) => {
  const configResponse = await page.request.get('/__agency/config');
  expect(configResponse.ok()).toBeTruthy();
  const runtimeConfig = await configResponse.json() as { token?: string };
  expect(runtimeConfig.token).toBeTruthy();

  await page.goto('/admin/danger');
  await settle(page);

  const dangerHeading = page.getByRole('heading', { name: 'Danger Zone' }).last();
  const destroyAllButton = page.getByRole('button', { name: 'Destroy All' });

  await expect(dangerHeading).toBeVisible();
  await expect(destroyAllButton).toBeVisible();

  await destroyAllButton.click();
  const dialog = page.getByRole('alertdialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(/cannot be undone/i)).toBeVisible();

  await dialog.getByRole('button', { name: 'Destroy Everything' }).click();

  await expect.poll(async () => {
    try {
      const response = await page.request.get('/health');
      return response.ok() ? 'up' : 'down';
    } catch {
      return 'down';
    }
  }, {
    timeout: 60_000,
    intervals: [1000, 2000, 5000],
  }).toBe('down');
});
