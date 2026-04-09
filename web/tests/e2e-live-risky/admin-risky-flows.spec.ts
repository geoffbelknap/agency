import { expect, test, type Page } from '@playwright/test';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';

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
  const headers = await authHeaders(page);
  const response = method === 'DELETE'
    ? await page.request.delete(path, { headers })
    : await page.request.post(path, { headers });
  return response.status();
}

async function getJSONWithToken<T>(page: Page, path: string): Promise<T | null> {
  const response = await page.request.get(path, { headers: await authHeaders(page) });
  if (!response.ok()) {
    return null;
  }
  return response.json() as Promise<T>;
}

async function postJSONWithToken<T>(page: Page, path: string, body: unknown): Promise<T | null> {
  const response = await page.request.post(path, {
    headers: {
      ...(await authHeaders(page)),
      'Content-Type': 'application/json',
    },
    data: body,
  });
  if (!response.ok()) {
    return null;
  }
  return response.json() as Promise<T>;
}

async function authHeaders(page: Page): Promise<Record<string, string>> {
  const configResponse = await page.request.get('/__agency/config');
  const config = configResponse.ok() ? await configResponse.json() : {};
  const token = (config as { token?: string })?.token ?? '';
  return token ? { Authorization: `Bearer ${token}` } : {};
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

async function bestEffortDeleteMission(page: Page, missionName: string) {
  const status = await requestWithToken(page, 'DELETE', `/api/v1/missions/${encodeURIComponent(missionName)}`);
  if (status === 200 || status === 204 || status === 404) {
    return;
  }
  throw new Error(`mission delete failed for ${missionName}: ${status}`);
}

async function bestEffortDeleteAgent(page: Page, agentName: string) {
  const status = await requestWithToken(page, 'DELETE', `/api/v1/agents/${encodeURIComponent(agentName)}`);
  if (status === 200 || status === 204 || status === 404) {
    return;
  }
  throw new Error(`agent delete failed for ${agentName}: ${status}`);
}

async function clearBlockingToasts(page: Page) {
  const closeButtons = page.getByRole('button', { name: 'Close toast' });
  const count = await closeButtons.count();
  for (let i = 0; i < count; i += 1) {
    await closeButtons.nth(i).click({ force: true }).catch(() => {});
  }
}

type HubComponent = {
  name: string;
  kind: string;
  source?: string;
};

async function ensureInstalledHubComponent(page: Page, kind: 'connector' | 'pack') {
  const [searchResults, installedResults] = await Promise.all([
    getJSONWithToken<HubComponent[]>(page, '/api/v1/hub/search?q='),
    getJSONWithToken<Array<{ name?: string }>>(page, `/api/v1/hub/instances?kind=${kind}`),
  ]);

  const installedNames = new Set((installedResults ?? []).map((item) => item.name).filter(Boolean) as string[]);
  const candidates = (searchResults ?? []).filter((component) => component.kind === kind);
  const existing = candidates.find((component) => installedNames.has(component.name));
  if (existing) {
    return { component: existing, installedByTest: false };
  }

  const target = candidates.find((component) => !!component.name) ?? null;
  if (!target) {
    return { component: null, installedByTest: false };
  }

  const installed = await postJSONWithToken<{ name?: string }>(page, '/api/v1/hub/install', {
    component: target.name,
    kind: target.kind,
    source: target.source ?? '',
  });
  if (!installed?.name) {
    return { component: null, installedByTest: false };
  }

  return { component: target, installedByTest: true };
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

test('live risky suite supports notification test-send to a contained local sink', async ({ page }) => {
  const destinationName = uniqueName('playwright-notify-send');
  const receivedBodies: string[] = [];
  const server = createServer((req, res) => {
    let body = '';
    req.on('data', (chunk) => {
      body += String(chunk);
    });
    req.on('end', () => {
      receivedBodies.push(body);
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end('{"status":"ok"}');
    });
  });

  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address() as AddressInfo;
  const destinationUrl = `http://127.0.0.1:${port}/alerts`;

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

    await notificationRow.getByRole('button', { name: 'Send test notification' }).click();
    await expect.poll(() => receivedBodies.length, { timeout: 15_000 }).toBeGreaterThan(0);
    await expect.poll(() => receivedBodies[0] ?? '', { timeout: 5_000 }).toContain('operator_alert');
    await expect.poll(() => receivedBodies[0] ?? '', { timeout: 5_000 }).toContain('Test notification from agency');
  } finally {
    await bestEffortDelete(page, `/api/v1/events/notifications/${encodeURIComponent(destinationName)}`);
    await new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
  }
});

test('live risky suite supports hub install and remove for an eligible catalog component', async ({ page }) => {
  let target: HubComponent | null = null;

  try {
    await page.goto('/admin/hub');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    const [searchResults, installedResults] = await Promise.all([
      getJSONWithToken<HubComponent[]>(page, '/api/v1/hub/search?q='),
      getJSONWithToken<Array<{ name?: string }>>(page, '/api/v1/hub/instances'),
    ]);

    const installedNames = new Set((installedResults ?? []).map((item) => item.name).filter(Boolean) as string[]);
    target = (searchResults ?? []).find((component) =>
      ['preset', 'policy', 'skill', 'workspace'].includes(component.kind)
      && !!component.name
      && !installedNames.has(component.name),
    ) ?? null;

    test.skip(!target, 'No eligible removable hub component was available in the local catalog');

    await page.getByRole('tab', { name: /^Browse$/ }).click();
    await page.getByPlaceholder('Search components...').fill(target!.name);
    await page.getByRole('button', { name: /^Search$/ }).click();
    await settle(page);

    const browseRow = page.locator('div.bg-card').filter({ has: page.getByText(target!.name, { exact: true }) }).first();
    await expect(browseRow).toBeVisible();
    await browseRow.getByRole('button', { name: /^Install$/ }).click();
    await settle(page);

    await page.getByRole('tab', { name: /installed/i }).click();
    await settle(page);

    const installedRow = page.locator('tr').filter({ has: page.getByText(target!.name, { exact: true }) }).first();
    await expect(installedRow).toBeVisible();
    await installedRow.getByRole('button', { name: /^Remove$/ }).click();
    await settle(page);
    await expect(installedRow).toHaveCount(0);
  } finally {
    if (target?.name) {
      await requestWithToken(page, 'DELETE', `/api/v1/hub/${encodeURIComponent(target.name)}`).catch(() => {});
    }
  }
});

test('live risky suite supports connector deactivate and reactivate for a ready installed connector', async ({ page }) => {
  let target: { name: string; state?: string } | null = null;
  let installedByTest = false;

  try {
    const ensured = await ensureInstalledHubComponent(page, 'connector');
    target = ensured.component;
    installedByTest = ensured.installedByTest;
    test.skip(!target, 'No connector component was available in the local catalog');

    await page.goto('/admin/intake');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    const connectors = await getJSONWithToken<Array<{ name: string; state?: string }>>(page, '/api/v1/hub/instances?kind=connector');
    const runtimeTarget = (connectors ?? []).find((connector) => connector.name === target!.name) ?? null;
    test.skip(!runtimeTarget, 'Installed connector did not appear in intake list');
    target = runtimeTarget;

    const connectorButton = page.getByRole('button', { name: new RegExp(target!.name) }).first();
    await expect(connectorButton).toBeVisible();
    await connectorButton.click();
    await settle(page);

    const toggleLabel = target!.state === 'active' ? 'Deactivate' : 'Activate';
    const revertLabel = target!.state === 'active' ? 'Activate' : 'Deactivate';

    await page.getByRole('button', { name: toggleLabel }).click();
    await settle(page);
    await expect(page.getByRole('button', { name: revertLabel })).toBeVisible();

    await page.getByRole('button', { name: revertLabel }).click();
    await settle(page);
    await expect(page.getByRole('button', { name: toggleLabel })).toBeVisible();
  } finally {
    if (target?.name) {
      const desiredPath = target.state === 'active'
        ? `/api/v1/hub/${encodeURIComponent(target.name)}/activate`
        : `/api/v1/hub/${encodeURIComponent(target.name)}/deactivate`;
      await requestWithToken(page, 'POST', desiredPath).catch(() => {});
      if (installedByTest) {
        await requestWithToken(page, 'DELETE', `/api/v1/hub/${encodeURIComponent(target.name)}`).catch(() => {});
      }
    }
  }
});

test('live risky suite supports pack deploy and teardown for an installed pack', async ({ page }) => {
  let target: { name: string } | null = null;
  let installedByTest = false;

  try {
    const ensured = await ensureInstalledHubComponent(page, 'pack');
    target = ensured.component ? { name: ensured.component.name } : null;
    installedByTest = ensured.installedByTest;
    test.skip(!target, 'No pack component was available in the local catalog');

    await page.goto('/admin/hub');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    const packs = await getJSONWithToken<Array<{ name: string; kind?: string }>>(page, '/api/v1/hub/instances?kind=pack');
    target = (packs ?? []).find((pack) => pack.name === target!.name) ?? null;
    test.skip(!target, 'Installed pack did not appear in the instance list');

    await page.getByRole('tab', { name: /^Deploy$/ }).click();
    await page.locator('select').selectOption(target!.name);
    await page.getByRole('button', { name: /^Deploy$/ }).first().click();
    await settle(page);

    await expect(page.locator('pre')).toContainText(/status|deployed|pack/i);

    const packRow = page.locator('tr').filter({ has: page.getByText(target!.name, { exact: true }) }).first();
    await expect(packRow).toBeVisible();
    await packRow.getByRole('button', { name: /^Teardown$/ }).click();
    await page.getByRole('button', { name: /^Teardown$/ }).last().click();
    await settle(page);
    await expect(page.locator('pre')).toContainText(new RegExp(target!.name));
  } finally {
    if (target?.name) {
      await requestWithToken(page, 'POST', `/api/v1/hub/teardown/${encodeURIComponent(target.name)}`).catch(() => {});
      if (installedByTest) {
        await requestWithToken(page, 'DELETE', `/api/v1/hub/${encodeURIComponent(target.name)}`).catch(() => {});
      }
    }
  }
});

test('live risky suite supports mission create, update, and delete for an unassigned mission', async ({ page }) => {
  const missionName = uniqueName('playwright-mission');
  const description = `Live mission ${missionName}`;
  const updatedDescription = `${description} updated`;
  const headers = await authHeaders(page);

  try {
    await page.goto('/missions');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await bestEffortDeleteMission(page, missionName);

    await page.getByRole('button', { name: /new mission|create mission/i }).click();
    await page.getByPlaceholder('my-mission').fill(missionName);
    await page.getByPlaceholder('What does this mission do?').fill(description);
    await page.getByRole('button', { name: /^Next$/ }).click();
    await page.getByPlaceholder(/Describe what the agent should do when this mission is active/).fill(`Monitor ${missionName} and summarize findings.`);
    await page.getByRole('button', { name: /^Next$/ }).click();
    await page.getByRole('button', { name: /^Next$/ }).click();
    await page.getByRole('button', { name: /^Next$/ }).click();
    await page.getByRole('button', { name: /^Next$/ }).click();
    await page.getByRole('button', { name: /^Create Mission$/ }).last().click();
    await settle(page);

    const missionCard = page.locator('div.bg-card').filter({ has: page.getByText(missionName, { exact: true }) }).first();
    await expect(missionCard).toBeVisible();
    await expect(missionCard).toContainText('unassigned');

    await missionCard.click();
    await settle(page);
    await expect(page.getByRole('button', { name: 'Delete mission' })).toBeVisible();

    const descriptionSection = page.locator('span').filter({ hasText: 'Description' }).first().locator('xpath=ancestor::div[1]/..');
    await descriptionSection.getByText(/^edit$/).click();
    const descriptionInput = descriptionSection.locator('input').first();
    await descriptionInput.fill(updatedDescription);
    await descriptionInput.blur();
    await settle(page);
    await expect(page.getByText(updatedDescription, { exact: false })).toBeVisible();

    await page.getByRole('button', { name: 'Delete mission' }).click();
    await page.getByRole('button', { name: /^Delete$/ }).last().click();
    await settle(page);
    await expect(page).toHaveURL(/\/missions$/);
    await expect(page.getByText(missionName, { exact: true })).toHaveCount(0);
  } finally {
    const response = await page.request.delete(`/api/v1/missions/${encodeURIComponent(missionName)}`, { headers }).catch(() => null);
    if (response && ![200, 204, 404].includes(response.status())) {
      throw new Error(`mission delete failed for ${missionName}: ${response.status()}`);
    }
  }
});

test('live risky suite supports agent create, start, and delete with observable post-start state', async ({ page }) => {
  const agentName = uniqueName('playwright-agent');
  const headers = await authHeaders(page);

  try {
    await page.goto('/agents');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await bestEffortDeleteAgent(page, agentName);

    await page.getByRole('button', { name: /^Create$/ }).click();
    await page.getByLabel('Name').fill(agentName);
    await page.getByLabel('Start agent immediately').uncheck();
    await page.getByRole('button', { name: /^Create$/ }).last().click();
    await settle(page);

    const agentRow = page.getByRole('button', { name: new RegExp(agentName) }).first();
    await expect(agentRow).toBeVisible();
    await agentRow.click();
    await settle(page);

    await expect(page.getByRole('button', { name: /^Start$/ })).toBeVisible();
    await page.getByRole('button', { name: /^Start$/ }).click();
    await settle(page);

    await expect.poll(async () => {
      const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(agentName)}`, { headers });
      if (!response.ok()) return 'missing';
      const detail = await response.json() as { status?: string };
      return detail.status ?? 'unknown';
    }, { timeout: 30_000 }).not.toBe('stopped');

    await expect(page.getByRole('button', { name: /^Start$/ })).toHaveCount(0);
  } finally {
    await bestEffortDeleteAgent(page, agentName);
  }
});
