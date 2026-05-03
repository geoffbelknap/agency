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

async function expectKnowledgeVisible(page: Page) {
  const heading = page.getByRole('heading', { name: 'Query Knowledge' });
  if (await heading.count()) {
    await expect(heading.first()).toBeVisible();
    return;
  }
  const loading = page.getByText('Loading search', { exact: true });
  if (await loading.count()) {
    await expect(loading.first()).toBeVisible();
    return;
  }
  const surface = page.getByText('Knowledge', { exact: true });
  for (let i = 0; i < await surface.count(); i += 1) {
    if (await surface.nth(i).isVisible()) {
      await expect(surface.nth(i)).toBeVisible();
      return;
    }
  }
  await expect(page.getByText('Knowledge graph is empty')).toBeVisible();
}

async function expectSurfaceVisible(page: Page, name: string) {
  const matches = page.getByText(name, { exact: true });
  const count = await matches.count();
  for (let i = 0; i < count; i += 1) {
    const match = matches.nth(i);
    if (await match.isVisible()) {
      await expect(match).toBeVisible();
      return;
    }
  }
  throw new Error(`Expected visible surface text: ${name}`);
}

async function isAdminTabSelected(page: Page, name: string) {
  const tab = page.getByRole('tab', { name, exact: true });
  if (!(await tab.count())) return false;
  return (await tab.first().getAttribute('aria-selected')) === 'true';
}

