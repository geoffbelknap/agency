import { expect, test } from '@playwright/test';

test('setup wizard can advance past platform readiness into providers', async ({ page }) => {
  await page.route('**/__agency/config', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ token: '', gateway: '' }),
    });
  });

  await page.route('**/api/v1/infra/status', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ version: '0.2.1', build_id: 'test-build', components: [] }),
    });
  });

  await page.route('**/api/v1/infra/routing/config', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ configured: false, version: 'test-build' }),
    });
  });

  await page.route('**/api/v1/infra/setup/config', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ providers: { anthropic: { configured: true, validated: true } } }),
    });
  });

  await page.route('**/api/v1/infra/providers', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          name: 'anthropic',
          display_name: 'Anthropic',
          description: 'Claude models',
          category: 'cloud',
          installed: true,
          credential_name: 'anthropic-api-key',
          credential_label: 'API key',
          api_key_url: 'https://console.anthropic.com/settings/keys',
          credential_configured: true,
        },
      ]),
    });
  });

  await page.goto('/setup');
  await expect(page.getByRole('heading', { name: 'Prepare the workspace' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Connect providers' })).toBeVisible();
  await expect(page.getByText('Provider readiness')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Continue' })).toBeVisible();
});
