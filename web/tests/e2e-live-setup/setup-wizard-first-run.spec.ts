import { expect, test, type APIRequestContext } from '@playwright/test';

const provider = process.env.AGENCY_SETUP_PROVIDER || 'gemini';
const providerLabel = process.env.AGENCY_SETUP_PROVIDER_LABEL || 'Google Gemini';
const providerKey = process.env.AGENCY_SETUP_PROVIDER_API_KEY || '';
const agentName = process.env.AGENCY_SETUP_AGENT_NAME || `alpha-setup-${Date.now()}`;
const apiToken = process.env.AGENCY_SETUP_API_TOKEN || '';
const apiHeaders = apiToken ? { Authorization: `Bearer ${apiToken}` } : undefined;
const gatewayURL = process.env.AGENCY_GATEWAY_URL || '';
const waitForReply = process.env.AGENCY_SETUP_WAIT_FOR_REPLY === '1';

function apiPath(path: string) {
  return gatewayURL ? `${gatewayURL}/api/v1${path}` : `/api/v1${path}`;
}

async function pollMessages(request: APIRequestContext, channel: string) {
  const response = await request.get(apiPath(`/comms/channels/${channel}/messages?limit=50&reader=operator`), {
    headers: apiHeaders,
  });
  expect(response.ok(), `read ${channel} messages returned ${response.status()}`).toBeTruthy();
  return response.json() as Promise<Array<{ author?: string; content?: string; flags?: Record<string, boolean> }>>;
}

async function waitForAgentReply(request: APIRequestContext, channel: string, timeoutMs = 180_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const messages = await pollMessages(request, channel);
    if (messages.some((message) =>
      message.author === agentName &&
      !message.flags?.system &&
      (message.content || '').trim().length > 0,
    )) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 2_000));
  }
  throw new Error(`agent did not respond in ${channel} within ${timeoutMs}ms`);
}

async function cleanupSetupAgent(request: APIRequestContext) {
  const channel = `dm-${agentName}`;
  await request.delete(apiPath(`/agents/${agentName}`), { headers: apiHeaders }).catch(() => undefined);
  await request.post(apiPath(`/comms/channels/${channel}/archive`), { headers: apiHeaders, data: {} }).catch(() => undefined);
}

test.afterEach(async ({ request }) => {
  await cleanupSetupAgent(request);
});

test('first-run setup wizard reaches a live agent chat', async ({ page, request }) => {
  test.skip(!providerKey, 'AGENCY_SETUP_PROVIDER_API_KEY is required for the live setup wizard check');

  await page.goto('/setup');

  await expect(page.getByRole('heading', { name: /Preparing your platform|Welcome to Agency/ })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Welcome to Agency' })).toBeVisible({ timeout: 180_000 });

  await page.getByPlaceholder('operator').fill('operator');
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByRole('heading', { name: 'LLM Providers' })).toBeVisible();
  await page.getByRole('button', { name: providerLabel }).click();
  await page.locator('input[type="password"]').fill(providerKey);
  await page.getByRole('button', { name: 'Verify & Save' }).click();
  await expect(page.getByRole('button', { name: 'Continue' })).toBeEnabled({ timeout: 120_000 });
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByRole('heading', { name: 'Your First Agent' })).toBeVisible();
  const nameInput = page.locator('input').first();
  await nameInput.fill(agentName);
  await page.getByRole('button', { name: 'Create & Start' }).click();

  await expect(page.getByRole('heading', { name: 'What should your agents be able to do?' })).toBeVisible({ timeout: 300_000 });
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByRole('heading', { name: `Talk to ${agentName}` })).toBeVisible({ timeout: 120_000 });
  await expect(page.getByPlaceholder('What can you help me with?')).toBeEnabled({ timeout: 180_000 });

  const channel = `dm-${agentName}`;
  await expect.poll(async () => {
    const messages = await pollMessages(request, channel);
    return messages.some((message) =>
      (message.author === 'operator' || message.author === '_operator') &&
      !message.flags?.system &&
      (message.content || '').includes(`Hey ${agentName}`),
    );
  }, {
    message: 'setup wizard should send the initial operator prompt',
    timeout: 60_000,
    intervals: [2_000],
  }).toBe(true);

  if (waitForReply) {
    await waitForAgentReply(request, channel);
  }

  await expect(page.getByText(/Application Error|Something went wrong/)).toHaveCount(0);
});
