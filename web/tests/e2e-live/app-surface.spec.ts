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
    { path: '/admin/hub', assert: async () => expect(page.getByRole('tab', { name: 'Packages' })).toBeVisible() },
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

  const connectors = await fetchJson<Array<{ name?: string; id?: string }>>(request, '/api/v1/hub/instances?kind=connector');
  const firstConnector = Array.isArray(connectors) ? connectors.find((connector) => connector?.name)?.name : null;
  if (firstConnector) {
    await page.goto('/admin/intake');
    await settle(page);
    await expect(page.getByRole('tab', { name: 'Connectors' })).toBeVisible();
    await expect(page.getByText('Healthy Polling')).toBeVisible();
    await expect(page.getByText('Needs Review')).toBeVisible();

    await page.getByRole('button', { name: new RegExp(firstConnector) }).first().click();
    await expect(page.getByRole('button', { name: 'Setup' })).toBeVisible();
    await expect(page.getByText(/Ready to ingest|Inactive connector|No poll health yet|Needs connector review/)).toBeVisible();
    await page.getByRole('button', { name: 'Setup' }).click();

    await expect(page.getByText(`Setup: ${firstConnector}`)).toBeVisible();
    await expect(page.getByText(/Setup saves required credentials, applies connector egress rules, and activates the connector when it is ready\./)).toBeVisible();
    await expect(page.getByText(/Ready|Not configured|No requirements data/)).toBeVisible();
  }
});

test('live stack supports non-destructive operator diagnostics and recovery actions', async ({ page }) => {
  await page.goto('/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await page.goto('/admin/doctor');
  await settle(page);
  const runDoctorButton = page.getByRole('button', { name: /Run Doctor|Running\.\.\./ });
  await expect(runDoctorButton).toBeVisible();
  if (await page.getByRole('button', { name: 'Run Doctor' }).count()) {
    await page.getByRole('button', { name: 'Run Doctor' }).click();
  }
  await expectAnyVisible(page, ['Run Doctor', 'Running...', 'Running doctor checks...', 'No checks returned']);

  const platformGroup = page.getByText('(platform)');
  if (await platformGroup.count()) {
    await platformGroup.click();
    await expect(page.getByText(/checks/i)).toBeVisible();
  }

  await page.goto('/admin/infrastructure');
  await settle(page);
  await expect(page.getByRole('heading', { name: 'Infrastructure' })).toBeVisible();
  const refreshButton = page.getByRole('button', { name: /Refresh infrastructure|Refreshing infrastructure/ });
  await expect(refreshButton).toBeVisible();
  if (await page.getByRole('button', { name: 'Refresh infrastructure' }).count()) {
    await page.getByRole('button', { name: 'Refresh infrastructure' }).click();
  }
  await expect(page.getByRole('button', { name: /Refresh infrastructure|Refreshing infrastructure/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /Start All|Restart All/ })).toBeVisible();
});

test('live recovery surfaces expose likely next steps when recent failures exist', async ({ page }) => {
  await page.goto('/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await page.goto('/admin/events');
  await settle(page);
  await expect(page.getByRole('button', { name: 'Refresh' })).toBeVisible();

  if (await page.getByText(/recent event.*need attention/i).count()) {
    await expect(page.getByText(/recent event.*need attention/i)).toBeVisible();
    const firstAttentionRow = page.locator('text=/error|warning/i').first();
    if (await firstAttentionRow.count()) {
      await firstAttentionRow.click();
      await expect(page.getByText('Likely next step')).toBeVisible();
      await expect(page.getByRole('link', { name: /Open (Intake|Infrastructure|Webhooks|Channel)/ }).first()).toBeVisible();
      await expect(page.getByRole('link', { name: 'Open Doctor' }).first()).toBeVisible();
    }
  }

  await page.goto('/admin/usage');
  await settle(page);
  await expect(page.getByText('LLM usage and estimated spend')).toBeVisible();

  if (await page.getByText(/recent routing error.*need attention/i).count()) {
    await expect(page.getByText(/recent routing error.*need attention/i)).toBeVisible();
    await expect(page.getByText('Likely next step').first()).toBeVisible();
    await expect(page.getByRole('link', { name: /Open Agent:/ }).first()).toBeVisible();
    await expect(page.getByRole('link', { name: 'Open Doctor' }).first()).toBeVisible();
  }
});

test('live hub surfaces source trust and provenance guidance without mutating state', async ({ page }) => {
  await page.goto('/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await page.goto('/admin/hub');
  await settle(page);
  await expect(page.getByText('Packages and instances')).toBeVisible();
  await expect(page.getByText(/Installed packages are reusable local building blocks/i)).toBeVisible();
  await expect(page.getByText('Installed packages')).toBeVisible();
  await page.getByRole('tab', { name: 'Instances' }).click();
  await expect(page.getByText('Local instances')).toBeVisible();
  await expect(page.getByText('Authority nodes')).toBeVisible();
});
