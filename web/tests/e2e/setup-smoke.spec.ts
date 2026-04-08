import { expect, test } from '@playwright/test';

test('setup wizard can advance past hub sync into welcome', async ({ page }) => {
  await page.route('**/__agency/config', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ token: '', gateway: '' }),
    });
  });

  await page.route('**/api/v1/hub/update', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ok: true }),
    });
  });

  await page.route('**/api/v1/hub/upgrade', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ components: [] }),
    });
  });

  await page.goto('/setup');

  await expect(page.getByRole('heading', { name: 'Welcome to Agency' })).toBeVisible();
  const nameInput = page.getByPlaceholder('operator');
  await expect(nameInput).toBeVisible();

  await page.route('**/api/v1/init', async (route) => {
    const body = route.request().postDataJSON();
    expect(body).toMatchObject({ operator: 'operator' });
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'ok', home: '/tmp/agency-home' }),
    });
  });

  await nameInput.fill('operator');
  await page.getByRole('button', { name: 'Continue' }).click();
  await expect(page.getByRole('heading', { name: 'LLM Providers' })).toBeVisible();
});
