import { expect, test } from '@playwright/test';
import { installAgencyMocks } from './support/mockAgencyApi';

const controllers = new WeakMap<object, { assertNoUnhandledRequests: () => void }>();

test.describe('Admin tabs', () => {
  test.beforeEach(async ({ page }) => {
    const controller = await installAgencyMocks(page);
    controllers.set(page, controller);
  });

  test.afterEach(async ({ page }) => {
    controllers.get(page)?.assertNoUnhandledRequests();
  });

  test('all admin sections render with mocked data', async ({ page }) => {
    const tabs = [
      { path: '/admin/infrastructure', locator: page.locator('main').getByText('gateway', { exact: true }).first() },
      { path: '/admin/knowledge', locator: page.getByText('Structural Review') },
      { path: '/admin/egress', locator: page.getByText('provider-a.example.com').first() },
      { path: '/admin/policy', locator: page.getByRole('button', { name: 'Validate policy' }) },
      { path: '/admin/doctor', locator: page.getByRole('button', { name: 'Run Doctor' }) },
      { path: '/admin/usage', locator: page.getByText('Usage & cost') },
      { path: '/admin/audit', locator: page.locator('main').getByRole('button', { name: 'Search', exact: true }) },
      { path: '/admin/setup', locator: page.getByRole('link', { name: 'Re-run setup wizard' }) },
      { path: '/admin/danger', locator: page.getByRole('button', { name: 'Destroy All' }) },
    ];

    for (const tab of tabs) {
      await page.goto(tab.path);
      await expect(page.locator('main').getByText('Admin', { exact: true }).first()).toBeVisible();
      await expect(tab.locator).toBeVisible();
    }
  });
});
