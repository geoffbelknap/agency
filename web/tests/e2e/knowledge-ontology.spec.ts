import { expect, test } from '@playwright/test';
import { installAgencyMocks } from './support/mockAgencyApi';

const controllers = new WeakMap<object, { assertNoUnhandledRequests: () => void }>();

test.describe('Knowledge core surface', () => {
  test.beforeEach(async ({ page }) => {
    const controller = await installAgencyMocks(page);
    controllers.set(page, controller);
  });

  test.afterEach(async ({ page }) => {
    controllers.get(page)?.assertNoUnhandledRequests();
  });

  test('hides ontology review and governance surfaces in the default workspace', async ({ page }) => {
    await page.goto('/knowledge/search');

    await expect(page.getByRole('heading', { name: 'Knowledge', exact: true })).toBeVisible();
    await expect(page.getByText('Query Knowledge')).toBeVisible();
    await expect(page.getByText('Core Knowledge Surface')).toBeVisible();
    await expect(
      page.getByText(/advanced graph governance, ontology review, quarantine, and topology inspection are experimental/i),
    ).toBeVisible();

    await expect(page.getByRole('heading', { name: 'Ontology Review', exact: true })).toHaveCount(0);
    await expect(page.getByText('rollout-readiness', { exact: true })).toHaveCount(0);
    await expect(page.getByText('policy-drift', { exact: true })).toHaveCount(0);
  });
});
