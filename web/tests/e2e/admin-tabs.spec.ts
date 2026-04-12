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
      { path: '/admin/infrastructure', locator: page.getByText('gateway') },
      { path: '/admin/hub', locator: page.getByText('Packages and instances') },
      { path: '/admin/intake', locator: page.getByText('slack-intake') },
      { path: '/admin/knowledge', locator: page.getByText('Query Knowledge') },
      { path: '/admin/capabilities', locator: page.getByText('browser.open') },
      { path: '/admin/presets', locator: page.getByText('platform-expert') },
      { path: '/admin/trust', locator: page.getByText('5 — Autonomous') },
      { path: '/admin/egress', locator: page.getByText('api.anthropic.com') },
      { path: '/admin/policy', locator: page.getByRole('button', { name: 'Validate' }) },
      { path: '/admin/doctor', locator: page.getByRole('button', { name: 'Run Doctor' }) },
      { path: '/admin/usage', locator: page.getByText('LLM usage and estimated spend') },
      { path: '/admin/events', locator: page.getByText('message.created') },
      { path: '/admin/webhooks', locator: page.getByText('release-events', { exact: true }) },
      { path: '/admin/notifications', locator: page.getByText('agency-geoff', { exact: true }) },
      { path: '/admin/audit', locator: page.getByRole('button', { name: 'Summarize' }) },
      { path: '/admin/setup', locator: page.getByText('Re-run Setup Wizard') },
      { path: '/admin/danger', locator: page.getByRole('button', { name: 'Destroy All' }) },
    ];

    for (const tab of tabs) {
      await page.goto(tab.path);
      await expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible();
      await expect(tab.locator).toBeVisible();
    }
  });
});
