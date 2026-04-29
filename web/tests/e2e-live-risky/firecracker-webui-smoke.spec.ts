import { expect, test, type Page } from '@playwright/test';
import { execFile } from 'node:child_process';
import { appendFileSync, existsSync, mkdirSync, readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { promisify } from 'node:util';

const APP_ERROR_PATTERN = /Application Error|Something went wrong/;
const SETUP_HEADING_PATTERN = /Welcome to Agency|Re-configure Agency|Preparing your platform/;
const enabled = process.env.AGENCY_E2E_FIRECRACKER_WEBUI === '1';
const agencyBin = process.env.AGENCY_BIN || 'agency';
const execFileAsync = promisify(execFile);
const metricsFile = process.env.AGENCY_E2E_FIRECRACKER_METRICS_FILE || '';
const metricsMode = process.env.AGENCY_E2E_FIRECRACKER_ENFORCEMENT_MODE || '';

test.describe.configure({ timeout: 240_000 });
test.skip(!enabled, 'requires AGENCY_E2E_FIRECRACKER_WEBUI=1 and a Firecracker-capable live stack');

let cachedAuthHeaders: Record<string, string> | null = null;

type RuntimeManifest = {
  spec?: {
    package?: {
      env?: Record<string, string>;
    };
    transport?: {
      enforcer?: {
        type?: string;
        endpoint?: string;
      };
    };
  };
  backendStatus?: {
    details?: Record<string, string>;
  };
};

type RuntimeStatus = {
  healthy?: boolean;
  phase?: string;
  details?: Record<string, string>;
};

type SecurityMetrics = {
  transport_type: string;
  transport_endpoint: string;
  enforcement_mode: string | undefined;
  enforcer_substrate: string | undefined;
  enforcer_component_state: string | undefined;
  vsock_bridge_state: string | undefined;
  body_ws_connected: string | undefined;
  workload_host_service_target_env_count: number;
  workload_host_only_env_count: number;
  mediation_audit_seen: boolean;
  llm_audit_seen: boolean;
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

function writeMetric(data: Record<string, unknown>) {
  if (!metricsFile) return;
  mkdirSync(path.dirname(metricsFile), { recursive: true });
  appendFileSync(metricsFile, `${JSON.stringify({
    at: new Date().toISOString(),
    mode: metricsMode,
    ...data,
  })}\n`);
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

async function runtimeStatus(page: Page, name: string): Promise<RuntimeStatus> {
  const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(name)}/runtime/status`, {
    headers: await authHeaders(page),
  });
  if (!response.ok()) {
    throw new Error(`runtime status failed for ${name}: ${response.status()}`);
  }
  return response.json() as Promise<RuntimeStatus>;
}

async function agentLogs(page: Page, name: string) {
  const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(name)}/logs`, {
    headers: await authHeaders(page),
  });
  if (!response.ok()) return [];
  return response.json() as Promise<Array<Record<string, unknown>>>;
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

function rssKiB(pid: number): number | undefined {
  if (!processAlive(pid)) return undefined;
  try {
    const status = readFileSync(`/proc/${pid}/status`, 'utf8');
    const match = status.match(/^VmRSS:\s+(\d+)\s+kB$/m);
    return match ? Number.parseInt(match[1], 10) : undefined;
  } catch {
    return undefined;
  }
}

function diskBytes(targetPath: string): number | undefined {
  if (!existsSync(targetPath)) return undefined;
  const info = statSync(targetPath);
  let total = (info.blocks ?? Math.ceil(info.size / 512)) * 512;
  if (!info.isDirectory()) return total;
  for (const entry of readdirSync(targetPath)) {
    total += diskBytes(path.join(targetPath, entry)) ?? 0;
  }
  return total;
}

function stateDirFromManifest(manifest: RuntimeManifest): string {
  const logPath = manifest.backendStatus?.details?.log_path;
  if (!logPath) throw new Error('runtime manifest missing firecracker log_path');
  return path.dirname(path.dirname(logPath));
}

async function createAgentThroughUI(page: Page, name: string) {
  const started = Date.now();
  await deleteAgent(page, name);
  await page.getByRole('button', { name: 'Create new agent' }).click();
  await expect(page.getByRole('heading', { name: 'Create Agent' })).toBeVisible();
  await page.getByLabel('Name').fill(name);
  await page.getByRole('button', { name: /^Create$/ }).click();
  await expect(page).toHaveURL(new RegExp(`/channels/dm-${name}$`), { timeout: 180_000 });
  await waitForDmReady(page, name);
  return Date.now() - started;
}

async function sendDMAndWaitForReply(page: Page, name: string, prompt: string) {
  const started = Date.now();
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
  return Date.now() - started;
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

async function waitForRuntimeHealthy(page: Page, name: string) {
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/agents/${encodeURIComponent(name)}/runtime/status`, {
      headers: await authHeaders(page),
    });
    if (!response.ok()) return 'unavailable';
    const status = await response.json() as RuntimeStatus;
    if (!status.healthy) return status.phase ?? 'unhealthy';
    return [
      status.phase,
      status.details?.workload_vm_state,
      status.details?.enforcer_component_state,
      status.details?.vsock_bridge_state,
      status.details?.body_ws_connected,
    ].join('|');
  }, { timeout: 120_000, intervals: [2000, 5000, 10000] }).toBe('running|running|running|running|true');
}

async function waitForEnforcerReload(page: Page, name: string, before: RuntimeStatus) {
  if (before.details?.enforcer_substrate !== 'microvm') {
    await waitForRuntimeHealthy(page, name);
    return;
  }
  const previousPID = before.details?.enforcer_pid ?? '';
  await expect.poll(async () => {
    const status = await runtimeStatus(page, name);
    const details = status.details ?? {};
    if (!status.healthy) return 'unhealthy';
    return [
      status.phase,
      details.workload_vm_state,
      details.enforcer_component_state,
      details.vsock_bridge_state,
      details.body_ws_connected,
      previousPID && details.enforcer_pid !== previousPID ? 'reloaded' : 'same',
    ].join('|');
  }, { timeout: 120_000, intervals: [2000, 5000, 10000] }).toBe('running|running|running|running|true|reloaded');
}

async function runtimeResourceMetrics(page: Page, name: string) {
  const manifest = await runtimeManifest(page, name);
  const stateDir = stateDirFromManifest(manifest);
  const vmPID = pidFromManifest(manifest, 'pid');
  const enforcerPID = pidFromManifest(manifest, 'enforcer_pid');
  return {
    workload_rss_kib: rssKiB(vmPID),
    enforcer_rss_kib: rssKiB(enforcerPID),
    workload_task_bytes: diskBytes(path.join(stateDir, 'tasks', name)),
    enforcer_task_bytes: diskBytes(path.join(stateDir, 'tasks', `${name}-enforcer`)),
  };
}

async function runtimeSecurityMetrics(page: Page, name: string): Promise<SecurityMetrics> {
  const [manifest, status, logs] = await Promise.all([
    runtimeManifest(page, name),
    runtimeStatus(page, name),
    agentLogs(page, name),
  ]);
  const env = manifest.spec?.package?.env ?? {};
  const envKeys = Object.keys(env);
  const hostServiceTargetEnvCount = envKeys.filter((key) => key.startsWith('AGENCY_FIRECRACKER_HOST_SERVICE_TARGET_')).length;
  const workloadHostOnlyEnvCount = envKeys.filter((key) => (
    key.startsWith('AGENCY_FIRECRACKER_HOST_SERVICE_TARGET_') ||
    key === 'AGENCY_FIRECRACKER_ROOTFS_OVERLAYS'
  )).length;
  const transportType = manifest.spec?.transport?.enforcer?.type ?? '';
  const transportEndpoint = manifest.spec?.transport?.enforcer?.endpoint ?? '';
  const details = status.details ?? {};
  const logText = JSON.stringify(logs);
  const mediationAudit = /MEDIATION_|mediation/i.test(logText);
  const llmAudit = /LLM_|llm/i.test(logText);

  expect(transportType).toBe('vsock_http');
  expect(transportEndpoint).toMatch(/^vsock:\/\/2:\d+$/);
  expect(status.healthy).toBe(true);
  expect(details.enforcer_component_state).toBe('running');
  expect(details.vsock_bridge_state).toBe('running');
  expect(details.body_ws_connected).toBe('true');
  expect(hostServiceTargetEnvCount).toBe(0);
  expect(workloadHostOnlyEnvCount).toBe(0);
  if (metricsMode) {
    expect(details.enforcement_mode).toBe(metricsMode);
    expect(details.enforcer_substrate).toBe(metricsMode);
  }
  return {
    transport_type: transportType,
    transport_endpoint: transportEndpoint,
    enforcement_mode: details.enforcement_mode,
    enforcer_substrate: details.enforcer_substrate,
    enforcer_component_state: details.enforcer_component_state,
    vsock_bridge_state: details.vsock_bridge_state,
    body_ws_connected: details.body_ws_connected,
    workload_host_service_target_env_count: hostServiceTargetEnvCount,
    workload_host_only_env_count: workloadHostOnlyEnvCount,
    mediation_audit_seen: mediationAudit,
    llm_audit_seen: llmAudit,
  };
}

async function waitForSecurityMetrics(page: Page, name: string): Promise<SecurityMetrics> {
  let last: SecurityMetrics | null = null;
  await expect.poll(async () => {
    try {
      last = await runtimeSecurityMetrics(page, name);
      return last.mediation_audit_seen;
    } catch {
      return false;
    }
  }, { timeout: 30_000, intervals: [1000, 2000, 5000] }).toBe(true);
  return last ?? runtimeSecurityMetrics(page, name);
}

test('Firecracker agent can be managed and messaged through the web UI', async ({ page }) => {
  const agentName = `fc-webui-${Date.now()}`;
  const prompt = `Web UI Firecracker smoke ${agentName}: acknowledge briefly.`;

  try {
    await page.goto('/agents');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) return;

    const createMs = await createAgentThroughUI(page, agentName);

    await page.goto(`/agents/${encodeURIComponent(agentName)}`);
    await settle(page);
    await expect(page.getByText(agentName).first()).toBeVisible();
    await expect(page.getByText('running').first()).toBeVisible();
    await page.getByRole('button', { name: 'Open DM' }).click();
    await expect(page).toHaveURL(new RegExp(`/channels/dm-${agentName}$`));
    const dmMs = await sendDMAndWaitForReply(page, agentName, prompt);
    const securityMetrics = await waitForSecurityMetrics(page, agentName);
    writeMetric({
      test: 'manage',
      agent: agentName,
      create_ms: createMs,
      dm_ms: dmMs,
      ...(await runtimeResourceMetrics(page, agentName)),
      ...securityMetrics,
    });
    expect(securityMetrics.mediation_audit_seen).toBe(true);
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

    const createMs = await createAgentThroughUI(page, agentName);
    const preRestartDmMs = await sendDMAndWaitForReply(page, agentName, `Firecracker recovery precheck ${agentName}: reply briefly.`);

    const restartStarted = Date.now();
    await runAgency(['serve', 'restart']);
    await waitForGateway(page);

    await waitForRuntimeHealthy(page, agentName);
    const restartRecoverMs = Date.now() - restartStarted;

    await page.goto(`/agents/${encodeURIComponent(agentName)}`);
    await settle(page);
    await expect(page.getByText('running').first()).toBeVisible();
    const postRestartDmMs = await sendDMAndWaitForReply(page, agentName, `Firecracker recovery postcheck ${agentName}: reply briefly.`);
    const securityMetrics = await waitForSecurityMetrics(page, agentName);
    writeMetric({
      test: 'recover',
      agent: agentName,
      create_ms: createMs,
      pre_restart_dm_ms: preRestartDmMs,
      restart_recover_ms: restartRecoverMs,
      post_restart_dm_ms: postRestartDmMs,
      ...(await runtimeResourceMetrics(page, agentName)),
      ...securityMetrics,
    });
    expect(securityMetrics.mediation_audit_seen).toBe(true);
  } finally {
    await deleteAgent(page, agentName);
  }
});

test('Firecracker enforcer config reload preserves Web UI messaging', async ({ page }) => {
  const agentName = `fc-reload-${Date.now()}`;

  try {
    await page.goto('/agents');
    const initialized = await expectSetupOrInitialized(page);
    if (!initialized) return;

    const createMs = await createAgentThroughUI(page, agentName);
    const preReloadDmMs = await sendDMAndWaitForReply(page, agentName, `Firecracker reload precheck ${agentName}: reply briefly.`);
    const beforeReload = await runtimeStatus(page, agentName);

    const reloadStarted = Date.now();
    const configResponse = await page.request.put(`/api/v1/agents/${encodeURIComponent(agentName)}/config`, {
      headers: await authHeaders(page),
      data: {
        identity: `You are ${agentName}. Continue following all current Agency constraints.\n`,
      },
      timeout: 10_000,
    });
    expect(configResponse.ok()).toBe(true);
    await waitForEnforcerReload(page, agentName, beforeReload);
    const reloadMs = Date.now() - reloadStarted;

    const postReloadDmMs = await sendDMAndWaitForReply(page, agentName, `Firecracker reload postcheck ${agentName}: reply briefly.`);
    const securityMetrics = await waitForSecurityMetrics(page, agentName);
    writeMetric({
      test: 'reload',
      agent: agentName,
      create_ms: createMs,
      pre_reload_dm_ms: preReloadDmMs,
      reload_ms: reloadMs,
      post_reload_dm_ms: postReloadDmMs,
      ...(await runtimeResourceMetrics(page, agentName)),
      ...securityMetrics,
    });
    expect(securityMetrics.mediation_audit_seen).toBe(true);
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

    const createMs = await createAgentThroughUI(page, agentName);
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
    const securityMetrics = await waitForSecurityMetrics(page, agentName);

    const stopResponse = await page.request.post(`/api/v1/agents/${encodeURIComponent(agentName)}/stop`, {
      headers: await authHeaders(page),
      data: {},
      timeout: 60_000,
    });
    expect(stopResponse.ok()).toBe(true);

    const cleanupStarted = Date.now();
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
    writeMetric({
      test: 'cleanup',
      agent: agentName,
      create_ms: createMs,
      ...securityMetrics,
      cleanup_ms: Date.now() - cleanupStarted,
      workload_rss_kib: rssKiB(vmPID),
      enforcer_rss_kib: rssKiB(enforcerPID),
    });
  } finally {
    await deleteAgent(page, agentName);
  }
});
