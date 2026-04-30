import { expect, test, type Page } from '@playwright/test';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;
const enabled = process.env.AGENCY_E2E_APPLE_VF_WEBUI === '1';

test.describe.configure({ timeout: 360_000 });
test.skip(!enabled, 'requires AGENCY_E2E_APPLE_VF_WEBUI=1 and an Apple VF-backed live stack');

type RuntimeManifest = {
  spec?: {
    transport?: { enforcer?: { type?: string; endpoint?: string } };
  };
};

type RuntimeStatus = {
  backend?: string;
  phase?: string;
  healthy?: boolean;
  details?: Record<string, string>;
};

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

async function expectInitialized(page: Page) {
  await settle(page);
  await expect(page.getByRole('heading', { name: SETUP_HEADING_PATTERN })).toHaveCount(0);
}

async function deleteAgent(page: Page, name: string) {
  await page.request.post(`/api/v1/agents/${encodeURIComponent(name)}/stop`, {
    headers: await authHeaders(page),
    data: { type: 'immediate', reason: 'apple-vf webui smoke cleanup' },
    timeout: 60_000,
  }).catch(() => undefined);
  const response = await page.request.delete(`/api/v1/agents/${encodeURIComponent(name)}`, {
    headers: await authHeaders(page),
    timeout: 30_000,
  });
  if (![200, 204, 404].includes(response.status())) {
    throw new Error(`agent delete failed for ${name}: ${response.status()}`);
  }
}

async function runtimeManifest(page: Page, name: string): Promise<RuntimeManifest> {
  const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(name)}/runtime/manifest`, {
    headers: await authHeaders(page),
    timeout: 10_000,
  });
  expect(response.ok()).toBe(true);
  return response.json() as Promise<RuntimeManifest>;
}

async function runtimeStatus(page: Page, name: string): Promise<RuntimeStatus> {
  const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(name)}/runtime/status`, {
    headers: await authHeaders(page),
    timeout: 10_000,
  });
  expect(response.ok()).toBe(true);
  return response.json() as Promise<RuntimeStatus>;
}

async function readMessages(page: Page, channel: string) {
  const response = await page.request.get(
    `/api/v1/comms/channels/${encodeURIComponent(channel)}/messages?limit=100&reader=operator`,
    { headers: await authHeaders(page), timeout: 10_000 },
  );
  if (!response.ok()) return [];
  return response.json() as Promise<Array<{ author?: string; content?: string }>>;
}

async function waitForRuntimeHealthy(page: Page, name: string) {
  await expect.poll(async () => {
    const status = await runtimeStatus(page, name);
    const details = status.details ?? {};
    if (!status.healthy) return status.phase ?? 'unhealthy';
    return [
      status.backend,
      status.phase,
      details.workload_vm_state,
      details.enforcer_component_state,
      details.body_ws_connected,
    ].join('|');
  }, { timeout: 120_000, intervals: [2000, 5000, 10000] }).toBe('apple-vf-microvm|running|running|running|true');
}

async function createAgentThroughUI(page: Page, name: string) {
  await deleteAgent(page, name);
  await page.goto('/agents');
  await expectInitialized(page);
  await page.evaluate(() => window.localStorage.setItem('agency.agents.variant', 'split'));
  await page.reload();
  await settle(page);
  await page.getByRole('button', { name: /^Create( new agent)?$/ }).click();
  await expect(page.getByRole('heading', { name: 'Create Agent' })).toBeVisible();
  await page.getByLabel('Name').fill(name);
  await page.getByRole('button', { name: /^Create$/ }).click();
  await expect(page).toHaveURL(new RegExp(`/channels/dm-${name}$`), { timeout: 180_000 });
  await waitForRuntimeHealthy(page, name);
}

async function sendDMAndWaitForReply(page: Page, name: string, prompt: string) {
  await page.goto(`/channels/dm-${encodeURIComponent(name)}`);
  await settle(page);
  const before = await readMessages(page, `dm-${name}`);
  const priorReplies = before.filter((message) => message.author === name).length;
  await page.getByPlaceholder(`Message dm-${name}...`).fill(prompt);
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(page.getByText(prompt, { exact: true }).first()).toBeVisible();
  await expect.poll(async () => {
    const messages = await readMessages(page, `dm-${name}`);
    return messages.filter((message) => message.author === name).slice(priorReplies)[0]?.content ?? '';
  }, { timeout: 120_000, intervals: [2000, 5000, 10000] }).not.toBe('');
}

async function verifyInfrastructureLogs(page: Page) {
  await page.goto('/admin/infrastructure');
  await expectInitialized(page);
  const commsRow = page.getByRole('row').filter({ hasText: 'comms' }).first();
  await expect(commsRow).toBeVisible();
  await commsRow.getByRole('button', { name: /^Logs$/ }).click();
  await expect(page.getByRole('dialog', { name: /comms logs/i })).toBeVisible();
  await expect(page.locator('pre').filter({ hasText: /\S/ }).first()).toBeVisible();
}

test('operator can manage and message an Apple VF agent through the web UI', async ({ page }) => {
  const agentName = `avf-webui-${Date.now()}`;
  const prompt = `Apple VF Web UI smoke ${agentName}: reply with one concise sentence.`;

  try {
    await verifyInfrastructureLogs(page);
    await createAgentThroughUI(page, agentName);

    await page.goto(`/agents/${encodeURIComponent(agentName)}`);
    await settle(page);
    await expect(page.getByText(agentName).first()).toBeVisible();
    await expect(page.getByText('running').first()).toBeVisible();
    await expect(page.getByText('apple-vf-microvm').first()).toBeVisible();

    const manifest = await runtimeManifest(page, agentName);
    const status = await runtimeStatus(page, agentName);
    expect(manifest.spec?.transport?.enforcer?.type).toBe('vsock_http');
    expect(manifest.spec?.transport?.enforcer?.endpoint).toMatch(/^vsock:\/\/\d+:\d+$/);
    expect(status.backend).toBe('apple-vf-microvm');
    expect(status.healthy).toBe(true);
    expect(status.details?.enforcer_component_state).toBe('running');
    expect(status.details?.body_ws_connected).toBe('true');

    await page.goto(`/channels/dm-${encodeURIComponent(agentName)}`);
    await settle(page);
    await expect(page).toHaveURL(new RegExp(`/channels/dm-${agentName}$`));
    await sendDMAndWaitForReply(page, agentName, prompt);

    const stopResponse = await page.request.post(`/api/v1/agents/${encodeURIComponent(agentName)}/stop`, {
      headers: await authHeaders(page),
      data: {},
      timeout: 60_000,
    });
    expect(stopResponse.ok()).toBe(true);
    await expect.poll(async () => {
      const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(agentName)}`, {
        headers: await authHeaders(page),
        timeout: 10_000,
      });
      if (!response.ok()) return 'missing';
      const agent = await response.json() as { status?: string };
      return agent.status ?? '';
    }, { timeout: 60_000, intervals: [1000, 2000, 5000] }).toMatch(/^(halted|stopped)$/);

    await page.goto(`/agents/${encodeURIComponent(agentName)}`);
    await settle(page);
    await page.getByRole('tab', { name: 'System' }).click();
    await page.getByRole('button', { name: /^Delete$/ }).click();
    await page.getByRole('button', { name: /^Delete$/ }).last().click();
    await expect.poll(async () => {
      const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(agentName)}`, {
        headers: await authHeaders(page),
        timeout: 10_000,
      });
      return response.status();
    }, { timeout: 30_000, intervals: [1000, 2000, 5000] }).toBe(404);
  } finally {
    await deleteAgent(page, agentName);
  }
});
