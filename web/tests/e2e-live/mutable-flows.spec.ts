import { expect, test, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;

test.describe.configure({ timeout: 180_000 });

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

async function isAdminTabSelected(page: Page, name: string) {
  const tab = page.getByRole('tab', { name, exact: true });
  if (!(await tab.count())) return false;
  return (await tab.first().getAttribute('aria-selected')) === 'true';
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

async function bestEffortDelete(page: Page, path: string) {
  const headers = await authHeaders(page);
  const response = await page.request.delete(path, { headers });
  const status = response.status();
  if (status === 200 || status === 204 || status === 404) {
    return;
  }
  throw new Error(`cleanup failed for ${path}: ${status}`);
}

async function waitForAgentAbsent(page: Page, name: string, timeout = 60_000) {
  const headers = await authHeaders(page);
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(name)}`, { headers });
    return response.status();
  }, {
    timeout,
    message: `expected ${name} to disappear after delete`,
  }).toBe(404);
}

async function deleteAgentAndWait(page: Page, name: string) {
  await bestEffortDelete(page, `/api/v1/agents/${encodeURIComponent(name)}`);
  await waitForAgentAbsent(page, name);
}

async function authHeaders(page: Page) {
  const configResponse = await page.request.get('/__agency/config');
  const config = configResponse.ok() ? await configResponse.json() : {};
  const token = (config as { token?: string })?.token ?? '';
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function prunePlaywrightArtifacts(page: Page) {
  const headers = await authHeaders(page);

  const missionsResponse = await page.request.get('/api/v1/missions', { headers });
  if (missionsResponse.ok()) {
    const missions = await missionsResponse.json() as Array<{ name?: string }>;
    for (const mission of missions) {
      const name = mission?.name ?? '';
      if (!name.startsWith('playwright-mission-')) continue;
      await cleanupMission(page, name);
    }
  }

  const agentsResponse = await page.request.get('/api/v1/agents', { headers });
  if (agentsResponse.ok()) {
    const agents = await agentsResponse.json() as Array<{ name?: string }>;
    for (const agent of agents) {
      const name = agent?.name ?? '';
      if (!name.startsWith('playwright-agent-')) continue;
      await deleteAgentAndWait(page, name);
    }
  }
}

async function createMissionViaApi(page: Page, name: string, description: string, instructions: string) {
  const response = await page.request.post('/api/v1/missions', {
    headers: {
      ...(await authHeaders(page)),
      'Content-Type': 'application/x-yaml',
    },
    data: `name: ${name}
description: ${JSON.stringify(description)}
instructions: ${JSON.stringify(instructions)}
`,
  });
  if (!response.ok()) {
    throw new Error(`mission create failed for ${name}: ${response.status()}`);
  }
}

async function createAgentViaApi(page: Page, name: string) {
  const response = await page.request.post('/api/v1/agents', {
    headers: {
      ...(await authHeaders(page)),
      'Content-Type': 'application/json',
    },
    data: { name, preset: 'generalist', mode: 'assisted' },
  });
  if (!response.ok()) {
    throw new Error(`agent create failed for ${name}: ${response.status()}`);
  }
  const startResponse = await page.request.post(`/api/v1/agents/${encodeURIComponent(name)}/start`, {
    headers: {
      ...(await authHeaders(page)),
      'Content-Type': 'application/json',
    },
    data: {},
  });
  if (!startResponse.ok()) {
    throw new Error(`agent start failed for ${name}: ${startResponse.status()}`);
  }
}

async function waitForAgentDmReady(page: Page, name: string, timeout = 60_000) {
  const headers = await authHeaders(page);
  await expect.poll(async () => {
    const [agentResponse, channelsResponse] = await Promise.all([
      page.request.get(`/api/v1/agents/${encodeURIComponent(name)}`, { headers }),
      page.request.get(`/api/v1/agents/${encodeURIComponent(name)}/channels`, { headers }),
    ]);
    if (!agentResponse.ok() || !channelsResponse.ok()) {
      return false;
    }
    const agent = await agentResponse.json() as { status?: string };
    const channels = await channelsResponse.json() as Array<{ name?: string }>;
    return agent.status === 'running' && channels.some((channel) => channel.name === `dm-${name}`);
  }, {
    timeout,
    message: `expected ${name} to be running with a DM channel`,
  }).toBe(true);

  // Give the runtime a moment to transition from "running" to actually processing DM work.
  await page.waitForTimeout(3000);
}

async function waitForAgentStatus(page: Page, name: string, status: string, timeout = 60_000) {
  const headers = await authHeaders(page);
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(name)}`, { headers });
    if (!response.ok()) {
      return null;
    }
    const agent = await response.json() as { status?: string };
    return agent.status ?? null;
  }, {
    timeout,
    message: `expected ${name} status to become ${status}`,
  }).toBe(status);
}

