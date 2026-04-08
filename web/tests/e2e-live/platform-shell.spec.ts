import { expect, test } from '@playwright/test';

test('live stack serves health and renders a top-level UI shell', async ({ page, request }) => {
  const health = await request.get('/health');
  expect(health.ok()).toBeTruthy();

  await page.goto('/');
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(1500);

  await expect(page.getByText(/Application Error|Something went wrong/)).toHaveCount(0);

  const bodyText = await page.locator('body').innerText();
  expect(bodyText).toMatch(
    /Preparing your platform|Welcome to Agency|Re-configure Agency|Channels|Agents|Admin/,
  );
});

test('live stack routes to setup or initialized navigation without app errors', async ({ page }) => {
  await page.goto('/');
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(1500);

  await expect(page.getByText(/Application Error|Something went wrong/)).toHaveCount(0);

  const setupHeading = page.getByRole('heading', {
    name: /Welcome to Agency|Re-configure Agency|Preparing your platform/,
  });

  if (await setupHeading.count()) {
    await expect(setupHeading.first()).toBeVisible();
    await expect(page).toHaveURL(/\/setup$/);
    return;
  }

  await expect(page.getByRole('link', { name: 'Channels' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Agents' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Admin' })).toBeVisible();

  await page.goto('/admin');
  await expect(page.getByRole('heading', { name: 'Admin' })).toBeVisible();
});
