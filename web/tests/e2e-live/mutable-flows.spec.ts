import { expect, test, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;

test.describe.configure({ timeout: 60_000 });

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
  const configResponse = await page.request.get('/__agency/config');
  const config = configResponse.ok() ? await configResponse.json() : {};
  const token = (config as { token?: string })?.token ?? '';
  const response = await page.request.delete(path, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  const status = response.status();
  if (status === 200 || status === 204 || status === 404) {
    return;
  }
  throw new Error(`cleanup failed for ${path}: ${status}`);
}

async function clearBlockingToasts(page: Page) {
  const closeButtons = page.getByRole('button', { name: 'Close toast' });
  const count = await closeButtons.count();
  for (let i = 0; i < count; i += 1) {
    await closeButtons.nth(i).click({ force: true }).catch(() => {});
  }
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

test('live stack supports notification destination add and remove flow', async ({ page }) => {
  const destinationName = uniqueName('playwright-notify');
  const destinationUrl = `https://ntfy.sh/${destinationName}`;

  try {
    await page.goto('/admin/notifications');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await bestEffortDelete(page, `/api/v1/events/notifications/${encodeURIComponent(destinationName)}`);

    await page.getByRole('button', { name: 'Add destination' }).click();
    await page.getByPlaceholder('name').fill(destinationName);
    await page.getByPlaceholder(/url \(e\.g\./).fill(destinationUrl);
    await page.getByRole('button', { name: /^Add$/ }).click();
    await settle(page);

    const notificationRow = page.locator('tr').filter({ has: page.getByText(destinationName, { exact: true }) }).first();
    await expect(notificationRow).toBeVisible();
    await expect(notificationRow).toContainText('ntfy');
    await expect(notificationRow).toContainText(destinationUrl);

    await clearBlockingToasts(page);
    await notificationRow.getByRole('button', { name: 'Remove destination' }).click();
    await settle(page);
    await page.getByRole('button', { name: 'Refresh' }).click();
    await settle(page);
    await expect(notificationRow).toHaveCount(0);
  } finally {
    await bestEffortDelete(page, `/api/v1/events/notifications/${encodeURIComponent(destinationName)}`);
  }
});

test('live stack supports custom preset create, edit, and delete flow', async ({ page }) => {
  const presetName = uniqueName('playwright-preset');
  const updatedDescription = 'Updated by live Playwright coverage';
  const updatedPurpose = 'Validate live preset editing';
  const updatedBody = 'You are a temporary live test preset.';

  try {
    await page.goto('/admin/presets');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await bestEffortDelete(page, `/api/v1/hub/presets/${encodeURIComponent(presetName)}`);

    await page.getByRole('button', { name: 'New Preset' }).click();
    await page.locator('input').nth(0).fill(presetName);
    await page.locator('input').nth(1).fill('Live preset created by Playwright');
    await page.locator('input').nth(2).fill('bash, git');
    await page.getByPlaceholder('One-line purpose statement').fill('Temporary test preset');
    await page.getByPlaceholder('Agent personality prompt...').fill('You are a temporary live test preset.');
    await page.getByRole('button', { name: /^Save$/ }).click();
    await settle(page);

    await expect(page.getByRole('heading', { name: presetName })).toBeVisible();
    await expect(page.getByText(/Live preset created by Playwright · standard · user/)).toBeVisible();

    await page.getByRole('button', { name: 'Edit' }).click();
    await page.locator('input').nth(1).fill(updatedDescription);
    await page.getByPlaceholder('One-line purpose statement').fill(updatedPurpose);
    await page.getByPlaceholder('Agent personality prompt...').fill(updatedBody);
    await page.getByRole('button', { name: /^Save$/ }).click();
    await settle(page);

    await expect(page.getByText(new RegExp(`${updatedDescription} · standard · user`))).toBeVisible();
    await expect(page.getByText(updatedPurpose)).toBeVisible();
    await expect(page.getByText(updatedBody)).toBeVisible();

    await clearBlockingToasts(page);
    await page.locator('button:has(svg.lucide-trash2)').first().click({ force: true });
    await page.getByRole('button', { name: 'Delete' }).click();
    await settle(page);

    await expect(page.getByRole('button', { name: presetName })).toHaveCount(0);
    await expect(page.getByRole('heading', { name: presetName })).toHaveCount(0);
  } finally {
    await bestEffortDelete(page, `/api/v1/hub/presets/${encodeURIComponent(presetName)}`);
  }
});