async function cleanupMission(page: Page, name: string) {
  const headers = {
    ...(await authHeaders(page)),
    'Content-Type': 'application/json',
  };
  const deleteResponse = await page.request.delete(`/api/v1/missions/${encodeURIComponent(name)}`, { headers });
  if (deleteResponse.status() === 200 || deleteResponse.status() === 204 || deleteResponse.status() === 404) {
    return;
  }
  if (deleteResponse.status() !== 400) {
    throw new Error(`cleanup failed for /api/v1/missions/${encodeURIComponent(name)}: ${deleteResponse.status()}`);
  }

  const completeResponse = await page.request.post(`/api/v1/missions/${encodeURIComponent(name)}/complete`, {
    headers,
    data: {},
  });
  if (!(completeResponse.status() === 200 || completeResponse.status() === 400 || completeResponse.status() === 404)) {
    throw new Error(`mission completion failed for ${name}: ${completeResponse.status()}`);
  }

  const retryDeleteResponse = await page.request.delete(`/api/v1/missions/${encodeURIComponent(name)}`, { headers });
  if (retryDeleteResponse.status() === 200 || retryDeleteResponse.status() === 204 || retryDeleteResponse.status() === 404) {
    return;
  }
  throw new Error(`cleanup failed for /api/v1/missions/${encodeURIComponent(name)} after complete: ${retryDeleteResponse.status()}`);
}

