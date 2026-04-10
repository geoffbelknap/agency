import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;

test.describe.configure({ timeout: 120_000 });

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

async function fetchJson<T>(request: APIRequestContext, path: string): Promise<T | null> {
  const response = await request.get(path);
  if (!response.ok()) return null;
  return response.json();
}

async function expectAnyVisible(page: Page, candidates: string[]) {
  for (const text of candidates) {
    const locator = page.getByText(text, { exact: true });
    if (await locator.count()) {
      await expect(locator.first()).toBeVisible();
      return;
    }
  }
  throw new Error(`None of the expected texts were visible: ${candidates.join(', ')}`);
}

test('live admin tabs render across the real stack', async ({ page }) => {
  await page.goto('/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  const tabs = [
    { path: '/admin/infrastructure', assert: async () => expect(page.getByRole('heading', { name: 'Infrastructure' })).toBeVisible() },
    { path: '/admin/hub', assert: async () => expect(page.getByRole('tab', { name: 'Browse' })).toBeVisible() },
    { path: '/admin/intake', assert: async () => expect(page.getByRole('tab', { name: 'Connectors' })).toBeVisible() },
    { path: '/admin/knowledge', assert: async () => expect(page.getByText(/Query Knowledge|Knowledge graph is empty/)).toBeVisible() },
    { path: '/admin/capabilities', assert: async () => expect(page.getByText('Platform capability registry')).toBeVisible() },
    { path: '/admin/presets', assert: async () => expect(page.getByText('Agent preset templates')).toBeVisible() },
    { path: '/admin/trust', assert: async () => expect(page.getByText('Trust Level')).toBeVisible() },
    { path: '/admin/egress', assert: async () => expect(page.getByText('Domain Provenance')).toBeVisible() },
    { path: '/admin/policy', assert: async () => expect(page.getByRole('button', { name: 'Validate' })).toBeVisible() },
    { path: '/admin/doctor', assert: async () => expectAnyVisible(page, ['Run Doctor', 'Running...', 'Running doctor checks...', 'No checks returned']) },
    { path: '/admin/usage', assert: async () => expect(page.getByText('LLM usage and estimated spend')).toBeVisible() },
    { path: '/admin/events', assert: async () => expect(page.getByRole('button', { name: 'Refresh' })).toBeVisible() },
    { path: '/admin/webhooks', assert: async () => expect(page.getByText('Inbound webhooks for external event delivery')).toBeVisible() },
    { path: '/admin/notifications', assert: async () => expect(page.getByText('Operator notification destinations')).toBeVisible() },
    { path: '/admin/audit', assert: async () => expect(page.getByRole('button', { name: 'Summarize' })).toBeVisible() },
    { path: '/admin/setup', assert: async () => expect(page.getByText('Re-run Setup Wizard')).toBeVisible() },
    { path: '/admin/danger', assert: async () => expect(page.getByRole('button', { name: 'Destroy All' })).toBeVisible() },
  ];

  for (const tab of tabs) {
    await page.goto(tab.path);
    await settle(page);
    await expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible();
    await tab.assert();
  }
});

test('live stack supports read-only direct routes for available entities', async ({ page, request }) => {
  await page.goto('/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  const agents = await fetchJson<Array<{ name?: string }>>(request, '/api/v1/agents');
  const firstAgent = Array.isArray(agents) ? agents.find((agent) => agent?.name)?.name : null;
  if (firstAgent) {
    await page.goto(`/agents/${encodeURIComponent(firstAgent)}`);
    await settle(page);
    await expect(page.getByRole('tab', { name: 'Overview' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'System' })).toBeVisible();
  }

  const profiles = await fetchJson<Array<{ id?: string }>>(request, '/api/v1/admin/profiles');
  const firstProfile = Array.isArray(profiles) ? profiles.find((profile) => profile?.id)?.id : null;
  if (firstProfile) {
    await page.goto(`/profiles/${encodeURIComponent(firstProfile)}`);
    await settle(page);
    await expect(page.getByRole('heading', { name: 'Profiles' })).toBeVisible();
    await expect(page.getByRole('button', { name: /Refresh profiles|Refreshing profiles/ })).toBeVisible();
  }

  const missions = await fetchJson<Array<{ name?: string }>>(request, '/api/v1/missions');
  const firstMission = Array.isArray(missions) ? missions.find((mission) => mission?.name)?.name : null;
  if (firstMission) {
    await page.goto(`/missions/${encodeURIComponent(firstMission)}`);
    await settle(page);
    await expect(page.getByRole('button', { name: /Visual Editor|Open in Wizard/ }).first()).toBeVisible();
  }

  const channels = await fetchJson<Array<{ name?: string }>>(request, '/api/v1/comms/channels');
  const firstChannel = Array.isArray(channels)
    ? channels.find((channel) => channel?.name && !channel.name.startsWith('_'))?.name ?? channels.find((channel) => channel?.name)?.name
    : null;
  if (firstChannel) {
    await page.goto(`/channels/${encodeURIComponent(firstChannel)}`);
    await settle(page);
    const searchToggle = page.getByRole('button', { name: 'Toggle search' });
    if (await searchToggle.count()) {
      await expect(searchToggle).toBeVisible();
    } else {
      await expect(page.getByText(/Loading\.\.\.|No channels available/)).toBeVisible();
    }
  }
});
