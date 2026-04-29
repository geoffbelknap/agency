import { expect, test, type Page } from '@playwright/test';
import { execFile } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { promisify } from 'node:util';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;
const enabled = process.env.AGENCY_E2E_FIRECRACKER_WEBUI === '1';
const agencyBin = process.env.AGENCY_BIN || 'agency';
const execFileAsync = promisify(execFile);

test.describe.configure({ timeout: 240_000 });
test.skip(!enabled, 'requires AGENCY_E2E_FIRECRACKER_WEBUI=1 and a Firecracker-capable live stack');

let cachedAuthHeaders: Record<string, string> | null = null;

type RuntimeManifest = {
  backendStatus?: {
    details?: Record<string, string>;
  };
};

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

async function runAgency(args: string[]) {
  await execFileAsync(agencyBin, args, {
    env: process.env,
    timeout: 60_000,
  });
}

async function waitForGateway(page: Page) {
  await expect.poll(async () => {
    try {
      const response = await page.request.get('/api/v1/agents', {
        headers: await authHeaders(page),
        timeout: 5000,
      });
      return response.ok();
    } catch {
      return false;
    }
  }, { timeout: 60_000, intervals: [1000, 2000, 5000] }).toBe(true);
}

async function readMessages(page: Page, channel: string) {
  const response = await page.request.get(
    `/api/v1/comms/channels/${encodeURIComponent(channel)}/messages?limit=100&reader=operator`,
    { headers: await authHeaders(page) },
  );
  if (!response.ok()) return [];
  return response.json() as Promise<Array<{ author?: string; content?: string }>>;
}

async function runtimeManifest(page: Page, name: string): Promise<RuntimeManifest> {
  const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(name)}/runtime/manifest`, {
    headers: await authHeaders(page),
  });
  if (!response.ok()) {
    throw new Error(`runtime manifest failed for ${name}: ${response.status()}`);
  }
  return response.json() as Promise<RuntimeManifest>;
}

function processAlive(pid: number): boolean {
  if (!Number.isFinite(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (err) {
    return (err as NodeJS.ErrnoException).code === 'EPERM';
  }
}

function pidFromManifest(manifest: RuntimeManifest, key: string): number {
  return Number.parseInt(manifest.backendStatus?.details?.[key] ?? '', 10);
}

function stateDirFromManifest(manifest: RuntimeManifest): string {
  const logPath = manifest.backendStatus?.details?.log_path;
  if (!logPath) throw new Error('runtime manifest missing firecracker log_path');
  return path.dirname(path.dirname(logPath));
}

async function createAgentThroughUI(page: Page, name: string) {
  await deleteAgent(page, name);
  await page.getByRole('button', { name: 'Create new agent' }).click();
  await expect(page.getByRole('heading', { name: 'Create Agent' })).toBeVisible();
  await page.getByLabel('Name').fill(name);
  await page.getByRole('button', { name: /^Create$/ }).click();
  await expect(page).toHaveURL(new RegExp(`/channels/dm-${name}$`), { timeout: 180_000 });
  await waitForDmReady(page, name);
}

async function sendDMAndWaitForReply(page: Page, name: string, prompt: string) {
  await page.goto(`/channels/dm-${encodeURIComponent(name)}`);
  await settle(page);
  const before = await readMessages(page, `dm-${name}`);
  const priorAgentReplies = before.filter((message) => message.author === name).length;
  await page.getByPlaceholder(`Message ${name}...`).fill(prompt);
  await page.getByRole('button', { name: 'Send message' }).click();
  await expect(page.getByText(prompt, { exact: true }).first()).toBeVisible();

  await expect.poll(async () => {
    const messages = await readMessages(page, `dm-${name}`);
    return messages.filter((message) => message.author === name).slice(priorAgentReplies)[0]?.content ?? '';
  }, { timeout: 120_000, intervals: [2000, 5000, 10000] }).not.toBe('');
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

    await createAgentThroughUI(page, agentName);

    await page.goto(`/agents/${encodeURIComponent(agentName)}`);
    await settle(page);
    await expect(page.getByText(agentName).first()).toBeVisible();
    await expect(page.getByText('running').first()).toBeVisible();
    await page.getByRole('button', { name: 'Open DM' }).click();
    await expect(page).toHaveURL(new RegExp(`/channels/dm-${agentName}$`));
    await sendDMAndWaitForReply(page, agentName, prompt);
  } finally {
    await deleteAgent(page, agentName);
  }
});

test('Firecracker runtime recovers after daemon restart through the web UI', async ({ page }) => {
  const agentName = `fc-recover-${Date.now()}`;

  try {
    await page.goto('/agents');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) return;

    await createAgentThroughUI(page, agentName);
    await sendDMAndWaitForReply(page, agentName, `Firecracker recovery precheck ${agentName}: reply briefly.`);

    await runAgency(['serve', 'restart']);
    await waitForGateway(page);

    await expect.poll(async () => {
      const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(agentName)}/runtime/status`, {
        headers: await authHeaders(page),
      });
      if (!response.ok()) return 'unavailable';
      const status = await response.json() as { details?: Record<string, string>; phase?: string; healthy?: boolean };
      if (!status.healthy) return status.phase ?? 'unhealthy';
      return [
        status.phase,
        status.details?.workload_vm_state,
        status.details?.enforcer_component_state,
        status.details?.vsock_bridge_state,
        status.details?.body_ws_connected,
      ].join('|');
    }, { timeout: 120_000, intervals: [2000, 5000, 10000] }).toBe('running|running|running|running|true');

    await page.goto(`/agents/${encodeURIComponent(agentName)}`);
    await settle(page);
    await expect(page.getByText('running').first()).toBeVisible();
    await sendDMAndWaitForReply(page, agentName, `Firecracker recovery postcheck ${agentName}: reply briefly.`);
  } finally {
    await deleteAgent(page, agentName);
  }
});