async function waitForMissionStatus(page: Page, name: string, status: string, timeout = 60_000) {
  const headers = await authHeaders(page);
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/missions/${encodeURIComponent(name)}`, { headers });
    if (!response.ok()) {
      return null;
    }
    const mission = await response.json() as { status?: string };
    return mission.status ?? null;
  }, {
    timeout,
    message: `expected ${name} status to become ${status}`,
  }).toBe(status);
}

async function sendChannelMessageViaApi(page: Page, channelName: string, content: string) {
  const response = await page.request.post(`/api/v1/comms/channels/${encodeURIComponent(channelName)}/messages`, {
    headers: {
      ...(await authHeaders(page)),
      'Content-Type': 'application/json',
    },
    data: { content },
  });
  if (!response.ok()) {
    throw new Error(`message send failed for ${channelName}: ${response.status()}`);
  }
}

async function expectAgentReply(page: Page, agentName: string, expectedText: string, timeout = 120_000) {
  const reply = page.locator('div.group').filter({
    has: page.getByText('AGENT', { exact: true }),
    has: page.getByText(agentName, { exact: true }),
  }).filter({ hasText: expectedText }).first();
  await expect(reply).toBeVisible({ timeout });
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
  let exercised = false;

  try {
    await page.goto('/profiles');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }
    if (!routeIsActive(page, '/profiles')) {
      return;
    }
    exercised = true;

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
    if (!exercised) return;
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
    if (!(await isAdminTabSelected(page, 'Webhooks'))) {
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
    if (!(await isAdminTabSelected(page, 'Notifications'))) {
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

test('live stack supports agent create flow from the agents screen', async ({ page }) => {
  const agentName = uniqueName('playwright-agent');

  try {
    await page.goto('/agents');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await prunePlaywrightArtifacts(page);
    await deleteAgentAndWait(page, agentName);

    await page.getByRole('button', { name: 'Create' }).click();
    await expect(page.getByRole('heading', { name: 'Create Agent' })).toBeVisible();
    await page.getByPlaceholder('my-agent').fill(agentName);
    await page.getByRole('button', { name: /^Create$/ }).click();
    await settle(page);

    await page.goto(`/agents/${encodeURIComponent(agentName)}`);
    await settle(page);
    await expect(page.getByRole('code').filter({ hasText: agentName })).toBeVisible();
    await expect(page.getByText(/Mode: assisted|Mode: autonomous/).first()).toBeVisible();
  } finally {
    await deleteAgentAndWait(page, agentName);
  }
});

test('live stack supports first useful DM reply flow for a running agent', async ({ page }) => {
  const agentName = uniqueName('playwright-agent');
  const replyToken = uniqueName('alpha-ready');
  const taskPrompt = `Reply with exactly this token and nothing else: ${replyToken}`;

  try {
    await page.goto('/agents');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await prunePlaywrightArtifacts(page);
    await deleteAgentAndWait(page, agentName);
    await createAgentViaApi(page, agentName);
    await waitForAgentDmReady(page, agentName);
    await sendChannelMessageViaApi(page, `dm-${agentName}`, taskPrompt);

    await page.goto(`/channels/dm-${encodeURIComponent(agentName)}`);
    await settle(page);
    await expect(page).toHaveURL(new RegExp(`/channels/dm-${agentName}$`));
    await expect(page.getByText(taskPrompt, { exact: true })).toBeVisible();
    await expectAgentReply(page, agentName, replyToken);
  } finally {
    await deleteAgentAndWait(page, agentName);
  }
});

test('live stack supports agent pause, resume, and restart lifecycle flow', async ({ page }) => {
  const agentName = uniqueName('playwright-agent');

  try {
    await page.goto('/agents');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }

    await prunePlaywrightArtifacts(page);
    await deleteAgentAndWait(page, agentName);
    await createAgentViaApi(page, agentName);
    await waitForAgentDmReady(page, agentName);

    await page.goto(`/agents/${encodeURIComponent(agentName)}`);
    await settle(page);
    await expect(page.getByRole('code').filter({ hasText: agentName })).toBeVisible();

    await page.getByRole('button', { name: /^Pause$/ }).click();
    await waitForAgentStatus(page, agentName, 'halted');
    await settle(page);
    await expect(page.getByRole('button', { name: /^Resume$/ })).toBeVisible();

    await page.getByRole('button', { name: /^Resume$/ }).click();
    await waitForAgentStatus(page, agentName, 'running');
    await settle(page);
    await expect(page.getByRole('button', { name: /^Restart$/ })).toBeVisible();

    await page.getByRole('button', { name: /^Restart$/ }).click();
    await waitForAgentStatus(page, agentName, 'running');
    await waitForAgentDmReady(page, agentName);
    await settle(page);
    await expect(page.getByRole('button', { name: /^Pause$/ })).toBeVisible();
  } finally {
    await deleteAgentAndWait(page, agentName);
  }
});

test('live stack supports team create and delete flow', async ({ page }) => {
  const teamName = uniqueName('playwright-team');
  let exercised = false;

  try {
    await page.goto('/teams');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }
    if (!routeIsActive(page, '/teams')) {
      return;
    }
    exercised = true;

    await prunePlaywrightArtifacts(page);
    await bestEffortDelete(page, `/api/v1/admin/teams/${encodeURIComponent(teamName)}`);

    await page.getByRole('button', { name: 'Create Team' }).click();
    await page.getByPlaceholder('Team name...').fill(teamName);
    await page.getByRole('button', { name: /^Create$/ }).click();
    await settle(page);

    await expect(page.getByText(teamName, { exact: true })).toBeVisible();

    await page.getByRole('button', { name: `Delete team ${teamName}` }).click();
    await page.getByRole('button', { name: /^Delete$/ }).click();
    await settle(page);

    await expect(page.getByText(teamName, { exact: true })).toHaveCount(0);
  } finally {
    if (!exercised) return;
    await bestEffortDelete(page, `/api/v1/admin/teams/${encodeURIComponent(teamName)}`);
  }
});

test('live stack supports mission assignment from mission detail', async ({ page }) => {
  const agentName = uniqueName('playwright-agent');
  const missionName = uniqueName('playwright-mission');
  const missionDescription = 'Mission assigned from detail page during live coverage';
  const missionInstructions = 'Acknowledge assignment and report status back to the operator.';
  let exercised = false;

  try {
    await page.goto('/missions');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }
    if (!routeIsActive(page, '/missions')) {
      return;
    }
    exercised = true;

    await prunePlaywrightArtifacts(page);
    await cleanupMission(page, missionName);
    await deleteAgentAndWait(page, agentName);

    await createAgentViaApi(page, agentName);
    await createMissionViaApi(page, missionName, missionDescription, missionInstructions);

    await page.goto(`/missions/${encodeURIComponent(missionName)}`);
    await settle(page);
    await expect(page.getByText('unassigned', { exact: true }).first()).toBeVisible();

    await page.getByRole('button', { name: 'assign', exact: true }).click();
    await page.getByLabel('Assign target').fill(agentName);
    await page.getByRole('button', { name: 'Assign' }).last().click();
    await settle(page);

    await expect(page.getByText('Assigned To', { exact: true }).first()).toBeVisible();
    await expect(page.getByText(agentName, { exact: true })).toBeVisible();
    await expect(page.getByText('(agent)', { exact: true })).toBeVisible();
  } finally {
    if (!exercised) return;
    await cleanupMission(page, missionName);
    await deleteAgentAndWait(page, agentName);
  }
});

test('live stack supports assigned mission pause, resume, complete, and delete flow', async ({ page }) => {
  const agentName = uniqueName('playwright-agent');
  const missionName = uniqueName('playwright-mission');
  const missionDescription = 'Mission lifecycle exercised from mission detail during live coverage';
  const missionInstructions = 'Stay ready for mission lifecycle testing and report state changes when asked.';
  let exercised = false;

  try {
    await page.goto('/missions');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }
    if (!routeIsActive(page, '/missions')) {
      return;
    }
    exercised = true;

    await prunePlaywrightArtifacts(page);
    await cleanupMission(page, missionName);
    await bestEffortDelete(page, `/api/v1/agents/${encodeURIComponent(agentName)}`);

    await createAgentViaApi(page, agentName);
    await createMissionViaApi(page, missionName, missionDescription, missionInstructions);

    await page.goto(`/missions/${encodeURIComponent(missionName)}`);
    await settle(page);

    await page.getByRole('button', { name: 'assign', exact: true }).click();
    await page.getByLabel('Assign target').fill(agentName);
    await page.getByRole('button', { name: 'Assign' }).last().click();
    await waitForMissionStatus(page, missionName, 'active');
    await settle(page);
    await expect(page.getByText('active', { exact: true }).first()).toBeVisible();

    await page.getByRole('button', { name: /^Pause$/ }).click();
    await waitForMissionStatus(page, missionName, 'paused');
    await settle(page);
    await expect(page.getByRole('button', { name: /^Resume$/ })).toBeVisible();

    await page.getByRole('button', { name: /^Resume$/ }).click();
    await waitForMissionStatus(page, missionName, 'active');
    await settle(page);
    await expect(page.getByRole('button', { name: /^Complete$/ })).toBeVisible();

    await page.getByRole('button', { name: /^Complete$/ }).click();
    await waitForMissionStatus(page, missionName, 'completed');
    await settle(page);
    await expect(page.getByText('completed', { exact: true }).first()).toBeVisible();

    await page.getByRole('button', { name: 'Delete mission' }).click();
    await page.getByRole('button', { name: /^Delete$/ }).click();
    await settle(page);
    await expect(page).toHaveURL(/\/missions$/);
    await expect(page.getByText(missionName, { exact: true })).toHaveCount(0);
  } finally {
    if (!exercised) return;
    await cleanupMission(page, missionName);
    await bestEffortDelete(page, `/api/v1/agents/${encodeURIComponent(agentName)}`);
  }
});

test('live stack supports mission create flow from the wizard', async ({ page }) => {
  const missionName = uniqueName('playwright-mission');
  const missionDescription = 'Live mission created by Playwright coverage';
  const missionInstructions = 'Check the platform shell and report whether the operator-facing surfaces look healthy.';
  let exercised = false;

  try {
    await page.goto('/missions');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) {
      return;
    }
    if (!routeIsActive(page, '/missions')) {
      return;
    }
    exercised = true;

    await prunePlaywrightArtifacts(page);
    await bestEffortDelete(page, `/api/v1/missions/${encodeURIComponent(missionName)}`);

    const createButton = page.getByRole('button', { name: /New Mission|Create Mission/ }).first();
    await createButton.click();
    await expect(page.getByRole('heading', { name: 'New Mission' })).toBeVisible();

    await page.getByPlaceholder('my-mission').fill(missionName);
    await page.getByPlaceholder('What does this mission do?').fill(missionDescription);
    await page.getByRole('button', { name: 'Next' }).click();

    await page.getByPlaceholder(/Describe what the agent should do when this mission is active/).fill(missionInstructions);
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();
    await page.getByRole('button', { name: 'Next' }).click();

    const createMissionButton = page.getByRole('button', { name: 'Create Mission' }).last();
    await expect(createMissionButton).toBeVisible();
    await createMissionButton.click();
    await settle(page);

    await expect(page.getByText(missionName, { exact: true })).toBeVisible();
    await expect(page.getByText(missionDescription)).toBeVisible();
  } finally {
    if (!exercised) return;
    await bestEffortDelete(page, `/api/v1/missions/${encodeURIComponent(missionName)}`);
  }
});
