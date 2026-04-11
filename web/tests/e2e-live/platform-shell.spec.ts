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

test('live stack serves health and renders a top-level UI shell', async ({ page, request }) => {
  const health = await request.get('/health');
  expect(health.ok()).toBeTruthy();

  await gotoRoute(page, '/');
  await settle(page);

  const bodyText = await page.locator('body').innerText();
  expect(bodyText).toMatch(
    /Preparing your platform|Welcome to Agency|Re-configure Agency|Channels|Agents|Admin/,
  );
});

test('live stack routes to setup or initialized navigation without app errors', async ({ page }) => {
  await gotoRoute(page, '/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await expect(page.getByRole('link', { name: 'Channels' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Agents' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Missions' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Teams' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Knowledge' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Profiles' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Hub' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Intake' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Admin' })).toBeVisible();

  await gotoRoute(page, '/admin');
  await expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible();
});

test('live stack top-level routes render without app errors when initialized', async ({ page }) => {
  await gotoRoute(page, '/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  const routes = [
    { path: '/overview', expectVisible: async () => expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible() },
    { path: '/channels', expectVisible: async () => {
      const searchToggle = page.getByRole('button', { name: 'Toggle search' });
      if (await searchToggle.count()) {
        await expect(searchToggle).toBeVisible();
        return;
      }
      await expect(page.getByText(/No channels available|Loading\.\.\./)).toBeVisible();
    } },
    { path: '/agents', expectVisible: async () => expect(page.getByRole('heading', { name: 'Agents' })).toBeVisible() },
    { path: '/missions', expectVisible: async () => expect(page.getByRole('heading', { name: 'Missions' })).toBeVisible() },
    { path: '/knowledge', expectVisible: async () => expect(page.getByRole('heading', { name: 'Knowledge' })).toBeVisible() },
    { path: '/profiles', expectVisible: async () => expect(page.getByRole('heading', { name: 'Profiles' })).toBeVisible() },
    { path: '/teams', expectVisible: async () => expect(page.getByRole('heading', { name: 'Teams' })).toBeVisible() },
    { path: '/admin', expectVisible: async () => expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible() },
  ];

  for (const route of routes) {
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

  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  await expect(page.getByText('Suggested next steps')).toBeVisible();

  if (await page.getByRole('button', { name: 'Start infrastructure' }).count()) {
    await expect(page.getByText(/start infrastructure first/i)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Start infrastructure' })).toBeVisible();
    return;
  }

  if (await page.getByRole('link', { name: 'Create an agent' }).count()) {
    await expect(page.getByText(/create your first agent/i)).toBeVisible();
    await expect(page.getByRole('link', { name: 'Create an agent' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Open setup wizard' })).toBeVisible();
    return;
  }

  await expect(page.getByText(/move into direct messages, missions, or intake/i)).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open channels' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open missions' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open intake' })).toBeVisible();
});

test('live stack supports read-only drill-downs for key initialized views', async ({ page }) => {
  await gotoRoute(page, '/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await gotoRoute(page, '/agents');
  await settle(page);
  if (await page.getByText('No agents. Create one to get started.').count()) {
    await expect(page.getByText('No agents. Create one to get started.')).toBeVisible();
  } else {
    await page.locator('tr[role="button"]').first().click();
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
  if (await page.getByRole('button', { name: 'Graph' }).count()) {
    await page.getByRole('button', { name: 'Graph' }).click();
    await settle(page);
  }
  if (await page.getByRole('button', { name: 'Search' }).count()) {
    await page.getByRole('button', { name: 'Search' }).click();
    await settle(page);
  }
  await expect(page.getByText(/Query Knowledge|Knowledge graph is empty/)).toBeVisible();

  await gotoRoute(page, '/admin/usage');
  await settle(page);
  await expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible();

  await gotoRoute(page, '/admin/events');
  await settle(page);
  await expect(page.getByRole('button', { name: 'Refresh' })).toBeVisible();
});

test('live stack supports interactive navigation without mutating state', async ({ page, request }) => {
  await gotoRoute(page, '/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await page.getByRole('link', { name: 'Agents' }).click();
  await settle(page);
  await expect(page.getByRole('heading', { name: 'Agents' })).toBeVisible();

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
    await expect(page.getByRole('heading', { name: 'Agents' })).toBeVisible();
  }

  await page.getByRole('link', { name: 'Knowledge' }).click();
  await settle(page);
  await expect(page.getByRole('heading', { name: 'Knowledge' })).toBeVisible();
  await page.getByRole('button', { name: 'Graph' }).click();
  await settle(page);
  if (await page.getByRole('button', { name: 'Radial (clusters)' }).count()) {
    await expect(page.getByRole('button', { name: 'Radial (clusters)' })).toBeVisible();
  } else {
    await expect(page.getByText('Knowledge graph is empty')).toBeVisible();
  }

  await page.getByRole('button', { name: 'Search' }).click();
  await settle(page);
  await expect(page.getByText(/Query Knowledge|Knowledge graph is empty/)).toBeVisible();

  await page.goBack();
  await settle(page);
  await expect(page.getByRole('heading', { name: 'Agents' })).toBeVisible();
});
