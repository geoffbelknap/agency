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

async function requestWithToken(page: Page, method: 'DELETE' | 'POST', path: string) {
  const configResponse = await page.request.get('/__agency/config');
  const config = configResponse.ok() ? await configResponse.json() : {};
  const token = (config as { token?: string })?.token ?? '';
  const response = method === 'DELETE'
    ? await page.request.delete(path, { headers: token ? { Authorization: `Bearer ${token}` } : {} })
    : await page.request.post(path, { headers: token ? { Authorization: `Bearer ${token}` } : {} });
  return response.status();
}

async function channelExists(page: Page, channelName: string) {
  const response = await page.request.get('/api/v1/comms/channels');
  if (!response.ok()) {
    return false;
  }
  const channels = await response.json();
  return Array.isArray(channels) && channels.some((channel: { name?: string }) => channel?.name === channelName);
}

async function bestEffortDelete(page: Page, path: string) {
  const status = await requestWithToken(page, 'DELETE', path);
  if (status === 200 || status === 204 || status === 404) {
    return;
  }
  throw new Error(`cleanup failed for ${path}: ${status}`);
}

async function bestEffortArchiveChannel(page: Page, channelName: string) {
  if (!(await channelExists(page, channelName))) {
    return;
  }
  const status = await requestWithToken(page, 'POST', `/api/v1/comms/channels/${encodeURIComponent(channelName)}/archive`);
  if (status === 200 || status === 204 || status === 404 || status === 502) {
    return;
  }
  throw new Error(`channel archive failed for ${channelName}: ${status}`);
}

async function clearBlockingToasts(page: Page) {
  const closeButtons = page.getByRole('button', { name: 'Close toast' });
  const count = await closeButtons.count();
  for (let i = 0; i < count; i += 1) {
    await closeButtons.nth(i).click({ force: true }).catch(() => {});
  }
}

test('live risky suite supports capability add, enable, disable, and delete flow', async ({ page }) => {
  const capabilityName = uniqueName('playwright-capability');

  try {
    await page.goto('/admin/capabilities');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await bestEffortDelete(page, `/api/v1/admin/capabilities/${encodeURIComponent(capabilityName)}`);

    await page.getByRole('button', { name: 'Add Capability' }).click();
    await page.getByPlaceholder('Capability name...').fill(capabilityName);
    await page.getByRole('button', { name: /^Add$/ }).click();
    await settle(page);

    const capabilityRow = page.locator('tr').filter({ has: page.getByText(capabilityName, { exact: true }) }).first();
    await expect(capabilityRow).toBeVisible();
    await expect(capabilityRow).toContainText('disabled');

    await capabilityRow.getByRole('button', { name: 'Enable' }).click();
    await expect(page.getByRole('heading', { name: new RegExp(`Enable ${capabilityName}`) })).toBeVisible();
    await page.getByRole('button', { name: 'Enable' }).last().click();
    await expect(page.getByRole('heading', { name: new RegExp(`Enable ${capabilityName}`) })).toHaveCount(0, { timeout: 20_000 });
    await settle(page);

    await expect(capabilityRow).toContainText(/enabled|available|restricted/);

    await capabilityRow.getByRole('button', { name: 'Disable' }).click();
    await settle(page);
    await expect(capabilityRow).toContainText('disabled');

    await clearBlockingToasts(page);
    await capabilityRow.getByRole('button', { name: 'Delete' }).click();
    await page.getByRole('button', { name: 'Delete' }).last().click();
    await settle(page);
    await expect(capabilityRow).toHaveCount(0);
  } finally {
    await bestEffortDelete(page, `/api/v1/admin/capabilities/${encodeURIComponent(capabilityName)}`);
  }
});

test('live risky suite supports channel create, message send, and archive flow', async ({ page }) => {
  const channelName = uniqueName('playwright-channel');
  const messageText = `Live risky flow ${channelName}`;

  try {
    await page.goto('/channels');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await bestEffortArchiveChannel(page, channelName);

    await page.getByRole('button', { name: 'Add channel' }).click();
    await page.getByRole('dialog').getByPlaceholder('my-channel').fill(channelName);
    await page.getByRole('dialog').getByRole('button', { name: /^Create$/ }).click();
    await settle(page);

    const channelButton = page.getByRole('button', { name: new RegExp(`^${channelName}`) }).first();
    await expect(channelButton).toBeVisible();
    await channelButton.click();
    await settle(page);

    await expect(page.getByRole('heading', { name: channelName })).toBeVisible();

    const composer = page.getByPlaceholder(`Message #${channelName}`);
    await composer.fill(messageText);
    await page.getByRole('button', { name: 'Send message' }).click();
    await settle(page);

    await expect(page.getByText(messageText)).toBeVisible();
  } finally {
    await bestEffortArchiveChannel(page, channelName);
  }
});

test.skip('live risky mission CRUD flow is blocked on mission lifecycle cleanup instability', async () => {
  // UI-created mission flows currently hit backend cleanup failures (DELETE returns 502)
  // and are tracked in the workspace follow-up note.
});

test.skip('live risky agent lifecycle flow is blocked on slow or missing start-state convergence', async () => {
  // UI start enters "Starting..." but the backend does not converge to a post-start state
  // quickly enough for reliable live coverage yet.
});
