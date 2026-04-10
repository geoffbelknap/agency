import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e-live',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: process.env.AGENCY_WEB_BASE_URL || 'http://127.0.0.1:8280',
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