test('live admin tabs render across the real stack', async ({ page }) => {
  await page.goto('/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  const tabs = [
    { path: '/admin/infrastructure', tab: 'Infrastructure', assert: async () => expect(page.getByRole('button', { name: 'Reload config' })).toBeVisible() },
    { path: '/admin/hub', tab: 'Packages', optional: true, assert: async () => expect(page.getByText('Installed packages')).toBeVisible() },
    { path: '/admin/intake', tab: 'Intake', optional: true, assert: async () => expect(page.getByRole('tab', { name: 'Connectors' })).toBeVisible() },
    { path: '/admin/knowledge', tab: 'Knowledge', assert: async () => expectKnowledgeVisible(page) },
    { path: '/admin/capabilities', tab: 'Capabilities', assert: async () => expect(page.getByText(/Capabilities|Provider tools|Local capabilities/).first()).toBeVisible() },
    { path: '/admin/presets', tab: 'Presets', assert: async () => expect(page.getByText(/Built-in|Custom|Presets/).first()).toBeVisible() },
    { path: '/admin/trust', tab: 'Trust', optional: true, assert: async () => expect(page.getByText('Trust Level')).toBeVisible() },
    { path: '/admin/egress', tab: 'Egress', assert: async () => expect(page.getByText(/Egress|Allowed domains|denied by default|mediated outside the agent boundary/).first()).toBeVisible() },
    { path: '/admin/policy', tab: 'Policy', assert: async () => expect(page.getByRole('button', { name: 'Validate' })).toBeVisible() },
    { path: '/admin/doctor', tab: 'Doctor', assert: async () => expectAnyVisible(page, ['Run Doctor', 'Running...', 'Running doctor checks...', 'No checks returned']) },
    { path: '/admin/usage', tab: 'Usage', assert: async () => expect(page.getByText(/Usage|Tokens|Cost/).first()).toBeVisible() },
    { path: '/admin/events', tab: 'Events', optional: true, assert: async () => expect(page.getByRole('button', { name: 'Refresh' })).toBeVisible() },
    { path: '/admin/webhooks', tab: 'Webhooks', optional: true, assert: async () => expect(page.getByText('Inbound webhooks for external event delivery')).toBeVisible() },
    { path: '/admin/notifications', tab: 'Notifications', optional: true, assert: async () => expect(page.getByText('Operator notification destinations')).toBeVisible() },
    { path: '/admin/audit', tab: 'Audit', assert: async () => expect(page.getByRole('button', { name: 'Refresh audit' })).toBeVisible() },
    { path: '/admin/setup', tab: 'Setup', assert: async () => expect(page.getByRole('heading', { name: 'Re-run setup wizard' })).toBeVisible() },
    { path: '/admin/danger', tab: 'Danger Zone', assert: async () => expect(page.getByRole('button', { name: 'Destroy All' })).toBeVisible() },
  ];

  for (const tab of tabs) {
    await page.goto(tab.path);
    await settle(page);
    if (!(await isAdminTabSelected(page, tab.tab))) {
      if (tab.optional) continue;
      throw new Error(`Expected admin tab ${tab.tab} to be selected for ${tab.path}`);
    }
    await expectSurfaceVisible(page, 'Admin');
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
    if (!(await isAdminTabSelected(page, 'Intake'))) {
      return;
    }
    await expect(page.getByRole('tab', { name: 'Connectors' })).toBeVisible();
    await expect(page.getByText(/Start by adding a connector|Work is arriving but needs routing|Connectors are ready, now verify delivery|Intake is delivering work/)).toBeVisible();
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

  const workItems = await fetchJson<Array<{ connector?: string; source?: string; summary?: string; id?: string }>>(request, '/api/v1/intake/items');
  const firstWorkItem = Array.isArray(workItems) ? workItems.find((item) => item?.id) : null;
  if (firstWorkItem) {
    const rowLabel = firstWorkItem.summary ?? firstWorkItem.connector ?? firstWorkItem.source ?? firstWorkItem.id ?? '';
    await page.goto('/admin/intake');
    await settle(page);
    if (!(await isAdminTabSelected(page, 'Intake'))) {
      return;
    }
    await page.getByRole('tab', { name: 'Work Items' }).click();
    await expect(page.getByText('Inbound work queue')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Refresh work items' })).toBeVisible();

    const rowButton = page.locator('button').filter({ hasText: rowLabel }).first();
    if (await rowButton.count()) {
      await rowButton.click();
      await expect(page.getByText(/Needs routing|Relayed downstream|Route target assigned|Pending review/)).toBeVisible();
      await expect(page.getByText(/Connector definition missing|Connector is inactive|Route rule likely missing|Target type needs review|Connector-to-route chain looks intact/)).toBeVisible();
      const openTarget = page.getByRole('link', { name: 'Open route target' });
      if (await openTarget.count()) {
        await expect(openTarget.first()).toBeVisible();
      }
      const openConnector = page.getByRole('button', { name: 'Open connector' });
      if (await openConnector.count()) {
        await openConnector.first().click();
        await expect(page.getByRole('tab', { name: 'Connectors' })).toHaveAttribute('aria-selected', 'true');
      }
    }
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
  await expect(page.getByRole('button', { name: /Run Doctor|Running\.\.\./ }).or(page.getByText(/Running doctor checks|No checks returned/)).first()).toBeVisible();

  const platformGroup = page.getByText('(platform)');
  if (await platformGroup.count()) {
    await platformGroup.click();
    const main = page.locator('main');
    await expect(main.getByText(/checks|capabilities|doctor/i).first()).toBeVisible();
  }

  await page.goto('/admin/infrastructure');
  await settle(page);
  await expect(page.getByRole('button', { name: 'Reload config' })).toBeVisible();
  const refreshButton = page.getByRole('button', { name: 'Reload config' });
  await expect(refreshButton).toBeVisible();
  if (await refreshButton.isEnabled()) {
    await refreshButton.click();
  }
  await expect(page.getByRole('button', { name: 'Reload config' })).toBeVisible();
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
  if (!(await isAdminTabSelected(page, 'Events'))) {
    return;
  }
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
  await expect(page.getByText('Usage overview')).toBeVisible();

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
  if (!(await isAdminTabSelected(page, 'Packages'))) {
    return;
  }
  await expect(page.getByText('Installed packages', { exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'Instances' }).click();
  await expect(page.getByText('Local instances')).toBeVisible();
  await expect(page.getByText('Authority nodes')).toBeVisible();
});
