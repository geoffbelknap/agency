import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;

async function settle(page: Page) {
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(1500);
  await expect(page.getByText(APP_ERROR_PATTERN)).toHaveCount(0);
}

async function gotoRoute(page: Page, path: string) {
  await page.goto(path, { waitUntil: 'domcontentloaded' });
}

async function isSetupFlow(page: Page) {
  return (await page.getByRole('heading', { name: SETUP_HEADING_PATTERN }).count()) > 0;
}

async function expectSetupOrInitialized(page: Page) {
  await settle(page);
  if (await isSetupFlow(page)) {
    await expect(page.getByRole('heading', { name: SETUP_HEADING_PATTERN }).first()).toBeVisible();
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

async function expectKnowledgeVisible(page: Page) {
  const main = page.locator('main');

  const searchHeading = page.getByRole('heading', { name: 'Query Knowledge' });
  if (await searchHeading.count()) {
    await expect(searchHeading.first()).toBeVisible();
    return;
  }
  const loading = page.getByText('Loading search', { exact: true });
  if (await loading.count()) {
    await expect(loading.first()).toBeVisible();
    return;
  }
  if (await main.getByText('Knowledge graph is empty').count()) {
    await expect(main.getByText('Knowledge graph is empty')).toBeVisible();
    return;
  }
  const knowledgeMetrics = page.getByLabel('Knowledge metrics');
  if (await knowledgeMetrics.count()) {
    await expect(knowledgeMetrics.first()).toBeVisible();
    return;
  }
  await expect(main.getByText(/Knowledge summary|Browser|Search|Graph|Structural Review|Durable Memory|node|relationship/i).first()).toBeVisible();
}

function escaped(text: string) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

async function navLinkExists(page: Page, name: string) {
  return (await page.getByRole('link', { name: new RegExp(`^${escaped(name)}\\b`) }).count()) > 0;
}

function navLink(page: Page, name: string) {
  return page.getByRole('link', { name: new RegExp(`^${escaped(name)}\\b`) }).first();
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

async function expectOverviewVisible(page: Page) {
  const main = page.locator('main');
  await expect(main.getByText(/Running agents|Decision inbox|Suggested next steps|Operator guidance/).first()).toBeVisible();
}

async function isAdminTabSelected(page: Page, name: string) {
  const tab = page.getByRole('tab', { name, exact: true });
  if (!(await tab.count())) return false;
  return (await tab.first().getAttribute('aria-selected')) === 'true';
}

test('live stack serves health and renders a top-level UI shell', async ({ page, request }) => {
  const health = await request.get('/health');
  expect(health.ok()).toBeTruthy();

  await gotoRoute(page, '/');
  await settle(page);

  if (await isSetupFlow(page)) {
    await expect(page.getByRole('heading', { name: SETUP_HEADING_PATTERN }).first()).toBeVisible();
    return;
  }

  await expect.poll(async () => {
    const labels = ['Channels', 'Agents', 'Admin'];
    for (const label of labels) {
      if (await navLinkExists(page, label)) {
        return label;
      }
    }
    return '';
  }).not.toBe('');
});

test('live stack routes to setup or initialized navigation without app errors', async ({ page }) => {
  await gotoRoute(page, '/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await expect(navLink(page, 'Channels')).toBeVisible();
  await expect(navLink(page, 'Agents')).toBeVisible();
  await expect(navLink(page, 'Knowledge')).toBeVisible();
  await expect(navLink(page, 'Admin')).toBeVisible();

  for (const label of ['Missions', 'Teams', 'Profiles', 'Hub', 'Intake']) {
    const link = page.getByRole('link', { name: label, exact: true });
    if (await link.count()) {
      await expect(link).toBeVisible();
    }
  }

  await gotoRoute(page, '/admin');
  await expect(page).toHaveURL(/\/admin/);
});

test('live stack top-level routes render without app errors when initialized', async ({ page }) => {
  await gotoRoute(page, '/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  const routes = [
    { path: '/overview', expectVisible: async () => expectOverviewVisible(page) },
    { path: '/channels', expectVisible: async () => {
      const searchToggle = page.getByRole('button', { name: 'Toggle search' });
      if (await searchToggle.count()) {
        await expect(searchToggle).toBeVisible();
        return;
      }
      await expectSurfaceVisible(page, 'Channels');
    } },
    { path: '/agents', expectVisible: async () => expectSurfaceVisible(page, 'Agents') },
    { path: '/missions', optionalLink: 'Missions', expectVisible: async () => {
      const heading = page.getByRole('heading', { name: 'Missions' });
      if (await heading.count()) {
        await expect(heading).toBeVisible();
        return;
      }
      await expect(page.getByRole('button', { name: /Create Mission|New Mission/ }).first()).toBeVisible();
    } },
    { path: '/knowledge', expectVisible: async () => expectSurfaceVisible(page, 'Knowledge') },
    { path: '/profiles', optionalLink: 'Profiles', expectVisible: async () => expectSurfaceVisible(page, 'Profiles') },
    { path: '/teams', optionalLink: 'Teams', expectVisible: async () => expectSurfaceVisible(page, 'Teams') },
    { path: '/admin', expectVisible: async () => expectSurfaceVisible(page, 'Admin') },
  ];

  for (const route of routes) {
    if (route.optionalLink && !(await navLinkExists(page, route.optionalLink))) {
      continue;
    }
    await gotoRoute(page, route.path);
    await settle(page);
    await route.expectVisible();
  }
});

test('live overview surfaces the right next-step guidance for the current stack state', async ({ page, request }) => {
  await gotoRoute(page, '/overview');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await expectOverviewVisible(page);
  await expect(page.getByText('Suggested next steps')).toBeVisible();

  if (await page.getByRole('button', { name: 'Start infrastructure' }).count()) {
    await expect(page.getByText(/start infrastructure first/i)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Start infrastructure' })).toBeVisible();
    return;
  }

  if (await page.getByRole('link', { name: 'Create first agent' }).count()) {
    await expect(page.locator('main').getByText(/create your first agent/i).first()).toBeVisible();
    await expect(page.getByRole('link', { name: 'Create first agent' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Review providers' })).toBeVisible();
    return;
  }

  await expect(page.getByText('Suggested next steps')).toBeVisible();
  if (await page.getByRole('link', { name: 'Open channels' }).count()) {
    await expect(page.getByRole('link', { name: 'Open channels' }).first()).toBeVisible();
  }
  if (await page.getByRole('link', { name: 'Open knowledge' }).count()) {
    await expect(page.getByRole('link', { name: 'Open knowledge' }).first()).toBeVisible();
  }
});

test('live stack supports read-only drill-downs for key initialized views', async ({ page }) => {
  await gotoRoute(page, '/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await gotoRoute(page, '/agents');
  await settle(page);
  const firstAgent = await page.evaluate(async () => {
    const response = await fetch('/api/v1/agents');
    if (!response.ok) return null;
    const agents = await response.json();
    return Array.isArray(agents) && agents.length > 0 ? agents[0]?.name ?? null : null;
  });
  if (!firstAgent && await page.getByText('No agents. Create one to get started.').count()) {
    await expect(page.getByText('No agents. Create one to get started.')).toBeVisible();
  } else if (firstAgent) {
    await gotoRoute(page, `/agents/${encodeURIComponent(firstAgent)}`);
    await settle(page);
    await expect(page.getByRole('tab', { name: 'Overview' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Activity' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Operations' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'System' })).toBeVisible();
  }

  await gotoRoute(page, '/missions');
  await settle(page);
  if (await page.getByText('No missions yet. Create one to get started.').count()) {
    await expect(page.getByText('No missions yet. Create one to get started.')).toBeVisible();
  } else {
    const firstMissionName = await page.evaluate(async () => {
      const response = await fetch('/api/v1/missions');
      if (!response.ok) return null;
      const missions = await response.json();
      return Array.isArray(missions) && missions.length > 0 ? missions[0]?.name ?? null : null;
    });
    if (firstMissionName) {
      await gotoRoute(page, `/missions/${encodeURIComponent(firstMissionName)}`);
      await settle(page);
      await expect(page.getByRole('button', { name: /Visual Editor|Open in Wizard/ }).first()).toBeVisible();
    }
  }

  await gotoRoute(page, '/knowledge');
  await settle(page);
  await expectSurfaceVisible(page, 'Knowledge');
  if (await page.getByRole('button', { name: 'Graph' }).count()) {
    await page.getByRole('button', { name: 'Graph' }).click();
    await settle(page);
  }
  const searchTab = page.getByRole('button', { name: 'Search', exact: true });
  if (await searchTab.count()) {
    await searchTab.click();
    await settle(page);
  }
  await expectKnowledgeVisible(page);

  await gotoRoute(page, '/admin/usage');
  await settle(page);
  await expectSurfaceVisible(page, 'Admin');

  await gotoRoute(page, '/admin/events');
  await settle(page);
  if (!(await isAdminTabSelected(page, 'Events'))) {
    return;
  }
  await expect(page.getByRole('button', { name: 'Refresh' })).toBeVisible();
});

test('live stack supports interactive navigation without mutating state', async ({ page, request }) => {
  await gotoRoute(page, '/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await navLink(page, 'Agents').click();
  await settle(page);
  await expectSurfaceVisible(page, 'Agents');

  const agents = await fetchJson<Array<{ name?: string }>>(request, '/api/v1/agents');
  const firstAgent = Array.isArray(agents) ? agents.find((agent) => agent?.name)?.name : null;

  if (firstAgent && await page.locator('tr[role="button"]').count()) {
    await page.locator('tr[role="button"]').first().click();
    await settle(page);

    await page.getByRole('tab', { name: 'Activity' }).click();
    await expect(page.getByRole('tab', { name: 'Activity' })).toHaveAttribute('aria-selected', 'true');

    await page.getByRole('tab', { name: 'Operations' }).click();
    await expect(page.getByRole('tab', { name: 'Operations' })).toHaveAttribute('aria-selected', 'true');
    await page.getByRole('tab', { name: 'Knowledge' }).click();
    await expect(page.getByRole('tab', { name: 'Knowledge' })).toHaveAttribute('aria-selected', 'true');

    await page.getByRole('tab', { name: 'System' }).click();
    await expect(page.getByRole('tab', { name: 'System' })).toHaveAttribute('aria-selected', 'true');
    await page.getByRole('tab', { name: 'Logs' }).click();
    await expect(page.getByRole('tab', { name: 'Logs' })).toHaveAttribute('aria-selected', 'true');

    await page.goBack();
    await settle(page);
    await expectSurfaceVisible(page, 'Agents');
  }

  await navLink(page, 'Knowledge').click();
  await settle(page);
  await expectSurfaceVisible(page, 'Knowledge');
  const graphButton = page.getByRole('button', { name: 'Graph' });
  if (await graphButton.count()) {
    await graphButton.click();
    await settle(page);
    if (await page.getByRole('button', { name: 'Radial (clusters)' }).count()) {
      await expect(page.getByRole('button', { name: 'Radial (clusters)' })).toBeVisible();
    } else {
      await expect(page.getByText('Knowledge graph is empty')).toBeVisible();
    }
  } else {
    await expectKnowledgeVisible(page);
  }

  const searchButton = page.getByRole('button', { name: 'Search', exact: true });
  if (await searchButton.count()) {
    await searchButton.click();
    await settle(page);
  }
  await expectKnowledgeVisible(page);

  await page.goBack();
  await settle(page);
  await expectSurfaceVisible(page, 'Agents');
});
