import { expect, test, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;

async function settle(page: Page) {
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(1500);
  await expect(page.getByText(APP_ERROR_PATTERN)).toHaveCount(0);
}

async function expectSetupOrInitialized(page: Page) {
  await settle(page);
  const setupHeading = page.getByRole('heading', { name: SETUP_HEADING_PATTERN });
  if (await setupHeading.count()) {
    await expect(setupHeading.first()).toBeVisible();
    await expect(page).toHaveURL(/\/setup$/);
    return false;
  }
  return true;
}

function uniqueName(prefix: string) {
  return `${prefix}-${Date.now()}`;
}

async function bestEffortDelete(page: Page, path: string) {
  if (page.isClosed()) {
    return;
  }
  const status = await page.evaluate(async (requestPath) => {
    const configResponse = await fetch('/__agency/config');
    const config = configResponse.ok ? await configResponse.json() : {};
    const token = config?.token ?? '';
    const response = await fetch(requestPath, {
      method: 'DELETE',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    return response.status;
  }, path);
  if (status === 200 || status === 204 || status === 404) {
    return;
  }
  throw new Error(`cleanup failed for ${path}: ${status}`);
}

test('live stack supports profile create, edit, and delete flow', async ({ page }) => {
  const profileId = uniqueName('playwright-profile');
  const updatedDisplayName = `${profileId} updated`;
  const updatedBio = 'Created by live Playwright coverage';

  try {
    await page.goto('/profiles');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await bestEffortDelete(page, `/api/v1/admin/profiles/${encodeURIComponent(profileId)}`);
    await page.evaluate((id) => {
      let promptCalls = 0;
      window.prompt = () => (promptCalls++ === 0 ? id : 'operator');
    }, profileId);

    await page.getByRole('button', { name: 'Create' }).click();
    await settle(page);
    await expect(page.getByRole('heading', { name: profileId })).toBeVisible();

    await page.getByRole('button', { name: 'Edit Profile' }).click();
    await page.locator('input').first().fill(updatedDisplayName);
    await page.locator('textarea').first().fill(updatedBio);
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByRole('button', { name: 'Edit Profile' })).toBeVisible();

    await expect(page.getByRole('heading', { name: updatedDisplayName })).toBeVisible();
    await expect(page.getByText(updatedBio)).toBeVisible();

    await page.evaluate(() => {
      window.confirm = () => true;
    });
    await page.getByRole('button', { name: /Delete/ }).click();
    await settle(page);

    await expect(page.getByRole('heading', { name: updatedDisplayName })).toHaveCount(0);
  } finally {
    await bestEffortDelete(page, `/api/v1/admin/profiles/${encodeURIComponent(profileId)}`);
  }
});

test('live stack supports webhook create, rotate, and delete flow', async ({ page }) => {
  const webhookName = uniqueName('playwright-webhook');
  const eventType = 'operator_alert';

  try {
    await page.goto('/admin/webhooks');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await bestEffortDelete(page, `/api/v1/events/webhooks/${encodeURIComponent(webhookName)}`);

    await page.getByRole('button', { name: 'Create' }).click();
    await page.getByPlaceholder('name').fill(webhookName);
    await page.getByPlaceholder('event type').fill(eventType);
    await page.getByRole('button', { name: /^Create$/ }).click();
    await settle(page);

    await expect(page.getByText(`Secret for "${webhookName}"`)).toBeVisible();
    const webhookRow = page.locator('tr').filter({ has: page.getByText(webhookName, { exact: true }) }).first();
    await expect(webhookRow).toBeVisible();

    await webhookRow.getByRole('button').first().click();
    await settle(page);
    await expect(page.getByText(`Secret for "${webhookName}"`)).toBeVisible();

    await webhookRow.getByRole('button').nth(1).click({ force: true });
    await settle(page);
    await expect(page.getByRole('cell', { name: webhookName })).toHaveCount(0);
  } finally {
    await bestEffortDelete(page, `/api/v1/events/webhooks/${encodeURIComponent(webhookName)}`);
  }
});
