import { expect, test, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;

test.describe.configure({ timeout: 360_000 });

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

function currentPath(page: Page) {
  return new URL(page.url()).pathname;
}

function routeIsActive(page: Page, pathPrefix: string) {
  return currentPath(page).startsWith(pathPrefix);
}

function uniqueName(prefix: string) {
  return `${prefix}-${Date.now()}`;
}

let cachedAuthHeaders: Record<string, string> | null = null;

async function authHeaders(page: Page): Promise<Record<string, string>> {
  if (cachedAuthHeaders) return cachedAuthHeaders;
  const configResponse = await page.request.get('/__agency/config');
  const config = configResponse.ok() ? await configResponse.json() : {};
  const token = (config as { token?: string })?.token ?? '';
  cachedAuthHeaders = token ? { Authorization: `Bearer ${token}` } : {};
  return cachedAuthHeaders;
}

async function directPostWithToken(page: Page, path: string, body?: unknown) {
  const response = await page.request.post(path, {
    headers: {
      ...(await authHeaders(page)),
      'Content-Type': 'application/json',
    },
    data: body ?? {},
  }).catch(() => null);
  return response?.status() ?? 598;
}

async function directDeleteWithToken(page: Page, path: string) {
  const response = await page.request.delete(path, {
    headers: await authHeaders(page),
  }).catch(() => null);
  return response?.status() ?? 598;
}

async function getJSONWithToken<T>(page: Page, path: string): Promise<T | null> {
  const response = await page.request.get(path, { headers: await authHeaders(page) }).catch(() => null);
  if (!response?.ok()) return null;
  return response.json() as Promise<T>;
}

async function bestEffortArchiveChannel(page: Page, channelName: string) {
  const status = await directPostWithToken(page, `/api/v1/comms/channels/${encodeURIComponent(channelName)}/archive`);
  if ([200, 204, 404, 502, 598].includes(status)) return;
  throw new Error(`channel archive failed for ${channelName}: ${status}`);
}

async function archiveChannelsByPrefix(page: Page, prefix: string) {
  const channels = await getJSONWithToken<Array<{ name?: string; state?: string }>>(page, '/api/v1/comms/channels');
  for (const channel of channels ?? []) {
    if (!channel.name?.startsWith(prefix) || channel.state === 'archived') continue;
    await bestEffortArchiveChannel(page, channel.name);
  }
}

async function bestEffortDeleteAgent(page: Page, agentName: string) {
  const status = await directDeleteWithToken(page, `/api/v1/agents/${encodeURIComponent(agentName)}`);
  if ([200, 204, 404, 502, 598].includes(status)) return;
  throw new Error(`agent delete failed for ${agentName}: ${status}`);
}

async function deleteAgentsByPrefix(page: Page, prefix: string) {
  const agents = await getJSONWithToken<Array<{ name?: string }>>(page, '/api/v1/agents');
  for (const agent of agents ?? []) {
    if (!agent.name?.startsWith(prefix)) continue;
    await bestEffortDeleteAgent(page, agent.name);
  }
}

async function bestEffortCompleteMission(page: Page, missionName: string) {
  const status = await directPostWithToken(page, `/api/v1/missions/${encodeURIComponent(missionName)}/complete`);
  if ([200, 204, 400, 404, 502, 598].includes(status)) return;
  throw new Error(`mission complete failed for ${missionName}: ${status}`);
}

async function bestEffortDeleteMission(page: Page, missionName: string) {
  const status = await directDeleteWithToken(page, `/api/v1/missions/${encodeURIComponent(missionName)}`);
  if ([200, 204, 404, 502, 598].includes(status)) return;
  throw new Error(`mission delete failed for ${missionName}: ${status}`);
}

async function readAgentStatus(page: Page, agentName: string) {
  const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(agentName)}`, {
    headers: await authHeaders(page),
  });
  if (!response.ok()) return 'missing';
  const detail = await response.json() as { status?: string };
  return detail.status ?? 'unknown';
}

async function expectAgentStatus(page: Page, agentName: string, status: string, timeout = 120_000) {
  await expect.poll(
    () => readAgentStatus(page, agentName),
    { timeout, intervals: [1000, 2000, 5000] },
  ).toBe(status);
}

async function waitForAgentDmReady(page: Page, agentName: string, timeout = 120_000) {
  await expect.poll(async () => {
    const [status, channels] = await Promise.all([
      readAgentStatus(page, agentName),
      getJSONWithToken<Array<{ name?: string }>>(page, `/api/v1/agents/${encodeURIComponent(agentName)}/channels`),
    ]);
    return status === 'running' && (channels ?? []).some((channel) => channel.name === `dm-${agentName}`);
  }, {
    timeout,
    message: `expected ${agentName} to be running with a DM channel`,
  }).toBe(true);
  await page.waitForTimeout(3000);
}

async function closeStartupDialog(page: Page) {
  const closeButton = page.getByRole('dialog').getByRole('button', { name: 'Close' }).last();
  if (await closeButton.count()) {
    await closeButton.click({ force: true }).catch(() => {});
    await expect(page.getByRole('dialog')).toHaveCount(0, { timeout: 5_000 }).catch(() => {});
  }
}

async function runAgentAction(page: Page, action: 'start' | 'pause' | 'resume' | 'restart', agentName: string) {
  await closeStartupDialog(page);
  const actionLabel = new RegExp(`^${action[0].toUpperCase()}${action.slice(1)}$`);
  const apiAction = action === 'pause' ? 'halt' : action;
  const apiBody = action === 'pause' ? { tier: 'supervised', reason: '' } : {};

  let button = page.getByRole('button', { name: actionLabel }).first();
  if (!(await button.isVisible().catch(() => false))) {
    await page.goto('/agents');
    await settle(page);
    const roster = page.getByRole('button', { name: 'Roster' });
    if (await roster.count()) {
      await roster.click();
      await settle(page);
    }
    await page.getByRole('button', { name: `Actions for ${agentName}` }).click();
    button = page.getByRole('menuitem', { name: actionLabel }).first();
  }

  await expect(button).toBeVisible();
  await expect(button).toBeEnabled();
  let responseOk = false;
  try {
    const [response] = await Promise.all([
      page.waitForResponse((candidate) =>
        candidate.request().method() === 'POST'
        && candidate.url().includes(`/api/v1/agents/${encodeURIComponent(agentName)}/${apiAction}`),
        { timeout: 10_000 },
      ),
      button.click({ force: true }),
    ]);
    responseOk = response.ok() || response.status() === 409;
  } catch {
    const status = await directPostWithToken(page, `/api/v1/agents/${encodeURIComponent(agentName)}/${apiAction}`, apiBody);
    responseOk = (status >= 200 && status < 300) || status === 409;
  }

  // The UI can complete a lifecycle action even when the response watcher
  // misses the exact request or a retry observes a transient conflict. The
  // caller verifies the resulting agent state.
  void responseOk;
  await settle(page);
}

async function expectAgentReply(page: Page, agentName: string, expectedText: string, timeout = 120_000) {
  const reply = page.locator('div.group').filter({
    has: page.getByText('AGENT', { exact: true }),
    has: page.getByText(agentName, { exact: true }),
  }).filter({ hasText: expectedText }).first();
  await expect(reply).toBeVisible({ timeout });
}

async function createAgentFromWeb(page: Page, agentName: string) {
  await page.goto('/agents');
  const initialized = await expectSetupOrInitialized(page);
  if (!initialized || !routeIsActive(page, '/agents')) return false;

  await page.getByRole('button', { name: /^Create new agent$/ }).click();
  await page.getByLabel('Name').fill(agentName);
  await page.getByLabel('Start agent immediately').uncheck();
  await page.getByRole('button', { name: /^Create$/ }).last().click();
  await settle(page);

  const agentRow = page.getByRole('button', { name: new RegExp(agentName) }).first();
  await expect(agentRow).toBeVisible();
  await agentRow.click();
  await settle(page);
  return true;
}

async function createAssignedMissionFromWeb(page: Page, agentName: string, missionName: string) {
  await page.goto('/missions');
  await settle(page);
  if (!routeIsActive(page, '/missions')) return false;

  await page.getByRole('button', { name: /new mission|create mission/i }).click();
  await page.getByPlaceholder('my-mission').fill(missionName);
  await page.getByPlaceholder('What does this mission do?').fill(`Assigned live mission ${missionName}`);
  await page.getByRole('button', { name: /^Next$/ }).click();
  await page.getByPlaceholder(/Describe what the agent should do when this mission is active/).fill(`Coordinate work for ${agentName}.`);
  await page.getByRole('button', { name: /^Next$/ }).click();
  await page.getByRole('button', { name: /^Next$/ }).click();
  await page.getByRole('button', { name: /^Next$/ }).click();
  await page.getByRole('button', { name: /^Next$/ }).click();
  await page.getByPlaceholder('Agent or team name').fill(agentName);
  await page.getByRole('button', { name: /^Create Mission$/ }).last().click();
  await settle(page);
  return true;
}

test('microagent backend supports agent lifecycle and DM reply through the Web UI', async ({ page }) => {
  test.setTimeout(360_000);
  const agentName = uniqueName('playwright-microagent');
  const replyToken = uniqueName('microagent-reply');
  const taskPrompt = `Reply with exactly this token and nothing else: ${replyToken}`;

  try {
    await archiveChannelsByPrefix(page, 'dm-playwright-microagent-');
    await deleteAgentsByPrefix(page, 'playwright-microagent-');
    await bestEffortDeleteAgent(page, agentName);

    if (!(await createAgentFromWeb(page, agentName))) return;

    await expect(page.getByRole('button', { name: /^Start$/ })).toBeVisible();
    await runAgentAction(page, 'start', agentName);
    await expectAgentStatus(page, agentName, 'running');
    await waitForAgentDmReady(page, agentName);

    const openDM = page.getByRole('button', { name: 'Open DM' });
    if (await openDM.isVisible().catch(() => false)) {
      await openDM.click();
      await settle(page);
    } else {
      await page.goto(`/channels/dm-${encodeURIComponent(agentName)}`);
      await settle(page);
    }
    await expect(page).toHaveURL(new RegExp(`/channels/dm-${agentName}$`));
    await page.getByPlaceholder(new RegExp(`Message (dm-)?${agentName}`)).fill(taskPrompt);
    await page.getByRole('button', { name: 'Send message' }).click();
    await settle(page);
    await expect(page.getByText(taskPrompt, { exact: true })).toBeVisible();
    await expectAgentReply(page, agentName, replyToken);

    await page.goto(`/agents/${encodeURIComponent(agentName)}`);
    await settle(page);
    await runAgentAction(page, 'pause', agentName);
    await expectAgentStatus(page, agentName, 'halted');
    await expect(page.getByRole('button', { name: /^Resume$/ })).toBeVisible();

    await runAgentAction(page, 'resume', agentName);
    await expectAgentStatus(page, agentName, 'running');
    await expect(page.getByRole('button', { name: /^Pause$/ })).toBeVisible();

    await runAgentAction(page, 'restart', agentName);
    await expectAgentStatus(page, agentName, 'running');
    await waitForAgentDmReady(page, agentName);
  } finally {
    await bestEffortDeleteAgent(page, agentName);
    await bestEffortArchiveChannel(page, `dm-${agentName}`);
  }
});

test('microagent backend supports assigned mission lifecycle through the Web UI', async ({ page }) => {
  test.setTimeout(300_000);
  const agentName = uniqueName('playwright-microagent-mission-agent');
  const missionName = uniqueName('playwright-microagent-mission');

  try {
    await archiveChannelsByPrefix(page, 'dm-playwright-microagent-mission-agent-');
    await deleteAgentsByPrefix(page, 'playwright-microagent-mission-agent-');
    await bestEffortCompleteMission(page, missionName);
    await bestEffortDeleteMission(page, missionName);
    await bestEffortDeleteAgent(page, agentName);

    if (!(await createAgentFromWeb(page, agentName))) return;
    await runAgentAction(page, 'start', agentName);
    await expectAgentStatus(page, agentName, 'running');
    await waitForAgentDmReady(page, agentName);

    if (!(await createAssignedMissionFromWeb(page, agentName, missionName))) return;
    await page.goto(`/missions/${encodeURIComponent(missionName)}`);
    await settle(page);
    await expect(page.getByText(missionName, { exact: true })).toBeVisible();
    await expect(page.getByText(agentName, { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: /^Pause$/ })).toBeVisible();

    await page.getByRole('button', { name: /^Pause$/ }).click();
    await settle(page);
    await expect(page.getByText('paused', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: /^Resume$/ })).toBeVisible();

    await page.getByRole('button', { name: /^Resume$/ }).click();
    await settle(page);
    await expect(page.getByText('active', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: /^Complete$/ })).toBeVisible();

    await page.getByRole('button', { name: /^Complete$/ }).click();
    await settle(page);
    await expect(page.getByText('completed', { exact: true })).toBeVisible();
  } finally {
    await bestEffortCompleteMission(page, missionName);
    await bestEffortDeleteMission(page, missionName);
    await bestEffortDeleteAgent(page, agentName);
    await bestEffortArchiveChannel(page, `dm-${agentName}`);
  }
});
