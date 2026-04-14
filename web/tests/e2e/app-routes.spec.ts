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
    await expect(page.getByRole('heading', { name: 'Welcome to Agency' })).toBeVisible();

    await page.goto('/channels');
    await expect(page.getByRole('link', { name: /channels/i })).toBeVisible();
    await expect(page.getByText('Hello from Alice')).toBeVisible();

    await page.getByRole('link', { name: /agents/i }).click();
    await expect(page.getByRole('heading', { name: 'Agents' })).toBeVisible();
    await expect(page.getByText('alice')).toBeVisible();

    await page.getByRole('link', { name: /missions/i }).click();
    await expect(page.getByRole('heading', { name: 'Missions' })).toBeVisible();
    await expect(page.getByText('release-train')).toBeVisible();

    await page.getByRole('link', { name: /knowledge/i }).click();
    await expect(page.getByRole('heading', { name: 'Knowledge' })).toBeVisible();
    await expect(page.getByText('Release notes')).toBeVisible();

    await page.getByRole('link', { name: /profiles/i }).click();
    await expect(page.getByRole('heading', { name: 'Profiles' })).toBeVisible();
    await expect(page.getByRole('cell', { name: 'Geoff', exact: true })).toBeVisible();

    await page.getByRole('link', { name: /admin/i }).click();
    await expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Infrastructure', exact: true })).toBeVisible();
    await expect(page.getByText('gateway', { exact: true })).toBeVisible();
  });

  test('direct deep links render representative detail views', async ({ page }) => {
    const routeExpectations = [
      { path: '/overview', locator: page.getByText('Platform health at a glance') },
      { path: '/channels/general', locator: page.getByText('Hello from Alice', { exact: true }) },
      { path: '/agents/alice', locator: page.locator('#panel-overview').getByText('prepare weekly release notes', { exact: true }) },
      { path: '/missions/release-train', locator: page.getByText('Prepare weekly release notes and rollout summary.') },
      { path: '/knowledge/graph', locator: page.getByRole('button', { name: 'Graph' }) },
      { path: '/knowledge/search', locator: page.getByText('Query Knowledge') },
      { path: '/profiles/operator', locator: page.getByText('Primary operator profile') },
      { path: '/teams', locator: page.getByText('Manage agent groups') },
    ];

    for (const route of routeExpectations) {
      await page.goto(route.path);
      await expect(route.locator).toBeVisible();
    }
  });
});
