import { expect, test, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;
const enabled = process.env.AGENCY_E2E_FIRECRACKER_WEBUI === '1';

test.describe.configure({ timeout: 240_000 });
test.skip(!enabled, 'requires AGENCY_E2E_FIRECRACKER_WEBUI=1 and a Firecracker-capable live stack');

let cachedAuthHeaders: Record<string, string> | null = null;

async function authHeaders(page: Page): Promise<Record<string, string>> {
  if (cachedAuthHeaders) return cachedAuthHeaders;
  const configResponse = await page.request.get('/__agency/config');
  const config = configResponse.ok() ? await configResponse.json() : {};
  const token = (config as { token?: string })?.token ?? '';
  cachedAuthHeaders = token ? { Authorization: `Bearer ${token}` } : {};
  return cachedAuthHeaders;
}

async function settle(page: Page) {
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(1000);
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

async function deleteAgent(page: Page, name: string) {
  const response = await page.request.delete(`/api/v1/agents/${encodeURIComponent(name)}`, {
    headers: await authHeaders(page),
    timeout: 10_000,
  });
  if (![200, 204, 404].includes(response.status())) {
    throw new Error(`agent delete failed for ${name}: ${response.status()}`);
  }
}

async function readMessages(page: Page, channel: string) {
  const response = await page.request.get(
    `/api/v1/comms/channels/${encodeURIComponent(channel)}/messages?limit=100&reader=operator`,
    { headers: await authHeaders(page) },
  );
  if (!response.ok()) return [];
  return response.json() as Promise<Array<{ author?: string; content?: string }>>;
}

async function waitForDmReady(page: Page, name: string) {
  await expect.poll(async () => {
    const headers = await authHeaders(page);
    const [agentResponse, channelsResponse] = await Promise.all([
      page.request.get(`/api/v1/agents/${encodeURIComponent(name)}`, { headers }),
      page.request.get(`/api/v1/agents/${encodeURIComponent(name)}/channels`, { headers }),
    ]);
    if (!agentResponse.ok() || !channelsResponse.ok()) return false;
    const agent = await agentResponse.json() as { status?: string };
    const channels = await channelsResponse.json() as Array<{ name?: string }>;
    return agent.status === 'running' && channels.some((channel) => channel.name === `dm-${name}`);
  }, { timeout: 120_000 }).toBe(true);
}

test('Firecracker agent can be managed and messaged through the web UI', async ({ page }) => {
  const agentName = `fc-webui-${Date.now()}`;
  const prompt = `Web UI Firecracker smoke ${agentName}: acknowledge briefly.`;

  try {
    await page.goto('/agents');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) return;

    await deleteAgent(page, agentName);
    await page.getByRole('button', { name: 'Create new agent' }).click();
    await expect(page.getByRole('heading', { name: 'Create Agent' })).toBeVisible();
    await page.getByLabel('Name').fill(agentName);
    await page.getByRole('button', { name: /^Create$/ }).click();
    await expect(page).toHaveURL(new RegExp(`/channels/dm-${agentName}$`), { timeout: 180_000 });
    await waitForDmReady(page, agentName);

    await page.goto(`/agents/${encodeURIComponent(agentName)}`);
    await settle(page);
    await expect(page.getByText(agentName).first()).toBeVisible();
    await expect(page.getByText('running').first()).toBeVisible();
    await page.getByRole('button', { name: 'Open DM' }).click();
    await expect(page).toHaveURL(new RegExp(`/channels/dm-${agentName}$`));
    await settle(page);

    const before = await readMessages(page, `dm-${agentName}`);
    const priorAgentReplies = before.filter((message) => message.author === agentName).length;
    await page.getByPlaceholder(`Message ${agentName}...`).fill(prompt);
    await page.getByRole('button', { name: 'Send message' }).click();
    await expect(page.getByText(prompt, { exact: true }).first()).toBeVisible();

    await expect.poll(async () => {
      const messages = await readMessages(page, `dm-${agentName}`);
      return messages.filter((message) => message.author === agentName).slice(priorAgentReplies)[0]?.content ?? '';
    }, { timeout: 120_000, intervals: [2000, 5000, 10000] }).not.toBe('');
  } finally {
    await deleteAgent(page, agentName);
  }
});