test('Firecracker stop and delete clean up per-agent runtime artifacts', async ({ page }) => {
  const agentName = `fc-cleanup-${Date.now()}`;

  try {
    await page.goto('/agents');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) return;

    await createAgentThroughUI(page, agentName);
    const manifest = await runtimeManifest(page, agentName);
    const vmPID = pidFromManifest(manifest, 'pid');
    const enforcerPID = pidFromManifest(manifest, 'enforcer_pid');
    const stateDir = stateDirFromManifest(manifest);
    const runtimeDir = path.join(stateDir, agentName);
    const taskDir = path.join(stateDir, 'tasks', agentName);
    const pidFile = path.join(stateDir, 'pids', `${agentName}.pid`);

    expect(processAlive(vmPID)).toBe(true);
    expect(processAlive(enforcerPID)).toBe(true);
    expect(existsSync(runtimeDir)).toBe(true);
    expect(existsSync(taskDir)).toBe(true);
    expect(existsSync(pidFile)).toBe(true);

    const stopResponse = await page.request.post(`/api/v1/agents/${encodeURIComponent(agentName)}/stop`, {
      headers: await authHeaders(page),
      data: {},
      timeout: 60_000,
    });
    expect(stopResponse.ok()).toBe(true);

    await expect.poll(() => processAlive(vmPID), { timeout: 30_000, intervals: [500, 1000, 2000] }).toBe(false);
    await expect.poll(() => processAlive(enforcerPID), { timeout: 30_000, intervals: [500, 1000, 2000] }).toBe(false);
    await expect.poll(() => existsSync(runtimeDir), { timeout: 30_000, intervals: [500, 1000, 2000] }).toBe(false);
    await expect.poll(() => existsSync(taskDir), { timeout: 30_000, intervals: [500, 1000, 2000] }).toBe(false);
    await expect.poll(() => existsSync(pidFile), { timeout: 30_000, intervals: [500, 1000, 2000] }).toBe(false);

    await deleteAgent(page, agentName);
    const showResponse = await page.request.get(`/api/v1/agents/${encodeURIComponent(agentName)}`, {
      headers: await authHeaders(page),
      timeout: 10_000,
    });
    expect(showResponse.status()).toBe(404);
  } finally {
    await deleteAgent(page, agentName);
  }
});
