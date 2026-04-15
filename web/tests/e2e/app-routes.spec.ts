import { expect, test } from '@playwright/test';
import { installAgencyMocks } from './support/mockAgencyApi';

const controllers = new WeakMap<object, { assertNoUnhandledRequests: () => void }>();

test.describe('Agency app routes', () => {
  test.beforeEach(async ({ page }) => {
    const controller = await installAgencyMocks(page);
    controllers.set(page, controller);
  });

  test.afterEach(async ({ page }) => {
    controllers.get(page)?.assertNoUnhandledRequests();
  });

  test('setup wizard and primary navigation routes render', async ({ page }) => {
    await page.goto('/setup');
    await expect(page.getByRole('heading', { name: 'Prepare the workspace' })).toBeVisible();

    await page.goto('/overview');
    await expect(page.getByText('Contract-first control plane')).toBeVisible();

    await page.goto('/channels');
    await expect(page.getByRole('link', { name: /channels/i })).toBeVisible();
    await expect(page.getByText('Hello from Alice')).toBeVisible();

    await page.getByRole('link', { name: /^Agents\b/ }).click();
    await expect(page.locator('main').getByText('alice', { exact: true }).first()).toBeVisible();

    await page.getByRole('link', { name: /^Knowledge\b/ }).click();
    await expect(page.locator('main').getByRole('tab', { name: 'Browser' })).toBeVisible();
    await expect(page.getByText('Release notes').first()).toBeVisible();

    await page.goto('/admin');
    await expect(page.locator('main').getByRole('heading', { name: 'Infrastructure' })).toBeVisible();
    await expect(page.getByText('Infrastructure', { exact: true }).first()).toBeVisible();
    await expect(page.locator('main').getByText('gateway', { exact: true }).first()).toBeVisible();
  });

  test('direct deep links render representative detail views', async ({ page }) => {
    const routeExpectations = [
      { path: '/overview', locator: page.getByText('Contract-first control plane') },
      { path: '/channels/general', locator: page.getByText('Hello from Alice', { exact: true }) },
      { path: '/agents/alice', locator: page.locator('main').getByText('alice', { exact: true }).first() },
      { path: '/knowledge/graph', locator: page.locator('main').getByText('2 nodes', { exact: true }).first() },
      { path: '/knowledge/search', locator: page.locator('main').getByRole('tab', { name: 'Search' }) },
      { path: '/admin/infrastructure', locator: page.locator('main').getByText('gateway', { exact: true }).first() },
      { path: '/admin/audit', locator: page.locator('main').getByText('LLM_DIRECT', { exact: true }).first() },
    ];

    for (const route of routeExpectations) {
      await page.goto(route.path);
      await expect(route.locator).toBeVisible();
    }
  });
});
