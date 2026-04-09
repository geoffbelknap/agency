import { expect, test } from '@playwright/test';
import { installAgencyMocks } from './support/mockAgencyApi';

const controllers = new WeakMap<object, { assertNoUnhandledRequests: () => void }>();

test.describe('Knowledge ontology review', () => {
  test.beforeEach(async ({ page }) => {
    const controller = await installAgencyMocks(page);
    controllers.set(page, controller);
  });

  test.afterEach(async ({ page }) => {
    controllers.get(page)?.assertNoUnhandledRequests();
  });

  test('promote, reject, and restore update the review surface', async ({ page }) => {
    await page.goto('/knowledge/search');

    await expect(page.getByRole('heading', { name: 'Knowledge', exact: true })).toBeVisible();
    await expect(page.getByText('Ontology Review')).toBeVisible();
    await expect(page.getByText('rollout-readiness', { exact: true })).toBeVisible();
    await expect(page.getByText('policy-drift', { exact: true })).toBeVisible();

    const rolloutCandidate = page
      .getByText('rollout-readiness', { exact: true })
      .locator('xpath=ancestor::div[contains(@class,"justify-between")][1]');
    await rolloutCandidate.getByTitle('Promote to ontology').click();

    const policyCandidate = page
      .getByText('policy-drift', { exact: true })
      .locator('xpath=ancestor::div[contains(@class,"justify-between")][1]');
    await policyCandidate.getByTitle('Reject candidate').click();

    const recentDecisions = page.locator('div').filter({ hasText: 'Recent Decisions' }).first();
    await expect(recentDecisions.getByText('rollout-readiness', { exact: true })).toBeVisible();
    await expect(recentDecisions.getByText('promote', { exact: true })).toBeVisible();
    await expect(recentDecisions.getByText('policy-drift', { exact: true })).toBeVisible();
    await expect(recentDecisions.getByText('reject', { exact: true })).toBeVisible();

    const rolloutDecision = recentDecisions
      .getByText('rollout-readiness', { exact: true })
      .locator('xpath=ancestor::div[contains(@class,"justify-between")][1]');
    await rolloutDecision.getByRole('button', { name: /restore/i }).click();

    const pendingCandidates = page.locator('div').filter({ hasText: 'Pending Candidates' }).first();
    await expect(pendingCandidates.getByText('rollout-readiness', { exact: true }).first()).toBeVisible();
    await expect(page.locator('div').filter({ hasText: 'rollout-readiness' }).filter({ hasText: 'restore' }).first()).toBeVisible();
  });
});
