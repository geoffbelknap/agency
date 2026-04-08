import { expect, test, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;

async function settle(page: Page) {
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(1500);
  await expect(page.getByText(APP_ERROR_PATTERN)).toHaveCount(0);
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

test('live stack serves health and renders a top-level UI shell', async ({ page, request }) => {
  const health = await request.get('/health');
  expect(health.ok()).toBeTruthy();

  await page.goto('/');
  await settle(page);

  const bodyText = await page.locator('body').innerText();
  expect(bodyText).toMatch(
    /Preparing your platform|Welcome to Agency|Re-configure Agency|Channels|Agents|Admin/,
  );
});

test('live stack routes to setup or initialized navigation without app errors', async ({ page }) => {
  await page.goto('/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await expect(page.getByRole('link', { name: 'Channels' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Agents' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Missions' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Knowledge' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Profiles' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Admin' })).toBeVisible();

  await page.goto('/admin');
  await expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible();
});

test('live stack top-level routes render without app errors when initialized', async ({ page }) => {
  await page.goto('/');
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
    await page.goto(route.path);
    await settle(page);
    await route.expectVisible();
  }
});

test('live stack supports read-only drill-downs for key initialized views', async ({ page }) => {
  await page.goto('/');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized) {
    return;
  }

  await page.goto('/agents');
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

  await page.goto('/missions');
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
      await page.goto(`/missions/${encodeURIComponent(firstMissionName)}`);
      await settle(page);
      await expect(page.getByRole('button', { name: /Visual Editor|Open in Wizard/ }).first()).toBeVisible();
    }
  }

  await page.goto('/knowledge');
  await settle(page);
  await page.getByRole('button', { name: 'Graph' }).click();
  await settle(page);
  await page.getByRole('button', { name: 'Search' }).click();
  await settle(page);
  await expect(page.getByText(/Query Knowledge|Knowledge graph is empty/)).toBeVisible();

  await page.goto('/admin/usage');
  await settle(page);
  await expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible();

  await page.goto('/admin/events');
  await settle(page);
  await expect(page.getByRole('button', { name: 'Refresh' })).toBeVisible();
});
